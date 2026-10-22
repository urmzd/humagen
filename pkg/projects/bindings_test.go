package projects_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func bind(client, slack string) *projects.SlackBinding {
	return &projects.SlackBinding{Client: client, Channel: slack}
}

func bindTo(t *testing.T, s *projects.Store, project, name string, b *projects.SlackBinding) (projects.Channel, error) {
	t.Helper()
	c, err := s.Channel("acme", project, name)
	if err != nil {
		t.Fatal(err)
	}
	return s.UpdateChannel("acme", project, name, c.Version, ada, t0, func(c *projects.Channel) error {
		c.Slack = b
		return nil
	})
}

// One Slack channel is bound to one channel of the workspace, through one
// client: binding it again elsewhere is refused while the first holds it, and
// archiving or unbinding frees it. The same Slack channel id through another
// client is that client's own.
func TestASlackChannelIsBoundToOneChannel(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	if _, err := s.CreateChannel("acme", projects.Channel{Project: "Field", Name: "renewals", Slack: bind("slack-acme", "C0123")}, ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, err := bindTo(t, s, "Field", projects.General, bind("slack-acme", "C0123")); !errors.Is(err, projects.ErrInUse) {
		t.Fatalf("binding a bound Slack channel again = %v, want ErrInUse", err)
	}
	if _, err := bindTo(t, s, "Field", projects.General, bind("slack-other", "C0123")); err != nil {
		t.Fatalf("the same Slack id through another client: %v", err)
	}
	if c, ok := s.BoundTo("acme", "slack-acme", "C0123"); !ok || c.Name != "renewals" || c.Project != "Field" {
		t.Fatalf("C0123 is bound to %+v, %v", c, ok)
	}
	if got := s.Bindings("acme", "slack-acme"); !reflect.DeepEqual(got, []projects.Binding{{SlackChannel: "C0123", Project: "Field", Channel: "renewals"}}) {
		t.Fatalf("slack-acme's bindings = %+v", got)
	}
	if _, err := s.ArchiveChannel("acme", "Field", "renewals", ada, t0); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.BoundTo("acme", "slack-acme", "C0123"); ok {
		t.Fatal("an archived channel stays bound")
	}
	if _, err := bindTo(t, s, "Field", projects.General, bind("slack-acme", "C0123")); err != nil {
		t.Fatalf("binding a freed Slack channel: %v", err)
	}
	if _, ok := s.BoundTo("globex", "slack-acme", "C0123"); ok {
		t.Fatal("another workspace reads acme's binding")
	}
}

func TestABindingNamesAClientAndASlackChannel(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	for name, b := range map[string]*projects.SlackBinding{
		"a direct message": bind("slack-acme", "D0123"),
		"no client":        bind("", "C0123"),
		"a client's title": bind("Slack Acme", "C0123"),
		"a channel's name": bind("slack-acme", "#general"),
		"a lower case id":  bind("slack-acme", "c0123"),
	} {
		if _, err := bindTo(t, s, "Field", projects.General, b); !errors.Is(err, projects.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
}

func TestBindingsSurviveARestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		mustCreate(t, s, "acme", team("Field", ""), ada)
		if _, err := bindTo(t, s, "Field", projects.General, bind("slack-acme", "C0123")); err != nil {
			t.Fatal(err)
		}
		again, err := projects.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if c, ok := again.BoundTo("acme", "slack-acme", "C0123"); !ok || c.Name != projects.General {
			t.Fatalf("after a restart C0123 is bound to %+v, %v", c, ok)
		}
	})
}
