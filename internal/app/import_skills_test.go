package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/library"
)

// depot is a skill folder whose fact is only in its supporting file.
func depot(t *testing.T, fact string) string {
	t.Helper()
	dir := t.TempDir()
	putFile(t, dir, "depot/SKILL.md", fmt.Sprintf(skillMD, "depot", "depot", "depot"))
	putFile(t, dir, "depot/references/facts.md", fact)
	return dir
}

// revisions is every revision the library holds of a personal skill.
func (s *importServer) revisions(t *testing.T, name string) []library.Revision {
	t.Helper()
	rows, err := s.lib.History(importTenant, importScope("personal"), name)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// publish tests and publishes a revision, as a person does in the console.
func (s *importServer) publish(t *testing.T, name string, version int) {
	t.Helper()
	scope := importScope("personal")
	if err := s.lib.Tested(importTenant, scope, name, version, "run-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.lib.Publish(importTenant, importPerson, scope, name, version, time.Now()); err != nil {
		t.Fatal(err)
	}
}

// edit saves a revision in the editor: the newest one with one file changed.
func (s *importServer) edit(t *testing.T, name, path, body string) {
	t.Helper()
	rows := s.revisions(t, name)
	d := rows[len(rows)-1].Definition
	for i, f := range d.Files {
		if f.Path == path {
			d.Files[i].Data = []byte(body)
		}
	}
	if _, err := s.lib.Draft(importTenant, importPerson, importScope("personal"), d, len(rows), nil, time.Now()); err != nil {
		t.Fatal(err)
	}
}

func fileOf(r library.Revision, path string) string {
	for _, f := range r.Files {
		if f.Path == path {
			return string(f.Data)
		}
	}
	return "<none>"
}

func skillsJSON(t *testing.T, args ...string) (skillsResult, error) {
	t.Helper()
	out, err := runImport(t, append([]string{"skills", "--json"}, args...)...)
	var res skillsResult
	if jerr := json.Unmarshal([]byte(out), &res); jerr != nil {
		t.Fatalf("json = %v, err = %v\n%s", jerr, err, out)
	}
	return res, err
}

// A folder that changed since it was imported is drafted again: a new
// revision, not published, beside the published one, which stays as it was.
// A changed supporting file alone, a renamed file and a deleted file each
// count, and the same folder again writes nothing.
func TestImportSkillsDraftsAChangedFolderAndLeavesThePublishedRevision(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := depot(t, "The code word is HERON.")
	if res, err := skillsJSON(t, dir, "--server", server.URL); err != nil || strings.Join(res.Added, ",") != "depot" {
		t.Fatalf("first import = %+v, %v", res, err)
	}
	api.publish(t, "depot", 1)

	// Only the supporting file changes.
	putFile(t, dir, "depot/references/facts.md", "The code word is OSPREY.")
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	for _, want := range []string{
		"Added 0 skills and updated 1 as drafts in the personal library; 0 already there, 0 in conflict, 0 failed.",
		"updated depot: draft v2 (changed references/facts.md); v1 stays published",
		"Next: test and publish each draft",
	} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("err = %v, out lacks %q:\n%s", err, want, out)
		}
	}
	rows := api.revisions(t, "depot")
	if len(rows) != 2 || rows[0].PublishedAt.IsZero() || fileOf(rows[0], "references/facts.md") != "The code word is HERON." ||
		!rows[1].PublishedAt.IsZero() || len(rows[1].Tests) != 0 || rows[1].Via != library.ViaImport || fileOf(rows[1], "references/facts.md") != "The code word is OSPREY." {
		t.Fatalf("revisions = %+v", rows)
	}
	if got, ok, _ := api.lib.Published(importTenant, importScope("personal"), "depot"); !ok || got.Version != 1 {
		t.Fatalf("published = v%d, %v", got.Version, ok)
	}

	// The same folder again is present, and nothing is written.
	writes := api.writes
	res, err := skillsJSON(t, dir, "--server", server.URL)
	if err != nil || strings.Join(res.Present, ",") != "depot" || len(res.Updated)+len(res.Added) != 0 || api.writes != writes || len(api.revisions(t, "depot")) != 2 {
		t.Fatalf("replay = %+v, %v, %d writes", res, err, api.writes-writes)
	}

	// A renamed file: the newest revision is an unpublished draft that
	// differs, so another draft follows it.
	if err := os.Rename(filepath.Join(dir, "depot/references/facts.md"), filepath.Join(dir, "depot/references/sheet.md")); err != nil {
		t.Fatal(err)
	}
	res, err = skillsJSON(t, dir, "--server", server.URL)
	if err != nil || len(res.Updated) != 1 || res.Updated[0].Version != 3 || res.Updated[0].Published != 1 ||
		strings.Join(res.Updated[0].Added, ",") != "references/sheet.md" || strings.Join(res.Updated[0].Removed, ",") != "references/facts.md" || len(res.Updated[0].Changed) != 0 {
		t.Fatalf("rename = %+v, %v", res, err)
	}
	// A deleted file.
	if err := os.Remove(filepath.Join(dir, "depot/references/sheet.md")); err != nil {
		t.Fatal(err)
	}
	res, err = skillsJSON(t, dir, "--server", server.URL)
	if err != nil || len(res.Updated) != 1 || res.Updated[0].Version != 4 || strings.Join(res.Updated[0].Removed, ",") != "references/sheet.md" || len(res.Updated[0].Added)+len(res.Updated[0].Changed) != 0 {
		t.Fatalf("delete = %+v, %v", res, err)
	}
	rows = api.revisions(t, "depot")
	if len(rows) != 4 || len(rows[3].Files) != 1 || fileOf(rows[2], "references/sheet.md") != "The code word is OSPREY." || fileOf(rows[0], "references/facts.md") != "The code word is HERON." {
		t.Fatalf("revisions = %+v", rows)
	}
	for _, r := range rows[1:] {
		if !r.PublishedAt.IsZero() || r.Via != library.ViaImport {
			t.Errorf("v%d: published %v, via %q", r.Version, r.PublishedAt, r.Via)
		}
	}
	if got, ok, _ := api.lib.Published(importTenant, importScope("personal"), "depot"); !ok || got.Version != 1 {
		t.Fatalf("published = v%d, %v", got.Version, ok)
	}
}

