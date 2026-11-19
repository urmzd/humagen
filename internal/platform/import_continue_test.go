package platform_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/continuation"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Every conversation imported here is generated for the tests. None is a
// real session.

// ruleRecorder is a model that answers every turn, and records the rule
// the platform passed it through the run claim (pkg/continuation).
type ruleRecorder struct {
	mu    sync.Mutex
	rules []*continuation.Rule
	model *agenttest.ScriptedProvider
}

func (r *ruleRecorder) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	rule, ok := continuation.Of(ctx)
	r.mu.Lock()
	if ok {
		r.rules = append(r.rules, &rule)
	} else {
		r.rules = append(r.rules, nil)
	}
	r.mu.Unlock()
	return r.model.Stream(ctx, req)
}

func (r *ruleRecorder) seen() []*continuation.Rule {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]*continuation.Rule(nil), r.rules...)
}

// continuing is a platform with the agent coder on a model that records its
// rules, the projects of field, and a source of imported sessions attached
// to Field, which opal works in.
func continuing(t *testing.T) (*platform.Platform, *ruleRecorder) {
	t.Helper()
	answers := make([][]saigetypes.Delta, 32)
	for i := range answers {
		answers[i] = agenttest.TextResponse("generated reply")
	}
	rec := &ruleRecorder{model: &agenttest.ScriptedProvider{Responses: answers}}
	p := startWith(t, platform.Config{Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{
		"recorder": func(native.Spec) (saigetypes.Provider, error) { return rec, nil },
	}}})
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "coder", Provider: "recorder", Model: "r1", SystemPrompt: "Answer."}, "test"); err != nil {
		t.Fatal(err)
	}
	field(t, p)
	workIn(t, p, "Field", operator)
	if _, err := p.Knowledge.Create(tenant, "sessions-in-field", "Conversations imported from an export, filed in the project Field.", "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AttachSource(admin, "sessions-in-field", "Field", ""); err != nil {
		t.Fatal(err)
	}
	// Vault holds a source of its own, and opal is not in it.
	if _, err := p.CreateProject(t.Context(), tenant, ada, platform.ProjectInput{Name: "Vault"}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Projects.Update(tenant, "Vault", 1, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = []projects.Entry{{Who: "ada", Role: projects.Owner}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Create(tenant, "vault-sessions", "", "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Configure(tenant, "vault-sessions", vaultConfig, time.Now()); err != nil {
		t.Fatal(err)
	}
	return p, rec
}

var vaultConfig = knowledge.SourceConfig{Mode: knowledge.Indexed, Provider: "manual", Owner: "ada", Project: "Vault"}

// vaulted imports a session into the source scoped to Vault.
func vaulted(t *testing.T, p *platform.Platform, session knowledge.Imported) (knowledge.Document, platform.ContinueImportInput) {
	t.Helper()
	src, err := p.Knowledge.Get(tenant, "vault-sessions")
	if err != nil {
		t.Fatal(err)
	}
	cfg := src.Lifecycle.SourceConfig
	doc, err := p.ImportDocument(admin, "vault-sessions", "A generated chat", session.Tool+":"+session.ID, "# A generated chat", time.Now(), &cfg, session)
	if err != nil {
		t.Fatal(err)
	}
	return doc, platform.ContinueImportInput{Source: "vault-sessions", Document: doc.ID, DocumentDigest: doc.Digest, StructureDigest: knowledge.StructureDigest(session), Agents: []string{"coder"}}
}

// leave takes someone out of a project, as its owner would.
func leave(t *testing.T, p *platform.Platform, project, subject string) {
	t.Helper()
	pr, ok := p.Projects.Tree(tenant).Get(project)
	if !ok {
		t.Fatalf("no project %s", project)
	}
	if _, err := p.Projects.Update(tenant, project, pr.Version, ada, time.Now(), func(pr *projects.Project) error {
		pr.Members = slices.DeleteFunc(slices.Clone(pr.Members), func(e projects.Entry) bool { return e.Who == subject })
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func generatedSession(tool, id string) knowledge.Imported {
	at := time.Date(2026, 10, 7, 8, 0, 0, 0, time.UTC)
	step := func(n int) *time.Time { t := at.Add(time.Duration(n) * time.Minute); return &t }
	return knowledge.Imported{Tool: tool, ID: id, StartedAt: step(0), EndedAt: step(4), Branches: 2,
		Turns: []knowledge.ImportedTurn{
			{Speaker: knowledge.SpeakerUser, At: step(0), Text: "generated question: which port does the staging proxy use?"},
			{Speaker: knowledge.SpeakerTool, At: step(1), Text: "Bash: grep port proxy.conf"},
			{Speaker: knowledge.SpeakerResult, At: step(2), Text: "port 8443"},
			{Speaker: knowledge.SpeakerAssistant, At: step(3), Text: "The staging proxy listens on port 8443."},
		},
		Omitted: knowledge.ImportedOmitted{Turns: 1, Attachments: 1}}
}

// imported writes a session's document through the import route's platform
// call, as the importer ada, and the pins a person reading it would hold.
func imported(t *testing.T, p *platform.Platform, session knowledge.Imported) (knowledge.Document, platform.ContinueImportInput) {
	t.Helper()
	doc, err := p.ImportDocument(admin, "sessions-in-field", "A generated "+session.Tool+" chat", session.Tool+":"+session.ID, "# A generated chat\n\ngenerated text", time.Now(), nil, session)
	if err != nil {
		t.Fatal(err)
	}
	return doc, platform.ContinueImportInput{Source: "sessions-in-field", Document: doc.ID, DocumentDigest: doc.Digest, StructureDigest: knowledge.StructureDigest(session), Agents: []string{"coder"}}
}

// A continuation of each of the four formats is a private plain thread of
// the person who continued it, in the source's project, holding the newest
// turns of the main path with the revision it pinned. The agent's first
// turn is sent them, labelled and fenced, with nothing left out, and is
// told through the run claim to start a new session; its next turn may
// continue only the thread's own.
func TestAContinuationOfEachFormatSeedsTheAgentsFirstTurn(t *testing.T) {
	p, rec := continuing(t)
	for i, tool := range []string{knowledge.ToolClaude, knowledge.ToolCodex, knowledge.ToolChatGPT, knowledge.ToolClaudeAI} {
		session := generatedSession(tool, "generated-"+tool)
		session.Child = tool == knowledge.ToolClaude
		doc, in := imported(t, p, session)
		th, err := p.ContinueImport(t.Context(), operator, in)
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		im := th.Import
		if th.Visibility != threads.Private || th.Project != "Field" || th.CreatedBy != operator.Subject || th.Session != nil || im == nil ||
			im.Structure != threads.StructureKept || im.Tool != tool || im.Child != session.Child || im.DocumentDigest != doc.Digest || im.StructureDigest != in.StructureDigest ||
			im.ImportedBy != admin.Subject || im.ImportedAt == nil || im.By != operator.Subject || *im.Branches != 2 || *im.ImportCut != (threads.ImportCut{Turns: 1, Attachments: 1}) ||
			len(im.Context) != 4 || im.Omitted != (threads.ImportOmitted{}) || !im.StartedAt.Equal(*session.StartedAt) || th.Title != doc.Title {
			t.Fatalf("%s: continuation %+v, import %+v", tool, th, im)
		}
		posted := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder which port, again?"})
		if len(posted.Runs) != 1 {
			t.Fatalf("%s: posted %+v", tool, posted)
		}
		r := await(t, p, posted.Runs[0].ID, runs.Completed)
		sent := wire.TextOf(r.Tag())
		person := "the person there"
		if session.Child {
			person = "the session that started it"
		}
		for _, want := range []string{
			"A line marked imported is from a conversation in another tool",
			"imported from " + tool + ", " + person + ": (imported, not said here) generated question: which port does the staging proxy use?",
			"imported from " + tool + ", what a tool there gave back: (imported, not said here) port 8443",
			"imported from " + tool + ", the assistant there: (imported, not said here) The staging proxy listens on port 8443.",
			"End of the conversation so far.",
		} {
			if !strings.Contains(sent, want) {
				t.Fatalf("%s: the first turn was not sent %q:\n%s", tool, want, sent)
			}
		}
		if strings.Contains(sent, "earlier turns omitted") {
			t.Fatalf("%s: the first turn left part of the seed out:\n%s", tool, sent)
		}
		seen := rec.seen()
		if got := seen[len(seen)-1]; got == nil || got.Thread != th.ID || !got.First {
			t.Fatalf("%s: turn %d was told %+v, not to start a new session", tool, i, got)
		}
	}
	// A later turn of a continuation may continue only that thread's own
	// session, and a plain thread's turn is told nothing.
	_, in := imported(t, p, generatedSession(knowledge.ToolChatGPT, "generated-later"))
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"@coder first", "@coder second"} {
		posted := post(t, p, operator, th.ID, platform.MessageInput{Text: text})
		await(t, p, posted.Runs[0].ID, runs.Completed)
	}
	seen := rec.seen()
	if a, b := seen[len(seen)-2], seen[len(seen)-1]; a == nil || !a.First || b == nil || b.First || b.Thread != th.ID {
		t.Fatalf("rules %+v then %+v", a, b)
	}
	plain := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@coder a plain question"})
	await(t, p, plain.Runs[0].ID, runs.Completed)
	if seen := rec.seen(); seen[len(seen)-1] != nil {
		t.Fatalf("a plain thread's turn was told %+v", seen[len(seen)-1])
	}
	// The continuation is audited, naming what it pinned.
	rows, err := p.Audit.List(tenant, audit.Query{Resource: "conversation", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Action == "conversation.continue_import" && row.Change.ID == th.ID && row.Thread == th.ID && row.Actor == operator.Subject && row.Outcome == audit.Succeeded &&
			row.After["document"] == in.Document && row.After["document_digest"] == in.DocumentDigest && row.After["structure_digest"] == in.StructureDigest && row.Project == "Field" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no audit record of the continuation in %+v", rows)
	}
}

// A document no import kept a structure with is continued as one block of
// its text, labelled as imported with its speakers not known, and stating
// nothing of a structure. One whose uri names no imported session is not.
func TestALegacyDocumentIsContinuedAsOneBlock(t *testing.T) {
	p, _ := continuing(t)
	legacy, err := p.Knowledge.Add(tenant, "sessions-in-field", "Written before structures", "codex:generated-legacy", "# Written before structures\n\n## user\n\ngenerated question\n\n## assistant\n\ngenerated answer", "ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	in := platform.ContinueImportInput{Source: "sessions-in-field", Document: legacy.ID, DocumentDigest: legacy.Digest, Agents: []string{"coder"}}
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	im := th.Import
	if im.Structure != threads.StructureNotKept || im.Tool != "" || im.StructureDigest != "" || im.Branches != nil || im.ImportCut != nil || len(im.Context) != 1 || im.Context[0].Speaker != threads.ImportDocument || !strings.Contains(im.Context[0].Text, "generated answer") {
		t.Fatalf("legacy import %+v", im)
	}
	posted := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder go on"})
	r := await(t, p, posted.Runs[0].ID, runs.Completed)
	if sent := wire.TextOf(r.Tag()); !strings.Contains(sent, "imported document, speakers not known: (imported, not said here) # Written before structures ## user generated question") {
		t.Fatalf("sent:\n%s", sent)
	}
	// A structure digest for a document that has none is not what was read.
	in.StructureDigest = strings.Repeat("0", 64)
	if _, err := p.ContinueImport(t.Context(), operator, in); !errors.Is(err, platform.ErrImportChanged) {
		t.Fatalf("a structure that is not there: %v", err)
	}
	other, err := p.Knowledge.Add(tenant, "sessions-in-field", "A runbook", "https://example.invalid/runbook", "generated runbook", "ada", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.ContinueImport(t.Context(), operator, platform.ContinueImportInput{Source: "sessions-in-field", Document: other.ID, DocumentDigest: other.Digest}); !errors.Is(err, platform.ErrImportNotSession) {
		t.Fatalf("a document that is no session: %v", err)
	}
}

// What the person read is what is continued: a document or a structure that
// changed since, a structure an edit made untrusted, an excluded document, a
// source detached from the project they saw it in or deleted, a source they
// cannot see, a project they may not post in, and another workspace each
// refuse it, and start no thread.
func TestAContinuationIsRefusedWhenWhatWasReadIsNoLongerSo(t *testing.T) {
	p, _ := continuing(t)
	before, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	session := generatedSession(knowledge.ToolCodex, "generated-pins")
	doc, in := imported(t, p, session)
	refused := func(name string, who auth.Principal, in platform.ContinueImportInput, want error) {
		t.Helper()
		_, err := p.ContinueImport(t.Context(), who, in)
		if err == nil || (want != nil && !errors.Is(err, want)) {
			t.Errorf("%s: err = %v, want %v", name, err, want)
		}
	}
	stale := in
	stale.DocumentDigest = strings.Repeat("0", 64)
	refused("a stale document digest", operator, stale, platform.ErrImportChanged)
	stale = in
	stale.StructureDigest = strings.Repeat("0", 64)
	refused("a stale structure digest", operator, stale, platform.ErrImportChanged)
	stale.StructureDigest = ""
	refused("no structure digest for a kept structure", operator, stale, platform.ErrImportChanged)
	elsewhere := "Initech"
	named := in
	named.Project = &elsewhere
	refused("a project the source is not filed in", operator, named, platform.ErrImportDetached)
	refused("a reader who may not start a run", viewer, in, auth.ErrForbidden)
	refused("another workspace", auth.Principal{Subject: operator.Subject, Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Operator}}, in, threads.ErrNotFound)

	// The structure reimported: the old pins are stale.
	session.Turns = append(session.Turns, knowledge.ImportedTurn{Speaker: knowledge.SpeakerUser, Text: "generated follow-up"})
	re, err := p.ReimportDocument(admin, "sessions-in-field", doc.ID, doc.Title, "# A generated chat\n\ngenerated text, grown", doc.Digest, time.Now(), nil, session)
	if err != nil {
		t.Fatal(err)
	}
	refused("a document reimported since", operator, in, platform.ErrImportChanged)
	in.DocumentDigest, in.StructureDigest = re.Digest, knowledge.StructureDigest(session)

	// An ordinary edit leaves the structure untrusted, under any pins.
	edited, err := p.Knowledge.ReplaceForScope(tenant, "sessions-in-field", doc.ID, doc.Title, "# A generated chat\n\nedited by hand", re.Digest, time.Now(), nil)
	if err != nil {
		t.Fatal(err)
	}
	editedIn := in
	editedIn.DocumentDigest, editedIn.StructureDigest = edited.Digest, ""
	refused("an edited structure", operator, editedIn, platform.ErrImportUntrusted)

	fresh, freshIn := imported(t, p, generatedSession(knowledge.ToolChatGPT, "generated-fresh"))
	field := "Field"
	freshIn.Project = &field
	if _, err := p.Knowledge.SetExcluded(tenant, "sessions-in-field", fresh.ID, true, time.Now()); err != nil {
		t.Fatal(err)
	}
	refused("an excluded document", operator, freshIn, threads.ErrNotFound)
	if _, err := p.Knowledge.SetExcluded(tenant, "sessions-in-field", fresh.ID, false, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := p.DetachSource(admin, "sessions-in-field", "Field", ""); err != nil {
		t.Fatal(err)
	}
	refused("a source detached from the project it was seen in", operator, freshIn, platform.ErrImportDetached)
	// Filed now only in Globex, which opal reads and may not post in.
	if _, err := p.AttachSource(admin, "sessions-in-field", "Globex", ""); err != nil {
		t.Fatal(err)
	}
	freshIn.Project = nil
	refused("a project opal may not post in", operator, freshIn, projects.ErrForbidden)
	// A source scoped to a project opal is not in is hidden from opal.
	vault, vaultIn := vaulted(t, p, generatedSession(knowledge.ToolClaude, "generated-vault"))
	refused("a source the reader cannot see", operator, vaultIn, threads.ErrNotFound)
	if err := p.Knowledge.Delete(tenant, "sessions-in-field"); err != nil {
		t.Fatal(err)
	}
	refused("a deleted source", admin, freshIn, threads.ErrNotFound)
	_ = vault
	after, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	if len(after) != len(before) {
		t.Fatalf("a refused continuation wrote a thread: %d then %d", len(before), len(after))
	}
}

// A continuation is seen, listed, searched, posted in and forked only by a
// reader who still sees the source it was imported from, as a fork is only
// by one who sees its source; a fork of it keeps its origin, its context
// and that rule. Its imported context is not published raw. Another
// workspace never sees it.
func TestAContinuationFollowsItsOriginSource(t *testing.T) {
	p, rec := continuing(t)
	joinAs(t, p, "Vault", projects.Member, operator.Subject)
	_, in := vaulted(t, p, generatedSession(knowledge.ToolClaudeAI, "generated-visibility"))
	in.Title = "Staging proxy follow-up"
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	if th.Project != "Vault" {
		t.Fatalf("filed in %q, not the source's project", th.Project)
	}
	// opal moves the private conversation to Field.
	if th, err = p.Threads.UpdateMoving(tenant, th.ID, th.Version, time.Now(), func(th *threads.Thread) error { th.Project = "Field"; return nil }); err != nil {
		t.Fatal(err)
	}
	first := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder which port?"})
	await(t, p, first.Runs[0].ID, runs.Completed)

	// Forked, it keeps its origin, and the fork's agent is sent the
	// imported lines ahead of the fork's own history.
	fork, err := p.ForkThread(t.Context(), operator, th.ID, platform.ForkInput{Through: first.Message.ID, Agents: []string{"coder"}})
	if err != nil {
		t.Fatal(err)
	}
	if fork.Import == nil || fork.Import.DocumentDigest != in.DocumentDigest || len(fork.Import.Context) != len(th.Import.Context) {
		t.Fatalf("the fork lost its origin: %+v", fork.Import)
	}
	forked := post(t, p, operator, fork.ID, platform.MessageInput{Text: "@coder and in the fork?"})
	r := await(t, p, forked.Runs[0].ID, runs.Completed)
	sent := wire.TextOf(r.Tag())
	if !strings.Contains(sent, "imported from claude-ai, the assistant there: (imported, not said here)") || !strings.Contains(sent, "Read-only fork context") ||
		strings.Index(sent, "imported from claude-ai") > strings.Index(sent, "Read-only fork context") {
		t.Fatalf("the fork's first turn:\n%s", sent)
	}
	if seen := rec.seen(); seen[len(seen)-1] == nil || seen[len(seen)-1].Thread != fork.ID || !seen[len(seen)-1].First {
		t.Fatalf("the fork's first turn was told %+v", seen[len(seen)-1])
	}

	// Neither is published raw: a reviewed summary is required.
	for _, id := range []string{th.ID, fork.ID} {
		preview, err := p.PreviewPlacement(t.Context(), operator, id, "Field", "general")
		if err != nil {
			t.Fatal(err)
		}
		if !preview.SummaryRequired {
			t.Fatalf("%s is publishable raw: %+v", id, preview)
		}
	}

	visible := func(id string) map[string]bool {
		t.Helper()
		listed := map[string]bool{}
		page, _, err := p.ThreadPageAs(operator, runs.ThreadQuery{})
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range page {
			listed[s.ID] = true
		}
		found := map[string]bool{}
		items, err := p.Discover(t.Context(), operator, platform.DiscoveryQuery{Q: "Staging proxy"})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range items {
			found[it.ID] = true
		}
		return map[string]bool{"sees": p.SeesThread(operator, id), "listed": listed[id], "found": found[id]}
	}
	if got := visible(th.ID); !got["sees"] || !got["listed"] || !got["found"] {
		t.Fatalf("the person who continued it does not see it: %v", got)
	}
	globex := auth.Principal{Subject: operator.Subject, Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
	if p.SeesThread(globex, th.ID) {
		t.Fatal("another workspace sees the continuation")
	}

	// opal leaves the project the source is scoped to, and still works in
	// Field, where the conversations now are: they are no longer seen.
	leave(t, p, "Vault", operator.Subject)
	for _, id := range []string{th.ID, fork.ID} {
		if got := visible(id); got["sees"] || got["listed"] || got["found"] {
			t.Fatalf("%s is seen without its origin source: %v", id, got)
		}
		if _, err := p.PostMessage(t.Context(), operator, id, platform.MessageInput{Text: "@coder still there?"}); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("%s: posted without its origin source: %v", id, err)
		}
	}
	if _, err := p.ForkThread(t.Context(), operator, th.ID, platform.ForkInput{Through: first.Message.ID}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("forked without its origin source: %v", err)
	}
	joinAs(t, p, "Vault", projects.Member, operator.Subject)
	if !p.SeesThread(operator, th.ID) {
		t.Fatal("back in the source's project, the continuation is not seen")
	}
	// Deleted, the source is seen by nobody, and neither is the
	// continuation, an admin of the workspace included.
	if err := p.Knowledge.Delete(tenant, "vault-sessions"); err != nil {
		t.Fatal(err)
	}
	if p.SeesThread(admin, th.ID) || p.SeesThread(operator, th.ID) {
		t.Fatal("a continuation of a deleted source is still seen")
	}
}
