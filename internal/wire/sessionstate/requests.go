package sessionstate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/saige/flow/thread"
)

// UnkeptRequest is the text a request is replaced with when it is not
// matched to its run and RequestMigration.Replace is set.
const UnkeptRequest = "[the request is not kept]"

// The kinds of RequestLine, and of RequestReport.Counts.
const (
	RequestRewritten     = "request rewritten"
	RequestReplaced      = "request replaced"
	RequestNotMatched    = "request not matched"
	RequestAsAsked       = "request already as asked"
	RequestAlreadyUnkept = "request already replaced"
	RequestNoRun         = "user entry without a run"
	RequestChanged       = "entry changed while read"
	ThreadLeft           = "thread left: a run has not ended"
	RequestInLeftThread  = "request in a thread left"
)

// AskedRequest is what RequestMigration.Request says of one run.
type AskedRequest struct {
	// Missing, when not empty, says why the run's request cannot be read,
	// such as a run the ledger does not hold.
	Missing string
	// Thread is the thread the run's exchange is journaled in.
	Thread thread.Key
	// Asked is the request as it was asked.
	Asked string
	// WithPassages reports whether a journaled text is the request with the
	// passages a turn retrieved for it, which is Asked once they are taken
	// out.
	WithPassages func(text string) bool
}

// RequestMigration rewrites one tenant's journaled requests to the request as
// it was asked.
type RequestMigration struct {
	Tenant string
	// Apply writes the changes. Without it nothing is written and the
	// report says what would be.
	Apply bool
	// Replace sets a request that is not matched to its run to
	// UnkeptRequest. Without it such a request is listed and left.
	Replace bool
	// Busy are the threads that hold a run that has not ended, each to one
	// such run. Their entries are left.
	Busy map[thread.Key]string
	// Request reads one run's request. An error stops the migration.
	Request func(ctx context.Context, runID string) (AskedRequest, error)
	// Page is how many entries are read at once; zero is 200.
	Page int
}

// RequestReport is what a RequestMigration did, or would do. It names
// entries by their keys and runs, never by their text.
type RequestReport struct {
	Tenant string
	Apply  bool
	// Requests is how many user entries it read.
	Requests int
	Lines    []RequestLine
	Counts   map[string]int
}

// RequestLine is one entry changed, or one entry or thread left and why.
type RequestLine struct {
	Kind string
	// Key is the entry's record key, or the thread's id for a thread left.
	Key    string
	Run    string
	Detail string
}

func (r *RequestReport) add(kind, key, run, detail string) {
	r.Lines = append(r.Lines, RequestLine{Kind: kind, Key: key, Run: run, Detail: detail})
	r.Counts[kind]++
}

// Changes is how many entries it wrote, or would write.
func (r RequestReport) Changes() int { return r.Counts[RequestRewritten] + r.Counts[RequestReplaced] }

// MigrateRequests reads every entry of m.Tenant's journal, a page at a time,
// and sets each user entry a run journaled to the request as it was asked:
//
//   - An entry that is the request as it was asked is left.
//   - An entry that is the request with the passages its turn retrieved
//     is set to the request as it was asked.
//   - Any other entry of a run (its run is not in the ledger, or its text is
//     neither) is listed and left, or set to UnkeptRequest with m.Replace.
//
// Every record of an entry (its place in the thread, and its index by run
// and by surface message) is written together, each at the version read, so
// an entry that changed while it was read is listed and left for the next
// run. Assistant entries, and user entries no run journaled, are left. A
// second run finds the first one's work and writes nothing.
func (j *Journal) MigrateRequests(ctx context.Context, m RequestMigration) (RequestReport, error) {
	rep := RequestReport{Tenant: m.Tenant, Apply: m.Apply, Counts: map[string]int{}}
	if m.Tenant == "" || m.Request == nil {
		return rep, errors.New("sessionstate: migrate requests: a tenant and a run reader are required")
	}
	page := m.Page
	if page <= 0 {
		page = 200
	}
	page = min(page, state.MaxList)
	busy := map[string]string{}
	for k, run := range m.Busy {
		if k.Tenant == m.Tenant {
			busy[journalKey(k)] = run
		}
	}
	listed := map[string]bool{}
	after := ""
	for {
		rows, next, err := j.state.List(ctx, namespace, m.Tenant, state.Query{Prefix: "e:", After: after, Limit: page})
		if err != nil {
			return rep, err
		}
		for _, row := range rows {
			if err := j.migrateRequest(ctx, m, row, busy, listed, &rep); err != nil {
				return rep, err
			}
		}
		if next == "" || len(rows) == 0 {
			return rep, nil
		}
		after = next
	}
}

