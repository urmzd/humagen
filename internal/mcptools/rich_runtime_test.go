package mcptools_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/urmzd/humagen/internal/mcptools"
	"github.com/urmzd/humagen/internal/toolprovider"
	"github.com/urmzd/saige/agent"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
)

type mediaModel struct{ agenttest.ScriptedProvider }

func (*mediaModel) Capabilities() saigetypes.ModelCapabilities {
	return saigetypes.ModelCapabilities{Provider: "test", Model: "media", Known: true,
		Caps:  map[saigetypes.Capability]bool{saigetypes.CapStreaming: true, saigetypes.CapTools: true},
		Media: saigetypes.ContentSupport{NativeTypes: map[saigetypes.MediaType]bool{saigetypes.MediaPNG: true, saigetypes.MediaWAV: true}},
	}
}

func TestAcceptedMCPMediaReachesTheNextModelCall(t *testing.T) {
	var imageBytes bytes.Buffer
	pic := image.NewRGBA(image.Rect(0, 0, 2, 2))
	pic.Set(1, 1, color.RGBA{G: 255, A: 255})
	if err := png.Encode(&imageBytes, pic); err != nil {
		t.Fatal(err)
	}
	audio := make([]byte, 46)
	copy(audio, "RIFF")
	binary.LittleEndian.PutUint32(audio[4:], 38)
	copy(audio[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(audio[16:], 16)
	binary.LittleEndian.PutUint16(audio[20:], 1)
	binary.LittleEndian.PutUint16(audio[22:], 1)
	binary.LittleEndian.PutUint32(audio[24:], 8000)
	binary.LittleEndian.PutUint32(audio[28:], 16000)
	binary.LittleEndian.PutUint16(audio[32:], 2)
	binary.LittleEndian.PutUint16(audio[34:], 16)
	copy(audio[36:], "data")
	binary.LittleEndian.PutUint32(audio[40:], 2)
	f := newFake()
	f.srv.AddTool(&mcp.Tool{Name: "capture", Description: "Capture the supplied image and audio", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "actual capture"}, &mcp.ImageContent{Data: imageBytes.Bytes(), MIMEType: "image/png"}, &mcp.AudioContent{Data: audio, MIMEType: "audio/wav"}}}, nil
	})
	store := toolprovider.NewMemory()
	s := connectInMemory(t, f, provider(store, t))
	snap := accept(t, store, s, 0, map[string]toolprovider.Class{"capture": toolprovider.ClassRead})
	set, err := s.Toolset(t.Context(), snap, toolprovider.GrantRead)
	if err != nil {
		t.Fatal(err)
	}
	model := &mediaModel{ScriptedProvider: agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.ToolCallResponse("capture-1", "fake__capture", nil), agenttest.TextResponse("Captured.")}}}
	a, err := agent.New(agent.Config{Provider: model, Tools: saigetypes.NewToolRegistry(set.All()...)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = agent.CollectText(a.Invoke(t.Context(), []saigetypes.Message{saigetypes.UserMsg(saigetypes.Text("inspect the capture"))})); err != nil {
		t.Fatal(err)
	}
	calls := model.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls=%d, want tool then answer", len(calls))
	}
	found := map[saigetypes.PartKind][]byte{}
	for _, message := range calls[1].Messages {
		for _, result := range saigetypes.Each[saigetypes.ToolResultPart](message) {
			for _, part := range result.Parts {
				if src, ok := saigetypes.SourceOf(part); ok {
					found[part.Kind()] = src.Inline
				}
			}
		}
	}
	if !bytes.Equal(found[saigetypes.KindImage], imageBytes.Bytes()) || !bytes.Equal(found[saigetypes.KindAudio], audio) {
		t.Fatalf("model did not receive actual image/audio: %v", found)
	}
	// The accepted callable retains its live-revision guard on the rich path.
	f.srv.AddTool(&mcp.Tool{Name: "capture", Description: "changed", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		t.Fatal("changed tool executed")
		return nil, nil
	})
	tool, _ := set.Lookup("fake__capture")
	if _, err := tool.(saigetypes.RichTool).ExecuteRich(t.Context(), nil); !errors.Is(err, mcptools.ErrHeldBack) {
		t.Fatal("changed rich tool was not held back")
	}
}
