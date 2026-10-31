// Package prefs holds what one person has chosen for themselves: how they are
// shown and how the console looks to them.
//
// Preferences belong to a person in a tenant. Nobody else reads or writes them,
// and they change nothing about what the person is allowed to do.
//
// It reads and writes the database per request (pkg/state, per request): it
// holds nothing in memory, so any number of processes on one database agree,
// and it does not need pkg/state's single-writer lock.
package prefs

import (
	"bytes"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
)

// Errors.
var (
	ErrInvalid = errors.New("prefs: invalid")
	ErrVersion = errors.New("prefs: email preferences changed; reload and try again")
	ErrNoPhoto = errors.New("prefs: no photo")
)

// MaxPhoto bounds an uploaded profile picture, in bytes.
const MaxPhoto = 512 << 10

// maxName bounds a display name, in bytes.
const maxName = 80

// MaxPinned bounds each list of conversation and project pins.
const MaxPinned = 50

// maxPinID bounds one pinned conversation's id, in bytes.
const maxPinID = 128

// MaxMuted bounds how many projects a person can mute.
const MaxMuted = 100

// Theme is how the console is drawn.
type Theme string

const (
	// System follows the device's setting.
	System Theme = "system"
	Light  Theme = "light"
	Dark   Theme = "dark"
)

// Accents are the accent colors the console offers. Legacy choices remain
// valid. workspace, the default, is the workspace's branded accent, which is
// red when its branding sets none.
var Accents = []string{"workspace", "red", "yellow", "green", "purple", "blue", "teal"}

// Preferences is one person's choices.
type Preferences struct {
	Personal Personal `json:"personal"`
	// DisplayName is shown in place of the name the identity provider gave.
	DisplayName string `json:"display_name"`
	Theme       Theme  `json:"theme"`
	Accent      string `json:"accent"`
	// HasPhoto reports whether a profile picture is stored.
	HasPhoto bool `json:"has_photo"`
	// PhotoVersion changes with every upload, so a browser never shows a
	// stale picture from its cache.
	PhotoVersion int `json:"photo_version"`
	// OnboardedAt is when the person finished or skipped the first-run
	// questions. Zero means they have not been through them.
	OnboardedAt time.Time `json:"onboarded_at,omitzero"`
	// Pinned is the conversations kept at the top of the person's sidebar,
	// most recently pinned first. Only ids are kept: a conversation the
	// person can no longer read is simply not shown.
	Pinned []string `json:"pinned"`
	// PinnedProjects keeps project shortcuts independently of conversations.
	// The workspace uses its address, "~". Unreadable projects are not shown.
	PinnedProjects []string `json:"pinned_projects"`
	// Muted is the projects whose announcements are left out of the
	// person's own inbox. They stay in each project's inbox, and the
	// workspace itself cannot be muted.
	Muted []string `json:"muted"`
}

// Defaults is what a person has before they choose anything.
func Defaults() Preferences {
	return Preferences{Personal: DefaultPersonal(), Theme: System, Accent: Accents[0], Pinned: []string{}, PinnedProjects: []string{}, Muted: []string{}}
}

// Store holds every person's preferences: in the namespace "prefs", one
// record per person keyed by subject in their tenant, and their picture in
// "prefs.photo" under the same key.
type Store struct {
	kept   *state.Table[Preferences]
	photos *state.Table[storedPhoto]
}

// storedPhoto is a picture as it is written.
type storedPhoto struct {
	Data []byte `json:"data"`
	Mime string `json:"mime"`
}

