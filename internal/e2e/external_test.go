package e2e

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestExternalReplicaHost(t *testing.T) {
	for _, host := range []string{"platform.withhumagen.com", "platform.withhumagen.com:8443", ""} {
		t.Run(fmt.Sprintf("host=%s", host), func(t *testing.T) {
			t.Setenv("HUMAGEN_E2E_HOST", host)
			t.Setenv("HUMAGEN_E2E_LOGIN", "operator@example.com")
			requests := make(chan string, 3)
			wantHost := host
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Host != wantHost || r.Header.Get("Tailscale-User-Login") != "operator@example.com" {
					t.Errorf("%s: Host=%q login=%q", r.URL.Path, r.Host, r.Header.Get("Tailscale-User-Login"))
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				requests <- r.URL.Path
				switch r.URL.Path {
				case "/readyz":
					w.WriteHeader(http.StatusOK)
				case "/api/runs":
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusAccepted)
					_, _ = fmt.Fprint(w, `{"id":"r1"}`)
				case "/api/events":
					w.Header().Set("Content-Type", "text/event-stream")
					_, _ = fmt.Fprint(w, "data: {\"id\":\"event-1\",\"type\":\"run.completed.v1\",\"run\":\"r1\",\"seq\":1}\n\n")
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}))
			defer server.Close()
			if wantHost == "" {
				parsed, err := url.Parse(server.URL)
				if err != nil {
					t.Fatal(err)
				}
				wantHost = parsed.Host
			}
			replica := externalReplica(t, "forwarded", server.URL+"/")
			if replica.url != server.URL {
				t.Fatalf("dial URL changed: %s", replica.url)
			}
			if status := replica.readyz(); status != http.StatusOK {
				t.Fatalf("readyz = %d", status)
			}
			var run runView
			replica.do(http.MethodPost, "/api/runs", `{"agent":"lead"}`, http.StatusAccepted, &run)
			if run.ID != "r1" {
				t.Fatal(run.ID)
			}
			events := replica.stream(t).until(t, run.ID, time.Second)
			if len(events) != 1 || events[0].ID != "event-1" {
				t.Fatalf("events = %+v", events)
			}
			for _, want := range []string{"/readyz", "/api/runs", "/api/events"} {
				if got := <-requests; got != want {
					t.Fatalf("path = %s, want %s", got, want)
				}
			}
		})
	}
}
