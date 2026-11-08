package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/speech"
)

// Voice from the console: what voice the workspace has, a recording's
// words, and the workspace's voice provider. A recording is transcribed with
// the workspace's voice provider, else the System spec's speech kind, and
// not kept; the person reads the words in the composer and sends them. The
// voice provider is set up by whoever manages the workspace's clients: it
// holds a key, and everyone in the workspace spends it.
func (s *Server) voiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/voice", s.authed(auth.SpeechWrite, s.getVoice))
	mux.HandleFunc("POST /api/voice/transcriptions", s.authed(auth.SpeechWrite, s.transcribeVoice))
	mux.HandleFunc("GET /api/voice/provider", s.authed(auth.ClientsWrite, s.getVoiceProvider))
	mux.HandleFunc("PUT /api/voice/provider", s.authed(auth.ClientsWrite, s.putVoiceProvider))
	mux.HandleFunc("DELETE /api/voice/provider", s.authed(auth.ClientsWrite, s.deleteVoiceProvider))
	mux.HandleFunc("POST /api/voice/provider/check", s.authed(auth.ClientsWrite, s.checkVoiceProvider))
	// Voice mode in a session: what the person said, and the agent's answer
	// spoken a few sentences at a time.
	mux.HandleFunc("POST /api/sessions/{id}/voice/transcriptions", s.authed(auth.SpeechWrite, s.seenSession(s.hearInSession)))
	mux.HandleFunc("POST /api/sessions/{id}/voice/speech", s.authed(auth.SpeechWrite, s.seenSession(s.speakInSession)))
}

func (s *Server) hearInSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	audio, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		fail(w, http.StatusRequestEntityTooLarge, "the recording is too long")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, "the recording could not be read")
		return
	}
	got, err := s.P.HearInSession(r.Context(), who, r.PathValue("id"), audio)
	switch {
	case errors.Is(err, platform.ErrNotAudio):
		fail(w, http.StatusBadRequest, "the recording is not audio")
	case errors.Is(err, speech.ErrTooLarge):
		fail(w, http.StatusRequestEntityTooLarge, "the recording is too long")
	case errors.Is(err, speech.ErrUnsupported):
		fail(w, http.StatusBadRequest, "the recording is in a format the voice provider does not take")
	case errors.Is(err, speech.ErrEmpty):
		fail(w, http.StatusUnprocessableEntity, "no words were heard")
	default:
		if s.voiceRefused(w, who, err) {
			return
		}
		ok(w, http.StatusOK, got)
	}
}

// speakInput asks for the next part of a run's answer, from an offset the
// last answer gave (0 to start).
type speakInput struct {
	Run  string `json:"run"`
	From int    `json:"from"`
}

func (s *Server) speakInSession(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in speakInput
	if !decode(w, r, &in) {
		return
	}
	if in.Run == "" || in.From < 0 {
		fail(w, http.StatusBadRequest, "name the run, and an offset of 0 or more")
		return
	}
	got, err := s.P.SpeakInSession(r.Context(), who, r.PathValue("id"), in.Run, in.From)
	if s.voiceRefused(w, who, err) {
		return
	}
	ok(w, http.StatusOK, got)
}

// voiceRefused answers a voice mode call that failed, and says whether it
// did.
func (s *Server) voiceRefused(w http.ResponseWriter, who auth.Principal, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, platform.ErrVoiceOff):
		fail(w, http.StatusConflict, "voice mode is off: no voice provider is set up")
	case errors.Is(err, platform.ErrVoiceSpend):
		fail(w, http.StatusPaymentRequired, detail(err, platform.ErrVoiceSpend))
	case errors.Is(err, speech.ErrUnavailable), errors.Is(err, platform.ErrVoiceProvider):
		s.Log.Warn("httpapi: voice mode", "tenant", who.Tenant, "error", err)
		fail(w, http.StatusServiceUnavailable, "the voice provider did not answer: "+speechReason(err))
	default:
		s.problem(w, err)
	}
	return true
}

