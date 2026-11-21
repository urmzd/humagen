package runs

import (
	"errors"
	"fmt"
	"testing"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestARunRecordsEachConnectionItNeededOnceWithinBounds(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		x, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		record(t, x, "acme", "r1", "lead", "api", 0, nil)
		github := ConnectionNeed{Provider: "github", Name: "GitHub"}
		for range 2 {
			if err := x.RecordConnectionNeed("acme", "r1", github); err != nil {
				t.Fatal(err)
			}
		}
		if err := x.RecordConnectionNeed("acme", "r1", ConnectionNeed{}); err != nil {
			t.Fatal(err)
		}
		got, _ := x.Get("acme", "r1")
		if len(got.NeedsConnection) != 1 || got.NeedsConnection[0] != github {
			t.Fatalf("needs = %+v", got.NeedsConnection)
		}
		for i := range MaxConnectionNeeds + 2 {
			if err := x.RecordConnectionNeed("acme", "r1", ConnectionNeed{Provider: fmt.Sprintf("p%d", i)}); err != nil {
				t.Fatal(err)
			}
		}
		reopened, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		if again, _ := reopened.Get("acme", "r1"); len(again.NeedsConnection) != MaxConnectionNeeds || again.NeedsConnection[0] != github {
			t.Fatalf("kept = %+v", again.NeedsConnection)
		}
		if err := x.RecordConnectionNeed("other", "r1", github); !errors.Is(err, ErrNotFound) {
			t.Fatalf("another tenant = %v", err)
		}
	})
}
