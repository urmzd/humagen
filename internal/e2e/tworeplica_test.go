// Package e2e drives real humagen processes end to end.
//
// TestTwoReplicas starts two `humagen serve` processes on one Postgres, the
// way two server pods share Cloud SQL, with a fake Anthropic model so no
// model is called. It runs only when HUMAGEN_E2E_POSTGRES_URL names a
// database it may create databases on; scripts/two-replica.sh starts one in
// Docker and runs it.
//
// With HUMAGEN_E2E_A_URL and HUMAGEN_E2E_B_URL set instead, it checks two
// replicas already running (a port-forward to each server pod) with the
// agent HUMAGEN_E2E_AGENT and that agent's real model: a run started on A
// streams to B, once. HUMAGEN_E2E_LOGIN, when set, is sent as
// Tailscale-User-Login, which a pod's loopback trusts. HUMAGEN_E2E_HOST
// optionally sets the HTTP Host expected by that deployment through port forwards.
package e2e

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	// URLEnv names a Postgres the test creates a fresh database on.
	URLEnv = "HUMAGEN_E2E_POSTGRES_URL"
	// BinEnv names a built humagen binary. Empty builds one.
	BinEnv = "HUMAGEN_E2E_BIN"
)

func TestTwoReplicas(t *testing.T) {
	if a, b := os.Getenv("HUMAGEN_E2E_A_URL"), os.Getenv("HUMAGEN_E2E_B_URL"); a != "" && b != "" {
		running(t, a, b)
		return
	}
	admin := os.Getenv(URLEnv)
	if admin == "" {
		t.Skipf("%s is not set; scripts/two-replica.sh runs this against Postgres in Docker", URLEnv)
	}
	dbURL := freshDatabase(t, admin)
	bin := humagenBinary(t)
	model := newFakeModel(t)
	dir := t.TempDir()
	a := newReplica(t, "a", bin, dir, dbURL, model.listen("a"))
	b := newReplica(t, "b", bin, dir, dbURL, model.listen("b"))
	a.start()
	b.start()
	byName := map[string]*replica{"a": a, "b": b}

	// Agents created through A are read by B from the shared stores.
	for _, body := range []string{
		`{"name":"streamer","provider":"anthropic","model":"claude-haiku-5-5","system_prompt":"Stream."}`,
		`{"name":"asker","provider":"anthropic","model":"claude-haiku-5-5","system_prompt":"Ask.","tools":["ask_person"],"answerers":["dev"]}`,
		`{"name":"slow","provider":"anthropic","model":"claude-haiku-5-5","system_prompt":"Slow."}`,
	} {
		a.do(http.MethodPost, "/api/agents", body, http.StatusCreated, nil)
	}
	b.do(http.MethodGet, "/api/agents/slow/history", "", http.StatusOK, nil)

	t.Run("one replica leads", func(t *testing.T) {
		eventually(t, 30*time.Second, "one replica to lead", func() bool { return a.leading()+b.leading() >= 1 })
		time.Sleep(2 * time.Second)
		if n := a.leading() + b.leading(); n != 1 {
			t.Fatalf("%d replicas lead, want 1", n)
		}
	})

	t.Run("a run started on A streams live to B", func(t *testing.T) {
		onA, onB := a.stream(t), b.stream(t)
		var run runView
		a.do(http.MethodPost, "/api/runs", `{"agent":"streamer","text":"`+markStream+`"}`, http.StatusAccepted, &run)
		gotB := onB.until(t, run.ID, 60*time.Second)
		gotA := onA.until(t, run.ID, 60*time.Second)

		deltas, firstDelta, end := 0, time.Time{}, time.Time{}
		for _, e := range gotB {
			switch e.Type {
			case "run.delta.text.v1":
				deltas++
				if firstDelta.IsZero() {
					firstDelta = e.at
				}
			case "run.completed.v1":
				end = e.at
			}
		}
		if end.IsZero() {
			t.Fatalf("B never saw the run complete: %v", types(gotB))
		}
		// The run is recorded after its first event is published, by A:
		// B's stream still delivers that event rather than drop it.
		if gotB[0].Type != "run.accepted.v1" {
			t.Errorf("B's first event of the run is %s, want run.accepted.v1", gotB[0].Type)
		}
		if deltas < streamN {
			t.Errorf("B saw %d text deltas, want at least %d: %v", deltas, streamN, types(gotB))
		}
		// The model spaces its deltas over ~1.5s: a reader that only got
		// them after the run ended would see them all at once.
		if gap := end.Sub(firstDelta); gap < time.Duration(streamN/2)*streamGap {
			t.Errorf("B saw the first delta only %v before the end: not live", gap)
		}
		noDuplicates(t, "B's stream", gotB)
		noDuplicates(t, "A's stream", gotA)
		if fmt.Sprint(ids(gotA)) != fmt.Sprint(ids(gotB)) {
			t.Errorf("the replicas streamed different events:\nA %v\nB %v", ids(gotA), ids(gotB))
		}
		persisted(t, b, run.ID)
	})

	t.Run("a question asked on one replica is answered on the other", func(t *testing.T) {
		onA := a.stream(t)
		var run runView
		a.do(http.MethodPost, "/api/runs", `{"agent":"asker","text":"`+markAsk+`"}`, http.StatusAccepted, &run)
		var parked runView
		eventually(t, 30*time.Second, "the run to park on its question, read through B", func() bool {
			b.do(http.MethodGet, "/api/runs/"+run.ID, "", http.StatusOK, &parked)
			return parked.Question.ID != ""
		})
		// The person it asks hears of it on the other replica: the inbox
		// notice crosses through Postgres NOTIFY.
		eventually(t, 15*time.Second, "A's stream to carry the inbox notice", func() bool { return onA.named("inbox") > 0 })
		b.do(http.MethodPost, "/api/runs/"+run.ID+"/questions/"+parked.Question.ID+"/answer", `{"action":"choose","choice":"red"}`, http.StatusOK, nil)
		got := onA.until(t, run.ID, 30*time.Second)
		if last := got[len(got)-1].Type; last != "run.completed.v1" {
			t.Fatalf("the answered run ended %s: %v", last, types(got))
		}
		noDuplicates(t, "A's stream", got)
		if c := model.callsFor(markAsk); len(c) != 2 || !c[1].Result {
			t.Errorf("model calls for the question = %+v, want the ask and then the answer", c)
		}
	})

	// A replica that is stopped mid-turn drains: it takes no new turn, waits
	// out its grace, then hands the run back, and the other replica resumes
	// it. A replica that crashes loses its claim when the lease runs out.
	for _, tc := range []struct {
		name, marker string
		sig          syscall.Signal
		within       time.Duration
	}{
		{"SIGTERM mid-turn drains and the other replica resumes", markTerm, syscall.SIGTERM, 60 * time.Second},
		{"SIGKILL mid-turn: the other replica resumes after the lease", markKill, syscall.SIGKILL, 150 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var run runView
			a.do(http.MethodPost, "/api/runs", `{"agent":"slow","text":"`+tc.marker+`"}`, http.StatusAccepted, &run)
			var owner string
			select {
			case owner = <-model.started:
			case <-time.After(30 * time.Second):
				t.Fatal("no replica started the turn")
			}
			own, other := byName[owner], a
			if own == a {
				other = b
			}
			onOther := other.stream(t)
			wasLeader := own.leading() > 0
			t.Logf("replica %s holds the run (leader: %v); sending %v", own.name, wasLeader, tc.sig)
			own.signal(tc.sig)
			if tc.sig == syscall.SIGTERM {
				// Draining: not ready, so a load balancer stops sending it work.
				code := 0
				for deadline := time.Now().Add(2 * time.Second); code != http.StatusServiceUnavailable && time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
					code = own.readyz()
				}
				if code != http.StatusServiceUnavailable {
					t.Errorf("/readyz while draining = %d, want 503", code)
				}
			}
			got := onOther.until(t, run.ID, tc.within)
			if last := got[len(got)-1].Type; last != "run.completed.v1" {
				t.Fatalf("the run ended %s on %s: %v", last, other.name, types(got))
			}
			if err := own.wait(30 * time.Second); tc.sig == syscall.SIGTERM && err != nil {
				t.Errorf("replica %s did not stop cleanly: %v", own.name, err)
			}
			calls := model.callsFor(tc.marker)
			if len(calls) != 2 || calls[0].Replica != own.name || calls[1].Replica != other.name {
				t.Errorf("model calls = %+v, want one hung on %s, then one on %s", calls, own.name, other.name)
			}
			noDuplicates(t, other.name+"'s stream", got)
			persisted(t, other, run.ID)
			if wasLeader {
				eventually(t, 60*time.Second, other.name+" to take the lead", func() bool { return other.leading() > 0 })
			}
			// Back to two replicas for what follows.
			own.start()
		})
	}
}

