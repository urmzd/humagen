// Package vendorcli runs a turn on another assistant's own command-line
// agent: Claude Code (claude) or Codex (codex), signed in the way that
// program is signed in on this host, a subscription included.
//
// It is a model provider. The program is the model and the tools at once:
// one request starts it in a directory, it reads, edits and runs what it
// decides to there, and what it says streams back as the answer, with each
// tool it used as the turn's tool activity.
//
// The program runs on this host, as the server's user, outside the run's
// sandbox, with whatever that user can reach. That is the point of it, and
// why a deployment gets it only when its configuration names a directory
// for it (Config.Dir). It is for a person's own machine, not a shared
// server.
package vendorcli

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/sessionimport"
	"github.com/urmzd/humagen/pkg/skillfiles"
)

// The kinds of program.
const (
	ClaudeCode = "claude-code"
	Codex      = "codex"
)

// ErrConfig is a configuration no program can be run from.
var ErrConfig = errors.New("vendorcli: invalid configuration")

// Config is one provider: a program and where it works.
type Config struct {
	// Dir is the directory the program works in. Required, absolute, and
	// it must exist: it is what the program may read and change.
	Dir string `json:"dir"`
	// Command is the program. Empty is "claude" or "codex" on PATH.
	Command string `json:"command,omitempty"`
	// Permissions is how much the program may do without asking, in its own
	// words. Nobody is there to ask, so what it would ask for it does not
	// do. For claude-code a permission mode: "default", "acceptEdits",
	// "plan" or "bypassPermissions". For codex a sandbox: "read-only",
	// "workspace-write" or "danger-full-access". Empty is "acceptEdits" and
	// "workspace-write": the program may change files in Dir.
	Permissions string `json:"permissions,omitempty"`
	// Allow are tools claude-code may use without asking, in its own
	// permission rule syntax, such as "Bash(go test:*)" or "Bash(git:*)".
	// With "acceptEdits" it edits files and runs nothing; these are the
	// commands it may run as well. Codex takes none: its sandbox decides.
	Allow []string `json:"allow,omitempty"`
	// APIKey lets the program use an API key in the server's environment
	// (ANTHROPIC_API_KEY, OPENAI_API_KEY and the like). By default those
	// are withheld from it, so it signs in as it is signed in on this host:
	// a key in the environment otherwise takes the place of a subscription.
	APIKey bool `json:"api_key,omitempty"`
	// Args are further arguments given to the program on every run.
	Args []string `json:"args,omitempty"`
	// StateDir is where the provider remembers which of the program's
	// sessions continues which conversation. Empty is a directory under the
	// user's cache directory.
	StateDir string `json:"state_dir,omitempty"`
	// Timeout ends a run that takes longer. Zero is two hours.
	Timeout Duration `json:"timeout,omitempty"`
}

// Duration is a time.Duration written as a string such as "30m".
type Duration time.Duration

// UnmarshalJSON reads "30m".
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("a duration is a string such as \"30m\"")
	}
	v, err := time.ParseDuration(s)
	if err != nil || v <= 0 {
		return fmt.Errorf("%q is not a positive duration", s)
	}
	*d = Duration(v)
	return nil
}

// MarshalJSON writes "30m0s".
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

var permissions = map[string][]string{
	ClaudeCode: {"acceptEdits", "default", "plan", "bypassPermissions"},
	Codex:      {"workspace-write", "read-only", "danger-full-access"},
}

// keyEnv are the environment variables that hand a program an API key.
var keyEnv = map[string][]string{
	ClaudeCode: {"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"},
	Codex:      {"OPENAI_API_KEY", "CODEX_API_KEY"},
}

// check fills in the defaults and refuses what cannot run.
func (c Config) check(kind string) (Config, error) {
	allowed, ok := permissions[kind]
	if !ok {
		return c, fmt.Errorf("%w: no program of kind %q", ErrConfig, kind)
	}
	if !filepath.IsAbs(c.Dir) {
		return c, fmt.Errorf("%w: %s needs dir, the absolute directory the program works in", ErrConfig, kind)
	}
	if info, err := os.Stat(c.Dir); err != nil || !info.IsDir() {
		return c, fmt.Errorf("%w: %s: dir %q is not a directory on this host", ErrConfig, kind, c.Dir)
	}
	if c.Permissions == "" {
		c.Permissions = allowed[0]
	}
	if !slices.Contains(allowed, c.Permissions) {
		return c, fmt.Errorf("%w: %s: permissions is one of %s", ErrConfig, kind, strings.Join(allowed, ", "))
	}
	if len(c.Allow) > 0 && kind != ClaudeCode {
		return c, fmt.Errorf("%w: %s takes no allow; its permissions decide what it runs", ErrConfig, kind)
	}
	for _, rule := range c.Allow {
		if strings.TrimSpace(rule) == "" || strings.HasPrefix(rule, "-") {
			return c, fmt.Errorf("%w: %s: allow holds permission rules such as \"Bash(go test:*)\"", ErrConfig, kind)
		}
	}
	if c.Command == "" {
		c.Command = map[string]string{ClaudeCode: "claude", Codex: "codex"}[kind]
	}
	if c.StateDir == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return c, fmt.Errorf("%w: %s: no cache directory for state_dir: %v", ErrConfig, kind, err)
		}
		c.StateDir = filepath.Join(cache, "humagen", "vendorcli")
	}
	if c.Timeout == 0 {
		c.Timeout = Duration(2 * time.Hour)
	}
	return c, nil
}

// New builds the provider factory for kind: what a models slot of that kind
// runs on. The model an agent names is passed to the program as its model;
// "default" leaves the program's own choice.
func New(kind string, c Config) (native.ProviderFunc, error) {
	c, err := c.check(kind)
	if err != nil {
		return nil, err
	}
	return func(s native.Spec) (saigetypes.Provider, error) {
		return &provider{kind: kind, cfg: c, model: s.Model, effort: s.Effort}, nil
	}, nil
}

