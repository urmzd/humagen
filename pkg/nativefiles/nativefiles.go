// Package nativefiles produces editable Office documents from a bounded revision model.
// Files contain ordinary Office parts and retain the model for subsequent agent revisions.
package nativefiles

import (
	"archive/zip"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaxInput       = 1 << 20
	MaxItems       = 100
	MaxText        = 32000
	modelPart      = "humagen/revision.json"
	relNS          = "http://schemas.openxmlformats.org/package/2006/relationships"
	officeRel      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships/"
	drawingNS      = "http://schemas.openxmlformats.org/drawingml/2006/main"
	presentationNS = "http://schemas.openxmlformats.org/presentationml/2006/main"
	wordNS         = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	sheetNS        = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
)

// Slide keeps the editable text, speaker notes and cited sources together.
type Slide struct {
	Title   string   `json:"title"`
	Body    []string `json:"body"`
	Notes   string   `json:"notes,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

// Document is the complete native revision input. Spreadsheet cells are literal
// text, never executable formulas. A new publication creates an immutable version.
type Document struct {
	Title      string     `json:"title,omitempty"`
	Paragraphs []string   `json:"paragraphs,omitempty"`
	Slides     []Slide    `json:"slides,omitempty"`
	Rows       [][]string `json:"rows,omitempty"`
	Sources    []string   `json:"sources,omitempty"`
}

func textOK(s string) bool {
	if !utf8.ValidString(s) || len(s) > MaxText {
		return false
	}
	for _, r := range s {
		if r < 32 && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

func parse(raw string) (Document, error) {
	var d Document
	if len(raw) > MaxInput {
		return d, errors.New("native file: content exceeds 1 MB")
	}
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&d); err != nil {
		return d, fmt.Errorf("native file: invalid revision JSON: %w", err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return d, errors.New("native file: one JSON document is required")
	}
	if !textOK(d.Title) || len(d.Paragraphs) > 500 || len(d.Slides) > MaxItems || len(d.Rows) > 1000 || len(d.Sources) > MaxItems {
		return d, errors.New("native file: too many items or an invalid title")
	}
	values := append([]string{d.Title}, d.Paragraphs...)
	values = append(values, d.Sources...)
	for _, s := range d.Slides {
		if len(s.Body) > MaxItems || len(s.Sources) > MaxItems {
			return d, errors.New("native file: a slide has too many items")
		}
		values = append(values, s.Title, s.Notes)
		values = append(values, s.Body...)
		values = append(values, s.Sources...)
	}
	for _, row := range d.Rows {
		if len(row) > 100 {
			return d, errors.New("native file: a row has more than 100 cells")
		}
		values = append(values, row...)
	}
	for _, s := range values {
		if !textOK(s) {
			return d, errors.New("native file: text must be valid XML text of at most 32000 bytes")
		}
	}
	return d, nil
}

func esc(s string) string     { var b bytes.Buffer; _ = xml.EscapeText(&b, []byte(s)); return b.String() }
func xmlFile(s string) string { return `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + s }
func rel(id, kind, target string) string {
	return `<Relationship Id="` + id + `" Type="` + officeRel + kind + `" Target="` + esc(target) + `"/>`
}
func rels(items string) string {
	return xmlFile(`<Relationships xmlns="` + relNS + `">` + items + `</Relationships>`)
}
func override(name, mime string) string {
	return `<Override PartName="/` + name + `" ContentType="` + mime + `"/>`
}
func mime(ext string) string {
	return "application/vnd.openxmlformats-officedocument." + map[string]string{"docx": "wordprocessingml.document", "pptx": "presentationml.presentation", "xlsx": "spreadsheetml.sheet"}[ext]
}

// MIME returns the official media type only for a document whose native parts
// and retained model can be read. A file extension alone is never evidence.
func MIME(name string, data []byte) string {
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if ext != "docx" && ext != "pptx" && ext != "xlsx" {
		return ""
	}
	if _, err := Decode(data); err != nil {
		return ""
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return ""
	}
	required := map[string]string{"docx": "word/document.xml", "pptx": "ppt/presentation.xml", "xlsx": "xl/workbook.xml"}[ext]
	if reader, err := r.Open(required); err != nil {
		return ""
	} else {
		_ = reader.Close()
	}
	return mime(ext)
}

