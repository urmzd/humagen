package platform

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/session"
)

// Past work is filed into sessions once (humagen migrate sessions), so it
// is listed in Recent and Sessions as work from now on is:
//
//   - A GitHub run from before GitHub sessions is filed in the session of
//     its issue or pull request, the one a mention there opens now: the same
//     place (githubPlace), the same record (openGitHubSession), its owner the
//     agent that answered there first, and private to the people the client
//     maps the askers' logins to now. Its conversation's runs move into the
//     session's, oldest first, and the owner's are bound to its record.
//   - A conversation from before sessions (a "th-" thread, a Slack thread or
//     direct message among them) is left as it is. A session's id is its
//     key's (s_...), and the conversation's messages, the runs filed in it,
//     its surface's ids and every link to it are kept under its own id, so it
//     cannot become a session without a new id that breaks them. A Slack
//     reply there keeps reaching it (ErrSurfaceThreadBeforeSessions).
//
// A session the backfill opens takes the times of its work: started when
// its first run was asked, and last active when its latest run or message
// was. A session the backfill finds is dated the same way when its record
// was created after the first run filed in it had ended: one an earlier
// backfill opened with the time it ran, and one a mention opened where
// older runs are filed too. An archived session is dated too. A session
// whose first run is the mention that opened it is left as it is, and so is
// one whose first run is still queued or running. A session whose runs
// carry no time is reported and not dated, and a deleted session's runs
// are left.
//
// Nothing is written unless apply is set, and a second apply writes
// nothing: each step finds what the first one did.

// Backfill kinds, the keys of BackfillReport.Counts.
const (
	BackfillSessionOpened    = "github session opened"
	BackfillSessionFound     = "github session found"
	BackfillRunFiled         = "github run filed"
	BackfillRunAlreadyFiled  = "github run already filed"
	BackfillRunBound         = "github run bound to its session"
	BackfillParticipantAdded = "github participant added"
	BackfillSessionDated     = "github session dated from its runs"
	BackfillSessionUndated   = "github session not dated"
	BackfillRunLeft          = "github run left"
	BackfillThreadLeft       = "conversation left"
	BackfillSlackThreadLeft  = "slack conversation left"
)

// BackfillReport is what a backfill did, or with Apply unset would do, in
// one tenant: one line for each change or each thing it left, and how many
// of each kind.
type BackfillReport struct {
	Tenant string `json:"tenant"`
	Apply  bool   `json:"apply"`
	// Runs is how many runs of the tenant it read.
	Runs int `json:"runs"`
	// Warnings are what an operator should fix before an apply, such as a
	// GitHub client that maps no logins to people.
	Warnings []string       `json:"warnings"`
	Lines    []BackfillLine `json:"lines"`
	Counts   map[string]int `json:"counts"`
}

// BackfillLine is one change, or one thing left and why.
type BackfillLine struct {
	Kind string `json:"kind"`
	// Subject is the run, conversation or session the line is about, and
	// Session the session it is filed in.
	Subject string `json:"subject"`
	Session string `json:"session,omitempty"`
	Detail  string `json:"detail,omitempty"`
}

func (r *BackfillReport) add(kind, subject, session, detail string) {
	r.Lines = append(r.Lines, BackfillLine{Kind: kind, Subject: subject, Session: session, Detail: detail})
	r.Counts[kind]++
}

// Changes is how many writes the report holds: every line but those that
// found what was there or left something.
func (r BackfillReport) Changes() int {
	n := 0
	for kind, c := range r.Counts {
		switch kind {
		case BackfillSessionOpened, BackfillRunFiled, BackfillRunBound, BackfillParticipantAdded, BackfillSessionDated:
			n += c
		}
	}
	return n
}

// githubBackfill is one past GitHub run and where it is to be filed.
type githubBackfill struct {
	run    runs.Run
	place  string
	sf     threads.Surface
	person string
}

