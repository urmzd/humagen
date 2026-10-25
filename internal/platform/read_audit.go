package platform

import (
	"strings"
	"sync"

	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/readaudit"
)

// AuditedReader attributes exceptional reads to the server's registered route.
// All Seers built for this request share the result for each place, including
// failed writes. A stream keeps the same bounded cache for its lifetime.
func (p *Platform) AuditedReader(who auth.Principal, route string) auth.Principal {
	var mu sync.Mutex
	type readKey struct {
		subject string
		project string
		thread  string
	}
	seen := map[readKey]bool{}
	reader := who
	reader.AuditRead = func(subject, project, thread string) bool {
		if !strings.HasPrefix(route, "GET ") {
			return true
		}
		mu.Lock()
		defer mu.Unlock()
		at := Place{Project: project, Thread: thread}
		key := readKey{subject, project, thread}
		if allowed, ok := seen[key]; ok {
			return allowed
		}
		// The request may not accumulate an unbounded set of audit decisions.
		if len(seen) >= 10000 {
			return false
		}
		effective := who
		effective.Subject = subject
		allowed := p.recordRead(effective, at, route)
		seen[key] = allowed
		return allowed
	}
	return reader
}

func (p *Platform) recordRead(who auth.Principal, at Place, route string) bool {
	if err := p.ReadAudit.Add(who.Tenant, readaudit.Record{Subject: who.Subject, Project: at.Project, Thread: at.Thread, Route: route, At: p.now()}); err != nil {
		p.log.Error("platform: break-glass read refused: audit failed", "error", err)
		return false
	}
	return true
}
