package platform_test

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/threads"
)

// installer is someone who may install and remove the store's packages.
var installer = auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Admin}}

// install adds store packages to a workspace, as a test's setup.
func install(t *testing.T, p *platform.Platform, tenant string, packages ...string) {
	t.Helper()
	who := installer
	who.Tenant = tenant
	for _, name := range packages {
		if _, err := p.InstallPackage(t.Context(), who, name); err != nil {
			t.Fatalf("install %s: %v", name, err)
		}
	}
}

// shipping is a platform that ships its own agents on the offline model.
func shipping(t *testing.T, cfg platform.Config) *platform.Platform {
	t.Helper()
	cfg.Workspace = t.TempDir()
	cfg.TestBuiltinProvider, cfg.TestBuiltinModel = "offline", "offline"
	p, err := platform.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	return p
}

func directory(t *testing.T, p *platform.Platform) []string {
	t.Helper()
	rows, _, err := p.AgentDirectory(t.Context(), tenant, wire.Page{Size: 100})
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range rows {
		names = append(names, r.Spec.Name)
	}
	slices.Sort(names)
	return names
}

func storePackage(t *testing.T, p *platform.Platform, name string) platform.StorePackage {
	t.Helper()
	all, err := p.StorePackages(t.Context(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range all {
		if pkg.Name == name {
			return pkg
		}
	}
	t.Fatalf("the store has no %s: %+v", name, all)
	return platform.StorePackage{}
}

var (
	coreAgents   = []string{platform.BuiltinWriter, platform.BuiltinAnalyst}
	reviewAgents = []string{platform.BuiltinCodeReviewer, platform.BuiltinSecurityReviewer, platform.BuiltinArchitectReviewer, platform.BuiltinQAReviewer}
)

// A new workspace has the entry agent and the approval agent, and the rest
// wait in the store, not installed.
func TestANewWorkspaceHasOnlyTheEntryAndApprovalAgents(t *testing.T) {
	p := shipping(t, platform.Config{})
	if got := directory(t, p); !slices.Equal(got, []string{platform.BuiltinApprover, platform.BuiltinEntry}) {
		t.Fatalf("a new workspace has %v", got)
	}
	all, err := p.StorePackages(t.Context(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].Name != "humagen/core" || all[1].Name != "humagen/review" {
		t.Fatalf("store = %+v", all)
	}
	for i, want := range [][]string{coreAgents, reviewAgents} {
		pkg := all[i]
		if pkg.Installed || !slices.Equal(pkg.Missing, want) || pkg.Title == "" || pkg.Description == "" || !strings.HasPrefix(pkg.Version, "sha256:") || len(pkg.Agents) != len(want) {
			t.Fatalf("%s = %+v", pkg.Name, pkg)
		}
	}
	// Ensuring again adds nothing from the store.
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	if got := directory(t, p); len(got) != 2 {
		t.Fatalf("ensuring added %v", got)
	}
}

// Installing adds exactly a package's agents, as platform revisions on the
// package's current version; installing again adds nothing.
func TestInstallingCoreThenReviewAddsExactlyTheirAgents(t *testing.T) {
	p := shipping(t, platform.Config{})
	core, err := p.InstallPackage(t.Context(), installer, "humagen/core")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(core.Added, coreAgents) || !core.Package.Installed || len(core.Package.Missing) != 0 {
		t.Fatalf("install core = %+v", core)
	}
	want := append(slices.Clone(coreAgents), platform.BuiltinApprover, platform.BuiltinEntry)
	slices.Sort(want)
	if got := directory(t, p); !slices.Equal(got, want) {
		t.Fatalf("after core: %v, want %v", got, want)
	}
	// The short name is the same package.
	review, err := p.InstallPackage(t.Context(), installer, "review")
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(review.Added, reviewAgents) || review.Package.Name != "humagen/review" {
		t.Fatalf("install review = %+v", review)
	}
	want = append(want, reviewAgents...)
	slices.Sort(want)
	if got := directory(t, p); !slices.Equal(got, want) {
		t.Fatalf("after review: %v, want %v", got, want)
	}
	revs := map[string]int{}
	for _, pkg := range []platform.StorePackage{core.Package, review.Package} {
		for _, a := range pkg.Agents {
			r, err := p.Agents.Get(t.Context(), tenant, a.Name, 0)
			if err != nil {
				t.Fatal(err)
			}
			if r.CreatedBy != "platform" || r.Spec.Package == nil || r.Spec.Package.Name != pkg.Name || r.Spec.Package.Version != pkg.Version || !a.Installed || a.Customized {
				t.Fatalf("%s = %+v, %+v", a.Name, r, a)
			}
			revs[a.Name] = r.Rev
		}
	}
	// The code reviewer brings in its specialists, as it always did.
	cr, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinCodeReviewer, 0)
	if got := slices.Sorted(slices.Values(cr.Spec.Access.Spawn)); !slices.Equal(got, slices.Sorted(slices.Values(reviewAgents[1:]))) {
		t.Fatalf("code-reviewer spawn = %v", got)
	}
	// The entry agent may call what was installed.
	entry, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0)
	for _, name := range append(slices.Clone(coreAgents), reviewAgents...) {
		if !slices.Contains(entry.Spec.Access.Spawn, name) {
			t.Fatalf("the entry agent may not call %s: %v", name, entry.Spec.Access.Spawn)
		}
	}
	again, err := p.InstallPackage(t.Context(), installer, "humagen/core")
	if err != nil || len(again.Added) != 0 || !again.Package.Installed {
		t.Fatalf("install again = %+v, %v", again, err)
	}
	for name, rev := range revs {
		if r, _ := p.Agents.Get(t.Context(), tenant, name, 0); r.Rev != rev {
			t.Fatalf("installing again revised %s: %d, was %d", name, r.Rev, rev)
		}
	}
	// Each install that added something is audited, once.
	records, err := p.AuditRecords(installer, audit.Query{Resource: "agent_package", From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour), Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 2 {
		t.Fatalf("audit = %+v", records)
	}
	for _, r := range records {
		if r.Action != "agent.package.install" || r.Actor != installer.Subject || r.Outcome != audit.Succeeded {
			t.Fatalf("audit record = %+v", r)
		}
	}
}

// Removing retires a package's agents: the directory no longer lists them,
// their history reads as before, and installing again brings them back.
func TestRemovingAPackageRetiresItsAgentsAndInstallingAgainRestoresThem(t *testing.T) {
	p := shipping(t, platform.Config{})
	install(t, p, tenant, "humagen/core")
	writer, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0)
	if err != nil {
		t.Fatal(err)
	}
	removed, err := p.RemovePackage(t.Context(), installer, "humagen/core", false)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(removed.Retired, coreAgents) || len(removed.Customized) != 0 || removed.Package.Installed || !slices.Equal(removed.Package.Missing, coreAgents) {
		t.Fatalf("remove = %+v", removed)
	}
	if got := directory(t, p); !slices.Equal(got, []string{platform.BuiltinApprover, platform.BuiltinEntry}) {
		t.Fatalf("after removing core: %v", got)
	}
	for _, name := range []string{platform.BuiltinWriter, "warp-writer"} {
		if _, err := p.Agents.Get(t.Context(), tenant, name, 0); !errors.Is(err, wire.ErrNotFound) {
			t.Fatalf("%s is still current: %v", name, err)
		}
	}
	if pinned, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, writer.Rev); err != nil || pinned.Hash != writer.Hash {
		t.Fatalf("pinned revision after removal: %+v, %v", pinned, err)
	}
	if history, err := p.Agents.History(t.Context(), tenant, platform.BuiltinWriter); err != nil || len(history) == 0 {
		t.Fatalf("history after removal: %v, %v", history, err)
	}
	entry, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinEntry, 0)
	if slices.Contains(entry.Spec.Access.Spawn, platform.BuiltinWriter) {
		t.Fatalf("the entry agent still calls the writer: %v", entry.Spec.Access.Spawn)
	}
	// Ensuring never brings a removed package back.
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	if got := directory(t, p); len(got) != 2 {
		t.Fatalf("ensuring restored %v", got)
	}
	// Removing again retires nothing.
	if again, err := p.RemovePackage(t.Context(), installer, "humagen/core", false); err != nil || len(again.Retired) != 0 {
		t.Fatalf("remove again = %+v, %v", again, err)
	}
	back, err := p.InstallPackage(t.Context(), installer, "humagen/core")
	if err != nil || !slices.Equal(back.Added, coreAgents) {
		t.Fatalf("reinstall = %+v, %v", back, err)
	}
	for _, name := range []string{platform.BuiltinWriter, "warp-writer", platform.BuiltinAnalyst, "warp-analyst"} {
		r, err := p.Agents.Get(t.Context(), tenant, name, 0)
		if err != nil || r.CreatedBy != "platform" || r.Spec.Package == nil || *r.Spec.Package != *writer.Spec.Package {
			t.Fatalf("%s after reinstall = %+v, %v", name, r, err)
		}
	}
	// Its versions continue: the revision it was at is still its own.
	if r, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0); r.Rev < writer.Rev || !r.Spec.SameContent(writer.Spec) {
		t.Fatalf("writer after reinstall = %+v", r)
	}
}

