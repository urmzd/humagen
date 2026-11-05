package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

// Markers a test puts in a run's text to choose what the fake model does.
const (
	markStream = "E2E-STREAM"    // ten text deltas, spaced, so a reader sees them arrive
	markAsk    = "E2E-ASK"       // ask_person, then a text answer once the tool result is back
	markTerm   = "E2E-SLOW-TERM" // the first call hangs until the caller goes; later calls answer
	markKill   = "E2E-SLOW-KILL" // the same, for the crash case
	streamN    = 10              // text deltas markStream sends
	streamGap  = 150 * time.Millisecond
)

// call is one request the fake answered: which replica made it, the marker
// it carried, and whether it hung until the caller went away.
type call struct {
	Replica string
	Marker  string
	Hung    bool
	Result  bool // the request carried a tool result
}

// fakeModel is an Anthropic Messages API that answers from a script chosen
// by a marker in the request. It listens once per replica, so it knows which
// replica called: each replica's spec points its base URL at its own port.
type fakeModel struct {
	t *testing.T

	mu    sync.Mutex
	calls []call
	hung  map[string]bool // markers whose first call already hung
	// started is told the replica name each time a call starts hanging.
	started chan string
}

func newFakeModel(t *testing.T) *fakeModel {
	return &fakeModel{t: t, hung: map[string]bool{}, started: make(chan string, 8)}
}

// listen serves the fake for one replica and returns its base URL.
func (f *fakeModel) listen(replica string) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		f.t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(replica, w, r) })}
	go func() { _ = srv.Serve(ln) }()
	f.t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String() + "/"
}

func (f *fakeModel) callsFor(marker string) []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []call
	for _, c := range f.calls {
		if c.Marker == marker {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeModel) serve(replica string, w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages") {
		http.NotFound(w, r)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	body := string(raw)
	marker := ""
	for _, m := range []string{markTerm, markKill, markStream, markAsk} {
		if strings.Contains(body, m) {
			marker = m
			break
		}
	}
	c := call{Replica: replica, Marker: marker, Result: strings.Contains(body, `"tool_result"`)}
	if marker == markTerm || marker == markKill {
		f.mu.Lock()
		c.Hung = !f.hung[marker]
		f.hung[marker] = true
		f.mu.Unlock()
	}
	f.mu.Lock()
	f.calls = append(f.calls, c)
	f.mu.Unlock()

	sse := newSSE(w)
	switch {
	case c.Hung:
		// A turn in flight: the model is still answering when the replica
		// stops. It writes the start, then waits for the caller to go.
		sse.start()
		f.started <- replica
		<-r.Context().Done()
	case marker == markStream:
		sse.start()
		sse.textBlock(0, func(delta func(string)) {
			for i := range streamN {
				delta(fmt.Sprintf("chunk-%d ", i))
				time.Sleep(streamGap)
			}
		})
		sse.stop("end_turn")
	case marker == markAsk && !c.Result:
		sse.start()
		sse.toolUse(0, "toolu_e2e_ask", "ask_person", map[string]any{"question": "Which colour?", "options": []string{"red", "blue"}})
		sse.stop("tool_use")
	case marker == markTerm || marker == markKill:
		sse.start()
		sse.textBlock(0, func(delta func(string)) { delta("resumed on " + replica) })
		sse.stop("end_turn")
	default:
		sse.start()
		sse.textBlock(0, func(delta func(string)) { delta("ok") })
		sse.stop("end_turn")
	}
}

// sse writes Anthropic's streaming events.
type sse struct {
	w http.ResponseWriter
	f http.Flusher
}

func newSSE(w http.ResponseWriter) *sse {
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	f, _ := w.(http.Flusher)
	return &sse{w: w, f: f}
}

func (s *sse) event(name string, v any) {
	b, _ := json.Marshal(v)
	_, _ = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, b)
	if s.f != nil {
		s.f.Flush()
	}
}

func (s *sse) start() {
	s.event("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_e2e", "type": "message", "role": "assistant", "model": "claude-haiku-5-5", "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
	}})
}

func (s *sse) textBlock(i int, write func(delta func(string))) {
	s.event("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "text", "text": ""}})
	write(func(text string) {
		s.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "text_delta", "text": text}})
	})
	s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
}

func (s *sse) toolUse(i int, id, name string, input any) {
	b, _ := json.Marshal(input)
	s.event("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}})
	s.event("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": map[string]any{"type": "input_json_delta", "partial_json": string(b)}})
	s.event("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
}

func (s *sse) stop(reason string) {
	s.event("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": reason, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 5}})
	s.event("message_stop", map[string]any{"type": "message_stop"})
}
