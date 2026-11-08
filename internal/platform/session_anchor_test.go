package platform_test

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/feedback"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
)

func TestSessionAnchorAndChangedPagesKeepOldTurnsAndTimestampTies(t *testing.T) {
	p := start(t, call.Limits{})
	root, err := p.StartSession(t.Context(), admin, platform.SessionInput{Project: "~", Agent: "lead", Text: "Begin"})
	if err != nil {
		t.Fatal(err)
	}
	var runID string
	for _, m := range root.Messages {
		if m.Run != nil {
			runID = m.Run.ID
			await(t, p, runID, runs.Completed)
		}
	}
	if runID == "" {
		t.Fatal("initial run missing")
	}
	initial, err := p.ReadSession(t.Context(), admin, root.ID, "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	posted := map[string]bool{}
	anchor := ""
	for i := 0; i < 450; i++ {
		m, _, err := p.Threads.Post(admin.Tenant, root.ID, threads.Message{By: admin.Subject, Name: "Ana", Text: fmt.Sprintf("Update %d", i)}, at)
		if err != nil {
			t.Fatal(err)
		}
		posted[m.ID] = true
		if i == 150 {
			anchor = m.ID
		}
	}
	page, err := p.ReadSession(t.Context(), admin, root.ID, "", "", false, anchor)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != platform.MaxTurnPage || page.Next == "" {
		t.Fatalf("anchor page was not bounded/paged: count=%d next=%q", len(page.Messages), page.Next)
	}
	found := false
	for _, m := range page.Messages {
		found = found || m.ID == anchor
	}
	if !found {
		t.Fatal("anchor omitted from page")
	}
	old, err := p.ReadSession(t.Context(), admin, root.ID, "", "", false, runID)
	if err != nil || old.Messages[1].ID != runID {
		t.Fatalf("old run anchor: %v %+v", err, old.Messages)
	}
	if _, err = p.ReadSession(t.Context(), admin, root.ID, "", "", false, "missing"); !errors.Is(err, threads.ErrNotFound) {
		t.Fatalf("unknown anchor: %v", err)
	}
	if _, err = p.ReadSession(t.Context(), admin, root.ID, initial.Cursor, "", false, anchor); !errors.Is(err, threads.ErrInvalid) {
		t.Fatalf("conflicting anchor cursor: %v", err)
	}
	cursor := initial.Cursor
	seen := map[string]bool{}
	for n := 0; ; n++ {
		delta, err := p.ReadSession(t.Context(), admin, root.ID, cursor, "", true)
		if err != nil {
			t.Fatal(err)
		}
		if len(delta.Messages) > platform.MaxTurnPage || n > 3 {
			t.Fatal("unbounded or stuck changed paging")
		}
		for _, m := range delta.Messages {
			if seen[m.ID] {
				t.Fatalf("change repeated across pages: %s", m.ID)
			}
			seen[m.ID] = true
		}
		cursor = delta.Cursor
		if !delta.More {
			break
		}
	}
	for id := range posted {
		if !seen[id] {
			t.Fatalf("timestamp-tied change lost: %s", id)
		}
	}
	if _, err := p.Feedback.Save(admin.Tenant, feedback.Record{Run: runID, Subject: admin.Subject, Rating: "up"}); err != nil {
		t.Fatal(err)
	}
	delta, err := p.ReadSession(t.Context(), admin, root.ID, cursor, "", false)
	if err != nil || len(delta.Messages) != 1 || delta.Messages[0].ID != runID || delta.Messages[0].Feedback == nil {
		t.Fatalf("paged-out run update lost: %v %+v", err, delta.Messages)
	}
}
