package e2e

import (
	"net"
	"net/http"
	"slices"
	"sync"
	"testing"
)

// refusingProxy is an HTTP proxy that refuses every request and remembers
// the hosts it was asked for. A process given it as its proxy reaches no
// host outside this machine, and the test can tell that it tried.
type refusingProxy struct {
	url string

	mu    sync.Mutex
	asked []string
}

func newRefusingProxy(t *testing.T) *refusingProxy {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &refusingProxy{url: "http://" + ln.Addr().String()}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.asked = append(p.asked, r.Method+" "+r.Host)
		p.mu.Unlock()
		http.Error(w, "this test reaches no outside host", http.StatusBadGateway)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return p
}

func (p *refusingProxy) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.asked)
}

func (p *refusingProxy) hosts() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.asked)
}
