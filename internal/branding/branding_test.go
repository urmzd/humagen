package branding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/spec"
)

const logo = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 10 10"><rect width="10" height="10"/></svg>`

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func str(s string) *string { return &s }

func TestNothingSetIsHumagen(t *testing.T) {
	s, err := Load(spec.Config{})
	if err != nil {
		t.Fatal(err)
	}
	b := s.For("default")
	if b.Name != "Humagen" || b.Title != "Humagen" || b.Wordmark != "humagen" || b.Logo != nil || b.CSS != "" {
		t.Fatalf("default = %+v", b)
	}
	built := `<html><head><title>the console</title><meta name="theme-color" content="#000000" /></head></html>`
	if page := string(s.Page([]byte(built))); page != built {
		t.Fatalf("an unbranded page changed:\n%s", page)
	}
	if m := string(s.WebManifest([]byte(`{"name": "x"}`))); m != `{"name": "x"}` {
		t.Fatalf("an unbranded manifest changed: %s", m)
	}
}

func TestTheDisplayNameNamesTheProduct(t *testing.T) {
	var cfg spec.Config
	cfg.System.Metadata.DisplayName = "Acme"
	s, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if b := s.For("default"); b.Name != "Acme" || b.Title != "Acme" || b.Wordmark != "Acme" {
		t.Fatalf("branding = %+v", b)
	}
}

func TestLoadReadsFilesAndMergesWorkspaces(t *testing.T) {
	dir := t.TempDir()
	logoPath := write(t, filepath.Join(dir, "logo.svg"), logo)
	icons := filepath.Join(dir, "icons")
	write(t, filepath.Join(icons, "favicon.svg"), logo)
	write(t, filepath.Join(icons, "icon-192.png"), "\x89PNG")
	var cfg spec.Config
	cfg.System.Spec.Branding = spec.Branding{
		Name: "Acme", Logo: &spec.Logo{Path: logoPath}, Icons: icons,
		Manifest: &spec.Manifest{ShortName: "Acme", ThemeColor: "#1A1A2E"},
		Palette:  &spec.Palette{Surface: &spec.Pair{Light: "#fafafa", Dark: "#1a1a2e"}},
		Fonts:    &spec.Fonts{UI: "Inter, sans-serif"},
	}
	cfg.Platforms = []spec.Platform{
		{Metadata: spec.Metadata{Name: "north"}, Spec: spec.PlatformSpec{Branding: spec.Branding{
			Name: "North", Wordmark: str(""), Palette: &spec.Palette{Accent: &spec.Pair{Light: "#c2185b", Dark: "#f06292"}},
		}}},
		{Metadata: spec.Metadata{Name: "south"}},
	}
	s, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sys := s.For("south")
	if sys.Name != "Acme" || sys.Logo == nil || sys.Logo.Type != "image/svg+xml" || sys.LogoAlt != "Acme" {
		t.Fatalf("a workspace with no branding has the deployment's: %+v", sys)
	}
	if len(s.Icons) != 2 || s.Icons["icon-192.png"].Type != "image/png" {
		t.Fatalf("icons = %v", s.Icons)
	}
	north := s.For("north")
	if north.Name != "North" || north.Title != "North" || north.Wordmark != "" || north.Logo == nil {
		t.Fatalf("north = %+v", north)
	}
	for _, want := range []string{"--aui-bg: #fafafa", "--aui-bg: #1a1a2e", `[data-accent="brand"]`, "--aui-accent: #c2185b", "--font-aui-sans: Inter, sans-serif"} {
		if !strings.Contains(north.CSS, want) {
			t.Errorf("north's stylesheet lacks %q:\n%s", want, north.CSS)
		}
	}
	if strings.Contains(sys.CSS, "brand") {
		t.Errorf("the deployment sets no accent:\n%s", sys.CSS)
	}

	page := string(s.Page([]byte(`<html><head><title>Humagen</title><meta name="application-name" content="Humagen" /><meta name="apple-mobile-web-app-title" content="Humagen" /><meta name="theme-color" content="#000000" media="(prefers-color-scheme: dark)" /><meta name="theme-color" content="#ffffff" media="(prefers-color-scheme: light)" /></head><body></body></html>`)))
	for _, want := range []string{"<title>Acme</title>", `application-name" content="Acme"`, `apple-mobile-web-app-title" content="Acme"`, `<meta name="theme-color" content="#1a1a2e" />`, `<style id="humagen-branding">`} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q:\n%s", want, page)
		}
	}
	if strings.Count(page, "theme-color") != 1 {
		t.Errorf("the built theme colors stay:\n%s", page)
	}

	var m map[string]any
	if err := json.Unmarshal(s.WebManifest([]byte(`{"name":"Humagen","short_name":"Humagen","theme_color":"#000000","start_url":"/inbox"}`)), &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "Acme" || m["short_name"] != "Acme" || m["theme_color"] != "#1a1a2e" || m["start_url"] != "/inbox" {
		t.Fatalf("manifest = %v", m)
	}
}

