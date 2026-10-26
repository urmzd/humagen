package platform_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/credref"
	"github.com/urmzd/humagen/pkg/nativefiles"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/agenttest"
	"github.com/urmzd/saige/agent/provider/anthropic"
	saigetypes "github.com/urmzd/saige/agent/types"
)

// officeText reads text from the rendered Office part, independently of the
// embedded revision model. It fails if the expected part or valid XML is absent.
func officeText(t *testing.T, data []byte, part string) []string {
	t.Helper()
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range z.File {
		if f.Name == part {
			r, err := f.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer r.Close()
			d := xml.NewDecoder(r)
			var text []string
			for {
				tok, err := d.Token()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				if e, ok := tok.(xml.StartElement); ok && e.Name.Local == "t" {
					var s string
					if err := d.DecodeElement(&s, &e); err != nil {
						t.Fatal(err)
					}
					text = append(text, s)
				}
			}
			return text
		}
	}
	t.Fatalf("missing Office part %s", part)
	return nil
}

func TestNativeAgentOfficeRevisionAcceptance(t *testing.T) {
	cases := []struct {
		ext, original, revised, part string
		first, second                []string
	}{
		{"xlsx", `{"rows":[["Item","Status"],["Launch","Pending"]]}`, `{"rows":[["Item","Status"],["Launch","Approved"]]}`, "xl/worksheets/sheet1.xml", []string{"Item", "Status", "Launch", "Pending"}, []string{"Item", "Status", "Launch", "Approved"}},
		{"pptx", `{"slides":[{"title":"Launch","body":["Status: Pending"]}]}`, `{"slides":[{"title":"Launch","body":["Status: Approved"]}]}`, "ppt/slides/slide1.xml", []string{"Launch", "Status: Pending"}, []string{"Launch", "Status: Approved"}},
	}
	for _, tc := range cases {
		t.Run(tc.ext, func(t *testing.T) {
			name := "launch." + tc.ext
			c := &cast{continues: true, shape: map[string]func(*agentspec.AgentSpec){"writer": func(s *agentspec.AgentSpec) { s.Approvals = []string{platform.ToolPublish} }}, says: map[string][][]saigetypes.Delta{"writer": {
				agenttest.ToolCallResponse("write1", platform.ToolFileWrite, map[string]any{"path": name, "format": "native", "content": tc.original}),
				agenttest.ToolCallResponse("publish1", platform.ToolPublish, map[string]any{"path": name}),
				agenttest.ToolCallResponse("read", platform.ToolFileRead, map[string]any{"path": name}),
				agenttest.ToolCallResponse("write2", platform.ToolFileWrite, map[string]any{"path": name, "format": "native", "content": tc.revised}),
				agenttest.ToolCallResponse("publish2", platform.ToolPublish, map[string]any{"path": name}), say("Done."),
			}}, tools: map[string][]string{"writer": platform.FileTools()}}
			p := stage(t, c, platform.Config{})
			run, err := p.Converse(t.Context(), tenant, platform.Turn{ID: "native-acceptance-" + tc.ext, Agent: "writer", Text: "Create a launch file with pending status, then change status to approved and publish both versions."})
			if err != nil {
				t.Fatal(err)
			}
			parked := await(t, p, run.ID, runs.Parked)
			if parked.Question == nil || parked.Question.Tool == nil || parked.Question.Tool.Name != platform.ToolPublish {
				t.Fatal("first publication did not require approval")
			}
			if files, _, _ := p.Artifacts.Page(tenant, artifacts.Query{Thread: run.Thread}); len(files) != 0 {
				t.Fatal("published before approval")
			}
			firstQuestion := parked.Question.ID
			if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, firstQuestion, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
				t.Fatal(err)
			}
			within(t, "second publication approval", func() bool {
				parked, _ = p.Runs.Get(tenant, run.ID)
				return parked.Status == runs.Parked && parked.Question != nil && parked.Question.ID != firstQuestion
			})
			before, _, err := p.Artifacts.Page(tenant, artifacts.Query{Thread: run.Thread})
			if err != nil || len(before) != 1 || before[0].Version != 1 {
				t.Fatalf("first publication: %+v, %v", before, err)
			}
			_, originalBytes, err := p.OpenArtifact(t.Context(), admin, before[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.AnswerQuestion(t.Context(), olu, run.ID, parked.Question.ID, platform.QuestionAnswer{Action: platform.ActApprove}); err != nil {
				t.Fatal(err)
			}
			await(t, p, run.ID, runs.Completed)
			files, _, err := p.Artifacts.Page(tenant, artifacts.Query{Thread: run.Thread})
			if err != nil || len(files) != 1 {
				t.Fatalf("published files: %+v, %v", files, err)
			}
			versions, err := p.Artifacts.Versions(tenant, files[0].ID)
			if err != nil || len(versions) != 2 {
				t.Fatalf("versions: %+v, %v", versions, err)
			}
			var original, revised artifacts.Artifact
			for _, v := range versions {
				if v.Version == 1 {
					original = v
				}
				if v.Version == 2 {
					revised = v
				}
			}
			if original.ID == "" || revised.ID == "" || original.ID == revised.ID || original.SHA256 == revised.SHA256 {
				t.Fatalf("invalid versions: %+v", versions)
			}
			_, a, err := p.OpenArtifact(t.Context(), admin, original.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(a, originalBytes) {
				t.Fatal("original artifact changed during revision")
			}
			_, b, err := p.OpenArtifact(t.Context(), admin, revised.ID)
			if err != nil {
				t.Fatal(err)
			}
			for i, item := range []struct {
				data   []byte
				want   []string
				absent string
			}{{a, tc.first, "Approved"}, {b, tc.second, "Pending"}} {
				if got := officeText(t, item.data, tc.part); !reflect.DeepEqual(got, item.want) {
					t.Fatalf("version %d actual part text = %q, want %q", i+1, got, item.want)
				}
				pdf, err := nativefiles.ExportPDF(name, item.data)
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte(item.want[len(item.want)-1])) || bytes.Contains(pdf, []byte(item.absent)) {
					t.Fatalf("version %d PDF missing requested content", i+1)
				}
			}
			_, again, err := p.OpenArtifact(t.Context(), admin, original.ID)
			if err != nil || !bytes.Equal(a, again) {
				t.Fatal("original artifact changed")
			}
			if _, _, err := p.OpenArtifact(t.Context(), stranger, original.ID); err == nil {
				t.Fatal("foreign tenant opened artifact")
			}
			sent := strings.Join(c.requests("writer"), " ")
			if !strings.Contains(sent, "Pending") {
				t.Fatal(fmt.Sprintf("agent did not receive original revision from file_read"))
			}
		})
	}
}

// TestLiveNativeOfficeAcceptance is opt-in because it makes paid external model
// calls. The provider credential is resolved at use, never included in output.
func TestLiveNativeOfficeAcceptance(t *testing.T) {
	if os.Getenv("HUMAGEN_LIVE_OFFICE_TEST") != "1" {
		t.Skip("set HUMAGEN_LIVE_OFFICE_TEST=1 to enable real-model acceptance")
	}
	model, ref := os.Getenv("HUMAGEN_LIVE_OFFICE_MODEL"), os.Getenv("HUMAGEN_LIVE_OFFICE_KEY_REF")
	if model == "" || ref == "" {
		t.Fatal("set HUMAGEN_LIVE_OFFICE_MODEL and HUMAGEN_LIVE_OFFICE_KEY_REF")
	}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), Ext: platform.Extensions{Providers: map[string]agentrt.ProviderFunc{"live-office": func(s agentrt.Spec) (saigetypes.Provider, error) {
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
	_, err = p.Agents.Create(ctx, tenant, agentspec.AgentSpec{Name: "live-writer", Provider: "live-office", Model: model, SystemPrompt: "Follow the user's file instructions exactly. Use the native file tools sequentially.", Tools: platform.FileTools()}, "test")
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
