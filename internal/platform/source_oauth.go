package platform

import (
	"fmt"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

// oauthSource reports whether a source provider is read with the person's own
// native OAuth connection, bound to each source before use.
func oauthSource(provider string) bool { return provider == "drive" || provider == "onedrive" }

func (p *Platform) sourceOAuthProof(who auth.Principal, src knowledge.Source) (string, error) {
	provider, marker, err := p.ConnectionAuth.Binding(who.Tenant, who.Subject, sourceCredentialID(src))
	if err != nil {
		return "", knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	return fmt.Sprintf("%s:%s", provider, marker), nil
}

// ConnectSourceOAuth requires an explicit person action for this exact source
// configuration; an account-level grant alone does not expose a source.
func (p *Platform) ConnectSourceOAuth(who auth.Principal, name, provider string) error {
	src, err := p.SourceAs(who, name)
	if err != nil {
		return knowledge.ErrNotFound
	}
	if who.Kind == auth.Machine || who.Subject == "" {
		return ErrForbidden
	}
	if !oauthSource(provider) || src.Lifecycle == nil || src.Lifecycle.Provider != provider {
		return knowledge.ErrInvalid
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "source.oauth.connect", Resource: "source_connection", ID: name, Scope: "personal", Subject: who.Subject, After: map[string]string{"provider": provider}})
	if err != nil {
		return err
	}
	err = p.ConnectionAuth.Bind(who.Tenant, who.Subject, sourceCredentialID(src), provider)
	p.finishSourceAudit(attempt, err)
	return err
}
