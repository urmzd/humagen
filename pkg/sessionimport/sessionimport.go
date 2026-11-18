// Package sessionimport reads the sessions other assistants keep on a
// person's machine, or hand out as an export, into one plain shape: who said
// what, in order. It reads Claude Code and Codex session files, and the
// conversation exports of ChatGPT and claude.ai.
//
// It only reads. What a caller does with a session, such as adding it to a
// knowledge source, is the caller's.
package sessionimport

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/klauspost/compress/zstd"
)

// The tools a session comes from.
const (
	Claude   = "claude"
	Codex    = "codex"
	ChatGPT  = "chatgpt"
	ClaudeAI = "claude-ai"
)

// ErrNotSession is a file that holds no conversation: an empty one, or one
// in a shape this package does not read.
var ErrNotSession = errors.New("sessionimport: not a session")

// Turn is one thing said. Role is "user", "assistant", "tool" or "result": a
// tool turn is one line naming a tool the assistant called and what it
// called it on, and a result turn is what a tool gave back, cut to
// MaxResult.
type Turn struct {
	Role string
	Text string
	// At is when the export says it was said. Zero is an export that does
	// not say.
	At time.Time
	// ID is the original id of the message the turn is from, and Call the
	// original id of the tool call a tool or result turn is of. ChatGPT gives
	// a call no id of its own: a result's Call is the id of the message that
	// made the call. "" is an export that has none.
	ID   string
	Call string
	// Attached names the files the message carried: each one's name, type
	// and size where the export states them, and its text where the export
	// holds text. The files themselves are not read.
	Attached string
}

// Session is one conversation.
type Session struct {
	Tool string
	ID   string
	// Title is the tool's own title, or the start of the first thing asked.
	Title string
	// Dir is the working directory it ran in. Exports have none.
	Dir     string
	Started time.Time
	// Child is set on a session another session started: a sub-agent's or a
	// reviewer's. A person did not write its prompts.
	Child bool
	// Parent is the URI of the session that started a child, when the file
	// says.
	Parent string
	// Turns is the main path: the branch of the conversation that was last
	// shown, or that the file ends on.
	Turns []Turn
	// Side holds the other branches: the edits and retries a person moved on
	// from, at most MaxBranches. Branches counts the ones left out past that.
	Side     []Branch
	Branches int
	// Causal is set when the export says which turn each turn follows, and
	// the turns are in that order. Otherwise they are in time order.
	Causal bool
}

// URI names the session where it came from, the same on every read.
func (s Session) URI() string { return s.Tool + ":" + s.ID }

// Asked counts what a person asked.
func (s Session) Asked() int {
	n := 0
	for _, t := range s.Turns {
		if t.Role == "user" {
			n++
		}
	}
	return n
}

// MaxResult bounds what is kept of one tool result.
const MaxResult = 1500

// result is a tool's output as a turn keeps it: its start, and a mark when
// cut.
func result(s string) string { return clip(strings.TrimSpace(s), MaxResult) }

// maxLine bounds one record of a session file. A record holds at most one
// message; a larger one is a tool's output, which is skipped anyway.
const maxLine = 64 << 20

// lines calls fn with each line of r, however long, and skips one over
// maxLine.
func lines(r io.Reader, fn func([]byte)) error {
	br := bufio.NewReaderSize(r, 1<<20)
	var buf []byte
	skipping := false
	for {
		chunk, err := br.ReadSlice('\n')
		if !skipping {
			buf = append(buf, chunk...)
			if len(buf) > maxLine {
				buf, skipping = buf[:0], true
			}
		}
		switch {
		case err == nil:
			if !skipping && len(bytes.TrimSpace(buf)) > 0 {
				fn(buf)
			}
			buf, skipping = buf[:0], false
		case errors.Is(err, bufio.ErrBufferFull):
			continue
		case errors.Is(err, io.EOF):
			if !skipping && len(bytes.TrimSpace(buf)) > 0 {
				fn(buf)
			}
			return nil
		default:
			return err
		}
	}
}

