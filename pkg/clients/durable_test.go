package clients

import (
	"errors"
	"reflect"
	"testing"

	"github.com/urmzd/warp/pkg/state"
	"github.com/urmzd/warp/pkg/state/statetest"
)

func TestRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		m, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		caps := DefaultCaps()
		// Created in an order their ids do not sort in.
		for _, c := range []Client{slackOf("zeta", "A0Z", "T0Z"), slackOf("alpha", "A0A", "T0A"), githubOf("mid", "acme")} {
			if _, err := m.Create("acme", c, caps, t0); err != nil {
				t.Fatal(err)
			}
		}
		up := slackOf("alpha", "A0A", "T0A")
		up.Slack = &SlackOptions{Machine: "slack-app", People: map[string]string{"U01ADALOVE": "ada"}}
		if _, err := m.Update("acme", up, t0.Add(1)); err != nil {
			t.Fatal(err)
		}
		if err := m.Delete("acme", "mid"); err != nil {
			t.Fatal(err)
		}
		env := slackOf("env-slack", "A0E", "T0E")
		env.Managed = ManagedEnv
		if _, err := m.Create("acme", env, caps, t0); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Create("globex", slackOf("zeta", "A0Z", "T0Z"), caps, t0); err != nil {
			t.Fatal(err)
		}

		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got := again.List("acme")
		// The environment's client is not kept: it is made again at start.
		if len(got) != 2 || got[0].ID != "zeta" || got[1].ID != "alpha" {
			t.Fatalf("after restart = %+v", got)
		}
		want, _ := m.Get("acme", "alpha")
		if !reflect.DeepEqual(got[1], want) || got[1].Version != 2 || got[1].Tenant != "acme" {
			t.Fatalf("alpha after restart = %+v, want %+v", got[1], want)
		}
		if len(again.On(Slack)) != 3 {
			t.Fatalf("On(slack) after restart = %+v", again.On(Slack))
		}
		if _, err := again.Get("globex", "alpha"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant's client = %v", err)
		}
		// A client created after the restart goes after those before it.
		later := githubOf("later", "acme")
		later.GitHub = &GitHubOptions{Login: "warp[bot]"}
		if _, err := again.Create("acme", later, caps, t0); err != nil {
			t.Fatal(err)
		}
		third, _ := Open(st)
		if l := third.List("acme"); len(l) != 3 || l[2].ID != "later" || l[2].GitHub == nil || l[2].GitHub.Login != "warp[bot]" {
			t.Fatalf("order after a second restart = %+v", l)
		}
	})
}

func TestSecretsRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := OpenSecrets(st)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range [][4]string{{"acme", "docs", SecretToken, "xoxb-1"}, {"acme", "docs", SecretSigning, "sig"}, {"acme", "ops", SecretToken, "xoxb-2"}, {"globex", "docs", SecretToken, "other"}} {
			if err := s.Put(w[0], w[1], w[2], w[3]); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Drop("acme", "ops"); err != nil {
			t.Fatal(err)
		}
		again, err := OpenSecrets(st)
		if err != nil {
			t.Fatal(err)
		}
		if v, ok := again.Get("acme", "docs", SecretSigning); !ok || v != "sig" {
			t.Fatalf("signing secret after restart = %q, %v", v, ok)
		}
		if _, ok := again.Get("acme", "ops", SecretToken); ok {
			t.Fatal("a dropped secret came back")
		}
		if v, _ := again.Get("globex", "docs", SecretToken); v != "other" {
			t.Fatalf("another tenant's secret = %q", v)
		}
	})
}

func TestFailedWriteChangesNothing(t *testing.T) {
	f := statetest.NewFaulty(state.NewMemory())
	m, _ := Open(f)
	s, _ := OpenSecrets(f)
	if _, err := m.Create("acme", slackOf("docs", "A0D", "T0D"), DefaultCaps(), t0); err != nil {
		t.Fatal(err)
	}
	if err := s.Put("acme", "docs", SecretToken, "kept"); err != nil {
		t.Fatal(err)
	}
	f.Fail(statetest.ErrInjected)
	if _, err := m.Create("acme", slackOf("ops", "A0O", "T0O"), DefaultCaps(), t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Create = %v", err)
	}
	off := slackOf("docs", "A0D", "T0D")
	off.Enabled = false
	if _, err := m.Update("acme", off, t0); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Update = %v", err)
	}
	if err := m.Delete("acme", "docs"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("Delete = %v", err)
	}
	if err := s.Put("acme", "docs", SecretToken, "lost"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("secret Put = %v", err)
	}
	if err := s.Drop("acme", "docs"); !errors.Is(err, statetest.ErrInjected) {
		t.Fatalf("secret Drop = %v", err)
	}
	if l := m.List("acme"); len(l) != 1 || !l[0].Enabled || l[0].Version != 1 {
		t.Fatalf("after failed writes = %+v", l)
	}
	if v, _ := s.Get("acme", "docs", SecretToken); v != "kept" {
		t.Fatalf("secret after failed writes = %q", v)
	}
}