func TestLoadRefusesWhatCannotBeServed(t *testing.T) {
	dir := t.TempDir()
	for name, tc := range map[string]struct {
		b    spec.Branding
		want string
	}{
		"a missing logo":            {spec.Branding{Logo: &spec.Logo{Path: filepath.Join(dir, "none.svg")}}, "no such file"},
		"a dark logo with a script": {spec.Branding{Logo: &spec.Logo{Path: write(t, filepath.Join(dir, "ok.svg"), logo), Dark: write(t, filepath.Join(dir, "dark.svg"), `<svg><script>x()</script></svg>`)}}, "branding.logo.dark"},
		"an SVG with a script":      {spec.Branding{Logo: &spec.Logo{Path: write(t, filepath.Join(dir, "bad.svg"), `<svg><script>alert(1)</script></svg>`)}}, "script"},
		"an SVG with a handler":     {spec.Branding{Logo: &spec.Logo{Path: write(t, filepath.Join(dir, "on.svg"), `<svg onload="x()"></svg>`)}}, "script"},
		"a file that is no SVG":     {spec.Branding{Logo: &spec.Logo{Path: write(t, filepath.Join(dir, "text.svg"), `hello`)}}, "not an SVG"},
		"a logo too large":          {spec.Branding{Logo: &spec.Logo{Path: write(t, filepath.Join(dir, "big.png"), strings.Repeat("x", maxLogo+1))}}, "at most"},
		"an icon of no such name":   {spec.Branding{Icons: filepath.Dir(write(t, filepath.Join(dir, "icons", "favicon.ico"), "x"))}, `no icon called "favicon.ico"`},
		"a missing icon directory":  {spec.Branding{Icons: filepath.Join(dir, "nowhere")}, "no such file"},
	} {
		t.Run(name, func(t *testing.T) {
			var cfg spec.Config
			cfg.System.Spec.Branding = tc.b
			if _, err := Load(cfg); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestFontFilesAreServedAndNamedInTheStylesheet(t *testing.T) {
	dir := t.TempDir()
	face := write(t, filepath.Join(dir, "Inter.WOFF2"), "wOF2 inter")
	italic := write(t, filepath.Join(dir, "inter-italic.ttf"), "ttf italic")
	var cfg spec.Config
	cfg.System.Spec.Branding = spec.Branding{Fonts: &spec.Fonts{UI: "Inter, sans-serif", Files: []spec.FontFile{
		{Family: "Inter", Path: face, Weight: "100 900"},
		{Family: "Inter", Path: italic, Style: "italic"},
	}}}
	cfg.Platforms = []spec.Platform{{Metadata: spec.Metadata{Name: "north"}, Spec: spec.PlatformSpec{Branding: spec.Branding{Name: "North"}}}}
	s, err := Load(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sys := s.For("default")
	if a := sys.Fonts["0.woff2"]; a.Type != "font/woff2" || string(a.Body) != "wOF2 inter" || sys.Fonts["1.ttf"].Type != "font/ttf" {
		t.Fatalf("fonts = %v", sys.Fonts)
	}
	for _, want := range []string{
		`@font-face { font-family: "Inter"; src: url("/branding/fonts/0.woff2"); font-weight: 100 900; font-style: normal; font-display: swap; }`,
		`src: url("/branding/fonts/1.ttf"); font-weight: 400; font-style: italic;`,
	} {
		if !strings.Contains(sys.CSS, want) {
			t.Errorf("the deployment's stylesheet lacks %s:\n%s", want, sys.CSS)
		}
	}
	north := s.For("north")
	if !strings.Contains(north.CSS, `url("/api/branding/fonts/0.woff2")`) || len(north.Fonts) != 2 {
		t.Errorf("a workspace serves the fonts it has from behind sign-in:\n%s", north.CSS)
	}

	cfg.System.Spec.Branding.Fonts.Files[0].Path = filepath.Join(dir, "gone.woff2")
	if _, err := Load(cfg); err == nil || !strings.Contains(err.Error(), "branding.fonts.files[0]") {
		t.Fatalf("a missing font file: %v", err)
	}
}
