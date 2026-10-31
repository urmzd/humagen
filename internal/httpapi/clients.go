package httpapi

import (
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/projects"
)

// A client is one Slack app or GitHub installation the workspace answers
// through. Anyone in the workspace can see which exist. Only an admin can
// create or change one, because that hands the workspace's agents a
// credential. A secret is written and never read back: no response carries one.
func (s *Server) clientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/clients/{id}/delivery", s.authed(auth.ClientsRead, s.clientBridgeDelivery))
	mux.HandleFunc("GET /api/clients/{id}/capabilities", s.authed(auth.ClientsRead, s.getClientCapabilities))
	mux.HandleFunc("PUT /api/clients/{id}/capabilities", s.authed(auth.ClientsWrite, s.putClientCapabilities))
	mux.HandleFunc("GET /api/clients", s.authed(auth.ClientsRead, s.listClients))
	mux.HandleFunc("POST /api/clients", s.authed(auth.ClientsWrite, s.createClient))
	mux.HandleFunc("GET /api/clients/{id}", s.authed(auth.ClientsRead, s.getClient))
	mux.HandleFunc("GET /api/clients/{id}/bindings", s.authed(auth.ClientsRead, s.clientBindings))
	mux.HandleFunc("PUT /api/clients/{id}", s.authed(auth.ClientsWrite, s.updateClient))
	mux.HandleFunc("DELETE /api/clients/{id}", s.authed(auth.ClientsWrite, s.deleteClient))
}

// clientBody is a client as a caller writes it. It has no field for the
// tenant, for who manages the client, or for its version: those are the
// server's to say.
type clientBody struct {
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Surface      clients.Surface        `json:"surface"`
	Identity     clients.Identity       `json:"identity"`
	DefaultAgent string                 `json:"default_agent"`
	Agents       []string               `json:"agents"`
	Bots         map[string]string      `json:"bots"`
	Slack        *clients.SlackOptions  `json:"slack"`
	GitHub       *clients.GitHubOptions `json:"github"`
	Home         *clients.Home          `json:"home"`
	// Enabled defaults to true on create.
	Enabled     *bool `json:"enabled"`
	Credentials struct {
		Token         clients.SecretInput `json:"token"`
		SigningSecret clients.SecretInput `json:"signing_secret"`
	} `json:"credentials"`
}

func (b clientBody) input() platform.ClientInput {
	return platform.ClientInput{
		Client: clients.Client{
			ID: b.ID, Name: b.Name, Surface: b.Surface, Identity: b.Identity,
			DefaultAgent: b.DefaultAgent, Agents: b.Agents, Bots: b.Bots, Slack: b.Slack, GitHub: b.GitHub, Home: b.Home,
			Enabled: b.Enabled == nil || *b.Enabled,
		},
		Token: b.Credentials.Token, SigningSecret: b.Credentials.SigningSecret,
	}
}

// clientView is a client and the limits on the runs it raises.
type clientView struct {
	clients.Client
	// Limits is the client's own policy, if it has one. It is set through
	// /api/policies with a scope of kind "client".
	Limits *policy.Policy `json:"limits,omitempty"`
}

// view is a client as seer's reader reads it (platform.Seer.Client): a home
// in a project they may not read is left out, as everything in that project
// is, and so is every agent it names that lives in one. The client reads as
// one with no home of its own and without those agents.
func (s *Server) view(seer *platform.Seer, tenant string, c clients.Client) (clientView, error) {
	v := clientView{Client: seer.Client(c)}
	pols, err := s.P.Policies.For(tenant, nil, "", c.ID)
	if err != nil {
		return clientView{}, err
	}
	for _, pol := range pols {
		if pol.Scope.Kind == policy.Client {
			v.Limits = &pol
		}
	}
	return v, nil
}

func (s *Server) listClients(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	// The store returns one tenant's clients and never more than its ceiling.
	all, err := s.P.Clients.List(who.Tenant)
	if err != nil {
		s.problem(w, err)
		return
	}
	out := make([]clientView, 0, len(all))
	seer := s.P.Seer(who)
	for _, c := range all {
		v, err := s.view(seer, who.Tenant, c)
		if err != nil {
			s.problem(w, err)
			return
		}
		out = append(out, v)
	}
	caps, err := s.P.ClientCaps(who.Tenant)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, map[string]any{"clients": out, "caps": caps})
}

func (s *Server) getClient(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	c, err := s.P.Clients.Get(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	v, err := s.view(s.P.Seer(who), who.Tenant, c)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}

// clientBindings lists the Slack channels a client is bound to, and the
// channel each is bound to here, leaving out those in projects the caller may
// not read.
func (s *Server) clientBindings(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	list, err := s.P.Bindings(who.Tenant, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	seer := s.P.Seer(who)
	out := []projects.Binding{}
	for _, b := range list {
		if seer.Project(b.Project) {
			out = append(out, b)
		}
	}
	ok(w, http.StatusOK, map[string]any{"bindings": out})
}

func (s *Server) createClient(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body clientBody
	if !decode(w, r, &body) {
		return
	}
	in := body.input()
	if err := s.P.SeesHome(who, in.Client); err != nil {
		s.problem(w, err)
		return
	}
	named, err := s.P.ClientWrittenBy(r.Context(), who, nil, in.Client)
	if err != nil {
		s.problem(w, err)
		return
	}
	in.Client = named
	c, err := s.P.CreateClient(r.Context(), who.Tenant, who.Subject, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	v, err := s.view(s.P.Seer(who), who.Tenant, c)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusCreated, v)
}

// updateClient replaces a client's record. A caller is not shown a home in
// a project they may not read, so a record they send back without one keeps
// it, as one sent without secrets keeps those; naming another home they may
// read replaces it. The agents it names that they are not shown are kept the
// same way (platform.ClientWrittenBy).
func (s *Server) updateClient(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body clientBody
	if !decode(w, r, &body) {
		return
	}
	in := body.input()
	if err := s.P.SeesHome(who, in.Client); err != nil {
		s.problem(w, err)
		return
	}
	seer := s.P.Seer(who)
	if old, err := s.P.Clients.Get(who.Tenant, r.PathValue("id")); err == nil && old.Managed == "" {
		if in.Home == nil && old.Home != nil && !seer.Project(old.HomeOf().Project) {
			in.Home = old.Home
		}
		named, err := s.P.ClientWrittenBy(r.Context(), who, &old, in.Client)
		if err != nil {
			s.problem(w, err)
			return
		}
		in.Client = named
	}
	c, err := s.P.UpdateClient(r.Context(), who.Tenant, r.PathValue("id"), who.Subject, in)
	if err != nil {
		s.problem(w, err)
		return
	}
	v, err := s.view(seer, who.Tenant, c)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}

func (s *Server) deleteClient(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteClient(r.Context(), who.Tenant, r.PathValue("id"), who.Subject); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
