// Package reposettings keeps what a workspace set for each repository a
// GitHub client covers: whether pull requests there are reviewed
// automatically and on which events, which agent answers a mention that names
// none, the instructions every run there is given, and what the platform reads
// for a run before it starts.
//
// A repository with no record follows its client: the client's automatic
// review switch and its default agent, which a deployment may set from its
// environment. A record overrides only the fields it sets, so removing it
// returns the repository to the client's defaults.
//
// It reads and writes the database per request, one record per repository,
// so every process agrees and each record carries its own version.
package reposettings

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/state"
)

// Errors a caller matches with errors.Is.
var (
	ErrInvalid  = errors.New("reposettings: invalid")
	ErrNotFound = errors.New("reposettings: not found")
	// ErrVersion reports a write made against a version that is no longer
	// the record's.
	ErrVersion = errors.New("reposettings: changed since it was read")
	ErrLimit   = errors.New("reposettings: limit reached")
)

// Bounds.
const (
	// MaxPerClient is how many repositories one client keeps settings for:
	// as many as an installation's list holds.
	MaxPerClient = 1000
	// MaxInstructions bounds a repository's instructions, in characters, as
	// a project's are bounded.
	MaxInstructions = 4000
	// MaxAgent bounds an agent's name.
	MaxAgent = 63
)

// The reads the platform makes before a run on a repository starts.
const (
	// ReadPullDiff reads the pull request's description and changed files.
	ReadPullDiff = "pull_diff"
	// ReadLinkedIssues reads the issues the pull request or issue links to
	// in the same repository.
	ReadLinkedIssues = "linked_issues"
)

// Reads are the reads a repository may turn on, in the order they are made.
var Reads = []string{ReadPullDiff, ReadLinkedIssues}

// AutoReview is when a pull request is reviewed automatically. The zero
// value reviews nothing.
type AutoReview struct {
	// Opened reviews a pull request when it opens or reopens ready for
	// review, or leaves draft.
	Opened bool `json:"opened"`
	// Pushes reviews the commits each push adds.
	Pushes bool `json:"pushes"`
}

// On reports whether any event is reviewed.
func (a AutoReview) On() bool { return a.Opened || a.Pushes }

// Settings is what a workspace set for one repository. A field left empty
// follows the client.
type Settings struct {
	// Repository is "owner/name" as it was first written.
	Repository string `json:"repository"`
	// AutoReview, when set, replaces the client's switch for this
	// repository.
	AutoReview *AutoReview `json:"auto_review,omitempty"`
	// Agent, when set, answers a mention that names no agent and reviews
	// pull requests here, in place of the client's default agent.
	Agent string `json:"agent,omitempty"`
	// Instructions are given to every run on the repository, after the
	// workspace's and the project's.
	Instructions string `json:"instructions,omitempty"`
	// Reads are what the platform reads before a run here starts, from
	// Reads.
	Reads []string `json:"reads,omitempty"`
	// CrossSurfaceReads, when false, keeps the repository to its own runs:
	// a run raised on another surface (Slack, the console) or on another
	// repository may not read it, and the console does not show its source.
	// Left out, it is readable, as every repository a client covers is.
	CrossSurfaceReads *bool     `json:"cross_surface_reads,omitempty"`
	UpdatedAt         time.Time `json:"updated_at"`
	UpdatedBy         string    `json:"updated_by,omitempty"`
}

// IsZero reports whether the settings change nothing.
func (s Settings) IsZero() bool {
	return s.AutoReview == nil && s.Agent == "" && s.Instructions == "" && len(s.Reads) == 0 && s.CrossSurfaceReads == nil
}

// ReadableElsewhere reports whether runs other than the repository's own may
// read it.
func (s Settings) ReadableElsewhere() bool { return s.CrossSurfaceReads == nil || *s.CrossSurfaceReads }

// ReadsOn reports whether the settings turn on a read.
func (s Settings) ReadsOn(read string) bool { return slices.Contains(s.Reads, read) }

