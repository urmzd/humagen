package platform

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/fiber/pkg/toolprovider"

	"github.com/urmzd/warp/pkg/auth"
	"github.com/urmzd/warp/pkg/clients"
	"github.com/urmzd/warp/pkg/credentials"
	"github.com/urmzd/warp/pkg/credref"
)

// Whose credential a tool uses.
//
// A person-scoped tool provider (toolprovider.ScopePerson) is reached with the
// credential of the person a run acts for, and with nothing when they have
// none: it names no shared credential, so there is none to fall back to.
// fiber decides when to ask. On every turn that expands a grant of such a
// provider it reads the actor on the run's record, never anything the turn
// says, and asks personCredential for theirs. warp decides who that actor is,
// once, when the run is accepted, by setting the actor's User to the person's
// subject:
//
//   - a run from the console or the API, Run Now on a schedule included,
//     acts for the person signed in (ActorOf);
//   - a run a surface asks for a person its client maps (act_as) acts for
//     that person (PersonActor);
//   - a run a Slack connector raised acts for the one person the team's
//     Slack clients map its Slack user to (surface.stamp);
//   - everything else acts for nobody: a machine asking as itself, a
//     schedule firing on its own clock, the platform's own housekeeping, an
//     eval, a run another agent called, and every GitHub actor, since no
//     client maps GitHub logins.
//
// A person gives their credential through ConnectTool, as a reference or, where
// the workspace allows it, a value (pkg/credentials).

// ErrNotPersonScoped refuses a credential for a provider every run reaches
// with the workspace's own credential.
var ErrNotPersonScoped = errors.New("platform: not a provider that takes your own credential")

// errPersonRef refuses a reference. It is one message for every reason, so
// nobody can probe which references other people or the server use.
var errPersonRef = fmt.Errorf("%w: that reference cannot be used by this workspace", credentials.ErrInvalid)

// ActorOf is who a run a signed-in caller asks for acts for. A person acts
// for themselves. A machine acts for nobody: it holds no person's
// credentials, and asks for a person only through act_as.
func ActorOf(who auth.Principal) envelope.Actor {
	a := envelope.Actor{ID: who.Subject, Display: who.Name, Bot: who.Kind == auth.Machine}
	if who.Kind != auth.Machine {
		a.User = who.Subject
	}
	return a
}

// PersonActor is the actor of a run a surface asks for a person its client
// maps to subject (see ActsFor).
func PersonActor(subject string) envelope.Actor {
	return envelope.Actor{ID: subject, Display: subject, User: subject}
}

// surfacePerson is the person a tag a client's connector raised acts for:
// the one subject that the enabled Slack clients serving the client's team
// map the tag's Slack user to. The map is often on another client than the
// one that received the message (a people map needs a machine, so an app
// built on the API holds it). No mapping, or two clients mapping the user to
// different people, is nobody, and so is a bot and every GitHub actor.
func (p *Platform) surfacePerson(c clients.Client, tag envelope.Tag) string {
	id := strings.TrimSpace(tag.Actor.ID)
	if c.Surface != clients.Slack || tag.Actor.Bot || id == "" || c.Identity.Team == "" {
		return ""
	}
	person := ""
	for _, other := range p.Clients.List(c.Tenant) {
		if other.Surface != clients.Slack || !other.Enabled || other.Identity.Team != c.Identity.Team || other.Slack == nil {
			continue
		}
		who := other.Slack.People[id]
		switch {
		case who == "":
		case person == "":
			person = who
		case person != who:
			p.log.Warn("platform: two Slack clients map one Slack user to different people, so the run acts for nobody",
				"tenant", c.Tenant, "team", c.Identity.Team, "user", id)
			return ""
		}
	}
	return person
}

// refAllowed reports whether tenant's people may give ref: it is a
// reference, it starts with one of the starts the operator listed for the
// tenant, and an env: one names a WARP_ variable that is not a sign-in
// secret, as a client's must.
func (p *Platform) refAllowed(tenant, ref string) bool {
	parsed, err := credref.Parse(ref)
	if err != nil || tenant == "" {
		return false
	}
	if parsed.Scheme == credref.Env {
		if _, err := clients.EnvRef(parsed.Raw); err != nil || strings.HasPrefix(parsed.Name, "WARP_OIDC_") {
			return false
		}
	}
	return slices.ContainsFunc(p.cfg.CredentialRefs[tenant], func(start string) bool {
		return start != "" && strings.HasPrefix(parsed.Raw, start)
	})
}

