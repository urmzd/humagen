package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/branding"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/join"
	"github.com/urmzd/humagen/pkg/spec"
)

const brandingSVG = `<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1"/></svg>`

func brandedServer(t *testing.T, tenant string) (http.Handler, string) {
	t.Helper()
	dir := t.TempDir()
	web := filepath.Join(dir, "web")
	icons := filepath.Join(dir, "icons")
	for path, body := range map[string]string{
		filepath.Join(web, "index.html"):           `<html><head><title>Humagen</title><meta name="application-name" content="Humagen" /></head><body></body></html>`,
		filepath.Join(web, "manifest.webmanifest"): `{"name":"Humagen","short_name":"Humagen"}`,
		filepath.Join(web, "favicon.svg"):          `<svg>humagen</svg>`,
		filepath.Join(web, "icon.svg"):             `<svg>humagen</svg>`,
		filepath.Join(icons, "favicon.svg"):        brandingSVG,
		filepath.Join(dir, "logo.svg"):             brandingSVG,
		filepath.Join(dir, "north.png"):            "\x89PNG north",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	var cfg spec.Config
	cfg.System.Spec.Branding = spec.Branding{
		Name: "Acme", Logo: &spec.Logo{Path: filepath.Join(dir, "logo.svg")}, Icons: icons,
		Palette: &spec.Palette{Surface: &spec.Pair{Light: "#fafafa"}},
	}
	cfg.Platforms = []spec.Platform{{Metadata: spec.Metadata{Name: "north"}, Spec: spec.PlatformSpec{Branding: spec.Branding{
		Name: "North", Logo: &spec.Logo{Path: filepath.Join(dir, "north.png"), Alt: "North logo"},
		Starters: []spec.Starter{{Agent: "lead", Label: "Plan the week", Text: "Plan "}},
		Copy:     map[string]string{"navigation.agents": "Assistants"},
	}}}}
	brand, err := branding.Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, err := platform.New(platform.Config{
		Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4},
		Router: func(string) join.Asker { return join.Words },
	})
	if err != nil {
		t.Fatal(err)
	}
	who := &auth.Principal{Subject: "ada", Tenant: tenant, Kind: auth.User, Roles: []auth.Role{auth.Viewer}}
	s := &Server{P: p, Authn: &Authn{Dev: who}, Log: slog.New(slog.DiscardHandler), WebDir: web, Branding: brand}
	return s.Handler(), dir
}

func brandGet(t *testing.T, h http.Handler, path string) (*http.Response, string) {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	res := w.Result()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func TestTheConsoleIsServedBranded(t *testing.T) {
	h, _ := brandedServer(t, "default")
	for _, path := range []string{"/", "/index.html", "/inbox"} {
		res, body := brandGet(t, h, path)
		if res.StatusCode != http.StatusOK || !strings.Contains(body, "<title>Acme</title>") || !strings.Contains(body, `<style id="humagen-branding">`) {
			t.Fatalf("%s: %d\n%s", path, res.StatusCode, body)
		}
	}
	if _, body := brandGet(t, h, "/manifest.webmanifest"); !strings.Contains(body, `"name": "Acme"`) {
		t.Fatalf("manifest:\n%s", body)
	}
	if res, body := brandGet(t, h, "/favicon.svg"); body != brandingSVG || res.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("a replaced icon: %s %q", res.Header.Get("Content-Type"), body)
	}
	if _, body := brandGet(t, h, "/icon.svg"); body != `<svg>humagen</svg>` {
		t.Fatalf("an icon not replaced is the console's own: %q", body)
	}
	if res, body := brandGet(t, h, "/branding/logo"); res.StatusCode != http.StatusOK || body != brandingSVG {
		t.Fatalf("the deployment's logo: %d %q", res.StatusCode, body)
	}
}

func TestAWorkspaceSeesItsOwnBranding(t *testing.T) {
	h, _ := brandedServer(t, "north")
	_, body := brandGet(t, h, "/api/branding")
	var v map[string]any
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		t.Fatal(err)
	}
	if v["name"] != "North" || v["title"] != "North" || v["wordmark"] != "North" || v["logo"] != "/api/branding/logo" || v["logo_alt"] != "North logo" || v["style"] != "/api/branding/style.css" {
		t.Fatalf("branding = %v", v)
	}
	if st, _ := v["starters"].([]any); len(st) != 1 || v["copy"].(map[string]any)["navigation.agents"] != "Assistants" {
		t.Fatalf("starters and copy = %v %v", v["starters"], v["copy"])
	}
	if res, body := brandGet(t, h, "/api/branding/logo"); res.Header.Get("Content-Type") != "image/png" || body != "\x89PNG north" {
		t.Fatalf("logo: %s %q", res.Header.Get("Content-Type"), body)
	}
	if res, body := brandGet(t, h, "/api/branding/style.css"); !strings.HasPrefix(res.Header.Get("Content-Type"), "text/css") || !strings.Contains(body, "--aui-bg: #fafafa") {
		t.Fatalf("style: %s %q", res.Header.Get("Content-Type"), body)
	}

	other, _ := brandedServer(t, "default")
	if _, body := brandGet(t, other, "/api/branding"); !strings.Contains(body, `"name":"Acme"`) || !strings.Contains(body, `"starters":null`) || !strings.Contains(body, `"copy":{}`) {
		t.Fatalf("a workspace with none has the deployment's: %s", body)
	}
}

func TestBrandingNeedsSignIn(t *testing.T) {
	p, err := platform.New(platform.Config{
		Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4},
		Router: func(string) join.Asker { return join.Words },
	})
	if err != nil {
		t.Fatal(err)
	}
	h := (&Server{P: p, Authn: &Authn{}, Log: slog.New(slog.DiscardHandler)}).Handler()
	for _, path := range []string{"/api/branding", "/api/branding/logo", "/api/branding/style.css"} {
		if res, _ := brandGet(t, h, path); res.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, res.StatusCode)
		}
	}
	if res, _ := brandGet(t, h, "/branding/logo"); res.StatusCode != http.StatusNotFound {
		t.Fatalf("no logo is set: %d", res.StatusCode)
	}
}