// BackfillSessions files a tenant's past work into sessions, as the
// comment above says: with apply unset it reads only, and reports what it
// would do.
func (p *Platform) BackfillSessions(ctx context.Context, tenant string, apply bool) (BackfillReport, error) {
	rep := BackfillReport{Tenant: tenant, Apply: apply, Warnings: []string{}, Lines: []BackfillLine{}, Counts: map[string]int{}}
	if strings.TrimSpace(tenant) == "" {
		return rep, fmt.Errorf("%w: a backfill names its tenant", threads.ErrInvalid)
	}
	if err := p.backfillGitHub(ctx, tenant, apply, &rep); err != nil {
		return rep, err
	}
	if err := p.backfillThreads(tenant, &rep); err != nil {
		return rep, err
	}
	return rep, nil
}

// backfillGitHub files the tenant's past GitHub runs in their sessions.
func (p *Platform) backfillGitHub(ctx context.Context, tenant string, apply bool, rep *BackfillReport) error {
	var past []runs.Run
	if err := p.Runs.Each(tenant, func(r runs.Run) bool {
		rep.Runs++
		if r.Origin == runs.OriginGitHub && r.Parent == "" {
			past = append(past, r)
		}
		return true
	}); err != nil {
		return err
	}
	p.warnGitHubClients(tenant, past, rep)
	// Oldest first, as they were asked: the first decides the owner.
	slices.SortStableFunc(past, func(a, b runs.Run) int { return a.CreatedAt.Compare(b.CreatedAt) })
	groups := map[string][]githubBackfill{}
	var order []string
	for _, r := range past {
		tag := r.Tag()
		place, sf, ok := githubPlace(tag, r.Client)
		switch {
		case !ok:
			rep.add(BackfillRunLeft, r.ID, "", "no mention on an issue or pull request started it, as an automation or the automatic review starts one")
			continue
		case r.UsesPrivateContext():
			// Filed with the others who asked there, it would be read by them.
			rep.add(BackfillRunLeft, r.ID, "", "it used its asker's private context")
			continue
		case r.Thread != r.ID && !strings.HasPrefix(r.Thread, "s_"):
			rep.add(BackfillRunLeft, r.ID, "", "it is in conversation "+r.Thread)
			continue
		}
		key := sf.Client + "\x00" + sf.Channel + "\x00" + sf.TS
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], githubBackfill{run: r, place: place, sf: sf, person: p.backfillPerson(tenant, r)})
	}
	// The session each place opens, by id, and the place: two clients on
	// one repository with one owner name the same session, which only the
	// first may hold (errGitHubSessionElsewhere).
	claimed := map[string]string{}
	for _, key := range order {
		if err := p.backfillGitHubPlace(ctx, tenant, apply, groups[key], claimed, rep); err != nil {
			return err
		}
	}
	return nil
}

// warnGitHubClients warns of each GitHub client past runs came through
// that maps no login to a person: their sessions would get no people, and
// runs every member read until now would be read by administrators alone.
func (p *Platform) warnGitHubClients(tenant string, past []runs.Run, rep *BackfillReport) {
	var ids []string
	for _, r := range past {
		if r.Client != "" && !slices.Contains(ids, r.Client) {
			ids = append(ids, r.Client)
		}
	}
	slices.Sort(ids)
	for _, id := range ids {
		c, err := p.Clients.Get(tenant, id)
		switch {
		case err != nil:
			rep.Warnings = append(rep.Warnings, "GitHub client "+id+" is not in the store: its askers map to nobody, so its sessions get no people")
		case c.GitHub == nil || len(c.GitHub.People) == 0:
			rep.Warnings = append(rep.Warnings, "GitHub client "+id+" maps no GitHub login to a person (github.people, HUMAGEN_GITHUB_PEOPLE): its sessions get no people, and its past runs would be read by administrators alone. Start a server with the map first")
		case !c.Enabled:
			rep.Warnings = append(rep.Warnings, "GitHub client "+id+" is turned off: its askers map to nobody, so its sessions get no people")
		}
	}
}