// personCredential is the credential the person a run acts for holds for a
// person-scoped provider. It is fiber's PersonResolver: it is asked on every
// expansion, and answers with the person's own or with nothing. The rules
// are checked here as well as when the credential was given, so one the
// workspace no longer allows is not used, however it was kept.
func (p *Platform) personCredential(_ context.Context, tenant string, prov toolprovider.Provider, actor envelope.Actor) (mcptools.PersonCredential, error) {
	person := actor.User
	if actor.Bot || person == "" {
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	c, ok := p.Credentials.Get(tenant, person, prov.Name)
	if !ok {
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	if c.Kind == credentials.Value {
		if !p.cfg.CredentialValues[tenant] {
			p.log.Warn("platform: a stored credential is not used: this workspace does not let people store values",
				"tenant", tenant, "person", person, "provider", prov.Name)
			return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
		}
		v, ok := p.Credentials.Value(tenant, person, prov.Name)
		if !ok {
			return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
		}
		return mcptools.PersonCredential{Secret: mcptools.NewSecret(v)}, nil
	}
	if !p.refAllowed(tenant, c.Ref) {
		p.log.Warn("platform: a credential reference is not used: this workspace's people may not reference it",
			"tenant", tenant, "person", person, "provider", prov.Name)
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	return mcptools.PersonCredential{Ref: c.Ref}, nil
}

// ToolCredential is a person-scoped tool provider, and whether the person
// asking has connected a credential for it.
type ToolCredential struct {
	Provider  string `json:"provider"`
	Transport string `json:"transport"`
	// Endpoint is where an http provider is reached, so a person sees where
	// their credential is sent. A stdio provider runs on the server and has
	// none here.
	Endpoint string `json:"endpoint,omitempty"`
	// Tools is how many tools the workspace accepted from the provider.
	Tools     int  `json:"tools"`
	Connected bool `json:"connected"`
	// Kind and Ref say how the credential is held: a reference, which is
	// shown, or a value, which never is.
	Kind      credentials.Kind `json:"kind,omitempty"`
	Ref       string           `json:"ref,omitempty"`
	UpdatedAt *time.Time       `json:"updated_at,omitempty"`
}

// ToolCredentials is the person-scoped providers of a workspace, as one
// person stands with each, and what the workspace lets them give.
type ToolCredentials struct {
	Providers []ToolCredential `json:"providers"`
	// Values reports whether this workspace lets people store a value.
	Values bool `json:"values"`
	// References are the starts of the references this workspace's people
	// may give.
	References []string `json:"references"`
}

// maxProviders bounds the providers listed. A person holds at most this many
// credentials too.
const maxProviders = credentials.MaxPerPerson

// ToolCredentials lists the workspace's person-scoped providers and whether
// the caller has connected each.
func (p *Platform) ToolCredentials(ctx context.Context, who auth.Principal) (ToolCredentials, error) {
	all, err := p.ToolProviders.ListProviders(ctx, who.Tenant)
	if err != nil {
		return ToolCredentials{}, err
	}
	out := ToolCredentials{
		Providers: []ToolCredential{}, Values: p.cfg.CredentialValues[who.Tenant],
		References: slices.Clone(p.cfg.CredentialRefs[who.Tenant]),
	}
	if out.References == nil {
		out.References = []string{}
	}
	for _, prov := range all {
		if prov.Scope() != toolprovider.ScopePerson {
			continue
		}
		if len(out.Providers) == maxProviders {
			break
		}
		c, err := p.toolCredential(ctx, who, prov)
		if err != nil {
			return ToolCredentials{}, err
		}
		out.Providers = append(out.Providers, c)
	}
	return out, nil
}

func (p *Platform) toolCredential(ctx context.Context, who auth.Principal, prov toolprovider.Provider) (ToolCredential, error) {
	snap, err := p.ToolProviders.Snapshot(ctx, who.Tenant, prov.Name, 0)
	if err != nil {
		return ToolCredential{}, err
	}
	c := ToolCredential{Provider: prov.Name, Transport: string(prov.Transport), Tools: len(snap.Tools)}
	if prov.Transport == toolprovider.TransportHTTP {
		c.Endpoint = prov.Endpoint
	}
	if who.Kind == auth.Machine {
		return c, nil
	}
	if held, ok := p.Credentials.Get(who.Tenant, who.Subject, prov.Name); ok {
		at := held.UpdatedAt
		c.Connected, c.Kind, c.Ref, c.UpdatedAt = true, held.Kind, held.Ref, &at
	}
	return c, nil
}

// personScoped returns a provider of the caller's workspace that takes each
// person's own credential.
func (p *Platform) personScoped(ctx context.Context, tenant, name string) (toolprovider.Provider, error) {
	if !toolprovider.ValidName(name) {
		return toolprovider.Provider{}, fmt.Errorf("%w: no tool provider %q", ErrProviderNotFound, name)
	}
	prov, err := p.ToolProviders.GetProvider(ctx, tenant, name)
	switch {
	case errors.Is(err, toolprovider.ErrNotFound):
		return toolprovider.Provider{}, fmt.Errorf("%w: no tool provider %q", ErrProviderNotFound, name)
	case err != nil:
		return toolprovider.Provider{}, err
	case prov.Scope() != toolprovider.ScopePerson:
		return toolprovider.Provider{}, fmt.Errorf("%w: %s is reached with the workspace's own credential, so there is nothing of yours to connect", ErrNotPersonScoped, name)
	}
	return prov, nil
}

// ErrProviderNotFound reports a tool provider the caller's workspace does
// not have.
var ErrProviderNotFound = errors.New("platform: tool provider not found")

// ConnectTool gives the caller's own credential for a person-scoped provider, in
// place of the one they had. A person gives it for themselves only: the
// route takes no subject, and a machine has no credentials of its own.
//
// A reference must start with one of the starts the operator listed for the
// workspace, and is not read here, so connecting says nothing about whether
// it names anything. A value is accepted only where the workspace lets people
// store one, and is then kept as a client's secret is.
func (p *Platform) ConnectTool(ctx context.Context, who auth.Principal, provider string, in credentials.Input) (ToolCredential, error) {
	// Looked up first, so another workspace's provider is not found
	// whoever asks.
	prov, err := p.personScoped(ctx, who.Tenant, provider)
	if err != nil {
		return ToolCredential{}, err
	}
	if who.Kind == auth.Machine {
		return ToolCredential{}, fmt.Errorf("%w: a machine has no credentials of its own; each person connects theirs", ErrForbidden)
	}
	if err := in.Check(); err != nil {
		return ToolCredential{}, err
	}
	switch {
	case in.Value != "" && !p.cfg.CredentialValues[who.Tenant]:
		return ToolCredential{}, fmt.Errorf("%w: this workspace does not keep people's credentials; give a reference to where yours is kept", ErrForbidden)
	case in.Ref != "" && !p.refAllowed(who.Tenant, in.Ref):
		return ToolCredential{}, errPersonRef
	}
	held, err := p.Credentials.Put(who.Tenant, who.Subject, prov.Name, in, p.now())
	if err != nil {
		return ToolCredential{}, err
	}
	p.log.Info("platform: credential connected", "tenant", who.Tenant, "subject", who.Subject, "provider", prov.Name, "kind", held.Kind)
	return p.toolCredential(ctx, who, prov)
}

// DisconnectTool removes the caller's own credential for a provider, and the
// value if they had stored one. Their runs then get no tools from it. Nothing
// of theirs is ErrNotFound.
func (p *Platform) DisconnectTool(_ context.Context, who auth.Principal, provider string) error {
	switch {
	case who.Kind == auth.Machine:
		// A machine holds none, so it finds none to remove.
		return fmt.Errorf("%w: a machine holds no credentials", credentials.ErrNotFound)
	case !toolprovider.ValidName(provider):
		return fmt.Errorf("%w: no tool provider %q", ErrProviderNotFound, provider)
	}
	// A credential outlives its provider leaving the file, so it can be
	// removed whatever the provider is now.
	if err := p.Credentials.Delete(who.Tenant, who.Subject, provider); err != nil {
		return err
	}
	p.log.Info("platform: credential removed", "tenant", who.Tenant, "subject", who.Subject, "provider", provider)
	return nil
}
