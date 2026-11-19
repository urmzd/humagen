package platform_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

// Every conversation imported here is generated for the tests. None is a
// real session.

// sentModel is a model that answers every turn, and keeps what each turn sent
// it.
type sentModel struct {
	mu    sync.Mutex
	texts []string
	model *agenttest.ScriptedProvider
}

func (a *sentModel) Stream(ctx context.Context, req saigetypes.Request) (<-chan saigetypes.Delta, error) {
	var parts []string
	for _, m := range req.Messages {
		if u, ok := m.(saigetypes.UserMessage); ok {
			for _, part := range u.Parts {
				if t, ok := part.(saigetypes.TextPart); ok {
					parts = append(parts, t.Text)
				}
			}
		}
	}
	a.mu.Lock()
	a.texts = append(a.texts, strings.Join(parts, "\n"))
	a.mu.Unlock()
	return a.model.Stream(ctx, req)
}

// last is what the latest turn sent the model.
func (a *sentModel) last() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.texts) == 0 {
		return ""
	}
	return a.texts[len(a.texts)-1]
}

// continuing is a platform with the agent coder on a model that keeps what
// it is sent, the projects of field, and a source of imported sessions attached
// to Field, which opal works in.
func continuing(t *testing.T) (*platform.Platform, *sentModel) {
	t.Helper()
	return continuingWith(t, platform.Config{})
}

// continuingAt is continuing on the clock now, nil for time.Now.
func continuingAt(t *testing.T, now func() time.Time) (*platform.Platform, *sentModel) {
	t.Helper()
	return continuingWith(t, platform.Config{Now: now})
}

// continuingWith is continuing with cfg's clock and backends.
func continuingWith(t *testing.T, cfg platform.Config) (*platform.Platform, *sentModel) {
	t.Helper()
	answers := make([][]saigetypes.Delta, 32)
	for i := range answers {
		answers[i] = agenttest.TextResponse("generated reply")
	}
	rec := &sentModel{model: &agenttest.ScriptedProvider{Responses: answers}}
	cfg.Ext = platform.Extensions{Providers: map[string]native.ProviderFunc{
		"recorder": func(native.Spec) (saigetypes.Provider, error) { return rec, nil },
	}}
	p := startWith(t, cfg)
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
// turn is sent them, labelled and fenced, with nothing left out.
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
		if got := rec.last(); !strings.Contains(got, "imported from "+tool+", the assistant there: (imported, not said here) The staging proxy listens on port 8443.") {
			t.Fatalf("%s: turn %d: the model was not sent the imported lines:\n%s", tool, i, got)
		}
	}
	// A later turn of a continuation is sent the imported lines and the
	// thread's own turns since, and a plain thread's turn no imported line.
	_, in := imported(t, p, generatedSession(knowledge.ToolChatGPT, "generated-later"))
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"@coder first", "@coder second"} {
		posted := post(t, p, operator, th.ID, platform.MessageInput{Text: text})
		await(t, p, posted.Runs[0].ID, runs.Completed)
	}
	if got := rec.last(); !strings.Contains(got, "(imported, not said here) The staging proxy listens on port 8443.") || !strings.Contains(got, "@coder first") || !strings.Contains(got, "generated reply") {
		t.Fatalf("the second turn was sent:\n%s", got)
	}
	plain := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@coder a plain question"})
	await(t, p, plain.Runs[0].ID, runs.Completed)
	if got := rec.last(); strings.Contains(got, "imported") {
		t.Fatalf("a plain thread's turn was sent imported lines:\n%s", got)
	}
	// The continuation is audited, naming what it pinned.
	rows, err := p.Audit.List(tenant, audit.Query{Resource: "conversation", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 50}, nil)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Action == "conversation.continue_import" && row.Change.ID == th.ID && row.Thread == th.ID && row.Actor == operator.Subject && row.Outcome == audit.Succeeded &&
			row.After["document"] == in.Document && row.After["document_digest"] == in.DocumentDigest && row.After["structure_digest"] == in.StructureDigest && row.Project == "Field" &&
			th.Import.SourceID != "" && row.After["source_id"] == th.Import.SourceID {
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
	if got := rec.last(); !strings.Contains(got, "imported from claude-ai, the assistant there: (imported, not said here)") {
		t.Fatalf("the fork's model was not sent the imported lines:\n%s", got)
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

// A workspace that sends agents no earlier conversation (history none)
// starts no continuation, since nothing it carries would be sent. One
// started before the setting changed is not posted in, run, followed up or
// forked while it holds, with no thread or message written; a plain
// thread is not held to it, and the continuation works again once history
// is sent.
func TestAContinuationIsRefusedWhileNoHistoryIsSent(t *testing.T) {
	p, rec := continuing(t)
	setHistory := func(h platform.History) {
		t.Helper()
		cfg := p.Settings(tenant)
		cfg.History = h
		if err := p.SetSettings(t.Context(), tenant, cfg); err != nil {
			t.Fatal(err)
		}
	}
	_, in := imported(t, p, generatedSession(knowledge.ToolChatGPT, "generated-none"))
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	first := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder which port?"})
	await(t, p, first.Runs[0].ID, runs.Completed)

	setHistory(platform.HistoryNone)
	before, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	_, again := imported(t, p, generatedSession(knowledge.ToolCodex, "generated-none-2"))
	if _, err := p.ContinueImport(t.Context(), operator, again); !errors.Is(err, platform.ErrImportNoHistory) {
		t.Fatalf("started with history none: %v", err)
	}
	if _, err := p.PostMessage(t.Context(), operator, th.ID, platform.MessageInput{Text: "@coder and now?"}); !errors.Is(err, platform.ErrImportNoHistory) {
		t.Fatalf("posted with history none: %v", err)
	}
	if _, err := p.ForkThread(t.Context(), operator, th.ID, platform.ForkInput{Through: first.Message.ID, Agents: []string{"coder"}}); !errors.Is(err, platform.ErrImportNoHistory) {
		t.Fatalf("forked with history none: %v", err)
	}
	if _, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "generated-follow-up", Agent: "coder", Text: "and through the API?", After: first.Runs[0].ID, Actor: event.Actor{ID: operator.Subject, User: operator.Subject}}); !errors.Is(err, platform.ErrImportNoHistory) {
		t.Fatalf("followed up through the API with history none: %v", err)
	}
	after, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	if len(after) != len(before) {
		t.Fatalf("a refused continuation or fork wrote a thread: %d then %d", len(before), len(after))
	}
	if msgs, _, err := p.Threads.Messages(tenant, th.ID, "", 50); err != nil || len(msgs) != 1 {
		t.Fatalf("a refused post wrote a message: %d, %v", len(msgs), err)
	}
	plain := startThread(t, p, operator, platform.ThreadInput{Project: "Field", Text: "@coder a plain question"})
	await(t, p, plain.Runs[0].ID, runs.Completed)

	setHistory(platform.HistoryRecent)
	later := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder back again"})
	await(t, p, later.Runs[0].ID, runs.Completed)
	if got := rec.last(); !strings.Contains(got, "(imported, not said here) The staging proxy listens on port 8443.") {
		t.Fatalf("with history sent again, the model was not sent the imported lines:\n%s", got)
	}
}

