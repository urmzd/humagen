package rooms

import (
	"fmt"
	"os"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// load is one simulated large room: viewers each reading their stream on
// their own goroutine, and a publisher posting at a fixed rate, every event
// involving one viewer (the person who posted it).
type load struct {
	Viewers  int
	Rate     int // events a second
	Duration time.Duration
	Interval time.Duration
}

type loadResult struct {
	Published  int
	Immediate  int64 // items delivered at once
	Pages      int64 // pages delivered
	Broadcast  int64 // what sending every event to every viewer would have cost
	MaxViewer  int64 // the most deliveries any one viewer had
	ItemP50    time.Duration
	ItemP99    time.Duration
	PageLagP50 time.Duration
	PageLagP99 time.Duration
	Slow       int64
	Elapsed    time.Duration
}

func (r loadResult) Deliveries() int64 { return r.Immediate + r.Pages }

func (r loadResult) String() string {
	return fmt.Sprintf("published=%d deliveries=%d (immediate=%d pages=%d) broadcast=%d (%.0fx fewer) per-event=%.2f max/viewer=%d item p50=%s p99=%s page lag p50=%s p99=%s slow=%d elapsed=%s",
		r.Published, r.Deliveries(), r.Immediate, r.Pages, r.Broadcast, float64(r.Broadcast)/float64(max(r.Deliveries(), 1)),
		float64(r.Deliveries())/float64(max(r.Published, 1)), r.MaxViewer, r.ItemP50, r.ItemP99, r.PageLagP50, r.PageLagP99, r.Slow, r.Elapsed.Round(time.Millisecond))
}

func simulate(tb testing.TB, l load) loadResult {
	tb.Helper()
	h := NewHub(Config{CoalesceAbove: DefaultCoalesceAbove, TailInterval: l.Interval}, idle)
	defer func() { _ = h.Close() }()

	var (
		immediate, pages, slow atomic.Int64
		mu                     sync.Mutex
		itemLat, pageLat       []time.Duration
		maxViewer              atomic.Int64
		wg                     sync.WaitGroup
	)
	viewers := make([]*Viewer, l.Viewers)
	for i := range viewers {
		viewers[i] = h.Join("acme", "th-big", "p"+strconv.Itoa(i))
	}
	for _, v := range viewers {
		wg.Go(func() {
			var mine int64
			var il, pl []time.Duration
			defer func() {
				immediate.Add(int64(len(il)))
				mu.Lock()
				itemLat = append(itemLat, il...)
				pageLat = append(pageLat, pl...)
				mu.Unlock()
				for {
					cur := maxViewer.Load()
					if mine <= cur || maxViewer.CompareAndSwap(cur, mine) {
						return
					}
				}
			}()
			for {
				select {
				case it := <-v.Items():
					il = append(il, time.Since(it.Value.(time.Time)))
					mine++
				case <-v.Pages():
					if p, ok := v.Page(); ok {
						pl = append(pl, time.Since(p.Through))
						pages.Add(1)
						mine++
					}
				case <-v.Done():
					if v.Err() == ErrSlow {
						slow.Add(1)
					}
					return
				}
			}
		})
	}

	start := time.Now()
	n := int(l.Duration.Seconds() * float64(l.Rate))
	every := time.Second / time.Duration(l.Rate)
	next := start
	for i := range n {
		// Paced in bursts of a millisecond's worth, as a bus delivers them.
		if d := time.Until(next); d > time.Millisecond {
			time.Sleep(d)
		}
		next = next.Add(every)
		kind := KindRun
		if i%5 == 0 {
			kind = KindThread
		}
		h.Publish("acme", "th-big", Item{Kind: kind, Involves: []string{"p" + strconv.Itoa((i*7919)%l.Viewers)}, Value: time.Now()})
	}
	// Two intervals more, for the last page.
	time.Sleep(2*l.Interval + 50*time.Millisecond)
	elapsed := time.Since(start)
	for _, v := range viewers {
		v.Leave()
	}
	wg.Wait()

	slices.Sort(itemLat)
	slices.Sort(pageLat)
	pct := func(s []time.Duration, q float64) time.Duration {
		if len(s) == 0 {
			return 0
		}
		return s[min(len(s)-1, int(float64(len(s))*q))].Round(time.Microsecond)
	}
	return loadResult{
		Published: n, Immediate: immediate.Load(), Pages: pages.Load(), Broadcast: int64(n) * int64(l.Viewers),
		MaxViewer: maxViewer.Load(), ItemP50: pct(itemLat, .5), ItemP99: pct(itemLat, .99),
		PageLagP50: pct(pageLat, .5), PageLagP99: pct(pageLat, .99), Slow: slow.Load(), Elapsed: elapsed,
	}
}

// What a large room delivers is bounded by the events that involve someone
// plus a page per viewer per interval, however fast the room talks.
func TestALargeRoomsFanOutIsBounded(t *testing.T) {
	l := load{Viewers: 5000, Rate: 2000, Duration: time.Second, Interval: 250 * time.Millisecond}
	if testing.Short() {
		l.Viewers = 1000
	}
	r := simulate(t, l)
	t.Log(r)
	ticks := int64(r.Elapsed/l.Interval) + 1
	if r.Slow != 0 {
		t.Fatalf("%d viewers fell behind", r.Slow)
	}
	if r.Immediate != int64(r.Published) {
		t.Errorf("immediate = %d, want one per event (%d)", r.Immediate, r.Published)
	}
	if bound := int64(l.Viewers) * ticks; r.Pages > bound {
		t.Errorf("pages = %d, over viewers x intervals = %d", r.Pages, bound)
	}
	if r.Deliveries()*20 > r.Broadcast {
		t.Errorf("deliveries = %d, not bounded well below a broadcast's %d", r.Deliveries(), r.Broadcast)
	}
}

// HUMAGEN_ROOM_LOAD=1 runs the RFC's case at 100,000 viewers: go test
// -run TestLoadAHundredThousandViewers -v ./pkg/rooms.
func TestLoadAHundredThousandViewers(t *testing.T) {
	if os.Getenv("HUMAGEN_ROOM_LOAD") == "" {
		t.Skip("set HUMAGEN_ROOM_LOAD=1 to run the 100,000-viewer room")
	}
	for _, l := range []load{
		{Viewers: 100_000, Rate: 170, Duration: 10 * time.Second, Interval: DefaultTailInterval},
		{Viewers: 100_000, Rate: 2000, Duration: 10 * time.Second, Interval: DefaultTailInterval},
	} {
		r := simulate(t, l)
		t.Logf("viewers=%d rate=%d/s interval=%s: %s", l.Viewers, l.Rate, l.Interval, r)
		if r.Slow != 0 {
			t.Errorf("%d viewers fell behind", r.Slow)
		}
	}
}

func BenchmarkLargeRoomPublish(b *testing.B) {
	for _, viewers := range []int{100, 10_000, 100_000} {
		b.Run(fmt.Sprintf("viewers=%d", viewers), func(b *testing.B) {
			h := NewHub(Config{TailInterval: DefaultTailInterval}, idle)
			defer func() { _ = h.Close() }()
			vs := make([]*Viewer, viewers)
			for i := range vs {
				vs[i] = h.Join("acme", "th-big", "p"+strconv.Itoa(i))
			}
			stop := make(chan struct{})
			var wg sync.WaitGroup
			// One reader drains every viewer's items, so none is ended.
			wg.Go(func() {
				for {
					select {
					case <-stop:
						return
					default:
					}
					for _, v := range vs {
						select {
						case <-v.Items():
						default:
						}
					}
				}
			})
			b.ResetTimer()
			for i := 0; b.Loop(); i++ {
				h.Publish("acme", "th-big", Item{Kind: KindRun, Involves: []string{"p" + strconv.Itoa(i%viewers)}})
			}
			b.StopTimer()
			close(stop)
			wg.Wait()
		})
	}
}
