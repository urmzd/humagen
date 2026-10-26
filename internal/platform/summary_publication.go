package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

type SummaryPublication struct {
	Project        string `json:"project"`
	Channel        string `json:"channel,omitempty"`
	Summary        string `json:"summary"`
	PlacementToken string `json:"placement_token"`
}

// PublishSummary creates only reviewed human text in the destination. It copies
// no source-thread references, execution events, source names or capabilities.
// The original stays private and no agent is started by publishing the copy.
func (p *Platform) PublishSummary(ctx context.Context, who auth.Principal, id string, in SummaryPublication) (threads.Thread, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	source, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.mayChangeThread(who, source); err != nil {
		return threads.Thread{}, err
	}
	if source.Visibility != threads.Private || !takesPart(source, who.Subject) {
		return threads.Thread{}, fmt.Errorf("%w: only a participating owner publishes a private conversation summary", threads.ErrInvalid)
	}
	project, channel, err := p.placeFor(who, in.Project, in.Channel)
	if err != nil {
		return threads.Thread{}, err
	}
	preview, err := p.previewPlacement(ctx, who, source, project, channel.Name)
	if err != nil {
		return threads.Thread{}, err
	}
	if in.PlacementToken == "" || in.PlacementToken != preview.Token {
		return threads.Thread{}, fmt.Errorf("%w: review the current destination audience before publishing", threads.ErrVersion)
	}
	text := strings.TrimSpace(in.Summary)
	if err := threads.CheckText(text); err != nil {
		return threads.Thread{}, err
	}
	if text == "" {
		return threads.Thread{}, fmt.Errorf("%w: review and write the summary to publish", threads.ErrInvalid)
	}
	by, name, _, _, err := p.author(who, nil)
	if err != nil {
		return threads.Thread{}, err
	}
	digest := sha256.Sum256([]byte(who.Tenant + "\x00" + id + "\x00" + in.PlacementToken + "\x00" + text))
	targetID := "th-" + hex.EncodeToString(digest[:8])
	if existing, err := p.Threads.Get(who.Tenant, targetID); err == nil {
		return existing, nil
	}
	change := audit.Change{Route: "POST /api/threads/{id}/publication", Rule: "runs:write; participating owner; destination membership", Action: "publish_summary", Resource: "conversation", ID: id, Scope: "project", Project: source.Project, Thread: id, After: map[string]string{"project": project, "channel": channel.Name, "copy": targetID}}
	if pr, ok := p.ProjectTree(who.Tenant).Get(source.Project); ok && pr.PersonalOwner != "" {
		change.Scope, change.Subject = "personal", pr.PersonalOwner
	}
	attempt, err := p.BeginAudit(who, change)
	if err != nil {
		return threads.Thread{}, err
	}
	target, _, err := p.Threads.Start(who.Tenant, threads.Thread{ID: targetID, Project: project, Channel: channel.Name, Visibility: threads.Shared}, threads.Message{By: by, Name: name, Text: text, Via: "console"}, p.now())
	if auditErr := attempt.Finish(err); auditErr != nil {
		p.log.Error("platform: summary publication audit completion pending", "audit", attempt.ID(), "error", auditErr)
	}
	return target, err
}
