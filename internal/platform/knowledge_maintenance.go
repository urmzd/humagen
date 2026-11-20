package platform

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/alerts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/graph"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
)

// Knowledge maintenance keeps people told about what needs them in their
// corpora, on the leader after each graph refresh, with no model calls:
//
//   - a member whose sync failed or whose catalog has not been crawled in
//     twice its cadence: an alert to the corpus's owner;
//   - stale documents: an alert to each owner, naming theirs;
//   - documents nobody owns: an alert to the corpus's owner to hand them on;
//   - warehouse tables that would add types the ontology lacks: a proposal,
//     and an alert to the corpus's owner to review it.
//
// Each alert lands in the person's own session and is resolved by the pass
// that finds its cause gone. The low-risk changes (staleness marks, facts
// retired with their source) need no proposal: the graph applies them as
// it rebuilds.

const (
	// KindKnowledge is the alert kind maintenance raises.
	KindKnowledge = alerts.Kind("knowledge")
	// maxListed bounds how many items one alert names.
	maxListed = 10
)

// knowledgeAlert is one alert maintenance wants open, by its key.
type knowledgeAlert struct {
	key, person, title, body, action string
}

func knowledgeKey(parts ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "knowledge/" + parts[0] + "/" + hex.EncodeToString(digest[:12])
}

func listed(items []string) string {
	slices.Sort(items)
	more := ""
	if len(items) > maxListed {
		more = fmt.Sprintf(", and %d more", len(items)-maxListed)
		items = items[:maxListed]
	}
	return strings.Join(items, ", ") + more
}

func corpusName(c corpora.Corpus) string {
	if c.Title != "" {
		return c.Title
	}
	return c.Name
}

// failingMembers are a corpus's members whose sync failed or went stale.
func (p *Platform) failingMembers(tenant string, c corpora.Corpus) []string {
	var out []string
	for _, m := range c.Members {
		switch m.Kind {
		case corpora.Source:
			src, err := p.Knowledge.Get(tenant, m.Name)
			if err != nil || src.Lifecycle == nil {
				continue
			}
			if lc := src.Lifecycle; lc.Status == knowledge.NeedsAttention || lc.Cause == knowledge.Stale {
				out = append(out, fmt.Sprintf("%s (%s)", m.Name, cmp.Or(string(lc.Cause), "needs attention")))
			}
		case corpora.Catalog:
			d, ok := p.declaredConnector(tenant, m.Name)
			stored, err := p.CatalogStore.Connector(tenant, m.Name)
			if !ok || err != nil {
				continue
			}
			p.catalog.mu.Lock()
			failed := p.catalog.attempts[[2]string{tenant, m.Name}].err
			p.catalog.mu.Unlock()
			refresh := d.Refresh
			if refresh <= 0 {
				refresh = 24 * time.Hour
			}
			switch {
			case failed != "":
				out = append(out, m.Name+" (its last crawl failed)")
			case stored.CrawledAt != nil && p.now().Sub(*stored.CrawledAt) > 2*refresh:
				out = append(out, m.Name+" (not crawled since "+stored.CrawledAt.Format("2006-01-02")+")")
			}
		}
	}
	return out
}

func documents(n int) string {
	if n == 1 {
		return "1 document"
	}
	return fmt.Sprintf("%d documents", n)
}

func isAre(n int) string {
	if n == 1 {
		return "is"
	}
	return "are"
}

func hasHave(n int) string {
	if n == 1 {
		return "has"
	}
	return "have"
}

// maintainCorpus is what one corpus needs of people now.
func (p *Platform) maintainCorpus(tenant string, c corpora.Corpus) ([]knowledgeAlert, error) {
	var out []knowledgeAlert
	name := corpusName(c)
	// Each alert opens where its decision is made: the sources, the graph
	// of documents and owners, or the proposed types.
	link, graphLink, typesLink := "/knowledge/"+c.Name, "/knowledge/"+c.Name+"/graph", "/knowledge/"+c.Name+"/types"
	if failing := p.failingMembers(tenant, c); len(failing) > 0 {
		out = append(out, knowledgeAlert{key: knowledgeKey("freshness", c.Name), person: c.Owner,
			title: "Sources in " + name + " need attention", action: link,
			body: "These sources could not be kept current: " + listed(failing) + ". Reconnect or sync them from the corpus."})
	}
	owner := p.invitationReader(tenant, c.Owner)
	g, sls, err := p.readerGraph(owner, []corpora.Corpus{c})
	if err != nil {
		return nil, err
	}
	view, err := p.graphView(tenant, g, sls, func(n graph.Node) bool { return n.Type == graph.Document }, maxGraphView)
	if err != nil {
		return nil, err
	}
	stale := map[string][]string{}
	var unowned []string
	for _, n := range view.Nodes {
		switch {
		case n.OwnerState == OwnerUnowned:
			unowned = append(unowned, n.Name)
		case n.Staleness != nil && n.Staleness.Stale:
			stale[n.Owner] = append(stale[n.Owner], fmt.Sprintf("%s (%s)", n.Name, strings.Join(n.Staleness.Reasons, ", ")))
		}
	}
	for person, docs := range stale {
		out = append(out, knowledgeAlert{key: knowledgeKey("stale", c.Name, person), person: person,
			title: fmt.Sprintf("%s you own in %s %s stale", documents(len(docs)), name, isAre(len(docs))), action: graphLink,
			body: "Update or replace them, or hand them to someone who owns them now: " + listed(docs) + "."})
	}
	if len(unowned) > 0 {
		out = append(out, knowledgeAlert{key: knowledgeKey("unowned", c.Name), person: c.Owner,
			title: fmt.Sprintf("%s in %s %s no owner", documents(len(unowned)), name, hasHave(len(unowned))), action: graphLink,
			body: "Claim them or hand them to someone: " + listed(unowned) + "."})
	}
	proposal, err := p.proposeOntology(tenant, c)
	if err != nil {
		return nil, err
	}
	if proposal != "" {
		out = append(out, knowledgeAlert{key: knowledgeKey("ontology", c.Name), person: c.Owner,
			title: "New types proposed for " + name, action: typesLink,
			body: "The warehouse tables in this corpus suggest types its ontology lacks: " + proposal + ". Accept or discard them on the corpus."})
	}
	return out, nil
}