// voiceProviderView is the workspace's voice provider, if it has one, and
// the kinds it can be, with their defaults.
type voiceProviderView struct {
	Provider *platform.VoiceProvider `json:"provider,omitempty"`
	Kinds    []platform.VoiceKind    `json:"kinds"`
}

func (s *Server) getVoiceProvider(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	out := voiceProviderView{Kinds: platform.VoiceKinds()}
	if v, has := s.P.VoiceProvider(who.Tenant); has {
		out.Provider = &v
	}
	ok(w, http.StatusOK, out)
}

func (s *Server) putVoiceProvider(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var in platform.VoiceProviderInput
	if !decode(w, r, &in) {
		return
	}
	v, err := s.P.SetVoiceProvider(r.Context(), who, in)
	if errors.Is(err, platform.ErrVoiceProvider) {
		fail(w, http.StatusBadRequest, detail(err, platform.ErrVoiceProvider))
		return
	}
	if err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, v)
}

func (s *Server) deleteVoiceProvider(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.DeleteVoiceProvider(r.Context(), who); err != nil {
		s.problem(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkVoiceProvider(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	got, err := s.P.CheckVoiceProvider(r.Context(), who)
	switch {
	case errors.Is(err, platform.ErrSpeechOff):
		fail(w, http.StatusNotFound, "no voice provider is set up")
	case errors.Is(err, speech.ErrUnavailable), errors.Is(err, platform.ErrVoiceProvider):
		s.Log.Warn("httpapi: voice provider check", "tenant", who.Tenant, "error", err)
		fail(w, http.StatusBadGateway, "the voice provider did not answer: "+speechReason(err))
	case errors.Is(err, speech.ErrEmpty):
		fail(w, http.StatusBadGateway, "the voice provider spoke the sample but heard no words in it")
	case err != nil:
		s.problem(w, err)
	default:
		ok(w, http.StatusOK, got)
	}
}

// speechReason is a provider's refusal without the package prefix, which
// names no key: the speech kinds keep only the provider's own message.
func speechReason(err error) string {
	msg := err.Error()
	for _, prefix := range []string{"speech: unavailable: ", "platform: voice provider: "} {
		if i := strings.LastIndex(msg, prefix); i >= 0 {
			msg = msg[i+len(prefix):]
		}
	}
	if len(msg) > 240 {
		msg = msg[:240]
	}
	return msg
}

func (s *Server) getVoice(w http.ResponseWriter, _ *http.Request, who auth.Principal) {
	ok(w, http.StatusOK, s.P.Voice(who.Tenant))
}

func (s *Server) transcribeVoice(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	audio, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		fail(w, http.StatusRequestEntityTooLarge, "the recording is too long")
		return
	case err != nil:
		fail(w, http.StatusBadRequest, "the recording could not be read")
		return
	}
	got, err := s.P.TranscribeRecording(r.Context(), who.Tenant, audio)
	switch {
	case errors.Is(err, platform.ErrSpeechOff):
		fail(w, http.StatusNotFound, "voice is off: no voice provider is set up")
	case errors.Is(err, platform.ErrNotAudio):
		fail(w, http.StatusBadRequest, "the recording is not audio")
	case errors.Is(err, speech.ErrTooLarge):
		fail(w, http.StatusRequestEntityTooLarge, "the recording is too long")
	case errors.Is(err, speech.ErrUnsupported):
		fail(w, http.StatusBadRequest, "the recording is in a format the transcriber does not take")
	case errors.Is(err, speech.ErrEmpty):
		fail(w, http.StatusUnprocessableEntity, "no words were heard in the recording")
	case errors.Is(err, speech.ErrUnavailable):
		s.Log.Warn("httpapi: transcription", "error", err)
		fail(w, http.StatusServiceUnavailable, "transcription is unavailable right now")
	case err != nil:
		s.problem(w, err)
	default:
		ok(w, http.StatusOK, got)
	}
}
