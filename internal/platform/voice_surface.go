package platform

import (
	"context"
	"strings"

	"github.com/urmzd/saige/flow/address"

	"github.com/urmzd/humagen/pkg/runs"
)

// clipUploader is a surface that can put an audio clip in a thread: the
// Slack integration (pkg/integrations/slack's UploadClip).
type clipUploader interface {
	UploadClip(ctx context.Context, channel, threadTS, name, title string, audio []byte) error
}

// SpokenTitle names the clip of a spoken answer in a thread.
const SpokenTitle = "Spoken answer"

// speakToSurface puts the spoken answer of a run that answers a spoken
// message in the Slack thread it was asked in, after its text: a completion
// step, so it runs once a run, after the ledger has ended it. Anything that
// stops it (no voice provider, a spend limit, no words, Slack refusing the
// upload) is logged and left: the written answer is there already.
func (p *Platform) speakToSurface(ctx context.Context, run runs.Run) error {
	if run.Status != runs.Completed || run.Parent != "" || run.Client == "" {
		return nil
	}
	to, err := address.Parse(run.Tag().Channel)
	if err != nil || to.Connector != "slack" || len(to.Path) == 0 || !p.AnswersSpoken(run.Tenant, run.ID) {
		return nil
	}
	s := p.surfaces["slack"]
	if s == nil {
		return nil
	}
	st := s.seat(run.Tenant, run.Client)
	if st == nil {
		return nil
	}
	up, ok := st.peer.(clipUploader)
	if !ok {
		return nil
	}
	answer, err := p.speakRunAnswer(ctx, run.Tenant, run)
	if err != nil {
		p.log.Info("platform: no spoken answer", "tenant", run.Tenant, "run", run.ID, "reason", err)
		return nil
	}
	thread, _ := to.Param("thread")
	if err := up.UploadClip(ctx, to.Path[0], thread, "answer."+clipExtension(answer.MediaType), SpokenTitle, answer.Audio); err != nil {
		p.log.Warn("platform: the spoken answer could not be uploaded", "tenant", run.Tenant, "run", run.ID, "error", err)
	}
	return nil
}

// clipExtension names a clip's file by its media type.
func clipExtension(mediaType string) string {
	switch strings.ToLower(strings.TrimSpace(strings.SplitN(mediaType, ";", 2)[0])) {
	case "audio/mpeg", "audio/mp3":
		return "mp3"
	case "audio/wav", "audio/x-wav", "audio/wave":
		return "wav"
	case "audio/ogg", "audio/opus":
		return "ogg"
	}
	return "audio"
}
