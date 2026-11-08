package httpapi

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/orchagent/flow/event"
)

// Old clients may retain 200 positions. A reconnect must advance past that
// cursor rather than repeatedly closing on the first unseen run.
func TestSessionStreamFullCursorResetsAndContinues(t *testing.T) {
	f := newFixture(t)
	token := f.person(t, "acme", "ana", "Ana", "humagen-admins")
	if res, out := f.do(t, nil, "POST", "/api/agents", token, helperSpec); res.StatusCode != 201 {
		t.Fatalf("agent: %d %v", res.StatusCode, out)
	}
	res, out := f.do(t, nil, "POST", "/api/sessions", token, `{"agent":"helper","text":"Before reset"}`)
	if res.StatusCode != 201 {
		t.Fatalf("session: %d %v", res.StatusCode, out)
	}
	path := "/api/sessions/" + out["id"].(string)
	cur := sessionStreamCursor{Runs: map[string]uint64{}}
	for i := 0; i < 200; i++ {
		cur.Runs[fmt.Sprintf("retained-%03d", i)] = 1
	}
	b, _ := json.Marshal(cur)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+path+"/stream", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Last-Event-ID", base64.RawURLEncoding.EncodeToString(b))
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("stream: %d", res.StatusCode)
	}
	scan := bufio.NewScanner(res.Body)
	scan.Buffer(make([]byte, 4096), 2<<20)
	kind := ""
	reset := false
	for scan.Scan() {
		line := scan.Text()
		if k, ok := strings.CutPrefix(line, "event: "); ok {
			kind = k
		}
		if line == "" && kind == "session-reset" {
			reset = true
			break
		}
	}
	if !reset {
		t.Fatalf("no reset: %v", scan.Err())
	}
	if res, out := f.do(t, nil, "POST", path+"/messages", token, `{"text":"After reset"}`); res.StatusCode != 201 {
		t.Fatalf("post: %d %v", res.StatusCode, out)
	}
	for scan.Scan() {
		line := scan.Text()
		if k, ok := strings.CutPrefix(line, "event: "); ok {
			kind = k
		}
		if data, ok := strings.CutPrefix(line, "data: "); ok && kind == "run" {
			var e platform.APIEvent
			if err := json.Unmarshal([]byte(data), &e); err != nil {
				t.Fatal(err)
			}
			if e.Type == event.TypeCompleted {
				return
			}
		}
	}
	t.Fatalf("stream did not continue: %v", scan.Err())
}
