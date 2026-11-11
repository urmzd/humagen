package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/urmzd/orchagent/flow/address"
	"github.com/urmzd/orchagent/flow/deliverystate"
	"github.com/urmzd/orchagent/flow/event"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/egressguard"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/state"
)

// A client's writes to its surface go through an egress guard.
//
// orchagent's connector keeps a run's message pending until a write of it
// succeeds, and its Flush writes every pending message again. The platform
// calls Flush for every client once a second, so a write the surface refuses
// for good (an installation without write permission) was sent again every
// second, by every replica, for as long as the row was pending: across
// restarts, because the row is in the database, and into the provider's
// secondary rate limit, whose own 403 then kept the refusals coming.
//
// The connector is not this repository's to change, so the platform contains
// it from three places it does own:
//
//   - the HTTP transport the client's API calls go through, which judges
//     every answer and sends nothing while writes are held;
//   - the delivery state it hands the connector, which stops listing a
//     message that was refused for good or is backing off;
//   - the two calls it makes into the connector (deliver, flush), which name
//     the attempt and take a failure the guard has dealt with as dealt with.
//
// What only the runtime can fix is noted in docs/architecture/egress.md.

// deliveryFailure is a run's message whose delivery stopped, kept so every
// replica and a restarted one leave it alone.
type deliveryFailure struct {
	Client  string              `json:"client"`
	Key     deliverystate.Key   `json:"key"`
	Refusal egressguard.Refusal `json:"refusal"`
}

// deliveryFailures is egressguard.Failures on the platform's state.
type deliveryFailures struct {
	table  *state.Table[deliveryFailure]
	client string
}

// deliveryFailureKey lists a run's stopped messages together.
func deliveryFailureKey(k deliverystate.Key) string {
	raw, _ := json.Marshal(k)
	sum := sha256.Sum256(raw)
	return runFailuresPrefix(k.Run) + hex.EncodeToString(sum[:])
}

func runFailuresPrefix(run string) string {
	sum := sha256.Sum256([]byte(run))
	return hex.EncodeToString(sum[:16]) + ":"
}

func (f deliveryFailures) Load(k deliverystate.Key) (egressguard.Refusal, bool, error) {
	row, err := f.table.Lookup(k.Tenant, deliveryFailureKey(k))
	if errors.Is(err, state.ErrNotFound) {
		return egressguard.Refusal{}, false, nil
	}
	if err != nil {
		return egressguard.Refusal{}, false, err
	}
	return row.Value.Refusal, true, nil
}

func (f deliveryFailures) Save(k deliverystate.Key, r egressguard.Refusal) (bool, error) {
	batch := f.table.Batch()
	batch.Create(k.Tenant, deliveryFailureKey(k), deliveryFailure{Client: f.client, Key: k, Refusal: r})
	err := batch.Commit()
	if errors.Is(err, state.ErrConflict) {
		return false, nil
	}
	return err == nil, err
}

// DeliveryFailures is why each of a run's messages that a surface refused
// for good stopped being delivered.
func (p *Platform) DeliveryFailures(tenant, run string) []egressguard.Refusal {
	rows, _, err := p.deliveryFailures.List(tenant, state.Query{Prefix: runFailuresPrefix(run), Limit: 100})
	if err != nil {
		return nil
	}
	out := make([]egressguard.Refusal, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Value.Refusal)
	}
	return out
}

// guardFor builds the egress guard of one client.
func (p *Platform) guardFor(c clients.Client) *egressguard.Guard {
	// Without a state store stopped messages are kept for as long as the
	// process lives, as everything else is then, and through a reseat.
	var failures egressguard.Failures = p.memoryFailures
	if p.deliveryFailures.Enabled() {
		failures = deliveryFailures{table: p.deliveryFailures, client: c.ID}
	}
	return egressguard.New(egressguard.Config{
		Policy:   p.cfg.EgressPolicy,
		Clock:    p.now,
		Log:      p.log,
		Attrs:    []any{"connector", string(c.Surface), "tenant", c.Tenant, "client", c.ID},
		Failures: failures,
		OnStopped: func(_ context.Context, k deliverystate.Key, r egressguard.Refusal) {
			if err := p.deliveryStopped(c, k, r); err != nil {
				p.log.Error("platform: a stopped delivery could not be shown to people", "tenant", c.Tenant, "client", c.ID, "run", k.Run, "error", err)
			}
		},
		OnHealth: func(h egressguard.Health) {
			if err := p.saveEgressHealth(c, h); err != nil {
				p.log.Warn("platform: a client's egress health could not be kept", "tenant", c.Tenant, "client", c.ID, "error", err)
			}
		},
	})
}

