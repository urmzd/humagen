// Package prefs holds what one person has chosen for themselves: how they are
// shown and how the console looks to them.
//
// Preferences belong to a person in a tenant. Nobody else reads or writes them,
// and they change nothing about what the person is allowed to do.
//
// The store is in memory. Built with Open, it writes every change through a
// state.Store and reads them back when it is built again.
package prefs

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/urmzd/warp/pkg/projects"
	"github.com/urmzd/warp/pkg/state"
)

// Errors.
var (
	ErrInvalid = errors.New("prefs: invalid")
	ErrNoPhoto = errors.New("prefs: no photo")
)

// MaxPhoto bounds an uploaded profile picture, in bytes.
const MaxPhoto = 512 << 10

// maxName bounds a display name, in bytes.
const maxName = 80

// MaxPinned bounds how many conversations a person can pin.
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

// Accents are the accent colors the console offers.
var Accents = []string{"purple", "blue", "yellow", "teal"}

// Preferences is one person's choices.
type Preferences struct {
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
	// Muted is the projects whose announcements are left out of the
	// person's own inbox. They stay in each project's inbox, and the
	// workspace itself cannot be muted.
	Muted []string `json:"muted"`
}

// Defaults is what a person has before they choose anything.
func Defaults() Preferences {
	return Preferences{Theme: System, Accent: Accents[0], Pinned: []string{}, Muted: []string{}}
}

type photo struct {
	data []byte
	mime string
}

type entry struct {
	Preferences
	photo photo
}

// Store holds every person's preferences.
type Store struct {
	mu sync.RWMutex
	by map[string]*entry
	// kept and photos are where preferences and pictures are written. Nil
	// keeps them only in memory.
	kept   *state.Table[Preferences]
	photos *state.Table[storedPhoto]
}

// storedPhoto is a picture as it is written.
type storedPhoto struct {
	Data []byte `json:"data"`
	Mime string `json:"mime"`
}

// NewStore returns an empty store in memory.
func NewStore() *Store { return &Store{by: map[string]*entry{}} }

// Open returns a store that keeps everything in st, holding what st already
// has. A nil st is NewStore.
func Open(st state.Store) (*Store, error) {
	s := NewStore()
	s.kept = state.NewTable[Preferences](st, "prefs")
	s.photos = state.NewTable[storedPhoto](st, "prefs.photo")
	if err := s.kept.Load(func(tenant, subject string, p Preferences) error {
		if p.Pinned == nil {
			p.Pinned = []string{}
		}
		if p.Muted == nil {
			p.Muted = []string{}
		}
		s.by[key(tenant, subject)] = &entry{Preferences: p}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("prefs: load: %w", err)
	}
	if err := s.photos.Load(func(tenant, subject string, ph storedPhoto) error {
		if e, ok := s.by[key(tenant, subject)]; ok {
			e.photo = photo{data: ph.Data, mime: ph.Mime}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("prefs: load photos: %w", err)
	}
	return s, nil
}

func key(tenant, subject string) string { return tenant + "\x00" + subject }

// change applies fn to a person's entry and writes the result, undoing it if
// the write fails. With photo, the picture is written too. It is called with
// the lock held.
func (s *Store) change(tenant, subject string, photo bool, fn func(e *entry)) (Preferences, error) {
	k := key(tenant, subject)
	before, existed := s.by[k]
	var saved entry
	if existed {
		saved = *before
	}
	e := s.of(tenant, subject)
	fn(e)
	if err := s.write(tenant, subject, e, photo); err != nil {
		if existed {
			*e = saved
		} else {
			delete(s.by, k)
		}
		return Preferences{}, err
	}
	return e.Preferences, nil
}

func (s *Store) write(tenant, subject string, e *entry, photo bool) error {
	if !s.kept.Enabled() {
		return nil
	}
	if photo {
		// The picture is written first: preferences that say there is a
		// picture never point at one that was not kept.
		var err error
		if e.HasPhoto {
			err = s.photos.Put(tenant, subject, storedPhoto{Data: e.photo.data, Mime: e.photo.mime})
		} else {
			err = s.photos.Delete(tenant, subject)
		}
		if err != nil {
			return fmt.Errorf("prefs: write photo: %w", err)
		}
	}
	if err := s.kept.Put(tenant, subject, e.Preferences); err != nil {
		return fmt.Errorf("prefs: write: %w", err)
	}
	return nil
}

func (s *Store) of(tenant, subject string) *entry {
	e, ok := s.by[key(tenant, subject)]
	if !ok {
		e = &entry{Preferences: Defaults()}
		s.by[key(tenant, subject)] = e
	}
	return e
}

// Get returns a person's preferences.
func (s *Store) Get(tenant, subject string) Preferences {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if e, ok := s.by[key(tenant, subject)]; ok {
		return e.Preferences
	}
	return Defaults()
}

// Set replaces what a person can choose. The photo is changed separately.
func (s *Store) Set(tenant, subject string, next Preferences) (Preferences, error) {
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change(tenant, subject, false, func(e *entry) {
		e.DisplayName, e.Theme, e.Accent = next.DisplayName, next.Theme, next.Accent
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change(tenant, subject, false, func(e *entry) { e.Pinned = next })
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change(tenant, subject, false, func(e *entry) { e.Muted = next })
}

// MarkOnboarded records that a person has been through the first-run questions.
// The time is the server's, and the first one stands: asking twice does not
// move it.
func (s *Store) MarkOnboarded(tenant, subject string, at time.Time) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.by[key(tenant, subject)]; ok && !e.OnboardedAt.IsZero() {
		return e.Preferences, nil
	}
	return s.change(tenant, subject, false, func(e *entry) { e.OnboardedAt = at })
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change(tenant, subject, true, func(e *entry) {
		e.photo = photo{data: bytes.Clone(data), mime: mime}
		e.HasPhoto = true
		e.PhotoVersion++
	})
}

// ClearPhoto removes a profile picture.
func (s *Store) ClearPhoto(tenant, subject string) (Preferences, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.change(tenant, subject, true, func(e *entry) {
		e.photo, e.HasPhoto = photo{}, false
		e.PhotoVersion++
	})
}

// Photo returns a person's picture and its type.
func (s *Store) Photo(tenant, subject string) ([]byte, string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.by[key(tenant, subject)]
	if !ok || !e.HasPhoto {
		return nil, "", ErrNoPhoto
	}
	return e.photo.data, e.photo.mime, nil
}
