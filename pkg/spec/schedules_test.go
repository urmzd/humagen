package spec

import (
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/projects"
)

// Rule 7: each of a schedule's checks refuses the file, naming the document
// and the schedule's path.
func TestEachScheduleCheckNamesItsPath(t *testing.T) {
	withSchedule := func(sc string) string {
		return "    agents: {enable: [lead]}\n    projects:\n      - name: Field\n        agents: {enable: []}\n        schedules:\n          - " + sc + "\n"
	}
	for name, tc := range map[string]struct{ workspace, want string }{
		"a cron that does not parse": {
			withSchedule(`{name: recap, cron: "whenever", agent: lead, text: "What changed?"}`), `workspace.projects[0].schedules[0].cron:`,
		},
		"an agent nobody declares": {
			withSchedule(`{name: recap, cron: "41 8 * * 1", agent: ghost, text: "What changed?"}`),
			`workspace.projects[0].schedules[0].agent: "ghost" is not an agent in this file or one of the platform's own`,
		},
		"an agent not available in its project": {
			withSchedule(`{name: recap, cron: "41 8 * * 1", agent: helper, text: "What changed?"}`),
			`workspace.projects[0].schedules[0].agent: helper is not available in "Field": nothing on its chain enables it`,
		},
		"an answer sent to nobody's inbox": {
			withSchedule(`{name: recap, cron: "41 8 * * 1", agent: lead, text: "What changed?", deliver: inbox}`),
			`workspace.projects[0].schedules[0].deliver: a schedule in the file has no creator`,
		},
		"somewhere else": {
			withSchedule(`{name: recap, cron: "41 8 * * 1", agent: lead, text: "What changed?", deliver: email}`),
			`workspace.projects[0].schedules[0].deliver: "email" is project`,
		},
		"a name an address cannot hold": {
			withSchedule(`{name: "weekly recap", cron: "41 8 * * 1", agent: lead, text: "What changed?"}`),
			`workspace.projects[0].schedules[0].name: "weekly recap" is letters, digits, dashes and underscores`,
		},
		"nothing to ask": {
			withSchedule(`{name: recap, cron: "41 8 * * 1", agent: lead, text: ""}`),
			`workspace.projects[0].schedules[0].text:`,
		},
		"one name twice": {
			"    agents: {enable: [lead]}\n    projects:\n      - name: Field\n        schedules:\n" +
				"          - {name: recap, cron: \"41 8 * * 1\", agent: lead, text: x}\n" +
				"      - name: Ops\n        schedules:\n          - {name: recap, cron: \"41 8 * * 1\", agent: lead, text: y}\n",
			`workspace.projects[1].schedules[0].name: "recap" is workspace.projects[0].schedules[0]'s name again`,
		},
		"a project's posts neither published nor reviewed": {
			"    projects:\n      - {name: Field, agent_posts: sometimes}\n", `workspace.projects[0].agent_posts: "sometimes" is publish or review`,
		},
		"the workspace's posts neither published nor reviewed": {
			"    agent_posts: always\n", `workspace.agent_posts: "always" is publish or review`,
		},
	} {
		_, err := loadBody(t, platformDoc(tc.workspace, platformAgentsYAML))
		if want := `platform "default": ` + tc.want; err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", name, err, want)
		}
	}
}

// A recap the design writes passes, with the project's agent_posts and the
// workspace's.
func TestARecapInTheFileLoads(t *testing.T) {
	workspace := `    agents: {enable: ["*"]}
    agent_posts: review
    projects:
      - name: Field
        agent_posts: publish
        schedules:
          - {name: field-recap, cron: "41 8 * * 1", agent: humagen-analyst, text: "What changed in Field this week?", deliver: project}
          - {name: field-digest, cron: "13 7 * * *", agent: lead, text: Yesterday in Field.}
`
	cfg, err := loadBody(t, platformDoc(workspace, platformAgentsYAML))
	if err != nil {
		t.Fatal(err)
	}
	w := cfg.Platforms[0].Spec.Workspace
	if *w.AgentPosts != AgentPostsReview || w.Projects[0].AgentPosts != AgentPostsPublish || len(w.Projects[0].Schedules) != 2 ||
		w.Projects[0].Schedules[0].Deliver != DeliverProject {
		t.Fatalf("loaded = %+v", w)
	}
}

// The language's words for an agent's posts are the project store's.
func TestTheLanguagesAgentPostsAreTheProjectStores(t *testing.T) {
	if AgentPostsPublish != projects.AgentPostsPublish || AgentPostsReview != projects.AgentPostsReview {
		t.Fatal("the spec and the store name agent posts differently")
	}
}