// backfillPerson is the person a past GitHub run's asker is now: the
// client's map of their login, as a mention now is read (surfacePerson).
// A client removed or turned off maps nobody.
func (p *Platform) backfillPerson(tenant string, r runs.Run) string {
	c, err := p.Clients.Get(tenant, r.Client)
	if err != nil {
		return ""
	}
	return p.surfacePerson(c, r.Tag())
}

// backfillGitHubPlace files the past runs asked at one issue or pull
// request, oldest first, in its session.
func (p *Platform) backfillGitHubPlace(ctx context.Context, tenant string, apply bool, past []githubBackfill, claimed map[string]string, rep *BackfillReport) error {
	first := past[0]
	th, found, err := p.Threads.BySurface(tenant, first.sf)
	if err != nil {
		return err
	}
	// A session is filed where its first run is; a run filed elsewhere stays
	// where it is.
	project := first.run.Project
	owner := first.run.Agent
	id := ""
	if found {
		if th.Session == nil || !strings.HasPrefix(th.Session.Parent, "github://") {
			for _, b := range past {
				rep.add(BackfillRunLeft, b.run.ID, th.ID, "its issue or pull request is conversation "+th.ID+", which is not a session")
			}
			return nil
		}
		if th.Archive != nil && th.Archive.Deleted != nil {
			// Its record is kept as a marker and nothing is filed in it.
			for _, b := range past {
				rep.add(BackfillRunLeft, b.run.ID, th.ID, "the session of "+first.place+" was deleted")
			}
			return nil
		}
		id, owner, project = th.ID, th.Session.Agent, th.Project
		rep.add(BackfillSessionFound, first.place, id, "owned by "+owner)
	} else {
		channel := cmp.Or(first.run.Channel, runs.DefaultChannel)
		if ch, err := p.Projects.Channel(tenant, project, channel); err != nil || ch.Archived {
			for _, b := range past {
				rep.add(BackfillRunLeft, b.run.ID, "", fmt.Sprintf("its channel %q #%s cannot hold a session now", project, channel))
			}
			return nil
		}
		id = session.Key{Tenant: tenant, Parent: first.place, Agent: owner}.Normalize().ID()
		here := first.sf.Client + "/" + first.sf.Channel + "/" + first.sf.TS
		held := claimed[id] != "" && claimed[id] != here
		if !held {
			if _, err := p.Threads.Get(tenant, id); err == nil {
				held = true
			} else if !errors.Is(err, threads.ErrNotFound) {
				return err
			}
		}
		if held {
			// What a mention there does now: the run stays its own
			// conversation (errGitHubSessionElsewhere).
			for _, b := range past {
				rep.add(BackfillRunLeft, b.run.ID, id, "another client on this repository holds the session of "+first.place+" for "+owner)
			}
			return nil
		}
		claimed[id] = here
		rep.add(BackfillSessionOpened, first.place, id, fmt.Sprintf("owned by %s, filed in %q, private to the people who asked", owner, project))
		if apply {
			tag := wire.Clone(first.run.Tag())
			actor := wire.ActorOf(tag)
			actor.User = first.person
			wire.SetActor(&tag, actor)
			if th, err = p.openGitHubSession(ctx, tag, project, first.place, first.sf, owner, first.person); errors.Is(err, errGitHubSessionElsewhere) {
				// Opened by another client since the place was read.
				for _, b := range past {
					rep.add(BackfillRunLeft, b.run.ID, id, "another client on this repository holds the session of "+first.place+" for "+owner)
				}
				return nil
			} else if err != nil {
				return fmt.Errorf("backfill: open the session of %s: %w", first.place, err)
			}
			if th.ID != id {
				return fmt.Errorf("backfill: the session of %s is %s, not %s", first.place, th.ID, id)
			}
		} else {
			th = threads.Thread{ID: id, Project: project, CreatedBy: first.person, Session: &threads.SessionIdentity{Agent: owner, Parent: first.place}}
		}
	}
	// When the session's work was done, read before anything moves.
	var span workSpan
	if found {
		if err := p.spanConversation(tenant, id, &span); err != nil {
			return err
		}
		if m, err := p.Threads.Last(tenant, id); err == nil {
			span.active(m.At)
		} else if !errors.Is(err, threads.ErrNotFound) {
			return err
		}
	}
	var firstFiled *runs.Run
	// The runtime opened the same record for many of these runs when they
	// ran, and bound them to it.
	bound := map[string]bool{}
	ids, err := p.Sessions.Runs(ctx, tenant, id, session.MaxPage)
	if err != nil && !errors.Is(err, session.ErrNotFound) {
		return err
	}
	for _, r := range ids {
		bound[r] = true
	}
	var bind []runs.Run
	people := []string{}
	for _, pt := range th.Participants {
		if pt.Kind == threads.Person {
			people = append(people, pt.Name)
		}
	}
	for _, b := range past {
		r := b.run
		if r.Thread != r.ID && r.Thread != id {
			rep.add(BackfillRunLeft, r.ID, id, "it is in conversation "+r.Thread)
			continue
		}
		if r.Project != project {
			rep.add(BackfillRunLeft, r.ID, id, fmt.Sprintf("it is filed in %q and its session in %q", r.Project, project))
			continue
		}
		if firstFiled == nil {
			firstFiled = &b.run
		}
		span.add(r.CreatedAt, r.UpdatedAt)
		if r.Thread != id {
			if err := p.spanConversation(tenant, r.Thread, &span); err != nil {
				return err
			}
		}
		if r.Thread == id {
			rep.add(BackfillRunAlreadyFiled, r.ID, id, "")
		} else {
			rep.add(BackfillRunFiled, r.ID, id, "asked by "+cmp.Or(b.person, "nobody the client maps"))
			if apply {
				if _, err := p.Runs.Refile(tenant, r.Thread, id, cmp.Or(wire.GitHubTitleOf(r.Tag()), wire.GitHubTitleOf(first.run.Tag()))); err != nil {
					return fmt.Errorf("backfill: file %s in %s: %w", r.ID, id, err)
				}
			}
		}
		// The owner's runs are its session's conversation; another agent's
		// keeps one of its own there, as it does when asked there now.
		if strings.EqualFold(strings.TrimSpace(r.Agent), owner) && !bound[r.ID] {
			bound[r.ID] = true
			bind = append(bind, r)
			rep.add(BackfillRunBound, r.ID, id, "")
		}
		if b.person == "" || b.person == th.CreatedBy || slices.Contains(people, b.person) {
			continue
		}
		people = append(people, b.person)
		rep.add(BackfillParticipantAdded, b.person, id, "asked in "+r.ID)
		if apply {
			cur, err := p.Threads.Get(tenant, id)
			if err != nil {
				return err
			}
			pt := threads.Participant{Kind: threads.Person, Name: b.person, At: p.now()}
			if _, err := p.Threads.Participants(tenant, id, cur.Version, []threads.Participant{pt}, nil, func(threads.Participant) bool { return true }, "github", p.now()); err != nil {
				return fmt.Errorf("backfill: add %s to %s: %w", b.person, id, err)
			}
		}
	}
	if apply && len(bind) > 0 {
		if err := p.bindInOrder(ctx, tenant, id, ids, bind); err != nil {
			return fmt.Errorf("backfill: bind runs to %s: %w", id, err)
		}
	}
	// A session found is dated only when the first run filed in it has
	// ended and its record was created after that. A mention opens its
	// session before its own run starts, and the servers keep running while
	// the backfill does, so a session whose first run is the mention that
	// opened it, still queued or running or ended since, is left as it is.
	// One a mention opened where older runs are filed too was created after
	// the first of those ended, and is dated from them. An archived session
	// is dated as any other; a deleted one was left above.
	if firstFiled == nil || (found && (!firstFiled.Status.Terminal() || !th.CreatedAt.After(firstFiled.UpdatedAt))) {
		return nil
	}
	if span.created.IsZero() {
		rep.add(BackfillSessionUndated, first.place, id, "none of its runs says when it was asked")
		return nil
	}
	detail := "started " + stampOf(span.created) + ", last active " + stampOf(span.updated)
	if found {
		detail += "; was started " + stampOf(th.CreatedAt) + ", last active " + stampOf(th.UpdatedAt)
	}
	rep.add(BackfillSessionDated, first.place, id, detail)
	if apply {
		if _, err := p.Threads.Redate(tenant, id, span.created, span.updated); err != nil {
			return fmt.Errorf("backfill: date %s: %w", id, err)
		}
	}
	return nil
}

