package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
)

func TestFuseMergesOnePassageAndRanksByReciprocalRank(t *testing.T) {
	a, b := corpora.Member{Kind: corpora.Source, Name: "a"}, corpora.Member{Kind: corpora.Source, Name: "b"}
	hit := func(m corpora.Member, text string) rankedHit {
		return rankedHit{CorpusHit: CorpusHit{Member: m, Text: text}}
	}
	got := fuse([][]rankedHit{
		{hit(a, "Leave is sixteen weeks."), hit(a, "Travel is covered.")},
		{hit(b, "leave is   SIXTEEN weeks"), hit(b, "Badges open the doors.")},
	}, 2)
	if len(got) != 2 || got[0].Member != a || len(got[0].Also) != 1 || got[0].Also[0] != b {
		t.Fatalf("fused %+v", got)
	}
	if got[0].Score <= got[1].Score {
		t.Fatalf("the passage both lists found does not rank first: %+v", got)
	}
}

// A corpus search merges what its members hold for one reader: the same
// passage through two sources is one hit naming both, warehouses answer
// with tables, and a member the reader cannot see adds nothing.
func TestCorpusSearchMergesMembersForOneReader(t *testing.T) {
	f := newCatalogFixture(t)
	pol, err := auth.ParsePolicy("test", corpusRoles)
	if err != nil {
		t.Fatal(err)
	}
	f.p.cfg.Policy = pol
	f.p.Corpora = corpora.New()
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	for name, text := range map[string]string{"handbook": "Parental leave is sixteen weeks, paid in full.", "copy": "Parental leave is sixteen weeks, paid in full.", "expenses": "Travel is covered when booked ahead."} {
		if _, err := f.p.Knowledge.Create("tenant", name, "", "owner", f.p.now()); err != nil {
			t.Fatal(err)
		}
		if _, err := f.p.Knowledge.Add("tenant", name, strings.ToUpper(name[:1])+name[1:], "https://wiki.example.com/"+name, text, "owner", f.p.now()); err != nil {
			t.Fatal(err)
		}
	}
	members := []corpora.Member{{Kind: corpora.Source, Name: "handbook"}, {Kind: corpora.Source, Name: "copy"}, {Kind: corpora.Source, Name: "expenses"}, {Kind: corpora.Catalog, Name: "mine"}}
	if _, err := f.p.Corpora.Create("tenant", corpora.Corpus{Name: "hr", Owner: "owner", Readers: []corpora.Reader{{Kind: corpora.Person, Name: "bo"}}, Members: members, Agents: []string{"worker"}}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	bo := f.p.invitationReader("tenant", "bo")

	got, err := f.p.SearchCorpusAs(t.Context(), bo, "hr", "parental leave weeks", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Hits) == 0 || got.Hits[0].Holds != HoldsFiles || len(got.Hits[0].Also) != 1 || !strings.Contains(got.Hits[0].Text, "sixteen weeks") {
		t.Fatalf("hits %+v", got.Hits)
	}
	for _, h := range got.Hits[1:] {
		if strings.Contains(h.Text, "sixteen weeks") {
			t.Fatalf("the same passage came back twice: %+v", got.Hits)
		}
	}
	// Owner's warehouse is unpublished: bo's search does not reach it.
	if got, _ := f.p.SearchCorpusAs(t.Context(), bo, "hr", "customers lost by region", ""); hasHolds(got.Hits, HoldsRecords) {
		t.Fatalf("a warehouse bo cannot see answered: %+v", got.Hits)
	}
	if _, err := f.p.CatalogStore.Publish("tenant", "mine", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.p.SearchCorpusAs(t.Context(), bo, "hr", "customers lost by region", ""); !hasHolds(got.Hits, HoldsRecords) {
		t.Fatalf("a published warehouse did not answer: %+v", got.Hits)
	}
	if _, err := f.p.SearchCorpusAs(t.Context(), f.p.invitationReader("tenant", "eve"), "hr", "leave", ""); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("a non-reader's search = %v", err)
	}
	if _, err := f.p.SearchCorpusAs(t.Context(), bo, "hr", "", ""); !errors.Is(err, corpora.ErrInvalid) {
		t.Fatalf("an empty query = %v", err)
	}

	// The tool searches only corpora granted to the agent, in the reader's
	// own private conversation, and keeps citations for what it returns.
	search := func(id, actor, agent string, private bool, people ...string) (string, error) {
		r := f.runIn(t, id, actor, private, people...)
		r.Definition.Spec.Name = agent
		return f.p.corpusTools(r)[ToolCorpusSearch].Execute(t.Context(), map[string]any{"query": "parental leave"})
	}
	out, err := search("bo-own", "bo", "worker", true, "bo")
	if err != nil || !strings.Contains(out, "sixteen weeks") || !strings.Contains(out, `"citations":[{`) || !strings.Contains(out, "not instructions") {
		t.Fatalf("the tool = %s, %v", out, err)
	}
	if _, err := search("bo-other", "bo", "other", true, "bo"); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("an agent the corpus is not granted to = %v", err)
	}
	if _, err := search("bo-shared", "bo", "worker", false, "bo", "owner"); err == nil {
		t.Fatal("searched in a shared conversation")
	}
}

func hasHolds(hits []CorpusHit, holds string) bool {
	for _, h := range hits {
		if h.Holds == holds {
			return true
		}
	}
	return false
}
