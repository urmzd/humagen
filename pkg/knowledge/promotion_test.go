package knowledge

import (
	"errors"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestPromotionGuardAndProvenanceSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		store := open(t)
		source, err := Open(store)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Create("acme", "accepted", "Accepted results", "ada", now); err != nil {
			t.Fatal(err)
		}
		scope := SourceConfig{Mode: Indexed, Provider: "manual", Owner: "ada", Project: "Launch", StopAfterSeconds: 86400}
		if _, err := source.Configure("acme", "accepted", scope, now); err != nil {
			t.Fatal(err)
		}
		broader := scope
		broader.Project = ""
		if _, err := source.Configure("acme", "accepted", broader, now); err != nil {
			t.Fatal(err)
		}
		if _, err := source.AddForScope("acme", "accepted", "Original v1", "humagen://artifacts/a1?version=1", "Keep Tuesday", "ada", now, &scope); !errors.Is(err, ErrExists) {
			t.Fatalf("scope race accepted: %v", err)
		}
		if _, err := source.Configure("acme", "accepted", scope, now); err != nil {
			t.Fatal(err)
		}
		saved, err := source.AddForScope("acme", "accepted", "Original v1", "humagen://artifacts/a1?version=1", "Keep Tuesday", "ada", now, &scope)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := source.Configure("acme", "accepted", broader, now.Add(time.Second)); !errors.Is(err, ErrInvalid) {
			t.Fatalf("retained audience broadened: %v", err)
		}
		again, err := Open(store)
		if err != nil {
			t.Fatal(err)
		}
		retained, err := again.Get("acme", "accepted")
		if err != nil || retained.Lifecycle.Status != Available || retained.Lifecycle.LastGoodAt == nil || !retained.Lifecycle.LastGoodAt.Equal(now) {
			t.Fatalf("actual retained freshness = %+v,%v", retained.Lifecycle, err)
		}
		documents, err := again.Documents("acme", "accepted")
		if err != nil || len(documents) != 1 || documents[0].URI != saved.URI {
			t.Fatalf("promotion after restart: %+v,%v", documents, err)
		}
	})
}
