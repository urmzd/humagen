package nativefiles_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"github.com/dslipak/pdf"
	"io"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/nativefiles"
)

func TestNativeFilesHaveEditableOfficePartsAndRoundTripTheRevisionModel(t *testing.T) {
	models := map[string]string{
		"docx": `{"title":"Quarterly & plan","paragraphs":["Keep <both>"],"rows":[["Item","Value"],["Renewal","=literal"]],"sources":["https://example.test/report"]}`,
		"pptx": `{"title":"Quarterly & plan","slides":[{"title":"Decision","body":["Ship the plan"],"notes":"Explain timing","sources":["https://example.test/report"]}]}`,
		"xlsx": `{"rows":[["Item","Value"],["Renewal","=literal"]]}`,
	}
	for _, ext := range []string{"docx", "pptx", "xlsx"} {
		t.Run(ext, func(t *testing.T) {
			data, err := nativefiles.Encode("plan."+ext, models[ext])
			if err != nil {
				t.Fatal(err)
			}
			reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			names := map[string]bool{}
			for _, file := range reader.File {
				names[file.Name] = true
				r, err := file.Open()
				if err != nil {
					t.Fatal(err)
				}
				b, err := io.ReadAll(r)
				_ = r.Close()
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(file.Name, ".xml") || strings.HasSuffix(file.Name, ".rels") {
					dec := xml.NewDecoder(bytes.NewReader(b))
					for {
						_, err := dec.Token()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatalf("invalid XML %s: %v", file.Name, err)
						}
					}
				}
			}
			for _, required := range []string{"[Content_Types].xml", "_rels/.rels", map[string]string{"docx": "word/document.xml", "pptx": "ppt/slides/slide1.xml", "xlsx": "xl/worksheets/sheet1.xml"}[ext]} {
				if !names[required] {
					t.Errorf("missing %s", required)
				}
			}
			decoded, err := nativefiles.Decode(data)
			want := map[string]string{"docx": "Keep ", "pptx": "Explain timing", "xlsx": "=literal"}[ext]
			if err != nil || !strings.Contains(decoded, want) {
				t.Fatalf("revision model: %s, %v", decoded, err)
			}
		})
	}
}

func TestNativeFilesRejectInvalidAndUnboundedInput(t *testing.T) {
	for _, item := range []struct{ name, content string }{
		{"x.exe", `{"title":"x"}`}, {"x.pptx", `{"slides":[]}`}, {"x.docx", `{"paragraphs":[]}`}, {"x.xlsx", `{"rows":[]}`}, {"x.docx", `{"paragraphs":["ok"],"unknown":true}`}, {"x.docx", `{"paragraphs":["` + strings.Repeat("x", 33000) + `"]}`},
	} {
		if _, err := nativefiles.Encode(item.name, item.content); err == nil {
			t.Errorf("accepted %s", item.name)
		}
	}
	if _, err := nativefiles.Decode([]byte("not an office document")); err == nil {
		t.Fatal("accepted invalid archive")
	}
}

func TestPDFIsIndependentlyReadableAndKeepsExactRevisionSource(t *testing.T) {
	data, err := nativefiles.Encode("plan.pdf", `{"title":"Launch plan","paragraphs":["Ship on Tuesday.","Keep (both) paths."],"sources":["https://example.test/plan"]}`)
	if err != nil {
		t.Fatal(err)
	}
	reader, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if reader.NumPage() != 1 {
		t.Fatalf("pages = %d", reader.NumPage())
	}
	plain, err := reader.GetPlainText()
	if err != nil {
		t.Fatal(err)
	}
	text, err := io.ReadAll(plain)
	if err != nil || !strings.Contains(string(text), "Ship on Tuesday.") || !strings.Contains(string(text), "Keep (both) paths.") {
		t.Fatalf("independent PDF text = %q,%v", text, err)
	}
	model, err := nativefiles.Decode(data)
	if err != nil || !strings.Contains(model, "https://example.test/plan") {
		t.Fatalf("revision source = %q,%v", model, err)
	}
	for _, raw := range []string{`{"paragraphs":["Unsupported: 😀"]}`, `{"slides":[{"title":"Too full","body":["` + strings.Repeat("long text ", 1000) + `"]}]}`} {
		if _, err := nativefiles.Encode("plan.pdf", raw); err == nil {
			t.Fatal("PDF silently replaced or cut content")
		}
	}
}

func TestPDFExportKeepsTheSelectedOfficeFormatsVisibleContent(t *testing.T) {
	raw := map[string]string{"docx": `{"paragraphs":["Word content"]}`, "pptx": `{"slides":[{"title":"PowerPoint content","body":[]}]}`, "xlsx": `{"rows":[["Excel content"]]}`}
	for _, ext := range []string{"docx", "pptx", "xlsx"} {
		native, err := nativefiles.Encode("file."+ext, raw[ext])
		if err != nil {
			t.Fatal(err)
		}
		output, err := nativefiles.ExportPDF("file."+ext, native)
		if err != nil {
			t.Fatal(err)
		}
		reader, err := pdf.NewReader(bytes.NewReader(output), int64(len(output)))
		if err != nil {
			t.Fatal(err)
		}
		plain, err := reader.GetPlainText()
		if err != nil {
			t.Fatal(err)
		}
		text, _ := io.ReadAll(plain)
		want := map[string]string{"docx": "Word content", "pptx": "PowerPoint content", "xlsx": "Excel content"}[ext]
		if !strings.Contains(string(text), want) {
			t.Fatalf("%s export lost visible content: %q", ext, text)
		}
	}
}
