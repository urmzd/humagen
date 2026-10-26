package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// PlacementPreview binds explicit publication consent to current contents and
// destination audience. It contains no former project or private source IDs.
type PlacementPreview struct {
	Thread     string   `json:"thread"`
	Project    string   `json:"project"`
	Channel    string   `json:"channel"`
	Audience   string   `json:"audience"`
	Runs       int      `json:"runs"`
	Token      string   `json:"token"`
	Exclusions []string `json:"exclusions"`
}

func (p *Platform) PreviewPlacement(ctx context.Context, who auth.Principal, id, project, channel string) (PlacementPreview, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.RLock()
	defer mu.RUnlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return PlacementPreview{}, err
	}
	if err := p.mayChangeThread(who, th); err != nil {
		return PlacementPreview{}, err
	}
	if !takesPart(th, who.Subject) {
		return PlacementPreview{}, projects.ErrForbidden
	}
	dest, ch, err := p.placeFor(who, project, channel)
	if err != nil {
		return PlacementPreview{}, err
	}
	return p.previewPlacement(ctx, who, th, dest, ch.Name)
}

// Caller serializes publication with both carryOut and direct admission.
func (p *Platform) previewPlacement(ctx context.Context, who auth.Principal, th threads.Thread, project, channel string) (PlacementPreview, error) {
	if th.Surface != nil || th.Announcement != "" {
		return PlacementPreview{}, fmt.Errorf("%w: a surface conversation stays on its surface", threads.ErrInvalid)
	}
	if p.Runs.AnyActive(who.Tenant, func(run runs.Run) bool { return placeOfRun(run).Thread == th.ID }) {
		return PlacementPreview{}, fmt.Errorf("%w: wait for active and waiting work to finish before publishing", threads.ErrVersion)
	}
	turns, _ := p.Runs.Conversation(who.Tenant, th.ID)
	ids := make([]string, 0, len(turns))
	for _, turn := range turns {
		tree, err := p.Runs.Tree(who.Tenant, turn.ID)
		if err != nil {
			return PlacementPreview{}, err
		}
		for _, run := range tree {
			if !run.Status.Terminal() {
				return PlacementPreview{}, fmt.Errorf("%w: wait for active and waiting work to finish before publishing", threads.ErrVersion)
			}
			if run.Client != "" || len(run.Tag().Routes()) > 0 {
				return PlacementPreview{}, fmt.Errorf("%w: externally delivered work stays on its original surface", threads.ErrInvalid)
			}
			rev, err := p.Agents.Get(ctx, who.Tenant, run.Agent, run.Rev)
			if err != nil {
				return PlacementPreview{}, fmt.Errorf("%w: cannot verify the historical source boundary", threads.ErrInvalid)
			}
			if slices.Contains(rev.Spec.Tools, "source_search") {
				return PlacementPreview{}, fmt.Errorf("%w: private connected sources require a reviewed summary before publication", threads.ErrInvalid)
			}
			ids = append(ids, run.ID+":"+run.UpdatedAt.String())
		}
	}
	tree := p.ProjectTree(who.Tenant)
	chain := tree.Chain(project)
	// Every ancestor version participates: a membership change invalidates consent.
	consent := struct {
		Thread      threads.Thread
		Destination []projects.Project
		Channel     string
		Runs        []string
	}{th, chain, channel, ids}
	raw, err := json.Marshal(consent)
	if err != nil {
		return PlacementPreview{}, err
	}
	digest := sha256.Sum256(raw)
	audience := "Everyone who can read the destination project"
	if target, ok := tree.Get(project); ok && target.PersonalOwner != "" {
		audience = "Only the destination personal project's owner"
	}
	return PlacementPreview{Thread: th.ID, Project: project, Channel: channel, Audience: audience, Runs: len(ids), Token: hex.EncodeToString(digest[:]), Exclusions: []string{"Original execution inputs and private source provenance", "Former personal project identities"}}, nil
}
