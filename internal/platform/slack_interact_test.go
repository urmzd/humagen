package platform_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/runs"
	saigetypes "github.com/urmzd/saige/agent/types"
	"github.com/urmzd/saige/flow/signature"
)

// press is a person pressing a button on one of the app's Slack messages,
// signed as Slack signs it.
func press(t *testing.T, h http.Handler, secret, user, action string, value platform.SlackActionValue) int {
	t.Helper()
	v, _ := json.Marshal(value)
	payload, _ := json.Marshal(map[string]any{
		"type": "block_actions", "team": map[string]string{"id": team}, "user": map[string]string{"id": user}, "trigger_id": "tr-1",
		"channel": map[string]string{"id": "C01"}, "message": map[string]string{"ts": "1700000000.000900", "thread_ts": "1700000000.000100"},
		"actions": []map[string]string{{"action_id": action + "#0", "block_id": "approval", "value": string(v)}},
	})
	body := url.Values{"payload": {string(payload)}}.Encode()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + body))
	r := httptest.NewRequest(http.MethodPost, "/connectors/slack/interactive", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set(signature.HeaderSlackTimestamp, ts)
	r.Header.Set(signature.HeaderSlackSignature, "v0="+hex.EncodeToString(mac.Sum(nil)))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

// Approve and Deny on a Slack message answer the approval as the person the
// client maps the presser to, through the console's own rules: the person
// whose message raised it cannot approve it, someone mapped to nobody does
// nothing, and an approver's press resumes the run. Thumbs on the finished
// answer are that person's feedback.
func TestSlackButtonsAnswerApprovalsAndRateAnswersAsTheMappedPerson(t *testing.T) {
	f := &fakes{}
	cfg := f.config()
	cfg.Policy = operators(t, "ada", "olu")
	p := stage(t, &cast{continues: true,
		says:  map[string][][]saigetypes.Delta{"deployer": {writing("plan.md", "ship"), say("Written.")}},
		tools: map[string][]string{"deployer": {platform.ToolFileWrite}},
		shape: map[string]func(*wire.AgentSpec){"deployer": func(s *wire.AgentSpec) { s.Approvals = []string{platform.ToolFileWrite} }},
	}, cfg)
	in := slackClient("app", "A0APP", team, "deployer")
	in.Slack.Machine, in.Slack.People = "svc-slack", map[string]string{"U0HUMAN1": "ada", "U0OLU": "olu"}
	create(t, p, tenant, in)
	h := p.IngressHandler()
	if code := signedSlack(h, "secret-app", mentionOf("A0APP", team, "Ev1", "C01", "1700000000.000100")); code != http.StatusOK {
		t.Fatalf("mention = %d", code)
	}
	var run runs.Run
	within(t, "the mention parked on its approval", func() bool {
		r, ok := runOf(p, tenant, "deployer")
		run = r
		return ok && r.Status == runs.Parked && r.Question != nil
	})
	approve := platform.SlackActionValue{Run: run.ID, Target: run.Question.ID}

	for _, who := range []string{"U0HUMAN1", "U0NOBODY"} {
		if code := press(t, h, "secret-app", who, platform.SlackApproveAction, approve); code != http.StatusOK {
			t.Fatalf("%s's press answered %d", who, code)
		}
	}
	if got, _ := p.Runs.Get(tenant, run.ID); got.Status != runs.Parked {
		t.Fatalf("the requester or someone mapped to nobody approved it: %s", got.Status)
	}
	if code := press(t, h, "secret-app", "U0OLU", platform.SlackApproveAction, approve); code != http.StatusOK {
		t.Fatalf("olu's press answered %d", code)
	}
	await(t, p, run.ID, runs.Completed)

	if code := press(t, h, "secret-app", "U0OLU", platform.SlackFeedbackAction, platform.SlackActionValue{Run: run.ID, Target: "up"}); code != http.StatusOK {
		t.Fatalf("olu's thumbs answered %d", code)
	}
	ratings, err := p.Feedback.List(tenant, run.ID, 0)
	if err != nil || len(ratings) != 1 || ratings[0].Subject != "olu" || ratings[0].Rating != "up" {
		t.Fatalf("feedback = %+v, %v", ratings, err)
	}
}