// proposeOntology infers types from a corpus's warehouse tables as its
// owner sees them, and keeps a proposal when it adds a type the ontology
// lacks and was not proposed before. It answers the new types' names, or
// "" when there is nothing new to propose.
func (p *Platform) proposeOntology(tenant string, c corpora.Corpus) (string, error) {
	owner := p.invitationReader(tenant, c.Owner)
	var names []string
	for _, m := range c.Members {
		if m.Kind == corpora.Catalog && p.memberVisible(owner, m) {
			names = append(names, m.Name)
		}
	}
	if len(names) == 0 {
		return "", nil
	}
	entries, err := p.CatalogStore.List(tenant, names, catalog.MaxEntries)
	if err != nil {
		return "", err
	}
	inferred := graph.Infer(entries)
	declared := ontologyOf(c)
	var fresh []string
	for t := range inferred.Types {
		if _, have := declared.Types[t]; !have {
			fresh = append(fresh, t)
		}
	}
	if len(fresh) == 0 {
		return "", nil
	}
	raw := inferred.Raw()
	digest := sha256.Sum256(raw)
	seen := hex.EncodeToString(digest[:12])
	if c.OntologySeen == seen {
		// Proposed already: open, accepted or discarded, it is not asked again.
		if len(c.Proposed) > 0 {
			return listed(fresh), nil
		}
		return "", nil
	}
	_, err = p.Corpora.Update(tenant, c.Name, c.Version, p.now(), func(c *corpora.Corpus) error {
		c.Proposed, c.OntologySeen = raw, seen
		return nil
	})
	if errors.Is(err, corpora.ErrConflict) {
		return "", nil // changed meanwhile: the next pass looks again
	}
	if err != nil {
		return "", err
	}
	return listed(fresh), nil
}

// MaintainKnowledge runs one maintenance pass over every corpus: it raises
// what needs people and resolves what no longer does. It finishes the pass
// whatever fails and answers every failure joined.
func (p *Platform) MaintainKnowledge(ctx context.Context) error {
	return p.Corpora.EachTenant(func(tenant string) error {
		cs, err := p.Corpora.List(tenant)
		if err != nil {
			return err
		}
		want := map[string]knowledgeAlert{}
		var failed []error
		for _, c := range cs {
			if err := ctx.Err(); err != nil {
				return err
			}
			found, err := p.maintainCorpus(tenant, c)
			if err != nil {
				failed = append(failed, fmt.Errorf("corpus %s: %w", c.Name, err))
				continue
			}
			for _, a := range found {
				want[a.key] = a
			}
		}
		for _, a := range want {
			if err := p.raiseKnowledge(ctx, tenant, a); err != nil {
				failed = append(failed, fmt.Errorf("alert %q: %w", a.title, err))
			}
		}
		// A corpus that could not be read keeps its alerts as they are.
		if len(failed) == 0 {
			failed = append(failed, p.resolveKnowledge(tenant, want))
		}
		return errors.Join(failed...)
	})
}

func (p *Platform) raiseKnowledge(ctx context.Context, tenant string, a knowledgeAlert) error {
	if a.person == "" || !p.invitationReader(tenant, a.person).Can(auth.KnowledgeRead) {
		return nil
	}
	// The alert lands in the person's own session with the builder. Before
	// a workspace has a model there is no built-in to hold that session, and
	// the alert stays in the person's alert center alone.
	session, project, agent := "", "", ""
	if id, err := p.EnsureAlertSession(ctx, tenant, BuiltinBuilder, "", a.person, a.title); err == nil {
		th, err := p.Threads.Get(tenant, id)
		if err != nil {
			return err
		}
		session, project, agent = id, th.Project, BuiltinBuilder
	} else if !errors.Is(err, projects.ErrInvalid) {
		return err
	}
	held, raised, err := p.Alerts.Raise(tenant, a.key, alerts.Alert{
		Session: session, Project: project, Agent: agent, Kind: KindKnowledge,
		Title: a.title, Body: a.body, Priority: alerts.Normal, Severity: alerts.Warning, State: alerts.OpenState,
		Action: a.action, ResolveWhen: "Maintenance finds the cause gone.",
		Who: []string{a.person}, Notify: []string{a.person}, At: p.now(),
	})
	if err != nil || !raised {
		return err
	}
	return p.inboxAlertCompletion(tenant, held)
}

// resolveKnowledge resolves every open maintenance alert whose cause the
// pass no longer found.
func (p *Platform) resolveKnowledge(tenant string, want map[string]knowledgeAlert) error {
	all, err := p.Alerts.Alerts(tenant)
	if err != nil {
		return err
	}
	for _, a := range all {
		if a.Kind != KindKnowledge || !a.Open() || !strings.HasPrefix(a.Rule, "knowledge/") {
			continue
		}
		if _, still := want[a.Rule]; still {
			continue
		}
		if _, err := p.AckAlert(tenant, a.ID, BuiltinBuilder); err != nil {
			return err
		}
	}
	return nil
}
