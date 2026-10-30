// Package runctx is a run's context: who the run acts for and what the
// settings of its place and its person resolve to, decided once when the run
// is admitted and pinned to it.
//
// The context is written into the run's trigger, with its digest, the way a
// definition is pinned. Every attempt of the run reads the same trigger back,
// so a retry or a replay sees exactly what the first attempt saw, and a
// change to a project's or a person's settings while the run works reaches
// only runs admitted after it. A context whose digest does not match is
// refused, never repaired.
//
// A called run inherits its caller's context (Narrow). It keeps the caller's
// person and can only tighten the caller's ceilings.
package runctx

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/settings"
)

// The keys a context is pinned under. They are private
// (wire.PrivateMetaPrefix), so fiber keeps them out of what a model is sent.
const (
	MetaContext = wire.PrivateMetaPrefix + "humagen.context"
	MetaDigest  = wire.PrivateMetaPrefix + "humagen.context.digest"
)

// maxPinned bounds a pinned context, in bytes.
const maxPinned = 64 << 10

// ErrInvalid reports a pinned context that cannot be read or does not match
// its digest.
var ErrInvalid = errors.New("runctx: invalid")

// Context is what a run was admitted with.
type Context struct {
	// Person is the subject the run acts for: whoever posted the message
	// that started it, in a shared thread too. Empty for a run that acts
	// for nobody, such as a schedule's.
	Person string `json:"person,omitempty"`
	// Settings are the run's ceilings and preferences, resolved from the
	// workspace, its project chain and its person.
	Settings settings.Resolved `json:"settings"`
	// ToolServers are the person's own tool servers on hosts the ceilings
	// allow. Never nil.
	ToolServers []personctx.ToolServer `json:"tool_servers"`
	// Memory names the person's memory volume.
	Memory string `json:"memory,omitempty"`
}

// New is the context a run acting for person gets from its resolved
// settings and the person's own context: their tool servers on hosts the
// ceilings allow, and their memory volume.
func New(person string, resolved settings.Resolved, own personctx.Context) Context {
	c := Context{Person: person, Settings: resolved, ToolServers: []personctx.ToolServer{}}
	if person == "" {
		return c
	}
	c.Memory = own.Memory
	for _, t := range own.ToolServers {
		if resolved.Ceilings.AllowsToolServer(t.Host()) {
			c.ToolServers = append(c.ToolServers, t)
		}
	}
	return c
}

// Narrow is the context a run called by this one gets: the same person, its
// ceilings tightened by by, and only what still fits them.
func (c Context) Narrow(by settings.Ceilings) Context {
	out := Context{Person: c.Person, Settings: c.Settings.Narrow(by), Memory: c.Memory, ToolServers: []personctx.ToolServer{}}
	for _, t := range c.ToolServers {
		if out.Settings.Ceilings.AllowsToolServer(t.Host()) {
			out.ToolServers = append(out.ToolServers, t)
		}
	}
	return out
}

func (c Context) encode() ([]byte, error) {
	if c.ToolServers == nil {
		c.ToolServers = []personctx.ToolServer{}
	}
	return json.Marshal(c)
}

// Digest is the context's content hash, "sha256:<hex>".
func (c Context) Digest() (string, error) {
	b, err := c.encode()
	if err != nil {
		return "", err
	}
	return digestOf(b), nil
}

func digestOf(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// Pin writes c and its digest into meta, in place of whatever was there.
func Pin(meta map[string]string, c Context) error {
	b, err := c.encode()
	if err != nil {
		return err
	}
	if len(b) > maxPinned {
		return fmt.Errorf("%w: the run's context is %d bytes, more than %d", ErrInvalid, len(b), maxPinned)
	}
	meta[MetaContext] = string(b)
	meta[MetaDigest] = digestOf(b)
	return nil
}

// Unpin removes a context from meta: what a trigger says about one is never
// believed.
func Unpin(meta map[string]string) {
	delete(meta, MetaContext)
	delete(meta, MetaDigest)
}

// Of reads the context pinned in meta. ok is false when none is pinned. One
// that cannot be read, or that does not match its digest, is ErrInvalid.
func Of(meta map[string]string) (c Context, ok bool, err error) {
	raw, has := meta[MetaContext]
	digest, hasDigest := meta[MetaDigest]
	if !has && !hasDigest {
		return Context{}, false, nil
	}
	if !has || !hasDigest || len(raw) > maxPinned || digestOf([]byte(raw)) != digest {
		return Context{}, true, fmt.Errorf("%w: the run's pinned context does not match its digest", ErrInvalid)
	}
	if err := json.Unmarshal([]byte(raw), &c); err != nil {
		return Context{}, true, fmt.Errorf("%w: the run's pinned context: %w", ErrInvalid, err)
	}
	if c.ToolServers == nil {
		c.ToolServers = []personctx.ToolServer{}
	}
	return c, true, nil
}
