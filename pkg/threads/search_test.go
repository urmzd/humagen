package threads_test

import (
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
	"testing"
	"time"
)

func TestSearchMessagesRebuildsAfterRestartAndConfinesTenantAndWindow(t *testing.T) {
	st := state.NewMemory()
	s, err := threads.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	th, _ := start(t, s, "acme", shared("Launch", "general"), msg("ada", "Original agenda"))
	post(t, s, "acme", th.ID, msg("ada", "Later zephyr evidence"), t0.Add(time.Minute))
	start(t, s, "other", shared("Launch", "general"), msg("eve", "zephyr secret"))
	again, err := threads.Open(st)
	if err != nil {
		t.Fatal(err)
	}
	hits := again.Search("acme", "ZEPHYR evidence", t0, 20, nil)
	if len(hits) != 1 || hits[0].ID != "m-2" || hits[0].Thread != th.ID {
		t.Fatal(hits)
	}
	if got := again.Search("acme", "zephyr", t0.Add(time.Hour), 20, nil); len(got) != 0 {
		t.Fatal(got)
	}
	if got := again.Search("unknown", "zephyr", t0, 20, nil); len(got) != 0 {
		t.Fatal(got)
	}
}
