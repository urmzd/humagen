package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/toolprovider"
)

// onePixel is a real PNG, since a tool result's image is checked by its bytes.
var onePixel, _ = base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==")

// browserFake is an MCP server that answers as the pinned Playwright MCP
// server does (docs/guides/browser.md): a page with an address, a title and
// a tree with references and boxes, and a screenshot as image content. It
// keeps every call, so a test says what reached the browser.
type browserFake struct {
	URL   string
	tools []toolprovider.Tool

	mu    sync.Mutex
	page  string
	calls []string
}

func newBrowserFake(t testing.TB) *browserFake {
	t.Helper()
	f := &browserFake{page: "about:blank"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "playwright", Version: "1"}, nil)
	text := func(s string) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: s}}}
	}
	add := func(name string, props map[string]any, run func(args map[string]any) *mcp.CallToolResult) {
		schema := map[string]any{"type": "object", "properties": props}
		raw, err := json.Marshal(schema)
		if err != nil {
			t.Fatal(err)
		}
		tool, err := toolprovider.NewTool(name, "Fixture "+name, raw)
		if err != nil {
			t.Fatal(err)
		}
		f.tools = append(f.tools, tool)
		srv.AddTool(&mcp.Tool{Name: name, Description: "Fixture " + name, InputSchema: schema},
			func(_ context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
				var args map[string]any
				_ = json.Unmarshal(req.Params.Arguments, &args)
				f.mu.Lock()
				defer f.mu.Unlock()
				keys := make([]string, 0, len(args))
				for k, v := range args {
					keys = append(keys, fmt.Sprintf("%s=%v", k, v))
				}
				slices.Sort(keys)
				f.calls = append(f.calls, strings.TrimSpace(name+" "+strings.Join(keys, " ")))
				return run(args), nil
			})
	}
	str := map[string]any{"type": "string"}
	add("browser_navigate", map[string]any{"url": str}, func(args map[string]any) *mcp.CallToolResult {
		f.page, _ = args["url"].(string)
		return text("### Page\n- Page URL: " + f.page)
	})
	add("browser_navigate_back", map[string]any{}, func(map[string]any) *mcp.CallToolResult { return text("back") })
	add("browser_click", map[string]any{"target": str, "element": str}, func(args map[string]any) *mcp.CallToolResult {
		if args["target"] == "gone" {
			return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "Error: Ref gone not found in the current page snapshot"}}}
		}
		return text("clicked")
	})
	add("browser_type", map[string]any{"target": str, "element": str, "text": str, "submit": map[string]any{"type": "boolean"}}, func(map[string]any) *mcp.CallToolResult { return text("typed") })
	add("browser_press_key", map[string]any{"key": str}, func(map[string]any) *mcp.CallToolResult { return text("pressed") })
	add("browser_snapshot", map[string]any{"boxes": map[string]any{"type": "boolean"}}, func(map[string]any) *mcp.CallToolResult {
		return text("### Page\n- Page URL: " + f.page + "\n- Page Title: Fixture page\n### Snapshot\n```yaml\n" +
			"- generic [active] [ref=e1] [box=0,0,800,600]:\n" +
			"  - heading \"Fixture\" [level=1] [ref=e2] [box=10,10,300,40]\n" +
			"  - link \"Learn \\\"more\\\"\" [ref=e3] [cursor=pointer] [box=10,60,82,18]:\n" +
			"    - /url: https://example.com/more\n" +
			"  - textbox \"Search\" [ref=e4] [box=10,90,200,24]\n" +
			"  - button \"Approve\" [ref=e5] [cursor=pointer]\n```")
	})
	add("browser_take_screenshot", map[string]any{"type": str, "scale": str}, func(map[string]any) *mcp.CallToolResult {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "### Result"}, &mcp.ImageContent{MIMEType: "image/png", Data: onePixel}}}
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return srv }, nil)
	ts := httptest.NewServer(h)
	t.Cleanup(func() {
		ts.CloseClientConnections()
		ts.Close()
	})
	f.URL = ts.URL + "/mcp"
	return f
}

// seed declares the fake as a tenant's shared browser, every tool accepted.
func (f *browserFake) seed(tenant, name string) platform.ToolProviderSeed {
	return platform.ToolProviderSeed{
		Provider: toolprovider.Provider{Tenant: tenant, Name: name, Transport: toolprovider.TransportHTTP, Endpoint: f.URL},
		Accept:   slices.Clone(f.tools),
	}
}

