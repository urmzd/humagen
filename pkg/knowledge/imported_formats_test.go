package knowledge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/urmzd/humagen/pkg/sessionimport"
)

// formats are the structures of the generated records the session reader's
// test data holds, one session of each of the four formats, as an import
// sends them.
func formats(t *testing.T) map[string][]byte {
	t.Helper()
	data := filepath.Join("..", "sessionimport", "testdata")
	lim := sessionimport.Limits{Bytes: MaxImportedBytes, Turns: MaxImportedTurns, TurnBytes: MaxImportedTurnBytes, ID: MaxImportedID}
	out := map[string][]byte{}
	keep := func(s sessionimport.Session) {
		st, ok := s.Structure(lim)
		if !ok {
			t.Fatalf("%s makes no structure", s.URI())
		}
		raw, err := json.Marshal(st)
		if err != nil {
			t.Fatal(err)
		}
		out[s.URI()] = raw
	}
	for tool, name := range map[string]string{sessionimport.Claude: "claude-session.jsonl", sessionimport.Codex: "codex-rollout.jsonl"} {
		s, err := sessionimport.Open(tool, filepath.Join(data, name))
		if err != nil {
			t.Fatal(err)
		}
		keep(s)
	}
	for _, name := range []string{"chatgpt-conversations.json", "claudeai-conversations.json"} {
		f, err := os.Open(filepath.Join(data, name))
		if err != nil {
			t.Fatal(err)
		}
		_, err = sessionimport.ReadExport(f, func(s sessionimport.Session) error { keep(s); return nil })
		_ = f.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	if len(out) != 4 {
		t.Fatalf("%d sessions", len(out))
	}
	return out
}

// The structure of a session of each format, as the import makes and sends
// it, is taken as the import route takes it, kept, and read back the same,
// field for field, on each store.
func TestEachFormatsStructureIsKeptAndReadBackAsSent(t *testing.T) {
	sent := formats(t)
	eachSeeded(t, func(t *testing.T, s *Store) {
		for uri, raw := range sent {
			im, err := ParseImported(raw)
			if err != nil {
				t.Fatalf("%s: %v", uri, err)
			}
			doc, err := s.AddImportedForScope("acme", "handbook", "A generated chat", uri, "generated text of "+uri, "ada", now, nil, im)
			if err != nil {
				t.Fatalf("%s: %v", uri, err)
			}
			got := origin(t, s, doc.ID)
			if got.Session == nil {
				t.Fatalf("%s: %+v", uri, got)
			}
			back, _ := json.Marshal(got.Session)
			if string(back) != string(raw) {
				t.Errorf("%s:\nsent %s\nread %s", uri, raw, back)
			}
		}
	})
}
