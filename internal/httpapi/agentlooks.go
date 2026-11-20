package httpapi

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/agentcolors"
	"github.com/urmzd/humagen/pkg/agentlooks"
	"github.com/urmzd/humagen/pkg/auth"
)

// agentLookView is an agent's look, its hue, and what a picker needs to
// offer the rest: the hues other agents hold, and every choice.
// lookOut is a look as the API answers with it: the look, and where its
// drawn picture is served when the console drew one.
type lookOut struct {
	agentlooks.Look
	ImageURL string `json:"image_url,omitempty"`
}

// looksFor answers with the look of each named agent that has one, or a
// drawn picture. An agent with neither is left out: its look is the default.
func (s *Server) looksFor(tenant string, names []string) (map[string]lookOut, error) {
	looks, err := s.P.AgentLooks.Get(tenant, names)
	if err != nil {
		return nil, err
	}
	marks, err := s.P.AgentLooks.Marks(tenant, names)
	if err != nil {
		return nil, err
	}
	out := map[string]lookOut{}
	for _, name := range names {
		l, styled := looks[name]
		image := s.P.MarkURL(tenant, marks[name])
		if styled || image != "" {
			out[name] = lookOut{Look: l, ImageURL: image}
		}
	}
	return out, nil
}

type agentLookView struct {
	Agent   string           `json:"agent"`
	Look    lookOut          `json:"look"`
	Hue     float64          `json:"hue"`
	Taken   []float64        `json:"taken"`
	Choices agentLookChoices `json:"choices"`
}

type agentLookChoices struct {
	Shapes   []string `json:"shapes"`
	Faces    []string `json:"faces"`
	Finishes []string `json:"finishes"`
	Hats     []string `json:"hats"`
	Glasses  []string `json:"glasses"`
}

var lookChoices = agentLookChoices{
	Shapes: agentlooks.Shapes, Faces: agentlooks.Faces, Finishes: agentlooks.Finishes,
	Hats: agentlooks.Hats, Glasses: agentlooks.Glasses,
}

// lookOf reads an agent's look, hue and the hues held by the others.
func (s *Server) lookOf(who auth.Principal, name string) (agentLookView, error) {
	looks, err := s.looksFor(who.Tenant, []string{name})
	if err != nil {
		return agentLookView{}, err
	}
	hues, err := s.P.AgentColors.Assign(who.Tenant, []string{name})
	if err != nil {
		return agentLookView{}, err
	}
	all, err := s.P.AgentColors.Taken(who.Tenant)
	if err != nil {
		return agentLookView{}, err
	}
	taken := make([]float64, 0, len(all))
	for other, hue := range all {
		if other != name {
			taken = append(taken, hue)
		}
	}
	slices.Sort(taken)
	return agentLookView{Agent: name, Look: looks[name], Hue: hues[name], Taken: taken, Choices: lookChoices}, nil
}

// getAgentLook answers with an agent's look. An agent that does not exist
// is 404, as one the caller may not see is (seenAgent).
func (s *Server) getAgentLook(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, name, 0); err != nil {
		s.problem(w, err)
		return
	}
	view, err := s.lookOf(who, name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, view)
}

