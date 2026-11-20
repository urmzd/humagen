package platform

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/operations"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// A person acting on a session's browser by hand.
//
// An agent does browser work through an MCP provider it holds a write grant
// on (docs/guides/browser.md). SessionBrowser lets the person in the session
// look at that same browser and act on it: the same provider record, the same
// accepted tools, the same open connection the agent's runs use, so what the
// person does is what the agent finds next. Nothing here is a second way to
// reach a browser: a session whose agent holds no such grant has none.
//
// It is held to what the agent's own call is held to, and tighter:
//
//   - the caller must see the session and be allowed to post in it;
//   - the session's agent must hold "mcp:<provider>:write" now;
//   - the action policy for that agent, project and person must not hold
//     either category a write grant's tools fall under. A policy that asks
//     is refused here as one that says never is: who approves is the
//     agent's approvers, and the request for approval is the agent's to make;
//   - only a shared provider reached over HTTP is acted on. A stdio server in
//     an isolated run is that run's own, and a person-scoped provider is not
//     reached outside a run;
//   - only tools the admin accepted are called, and each call checks again
//     that the server still offers the tool as accepted (internal/mcptools);
//   - an address is http or https, and a target is a reference from the
//     page's own tree, never a selector.
//
// Every action that can change a page is audited, without what was typed.

// The actions a person may take on a session's browser.
const (
	BrowserLook     = "look"
	BrowserNavigate = "navigate"
	BrowserBack     = "back"
	BrowserClick    = "click"
	BrowserType     = "type"
	BrowserPress    = "press"
)

// Limits on one browser action and on what it answers with.
const (
	MaxBrowserURL        = 2048
	MaxBrowserText       = 4000
	MaxBrowserElements   = 400
	MaxBrowserName       = 160
	MaxBrowserScreenshot = 8 << 20
	browserTimeout       = 45 * time.Second
)

var (
	// ErrNoBrowser reports a session with no browser a person can act on.
	ErrNoBrowser = errors.New("platform: this session has no browser to act on")
	// ErrBrowserInput reports an action that is not well formed.
	ErrBrowserInput = errors.New("platform: invalid browser action")
	// ErrBrowserFailed reports a browser that could not be reached, or an
	// action it refused.
	ErrBrowserFailed = errors.New("platform: the browser did not do that")
)

// BrowserAction is what a person asks of a session's browser.
type BrowserAction struct {
	// Provider names the browser, for a session whose agent holds more than
	// one. Empty is the first.
	Provider string `json:"provider,omitempty"`
	// Action is look, navigate, back, click, type or press.
	Action string `json:"action"`
	// URL is where navigate goes: http or https.
	URL string `json:"url,omitempty"`
	// Target is the element click and type act on: a reference from the
	// last view's elements.
	Target string `json:"target,omitempty"`
	// Element is how the person saw the target named, kept for the record.
	Element string `json:"element,omitempty"`
	// Text is what type enters.
	Text string `json:"text,omitempty"`
	// Submit presses Enter after typing.
	Submit bool `json:"submit,omitempty"`
	// Key is what press presses, such as Enter or ArrowDown.
	Key string `json:"key,omitempty"`
}

// BrowserImage is a screenshot of the page as it is now.
type BrowserImage struct {
	MediaType string `json:"media_type"`
	// Data is the image's bytes, base64 in JSON.
	Data []byte `json:"data"`
}

// BrowserElement is one thing on the page a person can act on.
type BrowserElement struct {
	// Ref is what click and type take as their target.
	Ref  string `json:"ref"`
	Role string `json:"role"`
	Name string `json:"name,omitempty"`
	// Box is where it is in the screenshot, in CSS pixels: x, y, width,
	// height. Absent for an element with no place on the screen.
	Box []int `json:"box,omitempty"`
}

// BrowserView is the browser after an action: where it is and what it shows.
type BrowserView struct {
	Provider   string           `json:"provider"`
	Action     string           `json:"action"`
	URL        string           `json:"url,omitempty"`
	Title      string           `json:"title,omitempty"`
	Screenshot *BrowserImage    `json:"screenshot,omitempty"`
	Elements   []BrowserElement `json:"elements"`
	// More is how many elements were left out for being over the limit.
	More int `json:"more,omitempty"`
}

var (
	browserRef = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	browserKey = regexp.MustCompile(`^[A-Za-z0-9]{1,24}(\+[A-Za-z0-9]{1,24}){0,3}$`)
)

