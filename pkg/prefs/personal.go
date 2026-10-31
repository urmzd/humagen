package prefs

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

type Delivery string

const (
	Immediate Delivery = "immediate"
	Digest    Delivery = "digest"
	Off       Delivery = "off"
)

// Personal settings only narrow a person's defaults. They never grant access
// or turn a project action into an allowed one.
type Personal struct {
	EmailVersion                int64               `json:"email_version,omitempty"`
	Email                       bool                `json:"email,omitempty"`
	Briefing                    string              `json:"briefing"`
	Notifications               map[string]Delivery `json:"notifications"`
	QuietStart                  string              `json:"quiet_start"`
	QuietEnd                    string              `json:"quiet_end"`
	Timezone                    string              `json:"timezone"`
	Remember                    bool                `json:"remember"`
	UsePersonalMemoryInProjects bool                `json:"use_personal_memory_in_projects"`
}

func DefaultPersonal() Personal {
	return Personal{Notifications: map[string]Delivery{}, Timezone: "UTC"}
}

var notificationKinds = map[string]bool{"approval": true, "clarification": true, "failure": true, "result": true, "mention": true, "announcement": true, "memory": true, "budget": true, "artifact": true, "alert": true}

func CleanPersonal(next Personal) (Personal, error) {
	if next.EmailVersion < 0 || next.EmailVersion > 9007199254740991 {
		return Personal{}, ErrInvalid
	}
	next.Briefing = strings.TrimSpace(next.Briefing)
	if !utf8.ValidString(next.Briefing) || utf8.RuneCountInString(next.Briefing) > 500 || strings.ContainsAny(next.Briefing, "\x00\r\n") {
		return Personal{}, fmt.Errorf("%w: briefing is one line of at most 500 characters", ErrInvalid)
	}
	if len(next.Notifications) > len(notificationKinds) {
		return Personal{}, ErrInvalid
	}
	modes := make(map[string]Delivery, len(next.Notifications))
	for kind, mode := range next.Notifications {
		if !notificationKinds[kind] || mode != Immediate && mode != Digest && mode != Off {
			return Personal{}, fmt.Errorf("%w: invalid notification preference", ErrInvalid)
		}
		modes[kind] = mode
	}
	next.Notifications = modes
	if next.Timezone == "" {
		next.Timezone = "UTC"
	}
	if len(next.Timezone) > 128 {
		return Personal{}, ErrInvalid
	}
	if _, err := time.LoadLocation(next.Timezone); err != nil {
		return Personal{}, fmt.Errorf("%w: unknown time zone", ErrInvalid)
	}
	if (next.QuietStart == "") != (next.QuietEnd == "") {
		return Personal{}, fmt.Errorf("%w: quiet hours need a start and an end", ErrInvalid)
	}
	if next.QuietStart != "" {
		if _, err := time.Parse("15:04", next.QuietStart); err != nil {
			return Personal{}, ErrInvalid
		}
		if _, err := time.Parse("15:04", next.QuietEnd); err != nil {
			return Personal{}, ErrInvalid
		}
		if next.QuietStart == next.QuietEnd {
			return Personal{}, fmt.Errorf("%w: quiet hours need different times", ErrInvalid)
		}
	}
	return next, nil
}
func (s *Store) SetPersonal(tenant, subject string, next Personal) (Preferences, error) {
	next, err := CleanPersonal(next)
	if err != nil {
		return Preferences{}, err
	}
	return s.change(tenant, subject, func(e *Preferences) error { e.Personal = next; return nil })
}

// NotificationDelivery is a dispatch policy, not a transport. A dispatcher must
// still authorize each inbox item and implement its chosen provider. Quiet
// hours narrow immediate delivery to digest, including across midnight and DST.
// Missing configuration defaults to off, so saving a profile starts no dispatch.
func (p Personal) NotificationDelivery(kind string, at time.Time) Delivery {
	mode := p.Notifications[kind]
	if mode == "" {
		return Off
	}
	if mode != Immediate || p.QuietStart == "" || p.QuietEnd == "" {
		return mode
	}
	location, err := time.LoadLocation(p.Timezone)
	if err != nil {
		return Off
	}
	clock := at.In(location).Format("15:04")
	quiet := clock >= p.QuietStart && clock < p.QuietEnd
	if p.QuietStart > p.QuietEnd {
		quiet = clock >= p.QuietStart || clock < p.QuietEnd
	}
	if quiet {
		return Digest
	}
	return mode
}