// seen is every call so far, and forgets them.
func (f *browserFake) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

const surferSpec = `{"name":"surfer","model":"offline","provider":"offline","system_prompt":"Browse.","tools":["mcp:browser:write"]}`

// exerciseBrowserContract answers the browser route's own statuses against
// the contract: the page after a look, and a browser that refused an action.
func exerciseBrowserContract(t *testing.T, c *contract, token string) {
	t.Helper()
	const route = "/api/sessions/{id}/browser"
	c.call(token, "POST", "/api/agents", "/api/agents", surferSpec, 201)
	s := c.call(token, "POST", "/api/sessions", "/api/sessions", `{"agent":"surfer","project":"~","text":"Open the page"}`, 201)
	path := "/api/sessions/" + s.str("id") + "/browser"
	view := c.call(token, "POST", route, path, `{"action":"look"}`, 200)
	if view.str("title") != "Fixture page" || obj(view.field("screenshot"))["media_type"] != "image/png" {
		t.Fatalf("the page was not read: %s", view.raw)
	}
	c.call(token, "POST", route, path, `{"action":"click","target":"gone"}`, 502)
	c.call(token, "POST", route, path, `{"action":"navigate","url":"file:///etc/passwd"}`, 400)
}

func browserFixture(t *testing.T) (*fixture, *browserFake, string) {
	t.Helper()
	browser := newBrowserFake(t)
	other := newBrowserFake(t)
	f := newFixtureWith(t, platform.Config{Ext: platform.Extensions{ToolProviders: []platform.ToolProviderSeed{
		browser.seed("acme", "browser"),
		// Reached with each person's own credential: never by this route.
		{Provider: toolprovider.Provider{Tenant: "acme", Name: "mine", Transport: toolprovider.TransportHTTP, Endpoint: other.URL, CredentialScope: toolprovider.ScopePerson}, Accept: slices.Clone(other.tools)},
	}}})
	token := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	want(t, f, token, "POST", "/api/agents", surferSpec, 201)
	want(t, f, token, "POST", "/api/agents", helperSpec, 201)
	return f, browser, token
}

func sessionWith(t *testing.T, f *fixture, token, agent string) string {
	t.Helper()
	s := want(t, f, token, "POST", "/api/sessions", fmt.Sprintf(`{"agent":%q,"project":"~","text":"Open the page"}`, agent), 201)
	return "/api/sessions/" + s["id"].(string) + "/browser"
}

func TestAPersonActsOnTheSessionsBrowserByHand(t *testing.T) {
	f, browser, token := browserFixture(t)
	path := sessionWith(t, f, token, "surfer")
	browser.seen()

	view := want(t, f, token, "POST", path, `{"action":"navigate","url":"https://example.com/"}`, 200)
	if view["url"] != "https://example.com/" || view["title"] != "Fixture page" || view["provider"] != "browser" || view["action"] != "navigate" {
		t.Fatalf("view = %v", view)
	}
	shot, _ := view["screenshot"].(map[string]any)
	if data, _ := base64.StdEncoding.DecodeString(fmt.Sprint(shot["data"])); shot["media_type"] != "image/png" || string(data) != string(onePixel) {
		t.Fatalf("screenshot = %v", shot)
	}
	// Only what a person acts on is listed, with its reference and its place.
	var got []string
	for _, e := range view["elements"].([]any) {
		el := e.(map[string]any)
		got = append(got, fmt.Sprintf("%v %v %q %v", el["ref"], el["role"], el["name"], el["box"]))
	}
	wantEls := []string{`e3 link "Learn \"more\"" [10 60 82 18]`, `e4 textbox "Search" [10 90 200 24]`, `e5 button "Approve" <nil>`}
	if !slices.Equal(got, wantEls) {
		t.Fatalf("elements = %q, want %q", got, wantEls)
	}
	want(t, f, token, "POST", path, `{"action":"click","target":"e5","element":"Approve"}`, 200)
	want(t, f, token, "POST", path, `{"action":"type","target":"e4","element":"Search","text":"hunter2","submit":true}`, 200)
	want(t, f, token, "POST", path, `{"action":"press","key":"Enter"}`, 200)
	want(t, f, token, "POST", path, `{"action":"back"}`, 200)
	want(t, f, token, "POST", path, `{"action":"look"}`, 200)

	view2 := []string{"browser_snapshot boxes=true", "browser_take_screenshot scale=css type=png"}
	calls := slices.Concat(
		[]string{"browser_navigate url=https://example.com/"}, view2,
		[]string{"browser_click element=Approve target=e5"}, view2,
		[]string{"browser_type element=Search submit=true target=e4 text=hunter2"}, view2,
		[]string{"browser_press_key key=Enter"}, view2,
		[]string{"browser_navigate_back"}, view2,
		view2,
	)
	if seen := browser.seen(); !slices.Equal(seen, calls) {
		t.Fatalf("the browser was sent\n%q, want\n%q", seen, calls)
	}

	// Every action that can change a page is audited, and a look is not.
	// What was typed is not kept.
	log := want(t, f, token, "GET", "/api/audit?action=session.browser.type", "", 200)
	raw, _ := json.Marshal(log)
	if !strings.Contains(string(raw), `"characters":"7"`) || strings.Contains(string(raw), "hunter2") {
		t.Fatalf("the audit of typing = %s", raw)
	}
	all, _ := json.Marshal(want(t, f, token, "GET", "/api/audit", "", 200))
	for _, action := range []string{"navigate", "click", "press", "back"} {
		if !strings.Contains(string(all), "session.browser."+action) {
			t.Errorf("%s was not audited", action)
		}
	}
	if strings.Contains(string(all), "session.browser.look") {
		t.Error("a look was audited as a change")
	}
}