// A dry run says what an import would do, from what the library holds, and
// writes nothing.
func TestImportSkillsDryRunSaysWhatWouldChangeAndWritesNothing(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := depot(t, "The code word is HERON.")
	if out, err := runImport(t, "skills", dir, "--server", server.URL, "--dry-run"); err != nil || !strings.Contains(out, "Would add 1 skills and update 0 as drafts in the personal library; 0 already there, 0 in conflict, 0 failed.") || api.writes != 0 {
		t.Fatalf("err = %v, %d writes\n%s", err, api.writes, out)
	}
	if _, err := runImport(t, "skills", dir, "--server", server.URL); err != nil {
		t.Fatal(err)
	}
	putFile(t, dir, "depot/references/facts.md", "The code word is OSPREY.")
	putFile(t, dir, "tidy/SKILL.md", fmt.Sprintf(skillMD, "tidy", "tidy", "tidy"))
	writes := api.writes
	out, err := runImport(t, "skills", dir, "--server", server.URL, "--dry-run")
	for _, want := range []string{
		"Would add 1 skills and update 1 as drafts in the personal library; 0 already there, 0 in conflict, 0 failed.",
		"updated depot: draft v2 (changed references/facts.md); no revision is published yet",
	} {
		if err != nil || !strings.Contains(out, want) {
			t.Fatalf("err = %v, out lacks %q:\n%s", err, want, out)
		}
	}
	if strings.Contains(out, "Next:") || api.writes != writes || len(api.revisions(t, "depot")) != 1 {
		t.Fatalf("a dry run wrote: %d writes\n%s", api.writes-writes, out)
	}
	res, err := skillsJSON(t, dir, "--server", server.URL, "--dry-run")
	if err != nil || !res.DryRun || len(res.Updated) != 1 || strings.Join(res.Added, ",") != "tidy" || api.writes != writes {
		t.Fatalf("res = %+v, %v", res, err)
	}
}

