package projects_test

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
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
	if got := s.Bindings("acme", "slack-acme"); !reflect.DeepEqual(got, []projects.Binding{{SlackChannel: "C0123", Project: "Field", Channel: "renewals", BoundAt: t0}}) {
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
		if got := again.Bindings("acme", "slack-acme"); len(got) != 1 || !got[0].BoundAt.Equal(t0) {
			t.Fatalf("after a restart the binding reads %+v, want bound at %v", got, t0)
		}
	})
}

// A binding says when it was made: a change that keeps it keeps the time,
// moving it to another Slack channel or client starts it again, and
// unbinding and binding again is a new binding. A client's app posts nothing
// announced before then.
func TestABindingSaysWhenItWasMade(t *testing.T) {
	s := projects.NewStore()
	mustCreate(t, s, "acme", team("Field", ""), ada)
	write := func(at time.Time, fn func(*projects.Channel)) projects.Channel {
		t.Helper()
		c, err := s.Channel("acme", "Field", projects.General)
		if err != nil {
			t.Fatal(err)
		}
		c, err = s.UpdateChannel("acme", "Field", projects.General, c.Version, ada, at, func(c *projects.Channel) error {
			fn(c)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	boundAt := func() time.Time {
		t.Helper()
		got := s.Bindings("acme", "slack-acme")
		if len(got) != 1 {
			t.Fatalf("bindings = %+v", got)
		}
		return got[0].BoundAt
	}
	t1, t2, t3, t4, t5 := t0.Add(time.Hour), t0.Add(2*time.Hour), t0.Add(3*time.Hour), t0.Add(4*time.Hour), t0.Add(5*time.Hour)
	write(t1, func(c *projects.Channel) { c.Slack = bind("slack-acme", "C0123") })
	if got := boundAt(); !got.Equal(t1) {
		t.Fatalf("bound at %v, want %v", got, t1)
	}
	write(t2, func(c *projects.Channel) { c.Title = "News"; c.BoundAt = t5 })
	if got := boundAt(); !got.Equal(t1) {
		t.Fatalf("after a change that keeps the binding, bound at %v, want %v", got, t1)
	}
	write(t3, func(c *projects.Channel) { c.Slack = bind("slack-acme", "C0456") })
	if got := boundAt(); !got.Equal(t3) {
		t.Fatalf("after moving the binding, bound at %v, want %v", got, t3)
	}
	if c := write(t4, func(c *projects.Channel) { c.Slack = nil }); !c.BoundAt.IsZero() {
		t.Fatalf("an unbound channel reads bound at %v", c.BoundAt)
	}
	write(t5, func(c *projects.Channel) { c.Slack = bind("slack-acme", "C0456") })
	if got := boundAt(); !got.Equal(t5) {
		t.Fatalf("bound again, bound at %v, want %v", got, t5)
	}
}
