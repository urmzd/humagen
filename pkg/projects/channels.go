package projects

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// General is the channel every project has. It exists before anything is
// written about it, and it cannot be archived.
const General = "general"

// Channel bounds.
const (
	// MaxChannels is how many channels a project keeps, general and the
	// archived ones included.
	MaxChannels    = 20
	MaxChannelName = 40
	MaxPurpose     = 200
	// MaxBindings is how many channels one Slack client is bound to.
	MaxBindings = 200
)

// ChannelChanged is what a change to one of a project's channels records.
const ChannelChanged = "channel"

// Channel is one place in a project where threads live. Its name is its
// identity within the project and never changes. A channel has its
// project's visibility.
type Channel struct {
	// Project is the project it is in. "" is the root.
	Project string `json:"project"`
	// Name is lower case letters, digits and dashes, at most 40.
	Name  string `json:"name"`
	Title string `json:"title,omitempty"`
	// Purpose is sent to every agent answering in the channel.
	Purpose string `json:"purpose,omitempty"`
	// Default answers an untagged message here: an agent, or "auto". ""
	// inherits from the project.
	Default string `json:"default,omitempty"`
	// Slack binds the channel to one Slack channel of one Slack client: a
	// Slack thread there is a shared thread here. One Slack channel is bound
	// to at most one channel in the workspace.
	Slack *SlackBinding `json:"slack,omitempty"`
	// BoundAt is when Slack was last bound as it stands: set when a binding
	// is made or moved, kept while it holds, and zero while unbound. A
	// client's app posts nothing announced before it (Binding.BoundAt).
	BoundAt time.Time `json:"bound_at,omitzero"`
	// Archived keeps the channel and its threads, and takes no new ones.
	Archived bool `json:"archived,omitempty"`
	// Managed is ManagedFile when the platform file declares the channel.
	// The file then owns its title, purpose and default.
	Managed string `json:"managed,omitempty"`
	// Version counts writes, from 1. General reads 0 until it is written.
	Version   int       `json:"version"`
	CreatedBy string    `json:"created_by,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedBy string    `json:"updated_by,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

// SlackBinding is a Slack channel a channel is bound to: the Slack client's
// id here, and the Slack channel's id there.
type BridgeDirection string

const (
	BridgeTwoWay   BridgeDirection = "two_way"
	BridgeInbound  BridgeDirection = "inbound"
	BridgeOutcomes BridgeDirection = "outcomes"
)

func (d BridgeDirection) Effective() BridgeDirection {
	if d == "" {
		return BridgeTwoWay
	}
	return d
}
func (d BridgeDirection) AllowsInbound() bool {
	return d.Effective() == BridgeTwoWay || d == BridgeInbound
}
func (d BridgeDirection) AllowsOutcomes() bool {
	return d.Effective() == BridgeTwoWay || d == BridgeOutcomes
}

// An omitted direction preserves an existing explicit binding's two-way
// behavior. A missing binding never grants either direction.
type SlackBinding struct {
	Direction BridgeDirection `json:"direction,omitempty"`
	Client    string          `json:"client"`
	Channel   string          `json:"channel"`
}

// Binding is one Slack channel a client is bound to, and the channel it is
// bound to here.
type Binding struct {
	Direction    BridgeDirection `json:"direction"`
	SlackChannel string          `json:"slack_channel"`
	Project      string          `json:"project"`
	Channel      string          `json:"channel"`
	// BoundAt is when the Slack channel was bound to this channel. What was
	// announced before then never reached it, and a catch-up does not post
	// it now.
	BoundAt time.Time `json:"bound_at,omitzero"`
}

// boundAt is when next's binding was made, as a write from cur at at
// leaves it: kept while the binding holds, at when it is new or moved, and
// zero when there is none. A binding written before the time was kept
// reads as bound at its channel's last change, which is no earlier.
func boundAt(cur, next Channel, at time.Time) time.Time {
	switch {
	case next.Slack == nil:
		return time.Time{}
	case cur.Slack == nil || !sameBinding(cur.Slack, next.Slack):
		return at
	case cur.BoundAt.IsZero():
		return cur.UpdatedAt
	}
	return cur.BoundAt
}

// ChannelChange is a channel before and after a change.
type ChannelChange struct {
	Before *Channel `json:"before,omitempty"`
	After  *Channel `json:"after,omitempty"`
}

var (
	validChannel = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	// validDefault is an agent's name, as a client's agent list holds it.
	validDefault = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)
	// validClient is a client's id; validSlackChannel a Slack channel's id,
	// public (C) or private (G): a direct message is never bound.
	validClient       = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)
	validSlackChannel = regexp.MustCompile(`^[CG][A-Z0-9]{2,31}$`)
)