var (
	reminder    = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	commandName = regexp.MustCompile(`(?s)<command-name>(.*?)</command-name>`)
	commandArgs = regexp.MustCompile(`(?s)<command-args>(.*?)</command-args>`)
)

// claudeText is what a person typed, out of a Claude Code user record's
// text. A record the harness wrote for the model is "".
func claudeText(s string) string {
	s = strings.TrimSpace(reminder.ReplaceAllString(s, ""))
	if m := commandName.FindStringSubmatch(s); m != nil {
		out := strings.TrimSpace(m[1])
		if a := commandArgs.FindStringSubmatch(s); a != nil && strings.TrimSpace(a[1]) != "" {
			out += " " + strings.TrimSpace(a[1])
		}
		return out
	}
	for _, p := range []string{"<local-command-stdout>", "<local-command-stderr>", "<local-command-caveat>", "<task-notification>", "<bash-stdout>", "<bash-stderr>"} {
		if strings.HasPrefix(s, p) {
			return ""
		}
	}
	return s
}

// called is one line for a tool call: the tool and the argument that says
// what it worked on.
func called(name string, input json.RawMessage) string {
	var args map[string]any
	_ = json.Unmarshal(input, &args)
	for _, k := range []string{"command", "cmd", "file_path", "path", "url", "query", "pattern", "description", "prompt", "skill"} {
		if v, ok := args[k].(string); ok && strings.TrimSpace(v) != "" {
			return name + ": " + clip(oneLine(v), 200)
		}
		if v, ok := args[k].([]any); ok && len(v) > 0 {
			parts := make([]string, 0, len(v))
			for _, p := range v {
				parts = append(parts, fmt.Sprint(p))
			}
			return name + ": " + clip(oneLine(strings.Join(parts, " ")), 200)
		}
	}
	if args == nil {
		var raw string
		if json.Unmarshal(input, &raw) == nil && strings.TrimSpace(raw) != "" {
			return name + ": " + clip(oneLine(raw), 200)
		}
	}
	return name
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip cuts s to at most n bytes on a rune boundary, with a mark when cut.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// finish titles a session and refuses one nobody asked anything in.
func (s Session) finish() (Session, error) {
	if s.ID == "" || s.Asked() == 0 {
		return Session{}, ErrNotSession
	}
	if strings.TrimSpace(s.Title) == "" {
		for _, t := range s.Turns {
			if t.Role == "user" && s.Title == "" {
				s.Title = clip(oneLine(t.Text), 80)
			}
		}
		// Nothing typed: the first thing asked was a file.
		for _, t := range s.Turns {
			if t.Role == "user" && s.Title == "" {
				s.Title = clip(oneLine(t.Attached), 80)
			}
		}
	}
	s.Title = oneLine(s.Title)
	if s.Started.IsZero() {
		for _, t := range s.Turns {
			if !t.At.IsZero() {
				s.Started = t.At
				break
			}
		}
	}
	return s, nil
}

type block struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ToolUseID string          `json:"tool_use_id"`
	Text      string          `json:"text"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	Content   json.RawMessage `json:"content"`
	Output    json.RawMessage `json:"output"`
	// ImageURL is a Codex image, and Source a Claude Code image or document.
	ImageURL string `json:"image_url"`
	Source   struct {
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// blockText is the text of a tool result's content: a string, or a list of
// blocks whose text is joined and whose other kinds are named.
func blockText(raw json.RawMessage) string {
	var str string
	if json.Unmarshal(raw, &str) == nil {
		return str
	}
	var blocks []struct {
		Type  string `json:"type"`
		Text  string `json:"text"`
		Title string `json:"title"`
	}
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var out []string
	for _, b := range blocks {
		switch {
		case b.Text != "":
			out = append(out, b.Text)
		case b.Title != "":
			out = append(out, b.Title)
		case b.Type == "image":
			out = append(out, "[image; the image itself is not imported]")
		case b.Type != "":
			out = append(out, "["+b.Type+"]")
		}
	}
	return strings.Join(out, "\n")
}

// ReadClaude reads one Claude Code session file (~/.claude/projects/*/*.jsonl).
// Its records name their parents: the main path is the one the file's last
// message ends, and a message a person went back and wrote again starts a
// branch.
func ReadClaude(r io.Reader) (Session, error) {
	s := Session{Tool: Claude}
	var nodes []node
	leaf := -1
	err := lines(r, func(line []byte) {
		var rec struct {
			Type          string `json:"type"`
			UUID          string `json:"uuid"`
			Parent        string `json:"parentUuid"`
			LogicalParent string `json:"logicalParentUuid"`
			SessionID     string `json:"sessionId"`
			Cwd           string `json:"cwd"`
			Timestamp     string `json:"timestamp"`
			IsSidechain   bool   `json:"isSidechain"`
			IsMeta        bool   `json:"isMeta"`
			AITitle       string `json:"aiTitle"`
			Message       struct {
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		if s.ID == "" {
			s.ID = rec.SessionID
		}
		if s.Dir == "" {
			s.Dir = rec.Cwd
		}
		at := parseTime(rec.Timestamp)
		if s.Started.IsZero() {
			s.Started = at
		}
		if rec.Type == "ai-title" {
			s.Title = rec.AITitle
		}
		spoke := rec.Type == "user" || rec.Type == "assistant"
		// A sub-agent's records are a chain of their own. Every other record
		// with an id may be a parent, whatever it holds.
		if rec.IsSidechain || (!spoke && rec.UUID == "") {
			return
		}
		nd := node{id: rec.UUID, parent: rec.Parent}
		if nd.parent == "" {
			nd.parent = rec.LogicalParent
		}
		if spoke && !rec.IsMeta {
			var text string
			var blocks []block
			if json.Unmarshal(rec.Message.Content, &text) != nil {
				_ = json.Unmarshal(rec.Message.Content, &blocks)
			} else {
				blocks = []block{{Type: "text", Text: text}}
			}
			var ts turns
			var files []attachment
			for _, b := range blocks {
				switch {
				case b.Type == "text" && rec.Type == "user":
					ts.add("user", claudeText(b.Text), at, rec.UUID, "")
				case b.Type == "text":
					ts.add("assistant", b.Text, at, rec.UUID, "")
				case (b.Type == "image" || b.Type == "document") && rec.Type == "user":
					files = append(files, attachment{name: b.Name, kind: b.Source.MediaType, size: encoded(b.Source.Data), image: b.Type == "image"})
				case b.Type == "tool_use":
					ts.add("tool", called(b.Name, b.Input), at, rec.UUID, b.ID)
				case b.Type == "tool_result":
					ts.add("result", result(blockText(b.Content)), at, rec.UUID, b.ToolUseID)
				}
			}
			ts.attach("user", files, at, rec.UUID)
			nd.turns = ts
		}
		nodes = append(nodes, nd)
		if len(nd.turns) > 0 {
			leaf = len(nodes) - 1
		}
	})
	if err != nil {
		return Session{}, err
	}
	s.grow(nodes, leaf, true)
	return s.finish()
}

// codexPreamble starts a user message Codex wrote itself: the instructions
// and the environment it gives the model before what the person typed.
var codexPreamble = []string{"# AGENTS.md instructions", "<environment_context", "<user_instructions", "<turn_aborted", "<permissions instructions", "<INSTRUCTIONS>", "<collaboration_mode", "<subagent_notification", "<task-notification>"}

// ReadCodex reads one Codex session file (~/.codex/sessions/**/rollout-*.jsonl),
// already decompressed.
func ReadCodex(r io.Reader) (Session, error) {
	s := Session{Tool: Codex}
	var ts turns
	err := lines(r, func(line []byte) {
		var rec struct {
			Type      string          `json:"type"`
			Timestamp string          `json:"timestamp"`
			Payload   json.RawMessage `json:"payload"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		// Before sessions had envelopes, each line was the item itself.
		body := rec.Payload
		if len(body) == 0 {
			body = line
		}
		var p struct {
			Type           string          `json:"type"`
			ID             string          `json:"id"`
			CallID         string          `json:"call_id"`
			Cwd            string          `json:"cwd"`
			Timestamp      string          `json:"timestamp"`
			ParentThreadID string          `json:"parent_thread_id"`
			Source         json.RawMessage `json:"source"`
			Role           string          `json:"role"`
			Name           string          `json:"name"`
			Arguments      json.RawMessage `json:"arguments"`
			Input          json.RawMessage `json:"input"`
			Output         json.RawMessage `json:"output"`
			Content        []block         `json:"content"`
		}
		if json.Unmarshal(body, &p) != nil {
			return
		}
		at := parseTime(rec.Timestamp)
		meta := rec.Type == "session_meta" || (rec.Type == "" && len(rec.Payload) == 0 && p.Type == "" && p.ID != "")
		switch {
		case meta:
			if s.ID == "" {
				s.ID, s.Dir, s.Started = p.ID, p.Cwd, parseTime(p.Timestamp)
				if p.ParentThreadID != "" {
					s.Parent = Codex + ":" + p.ParentThreadID
				}
				// A sub-agent's source is an object naming what started it.
				s.Child = p.ParentThreadID != "" || bytes.HasPrefix(bytes.TrimSpace(p.Source), []byte("{"))
			}
		case rec.Type == "event_msg", rec.Type == "turn_context", rec.Type == "world_state":
			// Events repeat the items; the rest is the harness's own state.
		case p.Type == "message" && (p.Role == "user" || p.Role == "assistant"):
			var files []attachment
			for _, b := range p.Content {
				text := strings.TrimSpace(b.Text)
				if p.Role == "user" {
					for _, pre := range codexPreamble {
						if strings.HasPrefix(text, pre) {
							text = ""
							break
						}
					}
					if b.Type == "input_image" {
						kind, size := dataURL(b.ImageURL)
						if kind == "" {
							kind = "image"
						}
						files = append(files, attachment{kind: kind, size: size, image: true})
						continue
					}
				}
				ts.add(p.Role, text, at, p.ID, "")
			}
			ts.attach(p.Role, files, at, p.ID)
		case p.Type == "function_call", p.Type == "custom_tool_call", p.Type == "local_shell_call":
			args := p.Arguments
			if len(args) == 0 {
				args = p.Input
			}
			// Arguments are JSON kept as a string.
			var inner string
			if json.Unmarshal(args, &inner) == nil && json.Valid([]byte(inner)) && strings.HasPrefix(strings.TrimSpace(inner), "{") {
				args = json.RawMessage(inner)
			}
			name := p.Name
			if name == "" {
				name = "shell"
			}
			ts.add("tool", called(name, args), at, p.ID, p.CallID)
		case p.Type == "function_call_output", p.Type == "custom_tool_call_output", p.Type == "local_shell_call_output":
			// The output is a string, or an object that holds one.
			var out string
			if json.Unmarshal(p.Output, &out) != nil {
				var obj struct {
					Output  string `json:"output"`
					Content string `json:"content"`
				}
				_ = json.Unmarshal(p.Output, &obj)
				out = obj.Output + obj.Content
			}
			ts.add("result", result(out), at, p.ID, p.CallID)
		}
	})
	if err != nil {
		return Session{}, err
	}
	// A rollout is one line of turns and names no parents: time orders it.
	s.Turns = timed(ts)
	return s.finish()
}

