package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/urmzd/orchagent/flow/attachment"
	"github.com/urmzd/orchagent/flow/event"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/speech"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/threads"
)

// Voice mode in a session: the person's words are heard with the workspace's
// voice provider and sent as ordinary messages (with spoken set), and the
// agent's answer is spoken a few sentences at a time. What is spoken is only
// ever the text of a run of that session, read here, never text a client
// sends, so voice mode cannot be used to speak anything else on the
// workspace's account. Every call is counted in the usage ledger and on the
// session's voice usage, and none is made when a spend limit is reached.

// ErrVoiceOff is a workspace with no voice provider: voice mode is off.
var ErrVoiceOff = errors.New("platform: voice mode is off: no voice provider is set up")

// ErrVoiceSpend is a spend limit that is reached: no speech call is made.
var ErrVoiceSpend = errors.New("platform: the spend limit is reached")

// Bounds on what one call speaks.
const (
	// maxSpoken is the most text one call speaks, in runes.
	maxSpoken = 400
	// enoughSpoken is where a call stops adding sentences, so the first
	// sound comes soon and later ones arrive while it plays.
	enoughSpoken = 160
	// CodeSpoken is said in place of a code block.
	CodeSpoken = "The code is in the session."
)

// VoiceUsage is what voice mode has used in one session.
type VoiceUsage struct {
	// HeardMS is how long the person spoke, as the provider measured it.
	HeardMS int64 `json:"heard_ms"`
	// SpokenChars is how many characters the agent's answers were spoken in.
	SpokenChars int64 `json:"spoken_chars"`
	// Micros is what it cost, in micro-USD.
	Micros int64 `json:"micros"`
	// Calls is every call to the voice provider; Unpriced, those whose
	// length the provider did not say.
	Calls    int `json:"calls"`
	Unpriced int `json:"unpriced_calls,omitempty"`
}

// VoiceSpeech is the next part of an answer, spoken.
type VoiceSpeech struct {
	// Audio and MediaType are the sound; empty when Pending.
	Audio     []byte `json:"audio,omitempty"`
	MediaType string `json:"media_type,omitempty"`
	// Text is what was said, for the captions.
	Text string `json:"text"`
	// Next is the offset to ask from next time.
	Next int `json:"next"`
	// Pending is an answer still being written, with no whole sentence
	// after From yet: ask again when more arrives.
	Pending bool `json:"pending,omitempty"`
	// Done is an answer that has ended and has been said in full.
	Done       bool  `json:"done,omitempty"`
	CostMicros int64 `json:"cost_micros"`
}

// voiceSession is the session voice mode works in, its agent and project,
// and the workspace's provider.
type voiceSession struct {
	th    threads.Thread
	voice Voice
	vp    VoiceProvider
}

func (p *Platform) voiceSession(ctx context.Context, who auth.Principal, id string) (voiceSession, error) {
	th, err := p.SessionRecord(ctx, who, id)
	if err != nil {
		return voiceSession{}, err
	}
	v, vp, ok := p.workspaceVoice(who.Tenant)
	if !ok {
		return voiceSession{}, ErrVoiceOff
	}
	if err := p.speechAllowed(who.Tenant, th.Project, th.Session.Agent); err != nil {
		return voiceSession{}, err
	}
	return voiceSession{th: th, voice: v, vp: vp}, nil
}

// speechAllowed refuses a call when a spend limit over the session's
// project, its agent or the workspace is reached. It fails closed: limits
// that cannot be read refuse.
func (p *Platform) speechAllowed(tenant, project, agent string) error {
	if p.Policies == nil || p.Usage == nil {
		return nil
	}
	var within []string
	if p.Projects != nil {
		within = p.Projects.Tree(tenant).Ancestors(project)
	}
	applied, err := p.applied(tenant, chainOf(project, within), agent, "", p.now())
	if err != nil {
		return fmt.Errorf("%w: the limits could not be read", ErrVoiceSpend)
	}
	switch d := policy.Exceeded(applied, nil); d.Verdict {
	case policy.Deny, policy.NeedsApproval:
		return fmt.Errorf("%w: %s", ErrVoiceSpend, d.Reason)
	}
	return nil
}

