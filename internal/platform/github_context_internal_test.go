package platform

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/threads"
)

// seededComments is a fake issue acme/repo#7 with n comments by ana, oldest
// first, as GitHub lists them.
func seededComments(t *testing.T, n int) (*github.Fake, github.Ref) {
	t.Helper()
	fake := github.NewFake()
	ref := github.Ref{Owner: "acme", Repo: "repo", Number: 7}
	read := github.Context{Subject: github.ContextSubject{Number: 7, Title: "Busy", State: "open"}}
	for i := 1; i <= n; i++ {
		c := github.ContextComment{ID: int64(i), Body: fmt.Sprintf("note-%03d", i)}
		c.User.Login = "ana"
		read.Comments = append(read.Comments, c)
	}
	if err := fake.SeedContext(ref, read); err != nil {
		t.Fatal(err)
	}
	return fake, ref
}

// GitHub lists an issue's comments oldest first, 30 to a page, so the
// latest are on the last page: they are the ones read, across pages.
func TestTheLatestCommentsAreReadAcrossPages(t *testing.T) {
	for _, n := range []int{3, 30, 40, 61, 65} {
		fake, ref := seededComments(t, n)
		pages := 0
		read, partial, err := readLatest(func(page int) (github.Context, error) {
			pages++
			return fake.ReadContext(context.Background(), ref, page)
		})
		if err != nil || partial {
			t.Fatalf("%d comments: partial %v, %v", n, partial, err)
		}
		if want := (n / 30) + 1; pages != want {
			t.Errorf("%d comments took %d reads, want %d", n, pages, want)
		}
		text := placeRead(read, "acme/repo", "issues", 7, nil, partial, "0123456789abcdef01234567")
		for i := max(1, n-placeReadComments+1); i <= n; i++ {
			if !strings.Contains(text, fmt.Sprintf("note-%03d", i)) {
				t.Errorf("%d comments: note-%03d, among the latest, was not read:\n%s", n, i, text)
			}
		}
		if n > placeReadComments && strings.Contains(text, fmt.Sprintf("note-%03d", n-placeReadComments)) {
			t.Errorf("%d comments: an older comment was kept over a latest one", n)
		}
	}
}

// Past the page bound the read says the latest may be missing.
func TestTooManyCommentsSayTheLatestMayBeMissing(t *testing.T) {
	fake, ref := seededComments(t, placeReadPages*30+5)
	read, partial, err := readLatest(func(page int) (github.Context, error) { return fake.ReadContext(context.Background(), ref, page) })
	if err != nil || !partial {
		t.Fatalf("partial %v, %v", partial, err)
	}
	if text := placeRead(read, "acme/repo", "issues", 7, nil, partial, "0123456789abcdef01234567"); !strings.Contains(text, "the latest may be missing") {
		t.Fatalf("the read does not say it is partial:\n%s", text)
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
	text := placeRead(read, "acme/repo", "issues", 7, map[string]bool{"humagen-token": true}, false, "0123456789abcdef01234567")
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
