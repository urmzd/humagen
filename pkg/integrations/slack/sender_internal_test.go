package slack

import (
	"strconv"
	"testing"
	"time"
)

// One person is told at most once in turnAwayEvery, and at most
// turnAwayPerMinute people are told in a minute.
func TestTheNotConnectedReplyIsBounded(t *testing.T) {
	var turned turnAway
	now := time.Unix(1700000000, 0)
	if !turned.allow("T01/U0EVE", now) {
		t.Fatal("the first reply to a person was held back")
	}
	if turned.allow("T01/U0EVE", now.Add(turnAwayEvery-time.Second)) {
		t.Fatal("a person was told twice within the interval")
	}
	if !turned.allow("T01/U0EVE", now.Add(turnAwayEvery)) {
		t.Fatal("a person was not told again after the interval")
	}

	turned, now = turnAway{}, now.Add(time.Hour)
	for i := range turnAwayPerMinute {
		if !turned.allow("T01/U"+strconv.Itoa(i), now) {
			t.Fatalf("person %d was held back under the limit", i)
		}
	}
	if turned.allow("T01/U0LATE", now.Add(30*time.Second)) {
		t.Fatal("more people were told in a minute than the limit")
	}
	if !turned.allow("T01/U0LATE", now.Add(time.Minute)) {
		t.Fatal("nobody was told in the next minute")
	}
}