func TestABrowserActionIsRefusedBeforeItReachesTheBrowser(t *testing.T) {
	f, browser, token := browserFixture(t)
	path := sessionWith(t, f, token, "surfer")
	browser.seen()
	for _, tc := range []struct {
		why, body string
		code      int
	}{
		{"a file address", `{"action":"navigate","url":"file:///etc/passwd"}`, 400},
		{"a script address", `{"action":"navigate","url":"javascript:alert(1)"}`, 400},
		{"no host", `{"action":"navigate","url":"https://"}`, 400},
		{"a selector for a target", `{"action":"click","target":"body > a"}`, 400},
		{"nothing to type", `{"action":"type","target":"e4","text":""}`, 400},
		{"a key that is a sentence", `{"action":"press","key":"rm -rf"}`, 400},
		{"an action that does not exist", `{"action":"evaluate"}`, 400},
		{"a field that does not exist", `{"action":"look","script":"1"}`, 400},
		{"a browser the agent does not hold", `{"action":"look","provider":"mine"}`, 404},
		{"a browser that does not exist", `{"action":"look","provider":"nope"}`, 404},
	} {
		if code, out := status(t, f, token, "POST", path, tc.body); code != tc.code {
			t.Errorf("%s = %d %v, want %d", tc.why, code, out, tc.code)
		}
	}

	// A session whose agent holds no browser has none, whatever is asked.
	want(t, f, token, "POST", sessionWith(t, f, token, "helper"), `{"action":"look"}`, 404)

	// A grant on a provider reached with each person's own credential is
	// not a browser a person acts on here.
	want(t, f, token, "POST", "/api/agents", `{"name":"private","model":"offline","provider":"offline","system_prompt":"Browse.","tools":["mcp:mine:write"]}`, 201)
	want(t, f, token, "POST", sessionWith(t, f, token, "private"), `{"action":"look"}`, 404)

	// Another workspace's session is not there.
	globex := f.person(t, "globex", "gil", "Gil", "humagen-admins")
	want(t, f, globex, "POST", path, `{"action":"look"}`, 404)

	// One who only reads runs does not act.
	reader := f.person(t, "acme", "rae", "Rae", "humagen-viewers")
	if code, _ := status(t, f, reader, "POST", path, `{"action":"look"}`); code != 403 && code != 404 {
		t.Errorf("a reader acted on the browser: %d", code)
	}

	// The action policy holds a person's hand as it holds the agent's.
	for _, level := range []string{"ask", "never"} {
		policy := want(t, f, token, "GET", "/api/agents/surfer/action-policy", "", 200)
		version := 0
		if p, ok := policy["policy"].(map[string]any); ok {
			if v, ok := p["version"].(float64); ok {
				version = int(v)
			}
		} else if v, ok := policy["version"].(float64); ok {
			version = int(v)
		}
		want(t, f, token, "PUT", "/api/agents/surfer/action-policy", fmt.Sprintf(`{"rules":{"other":%q},"version":%d}`, level, version), 200)
		want(t, f, token, "POST", path, `{"action":"look"}`, 403)
	}

	if seen := browser.seen(); len(seen) != 0 {
		t.Fatalf("a refused action reached the browser: %q", seen)
	}
}