// checked is in with its fields trimmed, or why it is not well formed.
func (in BrowserAction) checked() (BrowserAction, error) {
	bad := func(why string) (BrowserAction, error) {
		return BrowserAction{}, fmt.Errorf("%w: %s", ErrBrowserInput, why)
	}
	in.Provider = strings.TrimSpace(in.Provider)
	if in.Provider != "" && !toolprovider.ValidName(in.Provider) {
		return bad("no such browser")
	}
	in.Element = cutRunes(strings.Join(strings.Fields(in.Element), " "), MaxBrowserName)
	switch in.Action {
	case BrowserLook, BrowserBack:
	case BrowserNavigate:
		in.URL = strings.TrimSpace(in.URL)
		u, err := url.Parse(in.URL)
		if err != nil || len(in.URL) > MaxBrowserURL || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return bad("an address starts with http:// or https://")
		}
	case BrowserClick:
		if !browserRef.MatchString(in.Target) {
			return bad("click takes an element of the page")
		}
	case BrowserType:
		if !browserRef.MatchString(in.Target) {
			return bad("type takes an element of the page")
		}
		if in.Text == "" || len(in.Text) > MaxBrowserText {
			return bad(fmt.Sprintf("type takes 1 to %d characters", MaxBrowserText))
		}
	case BrowserPress:
		if !browserKey.MatchString(in.Key) {
			return bad("press takes a key, such as Enter or ArrowDown")
		}
	default:
		return bad("the action is look, navigate, back, click, type or press")
	}
	return in, nil
}

// browserTools are the tools of the pinned Playwright MCP server this needs.
const (
	toolBrowserNavigate   = "browser_navigate"
	toolBrowserBack       = "browser_navigate_back"
	toolBrowserClick      = "browser_click"
	toolBrowserType       = "browser_type"
	toolBrowserPress      = "browser_press_key"
	toolBrowserSnapshot   = "browser_snapshot"
	toolBrowserScreenshot = "browser_take_screenshot"
)

// browserProviders are the providers the agent holds a write grant on whose
// accepted tools include a screenshot: its browsers, in the order granted.
func (p *Platform) browserProviders(ctx context.Context, tenant string, tools []string) []string {
	var out []string
	for _, grant := range tools {
		parsed, ok, err := wire.ParseMCPGrant(grant)
		if !ok || err != nil || !parsed.Write || !toolprovider.ValidName(parsed.Provider) || slices.Contains(out, parsed.Provider) {
			continue
		}
		prov, err := p.ToolProviders.GetProvider(ctx, tenant, parsed.Provider)
		if err != nil || prov.Scope() != toolprovider.ScopeShared || prov.Transport != toolprovider.TransportHTTP {
			continue
		}
		snap, err := p.ToolProviders.Snapshot(ctx, tenant, parsed.Provider, 0)
		if err != nil {
			continue
		}
		if _, ok := snap.Tool(toolBrowserScreenshot); ok {
			out = append(out, parsed.Provider)
		}
	}
	return out
}

