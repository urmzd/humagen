package runs

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestPrivateContextMarkerWritesThroughWithoutChangingAcceptedTag(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		fault := statetest.NewFaulty(st)
		index, err := Open(fault)
		if err != nil {
			t.Fatal(err)
		}
		tag := wire.NewEvent(wire.Raise{ID: "private-tool", Tenant: "acme", Agent: "helper", Origin: OriginAPI, Text: "work", At: t0})
		id := runtime.RunID(tag.Tenant, tag.ID)
		if err := index.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		fault.Fail(statetest.ErrInjected)
		if err := index.MarkPrivateContext("acme", id); !errors.Is(err, statetest.ErrInjected) {
			t.Fatalf("marker ignored failed persistence: %v", err)
		}
		failed, _ := index.Get("acme", id)
		if failed.UsesPrivateContext() {
			t.Fatal("failed marker became visible")
		}
		fault.Fail(nil)
		if err := index.MarkPrivateContext("acme", id); err != nil {
			t.Fatal(err)
		}
		current, _ := index.Get("acme", id)
		if !current.UsesPrivateContext() || !reflect.DeepEqual(current.Tag(), tag) {
			t.Fatal("marker changed accepted identity or was not applied")
		}
		public, _ := json.Marshal(current)
		if strings.Contains(string(public), "private_context") {
			t.Fatal("internal privacy classification leaked in run JSON")
		}
		if err := index.Flush(); err != nil {
			t.Fatal(err)
		}
		again, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		replay, _ := again.Get("acme", id)
		if !replay.UsesPrivateContext() || !reflect.DeepEqual(replay.Tag(), tag) {
			t.Fatal("private context did not survive replay")
		}
		if err := again.MarkPrivateContext("other", id); err == nil {
			t.Fatal("marker crossed tenant scope")
		}
	})
}
