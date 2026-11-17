package mcptools

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	saigetypes "github.com/urmzd/saige/agent/types"
)

func samplePNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestRichResultRetainsOrderedMediaAndStructuredOutput(t *testing.T) {
	data := samplePNG(t)
	in := &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: "look at secret-value"},
		&mcp.ImageContent{Data: data, MIMEType: "image/png"},
		&mcp.EmbeddedResource{Resource: &mcp.ResourceContents{URI: "report.pdf", MIMEType: "application/pdf", Blob: []byte("%PDF-1.7\nreport")}},
		&mcp.ResourceLink{URI: "https://example.com/secret-value"},
	}, StructuredContent: map[string]any{"count": 2}}
	out, err := richResult(in, redactor{secret: NewSecret("secret-value")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Parts) != 5 || out.Parts[1].Kind() != saigetypes.KindImage || out.Parts[2].Kind() != saigetypes.KindDocument {
		t.Fatalf("lost ordered blocks: %v", out.Parts)
	}
	src, ok := saigetypes.SourceOf(out.Parts[1])
	if !ok || !bytes.Equal(src.Inline, data) || src.MediaType != saigetypes.MediaPNG || src.Digest == "" {
		t.Fatalf("image source = %+v", src)
	}
	if strings.Contains(out.Text(), "secret-value") || !strings.Contains(out.Text(), `"count":2`) {
		t.Fatalf("projection = %s", out.Text())
	}
	data[0] = 0
	if src.Inline[0] == 0 {
		t.Fatal("media aliases the server-owned result buffer")
	}
	if !strings.Contains(textProjection(out), "image image/png") {
		t.Fatal("text-only callers cannot see that media was returned")
	}
}

func TestRichResultRefusesMalformedAndUnboundedMedia(t *testing.T) {
	for name, in := range map[string]*mcp.CallToolResult{
		"nil":             nil,
		"empty image":     {Content: []mcp.Content{&mcp.ImageContent{MIMEType: "image/png"}}},
		"fake image":      {Content: []mcp.Content{&mcp.ImageContent{MIMEType: "image/png", Data: []byte("not an image")}}},
		"kind mismatch":   {Content: []mcp.Content{&mcp.ImageContent{MIMEType: "audio/wav", Data: samplePNG(t)}}},
		"too many blocks": {Content: make([]mcp.Content, MaxResultParts+1)},
		"oversized media": {Content: []mcp.Content{&mcp.ImageContent{MIMEType: "image/png", Data: make([]byte, MaxResultBytes+1)}}},
		"cumulative text": {Content: []mcp.Content{&mcp.TextContent{Text: strings.Repeat("a", MaxResultBytes)}, &mcp.TextContent{Text: "b"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := richResult(in, redactor{}); !errors.Is(err, ErrContent) {
				t.Fatalf("got %v, want invalid content", err)
			}
		})
	}
}
