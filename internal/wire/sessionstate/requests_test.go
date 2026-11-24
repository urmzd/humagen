package sessionstate_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/saige/flow/attachment"
	"github.com/urmzd/saige/flow/event"
	"github.com/urmzd/saige/flow/thread"

	"github.com/urmzd/humagen/internal/wire/sessionstate"
	"github.com/urmzd/humagen/pkg/state"
)

// requestJournal holds one exchange per run in key's thread: a request
// journaled with passages before its text, then the answer.
func requestJournal(t *testing.T, st state.Store, key thread.Key, runs map[string][]attachment.Attachment) {
	t.Helper()
	j := sessionstate.NewJournal(st)
	at := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	for run, files := range runs {
		if _, err := j.Append(t.Context(), key,
			thread.Entry{Role: thread.RoleUser, Text: "Passages retrieved.\n\n", Attachments: files, Actor: event.Actor{ID: "ada"}, RunID: run, At: at},
			thread.Entry{Role: thread.RoleAssistant, Text: "answer", Actor: event.Actor{ID: "docs", Bot: true}, RunID: run, At: at},
		); err != nil {
			t.Fatal(err)
		}
	}
}

// userTexts are the texts of every record of key's tenant's user entries.
func userTexts(t *testing.T, st state.Store, tenant string) []string {
	t.Helper()
	recs, err := st.Load(t.Context(), "agent-sessions")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range recs {
		if r.Tenant != tenant || (!strings.HasPrefix(r.Key, "e:") && !strings.HasPrefix(r.Key, "i:")) {
			continue
		}
		var e thread.Entry
		if err := json.Unmarshal(r.Value, &e); err != nil {
			t.Fatal(err)
		}
		if e.Role == thread.RoleUser {
			out = append(out, e.Text)
		}
	}
	return out
}

// emptyAsked reads every run as a request asked with no text, which a
// journaled text with passages before an empty request matches.
func emptyAsked(key thread.Key) func(context.Context, string) (sessionstate.AskedRequest, error) {
	return func(context.Context, string) (sessionstate.AskedRequest, error) {
		return sessionstate.AskedRequest{Thread: key, Asked: "", WithPassages: func(s string) bool { return s == "Passages retrieved.\n\n" }}, nil
	}
}

// A request of attachments alone, whose text as it was asked is empty, is
// matched and set to the empty text with its attachments kept, with or
// without Replace; one with neither text nor attachments is not matched.
func TestMigrateRequestsMatchesARequestOfAttachmentsAlone(t *testing.T) {
	key := thread.Key{Tenant: "acme", Agent: "docs", Surface: "slack://T1/C1?thread=1.1"}
	file := attachment.Attachment{Digest: strings.Repeat("a", 64), MediaType: "image/png", Name: "plot.png", Size: 3}
	for _, replace := range []bool{false, true} {
		st := state.NewMemory()
		requestJournal(t, st, key, map[string][]attachment.Attachment{"r-files": {file}})
		rep, err := sessionstate.NewJournal(st).MigrateRequests(t.Context(), sessionstate.RequestMigration{
			Tenant: "acme", Apply: true, Replace: replace, Request: emptyAsked(key),
		})
		if err != nil {
			t.Fatal(err)
		}
		if rep.Counts[sessionstate.RequestRewritten] != 1 || rep.Counts[sessionstate.RequestNotMatched] != 0 || rep.Counts[sessionstate.RequestReplaced] != 0 {
			t.Fatalf("replace %v: counts %v", replace, rep.Counts)
		}
		texts := userTexts(t, st, "acme")
		if len(texts) == 0 {
			t.Fatal("no user records")
		}
		for _, s := range texts {
			if s != "" {
				t.Fatalf("replace %v: a record is %q, want the empty request", replace, s)
			}
		}
		history, err := sessionstate.NewJournal(st).History(t.Context(), key, 2)
		if err != nil || len(history) != 2 || len(history[0].Attachments) != 1 {
			t.Fatalf("history = %+v, %v", history, err)
		}
	}
}

// With Apply, the threads that hold a run that has not ended are read again
// just before each entry is written: a thread named then is left, and a run
// whose thread cannot be read leaves the rest of the tenant.
func TestMigrateRequestsChecksAgainBeforeWriting(t *testing.T) {
	key := thread.Key{Tenant: "acme", Agent: "docs", Surface: "slack://T1/C1?thread=1.1"}
	file := attachment.Attachment{Digest: strings.Repeat("b", 64), MediaType: "image/png", Name: "plot.png", Size: 3}
	st := state.NewMemory()
	requestJournal(t, st, key, map[string][]attachment.Attachment{"r-1": {file}})
	before := userTexts(t, st, "acme")
	calls := 0
	rep, err := sessionstate.NewJournal(st).MigrateRequests(t.Context(), sessionstate.RequestMigration{
		Tenant: "acme", Apply: true, Request: emptyAsked(key),
		Recheck: func(context.Context) (map[thread.Key]string, string, error) {
			calls++
			return map[thread.Key]string{key: "r-new"}, "", nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || rep.Changes() != 0 || rep.Counts[sessionstate.ThreadLeft] != 1 || rep.Counts[sessionstate.RequestInLeftThread] != 1 {
		t.Fatalf("calls %d, counts %v", calls, rep.Counts)
	}
	if l := rep.Lines[0]; l.Run != "r-new" || l.Detail == "" {
		t.Fatalf("line = %+v", l)
	}
	if got := userTexts(t, st, "acme"); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatalf("a thread found busy was written: %q", got)
	}

	rep, err = sessionstate.NewJournal(st).MigrateRequests(t.Context(), sessionstate.RequestMigration{
		Tenant: "acme", Apply: true, Request: emptyAsked(key),
		Recheck: func(context.Context) (map[thread.Key]string, string, error) { return nil, "r-unread", nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Changes() != 0 || rep.Counts[sessionstate.TenantLeft] != 1 || rep.Lines[len(rep.Lines)-1].Run != "r-unread" {
		t.Fatalf("report = %+v", rep)
	}
	if got := userTexts(t, st, "acme"); strings.Join(got, "|") != strings.Join(before, "|") {
		t.Fatalf("a tenant left was written: %q", got)
	}

	// A dry run does not read them again.
	rep, err = sessionstate.NewJournal(st).MigrateRequests(t.Context(), sessionstate.RequestMigration{
		Tenant: "acme", Request: emptyAsked(key),
		Recheck: func(context.Context) (map[thread.Key]string, string, error) {
			t.Fatal("a dry run read the threads again")
			return nil, "", nil
		},
	})
	if err != nil || rep.Changes() != 1 {
		t.Fatalf("dry run = %+v, %v", rep, err)
	}
}