// Factory reads a spec block as a Config and builds kind's provider
// factory: what humagen.WithKind registers in the models slot.
func Factory(kind string) func(ctx context.Context, block json.RawMessage) (any, error) {
	return func(_ context.Context, block json.RawMessage) (any, error) {
		var c Config
		if len(block) > 0 && string(block) != "null" {
			dec := json.NewDecoder(bytes.NewReader(block))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&c); err != nil {
				return nil, fmt.Errorf("%w: %v", ErrConfig, err)
			}
		}
		fn, err := New(kind, c)
		if err != nil {
			return nil, err
		}
		return fn, nil
	}
}

type provider struct {
	kind  string
	cfg   Config
	model string
	// effort is the reasoning effort the agent asks for, in the program's
	// own words ("low", "high"). Empty leaves the program's default.
	effort string
}

// Name implements saigetypes.NamedProvider.
func (p *provider) Name() string { return p.kind }

// ReadsHostFiles implements skillfiles.HostReader: the program reads files
// on this host with its own tools and is offered none of the platform's, so
// a turn's pinned skill files are placed on the host for it.
func (p *provider) ReadsHostFiles() bool { return true }

// Capabilities implements saigetypes.CapabilityReporter. It says only what
// the program can be shown: a turn's attachments are sent as they are when
// their type is here, and as derived text otherwise. Known is false, so
// nothing else about the model is taken as declared.
func (p *provider) Capabilities() saigetypes.ModelCapabilities {
	types := map[saigetypes.MediaType]bool{}
	for t := range imageExt {
		types[saigetypes.MediaType(t)] = true
	}
	if p.kind == ClaudeCode {
		types["application/pdf"] = true
	}
	return saigetypes.ModelCapabilities{Provider: p.kind, Model: p.model, Media: saigetypes.ContentSupport{NativeTypes: types}}
}

// turnOf is a request as the program needs it: the instructions, what was
// said before, and what is asked now.
type turnOf struct {
	system string
	before []said
	ask    string
	// media are the images and documents sent with what is asked now, and
	// earlier are those sent with earlier messages, which a session that
	// is starting has not seen.
	media, earlier []media
	// fork is a session of the program's own that this conversation is to
	// carry on from: one a person names with /resume as a conversation's
	// first message. The program copies it, so the original is not changed.
	fork string
	// forkAsk is what that session is asked.
	forkAsk string
}

// media is one image or document a person sent.
type media struct {
	kind saigetypes.PartKind
	typ  string
	name string
	data []byte
}

// imageExt is the file extension of each image type a program is shown. A
// type is never read into a file name: one that is not here is refused.
var imageExt = map[string]string{"image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp"}

// ErrMedia is something sent that the program cannot be shown.
var ErrMedia = errors.New("vendorcli: unsupported content")

// mediaOf reads the images and documents among a user message's parts. A
// part the program has no way to take is an error that names it, never
// something left out without a word.
func (p *provider) mediaOf(parts []saigetypes.UserPart) ([]media, error) {
	var out []media
	for _, part := range parts {
		var src saigetypes.Source
		switch v := part.(type) {
		case saigetypes.TextPart:
			continue
		case saigetypes.ImagePart:
			if _, ok := imageExt[string(v.Source.MediaType)]; !ok {
				return nil, fmt.Errorf("%w: %s is shown PNG, JPEG, GIF and WebP images, not %s", ErrMedia, p.kind, v.Source.MediaType)
			}
			src = v.Source
		case saigetypes.DocumentPart:
			if p.kind != ClaudeCode {
				return nil, fmt.Errorf("%w: %s reads no %s document; send its text", ErrMedia, p.kind, v.Source.MediaType)
			}
			if v.Source.MediaType != "application/pdf" {
				return nil, fmt.Errorf("%w: %s reads PDF documents, not %s; send its text", ErrMedia, p.kind, v.Source.MediaType)
			}
			src = v.Source
		default:
			return nil, fmt.Errorf("%w: %s takes text, images and (Claude Code) PDF documents, not %s", ErrMedia, p.kind, part.Kind())
		}
		if len(src.Inline) == 0 {
			why := src.Unresolved
			if why == "" {
				why = "its bytes were not given"
			}
			return nil, fmt.Errorf("%w: %s %q: %s", ErrMedia, part.Kind(), src.Filename, why)
		}
		out = append(out, media{kind: part.Kind(), typ: string(src.MediaType), name: src.Filename, data: src.Inline})
	}
	return out, nil
}

type said struct{ role, text string }

// text is the text of a message's parts, in order.
func text[P any](parts []P) string {
	var out []string
	for _, part := range parts {
		if t, ok := any(part).(saigetypes.TextPart); ok && strings.TrimSpace(t.Text) != "" {
			out = append(out, t.Text)
		}
	}
	return strings.Join(out, "\n\n")
}

func (p *provider) split(msgs []saigetypes.Message) (turnOf, error) {
	var t turnOf
	var all []said
	for _, m := range msgs {
		switch m := m.(type) {
		case saigetypes.SystemMessage:
			if s := text(m.Parts); s != "" {
				t.system = strings.TrimSpace(t.system + "\n\n" + s)
			}
		case saigetypes.UserMessage:
			sent, err := p.mediaOf(m.Parts)
			if err != nil {
				return t, err
			}
			s := text(m.Parts)
			if s == "" && len(sent) > 0 {
				s = "See what is attached."
			}
			if s != "" {
				all = append(all, said{"user", s})
				// What came with an earlier message is kept until a later
				// one is asked.
				t.earlier = append(t.earlier, t.media...)
				t.media = sent
			}
		case saigetypes.AssistantMessage:
			if s := text(m.Parts); s != "" {
				all = append(all, said{"assistant", s})
			}
		}
	}
	if len(all) == 0 || all[len(all)-1].role != "user" {
		return t, errors.New("vendorcli: the request ends with nothing asked")
	}
	t.before, t.ask = all[:len(all)-1], all[len(all)-1].text
	return t, nil
}

