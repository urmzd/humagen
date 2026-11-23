package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/threads"
)

// commentsStub is a CommentsAPI over n comments by ana, oldest first, that
// counts its calls.
type commentsStub struct {
	n     int
	calls int
	err   error
}

func (s *commentsStub) LatestComments(_ context.Context, _ github.Ref, want int, _ func(github.ContextComment) bool) ([]github.ContextComment, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	return notes(max(1, s.n-want-25), s.n), nil
}

// notes are comments from..to by ana, each saying note-NNN.
func notes(from, to int) []github.ContextComment {
	var out []github.ContextComment
	for i := from; i <= to; i++ {
		c := github.ContextComment{ID: int64(i), Body: fmt.Sprintf("note-%03d", i)}
		c.User.Login = "ana"
		out = append(out, c)
	}
	return out
}

func quietPlatform() *Platform { return &Platform{log: slog.New(slog.NewTextHandler(io.Discard, nil))} }

// Page 1 holds every comment when it is not full: nothing more is read. A
// full page 1 is the oldest, so the latest are read through CommentsAPI,
// once, and those are the ones given.
func TestTheLatestCommentsAreReadThroughCommentsAPI(t *testing.T) {
	p := quietPlatform()
	ref := github.Ref{Owner: "acme", Repo: "repo", Number: 7}
	person := isPerson(nil)
	stub := &commentsStub{n: 300}
	if got, partial := p.latestComments(context.Background(), stub, ref, notes(1, 12), person); len(got) != 12 || partial || stub.calls != 0 {
		t.Fatalf("a short page 1 gave %d comments, partial %v, %d reads", len(got), partial, stub.calls)
	}
	got, partial := p.latestComments(context.Background(), stub, ref, notes(1, 30), person)
	if partial || stub.calls != 1 || got[len(got)-1].ID != 300 {
		t.Fatalf("a full page 1 gave up to %d, partial %v, %d reads", got[len(got)-1].ID, partial, stub.calls)
	}
	text := placeRead(github.Context{Subject: github.ContextSubject{Number: 7, Title: "Busy", State: "open"}, Comments: got}, "acme/repo", "issues", 7, person, partial, "0123456789abcdef01234567")
	for i := 296; i <= 300; i++ {
		if !strings.Contains(text, fmt.Sprintf("note-%03d", i)) {
			t.Errorf("note-%03d, among the latest, was not read", i)
		}
	}
	if strings.Contains(text, "note-295") || strings.Contains(text, "may be missing") {
		t.Errorf("exactly 300 comments read as partial or kept an older one:\n%s", text)
	}
}

// Without CommentsAPI, or when it fails, a full page 1 says the latest may
// be missing.
func TestUnreadLatestCommentsSaySo(t *testing.T) {
	p := quietPlatform()
	ref := github.Ref{Owner: "acme", Repo: "repo", Number: 7}
	for name, reader := range map[string]any{"no CommentsAPI": nil, "a failed read": &commentsStub{err: errors.New("status 502")}} {
		got, partial := p.latestComments(context.Background(), reader, ref, notes(1, 30), isPerson(nil))
		if !partial || len(got) != 30 {
			t.Fatalf("%s: partial %v, %d comments", name, partial, len(got))
		}
		if text := placeRead(github.Context{Comments: got}, "acme/repo", "issues", 7, isPerson(nil), partial, "0123456789abcdef01234567"); !strings.Contains(text, "the latest may be missing") {
			t.Fatalf("%s: the read does not say it is partial:\n%s", name, text)
		}
	}
}

// A comment by a login a client of the workspace answers as is Humagen's
// own answer, not a person's, even without "[bot]".
func TestTheClientsOwnCommentsAreNotAPersons(t *testing.T) {
	read := github.Context{Subject: github.ContextSubject{Number: 7, Title: "T", State: "open"}}
	for _, who := range []string{"ana", "Humagen-Token", "ci[bot]"} {
		c := github.ContextComment{Body: "said by " + who}
		c.User.Login = who
		read.Comments = append(read.Comments, c)
	}
	text := placeRead(read, "acme/repo", "issues", 7, isPerson(map[string]bool{"humagen-token": true}), false, "0123456789abcdef01234567")
	if !strings.Contains(text, "said by ana") || strings.Contains(text, "said by Humagen-Token") || strings.Contains(text, "said by ci[bot]") {
		t.Fatalf("comments read:\n%s", text)
	}
}

// A run reads a place through its own client only when the session's root is
// that client's thread on that very issue or pull request.
func TestAPlaceIsTheRaisingClientsOwnThreadOnIt(t *testing.T) {
	sf := &threads.Surface{Client: "acme-gh", Channel: "acme/repo", TS: "pull/7"}
	if !surfaceIsPlace(sf, "acme-gh", "Acme", "Repo", "pull", 7) {
		t.Fatal("the client's own thread on the place is not its place")
	}
	for name, c := range map[string]struct {
		sf     *threads.Surface
		client string
		kind   string
		number int
	}{
		"no surface":         {nil, "acme-gh", "pull", 7},
		"another client":     {sf, "labs-gh", "pull", 7},
		"no client":          {&threads.Surface{Channel: "acme/repo", TS: "pull/7"}, "", "pull", 7},
		"another number":     {sf, "acme-gh", "pull", 8},
		"an issue, not pull": {sf, "acme-gh", "issues", 7},
		"a Slack thread":     {&threads.Surface{Client: "acme-gh", Channel: "C01", TS: "1700000000.000100"}, "acme-gh", "pull", 7},
	} {
		if surfaceIsPlace(c.sf, c.client, "acme", "repo", c.kind, c.number) {
			t.Errorf("%s counts as the run's own place", name)
		}
	}
}
