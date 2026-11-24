package github

import (
	"strings"
	"testing"

	presentation "github.com/urmzd/humagen/internal/presentation/github"
	transport "github.com/urmzd/saige/flow/connectors/github"
)

// The commits are read for an asker only when the list is whole and every
// author and committer is linked to an account; the asker wrote one when
// they authored or committed it.
func TestCommitsAreReadWholeOrNotAtAll(t *testing.T) {
	head := strings.Repeat("a", 40)
	other := strings.Repeat("b", 40)
	theirs := transport.PRCommit{SHA: head, AuthorID: 583231, CommitterID: 583231}
	many := make([]transport.PRCommit, transport.MaxPullRequestCommits)
	for i := range many {
		many[i] = theirs
	}
	for name, c := range map[string]struct {
		commits         []transport.PRCommit
		asker           int64
		read, committed bool
	}{
		"another person's commits":    {commits: []transport.PRCommit{theirs}, asker: 1001, read: true},
		"the asker authored one":      {commits: []transport.PRCommit{theirs, {SHA: other, AuthorID: 1001, CommitterID: 583231}}, asker: 1001, read: true, committed: true},
		"the asker committed one":     {commits: []transport.PRCommit{theirs, {SHA: other, AuthorID: 583231, CommitterID: 1001}}, asker: 1001, read: true, committed: true},
		"web-flow committed a merge":  {commits: []transport.PRCommit{theirs, {SHA: other, AuthorID: 583231, CommitterID: 19864447, CommitterLogin: "web-flow"}}, asker: 1001, read: true},
		"an author with no account":   {commits: []transport.PRCommit{theirs, {SHA: other, CommitterID: 583231}}, asker: 1001},
		"a committer with no account": {commits: []transport.PRCommit{theirs, {SHA: other, AuthorID: 583231}}, asker: 1001},
		"no commits":                  {asker: 1001},
		"at the bound":                {commits: many, asker: 1001},
		"the reviewed commit absent":  {commits: []transport.PRCommit{{SHA: other, AuthorID: 583231, CommitterID: 583231}}, asker: 1001},
		"an asker with no ID":         {commits: []transport.PRCommit{theirs}},
	} {
		read, committed := commitsRead(c.commits, head, c.asker)
		if read != c.read || committed != c.committed {
			t.Errorf("%s: read %v committed %v, want %v %v", name, read, committed, c.read, c.committed)
		}
	}
}

// A Co-authored-by address names the asker when it is one the client maps
// them to or their GitHub no-reply address, in any case.
func TestACoAuthorAddressNamesTheAsker(t *testing.T) {
	grant := ReviewGrant{Asker: "Ada", AskerID: 1001, AskerEmails: []string{"ada@acme.example"}}
	for email, want := range map[string]bool{
		"ADA@acme.example":                   true,
		" 1001+ada@users.noreply.github.com": true,
		"ada@users.noreply.github.com":       true,
		"1002+ada@users.noreply.github.com":  false,
		"octocat@acme.example":               false,
		"":                                   false,
	} {
		if got := coAuthored([]string{"someone@acme.example", email}, grant); got != want {
			t.Errorf("%q: %v, want %v", email, got, want)
		}
	}
	if coAuthored([]string{"@users.noreply.github.com"}, ReviewGrant{}) {
		t.Error("an asker with no login matched an empty no-reply address")
	}
}

// sinkMarkers are the two markers the review sink adds to a review it
// posts.
var sinkMarkers = "\n\n<!-- humagen-review:" + strings.Repeat("c", 64) + " -->\n\n<!-- humagen-review-payload:" + strings.Repeat("d", 64) + " -->"

// Only a review the sink posted, whose host-written mark names the agent,
// is that agent's: a line like the mark in model text names nobody.
func TestStaleReviewsAreTheAgentsByTheHostsMark(t *testing.T) {
	head, old := strings.Repeat("a", 40), strings.Repeat("b", 40)
	modelText := "Looks fine.\n\n```\n" + presentation.AgentMark("gatekeeper") + "\n```"
	own := []transport.ReviewRecord{
		{ReviewResult: transport.ReviewResult{ID: 1}, State: "CHANGES_REQUESTED", CommitID: old, Body: presentation.WithAgentMark("Fix it.", "gatekeeper") + sinkMarkers},
		{ReviewResult: transport.ReviewResult{ID: 2}, State: "CHANGES_REQUESTED", CommitID: old, Body: presentation.WithAgentMark(modelText, "security") + sinkMarkers},
		{ReviewResult: transport.ReviewResult{ID: 3}, State: "CHANGES_REQUESTED", CommitID: old, Body: modelText + sinkMarkers},
		{ReviewResult: transport.ReviewResult{ID: 4}, State: "APPROVED", CommitID: old, Body: presentation.WithAgentMark("Good.", "gatekeeper") + sinkMarkers},
		{ReviewResult: transport.ReviewResult{ID: 5}, State: "CHANGES_REQUESTED", CommitID: old, Body: "Fix it.\n\n" + presentation.AgentMark("gatekeeper")},
	}
	if got := staleReviews(own, "gatekeeper", head, "COMMENT"); len(got) != 1 || got[0] != 4 {
		t.Fatalf("stale approvals %v, want [4]", got)
	}
	if got := staleReviews(own, "gatekeeper", head, "APPROVE"); len(got) != 0 {
		t.Fatalf("stale approvals under a new approval %v, want none", got)
	}
	// The latest of gatekeeper's reviews is the approval (4): its earlier
	// change request (1) is replaced; another agent's, a review it posted
	// without the sink and one it posted another way are not.
	if got := replacedChangeRequests(own, "gatekeeper"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("replaced change requests %v, want [1]", got)
	}
	latestAsks := append(own, transport.ReviewRecord{ReviewResult: transport.ReviewResult{ID: 6}, State: "CHANGES_REQUESTED", CommitID: head, Body: presentation.WithAgentMark("Still.", "gatekeeper") + sinkMarkers})
	if got := replacedChangeRequests(latestAsks, "gatekeeper"); len(got) != 0 {
		t.Fatalf("replaced change requests while the latest asks for changes %v, want none", got)
	}
}
