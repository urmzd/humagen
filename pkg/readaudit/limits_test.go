package readaudit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

func TestCapacityRefusesAnUnrecordableRead(t *testing.T) {
	st := state.NewMemory()
	s, _ := Open(st)
	full, _ := json.Marshal(MaxRecords)
	if err := st.Write(t.Context(), namespace, state.Put("acme", countKey, full)); err != nil {
		t.Fatal(err)
	}
	if err := s.Add("acme", Record{Subject: "ana", Route: "GET /api/projects", At: time.Now()}); err == nil {
		t.Fatal("capacity permitted an unrecordable read")
	}
	if n, _, err := s.count("acme"); err != nil || n != MaxRecords {
		t.Fatalf("capacity exceeded: %d, %v", n, err)
	}
}

func TestListKeepsOnlyTheNewestBoundedRows(t *testing.T) {
	s, _ := Open(nil)
	at := time.Now().UTC()
	for i := range 600 {
		if err := s.Add("acme", Record{Subject: "ana", Route: "GET /api/projects", At: at.Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.List("acme", at, at.Add(time.Hour), 1000)
	if err != nil || len(rows) != 500 || !rows[0].At.Equal(at.Add(599*time.Second)) || !rows[499].At.Equal(at.Add(100*time.Second)) {
		t.Fatalf("unbounded or unordered rows: %d, %v", len(rows), err)
	}
}

// The reads one response shows are recorded together or not at all, on both
// backends: a set with an invalid record, or one that would pass the
// tenant's cap, writes none of them and leaves the count where it was.
func TestAResponsesReadsAreRecordedTogetherOrNotAtAll(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		st := open(t)
		s, err := Open(st)
		if err != nil {
			t.Fatal(err)
		}
		at := time.Now().UTC()
		read := func(thread string) Record {
			return Record{Subject: "ana", Project: "launch", Thread: thread, Route: "GET /api/sessions", At: at}
		}
		kept := func() int {
			t.Helper()
			all, err := s.List("acme", at.Add(-time.Hour), at.Add(time.Hour), MaxRows)
			if err != nil {
				t.Fatal(err)
			}
			return len(all)
		}
		if err := s.AddAll("acme", []Record{read("s_a"), {Subject: "ana"}, read("s_c")}); err == nil || kept() != 0 {
			t.Fatalf("a set with an invalid record = %v, and kept %d records", err, kept())
		}
		if err := s.AddAll("acme", nil); err != nil || kept() != 0 {
			t.Fatalf("no records = %v, and kept %d", err, kept())
		}
		// Two records short of the cap.
		near, _ := json.Marshal(MaxRecords - 2)
		if err := st.Write(t.Context(), namespace, state.Put("acme", countKey, near)); err != nil {
			t.Fatal(err)
		}
		if err := s.AddAll("acme", []Record{read("s_a"), read("s_b"), read("s_c")}); err == nil || kept() != 0 {
			t.Fatalf("three records with room for two = %v, and kept %d records", err, kept())
		}
		if n, _, err := s.count("acme"); err != nil || n != MaxRecords-2 {
			t.Fatalf("the refused set moved the count to %d, %v", n, err)
		}
		if err := s.AddAll("acme", []Record{read("s_a"), read("s_b")}); err != nil || kept() != 2 {
			t.Fatalf("two records with room for two = %v, and kept %d records", err, kept())
		}
		if n, _, err := s.count("acme"); err != nil || n != MaxRecords {
			t.Fatalf("the count after two records = %d, %v", n, err)
		}
	})
}
