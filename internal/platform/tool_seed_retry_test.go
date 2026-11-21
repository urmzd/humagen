package platform_test

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/mcpfake"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/toolprovider"
)

// A tool server that is not up when Humagen starts, such as a browser pod
// still pulling its image, is asked again until it answers: its declared
// tools are then accepted with no restart of Humagen.
func TestAToolServerThatStartsLateIsReachedWithoutARestart(t *testing.T) {
	platform.QuickToolSeedRetries(t)
	fake := mcpfake.New(t)
	// An address nothing listens on yet.
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := hold.Addr().String()
	if err := hold.Close(); err != nil {
		t.Fatal(err)
	}
	provider := toolprovider.Provider{Tenant: tenant, Name: "late", Transport: toolprovider.TransportHTTP, Endpoint: "http://" + addr + "/mcp"}
	p := startWith(t, platform.Config{Ext: platform.Extensions{
		ToolProviders: []platform.ToolProviderSeed{{Provider: provider, Accept: fake.Tools(t)}},
	}})
	ctx := t.Context()
	accepted := func() []string {
		t.Helper()
		snap, err := p.ToolProviders.Snapshot(ctx, tenant, "late", 0)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, tool := range snap.Tools {
			names = append(names, tool.Name)
		}
		return names
	}
	if got := accepted(); len(got) != 0 {
		t.Fatalf("tools accepted from a server nothing was listening as: %v", got)
	}
	// Still nothing after several retries against the closed port.
	time.Sleep(150 * time.Millisecond)
	if got := accepted(); len(got) != 0 {
		t.Fatalf("tools accepted before the server was up: %v", got)
	}

	// The server comes up at the address the provider names.
	target, err := url.Parse(strings.TrimSuffix(fake.URL, "/mcp"))
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Skipf("the address was taken before the test could listen on it: %v", err)
	}
	late := &http.Server{Handler: httputil.NewSingleHostReverseProxy(target), ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = late.Serve(listener) }()
	t.Cleanup(func() { _ = late.Close() })

	deadline := time.Now().Add(10 * time.Second)
	for {
		if got := accepted(); len(got) == 1 && got[0] == mcpfake.ToolName {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("the server came up and its declared tool was never accepted: %v", accepted())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// A provider that is reachable at start is not asked again, and a platform
// that closes while one is still unreachable stops asking.
func TestAnUnreachableToolServerDoesNotHoldUpAClose(t *testing.T) {
	platform.QuickToolSeedRetries(t)
	fake := mcpfake.New(t)
	hold, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := hold.Addr().String()
	_ = hold.Close()
	provider := toolprovider.Provider{Tenant: tenant, Name: "never", Transport: toolprovider.TransportHTTP, Endpoint: "http://" + addr + "/mcp"}
	p := startWith(t, platform.Config{Ext: platform.Extensions{
		ToolProviders: []platform.ToolProviderSeed{{Provider: provider, Accept: fake.Tools(t)}},
	}})
	time.Sleep(100 * time.Millisecond)
	closed := make(chan error, 1)
	go func() { closed <- p.Close() }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Close waited on a tool server that never answered")
	}
}