// key names a conversation by what was said in it, so the program's session
// that produced those turns can be found again without the server telling
// the provider which conversation a request belongs to.
func (p *provider) key(turns []said) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00", p.kind, p.cfg.Dir)
	for _, s := range turns {
		// Spacing is not kept the same by everything a turn passes through.
		fmt.Fprintf(h, "%s\x00%s\x00", s.role, strings.Join(strings.Fields(s.text), " "))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// threadLine is the line of a turn's trigger context that names its thread.
// The platform writes its conversations to the model as one message: the
// trigger's context, the conversation so far, then what is asked. So a
// provider is not told which conversation a request is in, and reads it
// here. The first turn of a conversation started as a run has no such line:
// its thread is named after that run, which the request does not say.
var threadLine = regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(runs.MetaThread) + `: (\S+)$`)

// conversation is the platform thread a request is in, or "" when what is
// asked does not say. What a person typed is repeated in the context and may
// run over several paragraphs, so the line is looked for in all of it. A
// person can type a line of that shape too: when what is asked names more
// than one thread, none is taken, and the turn starts from what was said
// rather than continue a session that may be another conversation's.
func conversation(ask string) string {
	if !strings.HasPrefix(ask, "Context from the trigger:") {
		return ""
	}
	thread := ""
	for _, m := range threadLine.FindAllStringSubmatch(ask, -1) {
		if thread != "" && m[1] != thread {
			return ""
		}
		thread = m[1]
	}
	return thread
}

// A session is continued only by a turn whose conversation so far begins
// with everything the session holds. Two messages that follow the same turn
// are two branches of a conversation, and each must see its own turns and
// never the other's, so the thread a turn names is not enough to choose a
// session: the session's history has to be the start of the turn's.
//
// The provider is not told which turn a request follows in a way it can
// place its own turn by, so it reads the history the platform sends: the
// conversation so far, one line a turn. A note keeps, for one session, a
// digest of each line it was sent, and of what it was then asked and what it
// answered. A later turn continues the session when its conversation so far
// holds those lines in that order, then what was asked, then the answer.
// Anything else starts a session, which is told what was said so far in its
// own branch.

// note is what one session of the program holds, as far as the platform's
// history says it.
type note struct {
	Kind    string `json:"kind"`
	Dir     string `json:"dir"`
	Session string `json:"session"`
	// Ask and Answer are the start of a first turn's question and answer,
	// as notes were written before sessions kept their history. A note
	// with these and no Asked is one of those.
	Ask    string `json:"ask,omitempty"`
	Answer string `json:"answer,omitempty"`
	// Lines are the digests of the conversation so far the session was last
	// sent, oldest first, and Omitted says the platform had left earlier
	// turns out of it.
	Lines   []string `json:"lines,omitempty"`
	Omitted bool     `json:"omitted,omitempty"`
	// Asked and Answered are the digests of what the session was then
	// asked and what it answered, spacing made single as a line has it.
	Asked    string `json:"asked,omitempty"`
	Answered string `json:"answered,omitempty"`
	// Key is the state file a first turn's session is also under, for a
	// caller that keeps the messages itself (held). Whichever way the
	// session is continued, the other way to it is removed.
	Key string    `json:"key,omitempty"`
	At  time.Time `json:"at"`
}

// thread is the state file of one platform conversation: a session for each
// of its branches the program has answered.
type thread struct {
	Sessions []note `json:"sessions"`
}

// maxLines bounds the lines a note keeps, the newest, and maxSessions the
// sessions kept for one conversation, the newest.
const (
	maxLines    = 4096
	maxSessions = 64
)

// history is the conversation so far as a request states it.
type history struct {
	// known is false when the request does not say its history in a way
	// that can be believed: no session is continued for it.
	known bool
	// lines are the turns before what is asked, one a line, oldest first,
	// and omitted says earlier ones were left out.
	lines   []string
	omitted bool
	// typed is what is asked now.
	typed string
}

// What the platform puts around the conversation so far (threads.Ask).
const (
	historyOpen  = "Conversation so far, oldest first"
	historyClose = "End of the conversation so far."
)

var omittedLine = regexp.MustCompile(`^\(\d+ earlier turns omitted\)$`)

// historyOf reads the conversation so far out of what the platform asks. A
// first turn has none. Each earlier turn is one line, so what a person typed
// in one cannot begin or end the history; what they type now can hold lines
// that read like the platform's, and it is repeated in the context, so a
// request with more than one beginning or end is not believed.
func historyOf(ask string) history {
	lines := strings.Split(ask, "\n")
	open, end, opens, ends := -1, -1, 0, 0
	for i, l := range lines {
		if strings.HasPrefix(l, historyOpen) {
			open, opens = i, opens+1
		}
		if strings.HasPrefix(l, historyClose) {
			end, ends = i, ends+1
		}
	}
	if opens == 0 && ends == 0 {
		return history{known: true, typed: typedAlone(ask)}
	}
	if opens != 1 || ends != 1 || end < open || end+1 >= len(lines) || lines[end+1] != "" {
		return history{}
	}
	h := history{known: true, lines: lines[open+1 : end], typed: strings.Join(lines[end+2:], "\n")}
	if len(h.lines) > 0 && omittedLine.MatchString(h.lines[0]) {
		h.lines, h.omitted = h.lines[1:], true
	}
	return h
}

// typedAlone is what a person typed in a request with no conversation so
// far: what follows the context, which repeats it.
func typedAlone(ask string) string {
	if !strings.HasPrefix(ask, "Context from the trigger:") {
		return ask
	}
	for i := 0; ; {
		j := strings.Index(ask[i:], "\n\n")
		if j < 0 {
			return lastParagraph(ask)
		}
		i += j + 2
		if strings.Contains(ask[:i], "\n- "+runs.MetaAsk+": "+ask[i:]+"\n") {
			return ask[i:]
		}
	}
}

// digest names a text without keeping it. Spacing is made single first, as
// a line of the conversation so far has it.
func digest(s string) string {
	sum := sha256.Sum256([]byte(strings.Join(strings.Fields(s), " ")))
	return hex.EncodeToString(sum[:16])
}

// spoken is what a line of the conversation so far says, after who said it.
func spoken(line string) string {
	_, said, _ := strings.Cut(line, ": ")
	return said
}

