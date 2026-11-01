package platform_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// twoReplicas runs two platforms on one Postgres, each with its own pool, as
// two replicas of one deployment.
func twoReplicas(t *testing.T) (a, b *platform.Platform) {
	t.Helper()
	var ps [2]*platform.Platform
	f := &fakes{}
	for i, pool := range statetest.Pools(t, 2) {
		backends, release, err := platform.OpenPostgres(t.Context(), pool)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = release() })
		if i == 0 {
			if _, err := backends.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "lead", Model: "offline", Provider: "offline", SystemPrompt: "Lead."}, "test"); err != nil {
				t.Fatal(err)
			}
		}
		cfg := f.config()
		cfg.Backends = backends
		ps[i], _ = runningWith(t, cfg)
	}
	return ps[0], ps[1]
}

// A client created, changed and deleted through one replica is seated,
// reseated and unseated on the other: a webhook sent to the other replica
// right after the change is answered as the change says.
func TestAClientChangedOnOneReplicaIsSeatedOnTheOther(t *testing.T) {
	a, b := twoReplicas(t)
	const app, team = "A0CROSS", "T0CROSS"
	body := mentionOf(app, team, "Ev1", "C0CROSS", "1700000000.000100")

	if got := signedSlack(b.Webhook("slack"), "secret-cross", body); got != http.StatusNotFound {
		t.Fatalf("before the client exists: %d, want 404", got)
	}
	// An unclaimed webhook reads the change count at most every 250ms.
	time.Sleep(300 * time.Millisecond)
	create(t, a, tenant, slackClient("cross", app, team, "lead"))
	// At once: the webhook no seat claims makes the other replica look.
	if got := signedSlack(b.Webhook("slack"), "secret-cross", body); got != http.StatusOK {
		t.Fatalf("right after the create, on the other replica: %d, want 200", got)
	}

	// A new signing secret, through the first replica: the other replica
	// seats the new version within its sync interval, with no request
	// asking it to.
	in := slackClient("cross", app, team, "lead")
	in.SigningSecret = clients.SecretInput{Value: "secret-rotated"}
	in.Token = clients.SecretInput{}
	if _, err := a.UpdateClient(t.Context(), tenant, "cross", "test", in); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the other replica to verify with the new secret", func() bool {
		return signedSlack(b.Webhook("slack"), "secret-rotated", mentionOf(app, team, "Ev2", "C0CROSS", "1700000000.000200")) == http.StatusOK
	})
	if got := signedSlack(b.Webhook("slack"), "secret-cross", mentionOf(app, team, "Ev3", "C0CROSS", "1700000000.000300")); got != http.StatusUnauthorized {
		t.Fatalf("the old secret on the other replica: %d, want 401", got)
	}

	if err := a.DeleteClient(t.Context(), tenant, "cross", "test"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the other replica to unseat the deleted client", func() bool {
		return signedSlack(b.Webhook("slack"), "secret-rotated", mentionOf(app, team, "Ev4", "C0CROSS", "1700000000.000400")) == http.StatusNotFound
	})
}