// running checks two replicas someone else started: no process is stopped
// and no model is faked, so it checks only that a run started on A streams
// live to B, once, with the same events A streams.
func running(t *testing.T, aURL, bURL string) {
	agent := os.Getenv("HUMAGEN_E2E_AGENT")
	if agent == "" {
		t.Fatal("HUMAGEN_E2E_AGENT names the agent to run on the running replicas")
	}
	a := externalReplica(t, "a", aURL)
	b := externalReplica(t, "b", bURL)
	for _, r := range []*replica{a, b} {
		if code := r.readyz(); code != http.StatusOK {
			t.Fatalf("replica %s at %s: /readyz = %d", r.name, r.url, code)
		}
	}
	onA, onB := a.stream(t), b.stream(t)
	body, _ := json.Marshal(map[string]string{"agent": agent, "text": "Reply with one short sentence: a two-replica check."})
	var run runView
	a.do(http.MethodPost, "/api/runs", string(body), http.StatusAccepted, &run)
	gotB := onB.until(t, run.ID, 3*time.Minute)
	gotA := onA.until(t, run.ID, time.Minute)
	t.Logf("run %s: B streamed %v", run.ID, types(gotB))
	if gotB[0].Type != "run.accepted.v1" {
		t.Errorf("B's first event of the run is %s, want run.accepted.v1", gotB[0].Type)
	}
	if last := gotB[len(gotB)-1].Type; last != "run.completed.v1" {
		t.Errorf("the run ended %s", last)
	}
	noDuplicates(t, "B's stream", gotB)
	noDuplicates(t, "A's stream", gotA)
	if fmt.Sprint(ids(gotA)) != fmt.Sprint(ids(gotB)) {
		t.Errorf("the replicas streamed different events:\nA %v\nB %v", ids(gotA), ids(gotB))
	}
	persisted(t, b, run.ID)
}