// workSpan is when a session's work was done: the first run's start and
// the latest time a run or a message changed.
type workSpan struct{ created, updated time.Time }

func (s *workSpan) add(created, updated time.Time) {
	if !created.IsZero() && (s.created.IsZero() || created.Before(s.created)) {
		s.created = created
	}
	s.active(updated)
}

func (s *workSpan) active(at time.Time) {
	if at.After(s.updated) {
		s.updated = at
	}
}

// spanConversation adds what the run index holds of a conversation's
// times, nothing when it holds no run of it.
func (p *Platform) spanConversation(tenant, id string, span *workSpan) error {
	sum, err := p.Runs.ConversationSummary(tenant, id)
	if err != nil {
		return err
	}
	if sum.ID != "" {
		span.add(sum.CreatedAt, sum.UpdatedAt)
	}
	return nil
}

// stampOf is a time as the report shows it, to the nanosecond, so two
// times that differ by less than a second never read the same.
func stampOf(at time.Time) string { return at.UTC().Format(time.RFC3339Nano) }

// orderedBinder binds runs to a session in the order they were asked
// (sessionstate.Store.BindRunsInOrder).
type orderedBinder interface {
	BindRunsInOrder(ctx context.Context, tenant, id string, runs []string, later func(a, b string) bool) error
}