// holds reports whether the conversation so far begins with everything the
// session of n holds: the lines it was sent, then what it was asked, then,
// after any lines of others, what it answered. Where the platform left
// earlier turns out, of this turn's history or of what the session was
// sent, the start of the conversation cannot be compared: two branches can
// end alike and differ in what was left out. Such a session is not
// continued.
func (n note) holds(h history) bool {
	if n.Asked == "" {
		// A first turn noted before sessions kept their history: only a
		// conversation that is that turn and nothing more continues it.
		return n.Ask != "" && n.Answer != "" && !h.omitted && len(h.lines) == 2 &&
			strings.Contains(spoken(h.lines[0]), n.Ask) && strings.Contains(spoken(h.lines[1]), n.Answer)
	}
	if h.omitted || n.Omitted {
		return false
	}
	for at, line := range h.lines {
		if digest(spoken(line)) != n.Asked {
			continue
		}
		answered := false
		for _, later := range h.lines[at+1:] {
			if digest(spoken(later)) == n.Answered {
				answered = true
				break
			}
		}
		if !answered {
			continue
		}
		mine, theirs, same := len(n.Lines), at, true
		for mine > 0 && theirs > 0 && same {
			mine, theirs = mine-1, theirs-1
			same = digest(h.lines[theirs]) == n.Lines[mine]
		}
		if same && mine == 0 && theirs == 0 {
			return true
		}
	}
	return false
}

// noteOf is the note of a session that was sent h and answered.
func (p *provider) noteOf(session string, h history, answer string) note {
	n := note{Kind: p.kind, Dir: p.cfg.Dir, Session: session, Omitted: h.omitted, Asked: digest(h.typed), Answered: digest(answer), At: time.Now()}
	lines := h.lines
	if len(lines) > maxLines {
		lines, n.Omitted = lines[len(lines)-maxLines:], true
	}
	for _, l := range lines {
		n.Lines = append(n.Lines, digest(l))
	}
	return n
}

// begunFor is how long a first turn waits for its second.
const begunFor = 7 * 24 * time.Hour

// lastParagraph is what a person typed, after the context the platform
// puts before it.
func lastParagraph(ask string) string {
	if !strings.HasPrefix(ask, "Context from the trigger:") {
		return ask
	}
	if i := strings.LastIndex(ask, "\n\n"); i >= 0 {
		return ask[i+2:]
	}
	return ask
}

// named is the key of the program's sessions for a platform conversation.
func (p *provider) named(conv string) string {
	sum := sha256.Sum256([]byte(p.kind + "\x00" + p.cfg.Dir + "\x00conversation\x00" + conv))
	return hex.EncodeToString(sum[:])
}

// stateMu holds the session state within this process, and locked holds it
// against the other processes that share the state directory too (a server
// and its workers): a session is taken by one turn only, whichever process
// runs it. When the state cannot be held, locked says so, and nothing is
// continued or noted.
var stateMu sync.Mutex

func (p *provider) locked() (unlock func(), ok bool) {
	stateMu.Lock()
	release, err := lockState(p.cfg.StateDir)
	if err != nil {
		stateMu.Unlock()
		return nil, false
	}
	return func() { release(); stateMu.Unlock() }, true
}

// writeState replaces a state file whole, so no reader sees part of one.
func writeState(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(f.Name(), path)
	}
	if err != nil {
		_ = os.Remove(f.Name())
	}
	return err
}

// gone removes a state file, and reports whether it is no longer there.
func gone(path string) bool {
	err := os.Remove(path)
	return err == nil || errors.Is(err, fs.ErrNotExist)
}

// begin notes a first turn, which names no thread: its second turn has one
// but no session under it yet, and finds this note by what it holds.
func (p *provider) begin(n note) {
	if n.Session == "" {
		return
	}
	b, _ := json.Marshal(n)
	unlock, ok := p.locked()
	if !ok {
		return
	}
	defer unlock()
	_ = writeState(filepath.Join(p.cfg.StateDir, "begun", n.Session), b)
}

// readThread is a conversation's state file. One written before sessions
// kept their history is a session's id alone: it says nothing of what the
// session holds, so it reads as no session at all.
func (p *provider) readThread(conv string) thread {
	var t thread
	if b, err := os.ReadFile(filepath.Join(p.cfg.StateDir, p.named(conv))); err == nil {
		_ = json.Unmarshal(b, &t)
	}
	return t
}

func (p *provider) writeThread(conv string, t thread) error {
	if len(t.Sessions) > maxSessions {
		t.Sessions = t.Sessions[len(t.Sessions)-maxSessions:]
	}
	b, err := json.Marshal(t)
	if err != nil {
		return err
	}
	return writeState(filepath.Join(p.cfg.StateDir, p.named(conv)), append(b, '\n'))
}

// take finds the one session whose history the turn's conversation so far
// begins with, among those of its thread and the first turns that name
// none, and takes it out of the state: a session is continued by one turn
// at a time, and is noted again, a turn longer, only when that turn
// answers. A turn that fails may have left part of itself in the session,
// which no later conversation so far will say, so the session is left
// behind. When more than one session fits, none is taken, and the turn
// starts one from what was said rather than guess. The same holds when the
// state cannot be held against other processes, or the session cannot be
// taken out of it: another turn could then take it too.
func (p *provider) take(conv string, h history) string {
	if !h.known || len(h.lines) == 0 {
		return ""
	}
	unlock, ok := p.locked()
	if !ok {
		return ""
	}
	defer unlock()
	t := p.readThread(conv)
	found, at, file, key := "", -1, "", ""
	for i, n := range t.Sessions {
		if n.Kind != p.kind || n.Dir != p.cfg.Dir || n.Session == "" || !n.holds(h) {
			continue
		}
		if found != "" {
			return ""
		}
		found, at = n.Session, i
	}
	dir := filepath.Join(p.cfg.StateDir, "begun")
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		var n note
		if err != nil || json.Unmarshal(b, &n) != nil {
			continue
		}
		if time.Since(n.At) > begunFor {
			_ = os.Remove(path)
			continue
		}
		if n.Kind != p.kind || n.Dir != p.cfg.Dir || n.Session == "" || !n.holds(h) {
			continue
		}
		if found != "" {
			return ""
		}
		found, file, key = n.Session, path, filepath.Base(n.Key)
	}
	switch {
	case file != "":
		if !gone(file) || (key != "" && key != "." && !gone(filepath.Join(p.cfg.StateDir, key))) {
			return ""
		}
	case at >= 0:
		t.Sessions = slices.Delete(t.Sessions, at, at+1)
		if p.writeThread(conv, t) != nil {
			return ""
		}
	}
	return found
}