// HearInSession transcribes what a person said in voice mode. The words come
// back to be sent as a message, or read as an answer to an approval; the
// recording is not kept.
func (p *Platform) HearInSession(ctx context.Context, who auth.Principal, id string, audio []byte) (VoiceTranscript, error) {
	if len(audio) > MaxRecordingBytes {
		return VoiceTranscript{}, speech.ErrTooLarge
	}
	mt := attachment.Sniff(audio, "")
	if attachment.KindOf(mt) != attachment.KindAudio && !speech.IsAudio(mt) {
		return VoiceTranscript{}, ErrNotAudio
	}
	vs, err := p.voiceSession(ctx, who, id)
	if err != nil {
		return VoiceTranscript{}, err
	}
	heard, err := vs.voice.Hear(ctx, bytes.NewReader(audio), mt)
	if err != nil {
		return VoiceTranscript{}, err
	}
	h := hearing{price: vs.vp.Price, label: vs.vp.Kind + "/" + vs.vp.Model, workspace: true}
	micros := h.meter(p, speechUse{tenant: who.Tenant, project: vs.th.Project, agent: vs.th.Session.Agent}, heard.Duration)
	p.addVoiceUsage(who.Tenant, id, VoiceUsage{HeardMS: heard.Duration.Milliseconds(), Micros: micros, Calls: 1, Unpriced: unpricedIf(heard.Duration <= 0 && vs.vp.Price.HearPerHour > 0)})
	return VoiceTranscript{Text: clipRunes(heard.Text, threads.MaxText), Language: heard.Language}, nil
}

func unpricedIf(b bool) int {
	if b {
		return 1
	}
	return 0
}

// SpeakInSession speaks the next part of a run's answer in a session, from
// the byte offset from. The run must be the session's own and one who can
// see. A part still being written is not spoken: the answer says Pending,
// and nothing is spent.
func (p *Platform) SpeakInSession(ctx context.Context, who auth.Principal, id, runID string, from int) (VoiceSpeech, error) {
	vs, err := p.voiceSession(ctx, who, id)
	if err != nil {
		return VoiceSpeech{}, err
	}
	run, err := p.Runs.Get(who.Tenant, runID)
	if err != nil || run.Thread != id || !p.Seer(who).Run(run) {
		return VoiceSpeech{}, fmt.Errorf("%w: run %s of session %s", threads.ErrNotFound, runID, id)
	}
	answer, final, err := p.answerSoFar(who.Tenant, run)
	if err != nil {
		return VoiceSpeech{}, err
	}
	say, next, done := nextSpoken(answer, from, final)
	if say == "" {
		return VoiceSpeech{Next: next, Pending: !done, Done: done}, nil
	}
	audio, mt, err := vs.voice.Synthesize(ctx, say)
	if err != nil {
		return VoiceSpeech{}, err
	}
	chars := utf8.RuneCountInString(say)
	micros := vs.vp.Price.Speaking(chars)
	p.meterSpeech(speechUse{tenant: who.Tenant, project: vs.th.Project, agent: vs.th.Session.Agent, label: vs.vp.Kind + "/" + vs.vp.SpeechModel, micros: micros})
	p.addVoiceUsage(who.Tenant, id, VoiceUsage{SpokenChars: int64(chars), Micros: micros, Calls: 1})
	return VoiceSpeech{Audio: audio, MediaType: mt, Text: say, Next: next, Done: done, CostMicros: micros}, nil
}