// An agent someone changed is retired only when the removal is forced, and
// the removal says so; installing again starts it from the package's base.
func TestRemovingACustomizedAgentTakesForce(t *testing.T) {
	p := shipping(t, platform.Config{})
	install(t, p, tenant, "humagen/core")
	base, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0)
	custom := base.Spec.Clone()
	custom.SystemPrompt = "Write in the house style."
	if _, err := p.Agents.Revise(t.Context(), tenant, custom, base.Rev, "ana"); err != nil {
		t.Fatal(err)
	}
	if a := storePackage(t, p, "humagen/core").Agents[0]; !a.Customized || !a.Installed {
		t.Fatalf("customized writer reads %+v", a)
	}
	_, err := p.RemovePackage(t.Context(), installer, "humagen/core", false)
	if !errors.Is(err, platform.ErrStoreConflict) || !strings.Contains(err.Error(), "@humagen-writer has changes made in this workspace") {
		t.Fatalf("unforced removal = %v", err)
	}
	if got := directory(t, p); !slices.Contains(got, platform.BuiltinWriter) || !slices.Contains(got, platform.BuiltinAnalyst) {
		t.Fatalf("a refused removal retired something: %v", got)
	}
	removed, err := p.RemovePackage(t.Context(), installer, "humagen/core", true)
	if err != nil || !slices.Equal(removed.Retired, coreAgents) || !slices.Equal(removed.Customized, []string{platform.BuiltinWriter}) {
		t.Fatalf("forced removal = %+v, %v", removed, err)
	}
	install(t, p, tenant, "humagen/core")
	r, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0)
	if r.CreatedBy != "platform" || !r.Spec.SameContent(base.Spec) {
		t.Fatalf("reinstalled writer = %+v", r)
	}
	// The workspace's changes are still in its history.
	history, _ := p.Agents.History(t.Context(), tenant, platform.BuiltinWriter)
	if !slices.ContainsFunc(history, func(h wire.Revision) bool { return h.Spec.SystemPrompt == custom.SystemPrompt }) {
		t.Fatal("the customization left the history")
	}
}