// keep notes a session under its thread once its turn has answered.
func (p *provider) keep(conv string, n note) {
	if n.Session == "" {
		return
	}
	unlock, ok := p.locked()
	if !ok {
		return
	}
	defer unlock()
	t := p.readThread(conv)
	t.Sessions = slices.DeleteFunc(t.Sessions, func(o note) bool { return o.Session == n.Session })
	t.Sessions = append(t.Sessions, n)
	_ = p.writeThread(conv, t)
}

// held is a session's state file for a caller that keeps the messages
// itself: the session is named by everything said in it (key).
type held struct {
	Session string `json:"session"`
}

// session takes the session named by key out of the state, so that only the
// turn that follows everything it holds continues it: a later request that
// repeats an earlier part of the conversation and says something else is a
// branch, and starts its own. A file that is an id alone was written when a
// session stayed under every key it had passed, so it may hold more than
// its key says, and is not continued.
func (p *provider) session(key string) string {
	unlock, ok := p.locked()
	if !ok {
		return ""
	}
	defer unlock()
	path := filepath.Join(p.cfg.StateDir, key)
	b, err := os.ReadFile(path)
	if err != nil || !gone(path) {
		return ""
	}
	var h held
	_ = json.Unmarshal(b, &h)
	if h.Session != "" && !gone(filepath.Join(p.cfg.StateDir, "begun", filepath.Base(h.Session))) {
		return ""
	}
	return h.Session
}

func (p *provider) remember(key, session string) {
	if session == "" {
		return
	}
	b, _ := json.Marshal(held{Session: session})
	unlock, ok := p.locked()
	if !ok {
		return
	}
	defer unlock()
	_ = writeState(filepath.Join(p.cfg.StateDir, key), append(b, '\n'))
}

// resumeLine is a conversation's first message that asks to carry on from a
// session of the program's own: "/resume ID", and optionally what to ask it.
var resumeLine = regexp.MustCompile(`^/resume[ \t]+([0-9A-Za-z][0-9A-Za-z-]{7,63})(?:\s+([\s\S]+))?$`)

// resumeAsk is what a session is asked when a person names it and says
// nothing more.
const resumeAsk = "Carry on from where this session left off. First say, in two sentences, where it stands."

// resumed reads a first message as a request to carry on from a session. It
// returns the session and what to ask it, or "" when the message is not one.
func resumed(ask string) (session, rest string) {
	m := resumeLine.FindStringSubmatch(strings.TrimSpace(lastParagraph(ask)))
	if m == nil {
		return "", ""
	}
	if rest = strings.TrimSpace(m[2]); rest == "" {
		rest = resumeAsk
	}
	return m[1], rest
}

// ErrSession is a session a conversation may not carry on from.
var ErrSession = errors.New("vendorcli: session")

// mayFork refuses a session that began outside the provider's directory. A
// copy of a Codex session works where the original did, so one from another
// directory would take the agent out of the directory the configuration
// gave it. Claude Code finds a session only from the directory it began in,
// so it needs no check here.
func (p *provider) mayFork(session string) error {
	if session == "" || p.kind != Codex {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSession, err)
	}
	var found string
	for _, root := range []string{"sessions", "archived_sessions"} {
		for _, depth := range []string{"", "*", filepath.Join("*", "*"), filepath.Join("*", "*", "*")} {
			paths, _ := filepath.Glob(filepath.Join(home, ".codex", root, depth, "*"+session+".jsonl*"))
			if len(paths) > 0 {
				found = paths[0]
			}
		}
	}
	if found == "" {
		return fmt.Errorf("%w: codex has no session %s on this host", ErrSession, session)
	}
	s, err := sessionimport.Open(sessionimport.Codex, found)
	if err != nil && !errors.Is(err, sessionimport.ErrNotSession) {
		return fmt.Errorf("%w: %s: %v", ErrSession, session, err)
	}
	began, dir := realPath(s.Dir), realPath(p.cfg.Dir)
	if s.Dir == "" || (began != dir && !strings.HasPrefix(began, dir+string(filepath.Separator))) {
		return fmt.Errorf("%w: session %s began in %q, outside this agent's directory %q", ErrSession, session, s.Dir, p.cfg.Dir)
	}
	return nil
}

// realPath is path with its links followed, as far along it as exists: a
// session's directory may be gone, and what is left of it still says where
// it was.
func realPath(path string) string {
	path = filepath.Clean(path)
	rest := ""
	for at := path; ; {
		if real, err := filepath.EvalSymlinks(at); err == nil {
			return filepath.Join(real, rest)
		}
		parent := filepath.Dir(at)
		if parent == at {
			return path
		}
		rest, at = filepath.Join(filepath.Base(at), rest), parent
	}
}

// prompt is what the program is asked: the new message alone when it
// continues its own session, and otherwise the conversation so far with it.
func prompt(t turnOf, resumed bool) string {
	if t.fork != "" {
		return t.forkAsk
	}
	// A command of the program's own, such as /compact, is one only when it
	// is all the program is given. It acts on the session, so it is passed
	// alone only to a session that is being continued.
	if typed := strings.TrimSpace(lastParagraph(t.ask)); resumed && strings.HasPrefix(typed, "/") && !strings.ContainsAny(typed, "\n") {
		return typed
	}
	if resumed || len(t.before) == 0 {
		return t.ask
	}
	var b strings.Builder
	b.WriteString("This conversation was already under way. What was said so far:\n\n")
	for _, s := range t.before {
		who := "The person"
		if s.role == "assistant" {
			who = "You"
		}
		fmt.Fprintf(&b, "%s said:\n%s\n\n", who, s.text)
	}
	b.WriteString("Now the person says:\n")
	b.WriteString(t.ask)
	return b.String()
}

