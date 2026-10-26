package platform

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/memory"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

type OutcomeLearningPreview struct {
	Run          string        `json:"run"`
	Snapshot     string        `json:"snapshot"`
	Scope        library.Scope `json:"scope"`
	Outcome      runs.Outcome  `json:"outcome"`
	Project      string        `json:"project,omitempty"`
	ProjectTitle string        `json:"project_title,omitempty"`
	MayProject   bool          `json:"may_project"`
	CanDraft     bool          `json:"can_draft"`
	CanRemember  bool          `json:"can_remember"`
}
type OutcomeSkillDraft struct {
	Snapshot   string             `json:"snapshot"`
	Scope      library.Scope      `json:"scope,omitempty"`
	Definition library.Definition `json:"definition"`
}
type OutcomeDecisionMemory struct {
	Snapshot string        `json:"snapshot"`
	Scope    library.Scope `json:"scope,omitempty"`
	Decision int           `json:"decision"`
	Text     string        `json:"text"`
}
type learningState struct {
	preview     OutcomeLearningPreview
	run         runs.Run
	thread      threads.Thread
	tree        *projects.Tree
	channel     projects.Channel
	runSnapshot string
}

// Learning uses an ordinary human read. Break-glass visibility never grants
// permission to copy somebody else's private work into a new scope.
func (p *Platform) learningState(who auth.Principal, id string, scope library.Scope) (learningState, error) {
	var state learningState
	if who.Kind != auth.User || !who.Can(auth.RunsRead) {
		return state, runs.ErrNotFound
	}
	run, err := p.Runs.Get(who.Tenant, id)
	if err != nil {
		return state, runs.ErrNotFound
	}
	seer := p.Seer(who)
	seer.govern, seer.roles = false, seer.own
	if !seer.Run(run) {
		return state, runs.ErrNotFound
	}
	if run.Status != runs.Completed || run.Outcome == nil || run.Outcome.Validate() != nil {
		return state, runs.ErrLearningConflict
	}
	thread, err := p.ThreadRecord(who.Tenant, run.Thread)
	if err != nil {
		return state, runs.ErrNotFound
	}
	// Use the same membership snapshot that granted the ordinary source read.
	// GuardSnapshot rejects a revocation between that read and the domain write.
	tree := seer.tree
	project, exists := tree.Get(thread.Project)
	if !exists {
		return state, runs.ErrNotFound
	}
	reader := ReaderOf(who)
	reader.Govern = false
	mayProject := project.Name != "" && project.PersonalOwner == "" && !project.Archived && tree.Resolve(project.Name, reader).Role == projects.Owner && p.MayChangeNotes(who, project.Name) == nil
	if scope.Kind == "" {
		scope.Kind = "personal"
	}
	switch scope.Kind {
	case "personal":
		if scope.Subject != "" && scope.Subject != who.Subject || scope.Project != "" {
			return state, library.ErrNotFound
		}
		scope.Subject = who.Subject
	case "project":
		if scope.Subject != "" || scope.Project != "" && projects.NameOf(scope.Project) != project.Name {
			return state, library.ErrInvalid
		}
		if !mayProject {
			return state, projects.ErrForbidden
		}
		scope.Project = project.Name
	default:
		return state, library.ErrInvalid
	}
	channel, err := p.Projects.Channel(who.Tenant, thread.Project, thread.Channel)
	if err != nil {
		return state, err
	}
	shown := p.ShownRun(who.Tenant, run)
	preview := OutcomeLearningPreview{Run: id, Scope: scope, Outcome: *shown.Outcome, Project: project.Name, ProjectTitle: project.Title, MayProject: mayProject, CanDraft: who.Can(auth.ProjectsWrite), CanRemember: who.Can(auth.ProjectsWrite) && hasReviewedDecision(shown.Outcome.Decisions)}
	if preview.ProjectTitle == "" {
		preview.ProjectTitle = project.Name
	}
	runSnapshot := runs.LearningSnapshot(run)
	if runSnapshot == "" {
		return state, runs.ErrLearningConflict
	}
	stamp := struct {
		Run, Subject string
		Scope        library.Scope
		Thread       threads.Thread
		Chain        []projects.Project
		Channel      projects.Channel
	}{runSnapshot, who.Subject, scope, thread, tree.Chain(thread.Project), channel}
	raw, err := json.Marshal(stamp)
	if err != nil {
		return state, err
	}
	sum := sha256.Sum256(raw)
	preview.Snapshot = hex.EncodeToString(sum[:])
	return learningState{preview: preview, run: run, thread: thread, tree: tree, channel: channel, runSnapshot: runSnapshot}, nil
}

func (p *Platform) OutcomeLearning(who auth.Principal, id string, scope library.Scope) (OutcomeLearningPreview, error) {
	run, err := p.Runs.Get(who.Tenant, id)
	if err != nil {
		return OutcomeLearningPreview{}, runs.ErrNotFound
	}
	mu := p.moves.of(who.Tenant, run.Thread)
	mu.RLock()
	defer mu.RUnlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	state, err := p.learningState(who, id, scope)
	return state.preview, err
}

