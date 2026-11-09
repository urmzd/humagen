package platform

import (
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// A corpus item address resolves to compact content and its source, by the
// same rule as every corpus tool.
func TestACorpusItemResolvesForWhoMayReadIt(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	docs, err := f.p.Knowledge.Documents("tenant", "handbook")
	if err != nil || len(docs) != 1 {
		t.Fatalf("docs %v, %v", docs, err)
	}
	resolve := func(id, actor, address string) (CorpusItem, error) {
		r := f.runIn(t, id, actor, true, actor)
		r.Definition.Spec.Name = "worker"
		return f.p.ResolveCorpusItem(t.Context(), r, address)
	}
	doc := "corpus:hr/handbook/" + docs[0].ID
	got, err := resolve("bo-doc", "bo", doc)
	if err != nil || got.Kind != "document" || !strings.Contains(got.Text, "sixteen weeks") || got.Link != "https://wiki.example.com/leave" || got.Owner != "ana" || got.HeldBack != "" {
		t.Fatalf("bo's document = %+v, %v", got, err)
	}
	// The graph node for it resolves to the same document.
	view, _ := f.p.CorpusGraphAs(f.p.invitationReader("tenant", "bo"), "hr", "")
	byNode, err := resolve("bo-node", "bo", "corpus:hr/node/"+node(t, view, "Leave policy").ID)
	if err != nil || byNode.Title != "Leave policy" {
		t.Fatalf("by node = %+v, %v", byNode, err)
	}
	// owner's warehouse is unpublished: bo is told it is held back; its
	// owner reads the table's description and columns, never rows.
	table := "corpus:hr/mine/acme.billing.churn_monthly"
	if got, err := resolve("bo-table", "bo", table); err != nil || got.HeldBack == "" || got.Text != "" {
		t.Fatalf("bo's table = %+v, %v", got, err)
	}
	if got, err := resolve("owner-table", "owner", table); err != nil || got.Kind != "table" || !strings.Contains(got.Text, "Customers lost each month") {
		t.Fatalf("owner's table = %+v, %v", got, err)
	}
	if _, err := resolve("eve", "eve", doc); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("a non-reader = %v", err)
	}
	if _, err := resolve("bad", "bo", "corpus:hr"); !errors.Is(err, corpora.ErrInvalid) {
		t.Fatalf("a malformed address = %v", err)
	}
}

// The on-demand context lookup resolves an address for a run by its id,
// and for a person reading for themselves.
func TestACorpusItemResolvesForARunOrAPerson(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	docs, _ := f.p.Knowledge.Documents("tenant", "handbook")
	address := "corpus:hr/handbook/" + docs[0].ID
	r := f.runIn(t, "bo-run", "bo", true, "bo")
	run, _ := f.p.Runs.Get("tenant", r.RunID)
	if run.Agent != "worker" {
		t.Fatalf("the fixture's run agent is %q", run.Agent)
	}
	if got, err := f.p.ResolveCorpusItemForRun(t.Context(), "tenant", r.RunID, address); err != nil || !strings.Contains(got.Text, "sixteen weeks") {
		t.Fatalf("for bo's run = %+v, %v", got, err)
	}
	if got, err := f.p.ResolveCorpusItemForPerson(t.Context(), "tenant", "bo", address); err != nil || !strings.Contains(got.Text, "sixteen weeks") {
		t.Fatalf("for bo = %+v, %v", got, err)
	}
	if _, err := f.p.ResolveCorpusItemForPerson(t.Context(), "tenant", "eve", address); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("for eve = %v", err)
	}
	if _, err := f.p.ResolveCorpusItemForRun(t.Context(), "tenant", "no-such-run", address); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("for a missing run = %v", err)
	}
}

// get_context's corpus resolver answers a run by its audience and a person
// by their own access, and says so when the item may not be read.
func TestGetContextPullsACorpusItem(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	docs, _ := f.p.Knowledge.Documents("tenant", "handbook")
	address := "corpus:hr/handbook/" + docs[0].ID
	r := f.runIn(t, "bo-run", "bo", true, "bo")
	for name, req := range map[string]ContextRequest{
		"a run":    {Tenant: "tenant", Subject: "bo", Origin: address, Run: r.RunID},
		"a person": {Tenant: "tenant", Subject: "bo", Origin: address},
	} {
		got, err := f.p.corpusContext(t.Context(), req)
		if err != nil || !strings.Contains(got.Text, "sixteen weeks") || len(got.Sources) != 1 || got.Sources[0].Title != "Leave policy" || got.Sources[0].URL != "https://wiki.example.com/leave" {
			t.Fatalf("for %s = %+v, %v", name, got, err)
		}
	}
	for name, req := range map[string]ContextRequest{
		"a non-reader":        {Tenant: "tenant", Subject: "eve", Origin: address},
		"a malformed address": {Tenant: "tenant", Subject: "bo", Origin: "corpus:hr"},
	} {
		got, err := f.p.corpusContext(t.Context(), req)
		if err != nil || got.Text != "" || len(got.Sources) != 0 || !strings.Contains(got.Note, "no corpus item") {
			t.Fatalf("for %s = %+v, %v", name, got, err)
		}
	}
}
