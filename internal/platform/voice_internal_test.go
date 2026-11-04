package platform

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/urmzd/orchagent/flow/attachment"
	filesblob "github.com/urmzd/orchagent/flow/blob"

	"github.com/urmzd/humagen/pkg/speech"
	"github.com/urmzd/humagen/pkg/threads"
)

func voicePlatform(t *testing.T, tr speech.Transcriber) (*Platform, func(name, mediaType, body string) attachment.Attachment) {
	t.Helper()
	files := filesblob.NewMemory()
	p := &Platform{cfg: Config{Speech: tr}, files: files, log: slog.New(slog.DiscardHandler)}
	put := func(name, mediaType, body string) attachment.Attachment {
		info, err := files.Put(context.Background(), "acme", strings.NewReader(body), filesblob.PutOptions{})
		if err != nil {
			t.Fatal(err)
		}
		return attachment.Attachment{Digest: info.Digest.String(), MediaType: mediaType, Name: name, Size: info.Size}
	}
	return p, put
}

// A voice note's words join the message's text, marked, and its recording
// stays an attachment the hook does not touch.
func TestAVoiceNoteIsTranscribedIntoTheMessage(t *testing.T) {
	fake := &speech.Fake{Text: "Move standup to ten.", Language: "english"}
	p, put := voicePlatform(t, fake)
	ctx := context.Background()
	note := put("voice.webm", "audio/webm", "OggS-recording")
	files := []attachment.Attachment{put("plan.pdf", "application/pdf", "%PDF"), note}
	before := append([]attachment.Attachment(nil), files...)

	got := p.TranscribeVoiceNotes(ctx, "acme", "See the attached files.", files)
	if want := "See the attached files.\n\n[Voice note, english] Move standup to ten."; got != want {
		t.Fatalf("text = %q, want %q", got, want)
	}
	heard, mimes := fake.Heard()
	if len(heard) != 1 || !bytes.Equal(heard[0], []byte("OggS-recording")) || mimes[0] != "audio/webm" {
		t.Fatalf("heard %q %v: only the audio is transcribed", heard, mimes)
	}
	for i := range files {
		if !reflect.DeepEqual(files[i], before[i]) {
			t.Fatalf("attachment %d changed: %+v", i, files[i])
		}
	}
	// Only audio is heard; a message with none is unchanged.
	if got := p.TranscribeVoiceNotes(ctx, "acme", "hi", files[:1]); got != "hi" {
		t.Fatalf("no audio: %q", got)
	}
	// Another tenant's digest is not read.
	if got := p.TranscribeVoiceNotes(ctx, "globex", "", []attachment.Attachment{note}); got != "[Voice note: voice.webm could not be read]" {
		t.Fatalf("another tenant's note: %q", got)
	}
}

// Speech left out of the spec transcribes nothing.
func TestWithoutSpeechNothingIsTranscribed(t *testing.T) {
	p, put := voicePlatform(t, nil)
	if got := p.TranscribeVoiceNotes(context.Background(), "acme", "hi", []attachment.Attachment{put("v.ogg", "audio/ogg", "x")}); got != "hi" {
		t.Fatalf("text = %q", got)
	}
}

// A note that cannot be heard is said to be so, a message stays within
// what a thread keeps, and at most MaxVoiceNotes are sent.
func TestVoiceNotesAreBounded(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		err  error
		want string
	}{
		{speech.ErrTooLarge, "[Voice note: v.ogg is too long to transcribe]"},
		{speech.ErrEmpty, "[Voice note: v.ogg had no words in it]"},
		{errors.New("boom"), "[Voice note: v.ogg could not be transcribed]"},
	} {
		p, put := voicePlatform(t, &speech.Fake{Err: c.err})
		if got := p.TranscribeVoiceNotes(ctx, "acme", "", []attachment.Attachment{put("v.ogg", "audio/ogg", "x")}); got != c.want {
			t.Errorf("%v: %q", c.err, got)
		}
	}
	fake := &speech.Fake{Text: strings.Repeat("word ", threads.MaxText)}
	p, put := voicePlatform(t, fake)
	var many []attachment.Attachment
	for i := range MaxVoiceNotes + 2 {
		many = append(many, put("v.ogg", "audio/ogg", strings.Repeat("x", i+1)))
	}
	got := p.TranscribeVoiceNotes(ctx, "acme", "hi", many)
	if n := utf8.RuneCountInString(got); n > threads.MaxText || threads.CheckText(got) != nil {
		t.Fatalf("%d characters: %v", n, threads.CheckText(got))
	}
	if heard, _ := fake.Heard(); len(heard) != MaxVoiceNotes {
		t.Fatalf("%d notes transcribed, want %d", len(heard), MaxVoiceNotes)
	}
}
