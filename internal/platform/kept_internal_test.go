package platform

import "testing"

// Housekeeping runs and places are stored in schedules and tags under these
// keys.
func TestHousekeepingKeysAreKept(t *testing.T) {
	for got, want := range map[string]string{
		metaHook: "warp.hook", metaFor: "warp.for", metaKeepsNotes: "warp.keeps_notes",
		metaPlaceProject: "warp.place.project", metaPlaceThread: "warp.place.thread",
	} {
		if got != want {
			t.Errorf("kept key is %q, want %q", got, want)
		}
	}
}
