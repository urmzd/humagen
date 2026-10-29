package spec

import (
	"strings"
	"testing"
)

func TestBrandingReads(t *testing.T) {
	docs, err := Parse([]byte(`api_version: humagen/v1
kind: System
spec:
  branding:
    name: Acme
    title: Acme Agents
    logo: {path: ./assets/logo.svg, alt: Acme}
    icons: ./assets/icons
    manifest: {name: Acme Agents, short_name: Acme, theme_color: "#1a1a2e", background_color: "#fafafa"}
    palette:
      accent: {light: "#c2185b", dark: "#f06292"}
      surface: {light: "#fafafa", dark: "#1a1a2e"}
      text: {light: "#1a1a2e", dark: "#fafafa"}
    fonts: {ui: "Inter, sans-serif", heading: "Georgia, serif", mono: "'DM Mono', monospace"}
---
api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  branding:
    wordmark: ""
    palette: {accent: "#2e9e8f"}
`))
	if err != nil {
		t.Fatal(err)
	}
	cfg := docs.Config
	if err := cfg.check(); err != nil {
		t.Fatal(err)
	}
	b := cfg.System.Spec.Branding
	if b.Name != "Acme" || b.Logo.Path != "./assets/logo.svg" || b.Palette.Accent.Dark != "#f06292" || b.Fonts.Mono != "'DM Mono', monospace" {
		t.Fatalf("branding = %+v", b)
	}
	w := cfg.Platforms[0].Spec.Branding
	if w.Wordmark == nil || *w.Wordmark != "" || w.Palette.Accent.Light != "#2e9e8f" || w.Palette.Accent.Dark != "#2e9e8f" {
		t.Fatalf("a color written once is both themes', and an empty wordmark is kept: %+v", w)
	}
}

func TestBrandingRefuses(t *testing.T) {
	for name, tc := range map[string]struct{ system, platform, want string }{
		"icons in a workspace":    {platform: `icons: ./icons`, want: "set them in the System document"},
		"manifest in a workspace": {platform: `manifest: {name: X}`, want: "the installable app is the deployment's"},
		"a color that is not hex": {system: `palette: {accent: red}`, want: `"red" is not a hex color`},
		"text that cannot be read": {system: `palette: {text: {light: "#dddddd"}}`,
			want: "text.light on surface.light has a contrast of"},
		"a dark surface under default text": {system: `palette: {surface: {light: "#000000"}}`,
			want: "text.light on surface.light"},
		"an accent that disappears": {platform: `palette: {accent: {dark: "#222222"}}`,
			want: "accent.dark on surface.dark"},
		"a font that ends its declaration": {system: `fonts: {ui: "Inter; } body { color: red"}`,
			want: "branding.fonts.ui is a CSS font-family list"},
		"a logo that is not an image": {system: `logo: {path: ./logo.html}`, want: "branding.logo.path is an .svg"},
		"a font file of another type": {system: `fonts: {files: [{family: Inter, path: ./inter.css}]}`, want: "files[0].path is a .woff2"},
		"a font family with a quote":  {system: `fonts: {files: [{family: "In'ter", path: ./inter.woff2}]}`, want: "files[0].family"},
		"a font weight out of range":  {system: `fonts: {files: [{family: Inter, path: ./inter.woff2, weight: bold}]}`, want: "files[0].weight"},
		"a font style of its own":     {system: `fonts: {files: [{family: Inter, path: ./inter.woff2, style: oblique}]}`, want: "normal or italic"},
		"a logo with no path":         {system: `logo: {alt: Acme}`, want: "branding.logo.path is required"},
		"markup in a name":            {system: `name: "<b>Acme</b>"`, want: "branding.name: plain text"},
		"an unknown field":            {system: `favicon_dir: ./icons`, want: "favicon_dir"},
		"a workspace accent lost on the deployment's surface": {system: `palette: {surface: {dark: "#000000"}, text: {dark: "#ffffff"}}`,
			platform: `palette: {accent: {dark: "#101010"}}`, want: "over the System document's"},
	} {
		t.Run(name, func(t *testing.T) {
			doc := "api_version: humagen/v1\nkind: System\nspec: {}\n"
			if tc.system != "" {
				doc = "api_version: humagen/v1\nkind: System\nspec:\n  branding:\n    " + tc.system + "\n"
			}
			if tc.platform != "" {
				doc += "---\napi_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  branding:\n    " + tc.platform + "\n"
			}
			docs, err := Parse([]byte(doc))
			if err == nil {
				err = docs.check()
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want %q", err, tc.want)
			}
		})
	}
}

func TestContrast(t *testing.T) {
	if c := Contrast("#ffffff", "#000"); c < 20.9 || c > 21.1 {
		t.Fatalf("white on black = %.2f", c)
	}
	if c := Contrast("#777777", "#777777"); c != 1 {
		t.Fatalf("a color on itself = %.2f", c)
	}
	if Color("#ABC").Hex() != "#aabbcc" {
		t.Fatalf("hex = %s", Color("#ABC").Hex())
	}
}

func TestSlackCopyIsChecked(t *testing.T) {
	base := "api_version: humagen/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  surfaces:\n    slack: {enabled: false, "
	docs, err := Parse([]byte(base + `description: "Your team's agents", background_color: "#123456", command_description: Ask the agents, command_usage_hint: "[question]"}`))
	if err != nil {
		t.Fatal(err)
	}
	if err := docs.check(); err != nil {
		t.Fatal(err)
	}
	if s := docs.Platforms[0].Spec.Surfaces.Slack; s.Description != "Your team's agents" || s.BackgroundColor != "#123456" || s.CommandUsageHint != "[question]" {
		t.Fatalf("slack = %+v", s)
	}
	for field, want := range map[string]string{
		`background_color: blue`:                          "surfaces.slack.background_color",
		`description: "` + strings.Repeat("x", 141) + `"`: "surfaces.slack.description: at most 140",
	} {
		docs, err := Parse([]byte(base + field + "}"))
		if err == nil {
			err = docs.check()
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("%s: got %v, want %q", field, err, want)
		}
	}
}
