package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// What a member brings to a corpus.
const (
	// HoldsFiles is indexed text: passages and vectors kept in the corpus.
	HoldsFiles = "files"
	// HoldsReferences is pointers searched live at the source with the
	// reader's credential; no content is copied.
	HoldsReferences = "references"
	// HoldsRecords is warehouse tables kept as schema and owners only,
	// queried live; rows are never copied.
	HoldsRecords = "records"
)

// Member status, as a corpus shows it.
const (
	MemberReady   = "ready"
	MemberSyncing = "syncing"
	MemberFailed  = "failed"
	// MemberMissing is a member whose source was deleted.
	MemberMissing = "missing"
	// MemberHidden is a member the reader may not see. Its name is withheld.
	MemberHidden = "hidden"
)

// CorpusMemberView is one member as a reader sees it.
type CorpusMemberView struct {
	corpora.Member
	// Holds is files, references or records; empty when hidden or missing.
	Holds string `json:"holds,omitempty"`
	// Provider is where it comes from: manual, drive, notion, s3, gcs,
	// bigquery, snowflake.
	Provider string `json:"provider,omitempty"`
	// Items counts indexed documents, or the tables and views a warehouse
	// lists.
	Items  int    `json:"items"`
	Status string `json:"status"`
}

// CorpusView is a corpus as a reader sees it.
type CorpusView struct {
	corpora.Corpus
	Members []CorpusMemberView `json:"members"`
	// Mine is set for its owner, who alone changes or deletes it.
	Mine bool `json:"mine"`
}

// CorpusCreate is what a person sets when making a corpus.
type CorpusCreate struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description"`
	Readers     []corpora.Reader `json:"readers"`
	Members     []corpora.Member `json:"members"`
	Links       corpora.Links    `json:"links"`
	// Agents are the agents granted the corpus.
	Agents      []string `json:"agents"`
	GraphSearch bool     `json:"graph_search"`
}

// CorpusChange is a partial change to a corpus. A nil field is kept.
// Version, when set, must be the corpus's current version.
type CorpusChange struct {
	Title       *string           `json:"title,omitempty"`
	Description *string           `json:"description,omitempty"`
	Readers     *[]corpora.Reader `json:"readers,omitempty"`
	Members     *[]corpora.Member `json:"members,omitempty"`
	Links       *corpora.Links    `json:"links,omitempty"`
	GraphSearch *bool             `json:"graph_search,omitempty"`
	// Agents replaces the agents granted the corpus. Agents the owner can
	// no longer see are kept.
	Agents *[]string `json:"agents,omitempty"`
	// Owner hands the corpus to another person.
	Owner   *string `json:"owner,omitempty"`
	Version int64   `json:"version,omitempty"`
}

// corpusVisible reports whether who may read a corpus: its owner, and the
// readers it names. Default deny.
func (p *Platform) corpusVisible(who auth.Principal, c corpora.Corpus) bool {
	if !who.Can(auth.KnowledgeRead) || who.Subject == "" {
		return false
	}
	if c.Owner == who.Subject {
		return true
	}
	var seer *Seer
	for _, r := range c.Readers {
		switch r.Kind {
		case corpora.Workspace:
			return true
		case corpora.Person:
			if r.Name == who.Subject {
				return true
			}
		case corpora.Project:
			if seer == nil {
				seer = p.Seer(who)
			}
			if seer.Project(r.Name) {
				return true
			}
		}
	}
	return false
}

// CorpusReadable reports whether who may read a corpus by name. A corpus
// that does not exist is not readable.
func (p *Platform) CorpusReadable(who auth.Principal, name string) bool {
	c, err := p.Corpora.Get(who.Tenant, name)
	return err == nil && p.corpusVisible(who, c)
}

// memberVisible reports whether who may see a member's source, by the
// source's own rules. Holding it in a corpus never widens that.
func (p *Platform) memberVisible(who auth.Principal, m corpora.Member) bool {
	switch m.Kind {
	case corpora.Source:
		src, err := p.Knowledge.Get(who.Tenant, m.Name)
		return err == nil && p.sourceRecordVisible(who, src)
	case corpora.Catalog:
		c, err := p.CatalogStore.Connector(who.Tenant, m.Name)
		return err == nil && who.Can(auth.KnowledgeRead) && c.VisibleTo(who.Subject, p.Seer(who).Project)
	}
	return false
}

