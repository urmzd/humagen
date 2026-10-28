//go:build live_integration

package platform_test

import (
	"bytes"
	"context"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/urmzd/humagen/fiber/pkg/turn/native"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/nativefiles"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/provider/anthropic"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// TestLiveNativeOfficeAcceptance is opt-in because it makes paid external model
// calls. The provider credential is resolved at use, never included in output.
func TestLiveNativeOfficeAcceptance(t *testing.T) {
	if os.Getenv("HUMAGEN_LIVE_OFFICE_TEST") != "1" {
		t.Fatal("set HUMAGEN_LIVE_OFFICE_TEST=1 to enable real-model acceptance")
	}
	model, ref := os.Getenv("HUMAGEN_LIVE_OFFICE_MODEL"), os.Getenv("HUMAGEN_LIVE_OFFICE_KEY_REF")
	if model == "" || ref == "" {
		t.Fatal("set HUMAGEN_LIVE_OFFICE_MODEL and HUMAGEN_LIVE_OFFICE_KEY_REF")
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Ext: platform.Extensions{Providers: map[string]native.ProviderFunc{"live-office": func(s native.Spec) (saigetypes.Provider, error) {
		key, err := (credref.Resolver{}).Resolve(context.Background(), ref)
		if err != nil {
			return nil, err
		}
		return anthropic.New(anthropic.Config{APIKey: key, Model: saigetypes.ModelID(s.Model)})
	}}}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	_, err = p.Agents.Create(ctx, tenant, wire.AgentSpec{Name: "live-writer", Provider: "live-office", Model: model, SystemPrompt: "Follow the user's file instructions exactly. Use the native file tools sequentially.", Tools: platform.FileTools()}, "test")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		ext, part     string
		first, second []string
	}{
		{"xlsx", "xl/worksheets/sheet1.xml", []string{"Item", "Status", "Launch", "Pending"}, []string{"Item", "Status", "Launch", "Approved"}},
		{"pptx", "ppt/slides/slide1.xml", []string{"Launch", "Status: Pending"}, []string{"Launch", "Status: Approved"}},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			name := "launch." + tc.ext
			prompt := "Create " + name + " using native format. "
			if tc.ext == "xlsx" {
				prompt += "Make exactly two rows: Item, Status; Launch, Pending. "
			} else {
				prompt += "Make exactly one slide titled Launch with one body line Status: Pending. "
			}
			prompt += "Publish it. Then read the file, change Pending to Approved, write the revised file with the same name and publish it again. Wait for each tool to finish before the next."
			run, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "live-office-" + tc.ext, Agent: "live-writer", Text: prompt})
			if err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Minute)
			for {
				current, getErr := p.Runs.Get(tenant, run.ID)
				if getErr != nil {
					t.Fatal(getErr)
				}
				if current.Status == runs.Completed {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("live run did not complete: %s", current.Status)
				}
				time.Sleep(200 * time.Millisecond)
			}
			files, _, err := p.Artifacts.Page(tenant, artifacts.Query{Thread: run.Thread})
			if err != nil || len(files) != 1 {
				t.Fatalf("expected one published file: %v", err)
			}
			versions, err := p.Artifacts.Versions(tenant, files[0].ID)
			if err != nil || len(versions) != 2 {
				t.Fatalf("expected two versions: %v", err)
			}
			for _, v := range versions {
				_, data, err := p.OpenArtifact(t.Context(), admin, v.ID)
				if err != nil {
					t.Fatal(err)
				}
				want := tc.first
				if v.Version == 2 {
					want = tc.second
				} else if v.Version != 1 {
					t.Fatalf("unexpected version %d", v.Version)
				}
				if got := officeText(t, data, tc.part); !reflect.DeepEqual(got, want) {
					t.Fatalf("version %d Office content = %q, want %q", v.Version, got, want)
				}
				pdf, err := nativefiles.ExportPDF(name, data)
				if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte(want[len(want)-1])) {
					t.Fatalf("version %d PDF export failed: %v", v.Version, err)
				}
			}
		})
	}
}
