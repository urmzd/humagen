package platform

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/mcptools"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

type selectedQueryProvider struct {
	allCalls, selectedCalls int
	selected                []string
	docs                    []knowledge.ImportDocument
	err                     error
	afterFetch              func()
}

func (p *selectedQueryProvider) Fetch(context.Context, string, mcptools.Secret) ([]knowledge.ImportDocument, error) {
	p.allCalls++
	return []knowledge.ImportDocument{{ID: "unselected", Title: "Outside collection", Text: "release unselected canary"}}, nil
}

func (*selectedQueryProvider) Resources(context.Context, string, mcptools.Secret) ([]knowledge.Resource, error) {
	return nil, nil
}

func (p *selectedQueryProvider) FetchSelected(_ context.Context, origin string, ids []string, secret mcptools.Secret) ([]knowledge.ImportDocument, error) {
	p.selectedCalls++
	p.selected = slices.Clone(ids)
	if origin != "folder" || secret.Empty() {
		return nil, knowledge.ProviderError{Cause: knowledge.AuthExpired}
	}
	if p.afterFetch != nil {
		p.afterFetch()
	}
	return p.docs, p.err
}

type sourceQueryFixture struct {
	p   *Platform
	who auth.Principal
	req runtime.Request
	src knowledge.Source
}

func newSourceQueryFixture(t *testing.T, mode knowledge.Mode, provider knowledge.Provider) sourceQueryFixture {
	t.Helper()
	p := sourceGroundingPlatform(t)
	p.cfg.Ext.SourceProviders = map[string]knowledge.Provider{"test": provider}
	p.cfg.Ext.Resolver = mcptools.ResolverFunc(func(context.Context, string) (mcptools.Secret, error) {
		return mcptools.NewSecret("fixture-only"), nil
	})
	p.cfg.CredentialRefs = map[string][]string{"tenant": {"op://People/{subject}/"}}
	p.Credentials = credentials.New(nil)
	p.Threads = threads.NewStore()
	if _, err := p.Knowledge.Create("tenant", "external", "", "owner", p.now()); err != nil {
		t.Fatal(err)
	}
	cfg := knowledge.SourceConfig{Mode: mode, Provider: "test", Owner: "owner", Project: "Secret", Origin: "folder"}
	if mode != knowledge.Live {
		cfg.Resources = []string{"selected"}
	}
	src, err := p.Knowledge.Configure("tenant", "external", cfg, p.now())
	if err != nil {
		t.Fatal(err)
	}
	if mode != knowledge.Live {
		id, err := p.Knowledge.BeginSync("tenant", "external", p.now())
		if err != nil {
			t.Fatal(err)
		}
		src, err = p.Knowledge.CompleteSync("tenant", "external", id, []knowledge.ImportDocument{{ID: "selected", Title: "Retained", Text: "release cached canary"}}, "owner", p.now())
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := p.Credentials.Put("tenant", "owner", sourceCredentialID(src), credentials.Input{Ref: "op://People/owner/source"}, p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Threads.Create("tenant", threads.Thread{ID: "th-0000000000000001", Project: "Secret", Channel: "general", Visibility: threads.Private, CreatedBy: "owner", Participants: []threads.Participant{{Kind: threads.Person, Name: "owner", Owner: true}}}, p.now()); err != nil {
		t.Fatal(err)
	}
	tag := envelope.Tag{ID: "tool", Origin: runs.OriginAPI, Tenant: "tenant", Agent: "worker", Actor: envelope.Actor{ID: "owner", User: "owner"}, At: p.now(), Meta: map[string]string{runs.MetaProject: "Secret", runs.MetaThread: "th-0000000000000001"}}
	if err := p.Runs.Record("query-run", tag, 1); err != nil {
		t.Fatal(err)
	}
	return sourceQueryFixture{
		p: p, src: src,
		who: auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}},
		req: runtime.Request{Tenant: "tenant", RunID: "query-run", Revision: agentrt.Revision{Spec: agentrt.Spec{Name: "worker", Sources: []agentrt.Source{{Name: "external"}}}}},
	}
}

func (f sourceQueryFixture) tool(t *testing.T) (string, error) {
	t.Helper()
	return f.p.sourceTools(f.req)[ToolSourceSearch].Execute(t.Context(), map[string]any{"source": "external", "query": "release"})
}

