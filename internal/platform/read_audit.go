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

// ResponseReader attributes exceptional reads to the server's registered
// route as AuditedReader does, and writes them as one response's: a read is
// what a response shows, so the records of everything a response shows are
// written together, all or none, before any of it is sent.
//
// Until commit is called a read by exception is granted and its record kept
// back. commit writes every record kept back in one write
// (readaudit.Store.AddAll) and reports whether the response may be sent:
// when the write fails, or would pass the tenant's cap, nothing is written
// and nothing of the response may leave. The server calls it before the
// first byte of the answer. A read made after it, as a stream makes for
// each event it goes on to send, is recorded at once, before it is granted.
func (p *Platform) ResponseReader(who auth.Principal, route string) (reader auth.Principal, commit func() bool) {
	var mu sync.Mutex
	type readKey struct {
		subject string
		project string
		thread  string
	}
	seen := map[readKey]bool{}
	var kept []readaudit.Record
	var keptKeys []readKey
	committed := false
	reader = who
	reader.AuditRead = func(subject, project, thread string) bool {
		if !strings.HasPrefix(route, "GET ") {
			return true
		}
		mu.Lock()
		defer mu.Unlock()
		key := readKey{subject, project, thread}
		if allowed, ok := seen[key]; ok {
			return allowed
		}
		// The request may not accumulate an unbounded set of audit decisions.
		if len(seen) >= 10000 {
			return false
		}
		if committed {
			effective := who
			effective.Subject = subject
			seen[key] = p.recordRead(effective, Place{Project: project, Thread: thread}, route)
			return seen[key]
		}
		kept = append(kept, readaudit.Record{Subject: subject, Project: project, Thread: thread, Route: route, At: p.now()})
		keptKeys = append(keptKeys, key)
		seen[key] = true
		return true
	}
	commit = func() bool {
		mu.Lock()
		defer mu.Unlock()
		if committed {
			return true
		}
		committed = true
		if err := p.ReadAudit.AddAll(who.Tenant, kept); err != nil {
			p.log.Error("platform: break-glass read refused: audit failed", "error", err, "records", len(kept))
			for _, key := range keptKeys {
				seen[key] = false
			}
			kept, keptKeys = nil, nil
			return false
		}
		kept, keptKeys = nil, nil
		return true
	}
	return reader, commit
}

// recordReads writes the records of several reads granted by exception
// together, all or none, and reports whether the reads may go ahead.
func (p *Platform) recordReads(who auth.Principal, at []Place, route string) bool {
	records := make([]readaudit.Record, len(at))
	for i, place := range at {
		records[i] = readaudit.Record{Subject: who.Subject, Project: place.Project, Thread: place.Thread, Route: route, At: p.now()}
	}
	if err := p.ReadAudit.AddAll(who.Tenant, records); err != nil {
		p.log.Error("platform: break-glass read refused: audit failed", "error", err, "records", len(records))
		return false
	}
	return true
}
