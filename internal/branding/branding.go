// Package branding turns the spec's branding blocks into what the console is
// served: its name, logo, icons, manifest and a stylesheet of its colors and
// typefaces. Files are read once, at start, so a missing or unreadable one
// fails the start rather than a page.
package branding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/spec"
)

// DefaultWordmark is the sidebar's text when nothing names the product.
const DefaultWordmark = "humagen"

// Size limits for the files branding reads.
const (
	maxLogo = 512 << 10
	maxIcon = 1 << 20
)

// Asset is a file read at start.
type Asset struct {
	Type string
	Body []byte
}

// Brand is one level's branding, resolved: the deployment's, or a
// workspace's over it.
type Brand struct {
	Name     string
	Wordmark string
	Title    string
	Logo     *Asset
	LogoAlt  string
	// CSS is the stylesheet of the palette and fonts, empty with neither.
	CSS string
	// Spec is the merged block, for what is derived again per request.
	Spec spec.Branding
}

// Set is the deployment's branding and each workspace's.
type Set struct {
	System     Brand
	Icons      map[string]Asset
	Manifest   spec.Manifest
	Workspaces map[string]Brand
}

// Load reads the System document's branding and each Platform document's.
// The System document should have its defaults applied.
func Load(cfg spec.Config) (*Set, error) {
	sys := cfg.System.Spec.Branding
	if sys.Name == "" {
		sys.Name = cfg.System.Metadata.DisplayName
	}
	s := &Set{Icons: map[string]Asset{}, Workspaces: map[string]Brand{}}
	var err error
	if s.System, err = resolve(sys); err != nil {
		return nil, fmt.Errorf("system: %w", err)
	}
	if sys.Manifest != nil {
		s.Manifest = *sys.Manifest
	}
	if dir := sys.Icons; dir != "" {
		if s.Icons, err = readIcons(dir); err != nil {
			return nil, fmt.Errorf("system: branding.icons: %w", err)
		}
	}
	for _, p := range cfg.Platforms {
		if p.Spec.Branding == (spec.Branding{}) {
			continue
		}
		b, err := resolve(spec.MergeBranding(sys, p.Spec.Branding))
		if err != nil {
			return nil, fmt.Errorf("platform %q: %w", p.Metadata.Name, err)
		}
		s.Workspaces[p.Metadata.Name] = b
	}
	return s, nil
}

// For is a tenant's branding: its workspace's, else the deployment's.
func (s *Set) For(tenant string) Brand {
	if s == nil {
		return Default()
	}
	if b, ok := s.Workspaces[tenant]; ok {
		return b
	}
	return s.System
}

// Default is Humagen's own branding.
func Default() Brand {
	b, _ := resolve(spec.Branding{})
	return b
}

func resolve(b spec.Branding) (Brand, error) {
	out := Brand{Spec: b, Name: b.Name}
	if out.Name == "" {
		out.Name = spec.DefaultName
	}
	out.Title = b.Title
	if out.Title == "" {
		out.Title = out.Name
	}
	switch {
	case b.Wordmark != nil:
		out.Wordmark = *b.Wordmark
	case out.Name == spec.DefaultName:
		out.Wordmark = DefaultWordmark
	default:
		out.Wordmark = out.Name
	}
	if l := b.Logo; l != nil {
		a, err := readFile(l.Path, maxLogo)
		if err != nil {
			return out, fmt.Errorf("branding.logo: %w", err)
		}
		a.Type = spec.LogoTypes[strings.ToLower(filepath.Ext(l.Path))]
		if a.Type == "image/svg+xml" {
			if err := checkSVG(a.Body); err != nil {
				return out, fmt.Errorf("branding.logo: %s: %w", l.Path, err)
			}
		}
		out.Logo, out.LogoAlt = &a, l.Alt
		if out.LogoAlt == "" {
			out.LogoAlt = out.Name
		}
	}
	out.CSS = CSS(b)
	return out, nil
}

func readFile(path string, limit int64) (Asset, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Asset{}, err
	}
	if st.IsDir() {
		return Asset{}, fmt.Errorf("%s is a directory", path)
	}
	if st.Size() > limit {
		return Asset{}, fmt.Errorf("%s is %d bytes; at most %d", path, st.Size(), limit)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Asset{}, err
	}
	return Asset{Body: b}, nil
}