// ReadExport reads a conversations.json from a ChatGPT or a claude.ai data
// export, calling fn with each conversation that has something asked in it.
// It reads one conversation at a time, so a large export is not held whole.
// empty counts the conversations with nothing asked in them, which an
// export holds for every chat that was opened and left.
func ReadExport(r io.Reader, fn func(Session) error) (empty int, err error) {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return 0, fmt.Errorf("%w: an export is a JSON list of conversations", ErrNotSession)
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return empty, err
		}
		s, err := exported(raw)
		if errors.Is(err, ErrNotSession) {
			empty++
			continue
		}
		if err != nil {
			return empty, err
		}
		if err := fn(s); err != nil {
			return empty, err
		}
	}
	return empty, nil
}

func unix(f float64) time.Time {
	if f <= 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(f*float64(time.Second))).UTC()
}

// gptMessage is one message of a ChatGPT conversation.
type gptMessage struct {
	ID     string `json:"id"`
	Author struct {
		Role string `json:"role"`
		Name string `json:"name"`
	} `json:"author"`
	CreateTime float64 `json:"create_time"`
	Content    struct {
		Parts []json.RawMessage `json:"parts"`
		Text  string            `json:"text"`
	} `json:"content"`
	// Recipient is "all", or the tool an assistant's message calls.
	Recipient string `json:"recipient"`
	Metadata  struct {
		Hidden      bool `json:"is_visually_hidden_from_conversation"`
		Attachments []struct {
			ID       string `json:"id"`
			Name     string `json:"name"`
			MimeType string `json:"mime_type"`
			Size     int64  `json:"size"`
		} `json:"attachments"`
	} `json:"metadata"`
}

