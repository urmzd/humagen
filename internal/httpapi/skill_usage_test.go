package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestPublishedSkillUsageListsExactReadableBindingsBeforeLimit(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		f := newFixtureWith(t, platform.Config{Backends: platform.Backends{State: open(t)}})
		admin := f.machineAs(t, "admin", "acme", everything...)
		reader := f.person(t, "acme", "reader", "Reader", "humagen-viewers")
		scope := library.Scope{Kind: "workspace"}
		skill, err := f.p.SkillLibrary.Draft("acme", "admin", scope, library.Definition{Skill: projects.Skill{Name: "review", Description: "Review changes", Instructions: "Read the proposal"}, Tools: []string{}}, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if err := f.p.SkillLibrary.Tested("acme", scope, skill.Name, 1, "trusted-fixture-test"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.p.SkillLibrary.Publish("acme", "admin", scope, skill.Name, 1, time.Now()); err != nil {
			t.Fatal(err)
		}
		want(t, f, admin, "POST", "/api/projects", `{"name":"Secret","members":[{"who":"admin","role":"owner"}]}`, http.StatusCreated)
		for _, name := range []string{"a-hidden", "b-personal", "z-visible", "zz-visible"} {
			want(t, f, admin, "POST", "/api/agents", fmt.Sprintf(`{"name":%q,"model":"offline","provider":"offline","system_prompt":"Review"}`, name), http.StatusCreated)
		}
		want(t, f, admin, "PUT", "/api/agents/a-hidden/home", `{"project":"Secret"}`, http.StatusOK)
		personalOwner := auth.Principal{Kind: auth.User, Tenant: "acme", Subject: "ada", Roles: []auth.Role{auth.Admin}}
		personal, err := f.p.PersonalProject(personalOwner)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.p.SetAgentHome(t.Context(), "acme", "b-personal", personal.Name, projects.Writer{By: "ada", Govern: true}); err != nil {
			t.Fatal(err)
		}
		ref := []library.SkillRef{{ID: skill.ID, Version: 1}}
		for _, name := range []string{"a-hidden", "b-personal", "z-visible"} {
			if _, err := f.p.AgentSkillDefaults.Set("acme", name, 1, 0, ref); err != nil {
				t.Fatal(err)
			}
		}
		path := "/api/library/skills/" + skill.ID + "/versions/1/agents"
		first := want(t, f, reader, "GET", path+"?limit=1", "", http.StatusOK)
		agents := sliceOf(first["agents"])
		if len(agents) != 1 || obj(agents[0])["agent"] != "z-visible" || first["more"] != false || strings.Contains(fmt.Sprint(first), "a-hidden") || strings.Contains(fmt.Sprint(first), "b-personal") {
			t.Fatal("hidden binding affected usage result", first)
		}
		if _, err := f.p.AgentSkillDefaults.Set("acme", "zz-visible", 1, 0, ref); err != nil {
			t.Fatal(err)
		}
		bounded := want(t, f, reader, "GET", path+"?limit=1", "", http.StatusOK)
		if len(sliceOf(bounded["agents"])) != 1 || bounded["more"] != true {
			t.Fatal("visible usage bound missing", bounded)
		}
		noAgents := f.machineAs(t, "limited", "acme", "humagen:projects:read")
		hidden := want(t, f, noAgents, "GET", path+"?limit=1", "", http.StatusOK)
		if len(sliceOf(hidden["agents"])) != 0 || hidden["more"] != false {
			t.Fatal("agent permission bypass", hidden)
		}
		want(t, f, reader, "GET", path+"?limit=0", "", http.StatusBadRequest)
		want(t, f, reader, "GET", path+"?limit=201", "", http.StatusBadRequest)
		want(t, f, f.machine(t, "other", everything...), "GET", path, "", http.StatusNotFound)
		want(t, f, admin, "PUT", "/api/agents/z-visible", `{"spec":{"name":"z-visible","model":"offline","provider":"offline","system_prompt":"Changed review"},"expected_rev":1}`, http.StatusOK)
		changed := want(t, f, reader, "GET", path+"?limit=1", "", http.StatusOK)
		if obj(sliceOf(changed["agents"])[0])["needs_review"] != true {
			t.Fatal("stale spec binding claimed current", changed)
		}
		want(t, f, reader, "GET", "/api/library/skills/"+skill.ID+"/versions/2/agents", "", http.StatusNotFound)
		privateSkill, err := f.p.SkillLibrary.Draft("acme", "admin", library.Scope{Kind: "project", Project: "Secret"}, library.Definition{Skill: projects.Skill{Name: "hidden-skill", Description: "Private", Instructions: "Private"}, Tools: []string{}}, 0, nil, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		want(t, f, reader, "GET", "/api/library/skills/"+privateSkill.ID+"/versions/1/agents", "", http.StatusNotFound)
		adminUse := want(t, f, admin, "GET", path, "", http.StatusOK)
		if strings.Contains(fmt.Sprint(adminUse), "b-personal") {
			t.Fatal("admin saw personal agent usage", adminUse)
		}
	})
}
