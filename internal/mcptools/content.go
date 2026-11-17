package mcptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/attachment"
)

// Result limits apply to the complete result, not independently to each block.
const (
	MaxResultParts = 128
	MaxResultBytes = 25 << 20
)

var ErrContent = errors.New("mcptools: invalid result content")

func richResult(res *mcp.CallToolResult, red redactor) (saigetypes.ToolResult, error) {
	if res == nil || len(res.Content) > MaxResultParts {
		return saigetypes.ToolResult{}, fmt.Errorf("%w: missing result or too many parts", ErrContent)
	}
	out := saigetypes.ToolResult{IsError: res.IsError}
	size := 0
	add := func(part saigetypes.ToolOutputPart, n int) error {
		size += n
		if size > MaxResultBytes || len(out.Parts) >= MaxResultParts {
			return fmt.Errorf("%w: result exceeds its byte or part limit", ErrContent)
		}
		out.Parts = append(out.Parts, part)
		return nil
	}
	text := func(s string) error { return add(saigetypes.Text(red.text(s)), len(s)) }
	media := func(data []byte, declared, name string, kind attachment.Kind) error {
		if len(data) == 0 || len(data) > MaxResultBytes {
			return fmt.Errorf("%w: empty or oversized media", ErrContent)
		}
		actual := attachment.Sniff(data, name)
		if attachment.KindOf(actual) != kind || (declared != "" && attachment.KindOf(declared) != kind) {
			return fmt.Errorf("%w: media bytes do not match their content kind", ErrContent)
		}
		source := saigetypes.Bytes(saigetypes.MediaType(actual), append([]byte(nil), data...))
		source.Filename = red.text(attachment.CleanName(name))
		var part saigetypes.ToolOutputPart
		switch kind {
		case attachment.KindImage:
			part = saigetypes.Image(source)
		case attachment.KindAudio:
			part = saigetypes.Audio(source)
		case attachment.KindDocument:
			part = saigetypes.Document(source)
		default:
			part = saigetypes.File(source)
		}
		return add(part, len(data))
	}
	for _, content := range res.Content {
		var err error
		switch v := content.(type) {
		case *mcp.TextContent:
			if v == nil {
				return saigetypes.ToolResult{}, ErrContent
			}
			err = text(v.Text)
		case *mcp.ImageContent:
			if v == nil {
				return saigetypes.ToolResult{}, ErrContent
			}
			err = media(v.Data, v.MIMEType, "", attachment.KindImage)
		case *mcp.AudioContent:
			if v == nil {
				return saigetypes.ToolResult{}, ErrContent
			}
			err = media(v.Data, v.MIMEType, "", attachment.KindAudio)
		case *mcp.ResourceLink:
			if v == nil {
				return saigetypes.ToolResult{}, ErrContent
			}
			err = text(fmt.Sprintf("[resource %s]", v.URI))
		case *mcp.EmbeddedResource:
			if v == nil || v.Resource == nil {
				return saigetypes.ToolResult{}, ErrContent
			}
			r := v.Resource
			switch {
			case r.Text != "":
				err = text(r.Text)
			case len(r.Blob) > 0:
				kind := attachment.KindOf(attachment.Sniff(r.Blob, r.URI))
				err = media(r.Blob, r.MIMEType, r.URI, kind)
			default:
				err = text(fmt.Sprintf("[resource %s]", r.URI))
			}
		default:
			return saigetypes.ToolResult{}, fmt.Errorf("%w: unsupported MCP content type %T", ErrContent, content)
		}
		if err != nil {
			return saigetypes.ToolResult{}, err
		}
	}
	// Keep structured output even when the server also supplies text or media.
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err != nil {
			return saigetypes.ToolResult{}, fmt.Errorf("%w: structured output: %v", ErrContent, err)
		}
		if err := text(string(b)); err != nil {
			return saigetypes.ToolResult{}, err
		}
	}
	if len(out.Parts) == 0 {
		out.Parts = []saigetypes.ToolOutputPart{saigetypes.Text("")}
	}
	return out, nil
}

// textProjection names media for callers of the text-only Tool interface.
func textProjection(result saigetypes.ToolResult) string {
	var parts []string
	for _, part := range result.Parts {
		if source, ok := saigetypes.SourceOf(part); ok {
			parts = append(parts, fmt.Sprintf("[%s %s, %d bytes]", part.Kind(), source.MediaType, source.Size))
		} else if t, ok := part.(saigetypes.TextPart); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}
