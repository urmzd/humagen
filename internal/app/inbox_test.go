package app

import (
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/urmzd/warp/internal/platform"
)

// A start hands the platform each workspace's inbox retention as its
// Platform document sets it, and nothing for a workspace that leaves it out,
// which keeps the default.
func TestAStartKeepsEachInboxForItsRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "warp.yaml")
	body := "api_version: warp/v1\nkind: System\nspec:\n  auth: {kind: dev, tenant: {claim: tenant}}\n---\n" +
		"api_version: warp/v1\nkind: Platform\nmetadata: {name: default}\nspec:\n  workspace: {inbox_retention: 30d}\n---\n" +
		"api_version: warp/v1\nkind: Platform\nmetadata: {name: globex}\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	var got map[string]time.Duration
	o := Options{Platform: func(cfg *platform.Config) error {
		got = maps.Clone(cfg.InboxRetention)
		return nil
	}}
	_, stop := serveUntil(t, o, "-f", path, "-addr", "127.0.0.1:0")
	stop()
	if want := map[string]time.Duration{"default": 30 * 24 * time.Hour}; !maps.Equal(got, want) {
		t.Fatalf("retention = %v, want %v", got, want)
	}
}
