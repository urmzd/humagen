package nativefiles

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// PDF exports the same revision content as actual PDF pages. The built-in
// Helvetica font uses Windows-1252; unsupported characters are refused so a
// download never silently drops or replaces authored text.
func pdfDocument(d Document) ([]byte, error) {
	if len(d.Paragraphs) == 0 && len(d.Slides) == 0 && len(d.Rows) == 0 {
		return nil, errors.New("native PDF: require paragraphs, slides or rows")
	}
	var pages [][]string
	if len(d.Slides) > 0 {
		for _, slide := range d.Slides {
			lines := []string{slide.Title}
			lines = append(lines, slide.Body...)
			if slide.Notes != "" {
				lines = append(lines, "Notes: "+slide.Notes)
			}
			lines = append(lines, slide.Sources...)
			lines = append(lines, d.Sources...)
			pages = append(pages, wrap(lines))
		}
	} else {
		lines := []string{d.Title}
		lines = append(lines, d.Paragraphs...)
		for _, row := range d.Rows {
			lines = append(lines, strings.Join(row, " | "))
		}
		lines = append(lines, d.Sources...)
		wrapped := wrap(lines)
		for len(wrapped) > 0 {
			n := min(len(wrapped), 48)
			pages = append(pages, wrapped[:n])
			wrapped = wrapped[n:]
		}
	}
	if len(pages) == 0 || len(pages) > 100 {
		return nil, errors.New("native PDF: require 1 to 100 pages")
	}
	for _, page := range pages {
		if len(page) > 48 {
			return nil, errors.New("native PDF: a slide exceeds 48 lines; shorten it or use PPTX")
		}
	}
	objects := []string{"", "", `<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>`}
	kids := ""
	for i, page := range pages {
		pageID := 4 + i*2
		contentID := pageID + 1
		kids += fmt.Sprintf("%d 0 R ", pageID)
		var text strings.Builder
		text.WriteString("BT\n/F1 12 Tf\n50 790 Td\n15 TL\n")
		for _, line := range page {
			encoded, err := charmap.Windows1252.NewEncoder().String(line)
			if err != nil {
				return nil, errors.New("native PDF: text contains characters outside Windows-1252; use DOCX or PPTX to retain all characters")
			}
			escaped := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`, "\r", " ", "\n", " ").Replace(encoded)
			text.WriteString("(" + escaped + ") Tj\nT*\n")
		}
		text.WriteString("ET\n")
		objects = append(objects, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 3 0 R >> >> /Contents %d 0 R >>", contentID), fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", text.Len(), text.String()))
	}
	objects[0] = "<< /Type /Catalog /Pages 2 0 R >>"
	objects[1] = fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(pages), kids)
	var out bytes.Buffer
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := []int{0}
	for i, obj := range objects {
		offsets = append(offsets, out.Len())
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	model, _ := json.Marshal(d)
	fmt.Fprintf(&out, "%%HUMAGEN-REVISION %s\n", base64.StdEncoding.EncodeToString(model))
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets))
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&out, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets), xref)
	return out.Bytes(), nil
}

func wrap(paragraphs []string) []string {
	var lines []string
	for _, paragraph := range paragraphs {
		for _, line := range strings.Split(strings.ReplaceAll(paragraph, "\r\n", "\n"), "\n") {
			chars := []rune(line)
			for len(chars) > 88 {
				cut := 88
				for cut > 40 && chars[cut] != ' ' {
					cut--
				}
				if cut == 40 {
					cut = 88
				}
				lines = append(lines, string(chars[:cut]))
				chars = chars[cut:]
				if len(chars) > 0 && chars[0] == ' ' {
					chars = chars[1:]
				}
			}
			lines = append(lines, string(chars))
		}
	}
	return lines
}

// ExportPDF renders only the parts visible in the selected native format;
// the retained revision model can also carry unused data for another format.
func ExportPDF(name string, data []byte) ([]byte, error) {
	raw, err := Decode(data)
	if err != nil {
		return nil, err
	}
	model, err := parse(raw)
	if err != nil {
		return nil, err
	}
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		model.Slides = nil
		model.Rows = nil
	case ".pptx":
		model.Paragraphs = nil
		model.Rows = nil
	case ".xlsx":
		model.Paragraphs = nil
		model.Slides = nil
	case ".pdf":
	default:
		return nil, errors.New("native PDF: no export for this file type")
	}
	return pdfDocument(model)
}
