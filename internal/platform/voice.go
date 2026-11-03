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
// attachment calls it the same way. With no transcriber (speech left out
// of the System spec), or no audio, the text comes back unchanged. A note
// that cannot be transcribed is said to be so, and the message still
// posts. The result stays within threads.MaxText.
func (p *Platform) TranscribeVoiceNotes(ctx context.Context, tenant, text string, files []attachment.Attachment) string {
	if p.cfg.Speech == nil || p.files == nil {
		return text
	}
	var notes []string
	for _, a := range files {
		if len(notes) == MaxVoiceNotes {
			break
		}
		if a.Unread != "" || a.Kind() != attachment.KindAudio && !speech.IsAudio(a.MediaType) {
			continue
		}
		notes = append(notes, p.voiceNote(ctx, tenant, a))
	}
	if len(notes) == 0 {
		return text
	}
	out := strings.TrimSpace(text)
	for _, n := range notes {
		if out != "" {
			out += "\n\n"
		}
		out += n
	}
	return clipRunes(out, threads.MaxText)
}

// voiceNote is one attachment's transcript, marked, or a note that it
// could not be heard.
func (p *Platform) voiceNote(ctx context.Context, tenant string, a attachment.Attachment) string {
	r, _, err := p.files.Get(ctx, tenant, filesblob.Digest(a.Digest))
	if err != nil {
		p.log.Warn("voice note: read", "tenant", tenant, "file", a.Label(), "err", err)
		return VoiceNoteMark + ": " + a.Label() + " could not be read]"
	}
	defer r.Close()
	text, lang, err := p.cfg.Speech.Transcribe(ctx, r, a.MediaType)
	switch {
	case errors.Is(err, speech.ErrTooLarge):
		return VoiceNoteMark + ": " + a.Label() + " is too long to transcribe]"
	case errors.Is(err, speech.ErrEmpty):
		return VoiceNoteMark + ": " + a.Label() + " had no words in it]"
	case err != nil:
		p.log.Warn("voice note: transcribe", "tenant", tenant, "file", a.Label(), "err", err)
		return VoiceNoteMark + ": " + a.Label() + " could not be transcribed]"
	}
	if lang != "" {
		return fmt.Sprintf("%s, %s] %s", VoiceNoteMark, lang, text)
	}
	return VoiceNoteMark + "] " + text
}

// MaxRecordingBytes bounds a recording the console sends to be
// transcribed: about ten minutes of Opus at the browser's usual rate.
const MaxRecordingBytes = 10 << 20

// ErrSpeechOff is a deployment whose System spec has no speech kind.
var ErrSpeechOff = errors.New("platform: voice notes are off: the System spec has no speech kind")

// ErrNotAudio is a recording whose bytes are not audio.
var ErrNotAudio = errors.New("platform: the recording is not audio")

// VoiceSettings is what the console needs to offer a recording.
type VoiceSettings struct {
	Transcribe bool  `json:"transcribe"`
	MaxBytes   int64 `json:"max_bytes"`
}

// Voice says whether voice notes are on.
func (p *Platform) Voice() VoiceSettings {
	return VoiceSettings{Transcribe: p.cfg.Speech != nil, MaxBytes: MaxRecordingBytes}
}

// VoiceTranscript is a recording's words.
type VoiceTranscript struct {
	Text     string `json:"text"`
	Language string `json:"language,omitempty"`
}

// TranscribeRecording transcribes a recording a person made, with the same
// transcriber TranscribeVoiceNotes uses. What the bytes are is sniffed, not
// taken from the sender. The recording is not kept.
func (p *Platform) TranscribeRecording(ctx context.Context, audio []byte) (VoiceTranscript, error) {
	if p.cfg.Speech == nil {
		return VoiceTranscript{}, ErrSpeechOff
	}
	if len(audio) > MaxRecordingBytes {
		return VoiceTranscript{}, speech.ErrTooLarge
	}
	mt := attachment.Sniff(audio, "")
	if attachment.KindOf(mt) != attachment.KindAudio && !speech.IsAudio(mt) {
		return VoiceTranscript{}, ErrNotAudio
	}
	text, lang, err := p.cfg.Speech.Transcribe(ctx, bytes.NewReader(audio), mt)
	if err != nil {
		return VoiceTranscript{}, err
	}
	return VoiceTranscript{Text: clipRunes(text, threads.MaxText), Language: lang}, nil
}
