package platform

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/speech"
	"github.com/urmzd/humagen/pkg/usage"
)

// A workspace's voice provider is the speech service an admin sets up for
// it: what hears a person in the console and speaks an agent's answers in
// voice mode. It is off until set up. Without one, voice mode is off and the
// composer's microphone uses the deployment's speech kind (the System spec's
// speech slot) or the browser's own dictation.
//
// It is kept on the workspace's settings record, apart from Settings, so
// GET /api/settings never shows it and PUT /api/settings cannot change it.
// Its key is a value kept in Secrets or an env reference this server grants
// the workspace, as a client's secrets are, and is never returned.

// ErrVoiceProvider is a voice provider that cannot be set up as given.
var ErrVoiceProvider = errors.New("platform: voice provider")

// MaxVoiceKey bounds a voice provider's key.
const MaxVoiceKey = 4096

// voiceSecretsOwner is who the key is kept under in Secrets. A client's id
// holds no slash, so it is no client's.
const voiceSecretsOwner = "voice/provider"

// VoiceSample is what a check speaks and hears back.
const VoiceSample = "Voice is set up. I can hear you and answer out loud."

// validVoiceName is a model or voice name: letters, digits and . _ / -.
var validVoiceName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]{0,79}$`)

// VoiceProvider is a workspace's voice provider. Its models, voice and
// price are filled with its kind's defaults when it is set up.
type VoiceProvider struct {
	// Kind is groq or openai.
	Kind string `json:"kind"`
	// Model transcribes; SpeechModel and Voice speak.
	Model       string       `json:"model"`
	SpeechModel string       `json:"speech_model"`
	Voice       string       `json:"voice"`
	Price       speech.Price `json:"price"`
	// KeyRef is the env reference the key is read from; KeyStored says a
	// value is kept instead. The value is never returned.
	KeyRef    string    `json:"key_ref,omitempty"`
	KeyStored bool      `json:"key_stored"`
	SetBy     string    `json:"set_by"`
	SetAt     time.Time `json:"set_at"`
}

// VoiceProviderInput sets a voice provider up. An empty model or voice is
// the kind's default, and no price is the kind's published one, or the
// price it had when the kind is unchanged. With no key and no reference, a
// provider of the same kind keeps its key.
type VoiceProviderInput struct {
	Kind        string        `json:"kind"`
	Model       string        `json:"model,omitempty"`
	SpeechModel string        `json:"speech_model,omitempty"`
	Voice       string        `json:"voice,omitempty"`
	Price       *speech.Price `json:"price,omitempty"`
	KeyRef      string        `json:"key_ref,omitempty"`
	Key         string        `json:"key,omitempty"`
}

// VoiceKind is a kind a voice provider can be, with its defaults and the
// voices it is known to offer.
type VoiceKind struct {
	Kind        string       `json:"kind"`
	Model       string       `json:"model"`
	SpeechModel string       `json:"speech_model"`
	Voice       string       `json:"voice"`
	Voices      []string     `json:"voices"`
	Price       speech.Price `json:"price"`
}

// VoiceKinds are the kinds a workspace can set up, groq first.
func VoiceKinds() []VoiceKind {
	return []VoiceKind{
		{Kind: speech.KindGroq, Model: speech.GroqTranscriptionModel, SpeechModel: speech.GroqSpeechModel, Voice: speech.GroqVoice,
			Voices: []string{"autumn", "diana", "hannah", "austin", "daniel", "troy"}, Price: speech.GroqPrice},
		{Kind: speech.KindOpenAI, Model: speech.OpenAITranscriptionModel, SpeechModel: speech.OpenAISpeechModel, Voice: speech.OpenAIVoice,
			Voices: []string{"alloy", "ash", "coral", "echo", "fable", "nova", "onyx", "sage", "shimmer"}, Price: speech.OpenAIPrice},
	}
}

// Voice is a built voice provider: it hears and it speaks.
type Voice interface {
	speech.Hearer
	speech.Synthesizer
}

// VoiceBuild builds a voice provider of a kind. Nil in Config is speech.New.
type VoiceBuild func(kind string, c speech.Config) (Voice, error)

func buildVoice(kind string, c speech.Config) (Voice, error) { return speech.New(kind, c) }

// VoiceProvider is who's workspace voice provider, and whether it has one.
func (p *Platform) VoiceProvider(tenant string) (VoiceProvider, bool) {
	if p.settings.rec.st == nil {
		// A platform built without its stores, as a unit test builds one.
		return VoiceProvider{}, false
	}
	v, _, err := p.settings.read(tenant)
	if err != nil {
		p.log.Error("platform: settings could not be read", "tenant", tenant, "error", err)
		return VoiceProvider{}, false
	}
	if v.VoiceProvider == nil {
		return VoiceProvider{}, false
	}
	return *v.VoiceProvider, true
}

// SetVoiceProvider sets who's workspace voice provider up, or replaces it.
// Only someone who may manage the workspace's clients may: it holds a key as
// a client does, and everyone in the workspace spends it.
func (p *Platform) SetVoiceProvider(ctx context.Context, who auth.Principal, in VoiceProviderInput) (VoiceProvider, error) {
	if !who.Can(auth.ClientsWrite) {
		return VoiceProvider{}, auth.ErrForbidden
	}
	tenant := who.Tenant
	for _, f := range []*string{&in.Kind, &in.Model, &in.SpeechModel, &in.Voice, &in.KeyRef, &in.Key} {
		*f = strings.TrimSpace(*f)
	}
	defaults, err := speech.New(in.Kind, speech.Config{Key: func(context.Context) (string, error) { return "", nil }})
	if err != nil {
		return VoiceProvider{}, fmt.Errorf("%w: the kind is groq or openai", ErrVoiceProvider)
	}
	for _, name := range []string{in.Model, in.SpeechModel, in.Voice} {
		if name != "" && !validVoiceName.MatchString(name) {
			return VoiceProvider{}, fmt.Errorf("%w: a model or voice is letters, digits and . _ / -, at most 80", ErrVoiceProvider)
		}
	}
	had, replacing := p.VoiceProvider(tenant)
	hear, speak := defaults.Models()
	next := VoiceProvider{Kind: in.Kind, Model: hear, SpeechModel: speak, Voice: in.Voice, Price: speech.DefaultPrice(in.Kind), SetBy: who.Subject, SetAt: p.now()}
	if in.Model != "" {
		next.Model = in.Model
	}
	if in.SpeechModel != "" {
		next.SpeechModel = in.SpeechModel
	}
	if next.Voice == "" {
		next.Voice = defaultVoice(in.Kind)
	}
	switch {
	case in.Price != nil:
		next.Price = *in.Price
	case replacing && had.Kind == in.Kind:
		next.Price = had.Price
	}
	if !next.Price.Valid() {
		return VoiceProvider{}, fmt.Errorf("%w: a price is from 0 to %d micro-USD, and the minimum at most an hour", ErrVoiceProvider, speech.MaxPrice)
	}
	switch {
	case in.Key != "" && in.KeyRef != "":
		return VoiceProvider{}, fmt.Errorf("%w: give a key or a reference to one, not both", ErrVoiceProvider)
	case len(in.Key) > MaxVoiceKey:
		return VoiceProvider{}, fmt.Errorf("%w: a key is at most %d characters", ErrVoiceProvider, MaxVoiceKey)
	case in.KeyRef != "":
		if _, ok := p.envSecret(tenant, in.KeyRef); !ok {
			return VoiceProvider{}, fmt.Errorf("%w: %w", ErrVoiceProvider, errSecretRef)
		}
		next.KeyRef = in.KeyRef
	case in.Key != "":
		next.KeyStored = true
	case replacing && had.Kind == in.Kind:
		next.KeyRef, next.KeyStored = had.KeyRef, had.KeyStored
	default:
		return VoiceProvider{}, fmt.Errorf("%w: give the provider's key, or a reference to one", ErrVoiceProvider)
	}
	if in.Key != "" {
		if p.secrets == nil {
			return VoiceProvider{}, fmt.Errorf("%w: this server keeps no secrets; give a reference instead", ErrVoiceProvider)
		}
		if err := p.secrets.Put(tenant, voiceSecretsOwner, "key", in.Key); err != nil {
			return VoiceProvider{}, err
		}
	}
	if err := p.settings.update(tenant, func(v *storedSettings) error {
		v.VoiceProvider = &next
		return nil
	}); err != nil {
		return VoiceProvider{}, err
	}
	// A key that moved from a value to a reference leaves no value behind.
	if replacing && had.KeyStored && !next.KeyStored && p.secrets != nil {
		_ = p.secrets.Drop(tenant, voiceSecretsOwner)
	}
	p.log.Info("platform: voice provider set", "tenant", tenant, "kind", next.Kind, "by", who.Subject, "replaced", replacing)
	return next, nil
}

func defaultVoice(kind string) string {
	if kind == speech.KindOpenAI {
		return speech.OpenAIVoice
	}
	return speech.GroqVoice
}

// DeleteVoiceProvider turns who's workspace voice off and forgets its key.
// Voice mode stops; the microphone falls back as if none had been set up.
func (p *Platform) DeleteVoiceProvider(ctx context.Context, who auth.Principal) error {
	if !who.Can(auth.ClientsWrite) {
		return auth.ErrForbidden
	}
	had, ok := p.VoiceProvider(who.Tenant)
	if !ok {
		return nil
	}
	if err := p.settings.update(who.Tenant, func(v *storedSettings) error {
		v.VoiceProvider = nil
		return nil
	}); err != nil {
		return err
	}
	if had.KeyStored && p.secrets != nil {
		_ = p.secrets.Drop(who.Tenant, voiceSecretsOwner)
	}
	p.log.Info("platform: voice provider removed", "tenant", who.Tenant, "by", who.Subject)
	return nil
}

// voiceKey reads the provider's key when a request is sent.
func (p *Platform) voiceKey(tenant string, v VoiceProvider) func(context.Context) (string, error) {
	return func(context.Context) (string, error) {
		switch {
		case v.KeyStored:
			if p.secrets == nil {
				return "", fmt.Errorf("%w: no secret store", ErrVoiceProvider)
			}
			k, ok := p.secrets.Get(tenant, voiceSecretsOwner, "key")
			if !ok || k == "" {
				return "", fmt.Errorf("%w: its key is no longer kept; set it again", ErrVoiceProvider)
			}
			return k, nil
		case v.KeyRef != "":
			k, ok := p.envSecret(tenant, v.KeyRef)
			if !ok {
				return "", fmt.Errorf("%w: %w", ErrVoiceProvider, errSecretRef)
			}
			return k, nil
		}
		return "", fmt.Errorf("%w: no key", ErrVoiceProvider)
	}
}

// workspaceVoice builds the workspace's voice provider, if it has one.
func (p *Platform) workspaceVoice(tenant string) (Voice, VoiceProvider, bool) {
	v, ok := p.VoiceProvider(tenant)
	if !ok {
		return nil, VoiceProvider{}, false
	}
	build := p.cfg.VoiceBuild
	if build == nil {
		build = buildVoice
	}
	built, err := build(v.Kind, speech.Config{Model: v.Model, SpeechModel: v.SpeechModel, Voice: v.Voice, Key: p.voiceKey(tenant, v)})
	if err != nil {
		p.log.Warn("platform: voice provider could not be built", "tenant", tenant, "kind", v.Kind, "error", err)
		return nil, VoiceProvider{}, false
	}
	return built, v, true
}

// hearing is what transcribes for a workspace, and what it costs.
type hearing struct {
	t     speech.Transcriber
	price speech.Price
	// label is the provider and model, for the log.
	label string
	// workspace is the workspace's own provider, which it pays for. The
	// deployment's speech kind is the deployment's, and is not counted.
	workspace bool
}

// heardBy is what transcribes for a workspace: its voice provider, else
// the deployment's speech kind. A zero t is neither.
func (p *Platform) heardBy(tenant string) hearing {
	if v, vp, ok := p.workspaceVoice(tenant); ok {
		return hearing{t: v, price: vp.Price, label: vp.Kind + "/" + vp.Model, workspace: true}
	}
	return hearing{t: p.cfg.Speech, label: "deployment"}
}

// meter counts what hearing d of audio cost, for the workspace's provider.
// A duration the provider did not say is an unpriced call, never zero.
func (h hearing) meter(p *Platform, u speechUse, d time.Duration) int64 {
	if !h.workspace {
		return 0
	}
	u.label = h.label
	if d <= 0 && h.price.HearPerHour > 0 {
		u.unpriced = true
	} else {
		u.micros = h.price.Hearing(d)
	}
	p.meterSpeech(u)
	return u.micros
}

// VoiceCheck is a check's answer: the sample spoken, and what was heard
// back from it.
type VoiceCheck struct {
	Audio      []byte `json:"audio"`
	MediaType  string `json:"media_type"`
	Heard      string `json:"heard"`
	CostMicros int64  `json:"cost_micros"`
}

// CheckVoiceProvider speaks VoiceSample with who's workspace voice provider
// and hears it back, so an admin hears the voice and knows both ways work.
// What it spends is counted.
func (p *Platform) CheckVoiceProvider(ctx context.Context, who auth.Principal) (VoiceCheck, error) {
	if !who.Can(auth.ClientsWrite) {
		return VoiceCheck{}, auth.ErrForbidden
	}
	v, vp, ok := p.workspaceVoice(who.Tenant)
	if !ok {
		return VoiceCheck{}, ErrSpeechOff
	}
	audio, mt, err := v.Synthesize(ctx, VoiceSample)
	if err != nil {
		return VoiceCheck{}, err
	}
	out := VoiceCheck{Audio: audio, MediaType: mt, CostMicros: vp.Price.Speaking(utf8.RuneCountInString(VoiceSample))}
	p.meterSpeech(speechUse{tenant: who.Tenant, label: vp.Kind + "/" + vp.SpeechModel, micros: out.CostMicros})
	heard, err := v.Hear(ctx, bytes.NewReader(audio), mt)
	if err != nil {
		return VoiceCheck{}, err
	}
	h := hearing{price: vp.Price, label: vp.Kind + "/" + vp.Model, workspace: true}
	out.Heard = heard.Text
	out.CostMicros += h.meter(p, speechUse{tenant: who.Tenant}, heard.Duration)
	return out, nil
}

// speechUse is one call to a speech provider, for the usage ledger.
type speechUse struct {
	tenant, project, agent string
	// label is the provider and model, for the log.
	label    string
	micros   int64
	unpriced bool
}

// meterSpeech records what one call to the workspace's voice provider cost
// in the usage ledger, where it counts toward the workspace's spend and its
// limits as a model call does. Each call has a key of its own and no run:
// a run's spend settles from its model calls alone, and would swallow
// speech counted under it. A call that cost nothing and is not unpriced is
// not recorded.
func (p *Platform) meterSpeech(u speechUse) {
	if (u.micros <= 0 && !u.unpriced) || p.Usage == nil {
		return
	}
	var id [8]byte
	_, _ = rand.Read(id[:])
	spend := usage.Spend{Micros: max(u.micros, 0)}
	if u.unpriced {
		spend = usage.Spend{Unpriced: 1}
	}
	_, _, err := p.Usage.Record(usage.Entry{
		Kind: usage.Spent, Key: "speech:" + hex.EncodeToString(id[:]), Tenant: u.tenant, Project: u.project, Agent: u.agent,
		At: p.now(), Spend: &spend,
	})
	if err != nil {
		p.log.Error("platform: speech usage could not be written", "tenant", u.tenant, "provider", u.label, "error", err)
	}
}