// A skill someone edited in the library since it was imported is not
// written over: the import reports a conflict and writes nothing, whether
// or not the folder changed too, until --force drafts the folder after the
// edit. A skill an earlier release imported carries no record of it, and is
// treated the same.
func TestImportSkillsReportsAConflictWithAnEditMadeInTheLibrary(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := depot(t, "The code word is HERON.")
	if _, err := runImport(t, "skills", dir, "--server", server.URL); err != nil {
		t.Fatal(err)
	}
	api.publish(t, "depot", 1)
	api.edit(t, "depot", "references/facts.md", "The code word is KESTREL.")
	writes := api.writes

	// The folder is as it was imported: the library has moved on.
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err == nil || err.Error() != "1 skills were not drafted" ||
		!strings.Contains(out, "Added 0 skills and updated 0 as drafts in the personal library; 0 already there, 1 in conflict, 0 failed.") ||
		!strings.Contains(out, "conflict depot: this folder holds v1, and the library has since moved on to v2, which no import wrote; nothing was written.") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	// The folder changed too.
	putFile(t, dir, "depot/references/facts.md", "The code word is OSPREY.")
	res, err := skillsJSON(t, dir, "--server", server.URL)
	if err == nil || len(res.Conflicts) != 1 || res.Conflicts[0].Name != "depot" || len(res.Updated)+len(res.Added)+len(res.Present) != 0 ||
		!strings.Contains(res.Conflicts[0].Reason, "v2, was not written by an import, so it may hold edits this folder lacks") || !strings.Contains(res.Conflicts[0].Reason, "--force") {
		t.Fatalf("res = %+v, %v", res, err)
	}
	if rows := api.revisions(t, "depot"); api.writes != writes || len(rows) != 2 || fileOf(rows[1], "references/facts.md") != "The code word is KESTREL." {
		t.Fatalf("a conflict wrote: %d writes, %+v", api.writes-writes, rows)
	}
	// A dry run says the same.
	if res, err := skillsJSON(t, dir, "--server", server.URL, "--dry-run"); err == nil || len(res.Conflicts) != 1 {
		t.Fatalf("dry run = %+v, %v", res, err)
	}

	// --force drafts the folder after the edit, which stays in the history,
	// and the next change needs no --force.
	res, err = skillsJSON(t, dir, "--server", server.URL, "--force")
	if err != nil || len(res.Updated) != 1 || res.Updated[0].Version != 3 || res.Updated[0].Published != 1 {
		t.Fatalf("forced = %+v, %v", res, err)
	}
	rows := api.revisions(t, "depot")
	if len(rows) != 3 || fileOf(rows[1], "references/facts.md") != "The code word is KESTREL." || fileOf(rows[2], "references/facts.md") != "The code word is OSPREY." || !rows[2].PublishedAt.IsZero() {
		t.Fatalf("revisions = %+v", rows)
	}
	putFile(t, dir, "depot/references/facts.md", "The code word is WREN.")
	if res, err := skillsJSON(t, dir, "--server", server.URL); err != nil || len(res.Updated) != 1 || res.Updated[0].Version != 4 {
		t.Fatalf("after the forced draft = %+v, %v", res, err)
	}

	// A skill held from before imports were recorded: the same folder is
	// present, a different one is a conflict.
	held := &importServer{}
	heldServer := held.start(t)
	old := depot(t, "The code word is HERON.")
	def, _, err := skillFolder(old, "depot")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := held.lib.Draft(importTenant, importPerson, importScope("personal"), def, 0, nil, time.Now()); err != nil {
		t.Fatal(err)
	}
	if res, err := skillsJSON(t, old, "--server", heldServer.URL); err != nil || strings.Join(res.Present, ",") != "depot" || held.writes != 0 {
		t.Fatalf("the same folder = %+v, %v", res, err)
	}
	putFile(t, old, "depot/references/facts.md", "The code word is OSPREY.")
	if res, err := skillsJSON(t, old, "--server", heldServer.URL); err == nil || len(res.Conflicts) != 1 || held.writes != 0 {
		t.Fatalf("a changed folder = %+v, %v", res, err)
	}
}

