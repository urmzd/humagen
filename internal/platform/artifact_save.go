package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

// Save as Artifact: a message (an agent's answer or a person's message) or a
// scratchpad, kept as a versioned artifact of its conversation through the
// same publication files take (publishFrom): the same title versioning,
// storage limits, provenance and inbox notice. Repeating the same save opens
// its existing artifact; changed scratchpad content makes a new version.

// ArtifactSave names what to save. Source is "run" (an agent's answer),
// "message" (a person's message) or "scratchpad".
type ArtifactSave struct {
	Source string `json:"source"`
	ID     string `json:"id"`
	Title  string `json:"title,omitempty"`
}

// memArea is a fileArea holding one file, for publishing content that is
// not in a conversation's directory.
type memArea struct {
	name string
	data []byte
}

func (a memArea) list(context.Context) ([]sandboxFile, error) {
	return []sandboxFile{{Path: a.name, Size: int64(len(a.data))}}, nil
}

func (a memArea) read(_ context.Context, clean string) ([]byte, error) {
	if clean != a.name {
		return nil, fmt.Errorf("%w: there is no file at %q", ErrSandbox, clean)
	}
	return a.data, nil
}

func (memArea) write(context.Context, string, string) error {
	return fmt.Errorf("%w: a saved message is read only", ErrSandbox)
}

func (memArea) close() error { return nil }

// isReview reports whether an answer is a structured review
// (humagen.github.review.v1), kept as its JSON so its findings stay
// addressable by file and line.
func isReview(text string) bool {
	body := strings.TrimSpace(text)
	body = strings.TrimPrefix(body, "```json")
	body = strings.TrimSuffix(strings.TrimPrefix(body, "```"), "```")
	var head struct {
		Format string `json:"format"`
	}
	return json.Unmarshal([]byte(strings.TrimSpace(body)), &head) == nil && head.Format == github.ReviewFormat
}

// fileTitle makes a file name of a title, with ext unless it has one.
func fileTitle(title, fallback, ext string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = fallback
	}
	if !strings.Contains(title, ".") {
		title += ext
	}
	return title
}

// SaveArtifact saves a message or a scratchpad of a conversation as a
// versioned artifact of it.
func (p *Platform) SaveArtifact(ctx context.Context, who auth.Principal, id string, in ArtifactSave) (artifacts.Artifact, error) {
	if !who.Can(auth.ArtifactsWrite) {
		return artifacts.Artifact{}, auth.ErrForbidden
	}
	th, err := p.workspaceThread(who, id)
	if err != nil {
		return artifacts.Artifact{}, err
	}
	if th.Archive != nil {
		return artifacts.Artifact{}, fmt.Errorf("%w: this conversation is archived", ErrForbidden)
	}
	pub := Publication{Tenant: who.Tenant, Thread: th.ID, Project: th.Project, By: who.Subject}
	var content, name string
	switch in.Source {
	case "run":
		r, err := p.Runs.Get(who.Tenant, in.ID)
		if err != nil || (r.Thread != th.ID && r.ID != th.ID) || !p.Seer(who).Run(r) {
			return artifacts.Artifact{}, threads.ErrNotFound
		}
		if r.Status != runs.Completed && r.Status != runs.Stopped {
			return artifacts.Artifact{}, fmt.Errorf("%w: save an answer once its run has ended", ErrSandbox)
		}
		content = p.ShownRunAs(who, r).Answer
		pub.Run = r.ID
		if isReview(content) {
			name = fileTitle(in.Title, r.Agent+" review", ".review.json")
			pub.Kind, pub.Description = string(artifacts.Data), "Structured review ("+github.ReviewFormat+") saved from "+r.Agent+"'s answer"
		} else {
			name = fileTitle(in.Title, r.Agent+" answer", ".md")
			pub.Description = "Saved from " + r.Agent + "'s answer"
		}
	case "message":
		m, err := p.Threads.Message(who.Tenant, th.ID, in.ID)
		if err != nil {
			return artifacts.Artifact{}, threads.ErrNotFound
		}
		content = m.Text
		name = fileTitle(in.Title, m.Name+" message", ".md")
		pub.Description = "Saved from " + m.Name + "'s message"
	case "scratchpad":
		pad, err := p.Scratchpads.Get(ctx, who.Tenant, th.ID, in.ID)
		if err != nil {
			return artifacts.Artifact{}, scratchpadErr(err)
		}
		content = pad.Content
		name = fileTitle(in.Title, pad.Name, ".md")
		pub.Description = "Saved from the scratchpad " + pad.Name
	default:
		return artifacts.Artifact{}, fmt.Errorf("%w: source is run, message or scratchpad", ErrSandbox)
	}
	if strings.TrimSpace(content) == "" {
		return artifacts.Artifact{}, fmt.Errorf("%w: there is nothing to save", ErrSandbox)
	}
	if len(content) > MaxFileBytes || !utf8.ValidString(content) {
		return artifacts.Artifact{}, fmt.Errorf("%w: at most %d bytes of text are saved", ErrSandbox, MaxFileBytes)
	}
	clean, err := cleanPath(strings.ReplaceAll(name, "/", "-"))
	if err != nil {
		return artifacts.Artifact{}, err
	}
	pub.Path, pub.Title = clean, clean
	identity, _ := json.Marshal([]string{th.ID, in.Source, in.ID, clean, content})
	digest := sha256.Sum256(identity)
	pub.saveIdentity = hex.EncodeToString(digest[:])
	out, err := p.publishFrom(ctx, memArea{name: clean, data: []byte(content)}, pub)
	if errors.Is(err, artifacts.ErrInvalid) {
		return artifacts.Artifact{}, fmt.Errorf("%w: %v", ErrSandbox, err)
	}
	return out, err
}
