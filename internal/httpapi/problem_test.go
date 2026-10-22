package httpapi

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
)

// A request its caller cancels, as a browser does when a page moves on, is
// not something that went wrong here: it is logged at debug, not as an
// internal error, and is not answered as a server error (5xx).
func TestACancelledRequestIsNotAnInternalError(t *testing.T) {
	var logs bytes.Buffer
	s := &Server{Log: slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))}

	w := httptest.NewRecorder()
	s.problem(w, fmt.Errorf("runs: list: %w", context.Canceled))
	if w.Code >= 500 {
		t.Errorf("a cancelled request answered %d, a server error", w.Code)
	}
	if strings.Contains(logs.String(), "level=ERROR") || strings.Contains(logs.String(), "internal error") {
		t.Errorf("a cancelled request was logged as an internal error:\n%s", logs.String())
	}
	if !strings.Contains(logs.String(), "level=DEBUG") {
		t.Errorf("a cancelled request was not logged at debug:\n%s", logs.String())
	}

	// Anything else unexplained still is an internal error.
	logs.Reset()
	w = httptest.NewRecorder()
	s.problem(w, errors.New("disk on fire"))
	if w.Code != 500 || !strings.Contains(logs.String(), "level=ERROR") {
		t.Errorf("an unexplained error = %d, logged:\n%s", w.Code, logs.String())
	}
	if strings.Contains(w.Body.String(), "disk on fire") {
		t.Errorf("an internal error leaked its detail: %s", w.Body.String())
	}
}
