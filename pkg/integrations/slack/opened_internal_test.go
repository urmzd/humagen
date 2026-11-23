package slack

import "testing"

// Only a message that opens with the app's own bot user, by the delivery's
// authorizations, is one whose next word may name an agent.
func TestOnlyTheAppsOwnOpeningMentionAddressesAnAgent(t *testing.T) {
	const auth = `"authorizations":[{"user_id":"U08BOT","is_bot":true}]`
	for body, want := range map[string]bool{
		`{` + auth + `,"event":{"type":"app_mention","text":"<@U08BOT> ops: go"}}`:                      true,
		`{` + auth + `,"event":{"type":"app_mention","text":"  <@U08BOT|humagen> ops: go"}}`:            true,
		`{` + auth + `,"event":{"type":"app_mention","text":"<@U0ALICE> ops: go <@U08BOT>"}}`:           false,
		`{` + auth + `,"event":{"type":"message","text":"ops: broke it","thread_ts":"1.2"}}`:            false,
		`{` + auth + `,"event":{"type":"message","channel_type":"im","text":"ops: go"}}`:                false,
		`{"event":{"type":"app_mention","text":"<@U08BOT> ops: go"}}`:                                   false,
		`{"authorizations":[{"user_id":"U08BOT","is_bot":false}],"event":{"text":"<@U08BOT> ops: go"}}`: false,
		`not json`: false,
	} {
		if got := openedByApp([]byte(body)); got != want {
			t.Errorf("%s: opened = %v, want %v", body, got, want)
		}
	}
	for text, want := range map[string]int{"@U08BOT ops: go": 7, "@humagen: go": 8, "ops: go": 0, "": 0} {
		if got := firstToken(text); got != want {
			t.Errorf("firstToken(%q) = %d, want %d", text, got, want)
		}
	}
}
