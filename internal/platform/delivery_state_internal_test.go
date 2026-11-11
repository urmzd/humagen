package platform

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/integrations/github"
	"github.com/urmzd/humagen/pkg/integrations/slack"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/saige/flow/address"
	"github.com/urmzd/saige/flow/event"
)

type deadlinePeer struct {
	Peer
	seen chan context.Context
}

func (p deadlinePeer) Flush(ctx context.Context) error {
	p.seen <- ctx
	<-ctx.Done()
	return ctx.Err()
}

func TestPeriodicConnectorFlushBoundsSurfaceCalls(t *testing.T) {
	peer := deadlinePeer{seen: make(chan context.Context, 1)}
	p := &Platform{log: slog.New(slog.DiscardHandler), surfaces: map[string]*surface{
		"slack": {seats: []*seat{{client: clients.Client{ID: "app"}, peer: peer}}},
	}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- p.flushConnections(ctx) }()
	select {
	case call := <-peer.seen:
		deadline, ok := call.Deadline()
		if !ok || time.Until(deadline) > 5*time.Second {
			t.Error("periodic flush did not bound its surface request to five seconds")
		}
	case <-time.After(3 * time.Second):
		t.Error("periodic flush never ran")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceling the platform did not cancel its flush")
	}
}

// Each newly dialed peer models a client's reseat or a replica taking over
// delivery. Both must edit the same native message with the full answer.
func TestClientDeliverySurvivesReseatingAndReplicaChanges(t *testing.T) {
	for _, backend := range []string{"memory", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			slackAPI, githubAPI := slack.NewFake(), github.NewFake()
			open := func(b Backends) *Platform {
				p, err := New(Config{
					Workspace: t.TempDir(), Backends: b, ClientInterval: -1,
					SlackAPI:  func(string) slack.API { return slackAPI },
					GitHubAPI: func(string) github.API { return githubAPI },
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = p.Close() })
				return p
			}
			var replicas [2]*Platform
			if backend == "memory" {
				replicas[0] = open(Backends{})
				replicas[1] = replicas[0]
			} else {
				for i, pool := range statetest.Pools(t, 2) {
					b, release, err := OpenPostgres(t.Context(), pool)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() { _ = release() })
					replicas[i] = open(b)
				}
			}
			for _, surface := range []clients.Surface{clients.Slack, clients.GitHub} {
				t.Run(string(surface), func(t *testing.T) {
					c := clients.Client{Tenant: "t", ID: "app", Surface: surface,
						Identity: clients.Identity{Team: "T01ABCDEF", Owners: []string{"org"}}}
					to := address.MustParse("slack://T01ABCDEF/C02CHANNEL?thread=1700000000.000100")
					if surface == clients.GitHub {
						to = address.MustParse("github://org/repo/issues/42")
					}
					text := func(seq uint64, tenant, value string) event.Envelope {
						return event.Envelope{Tenant: tenant, Run: "run", Agent: "lead", Channel: to.String(), Seq: seq,
							Type: event.TypeText, Data: event.Text{Text: value}, Time: time.Now()}
					}
					dial := func(p *Platform, c clients.Client) Peer {
						c.Managed, c.Enabled = clients.ManagedStatic, true
						if err := p.Runs.Record("run", wire.NewEvent(wire.Raise{ID: "accepted", Tenant: c.Tenant, Origin: string(surface), Channel: to.String(), Agent: "lead", At: time.Now()}), 1); err != nil {
							t.Fatal(err)
						}
						st, err := p.dial(c, resolved{token: "token", signing: "secret"})
						if err != nil {
							t.Fatal(err)
						}
						return st.peer
					}
					first := dial(replicas[0], c)
					if err := first.Deliver(t.Context(), to, text(1, c.Tenant, "first ")); err != nil {
						t.Fatal(err)
					}
					next := dial(replicas[1], c)
					for range 2 {
						if err := next.Deliver(t.Context(), to, text(2, c.Tenant, "second")); err != nil {
							t.Fatal(err)
						}
					}
					bodies := func() []string {
						var result []string
						if surface == clients.Slack {
							for _, m := range slackAPI.Messages() {
								result = append(result, m.Text)
							}
						} else {
							for _, m := range githubAPI.Comments() {
								result = append(result, m.Body)
							}
						}
						return result
					}
					if got := bodies(); len(got) != 1 || !strings.Contains(got[0], "first second") || strings.Contains(got[0], "secondsecond") {
						t.Fatalf("after takeover and redelivery: %q", got)
					}
					// Identical run and destination names in another installation or
					// tenant must not claim the first installation's native message.
					for _, other := range []clients.Client{
						{Tenant: "t", ID: "other", Surface: surface, Identity: c.Identity},
						{Tenant: "other", ID: "app", Surface: surface, Identity: c.Identity},
					} {
						if err := dial(replicas[1], other).Deliver(t.Context(), to, text(1, other.Tenant, "independent")); err != nil {
							t.Fatal(err)
						}
					}
					if got := bodies(); len(got) != 3 || !strings.Contains(got[0], "first second") {
						t.Fatalf("installation and tenant isolation: %q", got)
					}
				})
			}
		})
	}
}