// Normalize trims what a caller wrote and orders the reads, so one setting
// written two ways is one setting. It does not validate.
func (s Settings) Normalize() Settings {
	s.Repository = strings.TrimSpace(s.Repository)
	s.Agent = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s.Agent), "@"))
	s.Instructions = strings.TrimSpace(s.Instructions)
	if s.AutoReview != nil {
		a := *s.AutoReview
		s.AutoReview = &a
	}
	// Readable is the default, so it is kept as nothing.
	if s.CrossSurfaceReads != nil && *s.CrossSurfaceReads {
		s.CrossSurfaceReads = nil
	} else if s.CrossSurfaceReads != nil {
		off := false
		s.CrossSurfaceReads = &off
	}
	var reads []string
	for _, r := range Reads {
		if slices.ContainsFunc(s.Reads, func(x string) bool { return strings.EqualFold(strings.TrimSpace(x), r) }) {
			reads = append(reads, r)
		}
	}
	for _, x := range s.Reads {
		if x = strings.TrimSpace(x); !slices.ContainsFunc(Reads, func(r string) bool { return strings.EqualFold(r, x) }) {
			reads = append(reads, x)
		}
	}
	s.Reads = reads
	return s
}

// Validate reports every problem with normalized settings at once.
func (s Settings) Validate() error {
	var problems []string
	if _, err := RepoKey(s.Repository); err != nil {
		problems = append(problems, err.Error())
	}
	if len(s.Agent) > MaxAgent || strings.ContainsFunc(s.Agent, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r) || r == '@' || r == ':' || r == '/'
	}) {
		problems = append(problems, "an agent is named by its handle")
	}
	if !utf8.ValidString(s.Instructions) || strings.ContainsRune(s.Instructions, 0) || utf8.RuneCountInString(s.Instructions) > MaxInstructions {
		problems = append(problems, fmt.Sprintf("instructions are text of at most %d characters", MaxInstructions))
	}
	for _, r := range s.Reads {
		if !slices.Contains(Reads, r) {
			problems = append(problems, fmt.Sprintf("%q is not a read; the reads are %s", r, strings.Join(Reads, ", ")))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("%w: %s", ErrInvalid, strings.Join(problems, "; "))
	}
	return nil
}

// RepoKey is the lowercase "owner/name" a repository's record is kept under,
// or an error for a name that is not one repository.
func RepoKey(repo string) (string, error) {
	owner, name, ok := strings.Cut(strings.TrimSpace(repo), "/")
	bad := func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.')
	}
	if !ok || owner == "" || name == "" || len(owner) > 39 || len(name) > 100 || name == "." || name == ".." ||
		strings.ContainsFunc(owner, func(r rune) bool { return bad(r) || r == '_' || r == '.' }) || strings.ContainsFunc(name, bad) {
		return "", fmt.Errorf("%w: %q is not an owner/name repository", ErrInvalid, repo)
	}
	return strings.ToLower(owner + "/" + name), nil
}

// Item is a repository's settings and the version they were read at.
type Item struct {
	Settings
	Version int64 `json:"version"`
}

// Store keeps each workspace's repository settings.
type Store struct {
	t *state.Table[Settings]
}

// Open returns a store on st, which may be shared with other processes. A
// nil st keeps settings in this process's memory.
func Open(st state.Store) *Store {
	if st == nil {
		st = state.NewMemory()
	}
	return &Store{t: state.NewTable[Settings](st, "github_repo_settings")}
}

// prefix is where a client's records are kept: its id, escaped, then a
// slash, so one client's records never begin another's.
func prefix(client string) string { return url.PathEscape(client) + "/" }

func key(client, repo string) (string, error) {
	if strings.TrimSpace(client) == "" || len(client) > 128 {
		return "", fmt.Errorf("%w: a client is named by its id", ErrInvalid)
	}
	k, err := RepoKey(repo)
	if err != nil {
		return "", err
	}
	return prefix(client) + k, nil
}

