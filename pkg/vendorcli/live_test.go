package vendorcli

import (
	"bytes"
	"context"
	image2 "image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"
)

// collect runs one request and returns what was said, the tools called and
// the error, if any.
func collect(t *testing.T, p saigetypes.Provider, msgs ...saigetypes.Message) (said string, calls []string, results []string, usage saigetypes.UsageDelta, err error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	ch, serr := p.Stream(ctx, saigetypes.Request{Messages: msgs})
	if serr != nil {
		return "", nil, nil, usage, serr
	}
	done := false
	for d := range ch {
		switch d := d.(type) {
		case saigetypes.PartDelta:
			said += d.Text
		case saigetypes.ToolExecStartDelta:
			calls = append(calls, d.Name)
		case saigetypes.ToolExecEndDelta:
			results = append(results, d.Result+d.Error)
		case saigetypes.UsageDelta:
			usage = d
		case saigetypes.ErrorDelta:
			err = d.Error
		case saigetypes.DoneDelta:
			done = true
		}
	}
	if err == nil && !done {
		t.Fatal("the stream ended without Done or an error")
	}
	return said, calls, results, usage, err
}

func user(s string) saigetypes.Message {
	return saigetypes.UserMessage{Parts: []saigetypes.UserPart{saigetypes.TextPart{Text: s}}}
}

func assistant(s string) saigetypes.Message {
	return saigetypes.AssistantMessage{Parts: []saigetypes.AssistantPart{saigetypes.TextPart{Text: s}}}
}

// TestLive runs the real programs, signed in as they are on this host. It
// spends a little of that account, so it runs only when asked:
// VENDORCLI_LIVE=claude-code,codex.
func TestLive(t *testing.T) {
	for _, kind := range strings.Split(os.Getenv("VENDORCLI_LIVE"), ",") {
		if kind == "" {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			dir, state := t.TempDir(), t.TempDir()
			dir, _ = filepath.EvalSymlinks(dir)
			if err := os.WriteFile(filepath.Join(dir, "note.txt"), []byte("the heron lands at dusk\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			fn, err := New(kind, Config{Dir: dir, StateDir: state})
			if err != nil {
				t.Fatal(err)
			}
			model := map[string]string{ClaudeCode: "haiku", Codex: "default"}[kind]
			p, err := fn(native.Spec{Name: "coder", Model: model})
			if err != nil {
				t.Fatal(err)
			}
			first := "Read note.txt, then create a file named done.txt whose content is the word ok. Reply with the contents of note.txt only."
			said, calls, results, usage, err := collect(t, p, user(first))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("said %q calls %v results %d usage %+v", said, calls, len(results), usage)
			if !strings.Contains(said, "heron") || len(calls) == 0 || len(results) == 0 || usage.CompletionTokens == 0 || usage.ResponseID == "" {
				t.Fatalf("first turn: said %q, calls %v", said, calls)
			}
			if b, err := os.ReadFile(filepath.Join(dir, "done.txt")); err != nil || !strings.Contains(string(b), "ok") {
				t.Fatalf("done.txt = %q, %v", b, err)
			}
			session := usage.ResponseID
			// The second request carries the conversation, as the server sends
			// it. The program continues its own session.
			said2, _, _, usage2, err := collect(t, p, user(first), assistant(said), user("Which two files did you just touch? Name them only."))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("second said %q session %s", said2, usage2.ResponseID)
			if !strings.Contains(said2, "note.txt") || !strings.Contains(said2, "done.txt") {
				t.Fatalf("second turn: %q", said2)
			}
			if kind == ClaudeCode && usage2.ResponseID != session {
				t.Fatalf("session %s was not continued: %s", session, usage2.ResponseID)
			}
			// An image, sent into the session that is continuing.
			third := "Which two files did you just touch? Name them only."
			said3, _, _, _, err := collect(t, p, user(first), assistant(said), user(third), assistant(said2),
				userWith("What single colour fills this image? Answer with one word.", image("fill.png", solidPNG(64, 64, 0, 0, 255))))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("image said %q", said3)
			if !strings.Contains(strings.ToLower(said3), "blue") {
				t.Fatalf("the image was not read: %q", said3)
			}
		})
	}
}

// solidPNG is a PNG of one colour.
func solidPNG(w, h int, r, g, b byte) []byte {
	img := image2.NewRGBA(image2.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = r, g, b, 255
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

// TestLiveResume carries a conversation on from a session the real program
// already has, named with /resume. It runs only when asked, as TestLive.
func TestLiveResume(t *testing.T) {
	for _, kind := range strings.Split(os.Getenv("VENDORCLI_LIVE"), ",") {
		if kind == "" {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			dir, _ = filepath.EvalSymlinks(dir)
			model := map[string]string{ClaudeCode: "haiku", Codex: "default"}[kind]
			build := func() saigetypes.Provider {
				fn, err := New(kind, Config{Dir: dir, StateDir: t.TempDir()})
				if err != nil {
					t.Fatal(err)
				}
				p, err := fn(native.Spec{Name: "coder", Model: model})
				if err != nil {
					t.Fatal(err)
				}
				return p
			}
			// A session made elsewhere: another provider, with its own state.
			_, _, _, usage, err := collect(t, build(), user("Remember the word persimmon for later. Reply with exactly: noted"))
			if err != nil {
				t.Fatal(err)
			}
			original := usage.ResponseID
			// A new conversation names it.
			p := build()
			first := "/resume " + original + " Which word did I ask you to remember? Reply with that word only."
			said, _, _, usage2, err := collect(t, p, user(first))
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("resumed said %q, session %s from %s", said, usage2.ResponseID, original)
			if !strings.Contains(strings.ToLower(said), "persimmon") {
				t.Fatalf("the session was not carried on: %q", said)
			}
			if usage2.ResponseID == original || usage2.ResponseID == "" {
				t.Fatalf("the conversation runs in the original session %s, not a copy", original)
			}
			// The conversation then continues in the copy.
			said3, _, _, usage3, err := collect(t, p, user(first), assistant(said), user("Say it once more, in upper case only."))
			if err != nil || !strings.Contains(said3, "PERSIMMON") {
				t.Fatalf("second turn: %q, %v", said3, err)
			}
			if kind == ClaudeCode && usage3.ResponseID != usage2.ResponseID {
				t.Fatalf("the second turn left the copy: %s then %s", usage2.ResponseID, usage3.ResponseID)
			}
		})
	}
}