// A workspace that has every agent keeps them: ensuring keeps them current
// and the store reads both packages as installed.
func TestAWorkspaceWithEveryAgentKeepsThem(t *testing.T) {
	p := shipping(t, platform.Config{})
	install(t, p, tenant, "humagen/core", "humagen/review")
	writer, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0)
	older := writer.Spec.Clone()
	older.Package.Version = "sha256:previous"
	older.SystemPrompt = "An earlier package prompt."
	if _, err := p.Agents.Revise(t.Context(), tenant, older, writer.Rev, "platform"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
			t.Fatal(err)
		}
	}
	if got := directory(t, p); len(got) != 8 {
		t.Fatalf("directory = %v", got)
	}
	upgraded, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinWriter, 0)
	if !upgraded.Spec.SameContent(writer.Spec) || *upgraded.Spec.Package != *writer.Spec.Package {
		t.Fatalf("an installed agent was not kept current: %+v", upgraded)
	}
	for _, name := range []string{"humagen/core", "humagen/review"} {
		if pkg := storePackage(t, p, name); !pkg.Installed || len(pkg.Missing) != 0 {
			t.Fatalf("%s = %+v", name, pkg)
		}
	}
}

// A package one of whose agents was retired reads as installed with that
// agent missing, and installing again restores only it.
func TestInstallingRestoresAPartlyRemovedPackage(t *testing.T) {
	p := shipping(t, platform.Config{})
	install(t, p, tenant, "humagen/review")
	qa, _ := p.Agents.Get(t.Context(), tenant, platform.BuiltinQAReviewer, 0)
	if err := p.Agents.Delete(t.Context(), tenant, platform.BuiltinQAReviewer, qa.Rev); err != nil {
		t.Fatal(err)
	}
	pkg := storePackage(t, p, "humagen/review")
	if !pkg.Installed || !slices.Equal(pkg.Missing, []string{platform.BuiltinQAReviewer}) {
		t.Fatalf("partly removed = %+v", pkg)
	}
	out, err := p.InstallPackage(t.Context(), installer, "humagen/review")
	if err != nil || !slices.Equal(out.Added, []string{platform.BuiltinQAReviewer}) || len(out.Package.Missing) != 0 {
		t.Fatalf("restore = %+v, %v", out, err)
	}
}

