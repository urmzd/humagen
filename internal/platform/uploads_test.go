package platform_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/turn/native"
)

type uploadModel struct{ agenttest.ScriptedProvider }

func (*uploadModel) Capabilities() saigetypes.ModelCapabilities {
	return saigetypes.ModelCapabilities{Provider: "uploads", Model: "vision", Known: true, Media: saigetypes.ContentSupport{NativeTypes: map[saigetypes.MediaType]bool{saigetypes.MediaPNG: true}}}
}

func uploadPlatform(t *testing.T) (*platform.Platform, *uploadModel) {
	t.Helper()
	model := &uploadModel{ScriptedProvider: agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{agenttest.TextResponse("Observed the image."), agenttest.TextResponse("The original pixels are still present.")}}}
	p, err := platform.New(platform.Config{Workspace: t.TempDir(), AgentOptions: []native.Option{native.WithProvider("uploads", func(native.Spec) (saigetypes.Provider, error) { return model, nil })}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Agents.Create(t.Context(), tenant, wire.AgentSpec{Name: "vision", Provider: "uploads", Model: "vision", SystemPrompt: "Inspect supplied pixels."}, "test"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- p.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		_ = p.Close()
	})
	return p, model
}

func imageUpload(t *testing.T) platform.Upload {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 3, 3))
	img.Set(1, 1, color.RGBA{B: 255, A: 255})
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return platform.Upload{Name: "capture.png", Data: b.Bytes(), Kind: "image"}
}

func awaitSessionAnswer(t *testing.T, p *platform.Platform, v platform.SessionView) {
	t.Helper()
	found := false
	for _, m := range v.Messages {
		if m.Run != nil && !m.Run.Status.Terminal() {
			await(t, p, m.Run.ID, runs.Completed)
			found = true
		}
	}
	if !found {
		for _, m := range v.Messages {
			if m.Run != nil && m.Run.Status == runs.Completed {
				return
			}
		}
		t.Fatal("no agent run for uploaded message")
	}
}

func TestUploadedImageReachesModelAndLaterHistory(t *testing.T) {
	p, model := uploadPlatform(t)
	file := imageUpload(t)
	first, err := p.StartSession(t.Context(), admin, platform.SessionInput{Agent: "vision", Text: "What color is the center pixel?", Uploads: []platform.Upload{file}})
	if err != nil {
		t.Fatal(err)
	}
	awaitSessionAnswer(t, p, first)
	next, err := p.PostSession(t.Context(), admin, first.ID, platform.MessageInput{Text: "Check the original picture again"})
	if err != nil {
		t.Fatal(err)
	}
	awaitSessionAnswer(t, p, next)
	calls := model.Requests()
	if len(calls) != 2 {
		t.Fatalf("model calls=%d", len(calls))
	}
	for i, call := range calls {
		seen := false
		for _, msg := range call.Messages {
			for _, part := range saigetypes.Each[saigetypes.ImagePart](msg) {
				seen = seen || bytes.Equal(part.Source.Inline, file.Data)
			}
		}
		if !seen {
			t.Fatalf("model call %d lost the original image bytes", i)
		}
	}
	view, err := p.ReadSession(t.Context(), admin, first.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range view.Messages {
		if m.Message != nil && len(m.Message.Attachments) > 0 {
			a := m.Message.Attachments[0]
			found = a.Digest != "" && a.Size == int64(len(file.Data)) && a.MediaType == "image/png"
		}
	}
	if !found {
		t.Fatal("durable transcript lost the attachment reference")
	}
	other := admin
	other.Tenant = "other"
	if _, err := p.PostSession(t.Context(), other, first.ID, platform.MessageInput{Text: "private", Uploads: []platform.Upload{file}}); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("foreign upload: %v", err)
	}
}

func TestMessagesAcceptImageBlocksAndRefuseInvalidBatchBeforePosting(t *testing.T) {
	p, _ := uploadPlatform(t)
	file := imageUpload(t)
	out, err := p.SendMessages(t.Context(), admin, platform.MessagesInput{Messages: []platform.Message{{Role: "user", Content: platform.Content{{Type: "mention", Agent: "vision"}, {Type: "text", Text: "Read this picture"}, {Type: "image", Name: file.Name, Data: file.Data}}}}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := p.ReadSession(t.Context(), admin, out.Root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	awaitSessionAnswer(t, p, view)
	before := len(view.Messages)
	_, err = p.SendMessagesUnder(t.Context(), admin, out.Root, platform.MessagesInput{Messages: []platform.Message{{Role: "user", Content: platform.Content{{Type: "text", Text: "should not post"}}}, {Role: "user", Content: platform.Content{{Type: "image", Name: "false.png", Data: []byte("not image")}}}}})
	if !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("invalid batch: %v", err)
	}
	view, err = p.ReadSession(t.Context(), admin, out.Root, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Messages) != before {
		t.Fatal("invalid trailing upload posted an earlier message")
	}
}
