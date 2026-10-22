package platform

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

var at = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

// A person's messages start no run, so admission never counts them: the
// post rate does. Each person posts at most PostsPerPerson a minute, each
// project takes at most PostsPerProject, and a window that has ended counts
// from nothing again.
func TestPeoplePostNoFasterThanTheRate(t *testing.T) {
	var r postRate
	for i := range PostsPerPerson {
		if err := r.take("acme", "dana", "Globex", at.Add(time.Duration(i)*time.Millisecond)); err != nil {
			t.Fatalf("message %d of a person's minute = %v", i+1, err)
		}
	}
	if err := r.take("acme", "dana", "Initech", at.Add(time.Second)); !errors.Is(err, ErrPostRate) {
		t.Fatalf("one past a person's rate, in another project = %v, want ErrPostRate", err)
	}
	if err := r.take("initech", "dana", "Globex", at.Add(time.Second)); err != nil {
		t.Fatalf("the same subject in another workspace = %v", err)
	}
	if err := r.take("acme", "dana", "Globex", at.Add(PostWindow)); err != nil {
		t.Fatalf("a person's next minute = %v", err)
	}

	var busy postRate
	for i := range PostsPerProject {
		if err := busy.take("acme", fmt.Sprintf("p%d", i), "Globex", at); err != nil {
			t.Fatalf("message %d of a project's minute = %v", i+1, err)
		}
	}
	if err := busy.take("acme", "fresh", "Globex", at); !errors.Is(err, ErrPostRate) {
		t.Fatalf("one past a project's rate = %v, want ErrPostRate", err)
	}
	// A refused message counted for nobody.
	if err := busy.take("acme", "fresh", "Initech", at); err != nil {
		t.Fatalf("the refused person in another project = %v", err)
	}
}

// The rate keeps at most PostBuckets windows. Past that, ended windows are
// forgotten, and with none ended a post is refused rather than a live
// window dropped, which would let its owner start over.
func TestThePostRateKeepsABoundedNumberOfWindows(t *testing.T) {
	var r postRate
	for i := range PostBuckets / 2 {
		if err := r.take("acme", fmt.Sprintf("p%d", i), fmt.Sprintf("P%d", i), at); err != nil {
			t.Fatal(err)
		}
	}
	if err := r.take("acme", "late", "Late", at); !errors.Is(err, ErrPostRate) || len(r.windows) != PostBuckets {
		t.Fatalf("with every window live, a new one = %v (%d kept)", err, len(r.windows))
	}
	if err := r.take("acme", "late", "Late", at.Add(PostWindow)); err != nil || len(r.windows) != 2 {
		t.Fatalf("once they ended = %v (%d kept)", err, len(r.windows))
	}
}

// Racers posting as one person are let in exactly PostsPerPerson times.
func TestRacersPostingAsOnePersonPassTheRateExactlyItsTimes(t *testing.T) {
	var r postRate
	var wg sync.WaitGroup
	var mu sync.Mutex
	passed := 0
	for range 40 {
		wg.Go(func() {
			if r.take("acme", "dana", "Globex", at) == nil {
				mu.Lock()
				passed++
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if passed != PostsPerPerson {
		t.Fatalf("%d of 40 racers passed, want %d", passed, PostsPerPerson)
	}
}
