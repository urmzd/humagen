package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/wire"

	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSourceDependenciesUseActualConfinedEvidenceAfterRestart(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		p := sourceGroundingPlatform(t)
		p.Threads = threads.NewStore()
		var err error
		p.Runs, err = runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		p.Artifacts, err = artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = p.Knowledge.Create("tenant", "public", "", "owner", p.now())
		for _, item := range []struct {
			id, title string
			private   bool
		}{{"th-0000000000000001", "Private chat", true}, {"th-0000000000000002", "Visible chat", false}} {
			visibility := threads.Shared
			if item.private {
				visibility = threads.Private
			}
			_, err = p.Threads.Create("tenant", threads.Thread{ID: item.id, Title: item.title, Project: "Secret", Channel: "general", Visibility: visibility, CreatedBy: "owner"}, p.now())
			if err != nil {
				t.Fatal(err)
			}
			tag := wire.NewEvent(wire.Raise{ID: item.id, Tenant: "tenant", Agent: "worker", Origin: runs.OriginAPI, At: p.now(), Context: map[string]string{runs.MetaThread: item.id, runs.MetaProject: "Secret"}})
			if err := p.Runs.Record(item.id, tag, 1); err != nil {
				t.Fatal(err)
			}
			_, err = p.SourceEvidence.RecordQuery("tenant", item.id, "rag:1", []knowledge.CitationEvidence{{Provider: "manual", Source: "public", DocumentID: "doc", Title: "Source", Text: "actual passage", Mode: knowledge.Indexed}})
			if err != nil {
				t.Fatal(err)
			}
			_, err = p.Artifacts.Publish("tenant", artifacts.Artifact{ID: p.Artifacts.NextID("tenant"), Title: item.title + ".txt", Key: item.id, Run: item.id, Thread: item.id, Project: "Secret", Agent: "worker", Kind: artifacts.Document, CreatedAt: p.now()})
			if err != nil {
				t.Fatal(err)
			}
		}

		// A declared source grant alone proves no retrieval or publication relation.
		unproven := wire.NewEvent(wire.Raise{ID: "unproven", Tenant: "tenant", Agent: "worker", Origin: runs.OriginAPI, At: p.now(), Context: map[string]string{runs.MetaThread: "th-0000000000000002", runs.MetaProject: "Secret", runs.MetaSources: "public"}})
		if err := p.Runs.Record("unproven", unproven, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Artifacts.Publish("tenant", artifacts.Artifact{ID: p.Artifacts.NextID("tenant"), Key: "unproven", Title: "Not a source dependency.md", Kind: artifacts.Document, Run: "unproven", Thread: "th-0000000000000002", Project: "Secret", CreatedAt: p.now()}); err != nil {
			t.Fatal(err)
		}
		p.SourceEvidence, err = knowledge.OpenEvidenceStore(st)
		if err != nil {
			t.Fatal(err)
		}
		p.Runs, err = runs.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		p.Artifacts, err = artifacts.Open(st)
		if err != nil {
			t.Fatal(err)
		}
		owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead, auth.RunsRead, auth.ArtifactsRead}}
		result, err := p.SourceDependenciesAs(t.Context(), owner, "public")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(result)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(encoded, &fields)
		var conversations, deliverables []map[string]string
		_ = json.Unmarshal(fields["conversations"], &conversations)
		_ = json.Unmarshal(fields["deliverables"], &deliverables)
		if len(conversations) != 2 || len(deliverables) != 2 {
			t.Fatalf("actual retained dependencies missing: %s", encoded)
		}

		project, _ := p.Projects.Get("tenant", "Secret")
		_, err = p.Projects.Update("tenant", "Secret", project.Version, projects.Writer{By: "owner", Govern: true}, p.now(), func(pr *projects.Project) error {
			pr.Members = append(pr.Members, projects.Entry{Who: "peer", Role: projects.Viewer})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		peer := owner
		peer.Subject = "peer"
		peerResult, err := p.SourceDependenciesAs(t.Context(), peer, "public")
		if err != nil {
			t.Fatal(err)
		}
		if len(peerResult.Conversations) != 1 || peerResult.Conversations[0].Title != "Visible chat" || len(peerResult.Deliverables) != 1 {
			t.Fatal("hidden thread names or counts exposed", peerResult)
		}
		if _, err := p.SourceDependenciesAs(t.Context(), peer, "private"); err != nil {
			t.Fatal("shared project source unexpectedly hidden", err)
		}

		runsOnly := owner
		runsOnly.Scopes = []auth.Permission{auth.KnowledgeRead, auth.RunsRead}
		noFiles, err := p.SourceDependenciesAs(t.Context(), runsOnly, "public")
		if err != nil || len(noFiles.Conversations) != 2 || len(noFiles.Deliverables) != 0 {
			t.Fatal("artifact permission boundary failed", noFiles, err)
		}
		owner.Scopes = []auth.Permission{auth.KnowledgeRead}
		denied, err := p.SourceDependenciesAs(t.Context(), owner, "public")
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ = json.Marshal(denied)
		_ = json.Unmarshal(encoded, &fields)
		if string(fields["conversations"]) != "[]" || string(fields["deliverables"]) != "[]" {
			t.Fatal("missing object permissions exposed dependencies", string(encoded))
		}
	})
}

