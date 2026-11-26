package github

import (
	"encoding/json"
	"strings"
	"testing"

	render "github.com/urmzd/saige/flow/connectors/github"
)

// A structured review renders as a review, with the session link under it.
func TestAReviewCommentIsTheReviewAndItsSessionLink(t *testing.T) {
	encoded, _ := json.Marshal(testReport())
	for _, v := range []render.View{
		{Agent: "reviewer", Status: render.StatusRunning, Text: string(encoded[:80])},
		{Agent: "reviewer", Status: render.StatusDone, Text: string(encoded)},
	} {
		if got, want := RenderComment(v, "https://console.example/sessions/s_1"), RenderReview(v)+SessionFooter("https://console.example/sessions/s_1"); got != want {
			t.Fatalf("a review rendered as\n%s\nwant\n%s", got, want)
		}
	}
}

// The footer links only a plain http(s) address, and keeps its link whole.
func TestTheSessionFooterLinksOnlyAPlainAddress(t *testing.T) {
	v := render.View{Agent: "humagen", Status: render.StatusDone, Text: "Done."}
	for _, bad := range []string{"", "javascript:alert(1)", "https://user:pw@console.example/s", "/sessions/s_1"} {
		if body := RenderComment(v, bad); body != "Done." {
			t.Errorf("%q gave\n%s", bad, body)
		}
	}
	if body := RenderComment(v, "https://console.example/sessions/s_(1)"); !strings.HasSuffix(body, "(https://console.example/sessions/s_%281%29)</sub>") {
		t.Errorf("a parenthesis broke the link:\n%s", body)
	}
}