// readIcons reads the directory's icons. A file under a name the console has
// no icon by is refused, so a misspelled one is not quietly ignored.
func readIcons(dir string) (map[string]Asset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	icons := map[string]Asset{}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if !slices.Contains(spec.BrandingIcons, name) {
			return nil, fmt.Errorf("%s: the console has no icon called %q; it has %s", dir, name, strings.Join(spec.BrandingIcons, ", "))
		}
		a, err := readFile(filepath.Join(dir, name), maxIcon)
		if err != nil {
			return nil, err
		}
		a.Type = "image/png"
		if strings.HasSuffix(name, ".svg") {
			a.Type = "image/svg+xml"
			if err := checkSVG(a.Body); err != nil {
				return nil, fmt.Errorf("%s: %w", name, err)
			}
		}
		icons[name] = a
	}
	return icons, nil
}

var svgScript = regexp.MustCompile(`(?i)<script|\son[a-z]+\s*=|javascript:`)

// checkSVG refuses an SVG that is not one, or that carries a script. It is
// served from the console's own origin, where a script would run as it.
func checkSVG(b []byte) error {
	if !bytes.Contains(b, []byte("<svg")) {
		return fmt.Errorf("not an SVG image")
	}
	if svgScript.Match(b) {
		return fmt.Errorf("an SVG with a script or an event handler is refused")
	}
	return nil
}

// CSS is the stylesheet for a block's palette and fonts, written over the
// console's own tokens. Empty with neither.
func CSS(b spec.Branding) string {
	var sb strings.Builder
	p := b.Palette
	if p != nil {
		for _, dark := range []bool{false, true} {
			theme, pick := "light", func(q *spec.Pair) string {
				if q == nil || q.Light == "" {
					return ""
				}
				return q.Light.Hex()
			}
			if dark {
				theme = "dark"
				pick = func(q *spec.Pair) string {
					if q == nil || q.Dark == "" {
						return ""
					}
					return q.Dark.Hex()
				}
			}
			var decls []string
			set := func(name, v string) {
				if v != "" {
					decls = append(decls, name+": "+v)
				}
			}
			surface, text, plane, rule := pick(p.Surface), pick(p.Text), pick(p.Plane), pick(p.Rule)
			fg := text
			if fg == "" {
				fg = "var(--aui-fg)"
			}
			set("--aui-bg", surface)
			if surface != "" {
				set("--aui-surface", "color-mix(in oklch, "+surface+" 97%, "+fg+")")
				set("--humagen-panel", surface)
				if plane == "" {
					plane = "color-mix(in oklch, " + surface + " 95%, " + fg + ")"
				}
			}
			set("--humagen-plane", plane)
			if text != "" {
				set("--aui-fg", text)
				bg := surface
				if bg == "" {
					bg = "var(--aui-bg)"
				}
				set("--aui-muted", "color-mix(in oklch, "+text+" 72%, "+bg+")")
				set("--aui-hover", "color-mix(in oklch, "+text+" 6%, transparent)")
				set("--humagen-ink", "color-mix(in oklch, "+text+" 44%, transparent)")
				set("--humagen-ink-faint", "color-mix(in oklch, "+text+" 13%, transparent)")
				if rule == "" {
					rule = "color-mix(in oklch, " + text + " 9%, transparent)"
				}
			}
			set("--humagen-rule", rule)
			if len(decls) > 0 {
				fmt.Fprintf(&sb, ":root[data-theme=%q], :root [data-theme=%q] { %s; }\n", theme, theme, strings.Join(decls, "; "))
			}
			if a := pick(p.Accent); a != "" {
				on := "#ffffff"
				if spec.Contrast(spec.Color(a), "#000000") > spec.Contrast(spec.Color(a), "#ffffff") {
					on = "#000000"
				}
				fmt.Fprintf(&sb, ":root[data-theme=%q][data-accent=\"brand\"], :root[data-accent=\"brand\"] [data-theme=%q] { --aui-accent: %s; --aui-accent-soft: color-mix(in oklch, %s 14%%, transparent); --aui-accent-fg: %s; }\n", theme, theme, a, a, on)
			}
		}
	}
	if f := b.Fonts; f != nil {
		var decls []string
		if f.UI != "" {
			decls = append(decls, "--font-aui-sans: "+f.UI)
		}
		if f.Heading != "" {
			decls = append(decls, "--font-humagen-display: "+f.Heading)
		}
		if f.Mono != "" {
			decls = append(decls, "--font-aui-mono: "+f.Mono)
		}
		if len(decls) > 0 {
			fmt.Fprintf(&sb, ":root:root { %s; }\n", strings.Join(decls, "; "))
		}
		if f.UI != "" {
			sb.WriteString("body { font-family: var(--font-aui-sans); }\n")
		}
	}
	return sb.String()
}