func TestExternalSourceQueriesUseExactSelectedResources(t *testing.T) {
	for _, mode := range []knowledge.Mode{knowledge.Indexed, knowledge.Hybrid} {
		t.Run(string(mode), func(t *testing.T) {
			provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Selected", Text: "release fresh selected"}}}
			f := newSourceQueryFixture(t, mode, provider)
			out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", "")
			if err != nil || len(out.Hits) != 1 || out.Hits[0].DocumentID != "selected" || out.Hits[0].Mode != knowledge.Live || strings.Contains(out.Hits[0].Text, "cached") || provider.allCalls != 0 || !slices.Equal(provider.selected, []string{"selected"}) {
				t.Fatalf("selection widened or cached data returned: %+v %v", out, err)
			}
			text, err := f.tool(t)
			if err != nil || !strings.Contains(text, "fresh selected") || strings.Contains(text, "canary") || provider.allCalls != 0 || provider.selectedCalls != 2 {
				t.Fatal("runtime tool did not preserve collection selection", text, err)
			}
			provider.docs = append(provider.docs, knowledge.ImportDocument{ID: "unselected", Title: "Unselected", Text: "release outside canary"})
			out, err = f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", "")
			if err != nil || len(out.Hits) != 0 || len(out.Warnings) != 1 || out.Warnings[0].Cause != knowledge.Unsupported {
				t.Fatal("provider returned data outside selection", out, err)
			}
			if text, err := f.tool(t); text != "" || knowledge.FailureCause(err) != knowledge.Unsupported {
				t.Fatal("runtime tool returned data outside selection", text, err)
			}
		})
	}
}

func TestSelectedSourceWithoutProviderSupportCannotWidenToOrigin(t *testing.T) {
	provider := &citationProvider{}
	f := newSourceQueryFixture(t, knowledge.Indexed, provider)
	out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", "")
	if err != nil || len(out.Hits) != 0 || len(out.Warnings) != 1 || out.Warnings[0].Cause != knowledge.Unsupported || provider.calls != 0 {
		t.Fatal("selected source fell back to full-origin fetch", out, err)
	}
	if text, err := f.tool(t); text != "" || knowledge.FailureCause(err) != knowledge.Unsupported || provider.calls != 0 {
		t.Fatal("runtime selected source fell back to full origin", text, err)
	}
}

func TestExternalSourceVariantsAndSnapshotsAreExplicitlyRefused(t *testing.T) {
	provider := &selectedQueryProvider{}
	f := newSourceQueryFixture(t, knowledge.Indexed, provider)
	if out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", "saved"); !errors.Is(err, knowledge.ErrInvalid) || len(out.Hits) != 0 || !strings.Contains(err.Error(), "saved variants") || provider.selectedCalls != 0 {
		t.Fatal("saved variant silently ignored", out, err)
	}
	cfg := f.src.Lifecycle.SourceConfig
	cfg.Snapshot = true
	if _, err := f.p.Knowledge.Configure("tenant", "external", cfg, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", ""); !errors.Is(err, knowledge.ErrInvalid) || len(out.Hits) != 0 || !strings.Contains(err.Error(), "frozen external snapshots") || provider.selectedCalls != 0 {
		t.Fatal("snapshot silently became a current query", out, err)
	}
	if text, err := f.tool(t); text != "" || !errors.Is(err, knowledge.ErrInvalid) || !strings.Contains(err.Error(), "frozen external snapshots") || provider.selectedCalls != 0 {
		t.Fatal("runtime snapshot silently became a current query", text, err)
	}
	docs, err := f.p.Knowledge.Documents("tenant", "external")
	if err != nil || len(docs) != 1 || docs[0].Title != "Retained" {
		t.Fatal("refusal erased retained snapshot", docs, err)
	}
}

func TestExternalSourceProviderFailureNeverFallsBackToRetainedCopy(t *testing.T) {
	for _, cause := range []knowledge.Cause{knowledge.AuthExpired, knowledge.Unavailable, knowledge.Quota} {
		t.Run(string(cause), func(t *testing.T) {
			provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Partial", Text: "release partial canary"}}, err: knowledge.ProviderError{Cause: cause}}
			f := newSourceQueryFixture(t, knowledge.Hybrid, provider)
			out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "release", "")
			if err != nil || len(out.Hits) != 0 || len(out.Warnings) != 1 || out.Warnings[0].Cause != cause {
				t.Fatal("provider failure returned partial or retained data", out, err)
			}
			if text, err := f.tool(t); text != "" || knowledge.FailureCause(err) != cause {
				t.Fatal("runtime provider failure returned partial or retained data", text, err)
			}
		})
	}
}

