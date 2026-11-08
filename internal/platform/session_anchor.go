package platform

import (
	"fmt"

	"github.com/urmzd/humagen/pkg/threads"
)

// sessionTurns keeps anchor reads on the same retained transcript and cursor
// positions as ordinary paging. It returns one bounded page, oldest first.
func (p *Platform) sessionTurns(tenant string, th threads.Thread, since, before, around string) ([]ThreadTurn, string, error) {
	if since != "" {
		if before != "" || around != "" {
			return nil, "", fmt.Errorf("%w: since cannot be combined with before or around", threads.ErrInvalid)
		}
		// Updates to older runs and feedback belong in catch-up reads even
		// when their original turns are outside the newest history page.
		return p.turnsOf(tenant, th), "", nil
	}
	if around == "" {
		_, turns, next, err := p.ThreadTurns(tenant, th.ID, before, MaxTurnPage)
		return turns, next, err
	}
	if since != "" || before != "" || len(around) > 200 {
		return nil, "", fmt.Errorf("%w: around is a message or run ID and cannot be combined with since or before", threads.ErrInvalid)
	}
	all := p.turnsOf(tenant, th)
	for i, turn := range all {
		if (turn.Message == nil || turn.Message.ID != around) && (turn.Run == nil || turn.Run.ID != around) {
			continue
		}
		end := min(len(all), max(MaxTurnPage, i+1+MaxTurnPage/2))
		start := max(0, end-MaxTurnPage)
		next := ""
		if start > 0 {
			next = turnCursor(start)
		}
		return all[start:end], next, nil
	}
	return nil, "", threads.ErrNotFound
}
