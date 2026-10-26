package knowledge

import (
	"context"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestSelectedDriveCopyNeverReadsUnselectedUnsupportedFiles(t *testing.T) {
	read := []string{}
	provider := HTTPProvider{Kind: "drive", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		read = append(read, r.URL.Path)
		body := `{"files":[{"id":"selected","name":"Runbook","mimeType":"text/plain"},{"id":"private","name":"Unselected","mimeType":"application/pdf"}]}`
		if strings.HasSuffix(r.URL.Path, "/selected") {
			body = "release safely"
		}
		if strings.HasSuffix(r.URL.Path, "/private") {
			t.Fatal("unselected file fetched")
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}
	resources, err := provider.Resources(t.Context(), "folder", mcptools.NewSecret("token"))
	if err != nil || len(resources) != 2 || resources[1].Supported {
		t.Fatalf("inventory %+v %v", resources, err)
	}
	docs, err := provider.FetchSelected(context.Background(), "folder", []string{"selected"}, mcptools.NewSecret("token"))
	if err != nil || len(docs) != 1 || docs[0].ID != "selected" {
		t.Fatalf("selected %+v %v", docs, err)
	}
	if _, err := provider.FetchSelected(t.Context(), "folder", []string{"missing"}, mcptools.NewSecret("token")); FailureCause(err) != AuthExpired {
		t.Fatal("missing selected file silently omitted", err)
	}
	if _, err := provider.Fetch(t.Context(), "folder", mcptools.NewSecret("token")); FailureCause(err) != Unsupported {
		t.Fatal("full copy silently skipped unsupported file", err)
	}
}
func TestSourceSelectionSnapshotAndRefreshSettingsSurviveRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = s.Create("tenant", "source", "", "owner", now)
		cfg := SourceConfig{Mode: Indexed, Provider: "drive", Origin: "folder", Owner: "owner", Resources: []string{"two", "one"}, StopAfterSeconds: 60, SyncEverySeconds: 60}
		got, err := s.Configure("tenant", "source", cfg, now)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Resources[0] = "changed"
		got.Lifecycle.Resources[0] = "changed"
		saved, _ := s.Get("tenant", "source")
		if saved.Lifecycle.Resources[0] != "one" {
			t.Fatal("caller mutated persisted resource selection")
		}
		frozen := saved.Lifecycle.SourceConfig
		frozen.Snapshot = true
		frozen.SyncEverySeconds = 0
		if _, err := s.Configure("tenant", "source", frozen, now); err == nil {
			t.Fatal("unproven copy frozen")
		}
		id, _ := s.BeginSync("tenant", "source", now)
		_, err = s.CompleteSync("tenant", "source", id, []ImportDocument{{ID: "one", Title: "Runbook", Text: "retained version"}}, "owner", now)
		if err != nil {
			t.Fatal(err)
		}
		if due := s.RefreshDue(now.Add(2*time.Minute), 200); len(due) != 1 {
			t.Fatal("scheduled copy not due", due)
		}
		if _, err := s.Configure("tenant", "source", frozen, now); err != nil {
			t.Fatal(err)
		}
		replay, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		copy, _ := replay.Get("tenant", "source")
		if !copy.Lifecycle.Snapshot || copy.HardStale(now.Add(48*time.Hour)) || len(replay.RefreshDue(now.Add(48*time.Hour), 200)) != 0 {
			t.Fatal("snapshot expired or scheduled itself")
		}
		id, _ = replay.BeginSync("tenant", "source", now.Add(time.Hour))
		_, _ = replay.FailSync("tenant", "source", id, AuthExpired, now.Add(time.Hour))
		copy, _ = replay.Get("tenant", "source")
		if copy.Documents != 1 || !copy.Lifecycle.Snapshot || copy.Lifecycle.LastGoodAt == nil {
			t.Fatal("failed replacement discarded frozen lastgood")
		}
	})
}
