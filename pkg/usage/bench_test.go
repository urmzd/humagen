package usage

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// BenchmarkSharedAdmission admits runs in one tenant from several processes
// at once, each a ledger on its own pool, racers spread over them: with no
// limit, and under a run limit on the tenant that every run fits under, so
// each admission is judged. It reports admissions per second.
func BenchmarkSharedAdmission(b *testing.B) {
	for _, procs := range []int{2, 8} {
		for _, limited := range []bool{false, true} {
			name := fmt.Sprintf("procs=%d/limit=%v", procs, limited)
			b.Run(name, func(b *testing.B) {
				ls := benchProcesses(b, procs)
				const racers = 32
				var next atomic.Int64
				max := 0
				if limited {
					max = b.N + 1
				}
				b.ResetTimer()
				var wg sync.WaitGroup
				for r := range racers {
					l := ls[r%len(ls)]
					wg.Go(func() {
						for {
							i := next.Add(1)
							if i > int64(b.N) {
								return
							}
							e := Entry{Tenant: "acme", Project: "Launch", Agent: fmt.Sprintf("a%d", i%16), RunID: fmt.Sprintf("r%d", i), At: t0.Add(time.Duration(i) * time.Millisecond)}
							ok, err := admitUnder(l, e, max)
							if err != nil || !ok {
								b.Errorf("admit %d: %v, %v", i, ok, err)
								return
							}
						}
					})
				}
				wg.Wait()
				b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "admits/s")
			})
		}
	}
}