// CorrectSpoken puts right the words of a spoken message its author posted in
// a session, for every later turn; the runs it started are not changed.
func (p *Platform) CorrectSpoken(ctx context.Context, who auth.Principal, id, mid, text string) (SessionView, error) {
	if _, err := p.SessionRecord(ctx, who, id); err != nil {
		return SessionView{}, err
	}
	if _, err := p.Threads.Correct(who.Tenant, id, mid, who.Subject, strings.TrimSpace(text), p.now()); err != nil {
		if errors.Is(err, threads.ErrNotAuthor) {
			return SessionView{}, fmt.Errorf("%w: %w", auth.ErrForbidden, err)
		}
		return SessionView{}, err
	}
	return p.ReadSession(ctx, who, id, "", "", false)
}

// answerSoFar is a run's answer: its final text once it has ended, else the
// text it has streamed, read from the journal.
func (p *Platform) answerSoFar(tenant string, run runs.Run) (string, bool, error) {
	if run.Status.Terminal() {
		return run.Answer, true, nil
	}
	if p.Journal == nil {
		return "", false, nil
	}
	evs, err := p.Journal.Events(tenant, []string{run.ID})
	if err != nil {
		return "", false, err
	}
	var b strings.Builder
	for _, e := range evs {
		if t, ok := e.Data.(event.Text); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String(), false, nil
}

// voiceUsage is where each session's voice usage is kept: one record a
// session, beside the workspace's settings in the same store.
func (p *Platform) voiceUsage() recordSet {
	return recordSet{st: p.settings.rec.st, namespace: "voice"}
}

func voiceUsageKey(session string) string { return "usage:" + session }

// addVoiceUsage adds one call to a session's voice usage. A failure is
// logged: the ledger has counted the money already.
func (p *Platform) addVoiceUsage(tenant, session string, add VoiceUsage) {
	if p.settings.rec.st == nil {
		return
	}
	set := p.voiceUsage()
	err := state.Retry(func() error {
		var cur VoiceUsage
		ver, err := set.lookup(tenant, voiceUsageKey(session), &cur)
		switch {
		case errors.Is(err, state.ErrNotFound):
			ver = state.Missing
		case err != nil:
			return err
		}
		cur.HeardMS += add.HeardMS
		cur.SpokenChars += add.SpokenChars
		cur.Micros += add.Micros
		cur.Calls += add.Calls
		cur.Unpriced += add.Unpriced
		op, err := recordOp(tenant, voiceUsageKey(session), cur, ver)
		if err != nil {
			return err
		}
		return set.write(op)
	})
	if err != nil {
		p.log.Error("platform: session voice usage could not be written", "tenant", tenant, "session", session, "error", err)
	}
}

// SessionVoiceUsage is a session's voice usage, or nil when voice mode was
// never used in it.
func (p *Platform) SessionVoiceUsage(tenant, session string) *VoiceUsage {
	if p.settings.rec.st == nil {
		return nil
	}
	var u VoiceUsage
	if _, err := p.voiceUsage().lookup(tenant, voiceUsageKey(session), &u); err != nil {
		return nil
	}
	return &u
}

// nextSpoken is what to say next of an answer, from byte offset from: whole
// sentences and lines, as plain words, until about enoughSpoken runes and at
// most maxSpoken. A sentence still being written waits, unless the answer is
// final. next is where to go on from; done is a final answer said in full.
func nextSpoken(answer string, from int, final bool) (say string, next int, done bool) {
	from = max(0, min(from, len(answer)))
	pos := from
	var parts []string
	size := 0
	for {
		pos = skipSpace(answer, pos)
		if pos >= len(answer) {
			return strings.Join(parts, " "), pos, final
		}
		end, plain, ok := spokenSegment(answer, pos, final)
		if !ok {
			// The rest is still being written.
			return strings.Join(parts, " "), pos, false
		}
		n := utf8.RuneCountInString(plain)
		if size > 0 && size+n > maxSpoken {
			return strings.Join(parts, " "), pos, false
		}
		if plain != "" {
			parts = append(parts, plain)
			size += n + 1
		}
		pos = end
		if size >= enoughSpoken {
			pos = skipSpace(answer, pos)
			return strings.Join(parts, " "), pos, final && pos >= len(answer)
		}
	}
}

func skipSpace(s string, i int) int {
	for i < len(s) {
		r, n := utf8.DecodeRuneInString(s[i:])
		if !unicode.IsSpace(r) {
			break
		}
		i += n
	}
	return i
}

// spokenSegment is the piece of the answer at pos: a code block, or text up to
// the end of its sentence or line. ok is false when it is still being
// written.
func spokenSegment(s string, pos int, final bool) (end int, plain string, ok bool) {
	rest := s[pos:]
	if strings.HasPrefix(rest, "```") {
		closeAt := strings.Index(rest[3:], "```")
		if closeAt < 0 {
			if !final {
				return 0, "", false
			}
			return len(s), CodeSpoken, true
		}
		end = pos + 3 + closeAt + 3
		// The rest of the closing fence's line goes with it.
		if nl := strings.IndexByte(s[end:], '\n'); nl >= 0 {
			end += nl
		} else {
			end = len(s)
		}
		return end, CodeSpoken, true
	}
	cut := -1
	for i := 0; i < len(rest); i++ {
		c := rest[i]
		if c == '\n' {
			cut = i
			break
		}
		if (c == '.' || c == '!' || c == '?') && i+1 < len(rest) && (rest[i+1] == ' ' || rest[i+1] == '\n') {
			cut = i + 1
			break
		}
	}
	if cut < 0 {
		if !final {
			return 0, "", false
		}
		cut = len(rest)
	}
	plain = speakable(rest[:cut])
	if utf8.RuneCountInString(plain) > maxSpoken {
		// A sentence too long for one part ends this part at a word; the
		// rest of it is the next part.
		cut = wordCut(rest[:cut])
		plain = speakable(rest[:cut])
	}
	return pos + cut, plain, true
}

// wordCut is the longest prefix of s, ending at a space, that speaks in at
// most maxSpoken runes; with no such space, a hard cut at a rune.
func wordCut(s string) int {
	var spaces []int
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			spaces = append(spaces, i)
		}
	}
	fits := func(i int) bool { return utf8.RuneCountInString(speakable(s[:i])) <= maxSpoken }
	lo, hi := 0, len(spaces)
	for lo < hi {
		mid := (lo + hi) / 2
		if fits(spaces[mid]) {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo > 0 {
		return spaces[lo-1]
	}
	n, i := 0, 0
	for i < len(s) && n < maxSpoken {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
		n++
	}
	return i
}

var (
	reImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reLink     = regexp.MustCompile(`\[([^\]]+)\]\([^)]*\)`)
	reURL      = regexp.MustCompile(`https?://\S+`)
	reListMark = regexp.MustCompile(`^(\s*([-*+]|\d+[.)])\s+|#{1,6}\s+|>\s*)`)
	reMarks    = regexp.MustCompile("\\*\\*|__|~~|`")
	reEmphasis = regexp.MustCompile(`(^|[\s(])[*_]([^*_\s][^*_]*?)[*_]([\s).,!?:;]|$)`)
	reSpaces   = regexp.MustCompile(`\s+`)
)

// speakable is one line of Markdown as words to say: no marks, links by
// their text, addresses as "a link", tables left out.
func speakable(line string) string {
	t := strings.TrimSpace(line)
	if strings.HasPrefix(t, "|") || strings.Trim(t, "-=*_ ") == "" {
		return ""
	}
	t = reListMark.ReplaceAllString(t, "")
	t = reImage.ReplaceAllString(t, "")
	t = reLink.ReplaceAllString(t, "$1")
	t = reURL.ReplaceAllString(t, "a link")
	t = reMarks.ReplaceAllString(t, "")
	t = reEmphasis.ReplaceAllString(t, "$1$2$3")
	return strings.TrimSpace(reSpaces.ReplaceAllString(t, " "))
}
