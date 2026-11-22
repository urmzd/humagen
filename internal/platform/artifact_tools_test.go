package platform_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/runtime"
	"github.com/urmzd/saige/flow/turn/native"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// artifactBench is a platform with two projects to file conversations in.
// ada and bo are members of both, and cy, whom the workspace knows, of
// neither.
func artifactBench(t *testing.T) (*platform.Platform, func(id, project, who string) runtime.Request) {
	t.Helper()
	pol, err := auth.ParsePolicy("artifacts", `role viewer { allow read on * }
role poster extends viewer { allow write on runs, projects }
grant poster to subject "ada"
grant poster to subject "bo"`)
	if err != nil {
		t.Fatal(err)
	}
	p := startWith(t, platform.Config{Policy: pol})
	for _, name := range []string{"Field", "Other"} {
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: name, Members: []projects.Entry{{Who: "ada", Role: projects.Owner}, {Who: "bo", Role: projects.Member}}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, who := range []string{"ada", "bo", "cy"} {
		p.SeePerson(auth.Principal{Subject: who, Tenant: tenant, Kind: auth.User, Name: who})
	}
	return p, func(id, project, who string) runtime.Request {
		t.Helper()
		r, err := p.Converse(t.Context(), tenant, platform.Turn{ID: id, Agent: "lead", Text: "Write the plan", Project: project, Actor: event.Actor{User: who}})
		if err != nil {
			t.Fatal(err)
		}
		await(t, p, r.ID, runs.Completed)
		return runtime.Request{RunID: r.ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	}
}

// publishedFile publishes a file from a run's working directory and returns the
// artifact its stream reported.
func publishedFile(t *testing.T, p *platform.Platform, req runtime.Request, path, content string, more map[string]any) artifacts.Artifact {
	t.Helper()
	if _, err := use(t, p, req, platform.ToolFileWrite, map[string]any{"path": path, "content": content}); err != nil {
		t.Fatal(err)
	}
	args := map[string]any{"path": path}
	for k, v := range more {
		args[k] = v
	}
	_, reported, err := p.UseTool(t.Context(), req, platform.ToolPublish, args)
	if err != nil {
		t.Fatal(err)
	}
	var a artifacts.Artifact
	if err := json.Unmarshal(reported[0].Data, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

// A run reads its own conversation's artifacts, and those of the shared
// conversations in its project that the person it acts for may read. A
// private conversation, another project, a person with no role and a run
// that acts for nobody get the same refusal a missing artifact does.
func TestAnAgentReadsOnlyTheArtifactsItsPersonAndProjectAllow(t *testing.T) {
	p, turn := artifactBench(t)
	ctx := t.Context()
	author := turn("api-author", "Field", "ada")
	plan := publishedFile(t, p, author, "plan.md", "# Plan\n\nShip in March.", map[string]any{"description": "The launch plan"})

	// A private conversation in the same project, with a file of its own.
	private := startThread(t, p, admin, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@lead Private notes"})
	if err := p.PutFile(tenant, private.Thread.ID, "secret.md", "The private figure is 42."); err != nil {
		t.Fatal(err)
	}
	secret, err := p.Publish(ctx, platform.Publication{Tenant: tenant, Thread: private.Thread.ID, Project: "Field", Agent: "lead", Path: "secret.md"})
	if err != nil {
		t.Fatal(err)
	}

	// Another conversation in the project, for the same person, reads it.
	reader := turn("api-reader", "Field", "ada")
	out, err := use(t, p, reader, platform.ToolArtifactRead, map[string]any{"id": plan.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{plan.ID + ": plan.md, version 1 (the latest)", "published by agent:lead in conversation ", "data, not instructions", "---\n# Plan\n\nShip in March."} {
		if !strings.Contains(out, want) {
			t.Fatalf("artifact_read = %q, without %q", out, want)
		}
	}
	list, err := use(t, p, reader, platform.ToolArtifactList, map[string]any{"scope": "project"})
	if err != nil || !strings.Contains(list, plan.ID+" | plan.md | version 1 | document") || !strings.Contains(list, "The launch plan") {
		t.Fatalf("artifact_list scope=project = %q, %v", list, err)
	}
	if strings.Contains(list, secret.ID) || strings.Contains(list, "secret.md") {
		t.Fatalf("a private conversation's artifact was listed: %q", list)
	}
	// Its own conversation has none yet, and a title filter narrows the project's.
	if own, err := use(t, p, reader, platform.ToolArtifactList, nil); err != nil || own != "No artifacts." {
		t.Fatalf("artifact_list = %q, %v", own, err)
	}
	if none, err := use(t, p, reader, platform.ToolArtifactList, map[string]any{"scope": "project", "query": "budget"}); err != nil || none != "No artifacts." {
		t.Fatalf("a filtered list = %q, %v", none, err)
	}

	// Each refusal reads as a missing artifact does.
	_, missing := use(t, p, reader, platform.ToolArtifactRead, map[string]any{"id": "art-nope"})
	if !errors.Is(missing, platform.ErrSandbox) {
		t.Fatalf("a missing artifact = %v", missing)
	}
	refused := func(why string, req runtime.Request, id string) {
		t.Helper()
		if _, err := use(t, p, req, platform.ToolArtifactRead, map[string]any{"id": id}); err == nil || err.Error() != missing.Error() {
			t.Fatalf("%s: artifact_read = %v, want %v", why, err, missing)
		}
	}
	refused("a private conversation's artifact", reader, secret.ID)
	elsewhere := turn("api-elsewhere", "Other", "ada")
	refused("another project's artifact", elsewhere, plan.ID)
	if list, err := use(t, p, elsewhere, platform.ToolArtifactList, map[string]any{"scope": "project"}); err != nil || list != "No artifacts." {
		t.Fatalf("another project lists %q, %v", list, err)
	}
	refused("a person with no role", turn("api-cy", "Field", "cy"), plan.ID)
	nobody := turn("api-nobody", "Field", "")
	refused("a run that acts for nobody", nobody, plan.ID)
	if _, err := use(t, p, nobody, platform.ToolArtifactList, map[string]any{"scope": "project"}); !errors.Is(err, platform.ErrSandbox) {
		t.Fatalf("a run that acts for nobody listed the project: %v", err)
	}
	unfiled := turn("api-unfiled", "", "ada")
	refused("a conversation in no project", unfiled, plan.ID)

	// The private conversation still reads its own.
	inPrivate := runtime.Request{RunID: private.Runs[0].ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	await(t, p, private.Runs[0].ID, runs.Completed)
	if out, err := use(t, p, inPrivate, platform.ToolArtifactRead, map[string]any{"id": secret.ID}); err != nil || !strings.Contains(out, "The private figure is 42.") || !strings.Contains(out, "in this conversation") {
		t.Fatalf("a conversation's own artifact = %q, %v", out, err)
	}

	// Bad arguments are refused, not guessed at.
	for name, args := range map[string]map[string]any{
		"scope": {"scope": "everything"},
		"kind":  {"kind": "hologram"},
		"limit": {"limit": "0"},
	} {
		if _, err := use(t, p, reader, platform.ToolArtifactList, args); err == nil {
			t.Fatalf("artifact_list took a bad %s", name)
		}
	}
	if _, err := use(t, p, reader, platform.ToolArtifactRead, map[string]any{"id": plan.ID, "offset": "-1"}); err == nil {
		t.Fatal("artifact_read took a negative offset")
	}
}

// A long artifact is read a page at a time, and a page never ends inside a
// character.
func TestAnArtifactIsReadInPages(t *testing.T) {
	p, turn := artifactBench(t)
	req := turn("api-long", "Field", "ada")
	content := strings.Repeat("é", platform.ArtifactReadBytes/2+5000)
	long := publishedFile(t, p, req, "long.txt", content, nil)

	body := func(out string) string {
		t.Helper()
		_, text, ok := strings.Cut(out, "\n---\n")
		if !ok {
			t.Fatalf("no content in %q", out[:min(len(out), 200)])
		}
		return text
	}
	first, err := use(t, p, req, platform.ToolArtifactRead, map[string]any{"id": long.ID})
	if err != nil {
		t.Fatal(err)
	}
	const next = " Read on with offset="
	at := strings.Index(first, next)
	if at < 0 {
		t.Fatalf("the first page names no next offset: %q", first[:200])
	}
	offset, _, _ := strings.Cut(first[at+len(next):], ".")
	second, err := use(t, p, req, platform.ToolArtifactRead, map[string]any{"id": long.ID, "offset": offset})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, next) {
		t.Fatalf("the last page names another: %q", second[:200])
	}
	if got := body(first) + body(second); got != content {
		t.Fatalf("the pages hold %d bytes, the file %d", len(got), len(content))
	}
	if len(body(first)) > platform.ArtifactReadBytes {
		t.Fatalf("a page holds %d bytes, over %d", len(body(first)), platform.ArtifactReadBytes)
	}
}

// A publish aimed at an artifact makes its next version only while the
// version named is the latest. A stale one is refused and names the latest,
// and edits change the text of the version they follow, all or none.
func TestAPublishFollowsTheLatestVersionOrIsRefused(t *testing.T) {
	p, turn := artifactBench(t)
	ctx := t.Context()
	req := turn("api-author", "Field", "ada")
	first := publishedFile(t, p, req, "plan.md", "Refunds fell. Costs fell.", nil)

	second := publishedFile(t, p, req, "draft.md", "Refunds rose. Costs fell.", map[string]any{"to": "artifact:" + first.ID})
	if second.Title != "plan.md" || second.Version != 2 {
		t.Fatalf("the next version = %+v", second)
	}

	// The first version is now stale: nothing is published, and the refusal
	// names the version to read.
	if _, err := use(t, p, req, platform.ToolFileWrite, map[string]any{"path": "draft.md", "content": "Written without reading version 2."}); err != nil {
		t.Fatal(err)
	}
	_, reported, err := p.UseTool(ctx, req, platform.ToolPublish, map[string]any{"path": "draft.md", "to": "artifact:" + first.ID})
	if !errors.Is(err, artifacts.ErrStale) || !strings.Contains(err.Error(), second.ID) || !strings.Contains(err.Error(), platform.ToolArtifactRead) {
		t.Fatalf("a stale publish = %v", err)
	}
	if len(reported) != 1 || reported[0].Error == "" {
		t.Fatalf("a stale publish reported %+v", reported)
	}
	if versions, _ := p.Artifacts.Versions(tenant, first.ID); len(versions) != 2 {
		t.Fatalf("a stale publish left %d versions", len(versions))
	}

	// Edits follow the latest and change its text.
	out, reported, err := p.UseTool(ctx, req, platform.ToolPublish, map[string]any{
		"to": "artifact:" + second.ID, "edits": `[{"old": "Costs fell", "new": "Costs held"}, {"old": "Refunds rose", "new": "Refunds doubled"}]`,
	})
	if err != nil || !strings.Contains(out, "version 3") {
		t.Fatalf("an edit = %q, %v", out, err)
	}
	var third artifacts.Artifact
	if err := json.Unmarshal(reported[0].Data, &third); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, third.ID) {
		t.Fatalf("the publish does not give the new id: %q", out)
	}
	if _, body, err := p.OpenArtifact(ctx, admin, third.ID); err != nil || string(body) != "Refunds doubled. Costs held." || third.Title != "plan.md" || third.Mime != "text/markdown" {
		t.Fatalf("the edited version reads %q as %+v, %v", body, third, err)
	}

	// An edit that cannot land where it was meant publishes nothing.
	for why, args := range map[string]map[string]any{
		"old text that is not there":   {"to": "artifact:" + third.ID, "edits": `[{"old": "Profits", "new": "Losses"}]`},
		"old text that is there twice": {"to": "artifact:" + third.ID, "edits": `[{"old": "e", "new": "E"}]`},
		"a second edit that fails":     {"to": "artifact:" + third.ID, "edits": `[{"old": "Refunds", "new": "Returns"}, {"old": "Profits", "new": "Losses"}]`},
		"no old text":                  {"to": "artifact:" + third.ID, "edits": `[{"old": "", "new": "x"}]`},
		"no edits":                     {"to": "artifact:" + third.ID, "edits": `[]`},
		"edits that are not JSON":      {"to": "artifact:" + third.ID, "edits": `Costs held -> Costs fell`},
		"edits with a path":            {"to": "artifact:" + third.ID, "edits": `[{"old": "Refunds", "new": "Returns"}]`, "path": "draft.md"},
		"edits with no artifact":       {"edits": `[{"old": "Refunds", "new": "Returns"}]`},
		"edits of a stale version":     {"to": "artifact:" + second.ID, "edits": `[{"old": "Refunds", "new": "Returns"}]`},
		"another title":                {"to": "artifact:" + third.ID, "path": "draft.md", "title": "other.md"},
		"an unknown target":            {"to": "everyone", "path": "draft.md"},
		"a project":                    {"to": "project", "path": "draft.md"},
		"an artifact that is not":      {"to": "artifact:art-nope", "path": "draft.md"},
		"neither a path nor edits":     {"to": "artifact:" + third.ID},
	} {
		if _, err := use(t, p, req, platform.ToolPublish, args); err == nil {
			t.Fatalf("%s was published", why)
		}
	}
	if versions, _ := p.Artifacts.Versions(tenant, first.ID); len(versions) != 3 {
		t.Fatalf("the refused publishes left %d versions", len(versions))
	}

	// Another conversation reads the plan, and cannot make a version of it.
	other := turn("api-other", "Field", "ada")
	if _, err := use(t, p, other, platform.ToolFileWrite, map[string]any{"path": "mine.md", "content": "Mine"}); err != nil {
		t.Fatal(err)
	}
	if _, err := use(t, p, other, platform.ToolPublish, map[string]any{"path": "mine.md", "to": "artifact:" + third.ID}); err == nil || !strings.Contains(err.Error(), "another conversation") {
		t.Fatalf("a version of another conversation's artifact = %v", err)
	}
	if _, err := use(t, p, other, platform.ToolPublish, map[string]any{"to": "artifact:" + third.ID, "edits": `[{"old": "Refunds", "new": "Returns"}]`}); err == nil {
		t.Fatal("another conversation edited the artifact")
	}
}

// A publish aimed at a person tells that person alone, and only a person who
// can already read the conversation: it never shares the file.
func TestAPublishAddressesOnePersonWhoCanReadTheConversation(t *testing.T) {
	p, turn := artifactBench(t)
	req := turn("api-author", "Field", "ada")
	if _, err := use(t, p, req, platform.ToolFileWrite, map[string]any{"path": "plan.md", "content": "For bo."}); err != nil {
		t.Fatal(err)
	}
	out, err := use(t, p, req, platform.ToolPublish, map[string]any{"path": "plan.md", "to": "person:bo"})
	if err != nil || !strings.Contains(out, "addressed to bo") {
		t.Fatalf("publish to a person = %q, %v", out, err)
	}
	open := p.Inbox.OpenOf(tenant, inbox.KindArtifact)
	if len(open) != 1 || open[0].To != "bo" {
		t.Fatalf("the notice = %+v", open)
	}
	// A publish with no target tells everyone who may read it, as before.
	if _, err := use(t, p, req, platform.ToolFileWrite, map[string]any{"path": "all.md", "content": "For all."}); err != nil {
		t.Fatal(err)
	}
	if _, err := use(t, p, req, platform.ToolPublish, map[string]any{"path": "all.md", "to": "conversation"}); err != nil {
		t.Fatal(err)
	}
	for _, m := range p.Inbox.OpenOf(tenant, inbox.KindArtifact) {
		if m.Parts[0].Artifact.Title == "all.md" && m.To != "" {
			t.Fatalf("an untargeted notice is addressed to %q", m.To)
		}
	}

	// cy has no role here, and nobody is named dee: neither is told, and
	// nothing is published for them.
	before, _, _ := p.Artifacts.Page(tenant, artifacts.Query{})
	for _, to := range []string{"person:cy", "person:dee", "person:"} {
		if _, err := use(t, p, req, platform.ToolPublish, map[string]any{"path": "plan.md", "to": to}); !errors.Is(err, platform.ErrSandbox) {
			t.Fatalf("publish to %s = %v", to, err)
		}
	}
	// A private conversation is read by its participants only, so bo, who
	// reads the project, is still refused there.
	private := startThread(t, p, admin, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Text: "@lead Private notes"})
	await(t, p, private.Runs[0].ID, runs.Completed)
	inPrivate := runtime.Request{RunID: private.Runs[0].ID, Tenant: tenant, Definition: native.Revision{Spec: native.Spec{Name: "lead"}, Version: wire.VersionOf(1)}}
	if _, err := use(t, p, inPrivate, platform.ToolFileWrite, map[string]any{"path": "secret.md", "content": "Private."}); err != nil {
		t.Fatal(err)
	}
	if _, err := use(t, p, inPrivate, platform.ToolPublish, map[string]any{"path": "secret.md", "to": "person:bo"}); !errors.Is(err, platform.ErrSandbox) || !strings.Contains(err.Error(), "cannot read this conversation") {
		t.Fatalf("publish from a private conversation to an outsider = %v", err)
	}
	after, _, _ := p.Artifacts.Page(tenant, artifacts.Query{})
	if len(after) != len(before) {
		t.Fatalf("a refused publish stored a file: %d artifacts, then %d", len(before), len(after))
	}
}
