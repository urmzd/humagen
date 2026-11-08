package slack_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/urmzd/humagen/pkg/integrations/slack"
)

func conversationAPI(t *testing.T, info string, pages ...string) *httptest.Server {
	t.Helper()
	page := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer xoxb-test" {
			t.Errorf("token %q", r.Header.Get("Authorization"))
		}
		switch r.URL.Path {
		case "/conversations.info":
			fmt.Fprint(w, info)
		case "/conversations.members":
			if page >= len(pages) {
				t.Fatal("read past the last page")
			}
			fmt.Fprint(w, pages[page])
			page++
		default:
			t.Errorf("unexpected call %s", r.URL.Path)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// A conversation's audience is read from Slack, never guessed from its id:
// private channels have C ids too.
func TestAConversationsAudienceIsReadFromSlack(t *testing.T) {
	for _, tc := range []struct {
		name, info string
		pages      []string
		want       slack.Conversation
	}{
		{"public", `{"ok":true,"channel":{"is_private":false}}`, nil, slack.Conversation{Type: slack.PublicChannel}},
		{"private with a C id", `{"ok":true,"channel":{"id":"C0PRIV","is_private":true}}`, []string{
			`{"ok":true,"members":["U1","U2"],"response_metadata":{"next_cursor":"next"}}`,
			`{"ok":true,"members":["U3"],"response_metadata":{"next_cursor":""}}`,
		}, slack.Conversation{Type: slack.PrivateChannel, Members: []string{"U1", "U2", "U3"}}},
		{"group DM", `{"ok":true,"channel":{"is_mpim":true,"is_private":true}}`, []string{`{"ok":true,"members":["U1","U4"]}`}, slack.Conversation{Type: slack.GroupDM, Members: []string{"U1", "U4"}}},
		{"DM", `{"ok":true,"channel":{"is_im":true}}`, nil, slack.Conversation{Type: slack.DirectMessage}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := conversationAPI(t, tc.info, tc.pages...)
			c := mustConnector(t, slack.Config{FileToken: "xoxb-test", WebAPI: api.URL})
			got, err := c.Conversation(t.Context(), "C0PRIV")
			if err != nil {
				t.Fatal(err)
			}
			if got.Type != tc.want.Type || !slices.Equal(got.Members, tc.want.Members) {
				t.Fatalf("conversation = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestAConversationSlackRefusesIsAnError(t *testing.T) {
	api := conversationAPI(t, `{"ok":false,"error":"channel_not_found"}`)
	c := mustConnector(t, slack.Config{FileToken: "xoxb-test", WebAPI: api.URL})
	if _, err := c.Conversation(t.Context(), "C0GONE"); err == nil {
		t.Fatal("a refused read gave an audience")
	}
}

func mustConnector(t *testing.T, cfg slack.Config) *slack.Connector {
	t.Helper()
	if cfg.API == nil {
		cfg.API = slack.NewFake()
	}
	c, err := slack.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
