package call

import "testing"

// Call edges are stored in tags and the ledger under these keys.
func TestCallEdgeKeysAreKept(t *testing.T) {
	for got, want := range map[string]string{
		metaPrefix: "warp.", metaRoot: "warp.root", metaParent: "warp.parent",
		metaDepth: "warp.depth", metaMode: "warp.mode", metaLease: "warp.lease",
	} {
		if got != want {
			t.Errorf("kept key is %q, want %q", got, want)
		}
	}
}
