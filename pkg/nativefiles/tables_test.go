package nativefiles_test

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/dslipak/pdf"
	"github.com/urmzd/humagen/pkg/nativefiles"
)

func TestDOCXRowsAreRealEditableTableCellsAndSurvivePDFExport(t *testing.T) {
	data, err := nativefiles.Encode("report.docx", `{"title":"Report","paragraphs":["A real table follows."],"rows":[["Check","Result"],["A & B","Completed"]],"sources":["Public source"]}`)
	if err != nil {
		t.Fatal(err)
	}
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	f, err := z.Open("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	var doc struct {
		Body struct {
			Tables []struct {
				Rows []struct {
					Cells []struct {
						Paragraphs []struct {
							Runs []struct {
								Text string `xml:"t"`
							} `xml:"r"`
						} `xml:"p"`
					} `xml:"tc"`
				} `xml:"tr"`
			} `xml:"tbl"`
		} `xml:"body"`
	}
	if err := xml.NewDecoder(f).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Body.Tables) != 1 || len(doc.Body.Tables[0].Rows) != 2 || len(doc.Body.Tables[0].Rows[1].Cells) != 2 {
		t.Fatalf("real OpenXML tables missing: %+v", doc)
	}
	if got := doc.Body.Tables[0].Rows[1].Cells[0].Paragraphs[0].Runs[0].Text; got != "A & B" {
		t.Fatalf("cell changed: %q", got)
	}
	output, err := nativefiles.ExportPDF("report.docx", data)
	if err != nil {
		t.Fatal(err)
	}
	p, err := pdf.NewReader(bytes.NewReader(output), int64(len(output)))
	if err != nil {
		t.Fatal(err)
	}
	plain, err := p.GetPlainText()
	if err != nil {
		t.Fatal(err)
	}
	text, _ := io.ReadAll(plain)
	if !strings.Contains(string(text), "A & B") || !strings.Contains(string(text), "Completed") {
		t.Fatalf("PDF lost table: %q", text)
	}
}

func TestNativeFormatsRefuseContentTheyCannotRender(t *testing.T) {
	for _, item := range []struct{ name, raw string }{
		{"x.docx", `{"paragraphs":["Visible"],"slides":[{"title":"Hidden","body":[]}]}`},
		{"x.pptx", `{"slides":[{"title":"Visible","body":[]}],"rows":[["Hidden"]]}`},
		{"x.pptx", `{"slides":[{"title":"Visible","body":[]}],"paragraphs":["Hidden"]}`},
		{"x.xlsx", `{"rows":[["Visible"]],"paragraphs":["Hidden"]}`},
		{"x.xlsx", `{"rows":[["Visible"]],"title":"Hidden"}`},
		{"x.xlsx", `{"rows":[["Visible"]],"sources":["Hidden"]}`},
		{"x.pdf", `{"slides":[{"title":"Visible","body":[]}],"rows":[["Hidden"]]}`},
	} {
		if _, err := nativefiles.Encode(item.name, item.raw); err == nil {
			t.Errorf("accepted unrendered content in %s", item.name)
		}
	}
}

func TestNativeTitleIsAuthoredAndMismatchedContainerFailsClosed(t *testing.T) {
	data, err := nativefiles.Encode("deck.pptx", `{"title":"Deck-wide title","slides":[{"title":"First slide","body":["Body"]}]}`)
	if err != nil {
		t.Fatal(err)
	}
	text, err := nativefiles.Text("deck.pptx", data)
	if err != nil || !strings.Contains(text, "Deck-wide title") || !strings.Contains(text, "First slide") {
		t.Fatalf("title is hidden: %q %v", text, err)
	}
	pdfData, err := nativefiles.Encode("text.pdf", `{"paragraphs":["Visible"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if nativefiles.MIME("pretend.docx", pdfData) != "" {
		t.Fatal("PDF was accepted as a DOCX archive")
	}
}

func TestPromotionTextReadsAuthoredOfficeBytesNotHiddenRevisionFields(t *testing.T) {
	data, err := nativefiles.Encode("old.docx", `{"paragraphs":["Actually visible"]}`)
	if err != nil {
		t.Fatal(err)
	}
	z, _ := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range z.File {
		out, _ := w.Create(f.Name)
		if f.Name == "humagen/revision.json" {
			_, _ = io.WriteString(out, `{"paragraphs":["Actually visible"],"rows":[["HIDDEN ROW"]]}`)
			continue
		}
		in, _ := f.Open()
		_, _ = io.Copy(out, in)
		_ = in.Close()
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := nativefiles.Decode(buf.Bytes()); err == nil {
		t.Fatal("stale hidden revision model remained editable")
	}
	if text, err := nativefiles.Text("old.docx", buf.Bytes()); err == nil {
		t.Fatalf("promoted mismatched retained revision: %q", text)
	}
	if _, err := nativefiles.ExportPDF("old.docx", buf.Bytes()); err == nil {
		t.Fatal("export rendered a mismatched revision")
	}
}
