package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/library"
)

// A draft past a full skill is refused with 400 and the reason: the bound
// and the way on, so an API or console caller reads why.
func TestADraftPastAFullSkillSaysWhy(t *testing.T) {
	f := newFixture(t)
	admin := f.machineAs(t, "svc-admin", "acme", everything...)
	draft := func(version int) (int, map[string]any) {
		t.Helper()
		body := map[string]any{"scope": map[string]string{"kind": "workspace"}, "definition": packaged("capped", fmt.Sprint("fact ", version)), "version": version}
		return status(t, f, admin, "POST", "/api/library/skills", mustJSON(t, body))
	}
	for v := range library.MaxVersions {
		if code, out := draft(v); code != http.StatusCreated {
			t.Fatalf("revision %d = %d %v", v+1, code, out)
		}
	}
	code, out := draft(library.MaxVersions)
	if msg, _ := out["error"].(string); code != http.StatusBadRequest || !strings.Contains(msg, fmt.Sprintf("capped holds as many revisions as a skill may (%d); a skill's revisions are never removed, so save this one as another skill, under another name or in another scope", library.MaxVersions)) {
		t.Fatalf("revision %d = %d %v", library.MaxVersions+1, code, out)
	}
}
