package platform_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/event"
	"github.com/urmzd/humagen/fiber/pkg/runtime"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

type publicationBarrier struct {
	state.Store
	armed   atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (s *publicationBarrier) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if namespace == "threads" && s.armed.CompareAndSwap(true, false) {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}

// The publication reaches the domain-write barrier while holding the project
// snapshot read guard. A concurrent audience change cannot enter its callback
// until that exact thread write has committed. Both real backends run this race.
func TestPublicationCommitSerializesWithDestinationMembership(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		barrier := &publicationBarrier{Store: open(t), entered: make(chan struct{}), release: make(chan struct{})}
		var release sync.Once
		unblock := func() { release.Do(func() { close(barrier.release) }) }
		defer unblock()
		p, err := newOn(t, barrier)
		if err != nil {
			t.Fatal(err)
		}
		who := auth.Principal{Tenant: tenant, Subject: "ada", Kind: auth.User, Roles: []auth.Role{auth.Operator}}
		personal, err := p.PersonalProject(who)
		if err != nil {
			t.Fatal(err)
		}
		destination, err := p.Projects.Create(tenant, projects.Project{Name: "Field", Members: []projects.Entry{{Who: "ada", Role: projects.Owner}}}, projects.Writer{By: "ada"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "helper", Model: "offline", Provider: "offline", SystemPrompt: "Help."}, "ada"); err != nil {
			t.Fatal(err)
		}
		th, _, err := p.Threads.Start(tenant, threads.Thread{Project: personal.Name, Channel: "general", Visibility: threads.Private}, threads.Message{By: "ada", Name: "Ada", Text: "Reviewed work"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := runtime.RunID(tenant, "membership-race")
		if err := p.Runs.Record(id, wire.NewEvent(wire.Raise{ID: "membership-race", Tenant: tenant, Agent: "helper", Origin: runs.OriginAPI, Text: "work", Context: map[string]string{runs.MetaProject: personal.Name, runs.MetaThread: th.ID}, At: time.Now()}), 1); err != nil {
			t.Fatal(err)
		}
		p.Runs.Observe(event.Envelope{Tenant: tenant, Run: id, Type: event.TypeCompleted, Data: wire.BodyOf(event.TypeCompleted, []byte(`{"text":"ready"}`)), Time: time.Now()})
		preview, err := p.PreviewPlacement(t.Context(), who, th.ID, "Field", "general")
		if err != nil {
			t.Fatal(err)
		}
		published := make(chan error, 1)
		barrier.armed.Store(true)
		go func() {
			_, err := p.Share(t.Context(), who, th.ID, platform.ThreadShare{Project: "Field", PlacementToken: preview.Token})
			published <- err
		}()
		select {
		case <-barrier.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("publication did not reach commit barrier")
		}
		entered := make(chan struct{})
		changed := make(chan error, 1)
		go func() {
			_, err := p.Projects.Update(tenant, "Field", destination.Version, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error {
				close(entered)
				pr.Members = append(pr.Members, projects.Entry{Who: "ben", Role: projects.Member})
				return nil
			})
			changed <- err
		}()
		select {
		case <-entered:
			t.Fatal("destination audience changed inside the publication commit")
		case <-time.After(50 * time.Millisecond):
		}
		unblock()
		if err := <-published; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
		moved, _ := p.Threads.Get(tenant, th.ID)
		if moved.Project != "Field" {
			t.Fatal("publication did not commit")
		}
	})
}