// ValidChannel reports whether a name can name a channel.
func ValidChannel(name string) bool { return validChannel.MatchString(name) }

// ValidDefault reports whether a default names an agent or "auto". Whether
// that agent is available where it is set is the platform's to check.
func ValidDefault(agent string) bool { return validDefault.MatchString(agent) }

// channelKey is where a channel is kept. Neither a project's name nor a
// channel's holds a "/".
func channelKey(project, name string) string { return project + "/" + name }

// virtualGeneral is a project's general channel before it is written.
func virtualGeneral(project string) Channel { return Channel{Project: project, Name: General} }

func (c Channel) validate() error {
	chars := utf8.RuneCountInString
	switch {
	case c.Project != Root && !ValidName(c.Project):
		return fmt.Errorf("%w: no project can be called %q", ErrInvalid, c.Project)
	case !ValidChannel(c.Name):
		return fmt.Errorf("%w: a channel's name is lower case letters, digits and dashes, starting with a letter or digit, at most %d", ErrInvalid, MaxChannelName)
	case chars(c.Title) > MaxTitle || !oneLine(c.Title):
		return fmt.Errorf("%w: a channel's title is one line of at most %d characters", ErrInvalid, MaxTitle)
	case chars(c.Purpose) > MaxPurpose || !oneLine(c.Purpose):
		return fmt.Errorf("%w: a channel's purpose is one line of at most %d characters", ErrInvalid, MaxPurpose)
	case c.Default != "" && !ValidDefault(c.Default):
		return fmt.Errorf("%w: a default is an agent's name or auto", ErrInvalid)
	case c.Name == General && c.Archived:
		return fmt.Errorf("%w: #%s cannot be archived", ErrInvalid, General)
	case c.Slack != nil && c.Slack.Direction.Effective() != BridgeTwoWay && c.Slack.Direction != BridgeInbound && c.Slack.Direction != BridgeOutcomes:
		return fmt.Errorf("%w: a Slack direction is two_way, inbound or outcomes", ErrInvalid)
	case c.Slack != nil && !validClient.MatchString(c.Slack.Client):
		return fmt.Errorf("%w: a Slack binding names a client by its id", ErrInvalid)
	case c.Slack != nil && !validSlackChannel.MatchString(c.Slack.Channel):
		return fmt.Errorf("%w: a Slack binding names a Slack channel by its id, such as C0123ABCD; a direct message is never bound", ErrInvalid)
	case c.Slack != nil && c.Archived:
		return fmt.Errorf("%w: an archived channel is bound to nothing", ErrInvalid)
	case c.Managed != "" && c.Managed != ManagedFile:
		return fmt.Errorf("%w: a channel is managed by the file or not at all", ErrInvalid)
	}
	return nil
}

// sameChannel reports whether two versions of a channel hold the same fields.
func sameChannel(a, b Channel) bool {
	return a.Title == b.Title && a.Purpose == b.Purpose && a.Default == b.Default && a.Archived == b.Archived && a.Managed == b.Managed &&
		sameBinding(a.Slack, b.Slack)
}

func (a SlackBinding) Equal(b SlackBinding) bool {
	return a.Client == b.Client && a.Channel == b.Channel && a.Direction.Effective() == b.Direction.Effective()
}
func sameBinding(a, b *SlackBinding) bool {
	return a == nil && b == nil || a != nil && b != nil && a.Equal(*b)
}

// checkBinding holds a channel's Slack binding to the rest of the tenant:
// one Slack channel is bound to one channel, and one client to at most
// MaxBindings.
func (t *tenant) checkBinding(c Channel) error {
	if c.Slack == nil {
		return nil
	}
	if t.projects[c.Project].PersonalOwner != "" {
		return fmt.Errorf("%w: personal projects cannot bind Slack channels", ErrInvalid)
	}
	n := 0
	for key, o := range t.channels {
		if o.Slack == nil || key == channelKey(c.Project, c.Name) || o.Slack.Client != c.Slack.Client {
			continue
		}
		if o.Slack.Channel == c.Slack.Channel {
			return fmt.Errorf("%w: that Slack channel is bound to #%s in %s", ErrInUse, o.Name, display(o.Project))
		}
		n++
	}
	if n >= MaxBindings {
		return fmt.Errorf("%w: a Slack client is bound to at most %d channels", ErrLimit, MaxBindings)
	}
	return nil
}

