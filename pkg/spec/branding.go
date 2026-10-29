package spec

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// DefaultName is the product's name when neither the System document's
// display_name nor its branding names another.
const DefaultName = "Humagen"

// Branding is how the console looks and what it calls itself. In the System
// document it is the deployment's, and covers what is served before anyone
// signs in: the tab title, icons and the installable app's manifest. In a
// Platform document it is that workspace's, over the deployment's, and is
// applied once a person of the workspace signs in. Everything left out is
// Humagen's own.
type Branding struct {
	// Name is the product's name: the tab title's suffix, the sidebar's
	// wordmark and the installed app's name, unless each is set itself.
	// Left out, it is the System document's metadata.display_name.
	Name string `json:"name,omitempty"`
	// Wordmark is the text beside the logo in the sidebar. Left out, it is
	// the name; "" shows the logo alone.
	Wordmark *string `json:"wordmark,omitempty"`
	// Title is what follows the page's own name in the browser tab, after
	// " · ". Left out, it is the name.
	Title string `json:"title,omitempty"`
	// Logo replaces Humagen's mark in the sidebar.
	Logo *Logo `json:"logo,omitempty"`
	// Icons is a directory of files that replace the console's own icons,
	// each under the same name (BrandingIcons). A name it lacks keeps
	// Humagen's. The System document's only: icons are served before
	// sign-in.
	Icons string `json:"icons,omitempty"`
	// Manifest is the installable app's name and colors. The System
	// document's only.
	Manifest *Manifest `json:"manifest,omitempty"`
	// Palette replaces the console's colors in either theme.
	Palette *Palette `json:"palette,omitempty"`
	// Fonts replaces the console's typefaces. A face must be installed on
	// the device or be one the console bundles; none is downloaded.
	Fonts *Fonts `json:"fonts,omitempty"`
}

// Logo is an image file, SVG, PNG, WebP or JPEG, read at start.
type Logo struct {
	Path string `json:"path"`
	// Alt is the logo's text for a screen reader. Left out, the name.
	Alt string `json:"alt,omitempty"`
}

// Manifest is the installable app's identity (manifest.webmanifest).
type Manifest struct {
	Name            string `json:"name,omitempty"`
	ShortName       string `json:"short_name,omitempty"`
	Description     string `json:"description,omitempty"`
	ThemeColor      Color  `json:"theme_color,omitempty"`
	BackgroundColor Color  `json:"background_color,omitempty"`
}

// Palette is the console's colors. Each is a pair, one per theme, or a
// single color for both.
type Palette struct {
	// Accent is the workspace's accent: the console's default, until a
	// person picks one of their own in their settings.
	Accent *Pair `json:"accent,omitempty"`
	// Surface is the page's background.
	Surface *Pair `json:"surface,omitempty"`
	// Plane is the sidebar's and the panels' background.
	Plane *Pair `json:"plane,omitempty"`
	// Text is the main text color.
	Text *Pair `json:"text,omitempty"`
	// Rule is the color of lines between things.
	Rule *Pair `json:"rule,omitempty"`
}

// Pair is a color for the light theme and one for the dark. Written as one
// string, it is the same in both.
type Pair struct {
	Light Color `json:"light,omitempty"`
	Dark  Color `json:"dark,omitempty"`
}

// UnmarshalJSON reads a pair, or a single color for both themes.
func (p *Pair) UnmarshalJSON(b []byte) error {
	if len(b) > 0 && b[0] == '"' {
		var c Color
		if err := json.Unmarshal(b, &c); err != nil {
			return err
		}
		p.Light, p.Dark = c, c
		return nil
	}
	type pair Pair
	var raw pair
	if err := strictJSON(b, &raw); err != nil {
		return err
	}
	*p = Pair(raw)
	return nil
}

// Fonts is a CSS font-family list for each use.
type Fonts struct {
	// UI is the console's text.
	UI string `json:"ui,omitempty"`
	// Heading is titles and figures.
	Heading string `json:"heading,omitempty"`
	// Mono is code and identifiers.
	Mono string `json:"mono,omitempty"`
}

// Color is a hex color, #rgb or #rrggbb.
type Color string

var hexColor = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// RGB is the color's channels, or false when it is not a hex color.
func (c Color) RGB() (r, g, b uint8, ok bool) {
	s := string(c)
	if !hexColor.MatchString(s) {
		return 0, 0, 0, false
	}
	s = s[1:]
	if len(s) == 3 {
		s = string([]byte{s[0], s[0], s[1], s[1], s[2], s[2]})
	}
	v, _ := strconv.ParseUint(s, 16, 32)
	return uint8(v >> 16), uint8(v >> 8), uint8(v), true
}

