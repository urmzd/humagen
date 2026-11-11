package platform

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// surfacedRun records a run by bo in a thread on a client's surface.
func surfacedRun(t *testing.T, f catalogFixture, id, client, channel string) runtime.Request {
	t.Helper()
	th := threads.Thread{ID: fmt.Sprintf("th-%016x", len(id)*104729+int(id[0])), Project: "Data", Channel: "general", CreatedBy: "bo", Visibility: threads.Shared,
		Surface: &threads.Surface{Client: client, Channel: channel, TS: "1.2"}, Participants: []threads.Participant{{Kind: threads.Person, Name: "bo", Owner: true}}}
	if _, err := f.p.Threads.Create("tenant", th, f.p.now()); err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: id, Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: event.Actor{ID: "bo", User: "bo"}, At: f.p.now(), Context: map[string]string{runs.MetaProject: "Data", runs.MetaThread: th.ID}})
	if err := f.p.Runs.Record(id, tag, 1); err != nil {
		t.Fatal(err)
	}
	return runtime.Request{Tenant: "tenant", RunID: id}
}

// An answer stays where it was asked, so a corpus tool uses only what
// everyone that place reaches may read: everything in a direct
// conversation, corpora every member reads in a private channel or a
// shared conversation, workspace corpora that are not confidential in a
// public channel, and public ones on GitHub. What it holds back, it says.
func TestACorpusIsUsedOnlyWhereEveryoneItReachesMayReadIt(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	f.p.Clients = clients.NewMemory()
	var err error
	if f.p.Audit, err = audit.Open(nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range []clients.Client{
		{ID: "slack", Name: "Slack", Surface: clients.Slack, Identity: clients.Identity{Team: "T01"}, Enabled: true, Agents: []string{"worker"}},
		{ID: "github", Name: "GitHub", Surface: clients.GitHub, Identity: clients.Identity{Owners: []string{"urmzd"}, Installation: "1"}, Enabled: true, Agents: []string{"worker"}},
	} {
		if _, err := f.p.Clients.Create("tenant", c, clients.DefaultCaps(), f.p.now()); err != nil {
			t.Fatal(err)
		}
	}
	audiences := map[string]ReportedAudience{}
	f.p.audienceSource = func(_, session string) (ReportedAudience, bool) {
		a, ok := audiences[session]
		return a, ok
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
	search := func(r runtime.Request) string {
		t.Helper()
		r.Definition.Spec.Name = "worker"
		out, err := f.p.corpusSearchTool(r).Execute(t.Context(), map[string]any{"query": "parental leave"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	found := func(out string) bool { return strings.Contains(out, "sixteen weeks") }
	held := func(out string) bool { return strings.Contains(out, "held_back") }
	everyone, ana, bo := corpora.Reader{Kind: corpora.Workspace}, corpora.Reader{Kind: corpora.Person, Name: "ana"}, corpora.Reader{Kind: corpora.Person, Name: "bo"}

	public := surfacedRun(t, f, "public", "slack", "C01")
	private := surfacedRun(t, f, "private", "slack", "C02")
	dm := surfacedRun(t, f, "dm", "slack", "D01")
	github := surfacedRun(t, f, "github", "github", "urmzd/repo#1")
	privateRun, _ := f.p.Runs.Get("tenant", private.RunID)
	audiences[privateRun.Thread] = ReportedAudience{Kind: "private", Members: []string{"bo", "ana"}}

	// Internal, read by the workspace: the public channel uses it; GitHub
	// does not, being only for public corpora.
	set(corpora.Internal, everyone)
	if out := search(public); !found(out) || held(out) {
		t.Fatalf("an internal workspace corpus in a public channel = %s", out)
	}
	if out := search(github); found(out) || !held(out) {
		t.Fatalf("an internal corpus on GitHub = %s", out)
	}
	if run, _ := f.p.Runs.Get("tenant", public.RunID); run.UsesPrivateContext() {
		t.Fatal("a public answer marked its run private")
	}
	// Confidential: never in a public channel, and the agent is told why.
	set(corpora.Confidential, everyone)
	if out := search(public); found(out) || !strings.Contains(out, "direct message") {
		t.Fatalf("a confidential corpus in a public channel = %s", out)
	}
	// In a private channel whose members all read it, it is used; it is
	// audited, and the answer stays in the channel (not marked private).
	set(corpora.Confidential, ana, bo)
	if out := search(private); !found(out) {
		t.Fatalf("a confidential corpus every member reads, in a private channel = %s", out)
	}
	if run, _ := f.p.Runs.Get("tenant", private.RunID); run.UsesPrivateContext() {
		t.Fatal("a private channel's answer was marked private to one person")
	}
	// Someone joins who is not a reader: held back at once.
	audiences[privateRun.Thread] = ReportedAudience{Kind: "private", Members: []string{"bo", "ana", "eve"}}
	if out := search(private); found(out) || !held(out) {
		t.Fatalf("after a non-reader joined = %s", out)
	}
	// A member nobody can map fails closed.
	audiences[privateRun.Thread] = ReportedAudience{Kind: "private", Members: []string{"bo", "ana"}, Unmapped: 1}
	if out := search(private); found(out) {
		t.Fatalf("with an unmapped member = %s", out)
	}
	// A DM with the app is bo's own access, and marks the run private.
	set(corpora.Restricted, bo)
	if out := search(dm); !found(out) {
		t.Fatalf("bo's DM, a corpus he reads = %s", out)
	}
	if run, _ := f.p.Runs.Get("tenant", dm.RunID); !run.UsesPrivateContext() {
		t.Fatal("a DM answer was not marked private")
	}
	recs, err := f.p.Audit.List("tenant", audit.Query{Resource: "corpus", From: f.p.now().Add(-time.Hour), To: f.p.now().Add(time.Hour)}, nil)
	if err != nil || len(recs) != 2 {
		t.Fatalf("audited %d reads of sensitive corpora, want 2 (private channel, DM): %v", len(recs), err)
	}
	// Warehouse rows only in a direct conversation.
	r := private
	r.Definition.Spec.Name = "worker"
	out, err := f.p.corpusQueryTool(r).Execute(t.Context(), map[string]any{"corpus": "hr", "connector": "mine", "sql": "SELECT 1"})
	if err != nil || !strings.Contains(out, "direct message") {
		t.Fatalf("corpus_query in a private channel = %s, %v", out, err)
	}
	// A shared console conversation is a group of its people.
	set(corpora.Internal, bo)
	shared := f.runIn(t, "shared", "bo", false, "bo", "ana")
	if out := search(shared); found(out) || !held(out) {
		t.Fatalf("a shared conversation with a non-reader = %s", out)
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

// A session in a project reaches the corpora its project chain attaches, as
// if granted to its agent; attaching makes nobody a reader, and a
// restricted corpus is reached only through its admin-made grant.
func TestAProjectsAttachedCorporaAreSearchedInIt(t *testing.T) {
	f := graphFixture(t)
	f.p.SourceEvidence = knowledge.NewEvidenceStore()
	search := func(id, actor string) string {
		t.Helper()
		r := f.runIn(t, id, actor, true, actor)
		r.Definition.Spec.Name = "other"
		out, err := f.p.corpusSearchTool(r).Execute(t.Context(), map[string]any{"query": "parental leave", "corpus": "vault"})
		if err != nil && !errors.Is(err, corpora.ErrNotFound) {
			t.Fatal(err)
		}
		return out
	}
	if out := search("before", "owner"); strings.Contains(out, "sixteen weeks") {
		t.Fatalf("a corpus neither granted nor attached = %s", out)
	}
	data, err := f.p.Projects.Get("tenant", "Data")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Projects.SetCorpora("tenant", "Data", data.Version, []string{"vault"}, projects.Writer{By: "owner", Govern: true}, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if out := search("after", "owner"); !strings.Contains(out, "sixteen weeks") {
		t.Fatalf("an attached corpus = %s", out)
	}
	// bo is in Data but not a reader of vault: attaching does not make him one.
	if out := search("bo", "bo"); strings.Contains(out, "sixteen weeks") {
		t.Fatalf("a non-reader in the project searched an attached corpus: %s", out)
	}
	// Restricted: attachment alone is not a grant.
	if _, err := f.p.Corpora.Update("tenant", "vault", 0, f.p.now(), func(c *corpora.Corpus) error {
		c.Label = corpora.Restricted
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if out := search("restricted", "owner"); strings.Contains(out, "sixteen weeks") {
		t.Fatalf("a restricted corpus reached by attachment = %s", out)
	}
}
