package join

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	saigetypes "github.com/urmzd/saige/agent/types"
)

var team = []Candidate{
	{Name: "lead", Description: "Coordinates the work."},
	{Name: "releaser", Description: "Cuts releases."},
	{Name: "oncall", Description: "Triages incidents."},
}

// Every agent is asked, at once, and each yes joins.
func TestEveryAgentIsAskedAndEachYesJoins(t *testing.T) {
	var running, peak atomic.Int32
	a := AskerFunc(func(_ context.Context, c Candidate, _ string) (Decision, error) {
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
		running.Add(-1)
		return Decision{Join: c.Name != "lead", Reason: "fits"}, nil
	})
	ds := Route(context.Background(), a, team, "the release broke prod")
	if len(ds) != 3 || ds[0].Agent != "lead" || ds[2].Agent != "oncall" {
		t.Fatalf("decisions = %+v", ds)
	}
	got := Joined(ds)
	if len(got) != 2 || got[0].Agent != "releaser" || got[1].Agent != "oncall" {
		t.Fatalf("joined = %+v", got)
	}
	if peak.Load() < 2 {
		t.Fatalf("agents were asked one at a time (peak %d)", peak.Load())
	}
}

// An agent that cannot be asked stays out, and so does everyone when nothing
// asks.
func TestFailingOrMissingAskerJoinsNobody(t *testing.T) {
	failing := AskerFunc(func(context.Context, Candidate, string) (Decision, error) {
		return Decision{Join: true}, errors.New("boom")
	})
	if got := Joined(Route(context.Background(), failing, team, "x")); len(got) != 0 {
		t.Fatalf("a failing asker joined %+v", got)
	}
	ds := Route(context.Background(), nil, team, "x")
	if len(ds) != 3 || len(Joined(ds)) != 0 {
		t.Fatalf("a nil asker: %+v", ds)
	}
}

type reply string

func (r reply) Stream(context.Context, saigetypes.Request) (<-chan saigetypes.Delta, error) {
	ch := make(chan saigetypes.Delta, 1)
	ch <- saigetypes.PartDelta{Text: string(r)}
	close(ch)
	return ch, nil
}

func TestModelReadsTheAnswer(t *testing.T) {
	for _, tc := range []struct {
		reply string
		join  bool
		err   bool
	}{
		{`{"join": true, "reason": "It cuts releases."}`, true, false},
		{"Sure.\n```json\n{\"join\": false, \"reason\": \"Not mine.\"}\n```", false, false},
		{`{"reason": "forgot"}`, false, true},
		{`yes`, false, true},
	} {
		d, err := Model{Provider: reply(tc.reply)}.Ask(context.Background(), team[1], "tag the release")
		if (err != nil) != tc.err || d.Join != tc.join {
			t.Errorf("%q: %+v, %v", tc.reply, d, err)
		}
		if err == nil && !strings.HasSuffix(d.Reason, ".") {
			t.Errorf("%q: reason %q", tc.reply, d.Reason)
		}
	}
}
