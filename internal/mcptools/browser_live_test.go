package mcptools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
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
<style>body{background:#208040;color:white;font:32px sans-serif;padding:40px}</style>
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
	defer func() { call("browser_close", nil) }()
	call("browser_navigate", map[string]any{"url": page.URL})
	// Playwright 0.0.85 saves automatic snapshots as server-side files.
	// An explicit snapshot without a filename returns the accessible tree.
	snapshot := call("browser_snapshot", nil)
	if !strings.Contains(snapshot, "Browser evidence") {
		t.Fatalf("did not read the real page: %s", snapshot)
	}
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
	tool, err := set.Lookup("browser__browser_take_screenshot")
	if err != nil {
		t.Fatal(err)
	}
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
		t.Logf("real browser navigation, ref click, and %dx%d PNG pixels passed (%d bytes)", pic.Bounds().Dx(), pic.Bounds().Dy(), len(source.Inline))
		return
	}
	t.Fatal("browser_take_screenshot returned no image bytes")
}
