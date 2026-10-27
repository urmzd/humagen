package platform

import (
	"cmp"
	"errors"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/textindex"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// discoverContent uses the authoritative stores' derived indexes. Current read
// access is checked before a result or excerpt is emitted.
func (p *Platform) discoverContent(who auth.Principal, q DiscoveryQuery, seer *Seer) ([]DiscoveryItem, error) {
	out := []DiscoveryItem{}
	needle := strings.ToLower(q.Q)
	wants := func(kind string) bool { return len(q.Kinds) == 0 || slices.Contains(q.Kinds, kind) }
	matches := func(values ...string) bool {
		return needle == "" || slices.ContainsFunc(values, func(value string) bool { return strings.Contains(strings.ToLower(value), needle) })
	}
	// A one-year window, 1000 message/run candidates and 50 source buckets
	// bound content search independently of the returned navigation rows.
	since := p.now().AddDate(-1, 0, 0)
	if wants("message") && who.Can(auth.RunsRead) && needle != "" {
		allowed := func(id, project string) bool {
			return (q.Project == nil || project == *q.Project) && seer.Conversation(id, project)
		}
		readable := map[string]bool{}
		for _, th := range p.Threads.Records(who.Tenant, func(threads.Thread) bool { return true }) {
			if allowed(th.ID, th.Project) {
				readable[th.ID] = true
			}
		}
		for _, m := range p.Threads.Search(who.Tenant, q.Q, since, 1000, func(th threads.Thread) bool { return readable[th.ID] }) {
			th, err := p.ThreadRecord(who.Tenant, m.Thread)
			if err != nil || !allowed(th.ID, th.Project) {
				continue
			}
			out = append(out, DiscoveryItem{Kind: "message", ID: m.Thread + ":" + m.ID, Title: p.discoveryTitle(who.Tenant, th, m.Text), Thread: th.ID, Message: m.ID, Project: th.Project, Channel: th.Channel, Snippet: textindex.Snippet(m.Text, q.Q)})
			if len(out) >= q.Limit*3 {
				break
			}
		}
		// Resolve fork/source access before entering the run index's read lock.
		for _, summary := range p.Runs.Threads(who.Tenant, 0) {
			at := p.CurrentPlace(who.Tenant, Place{Project: summary.Project, Thread: summary.ID})
			readable[summary.ID] = allowed(summary.ID, at.Project)
		}
		for _, r := range p.Runs.Search(who.Tenant, q.Q, since, 1000, func(r runs.Run) bool { return readable[r.Thread] }) {
			th, err := p.ThreadRecord(who.Tenant, r.Thread)
			if err != nil || !allowed(th.ID, th.Project) || !seer.Run(r) {
				continue
			}
			r = p.ShownRunAs(who, r)
			if !textindex.Matches(r.Text+"\n"+r.Answer, q.Q) {
				continue
			}
			text := r.Answer
			if text == "" {
				text = r.Text
			}
			out = append(out, DiscoveryItem{Kind: "message", ID: r.ID, Title: p.discoveryTitle(who.Tenant, th, r.Text), Thread: th.ID, Message: r.ID, Project: th.Project, Channel: th.Channel, Snippet: textindex.Snippet(text, q.Q)})
			if len(out) >= q.Limit*4 {
				break
			}
		}
	}
	if wants("file") && who.Can(auth.ArtifactsRead) {
		files, _, err := p.Artifacts.Page(who.Tenant, artifacts.Query{Limit: q.Limit, Search: q.Q, Keep: func(a artifacts.Artifact) bool {
			shown := p.ShownArtifact(who.Tenant, a)
			return seer.Artifact(a) && (q.Project == nil || shown.Project == *q.Project) && (q.Q == "" || textindex.Matches(shown.Title+"\n"+shown.Description+"\n"+shown.Preview+"\n"+shown.SearchText, q.Q))
		}})
		if err != nil {
			return nil, err
		}
		for _, a := range files {
			a = p.ShownArtifact(who.Tenant, a)
			out = append(out, DiscoveryItem{Kind: "file", ID: a.ID, Title: boundedTitle(a.Title), Thread: a.Thread, Project: a.Project, Snippet: textindex.Snippet(cmp.Or(a.SearchText, a.Description, a.Preview), q.Q)})
		}
	}
	if wants("source") && who.Can(auth.KnowledgeRead) {
		indexed := []string{}
		expected := map[string]*knowledge.SourceConfig{}
		for _, source := range p.SourcesAs(who) {
			if len(indexed) >= 50 {
				break
			}
			if source.Lifecycle != nil && (source.Lifecycle.Mode == knowledge.Live || (q.Project != nil && source.Lifecycle.Project != *q.Project)) {
				continue
			}
			if source.Lifecycle == nil && q.Project != nil && *q.Project != "" {
				continue
			}
			indexed = append(indexed, source.Name)
			if source.Lifecycle == nil {
				expected[source.Name] = nil
			} else {
				cfg := source.Lifecycle.SourceConfig
				expected[source.Name] = &cfg
			}
			if matches(source.Name, source.Description) {
				out = append(out, DiscoveryItem{Kind: "source", ID: source.Name, Source: source.Name, Title: boundedTitle(source.Name), Snippet: boundedTitle(source.Description)})
			}
		}
		if len(indexed) > 0 && needle != "" {
			hits, err := p.Knowledge.SearchForScopes(who.Tenant, indexed, q.Q, q.Limit, "", expected)
			if err != nil && !errors.Is(err, knowledge.ErrInvalid) {
				return nil, err
			}
			seen := map[string]bool{}
			for _, hit := range hits {
				key := hit.Source + ":" + hit.DocumentID
				if seen[key] || !p.SourceVisible(who, hit.Source) {
					continue
				}
				seen[key] = true
				out = append(out, DiscoveryItem{Kind: "source", ID: key, Source: hit.Source, Title: boundedTitle(hit.Title), Snippet: textindex.Snippet(hit.Text, q.Q)})
			}
		}
	}
	return out, nil
}

// Unnamed conversations use their first human message, like the native thread
// list, so a content match still identifies the conversation it belongs to.
func (p *Platform) discoveryTitle(tenant string, th threads.Thread, fallback string) string {
	if th.Title != "" {
		return boundedTitle(th.Title)
	}
	if first, err := p.Threads.Message(tenant, th.ID, "m-1"); err == nil {
		return boundedTitle(first.Text)
	}
	return boundedTitle(fallback)
}
