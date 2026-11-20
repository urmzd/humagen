package prefs

import (
	"context"
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

// A record written before notification modes and quiet hours existed has no
// personal settings at all. It must still load, deliver nothing by default,
// and take a window that begins and ends on the same day.
func TestOldStoredRecordReadsWithoutQuietHoursOrOff(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		old := []byte(`{"display_name":"Ada","theme":"dark","accent":"teal","pinned":["t-1"]}`)
		if err := st.Write(context.Background(), "prefs", state.Create("acme", "ada", old)); err != nil {
			t.Fatal(err)
		}
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		got := get(t, s, "acme", "ada")
		p := got.Personal
		if got.DisplayName != "Ada" || got.Theme != Dark || len(got.Pinned) != 1 {
			t.Fatalf("old record = %+v", got)
		}
		if p.QuietStart != "" || p.QuietEnd != "" || p.Timezone != "UTC" || len(p.Notifications) != 0 {
			t.Fatalf("old record gained settings: %+v", p)
		}
		noon := time.Date(2026, 11, 11, 12, 0, 0, 0, time.UTC)
		if mode := p.NotificationDelivery("mention", noon); mode != Off {
			t.Fatalf("unset kind = %s", mode)
		}
		p.Notifications["mention"] = Immediate
		if mode := p.NotificationDelivery("mention", noon); mode != Immediate {
			t.Fatalf("no quiet hours = %s", mode)
		}
		// 12:00 to 13:00 in Toronto is 17:00 to 18:00 UTC in November.
		p.QuietStart, p.QuietEnd, p.Timezone = "12:00", "13:00", "America/Toronto"
		if _, err := s.SetPersonal("acme", "ada", p); err != nil {
			t.Fatal(err)
		}
		p = get(t, s, "acme", "ada").Personal
		for _, row := range []struct {
			at   string
			want Delivery
		}{{"2026-11-11T16:59:00Z", Immediate}, {"2026-11-11T17:00:00Z", Digest}, {"2026-11-11T17:59:00Z", Digest}, {"2026-11-11T18:00:00Z", Immediate}} {
			at, _ := time.Parse(time.RFC3339, row.at)
			if mode := p.NotificationDelivery("mention", at); mode != row.want {
				t.Fatalf("%s: %s != %s", row.at, mode, row.want)
			}
		}
	})
}
