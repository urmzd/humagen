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