// plain reports whether the deployment is Humagen's own, so the console's
// files are served as they were built.
func (s *Set) plain() bool {
	b := s.System
	return b.Name == spec.DefaultName && b.Title == spec.DefaultName && b.CSS == "" && s.Manifest == (spec.Manifest{})
}

// Page is the console's index.html with the deployment's branding: its
// name in the title and the installed app's, its theme color, and its
// stylesheet after the console's own.
func (s *Set) Page(index []byte) []byte {
	if s.plain() {
		return index
	}
	b := s.System
	page := string(index)
	name := html.EscapeString(b.Title)
	appName := b.Name
	if s.Manifest.ShortName != "" {
		appName = s.Manifest.ShortName
	}
	page = replaceTag(page, `<title>`, `</title>`, name)
	page = replaceAttr(page, `<meta name="application-name" content="`, html.EscapeString(b.Name))
	page = replaceAttr(page, `<meta name="apple-mobile-web-app-title" content="`, html.EscapeString(appName))
	if d := s.Manifest.Description; d != "" {
		page = replaceAttr(page, `<meta name="description" content="`, html.EscapeString(d))
	}
	var head strings.Builder
	if c := s.Manifest.ThemeColor; c != "" {
		page = dropThemeColors(page)
		fmt.Fprintf(&head, `<meta name="theme-color" content="%s" />`, c.Hex())
	}
	if b.CSS != "" {
		fmt.Fprintf(&head, `<style id="humagen-branding">%s</style>`, b.CSS)
	}
	if head.Len() > 0 {
		page = strings.Replace(page, "</head>", head.String()+"</head>", 1)
	}
	return []byte(page)
}

func replaceTag(page, open, close, inner string) string {
	i := strings.Index(page, open)
	if i < 0 {
		return page
	}
	j := strings.Index(page[i:], close)
	if j < 0 {
		return page
	}
	return page[:i+len(open)] + inner + page[i+j:]
}

func replaceAttr(page, prefix, value string) string {
	i := strings.Index(page, prefix)
	if i < 0 {
		return page
	}
	start := i + len(prefix)
	j := strings.IndexByte(page[start:], '"')
	if j < 0 {
		return page
	}
	return page[:start] + value + page[start+j:]
}

var themeColorMeta = regexp.MustCompile(`\s*<meta name="theme-color"[^>]*>`)

func dropThemeColors(page string) string { return themeColorMeta.ReplaceAllString(page, "") }

// WebManifest is the console's built manifest with the deployment's name,
// description and colors over its own. A manifest that does not read is
// served as it is.
func (s *Set) WebManifest(built []byte) []byte {
	if s.plain() {
		return built
	}
	var m map[string]any
	if json.Unmarshal(built, &m) != nil {
		return built
	}
	name, short := s.System.Name, s.System.Name
	if s.Manifest.Name != "" {
		name = s.Manifest.Name
	}
	if s.Manifest.ShortName != "" {
		short = s.Manifest.ShortName
	}
	m["name"], m["short_name"] = name, short
	if d := s.Manifest.Description; d != "" {
		m["description"] = d
	}
	if c := s.Manifest.ThemeColor; c != "" {
		m["theme_color"] = c.Hex()
	}
	if c := s.Manifest.BackgroundColor; c != "" {
		m["background_color"] = c.Hex()
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return built
	}
	return out
}
