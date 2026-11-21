package mcptools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/humagen/pkg/spec"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestBrowserExamplePinsReviewedTools(t *testing.T) {
	cfg, err := spec.Load("../../examples/browser/humagen.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Platforms) != 1 || len(cfg.Platforms[0].Spec.Tools) != 1 {
		t.Fatal("expected one browser tool server")
	}
	mcp, err := spec.Block[spec.MCP](cfg.Platforms[0].Spec.Tools[0].Slot)
	if err != nil {
		t.Fatal(err)
	}
	accepted := browserAccepted(t)
	if len(accepted) == 0 || len(accepted) != len(mcp.Accept) {
		t.Fatal("example and reviewed tool inventory differ")
	}
	for _, pin := range mcp.Accept {
		found := false
		for _, tool := range accepted {
			if tool.Name == pin.Name && tool.SchemaHash == pin.SchemaHash && tool.Class == toolprovider.ClassWrite && pin.Class == "write" {
				found = true
			}
		}
		if !found {
			t.Fatalf("configuration has an unreviewed browser tool: %s", pin.Name)
		}
	}
}

func browserAccepted(t *testing.T) []toolprovider.Tool {
	t.Helper()
	data, err := os.ReadFile("../../examples/browser/accepted.json")
	if err != nil {
		t.Fatal(err)
	}
	var accepted []toolprovider.Tool
	if err := json.Unmarshal(data, &accepted); err != nil {
		t.Fatal(err)
	}
	return accepted
}

// The browser image (deploy/browser) runs the Playwright MCP release whose
// tool schemas examples/browser accepts by hash. The launcher and the image
// name the version apart, so this holds them to one.
func TestBrowserImagePinsTheReviewedVersion(t *testing.T) {
	read := func(path string) string {
		t.Helper()
		data, err := os.ReadFile(filepath.Join("..", "..", path))
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	launcher := regexp.MustCompile(`@playwright/mcp@([0-9][0-9A-Za-z.-]*)`).FindStringSubmatch(read("examples/browser/start.sh"))
	image := regexp.MustCompile(`(?m)^ARG PLAYWRIGHT_MCP_VERSION=([0-9][0-9A-Za-z.-]*)$`).FindStringSubmatch(read("deploy/browser/Dockerfile"))
	if len(launcher) != 2 || len(image) != 2 {
		t.Fatalf("version not found: launcher=%v image=%v", launcher, image)
	}
	if launcher[1] != image[1] {
		t.Fatalf("deploy/browser/Dockerfile runs Playwright MCP %s, examples/browser/start.sh %s: the accepted tool hashes are for one version", image[1], launcher[1])
	}
	// What the script runs, without its comments.
	var entry string
	for _, line := range strings.Split(read("deploy/browser/entrypoint.sh"), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "#") {
			entry += line + "\n"
		}
	}
	// The image serves images to the model as the launcher does, and one
	// browser to every client (TestBrowserLiveIsOneBrowserForEveryClient).
	// With --isolated, this version gives each client a browser of its own.
	if strings.Contains(entry, "--isolated") {
		t.Fatal("deploy/browser/entrypoint.sh passes --isolated: each Humagen process would then have a browser of its own")
	}
	for _, flag := range []string{"--headless", "--shared-browser-context", "--idle-timeout 0", "--no-webmcp", "--image-responses allow", "anchor.mjs"} {
		if !strings.Contains(entry, flag) {
			t.Fatalf("deploy/browser/entrypoint.sh does not pass %s", flag)
		}
	}
}