// memberView is one member as who sees it. owner is set for the corpus's
// owner, who added every member and so sees the name of one they can no
// longer read, to remove it; nothing else about it is shown.
func (p *Platform) memberView(who auth.Principal, m corpora.Member, owner bool) CorpusMemberView {
	hidden := CorpusMemberView{Member: corpora.Member{Kind: m.Kind}, Status: MemberHidden}
	if owner {
		hidden.Name = m.Name
	}
	switch m.Kind {
	case corpora.Source:
		src, err := p.Knowledge.Get(who.Tenant, m.Name)
		if errors.Is(err, knowledge.ErrNotFound) {
			return CorpusMemberView{Member: m, Status: MemberMissing}
		}
		if err != nil || !p.sourceRecordVisible(who, src) {
			return hidden
		}
		v := CorpusMemberView{Member: m, Holds: HoldsFiles, Provider: "manual", Items: src.Documents, Status: MemberReady}
		if lc := src.Lifecycle; lc != nil {
			v.Provider = lc.Provider
			if lc.Mode == knowledge.Live {
				v.Holds = HoldsReferences
			}
			switch lc.Status {
			case knowledge.NeedsAttention:
				v.Status = MemberFailed
			case knowledge.Importing, knowledge.Indexing, knowledge.Syncing:
				v.Status = MemberSyncing
			}
		}
		return v
	case corpora.Catalog:
		c, err := p.CatalogStore.Connector(who.Tenant, m.Name)
		if errors.Is(err, catalog.ErrNotFound) {
			return CorpusMemberView{Member: m, Status: MemberMissing}
		}
		if err != nil || !c.VisibleTo(who.Subject, p.Seer(who).Project) {
			return hidden
		}
		v := CorpusMemberView{Member: m, Holds: HoldsRecords, Provider: c.System, Items: c.Entries, Status: MemberReady}
		if c.CrawledAt == nil {
			v.Status = MemberSyncing
		}
		p.catalog.mu.Lock()
		if p.catalog.attempts[[2]string{who.Tenant, c.Name}].err != "" {
			v.Status = MemberFailed
		}
		p.catalog.mu.Unlock()
		return v
	}
	return hidden
}

func (p *Platform) corpusView(who auth.Principal, c corpora.Corpus) CorpusView {
	seer := p.Seer(who)
	c.Agents = slices.DeleteFunc(slices.Clone(c.Agents), func(a string) bool { return !seer.Agent(a) })
	v := CorpusView{Corpus: c, Members: make([]CorpusMemberView, 0, len(c.Members)), Mine: c.Owner == who.Subject}
	for _, m := range c.Members {
		v.Members = append(v.Members, p.memberView(who, m, v.Mine))
	}
	return v
}

// CorporaAs lists the corpora who may read, by name.
func (p *Platform) CorporaAs(who auth.Principal) ([]CorpusView, error) {
	out := []CorpusView{}
	if !who.Can(auth.KnowledgeRead) {
		return out, nil
	}
	all, err := p.Corpora.List(who.Tenant)
	if err != nil {
		return nil, err
	}
	for _, c := range all {
		if p.corpusVisible(who, c) {
			out = append(out, p.corpusView(who, c))
		}
	}
	return out, nil
}

// CorpusAs reads one corpus. One who may not read it is told it is not
// there.
func (p *Platform) CorpusAs(who auth.Principal, name string) (CorpusView, error) {
	c, err := p.Corpora.Get(who.Tenant, name)
	if err != nil || !p.corpusVisible(who, c) {
		return CorpusView{}, fmt.Errorf("%w: corpus %q", corpora.ErrNotFound, name)
	}
	return p.corpusView(who, c), nil
}

// ownCorpus is a corpus its owner changes. Another reader is forbidden; one
// who may not read it is told it is not there.
func (p *Platform) ownCorpus(who auth.Principal, name string) (corpora.Corpus, error) {
	c, err := p.Corpora.Get(who.Tenant, name)
	if err != nil || !p.corpusVisible(who, c) {
		return corpora.Corpus{}, fmt.Errorf("%w: corpus %q", corpora.ErrNotFound, name)
	}
	if !who.Can(auth.KnowledgeWrite) || c.Owner != who.Subject {
		return corpora.Corpus{}, fmt.Errorf("%w: only %s's owner may change it", ErrForbidden, name)
	}
	return c, nil
}

// grantable checks that every agent named exists and who may see it.
func (p *Platform) grantable(who auth.Principal, agents []string) error {
	seer := p.Seer(who)
	for _, a := range agents {
		if _, err := p.Catalog().Get(context.Background(), who.Tenant, a, 0); err != nil || !seer.Agent(a) {
			return fmt.Errorf("%w: no agent named %q", corpora.ErrInvalid, a)
		}
	}
	return nil
}

// addable checks that who may see every member they add.
func (p *Platform) addable(who auth.Principal, ms []corpora.Member) error {
	for _, m := range ms {
		if !p.memberVisible(who, m) {
			return fmt.Errorf("%w: no %s named %q", corpora.ErrInvalid, m.Kind, m.Name)
		}
	}
	return nil
}

// CreateCorpus makes a corpus owned by who. Every member must be a source or
// connector who can see.
func (p *Platform) CreateCorpus(who auth.Principal, in CorpusCreate) (CorpusView, error) {
	if !who.Can(auth.KnowledgeWrite) || who.Subject == "" {
		return CorpusView{}, auth.ErrForbidden
	}
	if err := p.addable(who, in.Members); err != nil {
		return CorpusView{}, err
	}
	if err := p.grantable(who, in.Agents); err != nil {
		return CorpusView{}, err
	}
	c, err := p.Corpora.Create(who.Tenant, corpora.Corpus{Name: in.Name, Title: in.Title, Description: in.Description, Owner: who.Subject, Readers: in.Readers, Members: in.Members, Agents: in.Agents, Links: in.Links, GraphSearch: in.GraphSearch}, p.now())
	if err != nil {
		return CorpusView{}, err
	}
	return p.corpusView(who, c), nil
}

