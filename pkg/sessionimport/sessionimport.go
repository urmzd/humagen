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

// Turn is one thing said. Role is "user", "assistant" or "tool": a tool turn
// is one line naming a tool the assistant called and what it called it on.
type Turn struct {
	Role string
	Text string
	At   time.Time
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
	Turns []Turn
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

// add appends a turn, joining it to the last when the same role spoke again.
func (s *Session) add(role, text string, at time.Time) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if n := len(s.Turns); n > 0 && s.Turns[n-1].Role == role && role != "user" {
		sep := "\n\n"
		if role == "tool" {
			sep = "\n"
		}
		s.Turns[n-1].Text += sep + text
		return
	}
	s.Turns = append(s.Turns, Turn{Role: role, Text: text, At: at})
}

// finish titles a session and refuses one nobody asked anything in.
func (s Session) finish() (Session, error) {
	if s.ID == "" || s.Asked() == 0 {
		return Session{}, ErrNotSession
	}
	if strings.TrimSpace(s.Title) == "" {
		for _, t := range s.Turns {
			if t.Role == "user" {
				s.Title = clip(oneLine(t.Text), 80)
				break
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
	Type  string          `json:"type"`
	Text  string          `json:"text"`
	Name  string          `json:"name"`
	Input json.RawMessage `json:"input"`
}

// ReadClaude reads one Claude Code session file (~/.claude/projects/*/*.jsonl).
func ReadClaude(r io.Reader) (Session, error) {
	s := Session{Tool: Claude}
	err := lines(r, func(line []byte) {
		var rec struct {
			Type        string `json:"type"`
			SessionID   string `json:"sessionId"`
			Cwd         string `json:"cwd"`
			Timestamp   string `json:"timestamp"`
			IsSidechain bool   `json:"isSidechain"`
			IsMeta      bool   `json:"isMeta"`
			AITitle     string `json:"aiTitle"`
			Message     struct {
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
		switch rec.Type {
		case "ai-title":
			s.Title = rec.AITitle
		case "user", "assistant":
			if rec.IsSidechain || rec.IsMeta {
				return
			}
			var text string
			var blocks []block
			if json.Unmarshal(rec.Message.Content, &text) != nil {
				_ = json.Unmarshal(rec.Message.Content, &blocks)
			} else {
				blocks = []block{{Type: "text", Text: text}}
			}
			for _, b := range blocks {
				switch {
				case b.Type == "text" && rec.Type == "user":
					s.add("user", claudeText(b.Text), at)
				case b.Type == "text":
					s.add("assistant", b.Text, at)
				case b.Type == "image" && rec.Type == "user":
					s.add("user", "[image]", at)
				case b.Type == "tool_use":
					s.add("tool", called(b.Name, b.Input), at)
				}
			}
		}
	})
	if err != nil {
		return Session{}, err
	}
	return s.finish()
}

// codexPreamble starts a user message Codex wrote itself: the instructions
// and the environment it gives the model before what the person typed.
var codexPreamble = []string{"# AGENTS.md instructions", "<environment_context", "<user_instructions", "<turn_aborted", "<permissions instructions", "<INSTRUCTIONS>", "<collaboration_mode", "<subagent_notification", "<task-notification>"}

// ReadCodex reads one Codex session file (~/.codex/sessions/**/rollout-*.jsonl),
// already decompressed.
func ReadCodex(r io.Reader) (Session, error) {
	s := Session{Tool: Codex}
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
			Cwd            string          `json:"cwd"`
			Timestamp      string          `json:"timestamp"`
			ParentThreadID string          `json:"parent_thread_id"`
			Source         json.RawMessage `json:"source"`
			Role           string          `json:"role"`
			Name           string          `json:"name"`
			Arguments      json.RawMessage `json:"arguments"`
			Input          json.RawMessage `json:"input"`
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
				// A sub-agent's source is an object naming what started it.
				s.Child = p.ParentThreadID != "" || bytes.HasPrefix(bytes.TrimSpace(p.Source), []byte("{"))
			}
		case rec.Type == "event_msg", rec.Type == "turn_context", rec.Type == "world_state":
			// Events repeat the items; the rest is the harness's own state.
		case p.Type == "message" && (p.Role == "user" || p.Role == "assistant"):
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
						text = "[image]"
					}
				}
				s.add(p.Role, text, at)
			}
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
			s.add("tool", called(name, args), at)
		}
	})
	if err != nil {
		return Session{}, err
	}
	return s.finish()
}

// ReadExport reads a conversations.json from a ChatGPT or a claude.ai data
// export, calling fn with each conversation that has something asked in it.
// It reads one conversation at a time, so a large export is not held whole.
func ReadExport(r io.Reader, fn func(Session) error) error {
	dec := json.NewDecoder(r)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('[') {
		return fmt.Errorf("%w: an export is a JSON list of conversations", ErrNotSession)
	}
	for dec.More() {
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return err
		}
		s, err := exported(raw)
		if errors.Is(err, ErrNotSession) {
			continue
		}
		if err != nil {
			return err
		}
		if err := fn(s); err != nil {
			return err
		}
	}
	return nil
}

func unix(f float64) time.Time {
	if f <= 0 {
		return time.Time{}
	}
	return time.Unix(0, int64(f*float64(time.Second))).UTC()
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
			Parent  string `json:"parent"`
			Message *struct {
				Author struct {
					Role string `json:"role"`
				} `json:"author"`
				CreateTime float64 `json:"create_time"`
				Content    struct {
					Parts []json.RawMessage `json:"parts"`
				} `json:"content"`
				Metadata struct {
					Hidden bool `json:"is_visually_hidden_from_conversation"`
				} `json:"metadata"`
			} `json:"message"`
		} `json:"mapping"`
		// claude.ai.
		UUID      string `json:"uuid"`
		Name      string `json:"name"`
		CreatedAt string `json:"created_at"`
		Messages  []struct {
			Sender    string `json:"sender"`
			Text      string `json:"text"`
			CreatedAt string `json:"created_at"`
		} `json:"chat_messages"`
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return Session{}, ErrNotSession
	}
	if c.Mapping == nil {
		s := Session{Tool: ClaudeAI, ID: c.UUID, Title: c.Name, Started: parseTime(c.CreatedAt)}
		for _, m := range c.Messages {
			role := "assistant"
			if m.Sender == "human" {
				role = "user"
			}
			s.add(role, m.Text, parseTime(m.CreatedAt))
		}
		return s.finish()
	}
	s := Session{Tool: ChatGPT, ID: c.ConversationID, Title: c.Title, Started: unix(c.CreateTime)}
	if s.ID == "" {
		s.ID = c.ID
	}
	// The conversation shown is the path from the current node to the root;
	// other branches are edits the person left behind.
	var path []string
	for id, hops := c.CurrentNode, 0; id != "" && hops <= len(c.Mapping); hops++ {
		node, ok := c.Mapping[id]
		if !ok {
			break
		}
		path = append(path, id)
		id = node.Parent
	}
	for i := len(path) - 1; i >= 0; i-- {
		m := c.Mapping[path[i]].Message
		if m == nil || m.Metadata.Hidden || (m.Author.Role != "user" && m.Author.Role != "assistant") {
			continue
		}
		var parts []string
		for _, p := range m.Content.Parts {
			var text string
			if json.Unmarshal(p, &text) == nil {
				parts = append(parts, text)
			} else {
				parts = append(parts, "[attachment]")
			}
		}
		s.add(m.Author.Role, strings.Join(parts, "\n"), unix(m.CreateTime))
	}
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
