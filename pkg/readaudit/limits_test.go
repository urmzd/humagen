package readaudit

import (
	"testing"
	"time"
)

func TestCapacityRefusesAnUnrecordableRead(t *testing.T) {
	s, _ := Open(nil)
	s.rows["acme"] = make([]entry, MaxRecords)
	if err := s.Add("acme", Record{Subject: "ana", Route: "GET /api/projects", At: time.Now()}); err == nil {
		t.Fatal("capacity permitted an unrecordable read")
	}
	if len(s.rows["acme"]) != MaxRecords {
		t.Fatal("capacity exceeded")
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
	rows := s.List("acme", at, at.Add(time.Hour), 1000)
	if len(rows) != 500 || !rows[0].At.Equal(at.Add(599*time.Second)) || !rows[499].At.Equal(at.Add(100*time.Second)) {
		t.Fatalf("unbounded or unordered rows: %d", len(rows))
	}
}
