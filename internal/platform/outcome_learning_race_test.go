package platform_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

type learningStoreBarrier struct {
	state.Store
	armed       atomic.Bool
	failLibrary atomic.Bool
	failMemory  atomic.Bool
	entered     chan struct{}
	release     chan struct{}
}

func (s *learningStoreBarrier) Write(ctx context.Context, namespace string, ops ...state.Op) error {
	if namespace == "library.skills" && s.failLibrary.Load() || namespace == "memory" && s.failMemory.Load() {
		return statetest.ErrInjected
	}
	if namespace == "memory" && s.armed.CompareAndSwap(true, false) {
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return s.Store.Write(ctx, namespace, ops...)
}

func TestOutcomeLearningWriteFailureLeavesNoApprovedMemoryOrDraft(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		fault := &learningStoreBarrier{Store: st}
		p, err := newOn(t, fault)
		if err != nil {
			t.Fatal(err)
		}
		who, id := learningRun(t, p)
		preview, err := p.OutcomeLearning(who, id, library.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		fault.failLibrary.Store(true)
		if _, err := p.DraftOutcomeSkill(who, id, platform.OutcomeSkillDraft{Snapshot: preview.Snapshot, Definition: learnedDefinition("failed-draft")}); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal("draft write failure ignored", err)
		}
		fault.failLibrary.Store(false)
		fault.failMemory.Store(true)
		input := platform.OutcomeDecisionMemory{Snapshot: preview.Snapshot, Text: "Keep Tuesday"}
		if _, err := p.RememberOutcomeDecision(who, id, input); !errors.Is(err, statetest.ErrInjected) {
			t.Fatal("review write failure ignored", err)
		}
		fault.failMemory.Store(false)
		again, err := newOn(t, st)
		if err != nil {
			t.Fatal(err)
		}
		if len(again.Memory.List(tenant, memory.PersonalOwner("ada"))) != 0 || len(again.SkillLibrary.List(tenant, library.Scope{Kind: "personal", Subject: "ada"})) != 0 {
			t.Fatal("failed learning state reappeared after restart")
		}
		if _, err := again.RememberOutcomeDecision(who, id, input); err != nil {
			t.Fatal("durable snapshot could not retry", err)
		}
	})
}

func TestOutcomeLearningCommitSerializesWithSourceMembership(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		barrier := &learningStoreBarrier{Store: open(t), entered: make(chan struct{}), release: make(chan struct{})}
		var once sync.Once
		unblock := func() { once.Do(func() { close(barrier.release) }) }
		defer unblock()
		p, err := newOn(t, barrier)
		if err != nil {
			t.Fatal(err)
		}
		who, id := learningRun(t, p)
		preview, err := p.OutcomeLearning(who, id, library.Scope{})
		if err != nil {
			t.Fatal(err)
		}
		pr, _ := p.Projects.Get(tenant, "Learning")
		written := make(chan error, 1)
		barrier.armed.Store(true)
		go func() {
			_, err := p.RememberOutcomeDecision(who, id, platform.OutcomeDecisionMemory{Snapshot: preview.Snapshot, Text: "Keep Tuesday"})
			written <- err
		}()
		select {
		case <-barrier.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("learning did not reach commit barrier")
		}
		attempting := make(chan struct{})
		entered := make(chan struct{})
		changed := make(chan error, 1)
		go func() {
			close(attempting)
			_, err := p.Projects.Update(tenant, "Learning", pr.Version, projects.Writer{By: "ada"}, time.Now(), func(pr *projects.Project) error {
				close(entered)
				pr.Members = append(pr.Members, projects.Entry{Who: "new-reader", Role: projects.Member})
				return nil
			})
			changed <- err
		}()
		<-attempting
		select {
		case <-entered:
			t.Fatal("source audience changed during reviewed write")
		case <-time.After(50 * time.Millisecond):
		}
		unblock()
		if err := <-written; err != nil {
			t.Fatal(err)
		}
		if err := <-changed; err != nil {
			t.Fatal(err)
		}
	})
}
