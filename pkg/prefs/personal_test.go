package prefs

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestPersonalSettingsKeepQuietHoursAcrossMidnightAndRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		next := DefaultPersonal()
		next.Notifications["mention"] = Immediate
		next.QuietStart = "22:00"
		next.QuietEnd = "07:00"
		next.Timezone = "America/Toronto"
		next.Briefing = "Use short paragraphs"
		if _, err := s.SetPersonal("acme", "ada", next); err != nil {
			t.Fatal(err)
		}
		next.Notifications["mention"] = Off
		s, err = Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p := get(t, s, "acme", "ada").Personal
		for _, row := range []struct {
			at   string
			want Delivery
		}{{"2026-10-25T02:30:00Z", Digest}, {"2026-10-25T15:00:00Z", Immediate}, {"2026-11-01T06:30:00Z", Digest}} {
			at, _ := time.Parse(time.RFC3339, row.at)
			if got := p.NotificationDelivery("mention", at); got != row.want {
				t.Fatalf("%s: %s != %s", row.at, got, row.want)
			}
		}
		if get(t, s, "other", "ada").Personal.NotificationDelivery("mention", time.Now()) != Off {
			t.Fatal("cross-tenant preferences")
		}
		if get(t, s, "acme", "someone").Personal.UsePersonalMemoryInProjects {
			t.Fatal("personal memory enabled by default")
		}
	})
}

func TestInvalidPersonalSettingsLeaveAllChoicesUnchanged(t *testing.T) {
	s := NewStore()
	next := DefaultPersonal()
	next.QuietStart = "22:00"
	if _, err := s.SetPersonal("acme", "ada", next); err == nil {
		t.Fatal("half quiet hours accepted")
	}
	next = DefaultPersonal()
	next.Notifications["unknown"] = Immediate
	if _, err := s.SetPersonal("acme", "ada", next); err == nil {
		t.Fatal("unbounded event kind accepted")
	}
	next = DefaultPersonal()
	next.Timezone = "unknown"
	if _, err := s.SetPersonal("acme", "ada", next); err == nil {
		t.Fatal("bad timezone accepted")
	}
}
