package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// MetaPersonalMemoryContext is an immutable private run marker set only when
// personal notes actually entered the turn. It is not model-visible metadata.
const MetaPersonalMemoryContext = "_humagen.context.personal_memory"

// PlacementPreview binds explicit publication consent to current contents and
// destination audience. It contains no former project or private source IDs.
type PlacementPreview struct {
	SummaryRequired bool     `json:"summary_required"`
	Thread          string   `json:"thread"`
	Project         string   `json:"project"`
	Channel         string   `json:"channel"`
	Audience        string   `json:"audience"`
	Runs            int      `json:"runs"`
	Token           string   `json:"token"`
	Exclusions      []string `json:"exclusions"`
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
	summaryRequired := false
	personalMemory := false
	operationalPrivate := false
	for _, guard := range p.publicationGuards {
		if !guard.ConversationOperationsSettled(who.Tenant, th.ID) {
			summaryRequired, operationalPrivate = true, true
		}
	}
	for _, turn := range turns {
		tree, err := p.Runs.Tree(who.Tenant, turn.ID)
		if err != nil {
			return PlacementPreview{}, err
		}
		for _, run := range tree {
			if !run.Status.Terminal() {
				return PlacementPreview{}, fmt.Errorf("%w: wait for active and waiting work to finish before publishing", threads.ErrVersion)
			}
			if run.Client != "" || !wire.SurfaceAddress(run.Tag()).IsZero() {
				return PlacementPreview{}, fmt.Errorf("%w: externally delivered work stays on its original surface", threads.ErrInvalid)
			}
			if wire.Meta(run.Tag())[MetaPersonalMemoryContext] == "true" {
				personalMemory = true
			}
			if p.runNeedsSummary(ctx, run, project) {
				summaryRequired = true
			}

			ids = append(ids, run.ID+":"+run.UpdatedAt.String())
		}
	}
	tree := p.ProjectTree(who.Tenant)
	chain := tree.Chain(project)
	channelRecord, err := p.Projects.Channel(who.Tenant, project, channel)
	if err != nil {
		return PlacementPreview{}, err
	}
	// Every ancestor version participates: a membership change invalidates consent.
	consent := struct {
		Thread      threads.Thread
		Destination []projects.Project
		Source      []projects.Project
		Channel     projects.Channel
		Runs        []string
	}{th, chain, tree.Chain(th.Project), channelRecord, ids}
	raw, err := json.Marshal(consent)
	if err != nil {
		return PlacementPreview{}, err
	}
	digest := sha256.Sum256(raw)
	audience := "Everyone who can read the destination project"
	if target, ok := tree.Get(project); ok && target.PersonalOwner != "" {
		audience = "Only the destination personal project's owner"
	}
	exclusions := []string{"Original execution inputs and private source provenance", "Former personal project identities"}
	if personalMemory {
		exclusions = append(exclusions, "Private personal memory remains in the original conversation")
	}
	if operationalPrivate {
		exclusions = append(exclusions, "Private assignment and handoff contracts remain in the original conversation")
	}
	return PlacementPreview{SummaryRequired: summaryRequired, Thread: th.ID, Project: project, Channel: channel, Audience: audience, Runs: len(ids), Token: hex.EncodeToString(digest[:]), Exclusions: exclusions}, nil
}

func (p *Platform) threadNeedsSummary(ctx context.Context, tenant, thread, destination string) bool {
	for _, guard := range p.publicationGuards {
		if !guard.ConversationOperationsSettled(tenant, thread) {
			return true
		}
	}
	turns, _ := p.Runs.Conversation(tenant, thread)
	for _, turn := range turns {
		if p.RunRequiresReviewedSummary(ctx, turn, destination) {
			return true
		}
	}
	return false
}

// ConversationPublicationGuard lets optional operational subsystems retain
// private contract provenance without a dependency from this lower layer.
// False requires a reviewed summary copy; it never grants raw publication.
type ConversationPublicationGuard interface {
	ConversationOperationsSettled(tenant, thread string) bool
}

// RegisterPublicationGuard is called while initializing an operational
// subsystem. It shares the same admission lock as preview and publication.
func (p *Platform) RegisterPublicationGuard(guard ConversationPublicationGuard) error {
	if guard == nil {
		return fmt.Errorf("%w: a publication guard is required", threads.ErrInvalid)
	}
	p.admission.Lock()
	defer p.admission.Unlock()
	if len(p.publicationGuards) >= 8 {
		return fmt.Errorf("%w: too many publication guards", threads.ErrInvalid)
	}
	p.publicationGuards = append(p.publicationGuards, guard)
	return nil
}

func (p *Platform) guardedThreadWrite(tenant string, snapshot *projects.Tree, project, channel string, write func() (threads.Thread, error)) (threads.Thread, error) {
	destination, err := p.Projects.Channel(tenant, project, channel)
	if err != nil {
		return threads.Thread{}, err
	}
	var result threads.Thread
	err = p.Projects.GuardSnapshot(tenant, snapshot, destination, func() error { var err error; result, err = write(); return err })
	return result, err
}
