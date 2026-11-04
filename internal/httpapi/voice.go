package httpapi

import (
	"errors"
	"io"
	"net/http"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/speech"
)

// Voice notes from the console: whether they are on, and a recording's
// words. The recording is transcribed with the System spec's speech kind and
// not kept; the person reads the words in the composer and sends them.
func (s *Server) voiceRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/voice", s.authed(auth.SpeechWrite, s.getVoice))
	mux.HandleFunc("POST /api/voice/transcriptions", s.authed(auth.SpeechWrite, s.transcribeVoice))
}

func (s *Server) getVoice(w http.ResponseWriter, _ *http.Request, _ auth.Principal) {
	ok(w, http.StatusOK, s.P.Voice())
}

func (s *Server) transcribeVoice(w http.ResponseWriter, r *http.Request, _ auth.Principal) {
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
	got, err := s.P.TranscribeRecording(r.Context(), audio)
	switch {
	case errors.Is(err, platform.ErrSpeechOff):
		fail(w, http.StatusNotFound, "voice notes are off in this deployment")
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