// granted are the directories the platform placed this turn's skill files
// in (skillfiles.Granted), which Claude Code is let read.
// command is one run of the program: its arguments, what it reads on its
// standard input, and what to remove when it is done. Claude Code is sent
// images and documents as a message on its input. Codex is given images as
// files, kept only for the run.
func (p *provider) command(t turnOf, session string, granted []string) (args []string, stdin []byte, done func(), err error) {
	done = func() {}
	model := p.model
	if model == "default" {
		model = ""
	}
	// A session that is starting is shown what earlier messages carried too.
	sent := t.media
	if session == "" {
		sent = append(slices.Clone(t.earlier), t.media...)
	}
	switch p.kind {
	case ClaudeCode:
		args = []string{"-p"}
		if len(sent) == 0 {
			args = append(args, prompt(t, session != ""))
		} else {
			content := []map[string]any{{"type": "text", "text": prompt(t, session != "")}}
			for _, m := range sent {
				block := "image"
				if m.kind == saigetypes.KindDocument {
					block = "document"
				}
				content = append(content, map[string]any{"type": block, "source": map[string]any{"type": "base64", "media_type": m.typ, "data": base64.StdEncoding.EncodeToString(m.data)}})
			}
			line, err := json.Marshal(map[string]any{"type": "user", "message": map[string]any{"role": "user", "content": content}})
			if err != nil {
				return nil, nil, done, err
			}
			stdin = append(line, '\n')
			args = append(args, "--input-format", "stream-json")
		}
		args = append(args, "--output-format", "stream-json", "--verbose", "--permission-mode", p.cfg.Permissions)
		// The supporting files of the skills this turn is pinned to are in
		// a directory of the turn's own, outside Dir, which Claude Code reads
		// only when it is named. Codex reads outside its workspace in every
		// sandbox, so it is given nothing. The directories are the ones the
		// platform granted this attempt, never ones the text names.
		for _, dir := range granted {
			args = append(args, "--add-dir", dir)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if p.effort != "" {
			args = append(args, "--effort", p.effort)
		}
		if t.system != "" {
			args = append(args, "--append-system-prompt", t.system)
		}
		if session != "" {
			args = append(args, "--resume", session)
		}
		if t.fork != "" {
			// A copy carries on, so the session a person named stays as it was.
			args = append(args, "--resume", t.fork, "--fork-session")
		}
		if len(p.cfg.Allow) > 0 {
			args = append(args, "--allowedTools", strings.Join(p.cfg.Allow, ","))
		}
		args = append(args, p.cfg.Args...)
	case Codex:
		args = []string{"exec"}
		if session != "" {
			args = append(args, "resume", session)
		}
		if t.fork != "" {
			// A copy keeps the directory it began in, which mayFork has
			// checked, and is given this provider's sandbox, not its own.
			args = append(args, "fork", t.fork, "-c", "sandbox_mode="+strconv.Quote(p.cfg.Permissions))
		}
		args = append(args, "--json", "--skip-git-repo-check")
		if session == "" && t.fork == "" {
			// A resumed session, and a copy of one, keeps the sandbox and
			// directory it began with.
			args = append(args, "--sandbox", p.cfg.Permissions, "--cd", p.cfg.Dir)
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		if p.effort != "" {
			args = append(args, "-c", "model_reasoning_effort="+strconv.Quote(p.effort))
		}
		if len(sent) > 0 {
			dir, err := os.MkdirTemp("", "humagen-vendorcli-")
			if err != nil {
				return nil, nil, done, err
			}
			done = func() { _ = os.RemoveAll(dir) }
			for i, m := range sent {
				path := filepath.Join(dir, fmt.Sprintf("image-%d.%s", i+1, imageExt[m.typ]))
				if err := os.WriteFile(path, m.data, 0o600); err != nil {
					return nil, nil, done, err
				}
				args = append(args, "--image", path)
			}
		}
		args = append(args, p.cfg.Args...)
		ask := prompt(t, session != "")
		if t.system != "" && session == "" {
			ask = "Instructions for this conversation:\n" + t.system + "\n\n" + ask
		}
		// What follows -- is the prompt, however many images came before it.
		args = append(args, "--", ask)
	}
	return args, stdin, done, nil
}

func (p *provider) env() []string {
	if p.cfg.APIKey {
		return os.Environ()
	}
	var out []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !slices.Contains(keyEnv[p.kind], name) {
			out = append(out, kv)
		}
	}
	return out
}

// Stream implements saigetypes.Provider: one run of the program.
func (p *provider) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	t, err := p.split(req.Messages)
	if err != nil {
		return nil, err
	}
	// A turn the platform runs names its thread in what it asks. Anything
	// else that calls the provider is known by what was said so far.
	conv := conversation(t.ask)
	// A session is continued only when everything it holds is the start of
	// this turn's conversation so far, so a branch never continues the
	// session of another branch.
	var h history
	session := ""
	switch {
	case conv != "":
		h = historyOf(t.ask)
		session = p.take(conv, h)
	case len(t.before) > 0:
		session = p.session(p.key(t.before))
	default:
		h = historyOf(t.ask)
	}
	// A conversation that has no session yet may name one to carry on from.
	if session == "" && len(t.before) == 0 {
		t.fork, t.forkAsk = resumed(t.ask)
		if err := p.mayFork(t.fork); err != nil {
			return nil, err
		}
	}
	args, stdin, cleanup, err := p.command(t, session, skillfiles.Granted(ctx))
	if err != nil {
		cleanup()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(p.cfg.Timeout))
	cmd := exec.CommandContext(ctx, p.cfg.Command, args...)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Dir = p.cfg.Dir
	cmd.Env = p.env()
	// The program starts its own children. Ending the run ends them all.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM) }
	cmd.WaitDelay = 10 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		cleanup()
		return nil, err
	}
	stderr := &tail{max: 4 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		cancel()
		cleanup()
		return nil, fmt.Errorf("vendorcli: start %s: %w; is it installed and on PATH?", p.cfg.Command, err)
	}
	out := make(chan saigetypes.Delta, 64)
	go func() {
		defer cancel()
		// The files given to the program are gone before the stream ends.
		defer close(out)
		defer cleanup()
		w := &writer{ctx: ctx, out: out, calls: map[string]called{}}
		var res result
		if p.kind == ClaudeCode {
			res = readClaude(stdout, w)
		} else {
			res = readCodex(stdout, w)
		}
		_, _ = io.Copy(io.Discard, stdout)
		waitErr := cmd.Wait()
		switch {
		case ctx.Err() != nil:
			w.last(saigetypes.ErrorDelta{Error: fmt.Errorf("vendorcli: %s was stopped: %w", p.cfg.Command, context.Cause(ctx))})
			return
		case res.err != "":
			w.last(saigetypes.ErrorDelta{Error: fmt.Errorf("vendorcli: %s: %s", p.cfg.Command, res.err)})
			return
		case !res.done:
			w.last(saigetypes.ErrorDelta{Error: fmt.Errorf("vendorcli: %s ended without an answer (%v): %s", p.cfg.Command, waitErr, stderr.String())})
			return
		}
		// The session now holds this turn too: its next request finds it by
		// what has been said up to and including this answer.
		switch {
		case conv != "":
			// A turn whose history is not known leaves no note: nothing
			// could tell which later turn follows it.
			if h.known && res.answer != "" {
				p.keep(conv, p.noteOf(res.session, h, res.answer))
			}
		case res.answer != "":
			key := p.key(append(slices.Clone(t.before), said{"user", t.ask}, said{"assistant", res.answer}))
			p.remember(key, res.session)
			if len(t.before) == 0 && h.known && len(h.lines) == 0 {
				n := p.noteOf(res.session, h, res.answer)
				n.Key = key
				p.begin(n)
			}
		}
		res.usage.ResponseModel = res.model
		res.usage.ResponseID = res.session
		w.last(res.usage)
		w.last(saigetypes.DoneDelta{})
	}()
	return out, nil
}

