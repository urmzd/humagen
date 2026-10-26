package inbox

import (
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func draftPart() Part {
	return Part{Type: PartDraft, Draft: &DraftPart{Project: "Field", Agent: "humagen-analyst", Run: "r1", Subject: "Field shipped", Text: "Two things."}}
}

// A draft is kept as it was raised, through a restart and a rebuild, and a
// reader's copy of it is their own.
func TestADraftSurvivesARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		m := raise(s, "draft:r1", t0, draftPart())
		if len(m.Parts) != 1 || m.Parts[0].State != PartOpen || *m.Parts[0].Draft != *draftPart().Draft {
			t.Fatalf("raised as %+v", m.Parts)
		}
		m.Parts[0].Draft.Subject = "changed by a reader"
		if got, _ := s.Get(acme, m.ID, "ada"); got.Parts[0].Draft.Subject != "Field shipped" {
			t.Fatalf("a reader's copy changed the store: %+v", got.Parts[0].Draft)
		}
		if _, _, err := s.Resolve(acme, m.ID, "p1", Outcome{Action: "discard", By: "ada", At: t0.Add(time.Second)}); err != nil {
			t.Fatal(err)
		}

		again, err := OpenStore(st)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := again.Snapshot(acme)
		want, _ := s.Snapshot(acme)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("after restart = %+v, want %+v", got, want)
		}
		rebuilt, _ := Rebuild(s.Changes(acme)).Snapshot(acme)
		if !reflect.DeepEqual(rebuilt, want) {
			t.Fatalf("rebuilt = %+v, want %+v", rebuilt, want)
		}
		if p := got[0].Parts[0]; p.Draft == nil || p.Draft.Text != "Two things." || p.State != PartResolved || p.Outcome.Action != "discard" {
			t.Fatalf("the draft after restart = %+v", p)
		}
	})
}