// A source deleted and made again under the same name is another source:
// a continuation of the first is seen by no one through it, for good, and
// nothing is posted in it.
func TestAContinuationIsNotRevivedBySourceOfTheSameName(t *testing.T) {
	p, _ := continuing(t)
	_, in := imported(t, p, generatedSession(knowledge.ToolCodex, "generated-revive"))
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	src, err := p.Knowledge.Get(tenant, "sessions-in-field")
	if err != nil || src.ID == "" || th.Import.SourceID != src.ID {
		t.Fatalf("the continuation pins source id %q, the source has %q (%v)", th.Import.SourceID, src.ID, err)
	}
	if err := p.Knowledge.Delete(tenant, "sessions-in-field"); err != nil {
		t.Fatal(err)
	}
	if p.SeesThread(operator, th.ID) {
		t.Fatal("seen with the source deleted")
	}
	if _, err := p.Knowledge.Create(tenant, "sessions-in-field", "An unrelated source reusing the name.", "ada", time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AttachSource(admin, "sessions-in-field", "Field", ""); err != nil {
		t.Fatal(err)
	}
	for _, who := range []auth.Principal{operator, admin} {
		if p.SeesThread(who, th.ID) {
			t.Fatalf("%s sees the continuation through an unrelated source that reuses its origin's name", who.Subject)
		}
	}
	if _, err := p.PostMessage(t.Context(), operator, th.ID, platform.MessageInput{Text: "@coder still there?"}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("posted through a source of the same name: %v", err)
	}
}

// A continuation holds what its document said when it was continued: the
// document excluded or removed since does not hide it, and its agent is
// still sent the imported lines.
func TestAContinuationKeepsItsLinesWhenItsDocumentIsExcludedOrRemoved(t *testing.T) {
	p, rec := continuing(t)
	doc, in := imported(t, p, generatedSession(knowledge.ToolClaude, "generated-removed"))
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func() error{
		func() error {
			_, err := p.Knowledge.SetExcluded(tenant, "sessions-in-field", doc.ID, true, time.Now())
			return err
		},
		func() error { return p.Knowledge.Remove(tenant, "sessions-in-field", doc.ID, time.Now()) },
	} {
		if err := change(); err != nil {
			t.Fatal(err)
		}
		if !p.SeesThread(operator, th.ID) {
			t.Fatal("the continuation is hidden with its document gone")
		}
		posted := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder go on"})
		await(t, p, posted.Runs[0].ID, runs.Completed)
		if got := rec.last(); !strings.Contains(got, "imported from claude, the assistant there: (imported, not said here) The staging proxy listens on port 8443.") {
			t.Fatalf("the model was not sent the imported lines:\n%s", got)
		}
	}
}

// At the smallest history limit a workspace may set, a continuation keeps
// what fits half of it, and its first turn sends the model all of that.
func TestAContinuationAtTheSmallestHistoryLimit(t *testing.T) {
	p, rec := continuing(t)
	cfg := p.Settings(tenant)
	cfg.HistoryLimit = platform.MinHistory
	if err := p.SetSettings(t.Context(), tenant, cfg); err != nil {
		t.Fatal(err)
	}
	session := generatedSession(knowledge.ToolCodex, "generated-small")
	for i := range 8 {
		session.Turns = append(session.Turns,
			knowledge.ImportedTurn{Speaker: knowledge.SpeakerUser, Text: fmt.Sprintf("generated question %d about the staging proxy and its port", i)},
			knowledge.ImportedTurn{Speaker: knowledge.SpeakerResult, Text: fmt.Sprintf("generated result %d", i)},
			knowledge.ImportedTurn{Speaker: knowledge.SpeakerAssistant, Text: fmt.Sprintf("generated answer %d: the proxy listens on 8443", i)})
	}
	_, in := imported(t, p, session)
	th, err := p.ContinueImport(t.Context(), operator, in)
	if err != nil {
		t.Fatal(err)
	}
	if th.Import.Omitted.Turns == 0 || len(th.Import.Context) == 0 {
		t.Fatalf("the test needs a conversation that does not fit: %+v", th.Import.Omitted)
	}
	posted := post(t, p, operator, th.ID, platform.MessageInput{Text: "@coder which port?"})
	await(t, p, posted.Runs[0].ID, runs.Completed)
	got := rec.last()
	if strings.Contains(got, "earlier turns omitted") {
		t.Fatalf("the first turn left part of the seed out:\n%s", got)
	}
	for _, k := range th.Import.Context {
		if !strings.Contains(got, "(imported, not said here) "+k.Text) {
			t.Fatalf("not sent %q:\n%s", k.Text, got)
		}
	}
}

// The source and the document are read again just before the write, and
// the record is built from that read: what changed since the first refuses
// it, and writes no thread. Between the two the attempt is audited, which is
// where these tests change things: the document reimported, the source
// moved to another project, or deleted and made again under its name.
func TestAContinuationIsRefusedWhenTheSecondReadDiffers(t *testing.T) {
	var armed atomic.Pointer[func()]
	now := func() time.Time {
		if f := armed.Load(); f != nil && calledFrom("(*Platform).BeginAudit") && armed.CompareAndSwap(f, nil) {
			(*f)()
		}
		return time.Now()
	}
	p, _ := continuingAt(t, now)
	before, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	session := generatedSession(knowledge.ToolCodex, "generated-between")
	doc, in := imported(t, p, session)
	between := func(name string, change func(), want error) {
		t.Helper()
		armed.Store(&change)
		if _, err := p.ContinueImport(t.Context(), operator, in); !errors.Is(err, want) {
			t.Fatalf("%s between the reads: %v, want %v", name, err, want)
		}
		if armed.Load() != nil {
			t.Fatalf("%s: the change was not made between the reads", name)
		}
	}
	between("the document reimported", func() {
		grown := session
		grown.Turns = append(slices.Clone(session.Turns), knowledge.ImportedTurn{Speaker: knowledge.SpeakerUser, Text: "generated follow-up"})
		if _, err := p.ReimportDocument(admin, "sessions-in-field", doc.ID, doc.Title, "# A generated chat\n\ngenerated text, grown", doc.Digest, time.Now(), nil, grown); err != nil {
			t.Error(err)
		}
	}, platform.ErrImportChanged)
	_, in = imported(t, p, generatedSession(knowledge.ToolCodex, "generated-between-2"))
	between("the source moved to another project", func() {
		if err := p.DetachSource(admin, "sessions-in-field", "Field", ""); err != nil {
			t.Error(err)
		}
		if _, err := p.AttachSource(admin, "sessions-in-field", "Globex", ""); err != nil {
			t.Error(err)
		}
	}, platform.ErrImportDetached)
	if err := p.DetachSource(admin, "sessions-in-field", "Globex", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := p.AttachSource(admin, "sessions-in-field", "Field", ""); err != nil {
		t.Fatal(err)
	}
	between("the source made again", func() {
		if err := p.Knowledge.Delete(tenant, "sessions-in-field"); err != nil {
			t.Error(err)
		}
		if _, err := p.Knowledge.Create(tenant, "sessions-in-field", "Made again.", "ada", time.Now()); err != nil {
			t.Error(err)
		}
		if _, err := p.AttachSource(admin, "sessions-in-field", "Field", ""); err != nil {
			t.Error(err)
		}
	}, threads.ErrNotFound)
	after, _ := p.Threads.Records(tenant, func(threads.Thread) bool { return true })
	if len(after) != len(before) {
		t.Fatalf("a refused continuation wrote a thread: %d then %d", len(before), len(after))
	}
}

// calledFrom reports whether a function whose name ends in name is on the
// caller's stack.
func calledFrom(name string) bool {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(2, pcs)])
	for {
		f, more := frames.Next()
		if strings.HasSuffix(f.Function, name) {
			return true
		}
		if !more {
			return false
		}
	}
}