// turns is what the message says. A message an assistant sends to a tool is
// a tool call, and a tool's message is its result: caller is the id of the
// message that called it.
func (m *gptMessage) turns(key, caller string) []Turn {
	role := m.Author.Role
	if m.Metadata.Hidden || (role != "user" && role != "assistant" && role != "tool") {
		return nil
	}
	id := m.ID
	if id == "" {
		id = key
	}
	at := unix(m.CreateTime)
	var parts []string
	var files []attachment
	named := make([]attachment, len(m.Metadata.Attachments))
	for i, a := range m.Metadata.Attachments {
		named[i] = attachment{name: a.Name, kind: a.MimeType, size: a.Size, image: isImage(a.MimeType, a.Name)}
	}
	for _, p := range m.Content.Parts {
		var text string
		if json.Unmarshal(p, &text) == nil {
			parts = append(parts, text)
			continue
		}
		// A part that is not text points at a file. It is one of the
		// message's named attachments, or is named by its kind alone.
		var ptr struct {
			ContentType string `json:"content_type"`
			Pointer     string `json:"asset_pointer"`
			Size        int64  `json:"size_bytes"`
		}
		_ = json.Unmarshal(p, &ptr)
		found := false
		for i, a := range m.Metadata.Attachments {
			if a.ID != "" && strings.Contains(ptr.Pointer, a.ID) {
				if found = true; named[i].size == 0 {
					named[i].size = ptr.Size
				}
				break
			}
		}
		if !found {
			files = append(files, attachment{kind: ptr.ContentType, size: ptr.Size, image: isImage(ptr.ContentType, "")})
		}
	}
	if m.Content.Text != "" {
		parts = append(parts, m.Content.Text)
	}
	text := strings.Join(parts, "\n")
	var ts turns
	switch {
	case role == "tool":
		ts.add("result", result(text), at, id, caller)
	case role == "assistant" && m.Recipient != "" && m.Recipient != "all":
		if line := clip(oneLine(text), 200); line != "" {
			ts.add("tool", m.Recipient+": "+line, at, id, "")
		}
	default:
		ts.add(role, text, at, id, "")
	}
	if role != "tool" {
		ts.attach(role, append(named, files...), at, id)
	}
	return ts
}