// externalReplica keeps the dial URL separate from the deployment's HTTP Host.
func externalReplica(t *testing.T, name, baseURL string) *replica {
	header := http.Header{}
	if login := os.Getenv("HUMAGEN_E2E_LOGIN"); login != "" {
		header.Set("Tailscale-User-Login", login)
	}
	return &replica{t: t, name: name, url: strings.TrimRight(baseURL, "/"), host: os.Getenv("HUMAGEN_E2E_HOST"), header: header, log: &lockedBuffer{}}
}

// runView is the part of a run the test reads.
type runView struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Question struct {
		ID string `json:"id"`
	} `json:"question"`
}

type ev struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Run  string `json:"run"`
	Seq  uint64 `json:"seq"`
	at   time.Time
}

func terminal(t string) bool {
	return t == "run.completed.v1" || t == "run.failed.v1" || t == "run.stopped.v1"
}

// persisted holds a run's stored events to one copy of each: sequence
// numbers rise, and exactly one terminal event. The run's stored events are
// written by the observer's consumer group, after the stream has them, so
// the terminal event is waited for.
func persisted(t *testing.T, r *replica, run string) {
	t.Helper()
	var out struct {
		Events []ev `json:"events"`
	}
	ends := func() int {
		n := 0
		for _, e := range out.Events {
			if terminal(e.Type) {
				n++
			}
		}
		return n
	}
	start := time.Now()
	for deadline := start.Add(30 * time.Second); ; time.Sleep(100 * time.Millisecond) {
		r.do(http.MethodGet, "/api/runs/"+run+"/events", "", http.StatusOK, &out)
		if ends() > 0 || time.Now().After(deadline) {
			break
		}
	}
	t.Logf("stored events of %s complete on %s after %v", run, r.name, time.Since(start).Round(time.Millisecond))
	var last uint64
	for _, e := range out.Events {
		if e.Seq <= last {
			t.Errorf("stored events of %s: seq %d after %d", run, e.Seq, last)
		}
		last = e.Seq
	}
	if n := ends(); n != 1 {
		t.Errorf("stored events of %s have %d terminal events: %v", run, n, types(out.Events))
	}
}

