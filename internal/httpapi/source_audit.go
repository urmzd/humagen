package httpapi

import (
	"errors"
	"net/http"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/knowledge"
)

type sourceSnapshotKey struct{}
type sourceReadSnapshot struct {
	Tenant string
	Source knowledge.Source
}

func (s *Server) sourceSnapshot(who auth.Principal, r *http.Request) (knowledge.Source, error) {
	if snapshot, ok := r.Context().Value(sourceSnapshotKey{}).(sourceReadSnapshot); ok && snapshot.Tenant == who.Tenant && snapshot.Source.Name == r.PathValue("name") {
		return snapshot.Source, nil
	}
	return s.P.SourceAs(who, r.PathValue("name"))
}

type sourceMutationWriter struct {
	http.ResponseWriter
	status int
}

func (w *sourceMutationWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}
func (w *sourceMutationWriter) Write(bytes []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(bytes)
}

// auditedSource observes an already-authorized source mutation. Handlers use
// this same request snapshot in the store's atomic scope guard.
func (s *Server) auditedSource(action string, next func(http.ResponseWriter, *http.Request, auth.Principal)) func(http.ResponseWriter, *http.Request, auth.Principal) {
	return func(w http.ResponseWriter, r *http.Request, who auth.Principal) {
		source, err := s.sourceSnapshot(who, r)
		if err != nil {
			s.problem(w, err)
			return
		}
		project := ""
		if source.Lifecycle != nil {
			project = source.Lifecycle.Project
		}
		attempt, err := s.P.BeginAudit(who, audit.Change{Action: action, Resource: "source", ID: source.Name, Scope: "project", Project: project, After: map[string]string{"route": r.Pattern, "rule": "knowledge:write"}})
		if err != nil {
			s.problem(w, err)
			return
		}
		response := &sourceMutationWriter{ResponseWriter: w}
		next(response, r, who)
		var result error
		if response.status >= 400 {
			result = errors.New("source mutation was refused")
		}
		if err := attempt.Finish(result); err != nil {
			s.Log.Error("source audit completion remains pending", "audit", attempt.ID(), "error", err)
		}
	}
}
