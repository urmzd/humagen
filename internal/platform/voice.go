package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/orchagent/flow/attachment"
	filesblob "github.com/urmzd/orchagent/flow/blob"

	"github.com/urmzd/humagen/pkg/speech"
	"github.com/urmzd/humagen/pkg/threads"
)

// MaxVoiceNotes bounds how many voice notes one message has transcribed.
// The rest stay attachments, as every voice note does.
const MaxVoiceNotes = 4

// VoiceNoteMark starts each transcript in a message's text, so a reader
// and an agent both know the words were spoken.
const VoiceNoteMark = "[Voice note"

// TranscribeVoiceNotes is the ingress hook for voice. Given a message that
// starts a turn, its text and the attachments its channel stored, it
// transcribes each audio attachment and returns the text with each
// transcript after it, marked as a voice note. The attachments are not
// changed, so the recording stays with the message.
//
// It knows nothing of the channel: Slack calls it with the files of a
// message, the console's transcription route uses the same transcriber,
// and a WhatsApp or iMessage connector that stores a voice note as an audio
// attachment calls it the same way. It transcribes with the workspace's
// voice provider, else the System spec's speech kind. With neither, or no
// audio, the text comes back unchanged. A note that cannot be transcribed
// is said to be so, and the message still posts. The result stays within
// threads.MaxText.
func (p *Platform) TranscribeVoiceNotes(ctx context.Context, tenant, text string, files []attachment.Attachment) string {
	out, _ := p.transcribeVoiceNotes(ctx, tenant, text, files)
	return out
}

// transcribeVoiceNotes is TranscribeVoiceNotes, and whether any note's words
// were heard: such a message was spoken, and its answer can be spoken back.
func (p *Platform) transcribeVoiceNotes(ctx context.Context, tenant, text string, files []attachment.Attachment) (string, bool) {
	h := p.heardBy(tenant)
	if h.t == nil || p.files == nil {
		return text, false
	}
	var notes []string
	heard := false
	for _, a := range files {
		if len(notes) == MaxVoiceNotes {
			break
		}
		if a.Unread != "" || a.Kind() != attachment.KindAudio && !speech.IsAudio(a.MediaType) {
			continue
		}
		note, ok := p.voiceNote(ctx, tenant, a, h)
		notes = append(notes, note)
		heard = heard || ok
	}
	if len(notes) == 0 {
		return text, false
	}
	out := strings.TrimSpace(text)
	for _, n := range notes {
		if out != "" {
			out += "\n\n"
		}
		out += n
	}
	return clipRunes(out, threads.MaxText), heard
}

// voiceNote is one attachment's transcript, marked, and true; or a note
// that it could not be heard, and false.
func (p *Platform) voiceNote(ctx context.Context, tenant string, a attachment.Attachment, h hearing) (string, bool) {
	r, _, err := p.files.Get(ctx, tenant, filesblob.Digest(a.Digest))
	if err != nil {
		p.log.Warn("voice note: read", "tenant", tenant, "file", a.Label(), "err", err)
		return VoiceNoteMark + ": " + a.Label() + " could not be read]", false
	}
	defer r.Close()
	heard, err := speech.Hear(ctx, h.t, r, a.MediaType)
	if err == nil {
		h.meter(p, speechUse{tenant: tenant}, heard.Duration)
	}
	text, lang := heard.Text, heard.Language
	switch {
	case errors.Is(err, speech.ErrTooLarge):
		return VoiceNoteMark + ": " + a.Label() + " is too long to transcribe]", false
	case errors.Is(err, speech.ErrEmpty):
		return VoiceNoteMark + ": " + a.Label() + " had no words in it]", false
	case err != nil:
		p.log.Warn("voice note: transcribe", "tenant", tenant, "file", a.Label(), "err", err)
		return VoiceNoteMark + ": " + a.Label() + " could not be transcribed]", false
	}
	if lang != "" {
		return fmt.Sprintf("%s, %s] %s", VoiceNoteMark, lang, text), true
	}
	return VoiceNoteMark + "] " + text, true
}

// MaxRecordingBytes bounds a recording the console sends to be
// transcribed: about ten minutes of Opus at the browser's usual rate.
const MaxRecordingBytes = 10 << 20

// ErrSpeechOff is a workspace with no voice provider, on a deployment whose
// System spec has no speech kind.
var ErrSpeechOff = errors.New("platform: voice is off: no voice provider is set up")

// ErrNotAudio is a recording whose bytes are not audio.
var ErrNotAudio = errors.New("platform: the recording is not audio")

// VoiceSettings is what the console needs to offer voice.
type VoiceSettings struct {
	// Transcribe is whether a recording is turned into words here: by the
	// workspace's voice provider or the deployment's speech kind.
	Transcribe bool `json:"transcribe"`
	// Speak is whether voice mode is on: the workspace has a voice provider,
	// which also speaks.
	Speak bool `json:"speak"`
	// Provider is the voice provider's kind, groq or openai, when it has one.
	Provider string `json:"provider,omitempty"`
	MaxBytes int64  `json:"max_bytes"`
}

// Voice says what voice a workspace has.
func (p *Platform) Voice(tenant string) VoiceSettings {
	out := VoiceSettings{MaxBytes: MaxRecordingBytes}
	if _, vp, ok := p.workspaceVoice(tenant); ok {
		out.Transcribe, out.Speak, out.Provider = true, true, vp.Kind
		return out
	}
	out.Transcribe = p.cfg.Speech != nil
	return out
}

// VoiceTranscript is a recording's words.
type VoiceTranscript struct {
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

// TranscribeRecording transcribes a recording a person in tenant made, with
// the same transcriber TranscribeVoiceNotes uses. What the bytes are is
// sniffed, not taken from the sender. The recording is not kept; what it
// cost is counted.
func (p *Platform) TranscribeRecording(ctx context.Context, tenant string, audio []byte) (VoiceTranscript, error) {
	h := p.heardBy(tenant)
	if h.t == nil {
		return VoiceTranscript{}, ErrSpeechOff
	}
	if len(audio) > MaxRecordingBytes {
		return VoiceTranscript{}, speech.ErrTooLarge
	}
	mt := attachment.Sniff(audio, "")
	if attachment.KindOf(mt) != attachment.KindAudio && !speech.IsAudio(mt) {
		return VoiceTranscript{}, ErrNotAudio
	}
	heard, err := speech.Hear(ctx, h.t, bytes.NewReader(audio), mt)
	if err != nil {
		return VoiceTranscript{}, err
	}
	h.meter(p, speechUse{tenant: tenant}, heard.Duration)
	return VoiceTranscript{Text: clipRunes(heard.Text, threads.MaxText), Language: heard.Language}, nil
}
