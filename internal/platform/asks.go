package platform

import (
	"cmp"
	"context"

	"github.com/urmzd/humagen/fiber/pkg/event"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/runs"
)

// A question a run in a thread waits on is read by everyone in the thread,
// and answered by the people it is for. So a thread says, to each reader,
// whether they may answer it, and whom it is for otherwise, and a surface
// offers the answer only to the people it is for: nobody types an answer
// the platform would refuse.

// Ask is a question a run waits on, as one reader stands to it.
type Ask struct {
	// Question is the question's id: question.id on the run.
	Question string `json:"question"`
	// Yours says the reader may answer it, as answering would check them.
	Yours bool `json:"yours"`
	// For is whom it is for: ForAnyone, ForPerson, ForExperts,
	// ForAnswerers, ForOwners or ForNobody.
	For string `json:"for"`
	// OwnersOf is what the project whose owners it is for, for ForOwners, is
	// called: its title, else its name. Absent for the workspace, and for a
	// project the reader may not see, which is never named.
	OwnersOf string `json:"owners_of,omitempty"`
	// Topic is the experts' topic, for ForExperts. Empty is every expert.
	Topic string `json:"topic,omitempty"`
	// Person is the person it is for, by handle, for ForPerson.
	Person string `json:"person,omitempty"`
}

// AsksIn says, of each run in turns parked on a question a person answers,
// whether who may answer it and whom it is for. The turns are a page the
// reader may read, so whom a question is for names nobody they cannot see:
// the person a run acts for asked in the thread, and an expert topic is the
// project's.
func (p *Platform) AsksIn(ctx context.Context, who auth.Principal, turns []ThreadTurn) []ThreadTurn {
	var s *shower
	for i, t := range turns {
		if t.Run == nil || t.Run.Status != runs.Parked || t.Run.Question == nil {
			continue
		}
		if s == nil {
			s = p.shower(who.Tenant)
		}
		if ask, ok := p.askOf(ctx, who, *t.Run, *t.Run.Question, s); ok {
			turns[i].Ask = &ask
		}
	}
	return turns
}

// askOf is the question a run waits on, as who stands to it. ok is false for
// a question no person answers, such as a delegation.
func (p *Platform) askOf(ctx context.Context, who auth.Principal, run runs.Run, q event.Question, s *shower) (Ask, bool) {
	if _, _, ok := questionParts(q.Kind); !ok || q.ID == "" {
		return Ask{}, false
	}
	// The message as it was raised decides who may answer, as answering
	// checks it. Until it is raised, the one it will be raised as.
	built, to := p.questionMessage(ctx, run, q, "")
	m, raised := p.Inbox.ByKey(run.Tenant, questionKey(run.ID, q.ID))
	if !raised {
		m = built
	}
	ask := Ask{Question: q.ID, For: to.kind, Topic: to.topic}
	if to.kind == ForPerson {
		ask.Person = s.key(to.person)
	}
	if to.kind == ForOwners && to.project != "" && p.Seer(who).Project(to.project) {
		if pr, ok := p.ProjectTree(who.Tenant).Get(to.project); ok {
			ask.OwnersOf = cmp.Or(pr.Title, pr.Name)
		}
	}
	i := -1
	for n, part := range m.Parts {
		if part.Question != nil {
			i = n
			break
		}
	}
	if i < 0 || !who.Can(auth.RunsWrite) {
		return ask, true
	}
	// An answer as plain as a surface offers: an approval as it was asked,
	// a choice of its first option, or words.
	a := QuestionAnswer{Action: ActReply, Text: "?"}
	switch q.Kind {
	case event.QuestionApproval:
		a = QuestionAnswer{Action: ActApprove}
	case event.QuestionChoice:
		a = QuestionAnswer{Action: ActChoose, Choice: firstOf(q.Options)}
	}
	ask.Yours = p.checkAnswer(ctx, who, readerOf(who), m, *m.Parts[i].Question, a, false) == nil
	return ask, true
}

func firstOf(options []string) string {
	if len(options) == 0 {
		return ""
	}
	return options[0]
}
