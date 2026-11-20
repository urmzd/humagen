package mcptools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/humagen/pkg/spec"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func TestDesktopExamplePinsReviewedTools(t *testing.T) {
	cfg, err := spec.Load("../../examples/desktop/humagen.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Platforms) != 1 || len(cfg.Platforms[0].Spec.Tools) != 1 {
		t.Fatal("expected one native desktop tool server")
	}
	mcp, err := spec.Block[spec.MCP](cfg.Platforms[0].Spec.Tools[0].Slot)
	if err != nil {
		t.Fatal(err)
	}
	accepted := desktopAccepted(t)
	if len(accepted) == 0 || len(mcp.Accept) != len(accepted) {
		t.Fatal("native configuration and reviewed inventory differ")
	}
	for _, pin := range mcp.Accept {
		found := false
		for _, tool := range accepted {
			if pin.Name == tool.Name && pin.SchemaHash == tool.SchemaHash && pin.Class == "write" && tool.Class == toolprovider.ClassWrite {
				found = true
			}
		}
		if !found {
			t.Fatalf("unreviewed native tool: %s", pin.Name)
		}
	}
	if mcp.Transport != "stdio" || mcp.Endpoint != "npx" {
		t.Fatal("native desktop tools must run on the explicitly configured Mac worker")
	}
}

func desktopAccepted(t *testing.T) []toolprovider.Tool {
	t.Helper()
	data, err := os.ReadFile("../../examples/desktop/accepted.json")
	if err != nil {
		t.Fatal(err)
	}
	var accepted []toolprovider.Tool
	if err := json.Unmarshal(data, &accepted); err != nil {
		t.Fatal(err)
	}
	return accepted
}