// NewStore returns an empty store in this process's memory.
func NewStore() *Store {
	s, _ := Open(nil)
	return s
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps preferences in this process's memory.
func Open(st state.Store) (*Store, error) {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{kept: state.NewTable[Preferences](st, "prefs"), photos: state.NewTable[storedPhoto](st, "prefs.photo")}, nil
}

// normal fills what a record kept by an older release may lack.
func normal(p Preferences) Preferences {
	if p.Personal.Timezone == "" {
		p.Personal.Timezone = "UTC"
	}
	if p.Personal.Notifications == nil {
		p.Personal.Notifications = map[string]Delivery{}
	}
	if p.Pinned == nil {
		p.Pinned = []string{}
	}
	if p.PinnedProjects == nil {
		p.PinnedProjects = []string{}
	}
	if p.Muted == nil {
		p.Muted = []string{}
	}
	return p
}

// change applies fn to a person's preferences, as last written, and writes
// the result at the version it read, reading again when another writer
// moved them in between. fn returning state.ErrUnchanged writes nothing.
func (s *Store) change(tenant, subject string, fn func(p *Preferences) error) (Preferences, error) {
	out, err := s.kept.Update(tenant, subject, func(cur Preferences, ok bool) (Preferences, error) {
		if !ok {
			cur = Defaults()
		}
		next := clonePreferences(normal(cur))
		if err := fn(&next); err != nil {
			return Preferences{}, err
		}
		return next, nil
	})
	if err != nil {
		if errors.Is(err, ErrInvalid) || errors.Is(err, ErrVersion) {
			return Preferences{}, err
		}
		return Preferences{}, fmt.Errorf("prefs: write: %w", err)
	}
	return clonePreferences(normal(out)), nil
}

// Get returns a person's preferences: Defaults when they have chosen
// nothing.
func (s *Store) Get(tenant, subject string) (Preferences, error) {
	it, err := s.kept.Lookup(tenant, subject)
	if errors.Is(err, state.ErrNotFound) {
		return Defaults(), nil
	}
	if err != nil {
		return Preferences{}, fmt.Errorf("prefs: read: %w", err)
	}
	return normal(it.Value), nil
}

// Set replaces what a person can choose. The photo is changed separately.
//
// With personal, an email opt-in that changes must name the version after
// the one stored (Personal.EmailVersion), and an opt-in that does not change
// keeps the stored version: a change made from preferences read before
// another changed the opt-in is ErrVersion, whichever process made either.
func (s *Store) Set(tenant, subject string, next Preferences, personal ...Personal) (Preferences, error) {
	if len(personal) > 1 {
		return Preferences{}, ErrInvalid
	}
	if len(personal) == 1 {
		checked, err := CleanPersonal(personal[0])
		if err != nil {
			return Preferences{}, err
		}
		personal[0] = checked
	}
	next.DisplayName = strings.TrimSpace(next.DisplayName)
	switch {
	case len(next.DisplayName) > maxName:
		return Preferences{}, fmt.Errorf("%w: a display name is at most %d characters", ErrInvalid, maxName)
	case strings.ContainsAny(next.DisplayName, "\r\n\t<>"):
		return Preferences{}, fmt.Errorf("%w: a display name is plain text on one line", ErrInvalid)
	case next.Theme != System && next.Theme != Light && next.Theme != Dark:
		return Preferences{}, fmt.Errorf("%w: theme is system, light or dark", ErrInvalid)
	}
	ok := false
	for _, a := range Accents {
		ok = ok || a == next.Accent
	}
	if !ok {
		return Preferences{}, fmt.Errorf("%w: accent is one of %s", ErrInvalid, strings.Join(Accents, ", "))
	}
	return s.change(tenant, subject, func(e *Preferences) error {
		if len(personal) == 1 {
			p := personal[0]
			switch {
			case p.Email == e.Personal.Email:
				p.EmailVersion = e.Personal.EmailVersion
			case p.EmailVersion != e.Personal.EmailVersion+1:
				return ErrVersion
			}
			e.Personal = p
		}
		e.DisplayName, e.Theme, e.Accent = next.DisplayName, next.Theme, next.Accent
		return nil
	})
}

// CleanPinned checks a list of pinned conversations and drops repeats,
// keeping the first.
func CleanPinned(ids []string) ([]string, error) {
	if len(ids) > MaxPinned {
		return nil, fmt.Errorf("%w: at most %d conversations can be pinned", ErrInvalid, MaxPinned)
	}
	seen := make(map[string]bool, len(ids))
	next := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" || len(id) > maxPinID || strings.ContainsAny(id, "\r\n\t<>") {
			return nil, fmt.Errorf("%w: a pinned conversation is named by its id", ErrInvalid)
		}
		if !seen[id] {
			seen[id] = true
			next = append(next, id)
		}
	}
	return next, nil
}

// SetPinned replaces the conversations a person has pinned.
func (s *Store) SetPinned(tenant, subject string, ids []string) (Preferences, error) {
	next, err := CleanPinned(ids)
	if err != nil {
		return Preferences{}, err
	}
	return s.change(tenant, subject, func(e *Preferences) error { e.Pinned = next; return nil })
}

// CleanPinnedProjects bounds project shortcuts and keeps each name once.
func CleanPinnedProjects(names []string) ([]string, error) {
	if len(names) > MaxPinned {
		return nil, fmt.Errorf("%w: at most %d projects can be pinned", ErrInvalid, MaxPinned)
	}
	seen := make(map[string]bool, len(names))
	next := make([]string, 0, len(names))
	for _, name := range names {
		if name != projects.RootAddress && !projects.ValidName(name) {
			return nil, fmt.Errorf("%w: a pinned project is named by its name", ErrInvalid)
		}
		if !seen[name] {
			seen[name] = true
			next = append(next, name)
		}
	}
	return next, nil
}

// SetPinnedProjects replaces the person's project shortcuts.
func (s *Store) SetPinnedProjects(tenant, subject string, names []string) (Preferences, error) {
	next, err := CleanPinnedProjects(names)
	if err != nil {
		return Preferences{}, err
	}
	return s.change(tenant, subject, func(e *Preferences) error { e.PinnedProjects = next; return nil })
}

