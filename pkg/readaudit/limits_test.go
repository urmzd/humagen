package readaudit

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/urmzd/humagen/pkg/state"
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
