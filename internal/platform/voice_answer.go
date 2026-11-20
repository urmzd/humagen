package platform

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/speech"
)

// A spoken answer is one audio clip of a finished run's answer, for a
// surface that sends a reply as a file: a Slack thread answers a voice clip
// with one. Like voice mode, it speaks only the run's own words, read here.

// MaxSpokenAnswer bounds the words one clip says, in runes. A longer answer
// is cut at a sentence and ends with RestInThread.
const MaxSpokenAnswer = 1200

// RestInThread ends a clip of an answer longer than MaxSpokenAnswer.
const RestInThread = "The rest is in the thread."

// ErrRunWorking is a run that has not ended: its answer is not whole yet.
var ErrRunWorking = errors.New("platform: the run is still working")

// SpokenAnswer is a finished run's answer as one clip.
type SpokenAnswer struct {
	Audio     []byte `json:"audio"`
	MediaType string `json:"media_type"`
	// Text is what the clip says.
	Text string `json:"text"`
	// Whole is an answer said in full, not cut at MaxSpokenAnswer.
	Whole      bool  `json:"whole"`
	CostMicros int64 `json:"cost_micros"`
}

// SpeakAnswer speaks a finished run's answer as one clip, with the
// workspace's voice provider. Each part spoken is counted as voice mode's
// are, toward the run's project and agent, and on its session.
func (p *Platform) SpeakAnswer(ctx context.Context, who auth.Principal, runID string) (SpokenAnswer, error) {
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || !p.Seer(who).Run(run) {
		return SpokenAnswer{}, fmt.Errorf("%w: %s", runs.ErrNotFound, runID)
	}
	return p.speakRunAnswer(ctx, who.Tenant, run)
}

// SpeakAnswerTo is SpeakAnswer for a surface's own delivery, which answers
// in the thread the run already posts to: the Slack integration calls it
// when a run that answers a spoken message (AnswersSpoken) has completed,
// and uploads the clip with slack.UploadClip.
func (p *Platform) SpeakAnswerTo(ctx context.Context, tenant, runID string) (SpokenAnswer, error) {
	run, err := p.Runs.Get(tenant, runID)
	if err != nil {
		return SpokenAnswer{}, fmt.Errorf("%w: %s", runs.ErrNotFound, runID)
	}
	return p.speakRunAnswer(ctx, tenant, run)
}

// AnswersSpoken reports whether a run answers a message its person spoke: a
// voice note that was heard, or a message sent in voice mode. Its answer is
// then also spoken back where the surface can carry audio.
func (p *Platform) AnswersSpoken(tenant, runID string) bool {
	if p.runSpoken(tenant, runID) {
		return true
	}
	run, err := p.Runs.Get(tenant, runID)
	if err != nil || run.Thread == "" || run.Message == "" || p.Threads == nil {
		return false
	}
	m, err := p.Threads.Message(tenant, run.Thread, run.Message)
	return err == nil && m.Spoken
}

func (p *Platform) speakRunAnswer(ctx context.Context, tenant string, run runs.Run) (SpokenAnswer, error) {
	if !run.Status.Terminal() {
		return SpokenAnswer{}, ErrRunWorking
	}
	v, vp, ok := p.workspaceVoice(tenant)
	if !ok {
		return SpokenAnswer{}, ErrVoiceOff
	}
	if err := p.speechAllowed(tenant, run.Project, run.Agent); err != nil {
		return SpokenAnswer{}, err
	}
	parts, whole := answerParts(run.Answer)
	if len(parts) == 0 {
		return SpokenAnswer{}, speech.ErrEmpty
	}
	clips := make([][]byte, len(parts))
	types := make([]string, len(parts))
	errs := make([]error, len(parts))
	var wg sync.WaitGroup
	for i, part := range parts {
		wg.Go(func() { clips[i], types[i], errs[i] = v.Synthesize(ctx, part) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return SpokenAnswer{}, err
	}
	audio, err := speech.Join(types[0], clips)
	if err != nil {
		return SpokenAnswer{}, err
	}
	out := SpokenAnswer{Audio: audio, MediaType: types[0], Whole: whole}
	chars := 0
	for _, part := range parts {
		n := utf8.RuneCountInString(part)
		chars += n
		micros := vp.Price.Speaking(n)
		out.CostMicros += micros
		p.meterSpeech(speechUse{tenant: tenant, project: run.Project, agent: run.Agent, label: vp.Kind + "/" + vp.SpeechModel, micros: micros})
	}
	if run.Thread != "" {
		p.addVoiceUsage(tenant, run.Thread, VoiceUsage{SpokenChars: int64(chars), Micros: out.CostMicros, Calls: len(parts)})
	}
	for i, part := range parts {
		if i > 0 {
			out.Text += " "
		}
		out.Text += part
	}
	return out, nil
}

// answerParts is a finished answer as the parts one clip says, each at most
// maxSpoken runes, up to MaxSpokenAnswer in all; a longer answer ends with
// RestInThread, and whole is false.
func answerParts(answer string) (parts []string, whole bool) {
	from, size := 0, 0
	for {
		say, next, done := nextSpoken(answer, from, true)
		if say != "" {
			n := utf8.RuneCountInString(say)
			if size > 0 && size+n > MaxSpokenAnswer {
				return append(parts, RestInThread), false
			}
			parts = append(parts, say)
			size += n
		}
		if done || next <= from {
			return parts, true
		}
		from = next
	}
}
