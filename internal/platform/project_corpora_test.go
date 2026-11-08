package platform_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/projects"
)

func corpusNames(cs []platform.ProjectCorpus) []string {
	var out []string
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}

// A project's context is the corpora it attaches. Attaching one makes no one
// its reader: a reader is shown only the corpora they may read, a corpus
// they may not read is never one they may attach, and saving what they were
// shown keeps what they were not.
func TestAProjectsCorporaAreShownAndKeptAsTheirReadersMayReadThem(t *testing.T) {
	p := start(t, call.Limits{})
	field(t, p)
	for _, c := range []platform.CorpusCreate{
		{Name: "secret", Title: "Secret"},
		{Name: "open", Title: "Open", Readers: []corpora.Reader{{Kind: corpora.Workspace}}},
	} {
		if _, err := p.CreateCorpus(admin, c); err != nil {
			t.Fatal(err)
		}
	}
	fieldAt := func() int {
		pr, err := p.Projects.Get(tenant, "Field")
		if err != nil {
			t.Fatal(err)
		}
		return pr.Version
	}
	got, err := p.SetProjectCorpora(admin, "Field", govern, platform.CorporaInput{Version: fieldAt(), Corpora: []string{"secret", "open"}})
	if err != nil {
		t.Fatal(err)
	}
	if names := corpusNames(got.Corpora); !slices.Equal(names, []string{"secret", "open"}) || got.Corpora[1].Title != "Open" {
		t.Fatalf("Field's corpora for their owner = %+v", got.Corpora)
	}
	shown, err := p.ProjectCorporaAs(ana, "Field")
	if err != nil {
		t.Fatal(err)
	}
	if names := corpusNames(shown.Corpora); !slices.Equal(names, []string{"open"}) || len(shown.Given) != 1 || shown.Given[0] != (projects.CorpusRef{Name: "open", From: "Field"}) {
		t.Fatalf("Field's corpora for ana = %+v", shown)
	}
	if renewals, _ := p.ProjectCorporaAs(ana, "Renewals"); len(renewals.Given) != 1 || renewals.Given[0].From != "Field" {
		t.Fatalf("what a session in Renewals searches, for ana = %+v", renewals.Given)
	}
	// Saving what ana was shown, less open, keeps secret.
	if _, err := p.SetProjectCorpora(ana, "Field", govern, platform.CorporaInput{Version: fieldAt(), Corpora: []string{}}); err != nil {
		t.Fatal(err)
	}
	if pr, _ := p.Projects.Get(tenant, "Field"); !slices.Equal(pr.Context.Corpora, []string{"secret"}) {
		t.Fatalf("after ana's save Field attaches %v", pr.Context.Corpora)
	}
	if _, err := p.SetProjectCorpora(ana, "Launch", govern, platform.CorporaInput{Version: 1, Corpora: []string{"secret"}}); !errors.Is(err, projects.ErrInvalid) {
		t.Fatalf("ana attaching a corpus she may not read = %v", err)
	}
	// A corpus deleted since drops out of what is shown, and is kept.
	view, err := p.CorpusAs(admin, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.DeleteCorpus(admin, "secret", view.Version); err != nil {
		t.Fatal(err)
	}
	if after, _ := p.ProjectCorporaAs(admin, "Field"); len(after.Corpora) != 0 || len(after.Given) != 0 {
		t.Fatalf("a deleted corpus is still shown: %+v", after)
	}
}
