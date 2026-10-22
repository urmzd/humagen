package httpapi

import (
	"fmt"
	"net/http"
	"slices"
	"testing"

	"github.com/urmzd/warp/internal/platform"
	"github.com/urmzd/warp/pkg/inbox"
)

// subjects lists the subjects of the messages in an inbox answer.
func subjects(out map[string]any) []string {
	var got []string
	for _, m := range out["messages"].([]any) {
		got = append(got, m.(map[string]any)["subject"].(string))
	}
	return got
}

func counts(t *testing.T, f *fixture, tok string) (open, updates float64) {
	t.Helper()
	c := want(t, f, tok, "GET", "/api/inbox/count", "", http.StatusOK)
	return c["open"].(float64), c["updates"].(float64)
}

// acmeNews is a tree with news in it: Field, open to everyone as members, and
// Globex under it, which everyone views and Cy is a member of.
func acmeNews(t *testing.T) (f *fixture, ana, bo, cy string) {
	t.Helper()
	f = newFixture(t)
	ana = f.machineAs(t, "ana", "acme", operator...)
	bo = f.machineAs(t, "bo", "acme", operator...)
	cy = f.machineAs(t, "cy", "acme", operator...)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Field"}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/projects", `{"name":"Globex","parent":"Field","members":[{"who":"*","role":"viewer"},{"who":"cy","role":"member"}]}`, http.StatusCreated)
	return f, ana, bo, cy
}

