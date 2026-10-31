package platform_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"reflect"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/nativefiles"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/saige/agent/agenttest"
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
			c := &cast{continues: true, shape: map[string]func(*wire.AgentSpec){"writer": func(s *wire.AgentSpec) { s.Approvals = []string{platform.ToolPublish} }}, says: map[string][][]saigetypes.Delta{"writer": {
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
				logEvents(t, p, run.ID)
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
			originalModel, err := nativefiles.Decode(a)
			if err != nil {
				t.Fatal(err)
			}
			sawRead := false
			for _, request := range modelFor(t, c, "writer").Requests() {
				for _, message := range request.Messages {
					for _, result := range saigetypes.Each[saigetypes.ToolResultPart](message) {
						if result.CallID == "read" && !result.IsError && result.Text() == originalModel {
							sawRead = true
						}
					}
				}
			}
			if !sawRead {
				t.Fatal("agent did not receive the original native revision as the file_read result")
			}
		})
	}
}