func exported(raw json.RawMessage) (Session, error) {
	var c struct {
		// ChatGPT.
		ID             string  `json:"id"`
		ConversationID string  `json:"conversation_id"`
		Title          string  `json:"title"`
		CreateTime     float64 `json:"create_time"`
		CurrentNode    string  `json:"current_node"`
		Mapping        map[string]struct {
			Parent   string      `json:"parent"`
			Children []string    `json:"children"`
			Message  *gptMessage `json:"message"`
		} `json:"mapping"`
		// claude.ai.
		UUID      string `json:"uuid"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
		Leaf      string `json:"current_leaf_message_uuid"`
		Messages  []struct {
			UUID        string  `json:"uuid"`
			Parent      string  `json:"parent_message_uuid"`
			Sender      string  `json:"sender"`
			Text        string  `json:"text"`
			CreatedAt   string  `json:"created_at"`
			Content     []block `json:"content"`
			Attachments []struct {
				Name    string `json:"file_name"`
				Type    string `json:"file_type"`
				Size    int64  `json:"file_size"`
				Content string `json:"extracted_content"`
			} `json:"attachments"`
			Files []struct {
				Name string `json:"file_name"`
				Kind string `json:"file_kind"`
			} `json:"files"`
		} `json:"chat_messages"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return Session{}, ErrNotSession
	}
	if c.Mapping == nil {
		s := Session{Tool: ClaudeAI, ID: c.UUID, Title: c.Name, Started: parseTime(c.CreatedAt)}
		var nodes []node
		var all turns
		tree, leaf := false, -1
		for i, m := range c.Messages {
			role, at := "assistant", parseTime(m.CreatedAt)
			if m.Sender == "human" {
				role = "user"
			}
			var ts turns
			// A message's blocks hold its text among the tools it used. An
			// older export has only the text.
			blocks := 0
			for _, b := range m.Content {
				switch b.Type {
				case "text":
					if strings.TrimSpace(b.Text) != "" {
						ts.add(role, b.Text, at, m.UUID, "")
						blocks++
					}
				case "tool_use":
					ts.add("tool", called(b.Name, b.Input), at, m.UUID, b.ID)
				case "tool_result":
					ts.add("result", result(blockText(b.Content)), at, m.UUID, b.ToolUseID)
				}
			}
			if blocks == 0 {
				ts.add(role, m.Text, at, m.UUID, "")
			}
			var files []attachment
			for _, a := range m.Attachments {
				files = append(files, attachment{name: a.Name, kind: a.Type, size: a.Size, text: a.Content, image: isImage(a.Type, a.Name)})
			}
			for _, f := range m.Files {
				files = append(files, attachment{name: f.Name, kind: f.Kind, image: isImage(f.Kind, f.Name)})
			}
			ts.attach(role, files, at, m.UUID)
			tree = tree || m.Parent != ""
			nodes = append(nodes, node{id: m.UUID, parent: m.Parent, turns: ts})
			all = append(all, ts...)
			// The export names no branch as the one shown, unless it names
			// its last message: the main path ends at the last one listed.
			if len(ts) > 0 && (leaf < 0 || c.Messages[leaf].UUID != c.Leaf || c.Leaf == "") {
				leaf = i
			}
		}
		if tree {
			s.grow(nodes, leaf, false)
		} else {
			// An older export names no parents: time orders it.
			s.Turns = timed(all)
		}
		return s.finish()
	}
	s := Session{Tool: ChatGPT, ID: c.ConversationID, Title: c.Title, Started: unix(c.CreateTime)}
	if s.ID == "" {
		s.ID = c.ID
	}
	// A mapping has no order of its own. Its records are read from each
	// start down, a record's children in the order it lists them, so the
	// same export always reads the same.
	keys := make([]string, 0, len(c.Mapping))
	under := map[string][]string{}
	for key := range c.Mapping {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var stack []string
	for i := len(keys) - 1; i >= 0; i-- {
		key := keys[i]
		if p := c.Mapping[key].Parent; p != "" && p != key {
			if _, ok := c.Mapping[p]; ok {
				under[p] = append(under[p], key)
				continue
			}
		}
		stack = append(stack, key)
	}
	var nodes []node
	seen := map[string]bool{}
	leaf := -1
	for len(stack) > 0 {
		key := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[key] {
			continue
		}
		seen[key] = true
		rec := c.Mapping[key]
		nd := node{id: key, parent: rec.Parent}
		if rec.Message != nil {
			// A tool's result names the message that called the tool.
			caller := ""
			if p, ok := c.Mapping[rec.Parent]; ok && p.Message != nil && p.Message.Author.Role == "assistant" && p.Message.Recipient != "" && p.Message.Recipient != "all" {
				if caller = p.Message.ID; caller == "" {
					caller = rec.Parent
				}
			}
			nd.turns = rec.Message.turns(key, caller)
		}
		nodes = append(nodes, nd)
		if key == c.CurrentNode {
			leaf = len(nodes) - 1
		}
		// Listed children first, in their order, then any the record does
		// not list; under holds them in reverse order already.
		var next []string
		for _, child := range rec.Children {
			if c.Mapping[child].Parent == key && child != key {
				next = append(next, child)
			}
		}
		listed := map[string]bool{}
		for _, child := range next {
			listed[child] = true
		}
		for _, child := range under[key] {
			if !listed[child] {
				stack = append(stack, child)
			}
		}
		for i := len(next) - 1; i >= 0; i-- {
			stack = append(stack, next[i])
		}
	}
	if leaf < 0 {
		// No record is named as the one shown: the path ends at the last
		// one that says something.
		for i, nd := range nodes {
			if len(nd.turns) > 0 {
				leaf = i
			}
		}
	}
	s.grow(nodes, leaf, false)
	return s.finish()
}

// Open reads the session file at path as tool's, decompressing a .zst.
func Open(tool, path string) (Session, error) {
	f, err := os.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer f.Close()
	var r io.Reader = f
	if strings.HasSuffix(path, ".zst") {
		z, err := zstd.NewReader(f)
		if err != nil {
			return Session{}, err
		}
		defer z.Close()
		r = z
	}
	switch tool {
	case Claude:
		return ReadClaude(r)
	case Codex:
		return ReadCodex(r)
	}
	return Session{}, fmt.Errorf("sessionimport: no session files are read for %q", tool)
}

// File is a session file found on disk.
type File struct {
	Tool     string
	Path     string
	Modified time.Time
}

// Discover lists the session files tool keeps under home, oldest first. A
// tool that is not installed has none. Claude Code's are the files directly
// in each project's folder; the folders below hold its sub-agents'.
func Discover(tool, home string) ([]File, error) {
	var patterns []string
	switch tool {
	case Claude:
		patterns = []string{filepath.Join(home, ".claude", "projects", "*", "*.jsonl")}
	case Codex:
		for _, dir := range []string{"sessions", "archived_sessions"} {
			root := filepath.Join(home, ".codex", dir)
			for _, depth := range []string{"", "*", filepath.Join("*", "*"), filepath.Join("*", "*", "*")} {
				for _, ext := range []string{"*.jsonl", "*.jsonl.zst"} {
					patterns = append(patterns, filepath.Join(root, depth, ext))
				}
			}
		}
	default:
		return nil, fmt.Errorf("sessionimport: no session files are read for %q", tool)
	}
	var out []File
	for _, p := range patterns {
		paths, err := filepath.Glob(p)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			info, err := os.Stat(path)
			if err != nil || info.IsDir() {
				continue
			}
			out = append(out, File{Tool: tool, Path: path, Modified: info.ModTime()})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].Modified.Equal(out[j].Modified) {
			return out[i].Modified.Before(out[j].Modified)
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}
