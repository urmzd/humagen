package platform

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/knowledge"
)

const maxSourceGrants = 1024
const sourceGrantTTL = 2 * time.Minute

type sourceGrant struct {
	Tenant, Run, Name, Project, Person string
	Config                             knowledge.SourceConfig
	Expires                            time.Time
}
type sourceGrounding struct {
	mu     sync.Mutex
	grants map[string]sourceGrant
}

// Scoped sources cross fiber's tenant/source-only retriever seam as one-use,
// unguessable handles. They carry no secret and never change the stored agent
// revision. The grant pins the exact source audience authorized for that turn.
func (p *Platform) grantSource(req runtime.Request, src knowledge.Source, project, person string) (string, error) {
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return "", err
	}
	token := "sourcectx-" + hex.EncodeToString(nonce[:])
	at := p.now()
	p.grounding.mu.Lock()
	defer p.grounding.mu.Unlock()
	if p.grounding.grants == nil {
		p.grounding.grants = map[string]sourceGrant{}
	}
	for key, grant := range p.grounding.grants {
		if !at.Before(grant.Expires) {
			delete(p.grounding.grants, key)
		}
	}
	if len(p.grounding.grants) >= maxSourceGrants {
		return "", fmt.Errorf("%w: source context capacity reached; retry", knowledge.ErrExists)
	}
	p.grounding.grants[token] = sourceGrant{Tenant: req.Tenant, Run: req.RunID, Name: src.Name, Project: project, Person: person, Config: src.Lifecycle.SourceConfig, Expires: at.Add(sourceGrantTTL)}
	return token, nil
}
func (p *Platform) sourceGrant(tenant, token string, take bool) (sourceGrant, bool) {
	p.grounding.mu.Lock()
	defer p.grounding.mu.Unlock()
	grant, ok := p.grounding.grants[token]
	if !ok {
		return sourceGrant{}, false
	}
	if !p.now().Before(grant.Expires) {
		delete(p.grounding.grants, token)
		return sourceGrant{}, false
	}
	if grant.Tenant != tenant {
		return sourceGrant{}, false
	}
	if take {
		delete(p.grounding.grants, token)
	}
	return grant, true
}
func (p *Platform) sourceNamesForTools(req runtime.Request) []string {
	names := []string{}
	for _, src := range req.Revision.Spec.Sources {
		name := src.Name
		if grant, ok := p.sourceGrant(req.Tenant, name, false); ok {
			if grant.Run != req.RunID {
				continue
			}
			name = grant.Name
		}
		names = append(names, name)
	}
	return names
}
func (p *Platform) withSourceGrounding(req runtime.Request, project, person string) (runtime.Request, error) {
	spec := req.Revision.Spec
	spec.Sources = slices.Clone(spec.Sources)
	out := []agentrt.Source{}
	minted := []string{}
	cleanup := func() {
		p.grounding.mu.Lock()
		defer p.grounding.mu.Unlock()
		for _, token := range minted {
			delete(p.grounding.grants, token)
		}
	}
	for _, declared := range spec.Sources {
		src, err := p.Knowledge.Get(req.Tenant, declared.Name)
		if err != nil || !p.runtimeSourceRecordAllowed(req.Tenant, src, project, person) {
			continue
		}
		if src.Lifecycle != nil && src.Lifecycle.Project != "" && src.Lifecycle.Mode != knowledge.Live {
			token, err := p.grantSource(req, src, project, person)
			if err != nil {
				cleanup()
				return req, err
			}
			minted = append(minted, token)
			declared.Name = token
		}
		out = append(out, declared)
	}
	spec.Sources = out
	req.Revision.Spec = spec
	return req, nil
}

// releaseSourceGrounding runs when the outer executor returns, including
// validation/model failures and cancellation before a retriever was built.
func (p *Platform) releaseSourceGrounding(req runtime.Request) {
	p.grounding.mu.Lock()
	defer p.grounding.mu.Unlock()
	for _, source := range req.Revision.Spec.Sources {
		grant, ok := p.grounding.grants[source.Name]
		if ok && grant.Tenant == req.Tenant && grant.Run == req.RunID {
			delete(p.grounding.grants, source.Name)
		}
	}
}
