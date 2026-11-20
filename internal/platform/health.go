package platform

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Health statuses, worst last. A component is down when its check failed,
// degraded when it answered slowly or only partly, and operational otherwise.
const (
	HealthOperational = "operational"
	HealthDegraded    = "degraded"
	HealthDown        = "down"
)

// Probe checks one dependency and says why it failed. It must not spend
// tokens: a model provider is asked for its model list, never a completion.
type Probe func(ctx context.Context) error

// Bounds on what Health keeps and how often it asks.
const (
	healthHistory  = 90               // samples per component, oldest dropped
	healthEvery    = 30 * time.Second // a report younger than this is reused
	healthTimeout  = 5 * time.Second  // each probe
	healthSlowness = 2 * time.Second  // slower than this is degraded
)

// HealthSample is one check of a component.
type HealthSample struct {
	At     time.Time `json:"at"`
	Status string    `json:"status"`
}

// HealthComponent is one dependency, as last checked.
type HealthComponent struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Group     string `json:"group"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	LatencyMS int64  `json:"latency_ms"`
	// History is the latest checks, oldest first, at most healthHistory.
	History []HealthSample `json:"history"`
}

// Health is the deployment's state: each component, and the worst of them.
type Health struct {
	Status     string            `json:"status"`
	CheckedAt  time.Time         `json:"checked_at"`
	Components []HealthComponent `json:"components"`
}

type healthBook struct {
	mu      sync.Mutex
	last    Health
	history map[string][]HealthSample
}

// Health checks each component the deployment runs on, at most once every
// healthEvery: the API, its stores and bus, and every model provider. With
// no model provider set up, agents cannot answer, and Health says so as an
// outage rather than leaving the list empty.
func (p *Platform) Health(ctx context.Context) Health {
	b := &p.health
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now().UTC()
	if !b.last.CheckedAt.IsZero() && now.Sub(b.last.CheckedAt) < healthEvery {
		return b.last
	}
	comps := []HealthComponent{
		{ID: "api", Name: "API", Group: "Platform", Status: HealthOperational, Detail: "Answering requests"},
		{ID: "stores", Name: "Stores", Group: "Platform", Status: HealthOperational, Detail: kept(p.cfg.Backends.State != nil)},
		{ID: "bus", Name: "Event bus", Group: "Platform", Status: HealthOperational, Detail: kept(p.cfg.Bus != nil || p.cfg.Backends.Bus != nil)},
	}
	comps = append(comps, p.providerHealth(ctx)...)
	if b.history == nil {
		b.history = map[string][]HealthSample{}
	}
	worst := HealthOperational
	for i := range comps {
		c := &comps[i]
		h := append(b.history[c.ID], HealthSample{At: now, Status: c.Status})
		if len(h) > healthHistory {
			h = h[len(h)-healthHistory:]
		}
		b.history[c.ID] = h
		c.History = slices.Clone(h)
		worst = worse(worst, c.Status)
	}
	b.last = Health{Status: worst, CheckedAt: now, Components: comps}
	return b.last
}

// providerHealth probes every provider the deployment declares, at once.
// A workspace's own are added per workspace (HealthFor).
func (p *Platform) providerHealth(ctx context.Context) []HealthComponent {
	names := p.cfg.Ext.ProviderNames()
	out := make([]HealthComponent, len(names))
	var wg sync.WaitGroup
	for i, name := range names {
		wg.Go(func() {
			out[i] = probeComponent(ctx, "provider:"+name, name, p.cfg.Ext.Probes[name])
		})
	}
	wg.Wait()
	return out
}

// HealthFor is Health as tenant's workspace reads it: each provider its
// administrators set up is listed, unchecked (Check on the provider asks
// it), and a workspace with no provider at all, declared or its own, is told
// so as an outage, since no agent of its can answer.
func (p *Platform) HealthFor(ctx context.Context, tenant string) Health {
	h := p.Health(ctx)
	h.Components = slices.Clone(h.Components)
	own := p.ModelProviders(tenant)
	for _, m := range own {
		h.Components = append(h.Components, HealthComponent{
			ID: "provider:" + m.Name, Name: m.Name, Group: "Model providers", Status: HealthOperational,
			Detail:  "Set up by an administrator; not checked here",
			History: []HealthSample{{At: h.CheckedAt, Status: HealthOperational}},
		})
	}
	if len(own) == 0 && len(p.cfg.Ext.ProviderNames()) == 0 {
		h.Components = append(h.Components, HealthComponent{
			ID: "provider:none", Name: "Model providers", Group: "Model providers", Status: HealthDown,
			Detail:  "No model provider is set up, so agents cannot answer. An administrator adds one in Settings, under Model providers.",
			History: []HealthSample{{At: h.CheckedAt, Status: HealthDown}},
		})
		h.Status = worse(h.Status, HealthDown)
	}
	return h
}

func kept(durable bool) string {
	if durable {
		return "Durable"
	}
	return "In memory: lost on restart"
}

func probeComponent(ctx context.Context, id, name string, probe Probe) HealthComponent {
	c := HealthComponent{ID: id, Name: name, Group: "Model providers", Status: HealthOperational}
	if probe == nil {
		c.Detail = "Declared; this kind has no check"
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	start := time.Now()
	err := probe(ctx)
	c.LatencyMS = time.Since(start).Milliseconds()
	switch {
	case err != nil:
		c.Status, c.Detail = HealthDown, err.Error()
	case time.Since(start) > healthSlowness:
		c.Status, c.Detail = HealthDegraded, "Slow to answer"
	default:
		c.Detail = "Reachable, key accepted"
	}
	return c
}

func worse(a, b string) string {
	rank := map[string]int{HealthOperational: 0, HealthDegraded: 1, HealthDown: 2}
	if rank[b] > rank[a] {
		return b
	}
	return a
}
