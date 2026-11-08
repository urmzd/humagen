package platform

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/orchagent/flow/runtime"

	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/corpora"
	"github.com/urmzd/humagen/pkg/graph"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// A corpus item address names one thing a corpus holds, so an agent can
// pull it in on demand, as corpus_search and corpus_graph handed it out:
//
//	corpus:<corpus>/<source>/<document id>   a document
//	corpus:<corpus>/<connector>/<table path> a warehouse table or view
//	corpus:<corpus>/node/<node id>           what a graph node stands for
//
// Resolving one gives compact content and where it comes from, by the same
// rule as every corpus tool: only what everyone the answer reaches may read,
// checked when it is resolved.

// CorpusAddressScheme is how a corpus item address starts.
const CorpusAddressScheme = "corpus:"

// maxContextText bounds what one resolved item hands an agent.
const maxContextText = 4000

// CorpusItem is one resolved corpus item.
type CorpusItem struct {
	Address string        `json:"address"`
	Corpus  string        `json:"corpus"`
	Kind    string        `json:"kind"` // document or table
	Title   string        `json:"title"`
	Text    string        `json:"text"`
	Link    string        `json:"link,omitempty"`
	Owner   string        `json:"owner,omitempty"`
	Stale   bool          `json:"stale,omitempty"`
	Label   corpora.Label `json:"label"`
	// HeldBack is set instead of content when the item exists but this
	// conversation may not be shown it.
	HeldBack string `json:"held_back,omitempty"`
}

// ParseCorpusAddress splits a corpus item address into its corpus, its
// member (or "node") and the rest.
func ParseCorpusAddress(address string) (corpus, member, rest string, err error) {
	body, ok := strings.CutPrefix(address, CorpusAddressScheme)
	if !ok {
		return "", "", "", fmt.Errorf("%w: a corpus item address starts with %s", corpora.ErrInvalid, CorpusAddressScheme)
	}
	parts := strings.SplitN(body, "/", 3)
	if len(parts) != 3 || !corpora.ValidName(parts[0]) || parts[1] == "" || parts[2] == "" || len(address) > 1024 {
		return "", "", "", fmt.Errorf("%w: a corpus item address is corpus:<corpus>/<member>/<item>", corpora.ErrInvalid)
	}
	return parts[0], parts[1], parts[2], nil
}

// ResolveCorpusItem resolves a corpus item address for a run.
func (p *Platform) ResolveCorpusItem(ctx context.Context, req runtime.Request, address string) (CorpusItem, error) {
	name, member, rest, err := ParseCorpusAddress(address)
	if err != nil {
		return CorpusItem{}, err
	}
	a, err := p.corpusAudienceOf(req)
	if err != nil {
		return CorpusItem{}, err
	}
	notFound := fmt.Errorf("%w: no corpus item %q you may read", corpora.ErrNotFound, address)
	c, err := p.Corpora.Get(req.Tenant, name)
	if err != nil || !reaches(req, p.attachedCorpora(req), c) || !p.corpusVisible(a.readers[0], c) {
		return CorpusItem{}, notFound
	}
	item := CorpusItem{Address: address, Corpus: c.Name, Label: c.Label}
	if !p.mayShow(a, c) {
		item.HeldBack = corporaHeldBack(1)
		return item, nil
	}
	if member == "node" {
		if member, rest, err = p.nodeAddress(a, c, rest); err != nil {
			return CorpusItem{}, notFound
		}
	}
	m, ok := memberNamed(c, member)
	if !ok {
		return CorpusItem{}, notFound
	}
	if !p.memberShown(a, m) {
		item.HeldBack = corporaHeldBack(1)
		return item, nil
	}
	switch m.Kind {
	case corpora.Source:
		err = p.resolveDocument(req, a, m.Name, rest, &item)
	case corpora.Catalog:
		err = p.resolveTable(req.Tenant, m.Name, rest, &item)
	}
	if errors.Is(err, knowledge.ErrUpstreamRead) {
		item.HeldBack = "This document is read live at its source: search for it with corpus_search in a direct conversation."
		return item, nil
	}
	if err != nil {
		return CorpusItem{}, notFound
	}
	p.annotateFromGraph(a, c, m, rest, &item)
	if err := p.readCorpora(ctx, req, a, []corpora.Corpus{c}); err != nil {
		return CorpusItem{}, err
	}
	return item, nil
}

func memberNamed(c corpora.Corpus, name string) (corpora.Member, bool) {
	for _, m := range c.Members {
		if m.Name == name {
			return m, true
		}
	}
	return corpora.Member{}, false
}

// nodeAddress turns a graph node into the member and item it stands for.
func (p *Platform) nodeAddress(a corpusAudience, c corpora.Corpus, id string) (string, string, error) {
	g, _, err := p.readerGraph(a.readers[0], []corpora.Corpus{c})
	if err != nil {
		return "", "", err
	}
	for _, n := range g.Nodes {
		if n.ID != id || n.Type == graph.Person {
			continue
		}
		m, ok := memberOf(n.Member)
		if !ok {
			break
		}
		// A node's key ends in its member's name, then the item.
		_, item, ok := strings.Cut(n.Key, "/"+m.Name+"/")
		if !ok {
			break
		}
		return m.Name, item, nil
	}
	return "", "", corpora.ErrNotFound
}

func (p *Platform) resolveDocument(req runtime.Request, a corpusAudience, source, id string, item *CorpusItem) error {
	src, err := p.Knowledge.Get(req.Tenant, source)
	if err != nil {
		return err
	}
	var expected *knowledge.SourceConfig
	if src.Lifecycle != nil {
		cfg := src.Lifecycle.SourceConfig
		expected = &cfg
	}
	doc, texts, err := p.Knowledge.DocumentTextForScope(req.Tenant, source, id, expected)
	if err != nil {
		return err
	}
	item.Kind, item.Title = "document", doc.Title
	item.Text = clip(strings.Join(texts, "\n\n"), maxContextText)
	item.Link = doc.URI
	if item.Link == "" {
		item.Link = "/knowledge/" + item.Corpus + "/sources/" + source
	}
	// Keep citation evidence for what the model is handed, as search does.
	_, err = p.recordSourceToolCitations(req, src, a.person, []knowledge.Hit{{Source: source, DocumentID: doc.ID, Title: doc.Title, URI: doc.URI, Text: item.Text, Mode: knowledge.Indexed}})
	return err
}

func (p *Platform) resolveTable(tenant, connector, path string, item *CorpusItem) error {
	entries, err := p.CatalogStore.List(tenant, []string{connector}, catalog.MaxEntries)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Path != path {
			continue
		}
		item.Kind, item.Title = "table", e.Path
		item.Text = clip(catalog.Text(e), maxContextText)
		item.Link = "/knowledge/" + item.Corpus + "/sources/" + connector
		return nil
	}
	return corpora.ErrNotFound
}

// annotateFromGraph adds who owns an item and whether it is stale.
func (p *Platform) annotateFromGraph(a corpusAudience, c corpora.Corpus, m corpora.Member, item string, out *CorpusItem) {
	g, sls, err := p.readerGraph(a.readers[0], []corpora.Corpus{c})
	if err != nil {
		return
	}
	suffix := "/" + m.Name + "/" + item
	view, err := p.graphView(a.readers[0].Tenant, g, sls, func(n graph.Node) bool { return strings.HasSuffix(n.Key, suffix) }, 1)
	if err != nil || len(view.Nodes) == 0 {
		return
	}
	out.Owner = view.Nodes[0].OwnerName
	out.Stale = view.Nodes[0].Staleness != nil && view.Nodes[0].Staleness.Stale
}