// The Helm chart's browser.enabled accepts the same tools, by the same
// hashes, as examples/browser: the chart carries its own list because it is
// installed without a checkout.
func TestBrowserChartAcceptsTheReviewedTools(t *testing.T) {
	data, err := os.ReadFile("../../deploy/helm/humagen/files/browser-tools.yaml")
	if err != nil {
		t.Fatal(err)
	}
	type tool struct{ name, hash, class string }
	var chart []tool
	for _, m := range regexp.MustCompile(`(?m)^- \{name: (\S+), schema_hash: ([0-9a-f]{64}), class: (\w+)\}$`).FindAllStringSubmatch(string(data), -1) {
		chart = append(chart, tool{m[1], m[2], m[3]})
	}
	// Every line that is not a comment is a tool in that form: an entry
	// written another way would be rendered and not read here.
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if !strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "- {name: ") {
			t.Fatalf("a line of the chart's file is neither a comment nor a tool in the reviewed form: %q", line)
		}
	}
	var reviewed []tool
	for _, a := range browserAccepted(t) {
		reviewed = append(reviewed, tool{a.Name, a.SchemaHash, string(a.Class)})
	}
	if len(chart) == 0 || !slices.Equal(chart, reviewed) {
		t.Fatalf("deploy/helm/humagen/files/browser-tools.yaml accepts %v, examples/browser/accepted.json %v", chart, reviewed)
	}
	// And every line of the chart's file that names a tool was read.
	if n := len(regexp.MustCompile(`(?m)^- `).FindAllString(string(data), -1)); n != len(chart) {
		t.Fatalf("%d tool lines in the chart's file, %d read", n, len(chart))
	}
}