// Installing and removing take agents:write; a name one of the workspace's
// own agents holds refuses the install before anything is added, and is
// never retired.
func TestTheStoreKeepsToPermissionsAndNamesTheWorkspaceOwns(t *testing.T) {
	p := shipping(t, platform.Config{})
	reader := auth.Principal{Tenant: tenant, Subject: "rae", Scopes: []auth.Permission{auth.AgentsRead}}
	if _, err := p.InstallPackage(t.Context(), reader, "humagen/core"); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a reader installed: %v", err)
	}
	if _, err := p.RemovePackage(t.Context(), reader, "humagen/core", true); !errors.Is(err, auth.ErrForbidden) {
		t.Fatalf("a reader removed: %v", err)
	}
	for _, name := range []string{"humagen/system", "humagen/entry", "nope"} {
		if _, err := p.InstallPackage(t.Context(), installer, name); !errors.Is(err, wire.ErrNotFound) {
			t.Fatalf("install %s = %v", name, err)
		}
	}
	mine, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: platform.BuiltinCodeReviewer, Provider: "offline", Model: "offline", SystemPrompt: "Our own reviewer."}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	pkg := storePackage(t, p, "humagen/review")
	if pkg.Installed || !pkg.Agents[0].Taken || slices.Contains(pkg.Missing, platform.BuiltinCodeReviewer) {
		t.Fatalf("a taken name reads %+v", pkg)
	}
	if _, err := p.InstallPackage(t.Context(), installer, "humagen/review"); !errors.Is(err, platform.ErrStoreConflict) || !strings.Contains(err.Error(), "@code-reviewer is one of this workspace's own agents") {
		t.Fatalf("install over a taken name = %v", err)
	}
	if _, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinSecurityReviewer, 0); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("a refused install added a specialist: %v", err)
	}
	if out, err := p.RemovePackage(t.Context(), installer, "humagen/review", true); err != nil || len(out.Retired) != 0 {
		t.Fatalf("remove over a taken name = %+v, %v", out, err)
	}
	if got, err := p.Agents.Get(t.Context(), tenant, platform.BuiltinCodeReviewer, 0); err != nil || got.Hash != mine.Hash {
		t.Fatalf("the workspace's own agent changed: %+v, %v", got, err)
	}
	// Ensuring leaves it alone too.
	if err := p.EnsureBuiltins(t.Context(), tenant); err != nil {
		t.Fatal(err)
	}
	// A server that ships no agents of its own has an empty store.
	bare, err := platform.New(platform.Config{Workspace: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = bare.Close() })
	if all, err := bare.StorePackages(t.Context(), tenant); err != nil || len(all) != 0 {
		t.Fatalf("bare store = %+v, %v", all, err)
	}
	if _, err := bare.InstallPackage(t.Context(), installer, "humagen/core"); !errors.Is(err, wire.ErrNotFound) {
		t.Fatalf("bare install = %v", err)
	}
}