// SessionBrowser does what a person asked of the session's browser and
// answers with the page as it is afterwards: its address, its title, a
// screenshot, and the elements a person can act on next.
func (p *Platform) SessionBrowser(ctx context.Context, who auth.Principal, id string, in BrowserAction) (BrowserView, error) {
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return BrowserView{}, err
	}
	if th.Session == nil || th.Session.Agent == "" {
		return BrowserView{}, ErrNoBrowser
	}
	if who.Kind != auth.User {
		return BrowserView{}, fmt.Errorf("%w: only a person acts on a browser by hand", ErrForbidden)
	}
	// Acting on the agent's browser is asking of it, as a message is.
	if err := p.MayPost(who, Place{Project: th.Project, Thread: th.ID}); err != nil {
		return BrowserView{}, err
	}
	if in, err = in.checked(); err != nil {
		return BrowserView{}, err
	}
	agent := th.Session.Agent
	rev, err := p.Catalog().Get(ctx, who.Tenant, agent, 0)
	if err != nil {
		return BrowserView{}, ErrNoBrowser
	}
	providers := p.browserProviders(ctx, who.Tenant, rev.Spec.Tools)
	provider := in.Provider
	switch {
	case len(providers) == 0, provider != "" && !slices.Contains(providers, provider):
		return BrowserView{}, ErrNoBrowser
	case provider == "":
		provider = providers[0]
	}
	for _, c := range []operations.Category{operations.Other, operations.ExternalMessages} {
		// A rule tightens: none set leaves the grant as it is, as for the agent.
		if level := p.ActionDecision(who.Tenant, th.Project, agent, who.Subject, c); level == operations.Never || level == operations.Ask {
			return BrowserView{}, fmt.Errorf("%w: the action policy does not allow browser actions without review here; ask the agent", ErrForbidden)
		}
	}
	if p.toolGrants == nil {
		return BrowserView{}, ErrNoBrowser
	}
	ctx, cancel := context.WithTimeout(ctx, browserTimeout)
	defer cancel()
	tools, ok, err := p.toolGrants.Expand(ctx, who.Tenant, wire.MCPGrant{Provider: provider, Write: true}.String())
	if err != nil || !ok {
		return BrowserView{}, fmt.Errorf("%w: it could not be reached", ErrBrowserFailed)
	}
	b := browserSession{provider: provider, tools: tools}

	if in.Action != BrowserLook {
		change := audit.Change{
			Route: "POST /api/sessions/{id}/browser", Rule: string(auth.RunsWrite),
			Action: "session.browser." + in.Action, Resource: "session", ID: th.ID, Scope: "project",
			Project: th.Project, Thread: th.ID, Agent: agent,
			After: browserAudit(provider, in),
		}
		attempt, err := p.BeginAudit(who, change)
		if err != nil {
			return BrowserView{}, err
		}
		err = b.act(ctx, in)
		if ferr := attempt.Finish(err); ferr != nil {
			p.LogAuditCompletionFailure(ferr)
		}
		if err != nil {
			return BrowserView{}, err
		}
	}
	view, err := b.view(ctx)
	if err != nil {
		return BrowserView{}, err
	}
	view.Action = in.Action
	return view, nil
}

// browserAudit is what the audit keeps of an action. What was typed is not
// kept: only how much.
func browserAudit(provider string, in BrowserAction) map[string]string {
	out := map[string]string{"provider": provider}
	switch in.Action {
	case BrowserNavigate:
		out["url"] = in.URL
	case BrowserClick:
		out["target"], out["element"] = in.Target, in.Element
	case BrowserType:
		out["target"], out["element"] = in.Target, in.Element
		out["characters"] = strconv.Itoa(len([]rune(in.Text)))
		out["submit"] = strconv.FormatBool(in.Submit)
	case BrowserPress:
		out["key"] = in.Key
	}
	return out
}

// browserGrants expands a provider grant into its accepted tools: the
// platform's mcptools.Grants, which a run's tools come from too.
type browserGrants interface {
	Expand(ctx context.Context, tenant, grant string) ([]saigetypes.Tool, bool, error)
}

// browserSession is one provider's accepted tools, as the grant offers them.
type browserSession struct {
	provider string
	tools    []saigetypes.Tool
}

// call runs one of the provider's tools. A tool the admin did not accept is
// not there to call.
func (b browserSession) call(ctx context.Context, name string, args map[string]any) (saigetypes.ToolResult, error) {
	want := toolprovider.QualifiedName(b.provider, name)
	for _, t := range b.tools {
		if t.Definition().Name != want {
			continue
		}
		rich, ok := t.(saigetypes.RichTool)
		if !ok {
			break
		}
		res, err := rich.ExecuteRich(ctx, args)
		if err != nil {
			return saigetypes.ToolResult{}, fmt.Errorf("%w: %s", ErrBrowserFailed, browserReason(err))
		}
		return res, nil
	}
	return saigetypes.ToolResult{}, fmt.Errorf("%w: %s is not among the tools accepted from %s", ErrBrowserFailed, name, b.provider)
}

// browserReason is the first line of why a call failed, short enough to show.
func browserReason(err error) string {
	line, _, _ := strings.Cut(strings.TrimSpace(err.Error()), "\n")
	return cutRunes(line, 300)
}