type dependencyReaderFunc func(context.Context, auth.Principal, []string) (SourceExtraDependencies, error)

func (f dependencyReaderFunc) ReadSourceDependencies(ctx context.Context, who auth.Principal, ids []string) (SourceExtraDependencies, error) {
	return f(ctx, who, ids)
}

func TestSourceDependencyReaderRejectsUnprovenOriginAndUnsafeLinks(t *testing.T) {
	p := sourceGroundingPlatform(t)
	reader := dependencyReaderFunc(func(_ context.Context, who auth.Principal, ids []string) (SourceExtraDependencies, error) {
		if who.Subject != "owner" || len(ids) != 1 || ids[0] != "visible-run" {
			t.Fatal("wrong adapter authority or retrieval set", who, ids)
		}
		return SourceExtraDependencies{Supported: []string{"skills", "memory"}, Skills: []SourceDependency{
			{ID: "skill", Title: "Recorded skill", OriginRun: "visible-run", Relation: "recorded_origin_run", Path: "/library/skills/skill"},
			{ID: "hidden", Title: "Hidden", OriginRun: "private-run", Relation: "recorded_origin_run", Path: "/library/skills/hidden"},
			{ID: "unsafe", Title: "Unsafe", OriginRun: "visible-run", Relation: "recorded_origin_run", Path: "//example.test"},
		}}, nil
	})
	if err := p.RegisterSourceDependencyReader(reader); err != nil {
		t.Fatal(err)
	}
	if err := p.RegisterSourceDependencyReader(reader); err == nil {
		t.Fatal("silently replaced native dependency reader")
	}
	out := SourceDependencies{Skills: []SourceDependency{}, Memory: []SourceDependency{}, Unsupported: []string{"skills", "memory"}}
	if err := p.extraSourceDependencies(t.Context(), auth.Principal{Subject: "owner"}, []string{"visible-run"}, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Skills) != 1 || out.Skills[0].OriginRun != "" || len(out.Unsupported) != 0 {
		t.Fatal("unproven or private origin metadata projected", out)
	}
	raw, _ := json.Marshal(out)
	if strings.Contains(string(raw), "visible-run") || strings.Contains(string(raw), "private-run") {
		t.Fatal("private origin run escaped DTO", string(raw))
	}
	q := sourceGroundingPlatform(t)
	q.cfg.SourceDependencies = reader
	if err := q.RegisterSourceDependencyReader(reader); err == nil {
		t.Fatal("replaced configured reader")
	}
}

func TestExtraSourceGraphRequiresOrdinaryHumanSourceAccess(t *testing.T) {
	p := sourceGroundingPlatform(t)
	p.Threads = threads.NewStore()
	tag := wire.NewEvent(wire.Raise{ID: "retrieved", Tenant: "tenant", Agent: "worker", Origin: runs.OriginAPI, At: p.now(), Context: map[string]string{runs.MetaProject: "Secret"}})
	if err := p.Runs.Record("run1", tag, 1); err != nil {
		t.Fatal(err)
	}
	_, err := p.SourceEvidence.RecordQuery("tenant", "run1", "rag:1", []knowledge.CitationEvidence{{Provider: "manual", Source: "private", Project: "Secret", DocumentID: "doc", Text: "retained", Mode: knowledge.Indexed}})
	if err != nil {
		t.Fatal(err)
	}
	owner := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead, auth.RunsRead}}
	if ids, _ := p.ordinarySourceDependencyRuns(owner, "private"); len(ids) != 1 {
		t.Fatal("ordinary owner lost real origin proof")
	}
	for _, provider := range []string{"", "drive"} {
		id := "historical-" + provider
		tag := wire.NewEvent(wire.Raise{ID: id, Tenant: "tenant", Agent: "worker", Origin: runs.OriginAPI, At: p.now(), Context: map[string]string{runs.MetaProject: "Secret"}})
		if err := p.Runs.Record(id, tag, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := p.SourceEvidence.RecordQuery("tenant", id, "rag:1", []knowledge.CitationEvidence{{Source: "private", Provider: provider, Project: "Secret", DocumentID: "doc", Text: "retained", Mode: knowledge.Indexed}}); err != nil {
			t.Fatal(err)
		}
	}
	if ids, _ := p.ordinarySourceDependencyRuns(owner, "private"); len(ids) != 1 || ids[0] != "run1" {
		t.Fatal("derived learning graph exposed external or unknown file provenance", ids)
	}
	govern := owner
	govern.Subject = "govern-only"
	govern.Scopes = append(govern.Scopes, auth.ProjectsGovern)
	if ids, _ := p.ordinarySourceDependencyRuns(govern, "private"); len(ids) != 0 {
		t.Fatal("governance became a derived-origin copy grant")
	}
	machine := owner
	machine.Kind = auth.Machine
	if ids, _ := p.ordinarySourceDependencyRuns(machine, "private"); len(ids) != 0 {
		t.Fatal("machine received personal origin references")
	}
}