// Two imports at once never both draft. Of the same folder, one drafts and
// the other finds the skill present; of two different folders, one drafts
// and the other is told the library changed, and writes nothing over it.
func TestTwoSkillImportsAtOnceLeaveOneDraft(t *testing.T) {
	for _, tc := range []struct {
		name       string
		a, b       string
		seed       bool
		conflicted int
	}{
		{"the same changed folder", "OSPREY", "OSPREY", true, 0},
		{"two different changes", "OSPREY", "WREN", true, 1},
		{"the same new skill", "OSPREY", "OSPREY", false, 0},
		{"two different new skills of one name", "OSPREY", "WREN", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &importServer{}
			server := api.start(t)
			before := 0
			if tc.seed {
				if _, err := runImport(t, "skills", depot(t, "The code word is HERON."), "--server", server.URL); err != nil {
					t.Fatal(err)
				}
				api.publish(t, "depot", 1)
				before = 1
			}
			api.mu.Lock()
			api.skillGate = 2
			api.mu.Unlock()
			results := make([]skillsResult, 2)
			errs := make([]error, 2)
			var wg sync.WaitGroup
			for i, fact := range []string{tc.a, tc.b} {
				dir := depot(t, "The code word is "+fact+".")
				wg.Add(1)
				go func() {
					defer wg.Done()
					results[i], errs[i] = skillsJSON(t, dir, "--server", server.URL)
				}()
			}
			wg.Wait()
			rows := api.revisions(t, "depot")
			if len(rows) != before+1 || api.drafts != before+1 {
				t.Fatalf("%d revisions, %d drafts taken; results %+v", len(rows), api.drafts, results)
			}
			wrote, present, conflicts, failed := 0, 0, 0, 0
			for i, res := range results {
				wrote += len(res.Added) + len(res.Updated)
				present += len(res.Present)
				conflicts += len(res.Conflicts)
				failed += len(res.Failed)
				if (errs[i] != nil) != (len(res.Conflicts) > 0) {
					t.Errorf("importer %d: err = %v, res = %+v", i, errs[i], res)
				}
				// The winner's files are the ones kept, whole.
				if len(res.Added)+len(res.Updated) == 1 {
					want := "The code word is " + []string{tc.a, tc.b}[i] + "."
					if got := fileOf(rows[len(rows)-1], "references/facts.md"); got != want {
						t.Errorf("importer %d drafted %q and the library holds %q", i, want, got)
					}
				}
			}
			if wrote != 1 || conflicts != tc.conflicted || present != 1-tc.conflicted || failed != 0 {
				t.Fatalf("wrote %d, present %d, conflicts %d, failed %d: %+v", wrote, present, conflicts, failed, results)
			}
			if tc.seed {
				if got, ok, _ := api.lib.Published(importTenant, importScope("personal"), "depot"); !ok || got.Version != 1 {
					t.Fatalf("published = v%d, %v", got.Version, ok)
				}
			}
		})
	}
}

