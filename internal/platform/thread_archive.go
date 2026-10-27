package platform

import (
	"fmt"
	"slices"
	"strings"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/threads"
)

// ArchiveThread puts a conversation away. It leaves every list and joins the
// deletion queue after threads.DeletionDelay, or at once when deleteNow. It
// takes what changing the thread takes (mayChangeThread). Nothing is deleted
// until an admin approves it (ApproveDeletion).
func (p *Platform) ArchiveThread(who auth.Principal, id string, deleteNow bool) (threads.Thread, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	th, err := p.ThreadRecord(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.mayChangeThread(who, th); err != nil {
		return threads.Thread{}, err
	}
	if th, err = p.recordOf(who.Tenant, id, p.claimant(who, th.Project)); err != nil {
		return threads.Thread{}, err
	}
	now := p.now()
	a := &threads.Archive{By: who.Subject, At: now, DeleteAfter: now.Add(threads.DeletionDelay), Requested: deleteNow}
	if th.Archive != nil {
		// Archiving again keeps the first date; asking to delete only adds.
		a.At, a.DeleteAfter, a.By = th.Archive.At, th.Archive.DeleteAfter, th.Archive.By
		a.Requested = th.Archive.Requested || deleteNow
	}
	return p.Threads.SetArchive(who.Tenant, id, a)
}

// UnarchiveThread brings a conversation back and cancels its deletion.
func (p *Platform) UnarchiveThread(who auth.Principal, id string) (threads.Thread, error) {
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	th, err := p.Threads.Get(who.Tenant, id)
	if err != nil {
		return threads.Thread{}, err
	}
	if err := p.mayChangeThread(who, th); err != nil && !who.Can(auth.ProjectsGovern) {
		return threads.Thread{}, err
	}
	if th.Archive == nil {
		return th, nil
	}
	return p.Threads.SetArchive(who.Tenant, id, nil)
}

// Deletion is a conversation waiting on an admin to approve its deletion.
type Deletion struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Project     string `json:"project"`
	ArchivedBy  string `json:"archived_by"`
	ArchivedAt  string `json:"archived_at"`
	DeleteAfter string `json:"delete_after"`
	// Requested is true when someone asked to delete it now, rather than
	// letting the archive run out.
	Requested bool `json:"requested"`
	// Due is true once it may be approved: asked for, or past DeleteAfter.
	Due      bool `json:"due"`
	Messages int  `json:"messages"`
}

// DeletionQueue lists every archived conversation, due ones first, for one
// who governs the workspace.
func (p *Platform) DeletionQueue(who auth.Principal) ([]Deletion, error) {
	if !who.Can(auth.ProjectsGovern) {
		return nil, fmt.Errorf("%w: the deletion queue is for admins", ErrForbidden)
	}
	now := p.now()
	recs, _ := p.Threads.Summaries(who.Tenant, func(th threads.Thread) bool {
		return th.Archive != nil && th.Archive.Deleted == nil
	})
	// Seen outside Summaries, which holds the store's lock.
	seer := p.Seer(who)
	out := make([]Deletion, 0, len(recs))
	for _, r := range recs {
		if !seer.Place(Place{Project: r.Project, Thread: r.ID}) {
			continue
		}
		a := r.Archive
		title := r.Title
		if title == "" {
			title = r.First
		}
		if title == "" {
			title = p.threadTitle(who.Tenant, r.Thread)
		}
		out = append(out, Deletion{
			ID: r.ID, Title: title, Project: r.Project, ArchivedBy: p.nameOf(who.Tenant, a.By),
			ArchivedAt: a.At.UTC().Format("2006-01-02T15:04:05Z"), DeleteAfter: a.DeleteAfter.UTC().Format("2006-01-02T15:04:05Z"),
			Requested: a.Requested, Due: a.Due(now), Messages: r.Messages,
		})
	}
	slices.SortFunc(out, func(x, y Deletion) int {
		if x.Due != y.Due {
			if x.Due {
				return -1
			}
			return 1
		}
		return strings.Compare(x.DeleteAfter, y.DeleteAfter)
	})
	return out, nil
}

// ApproveDeletion deletes an archived conversation that is due: its messages
// are removed and its record stays as a tombstone, so it never shows again.
// Its runs stay in the run journal, hidden with it.
func (p *Platform) ApproveDeletion(who auth.Principal, id string) error {
	if !who.Can(auth.ProjectsGovern) {
		return fmt.Errorf("%w: only an admin approves a deletion", ErrForbidden)
	}
	mu := p.moves.of(who.Tenant, id)
	mu.Lock()
	defer mu.Unlock()
	th, err := p.queued(who, id)
	if err != nil {
		return err
	}
	if !th.Archive.Due(p.now()) {
		return fmt.Errorf("%w: only an archived conversation that is due is deleted", threads.ErrInvalid)
	}
	return p.Threads.Purge(who.Tenant, id, who.Subject, p.now())
}

// RejectDeletion keeps an archived conversation: it is brought back.
func (p *Platform) RejectDeletion(who auth.Principal, id string) error {
	if !who.Can(auth.ProjectsGovern) {
		return fmt.Errorf("%w: only an admin rejects a deletion", ErrForbidden)
	}
	if _, err := p.queued(who, id); err != nil {
		return err
	}
	_, err := p.UnarchiveThread(who, id)
	return err
}

// queued is an archived conversation the admin sees. One that is not
// archived, or is hidden from them, is not in the queue: it is not found,
// as one that does not exist.
func (p *Platform) queued(who auth.Principal, id string) (threads.Thread, error) {
	th, err := p.Threads.Get(who.Tenant, id)
	if err != nil || th.Archive == nil || th.Archive.Deleted != nil || !p.Seer(who).Place(Place{Project: th.Project, Thread: id}) {
		return threads.Thread{}, fmt.Errorf("%w: no deletion for %q", threads.ErrNotFound, id)
	}
	return th, nil
}

// threadGone reports whether a conversation was deleted: no one sees it, or
// anything in it, again.
func (p *Platform) threadGone(tenant, id string) bool {
	if id == "" {
		return false
	}
	th, err := p.Threads.Get(tenant, id)
	return err == nil && th.Archive != nil && th.Archive.Deleted != nil
}
