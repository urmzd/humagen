package runctx

import (
	"errors"
	"testing"

	"github.com/urmzd/humagen/pkg/personctx"
	"github.com/urmzd/humagen/pkg/settings"
)

func sample() Context {
	ws := settings.Layer{Ceilings: settings.Ceilings{ToolServers: []string{"mcp.example.com", "x.test"}}}
	own := personctx.Context{Memory: "vol-1", ToolServers: []personctx.ToolServer{
		{Name: "notes", URL: "https://mcp.example.com/notes", Credential: "env:HUMAGEN_PERSON_ADA_NOTES"},
		{Name: "elsewhere", URL: "https://other.example.com"},
		{Name: "x", URL: "https://x.test"},
	}}
	return New("ada", settings.Resolve(ws, own.Run), own)
}

func TestNewKeepsOnlyServersTheCeilingsAllow(t *testing.T) {
	c := sample()
	if c.Person != "ada" || c.Memory != "vol-1" || len(c.ToolServers) != 2 || c.ToolServers[0].Name != "notes" || c.ToolServers[1].Name != "x" {
		t.Fatalf("context = %+v", c)
	}
	nobody := New("", c.Settings, personctx.Context{Memory: "vol-1", ToolServers: c.ToolServers})
	if nobody.Memory != "" || len(nobody.ToolServers) != 0 {
		t.Fatalf("a run for nobody got %+v", nobody)
	}
}

func TestPinRoundTripsAndRefusesTampering(t *testing.T) {
	c := sample()
	meta := map[string]string{}
	if _, ok, err := Of(meta); ok || err != nil {
		t.Fatalf("nothing pinned = %v, %v", ok, err)
	}
	if err := Pin(meta, c); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Of(meta)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	d1, _ := got.Digest()
	d2, _ := c.Digest()
	if d1 != d2 || d1 != meta[MetaDigest] {
		t.Fatalf("digest %s != %s", d1, d2)
	}
	meta[MetaContext] = `{"person":"eve","settings":{},"tool_servers":[]}`
	if _, _, err := Of(meta); !errors.Is(err, ErrInvalid) {
		t.Fatalf("tampered = %v", err)
	}
	delete(meta, MetaDigest)
	if _, _, err := Of(meta); !errors.Is(err, ErrInvalid) {
		t.Fatalf("no digest = %v", err)
	}
	Unpin(meta)
	if _, ok, _ := Of(meta); ok {
		t.Fatal("unpinned context still read")
	}
}

func TestACalledRunOnlyNarrows(t *testing.T) {
	parent := sample()
	zero := 0
	child := parent.Narrow(settings.Ceilings{ToolServers: []string{"x.test", "wider.test"}, CallDepth: &zero, Tools: []string{"file_read"}})
	if child.Person != parent.Person || child.Memory != parent.Memory {
		t.Fatalf("child = %+v", child)
	}
	if len(child.ToolServers) != 1 || child.ToolServers[0].Name != "x" || child.Settings.Ceilings.AllowsToolServer("wider.test") {
		t.Fatalf("child servers = %+v, ceilings %+v", child.ToolServers, child.Settings.Ceilings)
	}
	if child.Settings.Ceilings.Depth(5) != 0 || !child.Settings.Ceilings.AllowsTool("file_read") || child.Settings.Ceilings.AllowsTool("file_write") {
		t.Fatalf("child ceilings = %+v", child.Settings.Ceilings)
	}
	// Narrowing by nothing keeps the caller's context as it is.
	same := parent.Narrow(settings.Ceilings{})
	d1, _ := same.Digest()
	d2, _ := parent.Digest()
	if d1 != d2 {
		t.Fatalf("narrowing by nothing changed %+v to %+v", parent, same)
	}
}
