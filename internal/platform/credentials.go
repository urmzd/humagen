package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/fiber/pkg/toolprovider"

	"github.com/urmzd/humagen/internal/legacy"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/runs"
)

// Whose credential a tool uses.
//
// A person-scoped tool provider (toolprovider.ScopePerson) is reached with the
// credential of the person a run acts for, and with nothing when they have
// none: it names no shared credential, so there is none to fall back to.
// fiber decides when to ask. On every turn that expands a grant of such a
// provider it reads the actor on the run's record, never anything the turn
// says, and asks personCredential for theirs. humagen decides who that actor is,
// once, when the run is accepted, by setting the actor's User to the person's
// subject:
//
//   - a run from the console or the API acts for the person signed in
//     (ActorOf), and so does Run Now on a schedule when they wrote what it
//     runs, and otherwise it acts for nobody (Schedules.Authored);
//   - a run a surface asks for a person its client maps (act_as) acts for
//     that person (PersonActor);
//   - a run a Slack connector raised acts for the one person the team's
//     Slack clients map its Slack user to (surface.stamp);
//   - a run a message in a shared thread starts acts for the person who
//     posted it, or for whom a surface's credential posted it, and never
//     for another participant, though it is sent the room (carryOut);
//   - a retry of a failed turn acts for whoever the turn acted for, and only
//     that person may ask for one (mayRetry); a turn that answered a message
//     in a shared thread answers that message again (retryAnswer);
//   - everything else acts for nobody: a machine asking as itself, a
//     schedule firing on its own clock, the platform's own housekeeping, an
//     eval, a run another agent called, and an unmapped GitHub actor. GitHub logins are mapped only by
//     their own verified installation.
//
// What a person answers a run steers what its tools are asked next, and a
// called run's answer is its caller's. So a run that uses the credential of
// the person it acts for, or one it called, asks that person alone its
// replies and choices, also when it asks the experts, and anyone else
// answers its approvals as asked, with no edit and no note the model would
// be shown (maySteer, steeredFor).
//
// A person gives their credential through ConnectTool, as a reference or, where
// the workspace allows it, a value (pkg/credentials). A reference is only
// ever the person's own: under a start the operator listed, with their
// subject in it (credentials.Owns), checked when it is given and again on
// every turn that uses it.

// ErrNotPersonScoped refuses a credential for a provider every run reaches
// with the workspace's own credential.
var ErrNotPersonScoped = errors.New("platform: not a provider that takes your own credential")

// errPersonRef refuses a reference. It is one message for every reason, so
// nobody can probe which references other people or the server use.
var errPersonRef = fmt.Errorf("%w: that reference cannot be used by this workspace", credentials.ErrInvalid)

// ActorOf is who a run a signed-in caller asks for acts for. A person acts
// for themselves. A machine acts for nobody: it holds no person's
// credentials, and asks for a person only through act_as.
func ActorOf(who auth.Principal) event.Actor {
	a := event.Actor{ID: who.Subject, Display: who.Name, Bot: who.Kind == auth.Machine}
	if who.Kind != auth.Machine {
		a.User = who.Subject
	}
	return a
}

// namedActor is an actor as a run records it: the person it acts for called
// what the directory calls them, as every route names them, rather than by
// the name their sign-in carried, which can be missing, or by their subject,
// which is all a surface asking for them gives and can hold an address. A
// person the directory has not recorded keeps the name they were given,
// unless it is their subject or an address. A caller that acts for nobody
// is named as it named itself.
func (p *Platform) namedActor(tenant string, a event.Actor) event.Actor {
	person := personOf(a)
	if person == "" {
		return a
	}
	who := p.shower(tenant).person(person)
	given := a.Display
	if given == person || strings.Contains(given, "@") {
		given = ""
	}
	a.Display = cmp.Or(who.Name, given, who.Handle, who.ShownSubject(), someone)
	return a
}

// personOf is the person an actor acts for, as personCredential reads it:
// their subject, or "" for nobody. A bot acts for nobody, whatever it names.
func personOf(a event.Actor) string {
	if a.Bot {
		return ""
	}
	return a.User
}

// ownCredentialOf is the person whose own credentials a run's tools use: the
// person it acts for, when its agent's revision grants a provider that takes
// each person's own credential. "" when it acts for nobody, or reaches only
// shared credentials. A revision that cannot be read counts as one that
// does, and so does a granted provider that cannot be found.
func (p *Platform) ownCredentialOf(ctx context.Context, run runs.Run) string {
	person := personOf(run.Actor)
	if person == "" {
		return ""
	}
	rev, err := p.Catalog().Get(ctx, run.Tenant, run.Agent, run.Rev)
	if err != nil || p.grantsPersonScoped(ctx, run.Tenant, rev.Spec.Tools, time.Time{}) {
		return person
	}
	return ""
}

