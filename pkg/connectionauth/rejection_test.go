package connectionauth

import (
	"context"
	"testing"
	"time"
)

func TestTokenRejectionFencesReconnectRefreshAndLease(t *testing.T) {
	m, _, _ := setup(t)
	ctx := t.Context()
	connect := func() {
		a, err := m.Begin(ctx, "tenant", "alice", "notes")
		if err != nil {
			t.Fatal(err)
		}
		if err = m.Complete(ctx, "tenant", "alice", "notes", nonceOf(t, a), "code"); err != nil {
			t.Fatal(err)
		}
	}
	connect()
	old := m.Revision("tenant", "alice", "notes")
	connect() // The provider deliberately returns the same token for new consent.
	calls := 0
	m.cfg.OnAuthFailure = func(_ context.Context, tenant, person, provider string) error {
		if tenant != "tenant" || person != "alice" || provider != "notes" {
			t.Fatal("wrong owner")
		}
		calls++
		return nil
	}
	m.ReportTokenRejected(ctx, "tenant", "alice", "notes", old, "secret-access")
	if calls != 0 {
		t.Fatal("old consent rejection reached new grant")
	}
	current := m.Revision("tenant", "alice", "notes")
	token, err := m.Token(ctx, "tenant", "alice", "notes") // Rotates access token.
	if err != nil {
		t.Fatal(err)
	}
	m.ReportTokenRejected(ctx, "tenant", "alice", "notes", current, "secret-access")
	if calls != 0 {
		t.Fatal("old access token rejection reached refreshed grant")
	}
	row, _ := m.records.Lookup("tenant", recordKey("account", "alice", "notes"))
	var held account
	if m.open("tenant", row.Key, row.Value, &held) != nil {
		t.Fatal("open account")
	}
	held.LeaseUntil = m.now().Add(time.Minute)
	raw, _ := m.seal("tenant", row.Key, held)
	if err := m.records.Put("tenant", row.Key, raw); err != nil {
		t.Fatal(err)
	}
	m.ReportTokenRejected(ctx, "tenant", "alice", "notes", current, token)
	if calls != 0 {
		t.Fatal("in-flight refresh was rejected")
	}
	held.LeaseUntil = time.Time{}
	raw, _ = m.seal("tenant", row.Key, held)
	if err := m.records.Put("tenant", row.Key, raw); err != nil {
		t.Fatal(err)
	}
	m.ReportTokenRejected(ctx, "other", "alice", "notes", current, token)
	m.ReportTokenRejected(ctx, "tenant", "bob", "notes", current, token)
	m.ReportTokenRejected(ctx, "tenant", "alice", "notes", "", token)
	if calls != 0 {
		t.Fatal("unproved identity rejected grant")
	}
	m.ReportTokenRejected(ctx, "tenant", "alice", "notes", current, token)
	status, err := m.Status("tenant", "alice", "notes")
	if err != nil || !status.ReconnectRequired || calls != 1 {
		t.Fatal(status, err, calls)
	}
}