// UpdateCorpus changes a corpus its owner holds. A new members list keeps
// the members its owner can no longer see, so losing access to a source
// never drops it silently.
func (p *Platform) UpdateCorpus(who auth.Principal, name string, ch CorpusChange) (CorpusView, error) {
	if _, err := p.ownCorpus(who, name); err != nil {
		return CorpusView{}, err
	}
	if ch.Members != nil {
		if err := p.addable(who, *ch.Members); err != nil {
			return CorpusView{}, err
		}
	}
	if ch.Agents != nil {
		if err := p.grantable(who, *ch.Agents); err != nil {
			return CorpusView{}, err
		}
	}
	if ch.Owner != nil {
		if _, err := p.People.Get(who.Tenant, *ch.Owner); err != nil {
			return CorpusView{}, fmt.Errorf("%w: hand a corpus to a person in the workspace", corpora.ErrInvalid)
		}
	}
	c, err := p.Corpora.Update(who.Tenant, name, ch.Version, p.now(), func(c *corpora.Corpus) error {
		if c.Owner != who.Subject {
			return fmt.Errorf("%w: only %s's owner may change it", ErrForbidden, name)
		}
		if ch.Title != nil {
			c.Title = *ch.Title
		}
		if ch.Description != nil {
			c.Description = *ch.Description
		}
		if ch.Readers != nil {
			c.Readers = slices.Clone(*ch.Readers)
		}
		if ch.Links != nil {
			c.Links = *ch.Links
		}
		if ch.GraphSearch != nil {
			c.GraphSearch = *ch.GraphSearch
		}
		if ch.Members != nil {
			next := slices.Clone(*ch.Members)
			for _, m := range c.Members {
				if !p.memberVisible(who, m) && !slices.Contains(next, m) {
					next = append(next, m)
				}
			}
			c.Members = next
		}
		if ch.Agents != nil {
			seer := p.Seer(who)
			next := slices.Clone(*ch.Agents)
			for _, a := range c.Agents {
				if !seer.Agent(a) && !slices.Contains(next, a) {
					next = append(next, a)
				}
			}
			c.Agents = next
		}
		if ch.Owner != nil {
			c.Owner = *ch.Owner
		}
		return nil
	})
	if err != nil {
		return CorpusView{}, err
	}
	return p.corpusView(who, c), nil
}

// AddCorpusMember adds one source or connector to a corpus. Adding one it
// holds changes nothing.
func (p *Platform) AddCorpusMember(who auth.Principal, name string, m corpora.Member) (CorpusView, error) {
	if _, err := p.ownCorpus(who, name); err != nil {
		return CorpusView{}, err
	}
	if err := p.addable(who, []corpora.Member{m}); err != nil {
		return CorpusView{}, err
	}
	c, err := p.Corpora.Update(who.Tenant, name, 0, p.now(), func(c *corpora.Corpus) error {
		if c.Owner != who.Subject {
			return fmt.Errorf("%w: only %s's owner may change it", ErrForbidden, name)
		}
		if !c.Holds(m) {
			c.Members = append(c.Members, m)
		}
		return nil
	})
	if err != nil {
		return CorpusView{}, err
	}
	return p.corpusView(who, c), nil
}

// RemoveCorpusMember removes one member. It leaves the source itself as it
// is: other corpora may hold it.
func (p *Platform) RemoveCorpusMember(who auth.Principal, name string, m corpora.Member) (CorpusView, error) {
	held, err := p.ownCorpus(who, name)
	if err != nil {
		return CorpusView{}, err
	}
	if !held.Holds(m) {
		return CorpusView{}, fmt.Errorf("%w: %s holds no %s named %q", corpora.ErrNotFound, name, m.Kind, m.Name)
	}
	c, err := p.Corpora.Update(who.Tenant, name, 0, p.now(), func(c *corpora.Corpus) error {
		if c.Owner != who.Subject {
			return fmt.Errorf("%w: only %s's owner may change it", ErrForbidden, name)
		}
		c.Members = slices.DeleteFunc(c.Members, func(have corpora.Member) bool { return have == m })
		return nil
	})
	if err != nil {
		return CorpusView{}, err
	}
	return p.corpusView(who, c), nil
}

// DeleteCorpus removes a corpus. Its sources stay.
func (p *Platform) DeleteCorpus(who auth.Principal, name string, version int64) error {
	c, err := p.ownCorpus(who, name)
	if err != nil {
		return err
	}
	if version == 0 {
		// Delete the record whose owner was just checked, not a later one.
		version = c.Version
	}
	if err := p.Corpora.Delete(who.Tenant, name, version); err != nil {
		return err
	}
	return p.Graph.DeleteSlice(who.Tenant, name)
}