// A skill holds a bounded number of revisions: a change past it fails, in
// words that say why.
func TestImportSkillsSaysWhenASkillHoldsEveryRevisionItMay(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := depot(t, "fact 0")
	for i := range library.MaxVersions {
		putFile(t, dir, "depot/references/facts.md", fmt.Sprintf("fact %d", i))
		if _, err := runImport(t, "skills", dir, "--server", server.URL); err != nil {
			t.Fatal(err)
		}
	}
	putFile(t, dir, "depot/references/facts.md", "one more")
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err == nil || !strings.Contains(out, fmt.Sprintf("failed depot: the library holds as many revisions of it as it may (%d); nothing was written. A skill's revisions are never removed: to keep importing this folder, give it another name in its SKILL.md, or import it into another scope with --scope", library.MaxVersions)) || len(api.revisions(t, "depot")) != library.MaxVersions {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

// The JSON keeps drafted, the key it had before updates were reported, with
// what it held then: the skills first drafted by this import, never an
// updated one. It stands beside added, updated and conflicts.
func TestImportSkillsJSONKeepsDraftedForTheSkillsFirstDrafted(t *testing.T) {
	api := &importServer{}
	server := api.start(t)
	dir := depot(t, "The code word is HERON.")
	shape := func(wantDrafted string, wantUpdated, wantConflicts int, args ...string) {
		t.Helper()
		out, _ := runImport(t, append([]string{"skills", "--json", dir, "--server", server.URL}, args...)...)
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(out), &raw); err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		var drafted, added []string
		var updated, conflicts []map[string]any
		for key, into := range map[string]any{"drafted": &drafted, "added": &added, "updated": &updated, "conflicts": &conflicts} {
			if _, has := raw[key]; !has || string(raw[key]) == "null" {
				t.Fatalf("the JSON lacks %q as an array:\n%s", key, out)
			}
			if err := json.Unmarshal(raw[key], into); err != nil {
				t.Fatalf("%s: %v\n%s", key, err, out)
			}
		}
		if strings.Join(drafted, ",") != wantDrafted || strings.Join(added, ",") != wantDrafted || len(updated) != wantUpdated || len(conflicts) != wantConflicts {
			t.Fatalf("drafted %v, added %v, updated %v, conflicts %v; want drafted %q", drafted, added, updated, conflicts, wantDrafted)
		}
	}
	// As before this change: a new skill is drafted, and one there is not.
	shape("depot", 0, 0)
	shape("", 0, 0)
	// An updated skill is not in drafted, and neither is one in conflict.
	putFile(t, dir, "depot/references/facts.md", "The code word is OSPREY.")
	putFile(t, dir, "tidy/SKILL.md", fmt.Sprintf(skillMD, "tidy", "tidy", "tidy"))
	shape("tidy", 1, 0)
	api.edit(t, "depot", "references/facts.md", "The code word is KESTREL.")
	shape("", 0, 1)
}

// An import writes only after the head it read when it listed the library.
// One held after its list, while another import drafts, has lost a race
// when it resumes: it finds the skill present when the winner drafted what
// its own folder holds, a conflict otherwise, and in neither case drafts
// again. Reading the skill's revisions later never renews what it expects.
func TestAnImportHeldAfterItsListNeverDraftsOverAnotherImportsDraft(t *testing.T) {
	for _, tc := range []struct {
		name         string
		held, winner string
		seed         bool
	}{
		{"a different winner", "OSPREY", "WREN", true},
		{"a different winner, the other way round", "WREN", "OSPREY", true},
		{"the same winner", "OSPREY", "OSPREY", true},
		{"a new skill, a different winner", "OSPREY", "WREN", false},
		{"a new skill, the same winner", "OSPREY", "OSPREY", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &importServer{}
			server := api.start(t)
			before := 0
			if tc.seed {
				if _, err := runImport(t, "skills", depot(t, "The code word is HERON."), "--server", server.URL); err != nil {
					t.Fatal(err)
				}
				api.publish(t, "depot", 1)
				before = 1
			}
			api.mu.Lock()
			api.holdList, api.listReached, api.listRelease = true, make(chan struct{}), make(chan struct{})
			api.mu.Unlock()
			var held skillsResult
			var heldErr error
			done := make(chan struct{})
			heldDir, winnerDir := depot(t, "The code word is "+tc.held+"."), depot(t, "The code word is "+tc.winner+".")
			go func() {
				defer close(done)
				held, heldErr = skillsJSON(t, heldDir, "--server", server.URL)
			}()
			<-api.listReached
			// The held import has read the library. Another drafts.
			won, err := skillsJSON(t, winnerDir, "--server", server.URL)
			if err != nil || len(won.Added)+len(won.Updated) != 1 {
				t.Fatalf("the winner = %+v, %v", won, err)
			}
			close(api.listRelease)
			<-done

			rows := api.revisions(t, "depot")
			if len(rows) != before+1 || api.drafts != before+1 {
				t.Fatalf("%d revisions, %d drafts taken; the held import = %+v", len(rows), api.drafts, held)
			}
			if got := fileOf(rows[len(rows)-1], "references/facts.md"); got != "The code word is "+tc.winner+"." {
				t.Errorf("the newest revision holds %q", got)
			}
			if len(held.Added)+len(held.Updated)+len(held.Failed) != 0 {
				t.Fatalf("the held import wrote or failed: %+v", held)
			}
			if tc.held == tc.winner {
				if heldErr != nil || strings.Join(held.Present, ",") != "depot" || len(held.Conflicts) != 0 {
					t.Fatalf("the held import of the same folder = %+v, %v", held, heldErr)
				}
			} else if heldErr == nil || len(held.Conflicts) != 1 || held.Conflicts[0].Name != "depot" || !strings.Contains(held.Conflicts[0].Reason, "changed while this import ran") || len(held.Present) != 0 {
				t.Fatalf("the held import of a different folder = %+v, %v", held, heldErr)
			}
			if tc.seed {
				got, ok, _ := api.lib.Published(importTenant, importScope("personal"), "depot")
				if !ok || got.Version != 1 || fileOf(got, "references/facts.md") != "The code word is HERON." || len(rows[0].Tests) != 1 {
					t.Fatalf("published = v%d, %v", got.Version, ok)
				}
			}
		})
	}
}