// tail keeps the end of what is written to it.
type tail struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (t *tail) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, b...)
	if len(t.buf) > t.max {
		t.buf = t.buf[len(t.buf)-t.max:]
	}
	return len(b), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return strings.TrimSpace(string(t.buf))
}

// result is how a run ended.
type result struct {
	done    bool
	err     string
	session string
	model   string
	// answer is every text the program said, as the conversation will hold
	// it.
	answer string
	usage  saigetypes.UsageDelta
}

// writer sends the parts of one answer, numbering them.
type writer struct {
	ctx   context.Context
	out   chan<- saigetypes.Delta
	next  int
	said  []string
	calls map[string]called
}

func (w *writer) send(d saigetypes.Delta) {
	select {
	case w.out <- d:
	case <-w.ctx.Done():
	}
}

// last sends how the run ended. A turn that was stopped still reads it, so
// it does not give way to the stop as send does, only to a reader that has
// gone.
func (w *writer) last(d saigetypes.Delta) {
	select {
	case w.out <- d:
	case <-time.After(5 * time.Second):
	}
}

func (w *writer) text(s string) {
	if strings.TrimSpace(s) == "" {
		return
	}
	// Each thing said is a paragraph of the one answer.
	if len(w.said) > 0 {
		s = "\n\n" + s
	}
	w.said = append(w.said, s)
	i := w.next
	w.next++
	w.send(saigetypes.PartStart{Index: i, Kind: saigetypes.KindText})
	w.send(saigetypes.PartDelta{Index: i, Text: s})
	w.send(saigetypes.PartEnd{Index: i})
}

func (w *writer) answer() string { return strings.Join(w.said, "") }

// maxResult bounds how much of one tool's output is kept in the answer.
const maxResult = 16 << 10

// call and result publish a tool the program ran as tool activity of the
// turn: a call when it starts and its outcome when it ends. They are not
// parts of the answer. A part would be a call the server is asked to run,
// and the program has already run it.
func (w *writer) call(id, name string, input map[string]any) {
	w.calls[id] = called{name: name, what: summary(input)}
	w.send(saigetypes.ToolExecStartDelta{ToolCallID: id, Name: name})
}

func (w *writer) result(id, output string, failed bool) {
	c, ok := w.calls[id]
	if !ok {
		return
	}
	delete(w.calls, id)
	if len(output) > maxResult {
		output = "[… the start of this output is left out …]\n" + strings.ToValidUTF8(output[len(output)-maxResult:], "")
	}
	// The outcome says what the tool was called on, since the call was
	// published before the program said.
	if c.what != "" {
		output = c.what + "\n" + output
	}
	end := saigetypes.ToolExecEndDelta{ToolCallID: id, Name: c.name}
	if failed {
		end.Error = output
	} else {
		end.Result = output
	}
	w.send(end)
}

type called struct{ name, what string }

// summary is one line saying what a tool was called on.
func summary(input map[string]any) string {
	for _, k := range []string{"command", "file_path", "path", "url", "query", "pattern", "description", "prompt", "changes"} {
		switch v := input[k].(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				return "$ " + strings.Join(strings.Fields(v), " ")
			}
		case nil:
		default:
			if b, err := json.Marshal(v); err == nil {
				return "$ " + string(b)
			}
		}
	}
	return ""
}

// records calls fn with each JSON line of r. A line that is not JSON, such
// as a notice the program prints, is skipped.
func records(r io.Reader, fn func(line []byte)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) > 0 && line[0] == '{' && json.Valid(line) {
			fn(line)
		}
	}
}

// content is a tool result's content: a string, or blocks of text.
func content(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	_ = json.Unmarshal(raw, &blocks)
	var out []string
	for _, b := range blocks {
		if b.Type == "text" {
			out = append(out, b.Text)
		} else if b.Type != "" {
			out = append(out, "["+b.Type+"]")
		}
	}
	return strings.Join(out, "\n")
}

