package httpapi

import (
	"bytes"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/urmzd/humagen/internal/branding"
	"github.com/urmzd/humagen/pkg/auth"
)

// brandingRoutes serves the branding a person's workspace has: what the
// console shows once they sign in. The deployment's logo is public, like
// its icons, since the sign-in page shows it.
func (s *Server) brandingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/branding", s.authed("", s.getBranding))
	mux.HandleFunc("GET /api/branding/logo", s.authed("", s.getBrandingLogo))
	mux.HandleFunc("GET /api/branding/style.css", s.authed("", s.getBrandingStyle))
	mux.HandleFunc("GET /branding/logo", func(w http.ResponseWriter, r *http.Request) {
		serveAsset(w, r, s.Branding.For("").Logo)
	})
}

// brandingView is what the console reads to brand itself.
type brandingView struct {
	Name     string  `json:"name"`
	Wordmark string  `json:"wordmark"`
	Title    string  `json:"title"`
	Logo     *string `json:"logo"`
	LogoAlt  string  `json:"logo_alt"`
	Style    *string `json:"style"`
}

func (s *Server) getBranding(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	b := s.Branding.For(who.Tenant)
	v := brandingView{Name: b.Name, Wordmark: b.Wordmark, Title: b.Title, LogoAlt: b.LogoAlt}
	if b.Logo != nil {
		u := "/api/branding/logo"
		v.Logo = &u
	}
	if b.CSS != "" {
		u := "/api/branding/style.css"
		v.Style = &u
	}
	ok(w, http.StatusOK, v)
}

func (s *Server) getBrandingLogo(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	serveAsset(w, r, s.Branding.For(who.Tenant).Logo)
}

func (s *Server) getBrandingStyle(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	css := s.Branding.For(who.Tenant).CSS
	serveAsset(w, r, &branding.Asset{Type: "text/css; charset=utf-8", Body: []byte(css)})
}

func serveAsset(w http.ResponseWriter, r *http.Request, a *branding.Asset) {
	if a == nil {
		fail(w, http.StatusNotFound, "no branding of that kind is set")
		return
	}
	w.Header().Set("Content-Type", a.Type)
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(a.Body))
}

// brandedFile answers a console file branding replaces: the page itself, the
// manifest and the icons. It reports false for any other.
func brandedFile(w http.ResponseWriter, r *http.Request, dir, file string, b *branding.Set) bool {
	if b == nil {
		return false
	}
	switch file {
	case "/index.html":
		raw, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if err != nil {
			return false
		}
		w.Header().Set("Cache-Control", "no-cache")
		serveAsset(w, r, &branding.Asset{Type: "text/html; charset=utf-8", Body: b.Page(raw)})
	case "/manifest.webmanifest":
		raw, err := os.ReadFile(filepath.Join(dir, "manifest.webmanifest"))
		if err != nil {
			return false
		}
		serveAsset(w, r, &branding.Asset{Type: "application/manifest+json", Body: b.WebManifest(raw)})
	default:
		name := path.Base(file)
		icon, ok := b.Icons[name]
		if !ok || file != "/"+name {
			return false
		}
		serveAsset(w, r, &icon)
	}
	return true
}
