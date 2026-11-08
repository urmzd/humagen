package catalog

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestManagedConnectorAtomicallyReservesDefinitionAndName(t *testing.T) {
	statetest.Processes(t, func(t *testing.T, first, second state.Store) {
		a, _ := Open(first)
		b, _ := Open(second)
		definition := json.RawMessage(`{"kind":"bigquery","project":"cloud","datasets":["sales"]}`)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		for _, store := range []*Store{a, b} {
			wg.Go(func() {
				_, err := store.CreateManaged("t", Connector{Name: "warehouse", System: "bigquery", Owner: "alice", Published: &Mark{By: "forged"}, Entries: 50}, definition)
				errs <- err
			})
		}
		wg.Wait()
		close(errs)
		won, refused := 0, 0
		for err := range errs {
			if err == nil {
				won++
			} else if errors.Is(err, ErrExists) {
				refused++
			} else {
				t.Fatal(err)
			}
		}
		if won != 1 || refused != 1 {
			t.Fatalf("won=%d refused=%d", won, refused)
		}
		reopened, _ := Open(first)
		c, err := reopened.Connector("t", "warehouse")
		if err != nil || c.Published != nil || c.Entries != 0 || c.CrawledAt != nil {
			t.Fatal(c, err)
		}
		raw, err := reopened.ManagedDefinition("t", "warehouse")
		if err != nil || string(raw) != string(definition) {
			t.Fatal(string(raw), err)
		}
		if _, err = reopened.ManagedDefinition("other", "warehouse"); !errors.Is(err, ErrNotFound) {
			t.Fatal(err)
		}
		if _, err = reopened.Declare("t", Connector{Name: "warehouse", System: "bigquery", Owner: "bob"}); !errors.Is(err, ErrExists) {
			t.Fatalf("static takeover: %v", err)
		}
		if err = reopened.Remove("t", "warehouse"); err != nil {
			t.Fatal(err)
		}
		if _, err = reopened.ManagedDefinition("t", "warehouse"); !errors.Is(err, ErrNotFound) {
			t.Fatal("definition survived removal", err)
		}
	})
}