// Encode creates an editable document, presentation or spreadsheet, never a
// screenshot or an executable archive. The caller applies its storage limits.
func Encode(name, raw string) ([]byte, error) {
	d, err := parse(raw)
	if err != nil {
		return nil, err
	}
	ext := strings.TrimPrefix(strings.ToLower(path.Ext(name)), ".")
	if err := renderedFields(ext, d); err != nil {
		return nil, err
	}
	parts := map[string]string{}
	types := ""
	switch ext {
	case "pdf":
		return pdfDocument(d)
	case "docx":
		if len(d.Paragraphs) == 0 && len(d.Rows) == 0 {
			return nil, errors.New("native file: a document requires paragraphs or table rows")
		}
		body := ""
		for _, s := range append([]string{d.Title}, d.Paragraphs...) {
			body += wordParagraph(s)
		}
		if len(d.Rows) > 0 {
			columns := 0
			for _, row := range d.Rows {
				columns = max(columns, len(row))
			}
			if columns == 0 {
				return nil, errors.New("native file: a table needs at least one cell")
			}
			body += `<w:tbl><w:tblPr><w:tblW w:w="0" w:type="auto"/><w:tblBorders><w:top w:val="single" w:sz="4"/><w:left w:val="single" w:sz="4"/><w:bottom w:val="single" w:sz="4"/><w:right w:val="single" w:sz="4"/><w:insideH w:val="single" w:sz="4"/><w:insideV w:val="single" w:sz="4"/></w:tblBorders></w:tblPr><w:tblGrid>`
			for range columns {
				body += `<w:gridCol w:w="` + strconv.Itoa(9360/columns) + `"/>`
			}
			body += `</w:tblGrid>`
			for _, row := range d.Rows {
				body += `<w:tr>`
				for i := range columns {
					value := ""
					if i < len(row) {
						value = row[i]
					}
					body += `<w:tc><w:tcPr><w:tcW w:w="` + strconv.Itoa(9360/columns) + `" w:type="dxa"/></w:tcPr>` + wordParagraph(value) + `</w:tc>`
				}
				body += `</w:tr>`
			}
			body += `</w:tbl><w:p/>`
		}
		for _, source := range d.Sources {
			body += wordParagraph(source)
		}
		parts["word/document.xml"] = xmlFile(`<w:document xmlns:w="` + wordNS + `"><w:body>` + body + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440"/></w:sectPr></w:body></w:document>`)
		parts["_rels/.rels"] = rels(rel("rId1", "officeDocument", "word/document.xml"))
		types = override("word/document.xml", "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml")
	case "xlsx":
		if len(d.Rows) == 0 {
			return nil, errors.New("native file: a spreadsheet requires rows")
		}
		rows := ""
		for i, row := range d.Rows {
			cells := ""
			for j, s := range row {
				cells += fmt.Sprintf(`<c r="%s%d" t="inlineStr"><is><t xml:space="preserve">%s</t></is></c>`, column(j), i+1, esc(s))
			}
			rows += fmt.Sprintf(`<row r="%d">%s</row>`, i+1, cells)
		}
		parts["xl/worksheets/sheet1.xml"] = xmlFile(`<worksheet xmlns="` + sheetNS + `"><sheetData>` + rows + `</sheetData></worksheet>`)
		parts["xl/workbook.xml"] = xmlFile(`<workbook xmlns="` + sheetNS + `" xmlns:r="` + officeRel[:len(officeRel)-1] + `"><sheets><sheet name="Sheet 1" sheetId="1" r:id="rId1"/></sheets></workbook>`)
		parts["xl/_rels/workbook.xml.rels"] = rels(rel("rId1", "worksheet", "worksheets/sheet1.xml"))
		parts["_rels/.rels"] = rels(rel("rId1", "officeDocument", "xl/workbook.xml"))
		types = override("xl/workbook.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml") + override("xl/worksheets/sheet1.xml", "application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml")
	case "pptx":
		if len(d.Slides) == 0 {
			return nil, errors.New("native file: a presentation requires slides")
		}
		types = presentation(parts, d)
	default:
		return nil, errors.New("native file: use a .docx, .pptx, .xlsx or .pdf path")
	}
	model, _ := json.Marshal(d)
	parts[modelPart] = string(model)
	parts["[Content_Types].xml"] = xmlFile(`<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="json" ContentType="application/json"/>` + types + `</Types>`)
	keys := make([]string, 0, len(parts))
	for k := range parts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var buf bytes.Buffer
	z := zip.NewWriter(&buf)
	for _, k := range keys {
		w, err := z.Create(k)
		if err != nil {
			return nil, err
		}
		if _, err = io.WriteString(w, parts[k]); err != nil {
			return nil, err
		}
	}
	if err := z.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func column(n int) string {
	s := ""
	for n++; n > 0; n = (n - 1) / 26 {
		s = string(rune('A'+(n-1)%26)) + s
	}
	return s
}

// Decode reads the exact model retained for an immutable native artifact.
// It refuses arbitrary archives and bounds decompression before parsing JSON.
func Decode(data []byte) (string, error) {
	if len(data) > 2<<20 {
		return "", errors.New("native file: archive exceeds 2 MB")
	}
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		marker := []byte("%HUMAGEN-REVISION ")
		at := bytes.LastIndex(data, marker)
		if at < 0 {
			return "", errors.New("native PDF: no retained revision model")
		}
		raw, _, _ := bytes.Cut(data[at+len(marker):], []byte("\n"))
		model, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			return "", err
		}
		d, err := parse(string(model))
		if err != nil {
			return "", err
		}
		if err := validateRevision(data, d); err != nil {
			return "", err
		}
		canonical, _ := json.Marshal(d)
		return string(canonical), nil
	}
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", err
	}
	if len(r.File) > 700 {
		return "", errors.New("native file: too many archive parts")
	}
	for _, f := range r.File {
		if f.Name == modelPart {
			if f.UncompressedSize64 > MaxInput {
				return "", errors.New("native file: revision model exceeds 1 MB")
			}
			reader, err := f.Open()
			if err != nil {
				return "", err
			}
			defer func() { _ = reader.Close() }()
			b, err := io.ReadAll(io.LimitReader(reader, MaxInput+1))
			if err != nil {
				return "", err
			}
			d, err := parse(string(b))
			if err != nil {
				return "", err
			}
			if err := validateRevision(data, d); err != nil {
				return "", err
			}
			canonical, _ := json.Marshal(d)
			return string(canonical), nil
		}
	}
	return "", errors.New("native file: no Humagen revision model; read the original source or request a new native document")
}

// Text returns the visible authored content, notes and sources of a native
// document, for an explicit audience-checked promotion. It never reads links.
func Text(name string, data []byte) (string, error) { return visibleText(name, data) }
