// Package continuation tells a model whose program keeps sessions of its
// own (pkg/vendorcli) that a turn is one of a conversation continued from an
// imported one, and what that allows.
//
// Such a turn is sent the imported conversation as passive lines. A program
// that picks the session to continue by what the conversation so far says
// could be steered by those lines to a session of another conversation
// whose words they repeat. So the rule is not read from the turn's text:
// the platform marks it for the turn's run when it composes the turn
// (Mark), and the provider reads it by the run claim the runtime puts on
// every turn's context (isolation.RunFrom), whose tenant and run the
// platform sets from the run's durable record. A turn that is not marked is
// not a continuation's, and a caller that is not a platform turn carries no
// claim and is never told one is.
package continuation

import (
	"context"
	"sync"

	"github.com/urmzd/saige/flow/isolation"
)

// Rule is what a continuation's turn allows its program.
type Rule struct {
	// Thread is the continuation's thread: the only conversation whose own
	// sessions may be continued.
	Thread string
	// First is set on a turn no earlier turn of the thread came before:
	// the program starts a new session, and continues none.
	First bool
}

// marked holds the rule of each turn being composed in this process, by
// tenant and run. A turn's entry lives from Mark until Release, which the
// platform calls when the turn returns, however it ends, so the map holds at
// most the turns running at once.
var marked = struct {
	sync.Mutex
	rules map[[2]string]Rule
}{rules: map[[2]string]Rule{}}

// Mark records that the turn of a run is a continuation's. tenant and run
// must both be named; otherwise nothing is marked.
func Mark(tenant, run string, r Rule) {
	if tenant == "" || run == "" || r.Thread == "" {
		return
	}
	marked.Lock()
	marked.rules[[2]string{tenant, run}] = r
	marked.Unlock()
}

// Release forgets a run's mark once its turn has returned.
func Release(tenant, run string) {
	marked.Lock()
	delete(marked.rules, [2]string{tenant, run})
	marked.Unlock()
}

// Of is the rule of the turn ctx runs, read by the run claim alone, and
// false when the turn is not a continuation's or carries no claim.
func Of(ctx context.Context) (Rule, bool) {
	claim, ok := isolation.RunFrom(ctx)
	if !ok || claim == nil {
		return Rule{}, false
	}
	req := claim.Request()
	if req.Tenant == "" || req.Run == "" {
		return Rule{}, false
	}
	marked.Lock()
	r, ok := marked.rules[[2]string{req.Tenant, req.Run}]
	marked.Unlock()
	return r, ok
}
