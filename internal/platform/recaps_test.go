package platform_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/runtime"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/alerts"
	"github.com/urmzd/warp/pkg/artifacts"
	"github.com/urmzd/warp/pkg/call"
	"github.com/urmzd/warp/pkg/inbox"
	"github.com/urmzd/warp/pkg/memory"
	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/runs"
	"github.com/urmzd/warp/pkg/threads"
)

// project creates a project at the top of the tree.
func project(t *testing.T, p *platform.Platform, name string) {
	t.Helper()
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, platform.ProjectInput{Name: name}); err != nil {
		t.Fatal(err)
	}
}

// publishIn records a published file in a project, as publish_artifact does.
func publishIn(t *testing.T, p *platform.Platform, project, title, description string) {
	t.Helper()
	if _, err := p.Artifacts.Publish(tenant, artifacts.Artifact{
		ID: p.Artifacts.NextID(tenant), Key: "k-" + title, Thread: "th-" + project, Title: title, Kind: artifacts.Document,
		Agent: "lead", Project: project, Description: description, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
}

func scheduleIn(t *testing.T, p *platform.Platform, project, name string) {
	t.Helper()
	sc := cron.Schedule{
		Name: name, Expression: "41 8 * * 1", Agent: "lead", Text: "What changed?", Tenant: tenant,
		Meta: map[string]string{runs.MetaProject: project, "created_by": "ada"},
	}
	if err := p.Schedules.Add(sc, platform.About{CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
}

func TestRunsSummaryCountsOnlyTheProjectSubtree(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	project(t, p, "Other")
	field, _ := turn(t, p, "api-f1", "Field")
	turn(t, p, "api-f2", "Field")
	turn(t, p, "api-o1", "Other")
	turn(t, p, "api-r1", "")
	req := as(field, platform.BuiltinAnalyst)

	total := func(args map[string]any) int {
		t.Helper()
		out, err := use(t, p, req, platform.ToolRunsSummary, args)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Total struct {
				Runs int `json:"runs"`
			} `json:"total"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return got.Total.Runs
	}
	if n := total(map[string]any{"project": "Field"}); n != 2 {
		t.Fatalf("Field counted %d runs, want its 2", n)
	}
	if n := total(map[string]any{"project": "Other"}); n != 1 {
		t.Fatalf("Other counted %d runs, want its 1", n)
	}
	// The root, named or not, is the whole workspace.
	for _, args := range []map[string]any{{"project": "~"}, {}} {
		if n := total(args); n != 4 {
			t.Fatalf("%v counted %d runs, want all 4", args, n)
		}
	}
	if _, err := use(t, p, req, platform.ToolRunsSummary, map[string]any{"project": "Ghost"}); !errors.Is(err, platform.ErrProjectNotFound) {
		t.Fatalf("a project the workspace does not have: err = %v", err)
	}
}

func TestProjectChangesReadsOnlyItsSubtree(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	project(t, p, "Other")
	// Ops is as open as Field; Globex, an account, is members only, so a
	// recap of Field, which everyone in Field reads, leaves it out.
	for _, in := range []platform.ProjectInput{{Name: "Ops", Parent: "Field"}, {Name: "Globex", Parent: "Field", Kind: projects.Account}} {
		if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "ada"}, in); err != nil {
			t.Fatal(err)
		}
	}
	asking, _ := turn(t, p, "api-f1", "Field")
	turn(t, p, "api-f2", "Field")
	turn(t, p, "api-o1", "Ops")
	turn(t, p, "api-g1", "Globex")
	// A private thread is its participants' alone, so no recap reads it.
	if _, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Field", Visibility: threads.Private, Title: "hushhush", Text: "between us", PostOnly: true}); err != nil {
		t.Fatal(err)
	}
	// A thread of people alone has had no turns and no agent.
	if _, err := p.StartThread(t.Context(), operator, platform.ThreadInput{Project: "Field", Visibility: threads.Shared, Title: "Site 1 handover checklist", Text: "Who signs it off?", PostOnly: true}); err != nil {
		t.Fatal(err)
	}
	turn(t, p, "api-o1", "Other")
	now := time.Now().UTC()
	for project, subject := range map[string]string{"Field": "Field moved to the new office", "Other": "Other news"} {
		if _, err := p.Inbox.Post(tenant, inbox.Message{
			Subject: subject, From: inbox.From{Kind: "person", Name: "ada"}, Source: inbox.Source{Project: project},
			Parts: []inbox.Part{{Type: inbox.PartText, Text: "x"}},
		}, now, inbox.PostCaps{}); err != nil {
			t.Fatal(err)
		}
	}
	for owner, notes := range map[string][]struct {
		text   string
		status memory.Status
	}{
		memory.ProjectOwner("Field"): {{"Field ships on Friday.", memory.Approved}, {"Field might slip a week.", memory.Pending}},
		memory.ProjectOwner("Other"): {{"Other renews in March.", memory.Approved}},
	} {
		for _, n := range notes {
			if _, err := p.Memory.Add(tenant, owner, n.text, memory.Person, n.status, "ada", now); err != nil {
				t.Fatal(err)
			}
		}
	}
	publishIn(t, p, "Field", "field-plan.md", "The plan for Field")
	publishIn(t, p, "Other", "renewal.md", "The renewal")
	scheduleIn(t, p, "Field", "field-recap")
	scheduleIn(t, p, "Other", "renewal-recap")

	out, err := use(t, p, as(asking, platform.BuiltinAnalyst), platform.ToolProjectChanges, map[string]any{"days": "7"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"What changed in Field and the projects below it", "Field ships on Friday.", "field-plan.md", "field-recap created by ada",
		"In all, 2 runs: 2 completed", "In Ops: 1 run: 1 completed", "Field #general: \"Write the report\" started, settled, 1 turn, agent lead\n",
		"Field #general: \"Site 1 handover checklist\" started, settled, 0 turns\n",
		"Ops #general", "\"Field moved to the new office\", from person ada", "Ops: created by ada",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the changes leave out %q:\n%s", want, out)
		}
	}
	// Nothing of another project, nothing still waiting for a person, and
	// not the run that asked.
	for _, never := range []string{"Other", "renewal", "might slip", "Globex", "hushhush"} {
		if strings.Contains(out, never) {
			t.Errorf("the changes carry %q:\n%s", never, out)
		}
	}
	for _, bad := range []string{"0", "32", "a week"} {
		if _, err := use(t, p, as(asking, platform.BuiltinAnalyst), platform.ToolProjectChanges, map[string]any{"days": bad}); err == nil {
			t.Errorf("days %q was taken", bad)
		}
	}
}

func TestProjectChangesRefusesARunWithNoProject(t *testing.T) {
	p := start(t, call.Limits{})
	run, _ := turn(t, p, "api-1", "")
	if _, err := use(t, p, as(run, platform.BuiltinAnalyst), platform.ToolProjectChanges, nil); !errors.Is(err, platform.ErrNoProject) {
		t.Fatalf("a run at the root: err = %v", err)
	}
}

func TestProjectChangesStaysWithin32KB(t *testing.T) {
	long := strings.Repeat("word ", 58)
	full := func(title string, n int) platform.ChangeSection {
		s := platform.ChangeSection{Title: title}
		for i := range n {
			s.Rows = append(s.Rows, fmt.Sprintf("%s %d %s", title, i, long))
		}
		return s
	}
	out := platform.RenderChanges("Field", 3, []platform.ChangeSection{full("Runs", 60), full("Files", 60), full("Notes", 60), full("Schedules", 60)})
	if len(out) > platform.MaxChangesBytes {
		t.Fatalf("the answer is %d bytes", len(out))
	}
	if !strings.Contains(out, "Runs 49 ") || strings.Contains(out, "Runs 50 ") || !strings.Contains(out, "(10 more)") {
		t.Fatalf("a section shows more than %d rows, or does not say how many it left out", platform.MaxChangeRows)
	}
	if !strings.Contains(out, "more rows not sent: this answer is at most 32 KB") {
		t.Fatalf("a cut answer does not say so:\n%s", out[len(out)-300:])
	}

	// A small answer is whole, with every section named.
	small := platform.RenderChanges("Field", 1, []platform.ChangeSection{full("Runs", 2), {Title: "Files"}})
	if !strings.Contains(small, "What changed in Field from") || strings.Contains(small, "below it") ||
		!strings.Contains(small, "Files:\n- nothing") || strings.Contains(small, "not sent") {
		t.Fatalf("a small answer = %s", small)
	}
}

func TestAnAnswerBecomesAnAnnouncement(t *testing.T) {
	long := strings.Repeat("a", 200)
	for _, c := range []struct {
		name, answer, subject, text string
		post                        bool
	}{
		{"headline then detail", "Field shipped 3 things\nThe renewal plan is done.\nTwo runs failed.", "Field shipped 3 things", "The renewal plan is done.\nTwo runs failed.", true},
		{"headline only", "Quiet week in Field", "Quiet week in Field", "", true},
		{"markdown heading", "## Weekly recap\n- one", "Weekly recap", "- one", true},
		{"none", "NONE", "", "", false},
		{"none, loosely", "  none. \n", "", "", false},
		{"none, spaced", "N O N E!", "", "", false},
		{"nothing", " \n\t ", "", "", false},
		{"not none", "None of the runs failed", "None of the runs failed", "", true},
		{"long subject", long + "\nrest", strings.Repeat("a", platform.MaxAnnouncementSubject), "rest", true},
		{"control characters", "Recap\x07 \u202eflip\r\nline\x00", "Recap flip", "line", true},
	} {
		subject, text, post := platform.AnnouncementOf(c.answer)
		if subject != c.subject || text != c.text || post != c.post {
			t.Errorf("%s: got (%q, %q, %v), want (%q, %q, %v)", c.name, subject, text, post, c.subject, c.text, c.post)
		}
	}
	_, text, _ := platform.AnnouncementOf("s\n" + strings.Repeat("é", platform.MaxAnnouncementText+10))
	if n := len([]rune(text)); n != platform.MaxAnnouncementText {
		t.Fatalf("the text kept %d characters", n)
	}
}

// A tool's answer reaches everyone who reads where it is posted, so the
// analyst reads a members-only project only for a run filed in it:
// runs_summary answers one named from outside as a project the workspace
// does not have, and leaves it out of the whole workspace, and list_alerts
// leaves out its rules and alerts.
func TestTheAnalystReadsAMembersOnlyProjectOnlyFromInsideIt(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	field, _ := turn(t, p, "api-f1", "Field")
	vault, _ := turn(t, p, "api-v1", "Vault")
	fromField, fromVault := as(field, platform.BuiltinAnalyst), as(vault, platform.BuiltinAnalyst)
	summary := func(req runtime.Request, args map[string]any) (int, string) {
		t.Helper()
		out, err := use(t, p, req, platform.ToolRunsSummary, args)
		if err != nil {
			t.Fatal(err)
		}
		var got struct {
			Total struct {
				Runs int `json:"runs"`
			} `json:"total"`
		}
		if err := json.Unmarshal([]byte(out), &got); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
		return got.Total.Runs, out
	}
	if _, err := use(t, p, fromField, platform.ToolRunsSummary, map[string]any{"project": "Vault"}); !errors.Is(err, platform.ErrProjectNotFound) {
		t.Fatalf("Vault named from Field: err = %v", err)
	}
	if n, out := summary(fromField, map[string]any{}); n != 1 || strings.Contains(out, vault.ID) {
		t.Fatalf("the workspace from Field counted %d runs: %s", n, out)
	}
	if n, _ := summary(fromVault, map[string]any{"project": "Vault"}); n != 1 {
		t.Fatalf("Vault from inside counted %d runs, want its 1", n)
	}
	if n, _ := summary(fromVault, map[string]any{}); n != 2 {
		t.Fatalf("the workspace from Vault counted %d runs, want 2", n)
	}

	for _, r := range []alerts.Rule{
		{Name: "vault-rule", Kind: alerts.RunFailed, Severity: alerts.Warning, Project: "Vault", CreatedBy: "olu"},
		{Name: "open-rule", Kind: alerts.RunFailed, Severity: alerts.Warning, CreatedBy: "ada"},
	} {
		if err := p.Alerts.AddRule(tenant, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := p.Alerts.Raise(tenant, "vault-alert", alerts.Alert{Kind: alerts.RunFailed, Severity: alerts.Warning, Project: "Vault", RunID: vault.ID, Title: "the vault plan failed", At: time.Now()}); err != nil {
		t.Fatal(err)
	}
	list := func(req runtime.Request) string {
		t.Helper()
		out, err := use(t, p, req, platform.ToolListAlerts, nil)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if out := list(fromField); !strings.Contains(out, "open-rule") || strings.Contains(out, "vault") {
		t.Fatalf("list_alerts from Field = %s", out)
	}
	if out := list(fromVault); !strings.Contains(out, "vault-rule") || !strings.Contains(out, "the vault plan failed") {
		t.Fatalf("list_alerts from Vault = %s", out)
	}
}

// A housekeeping run is counted where the turn it serves is: a summary read
// from outside a members-only project counts nothing a turn there started.
func TestAHousekeepingRunIsCountedWithTheTurnItServes(t *testing.T) {
	p := startWith(t, platform.Config{OfflineHooks: true})
	if err := p.SetSettings(t.Context(), tenant, platform.Settings{TitleAgent: "helper"}); err != nil {
		t.Fatal(err)
	}
	project(t, p, "Field")
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	field, _ := turn(t, p, "api-f1", "Field")
	vault, _ := turn(t, p, "api-v1", "Vault")
	eventually(t, "both turns were titled", func() bool {
		done := 0
		for _, r := range p.Runs.List(tenant, 50) {
			if r.Origin == runs.OriginSystem && r.Status == runs.Completed {
				done++
			}
		}
		return done == 2
	})
	out, err := use(t, p, as(field, platform.BuiltinAnalyst), platform.ToolRunsSummary, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Total struct {
			Runs int `json:"runs"`
		} `json:"total"`
	}
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	// Field's turn and the run that titled it.
	if got.Total.Runs != 2 || strings.Contains(out, vault.ID) {
		t.Fatalf("the workspace from Field counted %d runs: %s", got.Total.Runs, out)
	}
}

// A recap names an agent only when everyone it reaches may read it. An agent
// that moved from Field to a members-only project below it is left out of
// Field's row for the move, as Field's history leaves it out for them.
func TestARecapNamesOnlyTheAgentsEveryoneItReachesMayRead(t *testing.T) {
	p := start(t, call.Limits{})
	project(t, p, "Field")
	if _, err := p.CreateProject(t.Context(), tenant, projects.Writer{By: "olu"}, platform.ProjectInput{Name: "Vault", Parent: "Field", Kind: projects.Account}); err != nil {
		t.Fatal(err)
	}
	for _, home := range []string{"Field", "Vault"} {
		if err := p.SetAgentHome(t.Context(), tenant, "helper", home, govern); err != nil {
			t.Fatal(err)
		}
	}
	// lead comes to live in Field, which everyone the recap reaches reads.
	if err := p.SetAgentHome(t.Context(), tenant, "lead", "Field", govern); err != nil {
		t.Fatal(err)
	}
	asking, _ := turn(t, p, "api-f1", "Field")
	out, err := use(t, p, as(asking, platform.BuiltinAnalyst), platform.ToolProjectChanges, map[string]any{"days": "7"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Field: home by root\n") || !strings.Contains(out, "Field: home by root (lead)") || strings.Contains(out, "helper") {
		t.Fatalf("Field's recap of helper's move to Vault:\n%s", out)
	}
}