// CleanMuted checks a list of muted projects and drops repeats, keeping the
// first. Each is a project's name; the workspace, "" or "~", is never muted,
// since what it announces is for everyone.
func CleanMuted(names []string) ([]string, error) {
	if len(names) > MaxMuted {
		return nil, fmt.Errorf("%w: at most %d projects can be muted", ErrInvalid, MaxMuted)
	}
	seen := make(map[string]bool, len(names))
	next := make([]string, 0, len(names))
	for _, name := range names {
		switch {
		case name == projects.Root || name == projects.RootAddress:
			return nil, fmt.Errorf("%w: the workspace cannot be muted", ErrInvalid)
		case !projects.ValidName(name):
			return nil, fmt.Errorf("%w: a muted project is named by its name", ErrInvalid)
		}
		if !seen[name] {
			seen[name] = true
			next = append(next, name)
		}
	}
	return next, nil
}

// SetMuted replaces the projects a person has muted.
func (s *Store) SetMuted(tenant, subject string, names []string) (Preferences, error) {
	next, err := CleanMuted(names)
	if err != nil {
		return Preferences{}, err
	}
	return s.change(tenant, subject, func(e *Preferences) error { e.Muted = next; return nil })
}

// MarkOnboarded records that a person has been through the first-run questions.
// The time is the server's, and the first one stands: asking twice does not
// move it.
func (s *Store) MarkOnboarded(tenant, subject string, at time.Time) (Preferences, error) {
	return s.change(tenant, subject, func(e *Preferences) error {
		if !e.OnboardedAt.IsZero() {
			return state.ErrUnchanged
		}
		e.OnboardedAt = at
		return nil
	})
}

// imageType reports the type of an image from its own bytes. What the uploader
// says the file is plays no part: only a real PNG, JPEG or WebP is accepted,
// which keeps out SVG and HTML, both of which can carry script.
func imageType(data []byte) (string, bool) {
	switch {
	case bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")):
		return "image/png", true
	case bytes.HasPrefix(data, []byte("\xff\xd8\xff")):
		return "image/jpeg", true
	case len(data) >= 12 && bytes.Equal(data[:4], []byte("RIFF")) && bytes.Equal(data[8:12], []byte("WEBP")):
		return "image/webp", true
	}
	return "", false
}

// SetPhoto stores a profile picture.
func (s *Store) SetPhoto(tenant, subject string, data []byte) (Preferences, error) {
	if len(data) == 0 {
		return Preferences{}, fmt.Errorf("%w: the picture is empty", ErrInvalid)
	}
	if len(data) > MaxPhoto {
		return Preferences{}, fmt.Errorf("%w: a picture is at most %d KB", ErrInvalid, MaxPhoto>>10)
	}
	mime, ok := imageType(data)
	// The standard sniffer must agree, as a second opinion on the bytes.
	if !ok || http.DetectContentType(data) != mime {
		return Preferences{}, fmt.Errorf("%w: a picture is a PNG, JPEG or WebP image", ErrInvalid)
	}
	// The picture is written first: preferences that say there is a
	// picture never point at one that was not kept.
	if err := s.photos.Put(tenant, subject, storedPhoto{Data: bytes.Clone(data), Mime: mime}); err != nil {
		return Preferences{}, fmt.Errorf("prefs: write photo: %w", err)
	}
	return s.change(tenant, subject, func(e *Preferences) error {
		e.HasPhoto = true
		e.PhotoVersion++
		return nil
	})
}

// ClearPhoto removes a profile picture.
func (s *Store) ClearPhoto(tenant, subject string) (Preferences, error) {
	if err := s.photos.Delete(tenant, subject); err != nil {
		return Preferences{}, fmt.Errorf("prefs: write photo: %w", err)
	}
	return s.change(tenant, subject, func(e *Preferences) error {
		e.HasPhoto = false
		e.PhotoVersion++
		return nil
	})
}

// Photo returns a person's picture and its type.
func (s *Store) Photo(tenant, subject string) ([]byte, string, error) {
	p, err := s.Get(tenant, subject)
	if err != nil {
		return nil, "", err
	}
	if !p.HasPhoto {
		return nil, "", ErrNoPhoto
	}
	it, err := s.photos.Lookup(tenant, subject)
	if errors.Is(err, state.ErrNotFound) {
		return nil, "", ErrNoPhoto
	}
	if err != nil {
		return nil, "", fmt.Errorf("prefs: read photo: %w", err)
	}
	return it.Value.Data, it.Value.Mime, nil
}

func clonePreferences(p Preferences) Preferences {
	p.Pinned = slices.Clone(p.Pinned)
	p.PinnedProjects = slices.Clone(p.PinnedProjects)
	p.Muted = slices.Clone(p.Muted)
	p.Personal.Notifications = maps.Clone(p.Personal.Notifications)
	return p
}