// readClaude reads `claude -p --output-format stream-json --verbose`.
func readClaude(r io.Reader, w *writer) result {
	var res result
	compacted := ""
	records(r, func(line []byte) {
		var rec struct {
			Compact struct {
				Trigger string `json:"trigger"`
				Before  int    `json:"pre_tokens"`
				After   int    `json:"post_tokens"`
			} `json:"compact_metadata"`
			Type      string `json:"type"`
			Subtype   string `json:"subtype"`
			SessionID string `json:"session_id"`
			Model     string `json:"model"`
			IsError   bool   `json:"is_error"`
			Result    string `json:"result"`
			Parent    string `json:"parent_tool_use_id"`
			Message   struct {
				Content []struct {
					Type      string          `json:"type"`
					Text      string          `json:"text"`
					ID        string          `json:"id"`
					Name      string          `json:"name"`
					Input     map[string]any  `json:"input"`
					ToolUseID string          `json:"tool_use_id"`
					Content   json.RawMessage `json:"content"`
					IsError   bool            `json:"is_error"`
				} `json:"content"`
			} `json:"message"`
			Usage struct {
				Input      int `json:"input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
				Output     int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		if rec.SessionID != "" {
			res.session = rec.SessionID
		}
		switch rec.Type {
		case "system":
			if rec.Subtype == "init" && rec.Model != "" {
				res.model = rec.Model
			}
			// Only a compaction that was asked for is the turn's answer. One
			// the program makes on its own, in a long session, comes before
			// the answer to what was asked, which is read as usual.
			if rec.Subtype == "compact_boundary" && rec.Compact.Trigger == "manual" {
				compacted = fmt.Sprintf("Compacted the session: %d tokens before, %d after.", rec.Compact.Before, rec.Compact.After)
			}
		case "assistant", "user":
			// What a sub-agent says is its caller's tool result, not the
			// answer. And after a compaction that was asked for, the program
			// replays what it kept of the session, said in an earlier turn.
			if rec.Parent != "" || compacted != "" {
				return
			}
			for _, b := range rec.Message.Content {
				switch {
				case b.Type == "text" && rec.Type == "assistant":
					w.text(b.Text)
				case b.Type == "tool_use":
					w.call(b.ID, b.Name, b.Input)
				case b.Type == "tool_result":
					w.result(b.ToolUseID, content(b.Content), b.IsError)
				}
			}
		case "result":
			res.done = true
			if rec.IsError || (rec.Subtype != "" && rec.Subtype != "success") {
				res.err = strings.TrimSpace(rec.Subtype + ": " + rec.Result)
			}
			// A command of the program's own answers in its result, or not
			// at all, rather than as something the model said.
			if res.err == "" && len(w.said) == 0 {
				// After a compaction the result can still hold the answer
				// before it, so what the compaction reported comes first.
				w.text(cmp.Or(compacted, strings.TrimSpace(rec.Result)))
			}
			u := rec.Usage
			res.usage = saigetypes.UsageDelta{
				PromptTokens: u.Input + u.CacheRead + u.CacheWrite, CachedPromptTokens: u.CacheRead, CacheWriteTokens: u.CacheWrite,
				CompletionTokens: u.Output, TotalTokens: u.Input + u.CacheRead + u.CacheWrite + u.Output,
			}
		}
	})
	res.answer = w.answer()
	return res
}

// readCodex reads `codex exec --json`.
func readCodex(r io.Reader, w *writer) result {
	var res result
	var notice string
	records(r, func(line []byte) {
		var rec struct {
			Type     string `json:"type"`
			ThreadID string `json:"thread_id"`
			Message  string `json:"message"`
			Error    struct {
				Message string `json:"message"`
			} `json:"error"`
			Item struct {
				ID       string          `json:"id"`
				Type     string          `json:"type"`
				Text     string          `json:"text"`
				Command  string          `json:"command"`
				Output   string          `json:"aggregated_output"`
				ExitCode *int            `json:"exit_code"`
				Status   string          `json:"status"`
				Server   string          `json:"server"`
				Tool     string          `json:"tool"`
				Query    string          `json:"query"`
				Changes  json.RawMessage `json:"changes"`
				Message  string          `json:"message"`
			} `json:"item"`
			Usage struct {
				Input  int `json:"input_tokens"`
				Cached int `json:"cached_input_tokens"`
				Output int `json:"output_tokens"`
			} `json:"usage"`
		}
		if json.Unmarshal(line, &rec) != nil {
			return
		}
		it := rec.Item
		// Each item gets an id that is its own across the session's turns.
		id := res.session + "/" + it.ID
		switch rec.Type {
		case "thread.started":
			res.session = rec.ThreadID
		case "item.started":
			switch it.Type {
			case "command_execution":
				w.call(id, "shell", map[string]any{"command": it.Command})
			case "mcp_tool_call":
				w.call(id, it.Server+"."+it.Tool, nil)
			case "web_search":
				w.call(id, "web_search", map[string]any{"query": it.Query})
			}
		case "item.completed":
			switch it.Type {
			case "agent_message":
				w.text(it.Text)
			case "command_execution":
				w.result(id, it.Output, it.ExitCode != nil && *it.ExitCode != 0)
			case "mcp_tool_call", "web_search":
				w.result(id, it.Status, it.Status == "failed")
			case "file_change":
				var changes any
				_ = json.Unmarshal(it.Changes, &changes)
				w.call(id, "file_change", map[string]any{"changes": changes})
				w.result(id, it.Status, it.Status == "failed")
			case "error":
				// A notice, not the turn's end: the program says here that
				// it dropped some of its skills' descriptions, or that it is
				// reconnecting, and then goes on to answer.
				notice = it.Message
			}
		case "turn.completed":
			res.done, res.err = true, ""
			u := rec.Usage
			res.usage = saigetypes.UsageDelta{PromptTokens: u.Input, CachedPromptTokens: u.Cached, CompletionTokens: u.Output, TotalTokens: u.Input + u.Output}
		case "turn.failed":
			// A failed turn is an error whatever it says, or does not say,
			// about why.
			res.done, res.err = true, rec.Error.Message
			if res.err == "" {
				res.err = "the turn failed"
				if notice != "" {
					res.err += ": " + notice
				}
			}
		case "error":
			notice = rec.Message
		}
	})
	// Only a turn that failed, or one that never ended, is an error. What
	// the program last complained of says why it never ended.
	if !res.done && res.err == "" {
		res.err = notice
	}
	res.answer = w.answer()
	return res
}
