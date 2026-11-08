package platform

import (
	"context"
	"errors"
	"strings"

	"github.com/urmzd/orchagent/flow/attachment"

	"github.com/urmzd/humagen/pkg/speech"
	"github.com/urmzd/humagen/pkg/state"
)

// A surface that posts a person's message straight as a run, as the Slack
// integration does, brings a voice clip as an audio attachment with no
// words. Its turn hears it: the words go into the text the agent reads,
// marked as a voice note as a message's are, and the run is remembered as
// answering a spoken message, so its answer is spoken back (SpeakAnswerTo).
// A message posted through the API was heard when it was posted, so its
// text already holds the mark and nothing is heard twice.

// hearFunc hears the voice notes among a turn's files into its text
// (Platform.hearTurn), and says whether the text changed.
type hearFunc func(ctx context.Context, tenant, run, text string, files []attachment.Attachment) (string, bool)

// voiceTurn is a run's text with its voice notes heard, kept so a turn run
// again (a retry, a resume after an approval) neither hears nor pays twice.
type voiceTurn struct {
	Text   string `json:"text"`
	Spoken bool   `json:"spoken"`
}

func voiceTurnKey(run string) string { return "turn:" + run }

// hearTurn is the turn's text with its voice notes heard, and whether it
// changed.
func (p *Platform) hearTurn(ctx context.Context, tenant, run, text string, files []attachment.Attachment) (string, bool) {
	if run == "" || strings.Contains(text, VoiceNoteMark) || !hasAudio(files) || p.settings.rec.st == nil {
		return text, false
	}
	set := p.voiceUsage()
	var kept voiceTurn
	if _, err := set.lookup(tenant, voiceTurnKey(run), &kept); err == nil {
		return kept.Text, kept.Text != text
	} else if !errors.Is(err, state.ErrNotFound) {
		p.log.Warn("platform: a voice turn could not be read", "tenant", tenant, "run", run, "error", err)
	}
	heard, spoken := p.transcribeVoiceNotes(ctx, tenant, text, files)
	if heard == text {
		return text, false
	}
	op, err := recordOp(tenant, voiceTurnKey(run), voiceTurn{Text: heard, Spoken: spoken}, state.Missing)
	if err == nil {
		err = set.write(op)
	}
	if err != nil && !errors.Is(err, state.ErrConflict) {
		p.log.Warn("platform: a voice turn could not be kept", "tenant", tenant, "run", run, "error", err)
	}
	return heard, true
}

// runSpoken reports whether a run's turn heard a voice note (hearTurn).
func (p *Platform) runSpoken(tenant, run string) bool {
	if p.settings.rec.st == nil {
		return false
	}
	var kept voiceTurn
	_, err := p.voiceUsage().lookup(tenant, voiceTurnKey(run), &kept)
	return err == nil && kept.Spoken
}

func hasAudio(files []attachment.Attachment) bool {
	for _, a := range files {
		if a.Unread == "" && (a.Kind() == attachment.KindAudio || speech.IsAudio(a.MediaType)) {
			return true
		}
	}
	return false
}