// A dry run with no server to ask still says which folders are skills the
// library would take, and that what the library holds is unknown. An import
// that is not a dry run fails.
func TestImportSkillsDryRunWithoutAServerListsTheFoldersAsUnknown(t *testing.T) {
	gone := httptest.NewServer(http.NotFoundHandler())
	url := gone.URL
	gone.Close()
	dir := skillStore(t)
	out, err := runImport(t, "skills", dir, "--only", "review,ship,tidy", "--server", url, "--dry-run", "--json")
	if err != nil {
		t.Fatalf("err = %v\n%s", err, out)
	}
	var raw map[string]json.RawMessage
	if jerr := json.Unmarshal([]byte(out), &raw); jerr != nil {
		t.Fatalf("%v\n%s", jerr, out)
	}
	var res skillsResult
	_ = json.Unmarshal([]byte(out), &res)
	// drafted is what a dry run printed before it read the library: every
	// folder that is a skill.
	if strings.Join(res.Drafted, ",") != "review,ship,tidy" || strings.Join(res.Unknown, ",") != "review,ship,tidy" || !res.DryRun || res.Unread == "" ||
		string(raw["added"]) != "[]" || string(raw["updated"]) != "[]" || string(raw["present"]) != "[]" || string(raw["conflicts"]) != "[]" || string(raw["failed"]) != "[]" {
		t.Fatalf("offline dry run = %s", out)
	}
	out, err = runImport(t, "skills", dir, "--server", url, "--dry-run")
	for _, want := range []string{
		"Would import 3 skills into the personal library; 2 failed.",
		"The library could not be read, so which of them would be added, updated, already there or in conflict is unknown:",
		"unknown: review, ship, tidy",
		"failed big: data.bin is over 1048576 bytes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("out lacks %q:\n%s", want, out)
		}
	}
	if err == nil || err.Error() != "2 skills were not drafted" {
		t.Errorf("err = %v", err)
	}
	// With a server, a dry run says what it would do, and unknown is empty.
	api := &importServer{}
	server := api.start(t)
	res, err = skillsJSON(t, dir, "--only", "ship", "--server", server.URL, "--dry-run")
	if err != nil || strings.Join(res.Added, ",") != "ship" || strings.Join(res.Drafted, ",") != "ship" || len(res.Unknown) != 0 || res.Unread != "" {
		t.Fatalf("online dry run = %+v, %v", res, err)
	}
	// Not a dry run: no server is an error, and nothing is reported done.
	if out, err := runImport(t, "skills", dir, "--only", "ship", "--server", url); err == nil || !strings.Contains(err.Error(), "is the server running?") || strings.Contains(out, "Added") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if out, err := runImport(t, "skills", dir, "--only", "ship", "--server", url, "--json"); err == nil || out != "" {
		t.Fatalf("err = %v\n%s", err, out)
	}
}