// TestBrowserLive exercises the actual pinned Playwright MCP server, without
// reading a person's browser profile or requiring a paid model. Start the
// server as docs/guides/browser.md describes and set HUMAGEN_BROWSER_MCP_URL.
func TestBrowserLive(t *testing.T) {
	endpoint := os.Getenv("HUMAGEN_BROWSER_MCP_URL")
	if endpoint == "" {
		t.Skip("set HUMAGEN_BROWSER_MCP_URL to a running Playwright MCP server")
	}
	outputDir := os.Getenv("HUMAGEN_BROWSER_OUTPUT_DIR")
	if outputDir == "" {
		t.Fatal("set HUMAGEN_BROWSER_OUTPUT_DIR to the local browser worker's output directory")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	downloadName := fmt.Sprintf("humagen-browser-record-%d.csv", time.Now().UnixNano())
	downloadPath := filepath.Join(outputDir, downloadName)
	t.Cleanup(func() { _ = os.Remove(downloadPath) })
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/record.csv" {
			w.Header().Set("Content-Type", "text/csv")
			w.Header().Set("Content-Disposition", `attachment; filename="`+downloadName+`"`)
			_, _ = w.Write([]byte("record,value\nfixture,329\n"))
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<!doctype html><title>Browser acceptance</title>
<style>html{color-scheme:light dark}body{background:#208040;color:white;font:32px sans-serif;padding:40px}button{font:inherit;min-height:44px;padding:8px 12px}a{display:block;margin-top:20px;color:white}.dark{display:none}@media(prefers-color-scheme:dark){.light{display:none}.dark{display:inline}}</style>
<p><span class="light">Light appearance</span><span class="dark">Dark appearance</span></p>
<h1>Browser evidence</h1><button onclick="document.querySelector('h1').textContent='Fixture accepted'">Approve fixture</button>
<a href="/record.csv" download>Download selected record</a>`))
	}))
	defer page.Close()
	provider := toolprovider.Provider{Tenant: "browser-test", Name: "browser", Transport: toolprovider.TransportHTTP, Endpoint: endpoint}
	session, err := mcptools.Connect(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	offered, err := session.Offered(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// The checked-in configuration, not tools discovered during this test,
	// decides what the agent may call. Schema drift fails this acceptance.
	accepted := browserAccepted(t)
	set, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: provider.Tenant, Provider: provider.Name, Tools: accepted}, toolprovider.GrantWrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.All()) != len(accepted) {
		t.Fatalf("browser tools changed: accepted=%d callable=%d offered=%d diff=%+v", len(accepted), len(set.All()), len(offered), set.Diff())
	}
	call := func(name string, args map[string]any) string {
		t.Helper()
		tool, err := set.Lookup("browser__" + name)
		if err != nil {
			t.Fatal(err)
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	// A worker of one client is closed at the end. The browser image's one
	// browser is every client's and refuses, which is not this test's to
	// fail on (TestBrowserLiveIsOneBrowserForEveryClient checks it).
	defer func() {
		tool, err := set.Lookup("browser__browser_close")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tool.Execute(ctx, nil); err != nil && !strings.Contains(err.Error(), "shared between clients") {
			t.Fatal(err)
		}
	}()
	call("browser_navigate", map[string]any{"url": browserFixtureURL(t, page.URL)})
	if size := os.Getenv("HUMAGEN_BROWSER_VIEWPORT"); size != "" {
		var width, height int
		if _, err := fmt.Sscanf(size, "%dx%d", &width, &height); err != nil || fmt.Sprintf("%dx%d", width, height) != size || width < 320 || width > 4096 || height < 320 || height > 4096 {
			t.Fatal("HUMAGEN_BROWSER_VIEWPORT must be WIDTHxHEIGHT, each from 320 to 4096")
		}
		call("browser_resize", map[string]any{"width": width, "height": height})
	}
	shot, err := set.Lookup("browser__browser_take_screenshot")
	if err != nil {
		t.Fatal(err)
	}
	capture := func(state string) {
		t.Helper()
		data := browserPixels(t, ctx, shot)
		if out := os.Getenv("HUMAGEN_BROWSER_CAPTURE_DIR"); out != "" {
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, state+".png"), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Playwright 0.0.85 saves automatic snapshots as server-side files.
	// An explicit snapshot without a filename returns the accessible tree.
	snapshot := call("browser_snapshot", nil)
	if !strings.Contains(snapshot, "Browser evidence") {
		t.Fatalf("did not read the real page: %s", snapshot)
	}
	if theme := os.Getenv("HUMAGEN_BROWSER_EXPECT_THEME"); theme != "" {
		label := map[string]string{"light": "Light appearance", "dark": "Dark appearance"}[theme]
		if label == "" || !strings.Contains(snapshot, label) {
			t.Fatalf("worker appearance does not match HUMAGEN_BROWSER_EXPECT_THEME=%q", theme)
		}
	}
	capture("before")
	ref := regexp.MustCompile(`button "Approve fixture" \[ref=([^\]]+)\]`).FindStringSubmatch(snapshot)
	if len(ref) != 2 {
		t.Fatalf("button reference not found in %s", snapshot)
	}
	call("browser_click", map[string]any{"element": "Approve fixture button", "target": ref[1]})
	clicked := call("browser_snapshot", nil)
	if !strings.Contains(clicked, "Fixture accepted") {
		t.Fatalf("click did not update the actual page: %s", clicked)
	}
	link := regexp.MustCompile(`link "Download selected record" \[ref=([^\]]+)\]`).FindStringSubmatch(clicked)
	if len(link) != 2 {
		t.Fatalf("download reference not found in %s", clicked)
	}
	downloaded := call("browser_click", map[string]any{"element": "Download selected record link", "target": link[1]})
	if !strings.Contains(downloaded, downloadName) {
		t.Fatalf("download action has no result evidence: %s", downloaded)
	}
	data, err := os.ReadFile(downloadPath)
	if err != nil {
		t.Fatalf("cannot read downloaded record in HUMAGEN_BROWSER_OUTPUT_DIR=%q: this must match the running browser worker's output directory: %v", outputDir, err)
	}
	if string(data) != "record,value\nfixture,329\n" {
		t.Fatalf("downloaded record bytes = %q, %v", data, err)
	}
	capture("after")
	t.Log("real navigation, changed page, downloaded CSV bytes, and before/after PNG pixels passed")
}

// TestBrowserLiveIsOneBrowserForEveryClient checks the browser image's
// promise: two clients, as a server pod and a worker pod are, see one page
// and one set of sign-ins. Set HUMAGEN_BROWSER_SHARED=1 beside
// HUMAGEN_BROWSER_MCP_URL for a worker started from deploy/browser. The
// launcher's isolated mode gives each client its own, so this is not run for it.
func TestBrowserLiveIsOneBrowserForEveryClient(t *testing.T) {
	endpoint := os.Getenv("HUMAGEN_BROWSER_MCP_URL")
	if endpoint == "" || os.Getenv("HUMAGEN_BROWSER_SHARED") == "" {
		t.Skip("set HUMAGEN_BROWSER_MCP_URL and HUMAGEN_BROWSER_SHARED=1 for a running browser image")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/sign-in" {
			http.SetCookie(w, &http.Cookie{Name: "signed", Value: "in", Path: "/"})
			_, _ = w.Write([]byte(`<!doctype html><title>Sign in</title><h1>Signed in</h1>`))
			return
		}
		state := "signed out"
		if c, err := r.Cookie("signed"); err == nil && c.Value == "in" {
			state = "still signed in"
		}
		_, _ = w.Write([]byte(`<!doctype html><title>Account</title><h1>Account: ` + state + `</h1>`))
	}))
	defer page.Close()
	accepted := browserAccepted(t)
	client := func() func(string, map[string]any) string {
		provider := toolprovider.Provider{Tenant: "browser-test", Name: "browser", Transport: toolprovider.TransportHTTP, Endpoint: endpoint}
		session, err := mcptools.Connect(ctx, provider)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		set, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: provider.Tenant, Provider: provider.Name, Tools: accepted}, toolprovider.GrantWrite)
		if err != nil {
			t.Fatal(err)
		}
		return func(name string, args map[string]any) string {
			t.Helper()
			tool, err := set.Lookup("browser__" + name)
			if err != nil {
				t.Fatal(err)
			}
			out, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	server, worker := client(), client()
	base := browserFixtureURL(t, page.URL)
	// A person signs in from the console's tab, on the server's connection.
	server("browser_navigate", map[string]any{"url": base + "/sign-in"})
	// A run on a worker's connection then opens a page that needs the sign-in.
	worker("browser_navigate", map[string]any{"url": base + "/account"})
	if seen := worker("browser_snapshot", nil); !strings.Contains(seen, "still signed in") {
		t.Fatalf("the second client did not have the first client's sign-in: %s", seen)
	}
	// And the console's tab shows the page the run left the browser on.
	if seen := server("browser_snapshot", nil); !strings.Contains(seen, "Account: still signed in") {
		t.Fatalf("the first client does not see the page the second opened: %s", seen)
	}
	// No client may close the browser the others are using.
	tool, err := sharedTool(t, ctx, endpoint, accepted, "browser_close")
	if err != nil {
		t.Fatal(err)
	}
	if out, err := tool.Execute(ctx, nil); err == nil || !strings.Contains(err.Error(), "shared between clients") {
		t.Fatalf("closing the shared browser was not refused: %q, %v", out, err)
	}
	if seen := worker("browser_snapshot", nil); !strings.Contains(seen, "Account: still signed in") {
		t.Fatalf("the page did not survive a refused close: %s", seen)
	}
}

// TestBrowserLiveKeepsItsPageWhenEveryClientLeaves checks that the browser
// image's browser outlives its clients. The server closes the browser when
// the last client disconnects, which a restart of every Humagen process is;
// the image keeps a client of its own connected (deploy/browser/anchor.mjs).
// So what a person signed in to, and the page they left, are still there
// for whoever connects next.
func TestBrowserLiveKeepsItsPageWhenEveryClientLeaves(t *testing.T) {
	endpoint := os.Getenv("HUMAGEN_BROWSER_MCP_URL")
	if endpoint == "" || os.Getenv("HUMAGEN_BROWSER_SHARED") == "" {
		t.Skip("set HUMAGEN_BROWSER_MCP_URL and HUMAGEN_BROWSER_SHARED=1 for a running browser image")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/sign-in" {
			// No expiry: a session cookie, which a closed browser forgets.
			http.SetCookie(w, &http.Cookie{Name: "kept", Value: "yes", Path: "/"})
			_, _ = w.Write([]byte(`<!doctype html><title>Sign in</title><h1>Signed in before everyone left</h1>`))
			return
		}
		state := "forgotten"
		if c, err := r.Cookie("kept"); err == nil && c.Value == "yes" {
			state = "kept"
		}
		_, _ = w.Write([]byte(`<!doctype html><title>Account</title><h1>Session ` + state + `</h1>`))
	}))
	defer page.Close()
	accepted := browserAccepted(t)
	provider := toolprovider.Provider{Tenant: "browser-test", Name: "browser", Transport: toolprovider.TransportHTTP, Endpoint: endpoint}
	connect := func() (*mcptools.Session, func(string, map[string]any) string) {
		t.Helper()
		session, err := mcptools.Connect(ctx, provider)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = session.Close() })
		set, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: provider.Tenant, Provider: provider.Name, Tools: accepted}, toolprovider.GrantWrite)
		if err != nil {
			t.Fatal(err)
		}
		return session, func(name string, args map[string]any) string {
			t.Helper()
			tool, err := set.Lookup("browser__" + name)
			if err != nil {
				t.Fatal(err)
			}
			out, err := tool.Execute(ctx, args)
			if err != nil {
				t.Fatal(err)
			}
			return out
		}
	}
	base := browserFixtureURL(t, page.URL)
	first, call := connect()
	call("browser_navigate", map[string]any{"url": base + "/sign-in"})
	// Every Humagen client leaves: this test is the only one.
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	_, later := connect()
	if seen := later("browser_snapshot", nil); !strings.Contains(seen, "Signed in before everyone left") {
		t.Fatalf("the page did not outlive its clients: %s", seen)
	}
	later("browser_navigate", map[string]any{"url": base + "/account"})
	if seen := later("browser_snapshot", nil); !strings.Contains(seen, "Session kept") {
		t.Fatalf("the session did not outlive its clients: %s", seen)
	}
}

// sharedTool is one accepted browser tool on a connection of its own.
func sharedTool(t *testing.T, ctx context.Context, endpoint string, accepted []toolprovider.Tool, name string) (saigetypes.Tool, error) {
	t.Helper()
	provider := toolprovider.Provider{Tenant: "browser-test", Name: "browser", Transport: toolprovider.TransportHTTP, Endpoint: endpoint}
	session, err := mcptools.Connect(ctx, provider)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = session.Close() })
	set, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: provider.Tenant, Provider: provider.Name, Tools: accepted}, toolprovider.GrantWrite)
	if err != nil {
		return nil, err
	}
	return set.Lookup("browser__" + name)
}

// browserFixtureURL is the address the browser worker opens this test's page
// at. A worker on this host uses the listener's own. One in a container
// reaches this host by another name, which HUMAGEN_BROWSER_FIXTURE_HOST
// gives ("host.docker.internal", or "host.lima.internal" under Colima); the
// port stays the listener's.
func browserFixtureURL(t *testing.T, listener string) string {
	t.Helper()
	host := os.Getenv("HUMAGEN_BROWSER_FIXTURE_HOST")
	if host == "" {
		return listener
	}
	u, err := url.Parse(listener)
	if err != nil {
		t.Fatal(err)
	}
	u.Host = net.JoinHostPort(host, u.Port())
	return u.String()
}

func browserPixels(t *testing.T, ctx context.Context, tool saigetypes.Tool) []byte {
	t.Helper()
	rich, ok := tool.(saigetypes.RichTool)
	if !ok {
		t.Fatal("browser screenshot tool does not preserve image content")
	}
	result, err := rich.ExecuteRich(ctx, map[string]any{"type": "png", "scale": "css"})
	if err != nil {
		t.Fatal(err)
	}
	for _, part := range result.Parts {
		if part.Kind() != saigetypes.KindImage {
			continue
		}
		source, ok := saigetypes.SourceOf(part)
		if !ok {
			t.Fatal("screenshot has no byte source")
		}
		pic, err := png.Decode(bytes.NewReader(source.Inline))
		if err != nil {
			t.Fatal(err)
		}
		if pic.Bounds().Dx() < 100 || pic.Bounds().Dy() < 100 {
			t.Fatalf("unexpected screenshot size %v", pic.Bounds())
		}
		r, g, b, _ := pic.At(5, 5).RGBA()
		if r>>8 != 0x20 || g>>8 != 0x80 || b>>8 != 0x40 {
			t.Fatalf("screenshot pixels do not match the rendered fixture: %x %x %x", r, g, b)
		}
		t.Logf("real browser PNG: %dx%d, %d bytes", pic.Bounds().Dx(), pic.Bounds().Dy(), len(source.Inline))
		return source.Inline
	}
	t.Fatal("browser_take_screenshot returned no image bytes")
	return nil
}