// migrateRequest decides, and with m.Apply writes, one journal entry.
func (j *Journal) migrateRequest(ctx context.Context, m RequestMigration, row state.Record, busy map[string]string, listed map[string]bool, rep *RequestReport) error {
	base, _, ok := strings.Cut(strings.TrimPrefix(row.Key, "e:"), ":")
	if !ok {
		return nil
	}
	var e thread.Entry
	if err := json.Unmarshal(row.Value, &e); err != nil {
		return fmt.Errorf("sessionstate: entry %s: %w", row.Key, err)
	}
	if e.Role != thread.RoleUser {
		return nil
	}
	rep.Requests++
	if run, ok := busy[base]; ok {
		if !listed[base] {
			listed[base] = true
			rep.add(ThreadLeft, base, run, "")
		}
		rep.Counts[RequestInLeftThread]++
		return nil
	}
	if e.RunID == "" {
		rep.Counts[RequestNoRun]++
		return nil
	}
	if e.Text == UnkeptRequest {
		rep.Counts[RequestAlreadyUnkept]++
		return nil
	}
	req, err := m.Request(ctx, e.RunID)
	if err != nil {
		return fmt.Errorf("sessionstate: entry %s: run %s: %w", row.Key, e.RunID, err)
	}
	if req.Missing == "" && journalKey(req.Thread) != base {
		req.Missing = "the run is journaled in another thread"
	}
	if req.Missing == "" && e.Text == req.Asked {
		rep.Counts[RequestAsAsked]++
		return nil
	}
	to, kind, detail := "", RequestRewritten, ""
	switch {
	case req.Missing != "":
		detail = req.Missing
	case req.WithPassages == nil || !req.WithPassages(e.Text):
		detail = "the text is not the request with passages before it"
	case strings.TrimSpace(req.Asked) == "" && len(e.Attachments) == 0:
		detail = "the request as it was asked is empty"
	default:
		to = req.Asked
	}
	if to == "" {
		if !m.Replace {
			rep.add(RequestNotMatched, row.Key, e.RunID, detail)
			return nil
		}
		to, kind = UnkeptRequest, RequestReplaced
	}
	ops, n, differ, err := j.copies(ctx, m.Tenant, base, row, e, to)
	if err != nil {
		return err
	}
	if differ {
		rep.add(RequestNotMatched, row.Key, e.RunID, "its records differ")
		return nil
	}
	if m.Apply {
		if err := j.state.Write(ctx, namespace, ops...); errors.Is(err, state.ErrConflict) {
			rep.add(RequestChanged, row.Key, e.RunID, "left for the next run")
			return nil
		} else if err != nil {
			return fmt.Errorf("sessionstate: entry %s: %w", row.Key, err)
		}
	}
	if detail != "" {
		detail += "; "
	}
	rep.add(kind, row.Key, e.RunID, fmt.Sprintf("%s%d records", detail, n))
	return nil
}

// copies are the writes that set every record of entry e (read from row) to
// text, each at the version read, and how many records they are. An index
// record that holds another entry is not one of them; one that holds this
// entry with another text is reported as differ, and nothing is written.
func (j *Journal) copies(ctx context.Context, tenant, base string, row state.Record, e thread.Entry, text string) ([]state.Op, int, bool, error) {
	out := e
	out.Text = text
	ops := []state.Op{put(tenant, row.Key, out, row.Version)}
	keys := []string{"i:" + base + ":" + hash("run:"+e.RunID+"\x00"+string(e.Role))}
	if e.SurfaceID != "" {
		keys = append(keys, "i:"+base+":"+hash("surface:"+e.SurfaceID))
	}
	for _, k := range keys {
		r, err := j.state.Lookup(ctx, namespace, tenant, k)
		if errors.Is(err, state.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, 0, false, err
		}
		var held thread.Entry
		if err := json.Unmarshal(r.Value, &held); err != nil {
			return nil, 0, false, fmt.Errorf("sessionstate: entry %s: %w", k, err)
		}
		if held.Seq != e.Seq {
			continue
		}
		if held.Text != e.Text {
			return nil, 0, true, nil
		}
		ops = append(ops, put(tenant, k, out, r.Version))
	}
	return ops, len(ops), false, nil
}