// Bindings returns the Slack channels a client is bound to, by Slack
// channel, at most MaxBindings.
func (s *Store) Bindings(tenantName, client string) []Binding {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []Binding{}
	t, ok := s.tenants[tenantName]
	if !ok {
		return out
	}
	for _, c := range t.channels {
		if c.Slack != nil && c.Slack.Client == client {
			out = append(out, Binding{Direction: c.Slack.Direction.Effective(), SlackChannel: c.Slack.Channel, Project: c.Project, Channel: c.Name, BoundAt: boundAt(c, c, c.UpdatedAt)})
		}
	}
	slices.SortFunc(out, func(a, b Binding) int { return strings.Compare(a.SlackChannel, b.SlackChannel) })
	if len(out) > MaxBindings {
		out = out[:MaxBindings]
	}
	return out
}

// BoundTo returns the channel a client's Slack channel is bound to, if any.
func (s *Store) BoundTo(tenantName, client, slackChannel string) (Channel, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	t, ok := s.tenants[tenantName]
	if !ok || client == "" || slackChannel == "" {
		return Channel{}, false
	}
	for _, c := range t.channels {
		if c.Slack != nil && c.Slack.Client == client && c.Slack.Channel == slackChannel {
			return cloneChannel(c), true
		}
	}
	return Channel{}, false
}

func cloneChannel(c Channel) Channel {
	if c.Slack != nil {
		binding := *c.Slack
		c.Slack = &binding
	}
	return c
}
func chanPtr(c Channel) *Channel { c = cloneChannel(c); return &c }

// loadChannel reads a channel's record.
func (t *tenant) loadChannel(key string, raw json.RawMessage) error {
	var c Channel
	if err := json.Unmarshal(raw, &c); err != nil {
		return fmt.Errorf("channel %q: %w", key, err)
	}
	if keyChannel+channelKey(c.Project, c.Name) != key {
		return fmt.Errorf("%w: the channel %q of %q is kept under %q", ErrInvalid, c.Name, c.Project, key)
	}
	t.channels[channelKey(c.Project, c.Name)] = c
	return nil
}

// checkChannels holds loaded channels to their records and their projects:
// a channel of a project that is not there is refused.
func (t *tenant) checkChannels() error {
	for key, c := range t.channels {
		if err := c.validate(); err != nil {
			return fmt.Errorf("channel %q: %w", key, err)
		}
		if _, ok := t.projects[c.Project]; !ok && c.Project != Root {
			return fmt.Errorf("%w: the channel %q belongs to %q, which is missing", ErrInvalid, c.Name, c.Project)
		}
		if err := t.checkBinding(c); err != nil {
			return fmt.Errorf("channel %q: %w", key, err)
		}
	}
	return nil
}

