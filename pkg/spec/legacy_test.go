package spec_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/pkg/spec"
)

func load(t *testing.T, doc string) (spec.Config, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "spec.yaml")
	if err := os.WriteFile(path, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	return spec.Load(path)
}

const slackDoc = `api_version: %s
kind: Platform
metadata: {name: default}
spec:
  surfaces:
    slack:
      enabled: true
      team: T1
      bot_token: {ref: env:SLACK_BOT_TOKEN}
      app_token: {ref: env:SLACK_APP_TOKEN}
%s`

func slack(version, fields string) string {
	return strings.Replace(strings.Replace(slackDoc, "%s", version, 1), "%s", fields, 1)
}

// A spec written before the rename still reads, with a note for each old
// name, and is written back in the new language.
func TestTheOldLanguageStillReads(t *testing.T) {
	cfg, err := load(t, slack("warp/v1", "      warp_client: {token_url: \"https://idp.example.com/t\", client_id: c, client_secret: {ref: env:WARP_SLACK_CLIENT_SECRET}}\n"))
	if err != nil {
		t.Fatal(err)
	}
	s := cfg.Platforms[0].Spec.Surfaces.Slack
	if s.HumagenClient == nil || s.HumagenClient.ClientID != "c" || s.OldClient != nil {
		t.Fatalf("slack = %+v", s)
	}
	notes := strings.Join(cfg.Notes, "\n")
	for _, want := range []string{"api_version warp/v1 still works; write humagen/v1", "surfaces.slack.warp_client still works; write humagen_client"} {
		if !strings.Contains(notes, want) {
			t.Errorf("no note %q in\n%s", want, notes)
		}
	}
	b, err := spec.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "warp") || !strings.Contains(string(b), "api_version: humagen/v1") || !strings.Contains(string(b), "humagen_client:") {
		t.Errorf("Marshal wrote:\n%s", b)
	}

	cfg, err = load(t, slack("warp/v1", "      warp_token: {ref: env:WARP_TOKEN}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if s := cfg.Platforms[0].Spec.Surfaces.Slack; s.HumagenToken == nil || s.HumagenToken.Ref != "env:WARP_TOKEN" || s.OldToken != nil {
		t.Fatalf("slack = %+v", s)
	}
}

func TestTheNewLanguageHasNoNotes(t *testing.T) {
	cfg, err := load(t, slack("humagen/v1", "      humagen_token: {ref: env:HUMAGEN_TOKEN}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Notes) != 0 {
		t.Errorf("notes %q", cfg.Notes)
	}
}

// Both names of one field is a file that disagrees with itself.
func TestBothNamesOfAFieldIsAnError(t *testing.T) {
	for _, fields := range []string{
		"      warp_token: {ref: env:A}\n      humagen_token: {ref: env:B}\n",
		"      warp_client: {token_url: \"https://i/t\", client_id: c, client_secret: {ref: env:A}}\n      humagen_client: {token_url: \"https://i/t\", client_id: c, client_secret: {ref: env:A}}\n",
	} {
		if _, err := load(t, slack("humagen/v1", fields)); err == nil || !strings.Contains(err.Error(), "not both") {
			t.Errorf("err = %v for\n%s", err, fields)
		}
	}
	if _, err := load(t, slack("other/v1", "")); err == nil {
		t.Error("an unknown api_version read")
	}
	if _, err := load(t, slack("humagen/v1", "      warp_other: x\n")); err == nil {
		t.Error("an unknown field read")
	}
}