// TestDesktopLive targets only a native app created by this test. No tool may
// fall back to the frontmost app or the whole desktop. The server is the real
// pinned Peekaboo binary with foreground authority for this exact fixture.
func TestDesktopLive(t *testing.T) {
	command := os.Getenv("HUMAGEN_DESKTOP_MCP_COMMAND")
	if command == "" {
		t.Skip("set HUMAGEN_DESKTOP_MCP_COMMAND to the Peekaboo 4.9.9 binary on macOS")
	}
	if runtime.GOOS != "darwin" {
		t.Fatal("the native desktop acceptance requires macOS")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Minute)
	defer cancel()
	provider := toolprovider.Provider{Tenant: "desktop-test", Name: "desktop", Transport: toolprovider.TransportStdio,
		Endpoint: command, Args: []string{"mcp", "serve", "--no-remote", "--allow-foreground"}}
	session, err := mcptools.Connect(ctx, provider)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = session.Close() }()
	accepted := desktopAccepted(t)
	set, err := session.Toolset(ctx, toolprovider.Snapshot{Tenant: provider.Tenant, Provider: provider.Name, Tools: accepted}, toolprovider.GrantWrite)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.All()) != len(accepted) {
		t.Fatalf("desktop tool inventory drifted: %+v", set.Diff())
	}
	call := func(name string, args map[string]any) saigetypes.ToolResult {
		t.Helper()
		tool, err := set.Lookup("desktop__" + name)
		if err != nil {
			t.Fatal(err)
		}
		rich, ok := tool.(saigetypes.RichTool)
		if !ok {
			t.Fatal("desktop tool does not preserve rich content")
		}
		result, err := rich.ExecuteRich(ctx, args)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	permissions := call("permissions", nil)
	t.Logf("permission result: %s", desktopText(permissions))
	for _, name := range []string{"Screen Recording (Required): [ok] Granted", "Accessibility (Required): [ok] Granted"} {
		if !strings.Contains(desktopText(permissions), name) {
			t.Fatal("desktop permission missing; grant it on the active Mac session before rerunning this acceptance")
		}
	}
	window, pid, receipt := desktopFixture(t, ctx)
	observe := func() saigetypes.ToolResult {
		return call("see", map[string]any{"app_target": fmt.Sprintf("PID:%d", pid), "window_id": window, "fresh": true})
	}
	result := observe()
	snapshot, input := desktopElement(t, desktopText(result), "fixture-input")
	call("type", map[string]any{"snapshot": snapshot, "on": input, "clear": true, "text": "after-native-acceptance"})
	// Each mutation invalidates its observation. Re-observe the exact window
	// rather than applying an ID from the earlier screenshot to a later state.
	result = observe()
	snapshot, button := desktopElement(t, desktopText(result), "fixture-change")
	call("action", map[string]any{"snapshot": snapshot, "on": button, "action": "AXPress"})
	data, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatalf("the native button did not produce its action receipt: %v", err)
	}
	var changed struct{ Value, Status string }
	if err := json.Unmarshal(data, &changed); err != nil || changed.Value != "after-native-acceptance" || changed.Status != "After acceptance" {
		t.Fatalf("native action receipt = %s, %v", data, err)
	}
	result = observe()
	if !strings.Contains(desktopText(result), "After acceptance") {
		t.Fatal("the changed native heading is missing from the final observation")
	}
	for _, part := range result.Parts {
		if part.Kind() != saigetypes.KindImage {
			continue
		}
		source, ok := saigetypes.SourceOf(part)
		if !ok {
			t.Fatal("native capture has no bytes")
		}
		pic, err := png.Decode(bytes.NewReader(source.Inline))
		if err != nil {
			t.Fatal(err)
		}
		if pic.Bounds().Dx() < 600 || pic.Bounds().Dy() < 350 {
			t.Fatalf("unexpected native fixture capture bounds %v", pic.Bounds())
		}
		r, g, b, _ := pic.At(12, pic.Bounds().Dy()-20).RGBA()
		if g <= r || g <= b {
			t.Fatal("native fixture screenshot does not contain its green background")
		}
		if out := os.Getenv("HUMAGEN_DESKTOP_CAPTURE_DIR"); out != "" {
			if err := os.MkdirAll(out, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(out, "desktop-fixture.png"), source.Inline, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		t.Logf("native input, button receipt, changed heading and %dx%d PNG pixels passed (%d bytes)", pic.Bounds().Dx(), pic.Bounds().Dy(), len(source.Inline))
		return
	}
	t.Fatal("native observation returned no image")
}

func desktopText(result saigetypes.ToolResult) string {
	return result.Text()
}

func desktopElement(t *testing.T, observation, identifier string) (snapshot, element string) {
	t.Helper()
	match := regexp.MustCompile(`(?m)^Snapshot ID: (ps1_[0-9a-f]{32})$`).FindStringSubmatch(observation)
	if len(match) != 2 {
		t.Fatal("native observation has no producer-bound snapshot reference")
	}
	for _, line := range strings.Split(observation, "\n") {
		if !strings.Contains(line, "identifier: "+identifier) || strings.Contains(line, "[not actionable]") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "-" {
			return match[1], fields[0]
		}
	}
	t.Fatalf("native fixture has no actionable element with identifier %s", identifier)
	return "", ""
}

func desktopFixture(t *testing.T, ctx context.Context) (window, pid int, receipt string) {
	t.Helper()
	root := t.TempDir()
	app := filepath.Join(root, "HumagenDesktopFixture.app", "Contents")
	bin := filepath.Join(app, "MacOS", "HumagenDesktopFixture")
	if err := os.MkdirAll(filepath.Dir(bin), 0o755); err != nil {
		t.Fatal(err)
	}
	info := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict><key>CFBundleIdentifier</key><string>dev.humagen.desktop-acceptance</string><key>CFBundleName</key><string>HumagenDesktopFixture</string><key>CFBundleExecutable</key><string>HumagenDesktopFixture</string><key>CFBundlePackageType</key><string>APPL</string><key>NSHighResolutionCapable</key><true/></dict></plist>`
	if err := os.WriteFile(filepath.Join(app, "Info.plist"), []byte(info), 0o644); err != nil {
		t.Fatal(err)
	}
	build := exec.CommandContext(ctx, "swiftc", "-framework", "AppKit", "-o", bin, "../../examples/desktop/fixture.swift")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build native fixture: %v: %s", err, output)
	}
	ready := filepath.Join(root, "ready.json")
	receipt = filepath.Join(root, "action.json")
	cmd := exec.CommandContext(ctx, bin, ready, receipt)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(ready)
		if err == nil {
			var value struct {
				Window int `json:"window_id"`
				PID    int `json:"pid"`
			}
			if json.Unmarshal(data, &value) == nil && value.Window > 0 && value.PID == cmd.Process.Pid {
				return value.Window, value.PID, receipt
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("native fixture did not expose its own window")
	return 0, 0, ""
}
