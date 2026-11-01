package state

import (
	"strings"
	"testing"
)

// The table and the migration's lock are shared with every release before
// the rename: an older binary on the same schema finds the same rows, and the
// two do not create the table at once.
func TestTheTableAndLockAreKept(t *testing.T) {
	if lockClass != 0x77617270 {
		t.Errorf("lockClass = %#x", lockClass)
	}
	if table != "warp_state" {
		t.Errorf("table = %q", table)
	}
	if !strings.Contains(schema, "CREATE TABLE IF NOT EXISTS warp_state (") {
		t.Errorf("schema does not create warp_state:\n%s", schema)
	}
}