// channelsOf returns a project's channels: general first, then the others by
// name.
func (t *tenant) channelsOf(project string) []Channel {
	out := []Channel{}
	general, ok := t.channels[channelKey(project, General)]
	if !ok {
		general = virtualGeneral(project)
	}
	out = append(out, cloneChannel(general))
	for _, c := range t.channels {
		if c.Project == project && c.Name != General {
			out = append(out, cloneChannel(c))
		}
	}
	slices.SortFunc(out[1:], func(a, b Channel) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// channel returns one channel as a write starts from: general reads as
// virtual until it is written.
func (t *tenant) channel(project, name string) (Channel, bool) {
	if c, ok := t.channels[channelKey(project, name)]; ok {
		return cloneChannel(c), true
	}
	if name == General {
		return virtualGeneral(project), true
	}
	return Channel{}, false
}

// putChannel writes a channel's record.
func (w *write) putChannel(c Channel) {
	if c.Slack != nil {
		b := *c.Slack
		c.Slack = &b
	}
	w.t.channels[channelKey(c.Project, c.Name)] = c
	w.put(keyChannel+channelKey(c.Project, c.Name), c)
}

// dropChannels removes every channel of a project, as deleting it does.
func (w *write) dropChannels(project string) {
	for key, c := range w.t.channels {
		if c.Project == project {
			delete(w.t.channels, key)
			w.delete(keyChannel + key)
		}
	}
}

// governs checks that a writer may change a project's own settings: the root
// and company projects need projects:govern.
func governs(p Project, w Writer) error {
	if (p.IsRoot() || p.Kind == Company) && !w.Govern {
		return fmt.Errorf("%w: the workspace and company projects are changed with projects:govern", ErrForbidden)
	}
	return nil
}

// Channels returns a project's channels, general first and then the others by
// name, archived ones included.
func (s *Store) Channels(tenantName, project string) ([]Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.Tree(tenantName).Has(project) {
		return nil, fmt.Errorf("%w: %q", ErrNotFound, project)
	}
	t, ok := s.tenants[tenantName]
	if !ok {
		return []Channel{virtualGeneral(project)}, nil
	}
	return t.channelsOf(project), nil
}

// Channel returns one of a project's channels. General is always there.
func (s *Store) Channel(tenantName, project, name string) (Channel, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.Tree(tenantName).Has(project) {
		return Channel{}, fmt.Errorf("%w: %q", ErrNotFound, project)
	}
	t, ok := s.tenants[tenantName]
	if !ok {
		t = newTenant()
	}
	c, ok := t.channel(project, name)
	if !ok {
		return Channel{}, fmt.Errorf("%w: #%s in %s", ErrNotFound, name, display(project))
	}
	return cloneChannel(c), nil
}

// CreateChannel adds a channel to a project. Its name must be free in the
// project, archived channels included, and general is always taken. A project
// keeps at most MaxChannels.
func (s *Store) CreateChannel(tenantName string, c Channel, w Writer, at time.Time) (Channel, error) {
	c = cloneChannel(c)
	c.Archived, c.Managed = false, ""
	if err := c.validate(); err != nil {
		return Channel{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createChannel(tenantName, c, w, at)
}

// createChannel is CreateChannel, and UpsertChannel's way in for a new
// channel. The caller holds the lock and has validated c.
func (s *Store) createChannel(tenantName string, c Channel, w Writer, at time.Time) (Channel, error) {
	tx := s.begin(tenantName)
	p, ok := current(tx.t, c.Project)
	if !ok {
		return Channel{}, fmt.Errorf("%w: %q", ErrNotFound, c.Project)
	}
	if err := governs(p, w); err != nil {
		return Channel{}, err
	}
	if _, taken := tx.t.channels[channelKey(c.Project, c.Name)]; taken || c.Name == General {
		return Channel{}, fmt.Errorf("%w: #%s in %s", ErrExists, c.Name, display(c.Project))
	}
	if n := len(tx.t.channelsOf(c.Project)); n >= MaxChannels {
		return Channel{}, fmt.Errorf("%w: a project has at most %d channels, archived ones included", ErrLimit, MaxChannels)
	}
	if err := tx.t.checkBinding(c); err != nil {
		return Channel{}, err
	}
	c.BoundAt = boundAt(Channel{}, c, at)
	c.Version, c.CreatedBy, c.CreatedAt, c.UpdatedBy, c.UpdatedAt = 1, w.By, at, w.By, at
	tx.root(w.By, at)
	tx.putChannel(c)
	tx.record(Change{At: at, By: w.By, Project: c.Project, What: ChannelChanged, Channel: &ChannelChange{After: chanPtr(c)}})
	if err := tx.commit(); err != nil {
		return Channel{}, err
	}
	return cloneChannel(c), nil
}

// UpdateChannel changes a channel's title, purpose, default or Slack binding. fn edits a
// copy; the project, the name, whether it is archived and its management
// cannot change. version must be the current one (ErrVersion), and general
// reads 0 until it is first written. An archived channel is not changed, and
// what the file declares is changed only by the file (ErrManaged). A change
// that changes nothing writes nothing and keeps the version.
func (s *Store) UpdateChannel(tenantName, project, name string, version int, w Writer, at time.Time, fn func(*Channel) error) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	p, ok := current(tx.t, project)
	if !ok {
		return Channel{}, fmt.Errorf("%w: %q", ErrNotFound, project)
	}
	cur, ok := tx.t.channel(project, name)
	if !ok {
		return Channel{}, fmt.Errorf("%w: #%s in %s", ErrNotFound, name, display(project))
	}
	if err := governs(p, w); err != nil {
		return Channel{}, err
	}
	if version != cur.Version {
		return Channel{}, fmt.Errorf("%w: #%s is at version %d, not %d", ErrVersion, name, cur.Version, version)
	}
	if cur.Archived {
		return Channel{}, fmt.Errorf("%w: #%s is archived", ErrInvalid, name)
	}
	next := cloneChannel(cur)
	if err := fn(&next); err != nil {
		return Channel{}, err
	}
	switch {
	case next.Project != cur.Project || next.Name != cur.Name:
		return Channel{}, fmt.Errorf("%w: a channel's project and name are fixed when it is created", ErrInvalid)
	case next.Archived != cur.Archived:
		return Channel{}, fmt.Errorf("%w: a channel is archived on its own", ErrInvalid)
	case next.Managed != cur.Managed:
		return Channel{}, fmt.Errorf("%w: only the platform file manages a channel", ErrInvalid)
	}
	next.Version, next.CreatedBy, next.CreatedAt, next.UpdatedBy, next.UpdatedAt = cur.Version, cur.CreatedBy, cur.CreatedAt, cur.UpdatedBy, cur.UpdatedAt
	if err := next.validate(); err != nil {
		return Channel{}, err
	}
	if sameChannel(cur, next) {
		return cur, nil
	}
	if cur.Managed == ManagedFile {
		return Channel{}, fmt.Errorf("%w: the platform file declares #%s in %s", ErrManaged, name, display(project))
	}
	return s.replaceChannel(tx, cur, next, w.By, at)
}

// ArchiveChannel archives a channel: it and its threads stay, and it takes no
// new ones. General cannot be archived (ErrInUse), nor a channel the file
// declares (ErrManaged). Archiving an archived channel changes nothing.
func (s *Store) ArchiveChannel(tenantName, project, name string, w Writer, at time.Time) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	p, ok := current(tx.t, project)
	if !ok {
		return Channel{}, fmt.Errorf("%w: %q", ErrNotFound, project)
	}
	cur, ok := tx.t.channel(project, name)
	if !ok {
		return Channel{}, fmt.Errorf("%w: #%s in %s", ErrNotFound, name, display(project))
	}
	if err := governs(p, w); err != nil {
		return Channel{}, err
	}
	switch {
	case name == General:
		return Channel{}, fmt.Errorf("%w: every project keeps #%s", ErrInUse, General)
	case cur.Managed == ManagedFile:
		return Channel{}, fmt.Errorf("%w: #%s in %s is declared in the platform file; remove it there", ErrManaged, name, display(project))
	case cur.Archived:
		return cur, nil
	}
	next := cur
	next.Archived, next.Slack = true, nil
	return s.replaceChannel(tx, cur, next, w.By, at)
}

// UpsertChannel applies the platform file's declaration of a channel: it
// creates the channel, or takes over the one by that name, with the title,
// purpose and default the file sets. The file writes as projects:govern. A
// declaration that changes nothing writes nothing and keeps the version.
func (s *Store) UpsertChannel(tenantName string, c Channel, by string, at time.Time) (Channel, error) {
	c.Archived, c.Managed = false, ManagedFile
	if err := c.validate(); err != nil {
		return Channel{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	if _, ok := current(tx.t, c.Project); !ok {
		return Channel{}, fmt.Errorf("%w: %q", ErrNotFound, c.Project)
	}
	cur, ok := tx.t.channel(c.Project, c.Name)
	if !ok {
		return s.createChannel(tenantName, c, Writer{By: by, Govern: true}, at)
	}
	next := cur
	next.Title, next.Purpose, next.Default, next.Slack, next.Archived, next.Managed = c.Title, c.Purpose, c.Default, c.Slack, false, ManagedFile
	if sameChannel(cur, next) {
		return cur, nil
	}
	return s.replaceChannel(tx, cur, next, by, at)
}

// ReleaseChannel lets the platform file go of a channel that left it. The
// channel stays, unmanaged.
func (s *Store) ReleaseChannel(tenantName, project, name, by string, at time.Time) (Channel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := s.begin(tenantName)
	cur, ok := tx.t.channel(project, name)
	if !ok {
		return Channel{}, fmt.Errorf("%w: #%s in %s", ErrNotFound, name, display(project))
	}
	if cur.Managed == "" {
		return cur, nil
	}
	next := cur
	next.Managed = ""
	return s.replaceChannel(tx, cur, next, by, at)
}

// replaceChannel writes next over cur as a new version, with its history
// entry, in one write.
func (s *Store) replaceChannel(tx *write, cur, next Channel, by string, at time.Time) (Channel, error) {
	if err := tx.t.checkBinding(next); err != nil {
		return Channel{}, err
	}
	next.BoundAt = boundAt(cur, next, at)
	next.Version, next.UpdatedBy, next.UpdatedAt = cur.Version+1, by, at
	if next.CreatedAt.IsZero() {
		next.CreatedBy, next.CreatedAt = by, at
	}
	change := &ChannelChange{After: chanPtr(next)}
	if cur.Version > 0 {
		change.Before = chanPtr(cur)
	}
	tx.root(by, at)
	tx.putChannel(next)
	tx.record(Change{At: at, By: by, Project: next.Project, What: ChannelChanged, Channel: change})
	if err := tx.commit(); err != nil {
		return Channel{}, err
	}
	return cloneChannel(next), nil
}
