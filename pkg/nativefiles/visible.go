package nativefiles

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/dslipak/pdf"
)

func wordParagraph(s string) string {
	return `<w:p><w:r><w:t xml:space="preserve">` + esc(s) + `</w:t></w:r></w:p>`
}

func renderedFields(ext string, d Document) error {
	unsupported := false
	switch ext {
	case "docx":
		unsupported = len(d.Slides) > 0
	case "pptx":
		unsupported = len(d.Paragraphs) > 0 || len(d.Rows) > 0
	case "xlsx":
		unsupported = d.Title != "" || len(d.Paragraphs) > 0 || len(d.Slides) > 0 || len(d.Sources) > 0
	case "pdf":
		unsupported = len(d.Slides) > 0 && (len(d.Paragraphs) > 0 || len(d.Rows) > 0)
	}
	if unsupported {
		return fmt.Errorf("native file: %s cannot render this field combination; DOCX accepts title, paragraphs, rows and sources; PPTX accepts title, slides and sources; XLSX accepts rows only; PDF accepts title and sources with paragraphs/rows or slides", ext)
	}
	return nil
}

func officeParts(data []byte) (map[string][]byte, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	if len(z.File) > 700 {
		return nil, errors.New("native file: too many archive parts")
	}
	parts := map[string][]byte{}
	var expanded uint64
	for _, f := range z.File {
		if !strings.HasSuffix(f.Name, ".xml") && !strings.HasSuffix(f.Name, ".rels") {
			continue
		}
		expanded += f.UncompressedSize64
		if f.UncompressedSize64 > 8<<20 || expanded > 16<<20 {
			return nil, errors.New("native file: expanded XML exceeds the read limit")
		}
		if _, exists := parts[f.Name]; exists {
			return nil, errors.New("native file: duplicate XML part")
		}
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		b, err := io.ReadAll(io.LimitReader(r, (8<<20)+1))
		_ = r.Close()
		if err != nil {
			return nil, err
		}
		if len(b) > 8<<20 {
			return nil, errors.New("native file: expanded XML exceeds the read limit")
		}
		parts[f.Name] = b
	}
	return parts, nil
}

// validateRevision refuses stale sidecars rather than turning previously hidden
// fields into visible content after an encoder upgrade. Downloads stay immutable.
func validateRevision(data []byte, d Document) error {
	raw, _ := json.Marshal(d)
	if bytes.HasPrefix(data, []byte("%PDF-")) {
		expected, err := Encode("file.pdf", string(raw))
		if err != nil || !bytes.Equal(expected, data) {
			return errors.New("native file: retained revision differs from the saved PDF; download the original")
		}
		return nil
	}
	actual, err := officeParts(data)
	if err != nil {
		return err
	}
	ext := ""
	if actual["word/document.xml"] != nil {
		ext = "docx"
	} else if actual["ppt/presentation.xml"] != nil {
		ext = "pptx"
	} else if actual["xl/workbook.xml"] != nil {
		ext = "xlsx"
	}
	if ext == "" {
		return errors.New("native file: no editable Office parts")
	}
	expected, err := Encode("file."+ext, string(raw))
	if err != nil {
		return errors.New("native file: retained revision contains fields absent from the saved file; download the original")
	}
	parts, err := officeParts(expected)
	if err != nil {
		return err
	}
	visible := func(name string) bool {
		return name == "word/document.xml" || name == "ppt/presentation.xml" || name == "xl/workbook.xml" || name == "xl/_rels/workbook.xml.rels" || strings.HasPrefix(name, "ppt/slides/slide") || strings.HasPrefix(name, "ppt/notesSlides/notesSlide") || strings.HasPrefix(name, "xl/worksheets/sheet") || strings.HasPrefix(name, "ppt/") && strings.HasSuffix(name, ".rels")
	}
	for name, content := range parts {
		if visible(name) && !bytes.Equal(content, actual[name]) {
			return errors.New("native file: retained revision differs from authored Office content; download the original")
		}
	}
	for name, content := range actual {
		if visible(name) && !bytes.Equal(content, parts[name]) {
			return errors.New("native file: retained revision differs from authored Office content; download the original")
		}
	}
	return nil
}

func xmlText(data []byte) (string, error) {
	if len(data) == 0 {
		return "", errors.New("native file: authored XML part is missing")
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var out strings.Builder
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch item := token.(type) {
		case xml.StartElement:
			if item.Name.Local == "t" || item.Name.Local == "v" {
				var text string
				if err := dec.DecodeElement(&text, &item); err != nil {
					return "", err
				}
				out.WriteString(text)
			}
		case xml.EndElement:
			switch item.Name.Local {
			case "p", "row", "tr":
				out.WriteString("\n")
			case "tc", "c":
				out.WriteString("\t")
			}
		}
		if out.Len() > MaxInput {
			return "", errors.New("native file: authored text exceeds the read limit")
		}
	}
	return strings.TrimSpace(out.String()), nil
}

func visibleText(name string, data []byte) (string, error) {
	if _, err := Decode(data); err != nil {
		return "", err
	}
	if strings.EqualFold(path.Ext(name), ".pdf") {
		r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return "", err
		}
		text, err := r.GetPlainText()
		if err != nil {
			return "", err
		}
		b, err := io.ReadAll(io.LimitReader(text, MaxInput+1))
		if err != nil {
			return "", err
		}
		if len(b) > MaxInput {
			return "", errors.New("native file: authored text exceeds the read limit")
		}
		return strings.TrimSpace(string(b)), nil
	}
	parts, err := officeParts(data)
	if err != nil {
		return "", err
	}
	keys := []string{}
	switch strings.ToLower(path.Ext(name)) {
	case ".docx":
		keys = append(keys, "word/document.xml")
	case ".xlsx":
		keys = append(keys, "xl/worksheets/sheet1.xml")
	case ".pptx":
		var numbers []int
		for key := range parts {
			if strings.HasPrefix(key, "ppt/slides/slide") {
				n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(key, "ppt/slides/slide"), ".xml"))
				if err == nil {
					numbers = append(numbers, n)
				}
			}
		}
		sort.Ints(numbers)
		for _, n := range numbers {
			keys = append(keys, fmt.Sprintf("ppt/slides/slide%d.xml", n), fmt.Sprintf("ppt/notesSlides/notesSlide%d.xml", n))
		}
	default:
		return "", errors.New("native file: no text extractor for this format")
	}
	var texts []string
	for _, key := range keys {
		text, err := xmlText(parts[key])
		if err != nil {
			return "", err
		}
		texts = append(texts, text)
	}
	result := strings.TrimSpace(strings.Join(texts, "\n\n"))
	if len(result) > MaxInput {
		return "", errors.New("native file: authored text exceeds the read limit")
	}
	return result, nil
}
