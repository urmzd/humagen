package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/connectionauth"
)

// accountLinkTTL is how long a browser keeps the secret an account link's
// consent is bound to: as long as the consent itself.
const accountLinkTTL = 10 * time.Minute

// browserCall reports a request from a person's browser session: not a call
// an agent's run or the MCP dispatched, and not one with a bearer token.
func (s *Server) browserCall(r *http.Request) bool {
	if _, dispatched := r.Context().Value(dispatchedKey{}).(auth.Principal); dispatched {
		return false
	}
	return r.Header.Get("Authorization") == ""
}

// linkCookieName is the cookie one provider's account link is bound to. Each
// provider has its own, so a link begun on one does not replace another's.
func (s *Server) linkCookieName(provider string) string {
	name := "humagen_account_link_" + strings.ReplaceAll(provider, "-", "_")
	if s.Authn != nil && s.Authn.Secure {
		return "__Host-" + name
	}
	return name
}

// setLinkCookie gives the browser the secret its account link is bound to,
// or takes it back when ttl is zero.
func (s *Server) setLinkCookie(w http.ResponseWriter, provider, value string, ttl time.Duration) {
	c := &http.Cookie{
		Name: s.linkCookieName(provider), Value: value, Path: "/", HttpOnly: true,
		Secure: s.Authn != nil && s.Authn.Secure, SameSite: http.SameSiteLaxMode, MaxAge: int(ttl.Seconds()),
	}
	if ttl <= 0 {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

type ConnectionCallback struct {
	State          string `json:"state"`
	Code           string `json:"code"`
	InstallationID string `json:"installation_id,omitempty"`
}

func (s *Server) listConnectionPresets(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.ConnectionPresets(who)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) authorizeConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var input platform.ConnectionAuthorizationInput
	if r.ContentLength != 0 {
		if !decode(w, r, &input) {
			return
		}
	}
	var out connectionauth.Authorization
	var err error
	if provider := r.PathValue("provider"); platform.IsAccountLink(provider) {
		// An account link starts only in the person's own browser, and its
		// callback completes only in that browser: it holds the secret in
		// an HttpOnly cookie the consent is bound to.
		if !s.browserCall(r) {
			s.problem(w, platform.ErrAccountInConsole)
			return
		}
		secret := token()
		out, err = s.P.AuthorizeAccount(r.Context(), who, provider, input, secret)
		if err == nil {
			s.setLinkCookie(w, provider, secret, accountLinkTTL)
		}
	} else {
		out, err = s.P.AuthorizeConnection(r.Context(), who, provider, input)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) completeConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body ConnectionCallback
	if !decode(w, r, &body) {
		return
	}
	var err error
	if provider := r.PathValue("provider"); platform.IsAccountLink(provider) {
		if !s.browserCall(r) {
			// A callback from anything but a browser session ends the
			// consent it names.
			s.P.EndAccountLink(who, provider, body.State)
			s.problem(w, platform.ErrAccountInConsole)
			return
		}
		secret := ""
		if c, cerr := r.Cookie(s.linkCookieName(provider)); cerr == nil {
			secret = c.Value
		}
		// The secret is spent with the state, whatever the answer.
		s.setLinkCookie(w, provider, "", 0)
		err = s.P.CompleteAccount(r.Context(), who, provider, body.State, body.Code, secret)
	} else if r.PathValue("provider") == "github" {
		err = s.P.CompleteGitHubInstall(r.Context(), who, body.State, body.Code, body.InstallationID)
	} else {
		err = s.P.CompleteConnection(r.Context(), who, r.PathValue("provider"), body.State, body.Code)
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
func (s *Server) disconnectConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	// A person unlinks their own account where they linked it: in their
	// browser.
	if platform.IsAccountLink(r.PathValue("provider")) && !s.browserCall(r) {
		s.problem(w, platform.ErrAccountInConsole)
		return
	}
	if err := s.P.DisconnectConnection(who, r.PathValue("provider")); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.CheckConnection(r.Context(), who, r.PathValue("provider"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
func (s *Server) checkClientConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.CheckClientConnection(r.Context(), who, r.PathValue("id"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) getConnection(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	out, err := s.P.GetConnection(r.Context(), who, r.PathValue("provider"))
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, out)
}