// bindInOrder binds past runs to a session, each ahead of the runs already
// bound that were asked after it, so the session's latest run
// (Sessions.Runs) stays its newest.
func (p *Platform) bindInOrder(ctx context.Context, tenant, id string, bound []string, past []runs.Run) error {
	o, ok := p.Sessions.(orderedBinder)
	if !ok {
		return errors.New("the session store cannot bind runs in order")
	}
	at := map[string]time.Time{}
	known, err := p.Runs.Many(tenant, bound)
	if err != nil {
		return err
	}
	for id, r := range known {
		at[id] = r.CreatedAt
	}
	ids := make([]string, len(past))
	for i, r := range past {
		ids[i], at[r.ID] = r.ID, r.CreatedAt
	}
	return o.BindRunsInOrder(ctx, tenant, id, ids, func(a, b string) bool {
		ta, okA := at[a]
		tb, okB := at[b]
		return okA && okB && ta.After(tb)
	})
}

// backfillThreads reports the conversations from before sessions, which
// it leaves as they are.
func (p *Platform) backfillThreads(tenant string, rep *BackfillReport) error {
	old, err := p.Threads.Records(tenant, func(th threads.Thread) bool { return th.Session == nil && th.Archive == nil })
	if err != nil {
		return err
	}
	slices.SortFunc(old, func(a, b threads.Thread) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	})
	const why = "a session's id is its key's, and this conversation's messages, runs, surface ids and links are kept under its own"
	for _, th := range old {
		if th.Surface != nil {
			rep.add(BackfillSlackThreadLeft, th.ID, "", why+"; replies on "+th.Surface.Client+" keep reaching it")
			continue
		}
		rep.add(BackfillThreadLeft, th.ID, "", why)
	}
	return nil
}
