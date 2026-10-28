//go:build live_integration

package knowledge

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/mcptools"
	"github.com/urmzd/humagen/pkg/credref"
)

// TestProviderLiveSmoke uses read-only, explicitly selected provider resources.
// It never prints provider content, resource identifiers, or credential errors.
func TestProviderLiveSmoke(t *testing.T) {
	enabled := os.Getenv("HUMAGEN_SOURCE_LIVE_TEST")
	if enabled == "" {
		t.Fatal("set HUMAGEN_SOURCE_LIVE_TEST to drive, notion, or both")
	}
	if enabled != "drive" && enabled != "notion" && enabled != "both" {
		t.Fatal("HUMAGEN_SOURCE_LIVE_TEST must be drive, notion, or both")
	}
	for _, kind := range []string{"drive", "notion"} {
		if enabled != "both" && enabled != kind {
			continue
		}
		t.Run(kind, func(t *testing.T) {
			prefix := "HUMAGEN_SMOKE_" + strings.ToUpper(kind) + "_"
			ref, origin, selected, marker := os.Getenv(prefix+"CREDENTIAL_REF"), os.Getenv(prefix+"ORIGIN"), os.Getenv(prefix+"RESOURCE"), os.Getenv(prefix+"MARKER")
			if ref == "" || origin == "" || selected == "" || marker == "" {
				t.Fatal("enabled provider requires CREDENTIAL_REF, ORIGIN, RESOURCE and MARKER")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			value, err := credref.Default.Resolve(ctx, ref)
			if err != nil {
				t.Fatal("credential reference could not be resolved")
			}
			secret := mcptools.NewSecret(value)
			p := HTTPProvider{Kind: kind}
			resources, err := p.Resources(ctx, origin, secret)
			if err != nil {
				t.Fatalf("discovery failed: %s", FailureCause(err))
			}
			found := false
			for _, resource := range resources {
				if resource.ID == selected && resource.Supported {
					found = true
				}
			}
			if !found {
				t.Fatal("selected supported resource was not discovered")
			}
			fetch := func(token mcptools.Secret) ([]ImportDocument, error) {
				return p.FetchSelected(ctx, origin, []string{selected}, token)
			}
			docs, err := fetch(secret)
			if err != nil {
				t.Fatalf("import failed: %s", FailureCause(err))
			}
			if len(docs) != 1 || docs[0].ID != selected || !strings.Contains(docs[0].Text, marker) || docs[0].URI == "" {
				t.Fatal("import did not return the selected document with the marker and URI")
			}
			s := NewStore()
			at := time.Now().UTC()
			if _, err := s.Create("smoke", "source", "", "tester", at); err != nil {
				t.Fatal("create source failed")
			}
			if _, err := s.Configure("smoke", "source", SourceConfig{Mode: Indexed, Provider: kind, Origin: origin, Owner: "tester", Resources: []string{selected}}, at); err != nil {
				t.Fatal("configure source failed")
			}
			sync := func(docs []ImportDocument, at time.Time) {
				t.Helper()
				id, err := s.BeginSync("smoke", "source", at)
				if err != nil {
					t.Fatal("begin sync failed")
				}
				if _, err := s.CompleteSync("smoke", "source", id, docs, "tester", at); err != nil {
					t.Fatal("complete sync failed")
				}
			}
			sync(docs, at)
			refresh, err := fetch(secret)
			if err != nil {
				t.Fatalf("refresh fetch failed: %s", FailureCause(err))
			}
			sync(refresh, at.Add(time.Second))
			before, err := s.Documents("smoke", "source")
			if err != nil || len(before) != 1 {
				t.Fatal("refreshed copy missing")
			}
			id, err := s.BeginSync("smoke", "source", at.Add(2*time.Second))
			if err != nil {
				t.Fatal("begin failure test failed")
			}
			_, err = fetch(mcptools.NewSecret("humagen-smoke-invalid-token"))
			if err == nil || FailureCause(err) != AuthExpired {
				t.Fatal("invalid credential did not produce an authentication failure")
			}
			if _, err = s.FailSync("smoke", "source", id, FailureCause(err), at.Add(2*time.Second)); err != nil {
				t.Fatal("record failure failed")
			}
			after, err := s.Documents("smoke", "source")
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("failed refresh changed retained copy")
			}
			hits, err := s.Search("smoke", []string{"source"}, marker, 6, "")
			if !errors.Is(err, ErrUpstreamRead) || len(hits) != 0 {
				t.Fatal("retained copy bypassed current upstream access")
			}
			failed, err := s.Get("smoke", "source")
			if err != nil || failed.Lifecycle == nil || failed.Lifecycle.Status != NeedsAttention || failed.Lifecycle.Cause != AuthExpired || failed.Lifecycle.LastGoodAt == nil || !failed.Lifecycle.LastGoodAt.Equal(at.Add(time.Second)) {
				t.Fatal("failed collection lost its status or last-good provenance")
			}
			// A query must authorize another provider read. Its temporary search
			// index never serves the retained collection after a failed refresh.
			query := func(token mcptools.Secret) ([]Hit, error) {
				fresh, err := fetch(token)
				if err != nil {
					return nil, err
				}
				current := NewStore()
				if _, err := current.Create("smoke", "source", "", "tester", at); err != nil {
					return nil, err
				}
				id, err := current.BeginSync("smoke", "source", at)
				if err != nil {
					return nil, err
				}
				if _, err := current.CompleteSync("smoke", "source", id, fresh, "tester", at); err != nil {
					return nil, err
				}
				hits, err := current.Search("smoke", []string{"source"}, marker, 6, "")
				for i := range hits {
					hits[i].Mode = Live
					hits[i].CopyAt = nil
					hits[i].RefreshFailed = false
				}
				return hits, err
			}
			if denied, err := query(mcptools.NewSecret("humagen-smoke-invalid-token")); err == nil || FailureCause(err) != AuthExpired || len(denied) != 0 {
				t.Fatal("query returned retained data without current provider access")
			}
			hits, err = query(secret)
			if err != nil || len(hits) != 1 {
				t.Fatal("fresh authorized marker was not searchable")
			}
			h := hits[0]
			if h.DocumentID != selected || h.URI != docs[0].URI || h.Mode != Live || h.CopyAt != nil || h.RefreshFailed || !strings.Contains(h.Text, marker) {
				t.Fatal("retrieval provenance did not identify a fresh provider query")
			}
			evidence := NewEvidenceStore()
			entry := CitationEvidence{Source: h.Source, Provider: kind, Person: "tester", DocumentID: h.DocumentID, Title: h.Title, URI: h.URI, Text: h.Text, Index: h.Index, Mode: h.Mode, CopyAt: h.CopyAt, RefreshFailed: h.RefreshFailed}
			recorded, err := evidence.RecordQuery("smoke", "run", "query", []CitationEvidence{entry})
			if err != nil || len(recorded) != 1 {
				t.Fatal("record citation evidence failed")
			}
			kept, ok := evidence.Get("smoke", "run", recorded[0].Label, selected)
			if !ok || kept.Provider != kind || kept.Person != "tester" || kept.Digest != EvidenceDigest(h.DocumentID, h.Title, h.URI, h.Text, h.Index) || kept.Mode != Live || kept.CopyAt != nil || kept.RefreshFailed {
				t.Fatal("citation evidence did not retain retrieval provenance")
			}
			recovered, err := fetch(secret)
			if err != nil {
				t.Fatalf("recovery fetch failed: %s", FailureCause(err))
			}
			sync(recovered, at.Add(3*time.Second))
			restored, err := s.Get("smoke", "source")
			if err != nil || restored.Lifecycle == nil || restored.Lifecycle.Status != InSync || restored.Lifecycle.Cause != "" || restored.Lifecycle.LastGoodAt == nil || !restored.Lifecycle.LastGoodAt.Equal(at.Add(3*time.Second)) {
				t.Fatal("successful retry did not recover the source")
			}
			if hits, err := s.Search("smoke", []string{"source"}, marker, 6, ""); !errors.Is(err, ErrUpstreamRead) || len(hits) != 0 {
				t.Fatal("successful collection became an upstream access grant")
			}
		})
	}
}