func (b browserSession) act(ctx context.Context, in BrowserAction) error {
	var err error
	switch in.Action {
	case BrowserNavigate:
		_, err = b.call(ctx, toolBrowserNavigate, map[string]any{"url": in.URL})
	case BrowserBack:
		_, err = b.call(ctx, toolBrowserBack, map[string]any{})
	case BrowserClick:
		args := map[string]any{"target": in.Target}
		if in.Element != "" {
			args["element"] = in.Element
		}
		_, err = b.call(ctx, toolBrowserClick, args)
	case BrowserType:
		args := map[string]any{"target": in.Target, "text": in.Text}
		if in.Element != "" {
			args["element"] = in.Element
		}
		if in.Submit {
			args["submit"] = true
		}
		_, err = b.call(ctx, toolBrowserType, args)
	case BrowserPress:
		_, err = b.call(ctx, toolBrowserPress, map[string]any{"key": in.Key})
	}
	return err
}

// view reads the page as it is now: its tree, with where each element is,
// then its pixels. The tree is read without a file name so the server
// answers with it (docs/guides/browser.md).
func (b browserSession) view(ctx context.Context) (BrowserView, error) {
	out := BrowserView{Provider: b.provider, Elements: []BrowserElement{}}
	if snap, err := b.call(ctx, toolBrowserSnapshot, map[string]any{"boxes": true}); err == nil {
		out.URL, out.Title, out.Elements, out.More = readBrowserSnapshot(snap.Text())
	}
	shot, err := b.call(ctx, toolBrowserScreenshot, map[string]any{"type": "png", "scale": "css"})
	if err != nil {
		return BrowserView{}, err
	}
	for _, part := range shot.Parts {
		img, ok := part.(saigetypes.ImagePart)
		if !ok || len(img.Source.Inline) == 0 {
			continue
		}
		if len(img.Source.Inline) > MaxBrowserScreenshot {
			return BrowserView{}, fmt.Errorf("%w: the screenshot is too large to show", ErrBrowserFailed)
		}
		out.Screenshot = &BrowserImage{MediaType: string(img.Source.MediaType), Data: img.Source.Inline}
		break
	}
	if out.Screenshot == nil {
		return BrowserView{}, fmt.Errorf("%w: it answered without a screenshot", ErrBrowserFailed)
	}
	return out, nil
}

var (
	browserLine  = regexp.MustCompile(`^\s*- ([A-Za-z][A-Za-z0-9_-]*)(?: "((?:[^"\\]|\\.)*)")?((?: \[[^\]]*\])*)`)
	browserAttr  = regexp.MustCompile(`\[(ref|box)=([^\]]*)\]`)
	browserActed = []string{"link", "button", "textbox", "searchbox", "combobox", "checkbox", "radio", "switch", "tab", "menuitem", "option", "slider", "spinbutton", "listbox", "treeitem"}
)

// readBrowserSnapshot reads the Playwright MCP server's page tree: the
// page's address and title, and each element a person acts on (a link, a
// button, a field) with its reference and its place on the screen.
func readBrowserSnapshot(text string) (address, title string, elements []BrowserElement, more int) {
	elements = []BrowserElement{}
	for _, line := range strings.Split(text, "\n") {
		if v, ok := strings.CutPrefix(line, "- Page URL: "); ok && address == "" {
			address = cutRunes(strings.TrimSpace(v), MaxBrowserURL)
			continue
		}
		if v, ok := strings.CutPrefix(line, "- Page Title: "); ok && title == "" {
			title = cutRunes(strings.TrimSpace(v), MaxBrowserName)
			continue
		}
		m := browserLine.FindStringSubmatch(line)
		if m == nil || !slices.Contains(browserActed, m[1]) {
			continue
		}
		el := BrowserElement{Role: m[1], Name: cutRunes(strings.NewReplacer(`\"`, `"`, `\\`, `\`).Replace(m[2]), MaxBrowserName)}
		for _, attr := range browserAttr.FindAllStringSubmatch(m[3], -1) {
			switch attr[1] {
			case "ref":
				el.Ref = attr[2]
			case "box":
				el.Box = browserBox(attr[2])
			}
		}
		if !browserRef.MatchString(el.Ref) {
			continue
		}
		if len(elements) >= MaxBrowserElements {
			more++
			continue
		}
		elements = append(elements, el)
	}
	return address, title, elements, more
}

// browserBox reads "x,y,width,height". Anything else is no box.
func browserBox(s string) []int {
	parts := strings.Split(s, ",")
	if len(parts) != 4 {
		return nil
	}
	box := make([]int, 4)
	for i, part := range parts {
		f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil || f < -1e6 || f > 1e6 {
			return nil
		}
		box[i] = int(f)
	}
	if box[2] <= 0 || box[3] <= 0 {
		return nil
	}
	return box
}

func cutRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max])
	}
	return s
}
