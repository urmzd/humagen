package state

import (
	"strings"
	"testing"
)

// The table and the lock are shared with every release before the rename:
// an older binary on the same schema must find the same rows and exclude
// this one during a rolling upgrade.
func TestTheTableAndLockAreKept(t *testing.T) {
	if lockClass != 0x77617270 {
		t.Errorf("lockClass = %#x", lockClass)
	}
	if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS warp_state (") {
		t.Errorf("schema does not create warp_state:\n%s", schema)
	}
}