// olderServerRewrites writes a knowledge source's record back as a server
// on an earlier release does: decoded without the id it does not know
// (knowledge Source.ID), and written again. "knowledge" and "source/" are
// where the knowledge store keeps a source.
func olderServerRewrites(t *testing.T, st state.Store, name string) {
	t.Helper()
	if _, err := state.NewTable[map[string]any](st, "knowledge").Update(tenant, "source/"+name, func(r map[string]any, ok bool) (map[string]any, error) {
		if src, _ := r["source"].(map[string]any); ok && src != nil {
			delete(src, "id")
		}
		return r, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// A source with no id (kept before ids, or rewritten by a server on an
// earlier release) is given one when a conversation imported into it is
// continued, and the continuation pins it. An empty id matches nothing: a
// source whose id an older server strips hides its continuations, and so
// does one deleted and made again with none, on either store.
func TestAContinuationAlwaysPinsASourceID(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p, _ := continuingWith(t, platform.Config{Backends: platform.Backends{State: st}})
		_, in := imported(t, p, generatedSession(knowledge.ToolCodex, "generated-legacy-id"))
		olderServerRewrites(t, st, "sessions-in-field")
		if src, err := p.Knowledge.Get(tenant, "sessions-in-field"); err != nil || src.ID != "" {
			t.Fatalf("the source kept an id through an older server's write: %q, %v", src.ID, err)
		}
		th, err := p.ContinueImport(t.Context(), operator, in)
		if err != nil {
			t.Fatal(err)
		}
		src, err := p.Knowledge.Get(tenant, "sessions-in-field")
		if err != nil || src.ID == "" || th.Import.SourceID != src.ID {
			t.Fatalf("the continuation pins %q, the source has %q (%v)", th.Import.SourceID, src.ID, err)
		}
		if !p.SeesThread(operator, th.ID) {
			t.Fatal("the continuation is not seen")
		}
		// An older server strips the id again: no one sees it, an admin
		// included, and nothing is posted in it.
		olderServerRewrites(t, st, "sessions-in-field")
		if p.SeesThread(operator, th.ID) || p.SeesThread(admin, th.ID) {
			t.Fatal("seen through a source with no id")
		}
		if _, err := p.PostMessage(t.Context(), operator, th.ID, platform.MessageInput{Text: "@coder still there?"}); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("posted through a source with no id: %v", err)
		}
		// Deleted and made again with no id, as an older server makes one,
		// it revives nothing either.
		if err := p.Knowledge.Delete(tenant, "sessions-in-field"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Knowledge.Create(tenant, "sessions-in-field", "Made again by an older server.", "ada", time.Now()); err != nil {
			t.Fatal(err)
		}
		if _, err := p.AttachSource(admin, "sessions-in-field", "Field", ""); err != nil {
			t.Fatal(err)
		}
		olderServerRewrites(t, st, "sessions-in-field")
		if p.SeesThread(operator, th.ID) || p.SeesThread(admin, th.ID) {
			t.Fatal("revived by a source of the same name with no id")
		}
	})
}