func noDuplicates(t *testing.T, where string, es []ev) {
	t.Helper()
	seen := map[string]bool{}
	for _, e := range es {
		if seen[e.ID] {
			t.Errorf("%s delivered %s (%s) twice", where, e.ID, e.Type)
		}
		seen[e.ID] = true
	}
}

func types(es []ev) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Type
	}
	return out
}

func ids(es []ev) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}

// replica is one humagen serve process.
type replica struct {
	t                   *testing.T
	name, bin, dir, db  string
	modelURL, spec, url string
	host                string
	port                int
	header              http.Header
	log                 *lockedBuffer
	cmd                 *exec.Cmd
	exited              chan error
	// env is added to the process's environment.
	env []string
}

func newReplica(t *testing.T, name, bin, dir, db, modelURL string) *replica {
	r := &replica{t: t, name: name, bin: bin, dir: dir, db: db, modelURL: modelURL, port: freePort(t), header: http.Header{}, log: &lockedBuffer{}}
	r.url = fmt.Sprintf("http://127.0.0.1:%d", r.port)
	r.spec = filepath.Join(dir, "spec-"+name+".yaml")
	spec := fmt.Sprintf(`api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  models:
    - name: anthropic
      kind: anthropic
      anthropic: {base_url: %q, key: {ref: env:E2E_FAKE_KEY}}
`, modelURL)
	if err := os.WriteFile(r.spec, []byte(spec), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if r.cmd != nil && r.cmd.ProcessState == nil {
			_ = r.cmd.Process.Signal(syscall.SIGKILL)
			_ = r.wait(30 * time.Second)
		}
		if t.Failed() {
			t.Logf("replica %s log:\n%s", r.name, r.log.String())
		}
	})
	return r
}

func (r *replica) start() {
	r.t.Helper()
	ws := filepath.Join(r.dir, "workspace-"+r.name)
	cmd := exec.Command(r.bin, "serve", "-f", r.spec,
		"-addr", fmt.Sprintf("127.0.0.1:%d", r.port), "-public-url", r.url,
		"-auth", "dev", "-sandbox", "local",
		// Blobs on one directory, as replicas share one bucket.
		"-storage", "disk", "-storage-dir", filepath.Join(r.dir, "blobs"),
		"-workspace", ws, "-drain-grace", "3s")
	cmd.Env = append(append(os.Environ(), "HUMAGEN_POSTGRES_URL="+r.db, "E2E_FAKE_KEY=fake"), r.env...)
	fmt.Fprintf(r.log, "--- start %s\n", time.Now().Format(time.RFC3339Nano))
	cmd.Stdout, cmd.Stderr = r.log, r.log
	if err := cmd.Start(); err != nil {
		r.t.Fatal(err)
	}
	r.cmd, r.exited = cmd, make(chan error, 1)
	go func(c *exec.Cmd, done chan error) { done <- c.Wait() }(cmd, r.exited)
	eventually(r.t, 60*time.Second, "replica "+r.name+" to be ready", func() bool { return r.readyz() == http.StatusOK })
}

func (r *replica) signal(sig syscall.Signal) {
	if err := r.cmd.Process.Signal(sig); err != nil {
		r.t.Fatal(err)
	}
}

func (r *replica) wait(d time.Duration) error {
	select {
	case err := <-r.exited:
		r.exited <- err
		return err
	case <-time.After(d):
		return fmt.Errorf("still running after %v", d)
	}
}

// leading counts the times this replica took the lead since it last started.
func (r *replica) leading() int {
	s := r.log.String()
	if i := strings.LastIndex(s, "--- start "); i >= 0 {
		s = s[i:]
	}
	return strings.Count(s, `msg="platform: leading"`)
}