// An announcement reaches the members of its project and of every project
// below it, decided when it is read. A viewer reads it in the project's own
// inbox, and is not sent it.
func TestAnAnnouncementReachesTheMembersOfItsProjectAndBelow(t *testing.T) {
	f, ana, bo, cy := acmeNews(t)
	want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/projects/Globex/announcements", `{"subject":"Renewal signed","text":"Three more years.","severity":"warning"}`, http.StatusCreated)

	for who, wanted := range map[string][]string{ana: {"Renewal signed", "Field offsite"}, bo: {"Field offsite"}, cy: {"Renewal signed", "Field offsite"}} {
		if got := subjects(want(t, f, who, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, wanted) {
			t.Errorf("an inbox = %q, want %q", got, wanted)
		}
	}
	if got := subjects(want(t, f, bo, "GET", "/api/projects/Globex/inbox", "", http.StatusOK)); !slices.Equal(got, []string{"Renewal signed"}) {
		t.Errorf("Globex's inbox, read by a viewer = %q", got)
	}
	if got := subjects(want(t, f, bo, "GET", "/api/projects/Field/inbox?children=true", "", http.StatusOK)); !slices.Equal(got, []string{"Renewal signed", "Field offsite"}) {
		t.Errorf("Field's inbox with what is below it = %q", got)
	}
	// Revoking a membership hides what was posted at the next read.
	want(t, f, ana, "PUT", "/api/projects/Globex/members", `{"version":1,"members":[{"who":"*","role":"viewer"}]}`, http.StatusOK)
	if got := subjects(want(t, f, cy, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{"Field offsite"}) {
		t.Errorf("after leaving Globex = %q", got)
	}
}

// Announcements count only in updates, so the badge never grows from them,
// and reading one counts it out.
func TestAnnouncementsNeverGrowTheBadge(t *testing.T) {
	f, ana, bo, _ := acmeNews(t)
	open, updates := counts(t, f, bo)
	m := want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	if o, u := counts(t, f, bo); o != open || u != updates+1 {
		t.Fatalf("after one announcement: open %v (was %v), updates %v (was %v)", o, open, u, updates)
	}
	want(t, f, bo, "POST", "/api/inbox/"+m["id"].(string)+"/read", `{}`, http.StatusOK)
	if _, u := counts(t, f, bo); u != updates {
		t.Fatalf("after reading it, updates = %v", u)
	}
}

// Hiding an announcement hides the reader's own copy and nothing others see,
// and needs only to read projects.
func TestHidingAnAnnouncementHidesOnlyTheReadersCopy(t *testing.T) {
	f, ana, bo, cy := acmeNews(t)
	m := want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	reader := f.machineAs(t, "dee", "acme", "warp:runs:read", "warp:projects:read")
	if out := want(t, f, reader, "POST", "/api/inbox/"+m["id"].(string)+"/dismiss", `{}`, http.StatusOK); out["state"] != string(inbox.Dismissed) {
		t.Fatalf("hidden for its reader = %v", out)
	}
	if got := subjects(want(t, f, reader, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("the reader who hid it sees %q", got)
	}
	for _, who := range []string{ana, bo, cy} {
		if got := subjects(want(t, f, who, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{"Field offsite"}) {
			t.Errorf("another reader sees %q", got)
		}
	}
	// Nor is another reader told who hid their copy, or who read it.
	id := m["id"].(string)
	want(t, f, reader, "POST", "/api/inbox/"+id+"/read", `{}`, http.StatusOK)
	for _, who := range []string{ana, bo} {
		for _, c := range want(t, f, who, "GET", "/api/inbox/"+id, "", http.StatusOK)["history"].([]any) {
			if by := c.(map[string]any)["by"]; by == "dee" {
				t.Errorf("another reader is shown %v", c)
			}
		}
	}
	mine := want(t, f, reader, "GET", "/api/inbox/"+id, "", http.StatusOK)["history"].([]any)
	if last := mine[len(mine)-1].(map[string]any); last["what"] != "read" || last["by"] != "dee" {
		t.Fatalf("the reader's own history ends %v", last)
	}
}

// A muted project's news stays out of the person's own inbox, never out of
// the project's, and the workspace is never muted.
func TestMutingKeepsAProjectsNewsOutOfOnesOwnInboxOnly(t *testing.T) {
	f, ana, bo, _ := acmeNews(t)
	want(t, f, ana, "POST", "/api/projects/~/announcements", `{"subject":"Holiday","text":"Friday off."}`, http.StatusForbidden)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "POST", "/api/projects/~/announcements", `{"subject":"Holiday","text":"Friday off."}`, http.StatusCreated)
	want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	prefs := want(t, f, bo, "PUT", "/api/preferences", `{"theme":"system","accent":"teal","muted":["Field","Field"]}`, http.StatusOK)
	if fmt.Sprint(prefs["muted"]) != "[Field]" {
		t.Fatalf("muted = %v", prefs["muted"])
	}
	want(t, f, bo, "PUT", "/api/preferences", `{"theme":"system","accent":"teal","muted":["~"]}`, http.StatusBadRequest)
	if got := subjects(want(t, f, bo, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{"Holiday"}) {
		t.Errorf("a muted inbox = %q", got)
	}
	if got := subjects(want(t, f, bo, "GET", "/api/projects/Field/inbox", "", http.StatusOK)); !slices.Equal(got, []string{"Field offsite"}) {
		t.Errorf("the muted project's own inbox = %q", got)
	}
}

// Who may announce is the project's to say, owners by default, and an
// announcement is withdrawn by its author or an owner, for everyone.
func TestWhoAnnouncesAndWhoWithdraws(t *testing.T) {
	f, ana, bo, cy := acmeNews(t)
	if out := want(t, f, bo, "POST", "/api/projects/Field/announcements", `{"subject":"Mine","text":"Hello."}`, http.StatusForbidden); out["error"] != "you need to own Field" {
		t.Fatalf("a member announcing by default = %v", out)
	}
	field := want(t, f, ana, "GET", "/api/projects/Field", "", http.StatusOK)["project"].(map[string]any)
	if !slices.Contains(viewActions(field), "announce") {
		t.Fatalf("the owner may %v", viewActions(field))
	}
	want(t, f, ana, "PUT", "/api/projects/Field", `{"version":1,"announce":"member"}`, http.StatusOK)
	mine := want(t, f, bo, "POST", "/api/projects/Field/announcements", `{"subject":"Mine","text":"Hello."}`, http.StatusCreated)
	theirs := want(t, f, bo, "POST", "/api/projects/Field/announcements", `{"subject":"Also mine","text":"Hello again."}`, http.StatusCreated)
	if out := want(t, f, cy, "DELETE", "/api/projects/Field/announcements/"+mine["id"].(string), "", http.StatusForbidden); out["error"] != "its author withdraws it, or you need to own Field" {
		t.Fatalf("someone else withdrawing = %v", out)
	}
	want(t, f, bo, "DELETE", "/api/projects/Field/announcements/"+mine["id"].(string), "", http.StatusNoContent)
	want(t, f, ana, "DELETE", "/api/projects/Field/announcements/"+theirs["id"].(string), "", http.StatusNoContent)
	if got := subjects(want(t, f, cy, "GET", "/api/projects/Field/inbox", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("after both were withdrawn = %q", got)
	}
	history := want(t, f, cy, "GET", "/api/inbox/"+mine["id"].(string), "", http.StatusOK)["history"].([]any)
	if last := history[len(history)-1].(map[string]any); last["what"] != "withdrawn" || last["by"] != "bo" {
		t.Fatalf("the withdrawal = %v", last)
	}
	want(t, f, ana, "DELETE", "/api/projects/Globex/announcements/"+theirs["id"].(string), "", http.StatusNotFound)
}

// Whatever a project's inbox shows a reader they may act on: a viewer who is
// not a member, and a member who muted the project, read, snooze and hide its
// announcements there, for themselves alone, and are still not sent them.
func TestAProjectsInboxIsReadByEveryoneWhoSeesIt(t *testing.T) {
	f, ana, bo, cy := acmeNews(t)
	globex := want(t, f, ana, "POST", "/api/projects/Globex/announcements", `{"subject":"Renewal signed","text":"Three more years."}`, http.StatusCreated)
	field := want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	want(t, f, cy, "PUT", "/api/preferences", `{"theme":"system","accent":"teal","muted":["Field"]}`, http.StatusOK)

	for _, tc := range []struct {
		who, reader, project string
		m                    map[string]any
	}{{bo, "a viewer of Globex", "Globex", globex}, {cy, "a member who muted Field", "Field", field}} {
		id := tc.m["id"].(string)
		own := "/api/projects/" + tc.project + "/inbox"
		if got := subjects(want(t, f, tc.who, "GET", own, "", http.StatusOK)); !slices.Contains(got, tc.m["subject"].(string)) {
			t.Fatalf("%s reads %s's inbox as %q", tc.reader, tc.project, got)
		}
		if out := want(t, f, tc.who, "GET", "/api/inbox/"+id, "", http.StatusOK)["message"].(map[string]any); out["unread"] != true {
			t.Fatalf("%s opening it = %v", tc.reader, out)
		}
		if out := want(t, f, tc.who, "POST", "/api/inbox/"+id+"/read", `{}`, http.StatusOK); out["unread"] != false {
			t.Fatalf("%s reading it = %v", tc.reader, out)
		}
		if out := want(t, f, tc.who, "POST", "/api/inbox/"+id+"/snooze", `{"until":"2999-01-01T00:00:00Z"}`, http.StatusOK); out["state"] != string(inbox.Snoozed) {
			t.Fatalf("%s snoozing it = %v", tc.reader, out)
		}
		if out := want(t, f, tc.who, "POST", "/api/inbox/"+id+"/dismiss", `{}`, http.StatusOK); out["state"] != string(inbox.Dismissed) {
			t.Fatalf("%s hiding it = %v", tc.reader, out)
		}
		if got := subjects(want(t, f, tc.who, "GET", own, "", http.StatusOK)); slices.Contains(got, tc.m["subject"].(string)) {
			t.Fatalf("%s still reads %q after hiding it", tc.reader, got)
		}
		if got := subjects(want(t, f, tc.who, "GET", "/api/inbox?kind=announcement&state=all", "", http.StatusOK)); slices.Contains(got, tc.m["subject"].(string)) {
			t.Fatalf("%s is sent %q", tc.reader, got)
		}
	}
	// Each hid their own copy, and nobody else's.
	if got := subjects(want(t, f, ana, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{"Field offsite", "Renewal signed"}) {
		t.Fatalf("the author's inbox = %q", got)
	}
	// The project's inbox is no way into another's: a message not addressed
	// to a reader is still not found.
	want(t, f, bo, "GET", "/api/inbox/m-404", "", http.StatusNotFound)
}

// A withdrawal is everyone's, even when the one withdrawing hid the
// announcement for themselves first.
func TestWithdrawingAnAnnouncementOneHidWithdrawsItForEveryone(t *testing.T) {
	f, ana, bo, _ := acmeNews(t)
	m := want(t, f, ana, "POST", "/api/projects/Field/announcements", `{"subject":"Field offsite","text":"Lisbon, in May."}`, http.StatusCreated)
	id := m["id"].(string)
	want(t, f, ana, "POST", "/api/inbox/"+id+"/dismiss", `{}`, http.StatusOK)
	want(t, f, ana, "DELETE", "/api/projects/Field/announcements/"+id, "", http.StatusNoContent)
	if got := subjects(want(t, f, bo, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("another reader still sees %q", got)
	}
	history := want(t, f, bo, "GET", "/api/inbox/"+id, "", http.StatusOK)["history"].([]any)
	if last := history[len(history)-1].(map[string]any); last["what"] != "withdrawn" || last["by"] != "ana" {
		t.Fatalf("the withdrawal = %v", last)
	}
	// Withdrawing it again changes nothing.
	want(t, f, ana, "DELETE", "/api/projects/Field/announcements/"+id, "", http.StatusNoContent)
}

// A release's notes are an announcement in the workspace's own inbox, from
// warp: everyone who reads the workspace is sent it, and finds it in What's
// new and in the workspace's inbox, each hides their own copy, an owner of
// the workspace withdraws it for everyone, and it is posted once.
func TestReleaseNotesReachEveryoneWhoReadsTheWorkspace(t *testing.T) {
	f, ana, bo, cy := acmeNews(t)
	notes := platform.Announcement{Subject: "What's new in warp 0.16.0", Text: "- projects: release notes in the inbox"}
	m, posted, err := f.p.PostReleaseNotes("acme", "0.16.0", notes)
	if err != nil || !posted || m.Source.Project != "" || m.From != (inbox.From{Kind: "system", Name: "warp"}) || m.Key != "release:0.16.0" {
		t.Fatalf("posted = %+v, %v, %v", m, posted, err)
	}
	for _, who := range []string{ana, bo, cy} {
		if got := subjects(want(t, f, who, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{notes.Subject}) {
			t.Errorf("what's new = %q", got)
		}
		if got := subjects(want(t, f, who, "GET", "/api/projects/~/inbox", "", http.StatusOK)); !slices.Equal(got, []string{notes.Subject}) {
			t.Errorf("the workspace's inbox = %q", got)
		}
	}
	out := want(t, f, bo, "GET", "/api/inbox/"+m.ID, "", http.StatusOK)["message"].(map[string]any)
	if from := out["from"].(map[string]any); from["kind"] != "system" || from["name"] != "warp" || out["kind"] != "announcement" {
		t.Fatalf("as read = %v", out)
	}
	if again, posted, err := f.p.PostReleaseNotes("acme", "0.16.0", notes); err != nil || posted || again.ID != m.ID {
		t.Fatalf("posted again = %+v, %v, %v", again, posted, err)
	}
	other := f.machine(t, "globex", "warp:runs:read", "warp:projects:read")
	if got := subjects(want(t, f, other, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("another workspace is sent %q", got)
	}
	want(t, f, other, "GET", "/api/inbox/"+m.ID, "", http.StatusNotFound)

	// Hide for Me hides one reader's copy.
	want(t, f, bo, "POST", "/api/inbox/"+m.ID+"/dismiss", `{}`, http.StatusOK)
	if got := subjects(want(t, f, bo, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("the reader who hid it sees %q", got)
	}
	if got := subjects(want(t, f, cy, "GET", "/api/inbox?kind=announcement", "", http.StatusOK)); !slices.Equal(got, []string{notes.Subject}) {
		t.Fatalf("another reader sees %q", got)
	}
	// It is nobody's to withdraw but an owner's of the workspace, and once
	// withdrawn it is not posted again.
	want(t, f, ana, "DELETE", "/api/projects/~/announcements/"+m.ID, "", http.StatusForbidden)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	want(t, f, admin, "DELETE", "/api/projects/~/announcements/"+m.ID, "", http.StatusNoContent)
	if _, posted, err := f.p.PostReleaseNotes("acme", "0.16.0", notes); err != nil || posted {
		t.Fatalf("posted after its withdrawal: %v, %v", posted, err)
	}
	if got := subjects(want(t, f, cy, "GET", "/api/projects/~/inbox", "", http.StatusOK)); len(got) != 0 {
		t.Fatalf("after its withdrawal = %q", got)
	}
}