func TestGlobalSourceSearchWarnsForFrozenCopyAndRetainsOtherResults(t *testing.T) {
	provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Current", Text: "zebra unversioned canary"}}}
	f := newSourceQueryFixture(t, knowledge.Indexed, provider)
	cfg := f.src.Lifecycle.SourceConfig
	cfg.Snapshot = true
	if _, err := f.p.Knowledge.Configure("tenant", "external", cfg, f.p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.Knowledge.Add("tenant", "private", "Local", "", "zebra approved local document", "owner", f.p.now()); err != nil {
		t.Fatal(err)
	}
	out, err := f.p.SearchSourcesDetailed(t.Context(), f.who, nil, "zebra", "")
	if err != nil || len(out.Hits) != 1 || out.Hits[0].Source != "private" || out.Hits[0].Title != "Local" || len(out.Warnings) != 1 || out.Warnings[0].Source != "external" || out.Warnings[0].Cause != knowledge.Unsupported {
		t.Fatal("unrelated frozen copy blocked global results or disappeared without a warning", out, err)
	}
	if provider.allCalls != 0 || provider.selectedCalls != 0 {
		t.Fatal("global search queried a newer version of the frozen copy")
	}
	if direct, err := f.p.SearchSourcesDetailed(t.Context(), f.who, []string{"external"}, "zebra", ""); !errors.Is(err, knowledge.ErrInvalid) || len(direct.Hits) != 0 || provider.selectedCalls != 0 {
		t.Fatal("named frozen copy query no longer fails closed", direct, err)
	}
}

func TestExternalSourceQueryRejectsChangesWhileFetching(t *testing.T) {
	for _, change := range []string{"credential", "selection", "project-membership"} {
		t.Run(change, func(t *testing.T) {
			provider := &selectedQueryProvider{docs: []knowledge.ImportDocument{{ID: "selected", Title: "Selected", Text: "release fresh canary"}}}
			f := newSourceQueryFixture(t, knowledge.Indexed, provider)
			provider.afterFetch = func() {
				switch change {
				case "credential":
					if err := f.p.Credentials.Delete("tenant", "owner", sourceCredentialID(f.src)); err != nil {
						t.Fatal(err)
					}
				case "selection":
					cfg := f.src.Lifecycle.SourceConfig
					cfg.Resources = []string{"different"}
					if _, err := f.p.Knowledge.Configure("tenant", "external", cfg, f.p.now()); err != nil {
						t.Fatal(err)
					}
				case "project-membership":
					project, err := f.p.Projects.Get("tenant", "Secret")
					if err != nil {
						t.Fatal(err)
					}
					if _, err := f.p.Projects.Update("tenant", "Secret", project.Version, projects.Writer{By: "owner", Govern: true}, time.Now(), func(project *projects.Project) error {
						project.Members = []projects.Entry{{Who: "different", Role: projects.Owner}}
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
			}
			text, err := f.tool(t)
			if text != "" || err == nil {
				t.Fatal("changed source authority returned in-flight content", text, err)
			}
			if change == "credential" && knowledge.FailureCause(err) != knowledge.AuthExpired {
				t.Fatal("credential revocation did not fail authentication", err)
			}
			if change != "credential" && !errors.Is(err, knowledge.ErrNotFound) {
				t.Fatal("changed source scope did not disappear", err)
			}
		})
	}
}

func TestLocalManualVariantSearchStillUsesSavedView(t *testing.T) {
	p := sourceGroundingPlatform(t)
	first, err := p.Knowledge.Add("tenant", "private", "First", "", "release excluded", "owner", p.now())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.Add("tenant", "private", "Second", "", "release included", "owner", p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.SetExcluded("tenant", "private", first.ID, true, p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.SaveVariant("tenant", "private", "reviewed", p.now()); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Knowledge.SetExcluded("tenant", "private", first.ID, false, p.now()); err != nil {
		t.Fatal(err)
	}
	who := auth.Principal{Tenant: "tenant", Subject: "owner", Kind: auth.User, Scopes: []auth.Permission{auth.KnowledgeRead}}
	out, err := p.SearchSourcesDetailed(t.Context(), who, []string{"private"}, "release", "reviewed")
	if err != nil || len(out.Hits) != 1 || out.Hits[0].Title != "Second" || out.Hits[0].Mode != knowledge.Indexed {
		t.Fatal("local saved view changed", out, err)
	}
	data, _ := json.Marshal(out)
	if strings.Contains(string(data), "excluded") {
		t.Fatal("excluded local content returned", string(data))
	}
}
