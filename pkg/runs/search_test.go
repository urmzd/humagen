package runs

import (
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/state"
)

func TestSearchAnswersReplayWithoutPreparedContext(t *testing.T) {
	st := state.NewMemory()
	x, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	tag := wire.NewEvent(wire.Raise{ID: "search", Tenant: "acme", Agent: "lead", Origin: OriginAPI, Text: "confidential prepared context", Context: map[string]string{MetaAsk: "Visible request"}, At: t0})
	if err := x.Record("r-search", tag, 1); err != nil {
		t.Fatal(err)
	}
	x.End("acme", "r-search", Completed, "Zephyr recorded answer", "", t0.Add(time.Minute))
	again, err := Open(st)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.Search("acme", "zephyr answer", t0, 20, nil); len(got) != 1 {
		t.Fatal(got)
	}
	for _, query := range []string{"confidential", "prepared"} {
		if got := again.Search("acme", query, t0, 20, nil); len(got) != 0 {
			t.Fatal(got)
		}
	}
	if got := again.Search("other", "zephyr", t0, 20, nil); len(got) != 0 {
		t.Fatal(got)
	}
}
