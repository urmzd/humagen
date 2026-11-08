package platform

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/orchagent/flow/event"
	"github.com/urmzd/orchagent/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// A shared conversation is shown only public and internal corpora everyone
// in it may read; confidential content stays in its reader's private
// conversation, marks the run private and is audited.
func TestWhatACorpusShowsFollowsItsLabelAndAudience(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	var err error
	if f.p.Audit, err = audit.Open(nil); err != nil {
		t.Fatal(err)
	}
	set := func(label corpora.Label, readers ...corpora.Reader) {
		t.Helper()
		if _, err := f.p.Corpora.Update("tenant", "hr", 0, f.p.now(), func(c *corpora.Corpus) error {
			c.Label, c.Readers = label, readers
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	search := func(r runtime.Request) (string, error) {
		r.Definition.Spec.Name = "worker"
		return f.p.corpusTools(r)[ToolCorpusSearch].Execute(t.Context(), map[string]any{"query": "parental leave"})
	}
	shared := f.runIn(t, "shared", "bo", false, "bo", "ana")
	own := f.runIn(t, "own", "bo", true, "bo")
	everyone := corpora.Reader{Kind: corpora.Workspace}

	// Internal, and both people read it: the shared conversation is shown
	// it, and the run is not marked private.
	set(corpora.Internal, corpora.Reader{Kind: corpora.Person, Name: "ana"}, corpora.Reader{Kind: corpora.Person, Name: "bo"})
	if out, err := search(shared); err != nil || !strings.Contains(out, "sixteen weeks") || !strings.Contains(out, `"label":"internal"`) {
		t.Fatalf("a shared search of an internal corpus = %s, %v", out, err)
	}
	if run, _ := f.p.Runs.Get("tenant", shared.RunID); run.UsesPrivateContext() {
		t.Fatal("a shared search of an internal corpus marked the run private")
	}
	// One of them cannot read it: nothing.
	set(corpora.Internal, corpora.Reader{Kind: corpora.Person, Name: "bo"})
	if _, err := search(shared); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("a corpus one person here cannot read = %v", err)
	}
	// A Slack thread's audience cannot be listed: only workspace corpora.
	slackThread := threads.Thread{ID: "th-00000000000051ac", Project: "Data", Channel: "general", CreatedBy: "bo", Visibility: threads.Shared,
		Surface: &threads.Surface{Client: "app", Channel: "C01", TS: "1.2"}, Participants: []threads.Participant{{Kind: threads.Person, Name: "bo", Owner: true}}}
	if _, err := f.p.Threads.Create("tenant", slackThread, f.p.now()); err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "slack", Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: event.Actor{ID: "bo", User: "bo"}, At: f.p.now(), Context: map[string]string{runs.MetaProject: "Data", runs.MetaThread: slackThread.ID}})
	if err := f.p.Runs.Record("slack", tag, 1); err != nil {
		t.Fatal(err)
	}
	slack := runtime.Request{Tenant: "tenant", RunID: "slack"}
	if _, err := search(slack); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("a Slack thread was shown a corpus not read by the workspace: %v", err)
	}
	set(corpora.Internal, everyone)
	if _, err := search(slack); err != nil {
		t.Fatalf("a Slack thread was refused a workspace corpus: %v", err)
	}

	// Confidential: never in a shared conversation; in bo's own, shown,
	// marked private and audited.
	set(corpora.Confidential, everyone)
	if _, err := search(shared); !errors.Is(err, corpora.ErrNotFound) {
		t.Fatalf("a confidential corpus in a shared conversation = %v", err)
	}
	if out, err := search(own); err != nil || !strings.Contains(out, "sixteen weeks") {
		t.Fatalf("bo's own search of a confidential corpus = %s, %v", out, err)
	}
	if run, _ := f.p.Runs.Get("tenant", own.RunID); !run.UsesPrivateContext() {
		t.Fatal("a private search was not marked private")
	}
	recs, err := f.p.Audit.List("tenant", audit.Query{Resource: "corpus", From: f.p.now().Add(-time.Hour), To: f.p.now().Add(time.Hour)}, nil)
	if err != nil || len(recs) != 1 || recs[0].Action != "corpus.read" || recs[0].Subject != "bo" || recs[0].Agent != "worker" {
		t.Fatalf("audit %+v, %v", recs, err)
	}
}

// Granting a restricted corpus to an agent takes a workspace admin and a
// stated purpose.
func TestGrantingARestrictedCorpusTakesAnAdminAndAPurpose(t *testing.T) {
	f := graphFixture(t)
	owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead, auth.KnowledgeWrite}}
	restricted := corpora.Restricted
	// hr is granted to worker already: restricting it is a new grant.
	if _, err := f.p.UpdateCorpus(owner, "hr", CorpusChange{Label: &restricted}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a manager restricted a granted corpus: %v", err)
	}
	admin := owner
	admin.Scopes = append(admin.Scopes, auth.ClientsWrite)
	if _, err := f.p.UpdateCorpus(admin, "hr", CorpusChange{Label: &restricted}); !errors.Is(err, corpora.ErrInvalid) {
		t.Fatalf("restricted with no purpose: %v", err)
	}
	purpose := "Answers employees' questions about their own leave"
	got, err := f.p.UpdateCorpus(admin, "hr", CorpusChange{Label: &restricted, Purpose: &purpose})
	if err != nil || got.Label != corpora.Restricted || got.Purpose != purpose {
		t.Fatalf("restricted by an admin = %+v, %v", got.Corpus, err)
	}
	// Changing its description later needs no admin.
	words := "Leave and pay"
	if _, err := f.p.UpdateCorpus(owner, "hr", CorpusChange{Description: &words}); err != nil {
		t.Fatalf("an unrelated change to a restricted corpus = %v", err)
	}
}