// Hex is the color as #rrggbb, lowercase.
func (c Color) Hex() string {
	r, g, b, _ := c.RGB()
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// luminance is the color's relative luminance (WCAG 2).
func (c Color) luminance() float64 {
	r, g, b, _ := c.RGB()
	lin := func(v uint8) float64 {
		x := float64(v) / 255
		if x <= 0.04045 {
			return x / 12.92
		}
		return math.Pow((x+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

// Contrast is the WCAG 2 contrast ratio of two colors, 1 to 21.
func Contrast(a, b Color) float64 {
	la, lb := a.luminance(), b.luminance()
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// The console's own surface and text, as near as hex comes, which a palette
// that sets only one of them is checked against.
var (
	DefaultSurface = Pair{Light: "#ffffff", Dark: "#121212"}
	DefaultText    = Pair{Light: "#010101", Dark: "#ffffff"}
)

// The least contrast a palette may have: text on its surface (WCAG AA for
// body text) and the accent on its surface (AA for controls and large text).
const (
	MinTextContrast   = 4.5
	MinAccentContrast = 3.0
)

// BrandingIcons are the console's icon files, which branding.icons may
// replace.
var BrandingIcons = []string{"favicon.svg", "favicon-32.png", "apple-touch-icon.png", "icon.svg", "icon-192.png", "icon-512.png"}

// LogoTypes are the image types a logo may be, by extension.
var LogoTypes = map[string]string{
	".svg":  "image/svg+xml",
	".png":  "image/png",
	".webp": "image/webp",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
}

// fontFamily is a CSS font-family list with nothing that would end the
// declaration it is written into.
var fontFamily = regexp.MustCompile(`^[A-Za-z0-9 ,'"._-]{1,200}$`)

// check checks the block. system is whether it is the System document's,
// which alone may set what is served before sign-in. Files are read at
// start, not here.
func (b Branding) check(system bool) error {
	for _, f := range []struct{ name, v string }{{"name", b.Name}, {"title", b.Title}} {
		if err := checkName(f.v); err != nil {
			return fmt.Errorf("branding.%s: %w", f.name, err)
		}
	}
	if b.Wordmark != nil {
		if err := checkName(*b.Wordmark); err != nil {
			return fmt.Errorf("branding.wordmark: %w", err)
		}
	}
	if l := b.Logo; l != nil {
		if l.Path == "" {
			return errors.New("branding.logo.path is required")
		}
		if _, ok := LogoTypes[strings.ToLower(path.Ext(l.Path))]; !ok {
			return errors.New("branding.logo.path is an .svg, .png, .webp or .jpg file")
		}
		if err := checkName(l.Alt); err != nil {
			return fmt.Errorf("branding.logo.alt: %w", err)
		}
	}
	if !system && b.Icons != "" {
		return errors.New("branding.icons: icons are served before anyone signs in, so they are the deployment's; set them in the System document")
	}
	if m := b.Manifest; m != nil {
		if !system {
			return errors.New("branding.manifest: the installable app is the deployment's; set it in the System document")
		}
		for _, f := range []struct{ name, v string }{{"name", m.Name}, {"short_name", m.ShortName}} {
			if err := checkName(f.v); err != nil {
				return fmt.Errorf("branding.manifest.%s: %w", f.name, err)
			}
		}
		if err := checkText(m.Description, 300, false); err != nil {
			return fmt.Errorf("branding.manifest.description: %w", err)
		}
		for _, f := range []struct {
			name string
			c    Color
		}{{"theme_color", m.ThemeColor}, {"background_color", m.BackgroundColor}} {
			if err := checkColor(f.c); err != nil {
				return fmt.Errorf("branding.manifest.%s: %w", f.name, err)
			}
		}
	}
	if p := b.Palette; p != nil {
		if err := p.check(); err != nil {
			return fmt.Errorf("branding.palette.%w", err)
		}
		// A workspace's palette is checked over the deployment's, in
		// Config.Check, since that is what it is shown over.
		if system {
			if err := p.checkContrast(); err != nil {
				return fmt.Errorf("branding.palette.%w", err)
			}
		}
	}
	if f := b.Fonts; f != nil {
		for _, x := range []struct{ name, v string }{{"ui", f.UI}, {"heading", f.Heading}, {"mono", f.Mono}} {
			if x.v != "" && !fontFamily.MatchString(x.v) {
				return fmt.Errorf("branding.fonts.%s is a CSS font-family list: letters, digits, spaces, commas, quotes, dots, dashes and underscores", x.name)
			}
		}
	}
	return nil
}

func checkName(s string) error {
	if strings.ContainsAny(s, "<>") {
		return errors.New("plain text, without < or >")
	}
	return checkText(s, 80, false)
}

func checkColor(c Color) error {
	if c == "" {
		return nil
	}
	if _, _, _, ok := c.RGB(); !ok {
		return fmt.Errorf("%q is not a hex color, #rgb or #rrggbb", string(c))
	}
	return nil
}

// check checks every color.
func (p Palette) check() error {
	for _, f := range []struct {
		name string
		p    *Pair
	}{{"accent", p.Accent}, {"surface", p.Surface}, {"plane", p.Plane}, {"text", p.Text}, {"rule", p.Rule}} {
		if f.p == nil {
			continue
		}
		if err := checkColor(f.p.Light); err != nil {
			return fmt.Errorf("%s.light: %w", f.name, err)
		}
		if err := checkColor(f.p.Dark); err != nil {
			return fmt.Errorf("%s.dark: %w", f.name, err)
		}
	}
	return nil
}

// checkContrast checks the contrast of text and the accent on the surface in
// each theme, against the console's own for a side left out.
func (p Palette) checkContrast() error {
	side := func(p *Pair, dark bool, def Pair) (Color, bool) {
		pick := func(q Pair) Color {
			if dark {
				return q.Dark
			}
			return q.Light
		}
		if p != nil && pick(*p) != "" {
			return pick(*p), true
		}
		return pick(def), false
	}
	for _, dark := range []bool{false, true} {
		theme := "light"
		if dark {
			theme = "dark"
		}
		surface, setSurface := side(p.Surface, dark, DefaultSurface)
		text, setText := side(p.Text, dark, DefaultText)
		if setSurface || setText {
			if c := Contrast(text, surface); c < MinTextContrast {
				return fmt.Errorf("text.%s on surface.%s has a contrast of %.2f:1; it needs at least %.1f:1", theme, theme, c, MinTextContrast)
			}
		}
		if accent, setAccent := side(p.Accent, dark, Pair{}); setAccent {
			if c := Contrast(accent, surface); c < MinAccentContrast {
				return fmt.Errorf("accent.%s on surface.%s has a contrast of %.2f:1; it needs at least %.1f:1", theme, theme, c, MinAccentContrast)
			}
		}
	}
	return nil
}

// MergeBranding is a workspace's block over the deployment's: each field it
// sets wins, color by color and face by face. A workspace that renames the
// product renames the title and wordmark derived from the deployment's
// name, unless it sets them itself.
func MergeBranding(sys, ws Branding) Branding {
	out := sys
	if ws.Name != "" {
		out.Name = ws.Name
		if ws.Title == "" {
			out.Title = ""
		}
		if ws.Wordmark == nil {
			out.Wordmark = nil
		}
	}
	if ws.Title != "" {
		out.Title = ws.Title
	}
	if ws.Wordmark != nil {
		out.Wordmark = ws.Wordmark
	}
	if ws.Logo != nil {
		out.Logo = ws.Logo
	}
	if p := ws.Palette; p != nil {
		var merged Palette
		if sys.Palette != nil {
			merged = *sys.Palette
		}
		pick := func(a, b *Pair) *Pair {
			if b == nil {
				return a
			}
			if a == nil {
				return b
			}
			m := *a
			if b.Light != "" {
				m.Light = b.Light
			}
			if b.Dark != "" {
				m.Dark = b.Dark
			}
			return &m
		}
		merged.Accent = pick(merged.Accent, p.Accent)
		merged.Surface = pick(merged.Surface, p.Surface)
		merged.Plane = pick(merged.Plane, p.Plane)
		merged.Text = pick(merged.Text, p.Text)
		merged.Rule = pick(merged.Rule, p.Rule)
		out.Palette = &merged
	}
	if f := ws.Fonts; f != nil {
		var merged Fonts
		if sys.Fonts != nil {
			merged = *sys.Fonts
		}
		if f.UI != "" {
			merged.UI = f.UI
		}
		if f.Heading != "" {
			merged.Heading = f.Heading
		}
		if f.Mono != "" {
			merged.Mono = f.Mono
		}
		out.Fonts = &merged
	}
	return out
}