func (r *replica) readyz() int {
	req, _ := http.NewRequest(http.MethodGet, r.url+"/readyz", nil)
	req.Header = r.header.Clone()
	req.Host = r.host
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0
	}
	_ = res.Body.Close()
	return res.StatusCode
}

func (r *replica) do(method, path, body string, want int, out any) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.url+path, strings.NewReader(body))
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header = r.header.Clone()
	req.Host = r.host
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatalf("%s %s on %s: %v", method, path, r.name, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		r.t.Fatalf("%s %s on %s = %d, want %d: %s", method, path, r.name, res.StatusCode, want, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			r.t.Fatalf("%s %s on %s: %v: %s", method, path, r.name, err, raw)
		}
	}
}

// raw writes the body of a 200 response to out.
func (r *replica) raw(method, path string, out io.Writer) {
	r.t.Helper()
	req, err := http.NewRequest(method, r.url+path, nil)
	if err != nil {
		r.t.Fatal(err)
	}
	req.Header = r.header.Clone()
	req.Host = r.host
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatalf("%s %s on %s: %v", method, path, r.name, err)
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		r.t.Fatalf("%s %s on %s = %d: %s", method, path, r.name, res.StatusCode, body)
	}
	_, _ = out.Write(body)
}

// stream is a reader's /api/events connection on one replica.
type stream struct {
	mu     sync.Mutex
	events []ev
	names  map[string]int
	wake   chan struct{}
}

func (r *replica) stream(t *testing.T) *stream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, r.url+"/api/events?"+url.Values{"from": {"0"}}.Encode(), nil)
	req.Header = r.header.Clone()
	req.Host = r.host
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusOK {
		t.Fatalf("/api/events on %s = %d", r.name, res.StatusCode)
	}
	s := &stream{names: map[string]int{}, wake: make(chan struct{}, 1)}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		name := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				s.mu.Lock()
				if name == "" {
					var e ev
					if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &e) == nil {
						e.at = time.Now()
						s.events = append(s.events, e)
					}
				} else {
					s.names[name]++
				}
				s.mu.Unlock()
				select {
				case s.wake <- struct{}{}:
				default:
				}
			case line == "":
				name = ""
			}
		}
	}()
	return s
}

// all returns every event the stream has delivered.
func (s *stream) all() []ev {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *stream) named(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.names[name]
}

// until returns run's events once one ends it, or fails after d.
func (s *stream) until(t *testing.T, run string, d time.Duration) []ev {
	t.Helper()
	deadline := time.After(d)
	for {
		s.mu.Lock()
		var out []ev
		for _, e := range s.events {
			if e.Run == run {
				out = append(out, e)
			}
		}
		s.mu.Unlock()
		if n := len(out); n > 0 && terminal(out[n-1].Type) {
			return out
		}
		select {
		case <-s.wake:
		case <-time.After(250 * time.Millisecond):
		case <-deadline:
			t.Fatalf("run %s did not end within %v; streamed %v", run, d, types(out))
		}
	}
}

func eventually(t *testing.T, d time.Duration, what string, ok func() bool) {
	t.Helper()
	for deadline := time.Now().Add(d); !ok(); time.Sleep(100 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("waited %v for %s", d, what)
		}
	}
}

func freePort(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

// freshDatabase creates a database for this test on admin's server and
// drops it when the test ends.
func freshDatabase(t *testing.T, admin string) string {
	t.Helper()
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()
	name := fmt.Sprintf("humagen_e2e_%d", time.Now().UnixNano())
	if _, err := conn.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c, err := pgx.Connect(ctx, admin); err == nil {
			_, _ = c.Exec(ctx, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = c.Close(ctx)
		}
	})
	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	return u.String()
}

func humagenBinary(t *testing.T) string {
	t.Helper()
	if bin := os.Getenv(BinEnv); bin != "" {
		return bin
	}
	bin := filepath.Join(t.TempDir(), "humagen")
	cmd := exec.Command("go", "build", "-o", bin, "github.com/urmzd/humagen/cmd/humagen")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return bin
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}