// putAgentLook keeps an agent's look and, when one is given, its hue. It
// takes the same ownership as changing the agent: the owners of the project
// it lives in. A look is presentation, so the platform's own agents take one
// too: a workspace may call its entry agent "Lead".
func (s *Server) putAgentLook(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		Look agentlooks.Look `json:"look"`
		Hue  *float64        `json:"hue,omitempty"`
	}
	if !decode(w, r, &body) {
		return
	}
	name := r.PathValue("name")
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, name, 0); err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.OwnsHome(who, name); err != nil {
		s.problem(w, err)
		return
	}
	look := body.Look.Normalize()
	if err := look.Validate(); err != nil {
		fail(w, http.StatusBadRequest, err.Error())
		return
	}
	// A display name may not be another agent's handle. Only the one
	// handle it would be is looked up, so this never lists agents.
	var others []string
	if look.DisplayName != "" {
		handle := wire.NormalizeName(look.DisplayName)
		if handle != name && handle != "" {
			if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, handle, 0); err == nil {
				others = []string{handle}
			} else if !errors.Is(err, wire.ErrNotFound) {
				s.problem(w, err)
				return
			}
		}
	}
	if body.Hue != nil {
		all, err := s.P.AgentColors.Taken(who.Tenant)
		if err != nil {
			s.problem(w, err)
			return
		}
		for other, hue := range all {
			if other != name && hue == *body.Hue {
				fail(w, http.StatusConflict, "another agent has that color")
				return
			}
		}
	}
	if _, err := s.P.AgentLooks.Set(who.Tenant, name, look, others); err != nil {
		switch {
		case errors.Is(err, agentlooks.ErrInvalid):
			fail(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, agentlooks.ErrNameTaken):
			fail(w, http.StatusConflict, "another agent is called that")
		default:
			s.problem(w, err)
		}
		return
	}
	if body.Hue != nil {
		if err := s.P.AgentColors.Choose(who.Tenant, name, *body.Hue); err != nil {
			if errors.Is(err, agentcolors.ErrTaken) {
				fail(w, http.StatusConflict, "another agent has that color")
				return
			}
			fail(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	view, err := s.lookOf(who, name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, view)
}

// MaxMarkBytes bounds a drawn picture. A 512 pixel PNG of a bot is far less.
const MaxMarkBytes = 256 << 10

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

// putAgentMark keeps the picture the console drew of an agent, as a PNG,
// under a new random token, and serves it at MarkURL from then on. The
// picture it replaces is deleted. It takes the ownership a look takes.
func (s *Server) putAgentMark(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	name := r.PathValue("name")
	if _, err := s.P.Catalog().Get(r.Context(), who.Tenant, name, 0); err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.OwnsHome(who, name); err != nil {
		s.problem(w, err)
		return
	}
	png, err := io.ReadAll(io.LimitReader(r.Body, MaxMarkBytes+1))
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig) || len(png) > MaxMarkBytes:
		fail(w, http.StatusRequestEntityTooLarge, "the picture is larger than 256 KiB")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, "the picture could not be read")
		return
	case !bytes.HasPrefix(png, pngMagic):
		fail(w, http.StatusBadRequest, "the picture is not a PNG")
		return
	}
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		s.problem(w, err)
		return
	}
	token := hex.EncodeToString(raw)
	if _, err := s.P.Blobs.Put(r.Context(), who.Tenant, agentlooks.MarkKey(token), bytes.NewReader(png), MaxMarkBytes); err != nil {
		s.problem(w, err)
		return
	}
	old, err := s.P.AgentLooks.SetMark(who.Tenant, name, token)
	if err != nil {
		_ = s.P.Blobs.Delete(r.Context(), who.Tenant, agentlooks.MarkKey(token))
		s.problem(w, err)
		return
	}
	if old != "" && old != token {
		_ = s.P.Blobs.Delete(r.Context(), who.Tenant, agentlooks.MarkKey(old))
	}
	view, err := s.lookOf(who, name)
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, view)
}

// agentMark serves an agent's drawn picture to anyone who has its address,
// Slack's servers among them, which fetch an icon without a credential. The
// address is the capability: its token is 128 random bits, and a replaced
// picture is no longer served. Anything else is the same 404.
func (s *Server) agentMark(w http.ResponseWriter, r *http.Request) {
	tenant, file := r.PathValue("tenant"), r.PathValue("file")
	token, isPNG := strings.CutSuffix(file, ".png")
	if !isPNG || !agentlooks.ValidToken(token) {
		http.NotFound(w, r)
		return
	}
	if current, err := s.P.AgentLooks.IsMark(tenant, token); err != nil || !current {
		http.NotFound(w, r)
		return
	}
	body, _, err := s.P.Blobs.Get(r.Context(), tenant, agentlooks.MarkKey(token))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer body.Close()
	h := w.Header()
	h.Set("Content-Type", "image/png")
	h.Set("Cache-Control", "public, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", "default-src 'none'")
	_, _ = io.Copy(w, body)
}