// clientHTTP is the HTTP client a client's connector reaches its provider
// through, before the guard and its credentials are put around it.
func (p *Platform) clientHTTP() *http.Client {
	if p.cfg.ClientHTTP != nil {
		return p.cfg.ClientHTTP
	}
	return http.DefaultClient
}

// guarded is a copy of client whose calls go through the guard.
func guarded(client *http.Client, g *egressguard.Guard, c egressguard.Classifier) *http.Client {
	copy := *client
	copy.Transport = g.Transport(client.Transport, c)
	return &copy
}

func surfaceTitle(s clients.Surface) string {
	if s == clients.GitHub {
		return "GitHub"
	}
	return "Slack"
}

// deliveryStopped tells people, once per run and client, that the surface
// refused the run's message and nothing more will be sent for it. The run
// itself is unchanged: its answer is in its session.
func (p *Platform) deliveryStopped(c clients.Client, k deliverystate.Key, r egressguard.Refusal) error {
	m := inbox.Message{
		Kind: inbox.KindFailure, Subject: surfaceTitle(c.Surface) + " refused a reply", From: inbox.From{Kind: "system", Name: SystemSender},
		Severity: inbox.Warning, Source: inbox.Source{Run: k.Run},
	}
	part := inbox.FailurePart{Run: k.Run, Error: "The reply was not delivered to " + k.Target + " and will not be tried again. " + r.String()}
	if run, err := p.Runs.Get(c.Tenant, k.Run); err == nil {
		m.Subject = surfaceTitle(c.Surface) + " refused " + run.Agent + "'s reply"
		m.Source.Thread, m.Source.Agent, m.Source.Project = run.Thread, run.Agent, run.Project
		part.Agent = run.Agent
	}
	m.Parts = []inbox.Part{{Type: inbox.PartFailure, Failure: &part}}
	return p.completionInbox(c.Tenant, "delivery-refused:"+c.ID+":"+k.Run, m, p.now())
}

// saveEgressHealth keeps what the guard says of a client's writes as the
// client's connection health, where a manual check keeps its own.
func (p *Platform) saveEgressHealth(c clients.Client, h egressguard.Health) error {
	if !p.connectionHealth.Enabled() {
		return nil
	}
	who := auth.Principal{Tenant: c.Tenant}
	revision := p.clientsHealthRevision(c.Tenant, c.ID, true)
	if revision == "" {
		return nil
	}
	return p.putConnectionHealth(c.Tenant, connectionObservationKey(who, c.ID, true), connectionObservation{
		Revision: revision,
		Health:   ConnectionHealth{Healthy: h.Healthy, CheckedAt: p.now(), Message: h.Message()},
	})
}

// deliver gives the client's connector one event of a run. A write the guard
// refused, stopped or held is finished as far as the caller is concerned:
// the connector committed the event to the message's pending state before it
// wrote, and flush writes it when the guard allows.
func (st *seat) deliver(ctx context.Context, to address.Address, e event.Envelope) error {
	if st.guard == nil {
		return st.peer.Deliver(ctx, to, e)
	}
	ctx, attempt := st.guard.Begin(ctx)
	err := st.peer.Deliver(ctx, to, e)
	if attempt.Covers(err) {
		return nil
	}
	return err
}

// flush writes the client's pending messages, unless its writes are held.
func (st *seat) flush(ctx context.Context) error {
	if st.guard == nil {
		return st.peer.Flush(ctx)
	}
	if _, held := st.guard.Held(); held {
		return nil
	}
	ctx, attempt := st.guard.Begin(ctx)
	err := st.peer.Flush(ctx)
	if attempt.Covers(err) {
		return nil
	}
	return err
}

// held reports whether the client's writes are paused, so the flush loop
// leaves its surface alone until they resume.
func (st *seat) held() (time.Time, bool) {
	if st.guard == nil {
		return time.Time{}, false
	}
	return st.guard.Held()
}
