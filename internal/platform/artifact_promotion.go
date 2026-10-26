package platform

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/nativefiles"
	"github.com/urmzd/humagen/pkg/threads"
)

const MaxPromotionBytes = 32000

// acceptedArtifact binds a promotion to one visible immutable version in a
// shared conversation. A private output cannot silently acquire a wider audience.
func (p *Platform) acceptedArtifact(ctx context.Context, who auth.Principal, id string, version int) (artifacts.Artifact, string, string, error) {
	if !who.Can(auth.ArtifactsRead) {
		return artifacts.Artifact{}, "", "", auth.ErrForbidden
	}
	artifact, err := p.Artifacts.Get(who.Tenant, id)
	if err != nil || !p.Seer(who).Artifact(artifact) {
		return artifacts.Artifact{}, "", "", artifacts.ErrNotFound
	}
	if version != artifact.Version {
		return artifacts.Artifact{}, "", "", fmt.Errorf("%w: the immutable version does not match", artifacts.ErrInvalid)
	}
	thread, err := p.ThreadRecord(who.Tenant, artifact.Thread)
	if err != nil || thread.Visibility != threads.Shared || !p.Seer(who).Conversation(thread.ID, thread.Project) {
		return artifacts.Artifact{}, "", "", fmt.Errorf("%w: publish a reviewed shared result before promoting it", artifacts.ErrInvalid)
	}
	_, bytes, err := p.OpenArtifact(ctx, who, id)
	if err != nil {
		return artifacts.Artifact{}, "", "", err
	}
	text := ""
	if isText(artifact.Mime) && utf8.Valid(bytes) && !strings.ContainsRune(string(bytes), 0) {
		text = string(bytes)
	} else {
		text, err = nativefiles.Text(artifact.Title, bytes)
		if err != nil {
			return artifacts.Artifact{}, "", "", fmt.Errorf("%w: this version has no promotable text", artifacts.ErrInvalid)
		}
	}
	if len(text) == 0 || len(text) > MaxPromotionBytes {
		return artifacts.Artifact{}, "", "", fmt.Errorf("%w: a promotable result must contain 1 to %d bytes; publish a shorter reviewed summary", artifacts.ErrInvalid, MaxPromotionBytes)
	}
	return artifact, thread.Project, text, nil
}

// PromoteArtifactKnowledge copies the exact accepted version into an indexed
// manual source of the same project or a narrower project, with provenance.
func (p *Platform) PromoteArtifactKnowledge(ctx context.Context, who auth.Principal, id string, version int, source string) (knowledge.Document, error) {
	if !who.Can(auth.KnowledgeWrite) {
		return knowledge.Document{}, auth.ErrForbidden
	}
	found, err := p.Artifacts.Get(who.Tenant, id)
	if err != nil {
		return knowledge.Document{}, artifacts.ErrNotFound
	}
	lock := p.moves.of(who.Tenant, found.Thread)
	lock.Lock()
	defer lock.Unlock()
	artifact, project, text, err := p.acceptedArtifact(ctx, who, id, version)
	if err != nil {
		return knowledge.Document{}, err
	}
	if !p.SourceVisible(who, source) {
		return knowledge.Document{}, knowledge.ErrNotFound
	}
	target, err := p.Knowledge.Get(who.Tenant, source)
	if err != nil {
		return knowledge.Document{}, err
	}
	destination := ""
	var expected *knowledge.SourceConfig
	if target.Lifecycle != nil {
		if target.Lifecycle.Mode != knowledge.Indexed || target.Lifecycle.Provider != "manual" {
			return knowledge.Document{}, fmt.Errorf("%w: select a manual indexed source", knowledge.ErrInvalid)
		}
		destination = target.Lifecycle.Project
		copy := target.Lifecycle.SourceConfig
		expected = &copy
	}
	if destination != project {
		return knowledge.Document{}, fmt.Errorf("%w: select a source in this shared result's project", knowledge.ErrInvalid)
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "artifact.promote_knowledge", Resource: "artifact", ID: id, Scope: "project", Project: project, Thread: artifact.Thread, After: map[string]string{"version": strconv.Itoa(version), "source": source}})
	if err != nil {
		return knowledge.Document{}, err
	}
	document, err := p.Knowledge.AddForScope(who.Tenant, source, fmt.Sprintf("%s v%d", artifact.Title, version), fmt.Sprintf("humagen://artifacts/%s?version=%d", id, version), text, who.Subject, p.now(), expected)
	if finishErr := attempt.Finish(err); finishErr != nil {
		p.log.Error("platform: artifact promotion audit remains pending", "attempt", attempt.ID(), "error", finishErr)
	}
	return document, err
}

// PromoteArtifactMemory proposes the exact accepted shared result. It never
// truncates an oversized file or approves memory on the reader's behalf.
func (p *Platform) PromoteArtifactMemory(ctx context.Context, who auth.Principal, id string, version int) (memory.Note, error) {
	if !who.Can(auth.RunsWrite) {
		return memory.Note{}, auth.ErrForbidden
	}
	found, err := p.Artifacts.Get(who.Tenant, id)
	if err != nil {
		return memory.Note{}, artifacts.ErrNotFound
	}
	lock := p.moves.of(who.Tenant, found.Thread)
	lock.Lock()
	defer lock.Unlock()
	_, project, text, err := p.acceptedArtifact(ctx, who, id, version)
	if err != nil {
		return memory.Note{}, err
	}
	return p.ProposeProjectMemory(who, project, text, fmt.Sprintf("humagen://artifacts/%s?version=%d", id, version))
}