// A run for one of the store's agents that the workspace has not installed
// is refused saying which package to install, and a schedule's refusal
// reaches a person as an alert.
func TestAStoreAgentNotInstalledIsRefusedSayingWhatToInstall(t *testing.T) {
	p := shipping(t, platform.Config{})
	_, err := p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "ask-writer", Tenant: tenant, Agent: platform.BuiltinWriter, Origin: "api", Text: "Write a report"}))
	if !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), "@humagen-writer is not installed: install the Core package from the Store") {
		t.Fatalf("a run for an uninstalled writer = %v", err)
	}
	_, err = p.Invoke(t.Context(), wire.NewEvent(wire.Raise{ID: "scheduled", Tenant: tenant, Agent: platform.BuiltinAnalyst, Origin: "cron", At: time.Now(), Text: "Recap", Context: map[string]string{wire.MetaCronSchedule: "weekly-recap"}}))
	if !errors.Is(err, platform.ErrNotAvailable) {
		t.Fatalf("a scheduled run for an uninstalled analyst = %v", err)
	}
	all, err := p.Alerts.Alerts(tenant)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(all, func(a alerts.Alert) bool {
		return strings.Contains(a.Title, "weekly-recap could not start: @humagen-analyst is not installed: install the Core package from the Store")
	}) {
		t.Fatalf("no alert says what to install: %+v", all)
	}
	if why := p.NotInstalled(t.Context(), tenant, "warp-analyst"); !strings.Contains(why, "Core package") {
		t.Fatalf("an old handle = %q", why)
	}
	if why := p.NotInstalled(t.Context(), tenant, platform.BuiltinApprover); why != "" {
		t.Fatalf("the approval agent = %q", why)
	}
	// An eval of one says the same, and starts nothing.
	suite := evals.Suite{Name: "writer-checks", Target: evals.Agent, Subject: platform.BuiltinWriter, Cases: []evals.Case{{ID: "c1", Input: "hello", Checks: []evals.Check{{Kind: evals.Contains, Value: "offline"}}}}}
	if err := p.Evals.Put(tenant, suite, false); err != nil {
		t.Fatal(err)
	}
	if _, err := p.StartEval(t.Context(), tenant, suite.Name, "ada"); !errors.Is(err, wire.ErrNotFound) || !strings.Contains(err.Error(), "install the Core package from the Store") {
		t.Fatalf("an eval of an uninstalled writer = %v", err)
	}
	install(t, p, tenant, "humagen/core")
	if why := p.NotInstalled(t.Context(), tenant, platform.BuiltinWriter); why != "" {
		t.Fatalf("an installed writer = %q", why)
	}
}

// A message that mentions one of the store's agents the workspace has not
// installed says which package to install, where a name nobody goes by
// otherwise names no one. The agent tool says the same of a call to one, and
// both work once the package is installed.
func TestAMentionOrACallOfAnUninstalledStoreAgentSaysWhatToInstall(t *testing.T) {
	p := shipping(t, platform.Config{})
	const hint = "@humagen-writer is not installed: install the Core package from the Store"
	first := startThread(t, p, operator, platform.ThreadInput{Text: "@humagen-writer draft the launch plan, with @nobody-here"})
	var told, noOne bool
	for _, mn := range first.Refused {
		told = told || mn.Name == platform.BuiltinWriter && strings.Contains(mn.Refused, hint)
		noOne = noOne || mn.Name == "nobody-here" && mn.Refused == threads.NoOne("nobody-here").Refused
	}
	if !told || !noOne {
		t.Fatalf("the mentions came to %+v", first.Refused)
	}
	for _, r := range first.Runs {
		if r.Agent == platform.BuiltinWriter {
			t.Fatalf("an uninstalled writer answered: %+v", r)
		}
	}

	asked, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "api-entry", Agent: platform.BuiltinEntry, Text: "Plan the launch"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Call(t.Context(), tenant, asked.ID, call.Request{Key: "writer", Agent: platform.BuiltinWriter, Text: "Draft the plan", Mode: call.Subagent})
	if !errors.Is(err, platform.ErrNotAvailable) || !strings.Contains(err.Error(), hint) {
		t.Fatalf("a call to an uninstalled writer = %v", err)
	}

	install(t, p, tenant, "humagen/core")
	again := startThread(t, p, operator, platform.ThreadInput{Text: "@humagen-writer draft the launch plan"})
	for _, mn := range again.Refused {
		if mn.Name == platform.BuiltinWriter {
			t.Fatalf("an installed writer was refused: %+v", mn)
		}
	}
	if _, err := p.Call(t.Context(), tenant, asked.ID, call.Request{Key: "writer", Agent: platform.BuiltinWriter, Text: "Draft the plan", Mode: call.Subagent}); err != nil && strings.Contains(err.Error(), "not installed") {
		t.Fatalf("a call to an installed writer = %v", err)
	}
}