// steeredFor is the person whose own credentials an answer to what a run
// asks may reach: ownCredentialOf the run, or of a run that called it, up to
// its root, since a called run's answer is its caller's tool result. "" when
// none uses a person's own credentials. A caller that cannot be read makes
// it nobody, an audience no reader is in, so nobody's answer is taken.
func (p *Platform) steeredFor(ctx context.Context, run runs.Run) string {
	for range run.Depth + 1 {
		if person := p.ownCredentialOf(ctx, run); person != "" {
			return person
		}
		if run.Parent == "" {
			return ""
		}
		parent, err := p.Runs.Get(run.Tenant, run.Parent)
		if err != nil {
			return nobody
		}
		run = parent
	}
	return nobody
}

// PersonActor is the actor of a run a surface asks for a person its client
// maps to subject (see ActsFor).
func PersonActor(subject string) event.Actor {
	return event.Actor{ID: subject, Display: subject, User: subject}
}

// surfacePerson is the person a tag a client's connector raised acts for:
// the one subject that the enabled Slack clients serving the client's team
// map the tag's Slack user to. The map is often on another client than the
// one that received the message (a people map needs a machine, so an app
// built on the API holds it). No mapping, or two clients mapping the user to
// different people, is nobody. GitHub uses only its own installation’s exact
// verified login map. A bot always acts for nobody.
func (p *Platform) surfacePerson(c clients.Client, tag event.Envelope) string {
	id := strings.TrimSpace(wire.ActorOf(tag).ID)
	if c.Surface == clients.GitHub {
		if !c.Enabled || wire.ActorOf(tag).Bot || c.GitHub == nil {
			return ""
		}
		return c.GitHub.People[wire.ActorOf(tag).ID]
	}
	if c.Surface != clients.Slack || wire.ActorOf(tag).Bot || id == "" || c.Identity.Team == "" {
		return ""
	}
	person := ""
	all, err := p.Clients.List(c.Tenant)
	if err != nil {
		p.log.Error("platform: clients could not be read, so the run acts for nobody", "tenant", c.Tenant, "error", err)
		return ""
	}
	for _, other := range all {
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

// refAllowed reports whether subject, one of tenant's people, may give ref:
// it is a reference, it is theirs under one of the starts the operator
// listed for the tenant (credentials.Owns: the start with their subject in
// it), and an env: one names a HUMAGEN_ variable that is not a sign-in secret,
// as a client's must. Nobody else's subject makes it theirs, so a person
// cannot give a reference to somebody else's credential.
func (p *Platform) refAllowed(tenant, subject, ref string) bool {
	parsed, err := credref.Parse(ref)
	if err != nil || tenant == "" || subject == "" {
		return false
	}
	if parsed.Scheme == credref.Env {
		if _, err := clients.EnvRef(parsed.Raw); err != nil || legacy.SignInSecret(parsed.Name) {
			return false
		}
	}
	return slices.ContainsFunc(p.cfg.CredentialRefs[tenant], func(start string) bool {
		return credentials.Owns(start, subject, parsed.Raw)
	})
}

// ownStarts is how the references subject may give start: each of the
// tenant's starts with their subject in it, leaving out a start their
// subject cannot be written in.
func (p *Platform) ownStarts(tenant, subject string) []string {
	out := []string{}
	for _, start := range p.cfg.CredentialRefs[tenant] {
		if own, ok := credentials.Expand(start, subject); ok {
			out = append(out, own)
		}
	}
	return out
}

// personCredential is the credential the person a run acts for holds for a
// person-scoped provider. It is fiber's PersonResolver: it is asked on every
// expansion, and answers with the person's own or with nothing. The rules
// are checked here as well as when the credential was given, so one the
// workspace no longer allows is not used, however it was kept.
func (p *Platform) personCredential(_ context.Context, tenant string, prov toolprovider.Provider, actor event.Actor) (mcptools.PersonCredential, error) {
	// Native source consent is not consent to an MCP endpoint.
	if strings.HasPrefix(prov.Name, "hsrc-") {
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	person := actor.User
	if actor.Bot || person == "" {
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	c, ok, err := p.Credentials.Get(tenant, person, prov.Name)
	if err != nil {
		return mcptools.PersonCredential{}, err
	}
	if !ok {
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	if c.Kind == credentials.Value {
		if !p.cfg.CredentialValues[tenant] {
			p.log.Warn("platform: a stored credential is not used: this workspace does not let people store values",
				"tenant", tenant, "person", person, "provider", prov.Name)
			return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
		}
		v, ok, err := p.Credentials.Value(tenant, person, prov.Name)
		if err != nil {
			return mcptools.PersonCredential{}, err
		}
		if !ok {
			return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
		}
		if err := p.Credentials.MarkUsed(tenant, person, prov.Name, p.now()); err != nil {
			return mcptools.PersonCredential{}, err
		}
		return mcptools.PersonCredential{Secret: mcptools.NewSecret(v)}, nil
	}
	if !p.refAllowed(tenant, person, c.Ref) {
		p.log.Warn("platform: a credential reference is not used: it is not this person's own under a start this workspace lists",
			"tenant", tenant, "person", person, "provider", prov.Name)
		return mcptools.PersonCredential{}, mcptools.ErrNoPersonCredential
	}
	if err := p.Credentials.MarkUsed(tenant, person, prov.Name, p.now()); err != nil {
		return mcptools.PersonCredential{}, err
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
	Kind       credentials.Kind `json:"kind,omitempty"`
	Ref        string           `json:"ref,omitempty"`
	UpdatedAt  *time.Time       `json:"updated_at,omitempty"`
	LastUsedAt *time.Time       `json:"last_used_at,omitempty"`
}

// ToolCredentials is the person-scoped providers of a workspace, as one
// person stands with each, and what the workspace lets them give.
type ToolCredentials struct {
	Providers []ToolCredential `json:"providers"`
	// Values reports whether this workspace lets people store a value.
	Values bool `json:"values"`
	// References is how the references the person asking may give start:
	// the starts the operator listed for the workspace, with their subject
	// in place of credentials.SubjectField. A machine gives none.
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
	out := ToolCredentials{Providers: []ToolCredential{}, Values: p.cfg.CredentialValues[who.Tenant], References: []string{}}
	if who.Kind != auth.Machine {
		out.References = p.ownStarts(who.Tenant, who.Subject)
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
	held, ok, err := p.Credentials.Get(who.Tenant, who.Subject, prov.Name)
	if err != nil {
		return ToolCredential{}, err
	}
	if ok {
		at := held.UpdatedAt
		c.Connected, c.Kind, c.Ref, c.UpdatedAt = true, held.Kind, held.Ref, &at
		if !held.LastUsedAt.IsZero() {
			used := held.LastUsedAt
			c.LastUsedAt = &used
		}
	}
	return c, nil
}

// personScoped returns a provider of the caller's workspace that takes each
// person's own credential.
func (p *Platform) personScoped(ctx context.Context, tenant, name string) (toolprovider.Provider, error) {
	if !toolprovider.ValidName(name) || strings.HasPrefix(name, "hsrc-") {
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
// A reference must be the caller's own under one of the starts the operator
// listed for the workspace, the start with their subject in it, and is not
// read here, so connecting says nothing about whether it names anything. A
// value is accepted only where the workspace lets people store one, and is
// then kept as a client's secret is.
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
	case in.Ref != "" && !p.refAllowed(who.Tenant, who.Subject, in.Ref):
		return ToolCredential{}, errPersonRef
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "credential.connect", Resource: "credential", ID: prov.Name, Subject: who.Subject, Route: "PUT /api/me/credentials/{provider}", Rule: string(auth.RunsWrite)})
	if err != nil {
		return ToolCredential{}, err
	}
	held, err := p.Credentials.Put(who.Tenant, who.Subject, prov.Name, in, p.now())
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
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
	if _, ok, err := p.Credentials.Get(who.Tenant, who.Subject, provider); err != nil {
		return err
	} else if !ok {
		return credentials.ErrNotFound
	}
	attempt, err := p.BeginAudit(who, audit.Change{Action: "credential.disconnect", Resource: "credential", ID: provider, Subject: who.Subject, Route: "DELETE /api/me/credentials/{provider}", Rule: string(auth.RunsWrite)})
	if err != nil {
		return err
	}
	err = p.Credentials.Delete(who.Tenant, who.Subject, provider)
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	if err != nil {
		return err
	}
	p.log.Info("platform: credential removed", "tenant", who.Tenant, "subject", who.Subject, "provider", provider)
	return nil
}
