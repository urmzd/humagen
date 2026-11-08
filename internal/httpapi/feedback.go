package httpapi

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	gh "github.com/urmzd/humagen/internal/presentation/github"
	"github.com/urmzd/orchagent/flow/event"
	"net/http"
	"strconv"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/feedback"
)

func (s *Server) putFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	var body struct {
		FindingID string          `json:"finding_id,omitempty"`
		Rating    string          `json:"rating"`
		Reason    string          `json:"reason"`
		Comment   string          `json:"comment"`
		ActAs     *platform.ActAs `json:"act_as"`
	}
	if !decode(w, r, &body) {
		return
	}
	actor := who
	if body.ActAs != nil {
		person, err := s.P.Acting(who, *body.ActAs)
		if err != nil {
			s.problem(w, err)
			return
		}
		actor = person
	}
	run, err := s.P.Runs.Get(who.Tenant, r.PathValue("id"))
	if err != nil || !actor.Can(auth.RunsRead) || !s.P.Seer(actor).Run(run) {
		notFound(w)
		return
	}
	if !run.Status.Terminal() {
		fail(w, http.StatusConflict, "feedback needs a finished run")
		return
	}
	if body.Rating != "up" && body.Rating != "down" || len(body.Comment) > 4000 || len(body.Reason) > 80 {
		fail(w, http.StatusBadRequest, "invalid feedback")
		return
	}
	if body.FindingID != "" {
		var report gh.ReviewReport
		if json.Unmarshal([]byte(gh.ReviewJSON(run.Answer)), &report) != nil || !gh.IsReviewFormat(report.Format) {
			fail(w, 400, "finding does not belong to this answer")
			return
		}
		found := false
		for _, finding := range report.Findings {
			if finding.ID == body.FindingID {
				found = true
				break
			}
		}
		if !found {
			fail(w, 400, "finding does not belong to this answer")
			return
		}
	}
	record, err := s.P.Feedback.Save(who.Tenant, feedback.Record{Run: run.ID, Subject: actor.Subject, Rating: body.Rating, Reason: body.Reason, Comment: body.Comment, FindingID: body.FindingID})
	if err != nil {
		s.problem(w, err)
		return
	}
	if err := s.P.RecordFeedbackCandidate(who.Tenant, record); err != nil {
		s.problem(w, err)
		return
	}
	reasons := []string(nil)
	if record.Reason != "" {
		reasons = []string{record.Reason}
	}
	identity := fmt.Sprintf("feedback-%x", sha256.Sum256([]byte(record.Run+"\x00"+record.Subject+"\x00"+record.FindingID+"\x00"+record.UpdatedAt.Format(time.RFC3339Nano))))
	if err := s.P.PublishFeedback(r.Context(), who.Tenant, identity, record.UpdatedAt, event.Feedback{RunID: record.Run, MessageID: record.Run, FindingID: record.FindingID, Actor: event.Actor{ID: actor.Subject, User: actor.Subject}, Rating: record.Rating, Reasons: reasons, Comment: record.Comment, Surface: "console"}); err != nil {
		s.problem(w, err)
		return
	}
	ok(w, http.StatusOK, s.shownFeedback(who, actor.Subject, []feedback.Record{record})[0])
}

// shownFeedback is feedback as it is handed out. A rater is named as the
// platform calls them, by handle and by name, and never by subject, which may
// hold an address; mine marks the rating of the person asking.
type shownFeedback struct {
	FindingID string    `json:"finding_id,omitempty"`
	Run       string    `json:"run"`
	By        string    `json:"by"`
	ByName    string    `json:"by_name"`
	Mine      bool      `json:"mine"`
	Rating    string    `json:"rating"`
	Reason    string    `json:"reason,omitempty"`
	Comment   string    `json:"comment,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Server) shownFeedback(who auth.Principal, self string, records []feedback.Record) []shownFeedback {
	names := s.P.Names(who.Tenant)
	out := make([]shownFeedback, len(records))
	for i, r := range records {
		out[i] = shownFeedback{
			FindingID: r.FindingID, Run: r.Run, By: names.Key(r.Subject), ByName: names.Of(r.Subject), Mine: r.Subject == self,
			Rating: r.Rating, Reason: r.Reason, Comment: r.Comment, UpdatedAt: r.UpdatedAt,
		}
	}
	return out
}

func (s *Server) listFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if _, err := s.P.Runs.Get(who.Tenant, r.PathValue("id")); err != nil {
		notFound(w)
		return
	}
	var after int64
	if raw := r.URL.Query().Get("after"); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != raw {
			fail(w, http.StatusBadRequest, "after is the next cursor of the page before")
			return
		}
		after = n
	}
	records, err := s.P.Feedback.List(who.Tenant, r.PathValue("id"), after)
	if err != nil {
		s.problem(w, err)
		return
	}
	next := ""
	if len(records) == feedback.MaxPage {
		next = strconv.FormatInt(records[len(records)-1].Seq, 10)
	}
	ok(w, http.StatusOK, map[string]any{"feedback": s.shownFeedback(who, who.Subject, records), "next": next})
}

func (s *Server) deleteFeedback(w http.ResponseWriter, r *http.Request, who auth.Principal) {
	if err := s.P.Feedback.Remove(who.Tenant, r.PathValue("id"), who.Subject, r.URL.Query().Get("finding_id")); err != nil {
		s.problem(w, err)
		return
	}
	s.P.RefreshGoodAsk(who.Tenant, r.PathValue("id"))
	at, err := s.P.Feedback.ChangedAt(who.Tenant, r.PathValue("id"), who.Subject)
	if err != nil {
		s.problem(w, err)
		return
	}
	if !at.IsZero() {
		finding := r.URL.Query().Get("finding_id")
		identity := fmt.Sprintf("feedback-removed-%x", sha256.Sum256([]byte(r.PathValue("id")+"\x00"+who.Subject+"\x00"+finding+"\x00"+at.Format(time.RFC3339Nano))))
		if err := s.P.PublishFeedback(r.Context(), who.Tenant, identity, at, event.Feedback{RunID: r.PathValue("id"), MessageID: r.PathValue("id"), FindingID: finding, Actor: event.Actor{ID: who.Subject, User: who.Subject}, Removed: true, Surface: "console"}); err != nil {
			s.problem(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}