func (p *Platform) guardLearning(who auth.Principal, state learningState, snapshot string, commit func() error) error {
	if snapshot == "" || snapshot != state.preview.Snapshot {
		return runs.ErrLearningConflict
	}
	return p.Projects.GuardSnapshot(who.Tenant, state.tree, state.channel, func() error {
		return p.Runs.GuardLearning(who.Tenant, state.run.ID, state.runSnapshot, func(runs.Run) error { return commit() })
	})
}

func (p *Platform) learningAudit(who auth.Principal, state learningState, action string, after map[string]string) (*AuditAttempt, error) {
	change := audit.Change{Route: "POST /api/runs/{id}/" + action, Rule: "projects:write; ordinary human source access; destination owner", Action: "outcome." + action, Resource: "run", ID: state.run.ID, Scope: "project", Project: state.thread.Project, Thread: state.thread.ID, After: after}
	if state.thread.Visibility == threads.Private || state.preview.Scope.Kind == "personal" {
		change.Scope, change.Subject = "personal", who.Subject
	}
	return p.BeginAudit(who, change)
}

// DraftOutcomeSkill creates only a reviewed, tool-free draft. Existing native
// test, publication and promotion rules remain the sole path to an active skill.
func (p *Platform) DraftOutcomeSkill(who auth.Principal, id string, in OutcomeSkillDraft) (library.Revision, error) {
	run, err := p.Runs.Get(who.Tenant, id)
	if err != nil {
		return library.Revision{}, runs.ErrNotFound
	}
	mu := p.moves.of(who.Tenant, run.Thread)
	mu.RLock()
	defer mu.RUnlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	state, err := p.learningState(who, id, in.Scope)
	if err != nil {
		return library.Revision{}, err
	}
	if !state.preview.CanDraft {
		return library.Revision{}, projects.ErrForbidden
	}
	if !in.Definition.Valid() || len(in.Definition.Tools) > 0 {
		return library.Revision{}, library.ErrInvalid
	}
	if in.Snapshot != state.preview.Snapshot {
		return library.Revision{}, runs.ErrLearningConflict
	}
	in.Definition.Tools = []string{}
	attempt, err := p.learningAudit(who, state, "skill-draft", map[string]string{"skill": library.ID(state.preview.Scope, in.Definition.Name), "scope": state.preview.Scope.Kind})
	if err != nil {
		return library.Revision{}, err
	}
	var result library.Revision
	err = p.guardLearning(who, state, in.Snapshot, func() error {
		old, find := p.SkillLibrary.Get(who.Tenant, state.preview.Scope, in.Definition.Name, 0)
		if find == nil && old.OriginRun == id && old.By == who.Subject && old.Version == 1 && old.PublishedAt.IsZero() && reflect.DeepEqual(old.Definition, in.Definition) {
			result = old
			return nil
		}
		var write error
		result, write = p.SkillLibrary.Draft(who.Tenant, who.Subject, state.preview.Scope, in.Definition, 0, nil, p.now(), id)
		return write
	})
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	if err != nil {
		return library.Revision{}, err
	}
	return p.LibraryView(who, result), nil
}

// RememberOutcomeDecision is the human approval itself, restricted to an exact
// reviewed body and current owner. It cannot authorize a tool or external action.
func (p *Platform) RememberOutcomeDecision(who auth.Principal, id string, in OutcomeDecisionMemory) (memory.Note, error) {
	run, err := p.Runs.Get(who.Tenant, id)
	if err != nil {
		return memory.Note{}, runs.ErrNotFound
	}
	mu := p.moves.of(who.Tenant, run.Thread)
	mu.RLock()
	defer mu.RUnlock()
	p.admission.Lock()
	defer p.admission.Unlock()
	state, err := p.learningState(who, id, in.Scope)
	if err != nil {
		return memory.Note{}, err
	}
	if !state.preview.CanRemember {
		return memory.Note{}, projects.ErrForbidden
	}
	if in.Snapshot != state.preview.Snapshot {
		return memory.Note{}, runs.ErrLearningConflict
	}
	text := strings.TrimSpace(in.Text)
	if in.Decision < 0 || in.Decision >= len(state.preview.Outcome.Decisions) || strings.TrimSpace(state.preview.Outcome.Decisions[in.Decision]) == "" || text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > memory.MaxChars {
		return memory.Note{}, memory.ErrInvalid
	}
	owner := memory.PersonalOwner(who.Subject)
	if state.preview.Scope.Kind == "project" {
		owner = memory.ProjectOwner(state.preview.Scope.Project)
	}
	bodyHash := sha256.Sum256([]byte(text))
	attempt, err := p.learningAudit(who, state, "decision-memory", map[string]string{"scope": state.preview.Scope.Kind, "decision": fmt.Sprint(in.Decision), "review_sha256": hex.EncodeToString(bodyHash[:])})
	if err != nil {
		return memory.Note{}, err
	}
	var result memory.Note
	err = p.guardLearning(who, state, in.Snapshot, func() error {
		var write error
		result, write = p.Memory.RememberDecision(who.Tenant, owner, text, who.Subject, p.now())
		return write
	})
	if completion := attempt.Finish(err); completion != nil {
		p.LogAuditCompletionFailure(completion)
	}
	return result, err
}

func hasReviewedDecision(decisions []string) bool {
	for _, decision := range decisions {
		if strings.TrimSpace(decision) != "" {
			return true
		}
	}
	return false
}