// Get returns a repository's settings, or ErrNotFound when it follows its
// client.
func (s *Store) Get(tenant, client, repo string) (Item, error) {
	k, err := key(client, repo)
	if err != nil {
		return Item{}, err
	}
	it, err := s.t.Lookup(tenant, k)
	if errors.Is(err, state.ErrNotFound) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return Item{Settings: it.Value, Version: it.Version}, nil
}

// List returns every repository a client keeps settings for, at most
// MaxPerClient, ordered by name.
func (s *Store) List(tenant, client string) ([]Item, error) {
	if strings.TrimSpace(client) == "" || len(client) > 128 {
		return nil, fmt.Errorf("%w: a client is named by its id", ErrInvalid)
	}
	items, _, err := s.t.List(tenant, state.Query{Prefix: prefix(client), Limit: MaxPerClient})
	if err != nil {
		return nil, err
	}
	out := make([]Item, 0, len(items))
	for _, it := range items {
		out = append(out, Item{Settings: it.Value, Version: it.Version})
	}
	return out, nil
}

// Put keeps a repository's settings and returns them as kept. version is
// the version the caller read: 0 for a repository that follows its client,
// so a write never replaces one the caller has not seen (ErrVersion). Zero
// settings remove the record, as Delete does.
func (s *Store) Put(tenant, client string, v Settings, version int64, by string, now time.Time) (Item, error) {
	v = v.Normalize()
	if err := v.Validate(); err != nil {
		return Item{}, err
	}
	k, err := key(client, v.Repository)
	if err != nil {
		return Item{}, err
	}
	if version < 0 {
		return Item{}, fmt.Errorf("%w: a version is 0 or more", ErrInvalid)
	}
	if v.IsZero() {
		if version == 0 {
			return Item{Settings: Settings{Repository: v.Repository}}, nil
		}
		return Item{Settings: Settings{Repository: v.Repository}}, s.Delete(tenant, client, v.Repository, version)
	}
	v.UpdatedAt, v.UpdatedBy = now.UTC(), by
	b := s.t.Batch()
	if version == 0 {
		kept, err := s.List(tenant, client)
		if err != nil {
			return Item{}, err
		}
		if len(kept) >= MaxPerClient {
			return Item{}, fmt.Errorf("%w: a client keeps settings for at most %d repositories", ErrLimit, MaxPerClient)
		}
		b.Create(tenant, k, v)
	} else {
		b.PutAt(tenant, k, v, version)
	}
	if err := b.Commit(); errors.Is(err, state.ErrConflict) {
		return Item{}, ErrVersion
	} else if err != nil {
		return Item{}, err
	}
	return s.Get(tenant, client, v.Repository)
}

// Delete returns a repository to its client's defaults. version is the
// version the caller read (ErrVersion when it moved on); a repository with
// no record is ErrNotFound.
func (s *Store) Delete(tenant, client, repo string, version int64) error {
	k, err := key(client, repo)
	if err != nil {
		return err
	}
	if version <= 0 {
		return fmt.Errorf("%w: removing settings names the version read", ErrInvalid)
	}
	if _, err := s.t.Lookup(tenant, k); errors.Is(err, state.ErrNotFound) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	b := s.t.Batch()
	b.DeleteAt(tenant, k, version)
	if err := b.Commit(); errors.Is(err, state.ErrConflict) {
		return ErrVersion
	} else if err != nil {
		return err
	}
	return nil
}

// DeleteClient removes every setting a client keeps, when the client goes.
func (s *Store) DeleteClient(tenant, client string) error {
	items, err := s.List(tenant, client)
	if err != nil {
		return err
	}
	b := s.t.Batch()
	for _, it := range items {
		k, err := key(client, it.Repository)
		if err != nil {
			continue
		}
		b.Delete(tenant, k)
	}
	if b.Len() == 0 {
		return nil
	}
	return b.Commit()
}
