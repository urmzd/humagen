package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/threads"
)

// An archived conversation leaves the list and waits for an admin; one
// asked for now is due at once, and only an approved one is deleted.
func TestArchiveQueuesDeletionForAnAdmin(t *testing.T) {
	f := newFixture(t)
	dana := f.person(t, "acme", "00u-dana", "Dana", "humagen-operators")
	admin := f.person(t, "acme", "00u-root", "Root", "humagen-admins")
	started := want(t, f, dana, "POST", "/api/threads", `{"visibility":"private","text":"Archive me.","post_only":true}`, http.StatusCreated)
	id := fmt.Sprint(started["thread"].(map[string]any)["id"])
	listed := func() bool {
		out := want(t, f, dana, "GET", "/api/threads", "", http.StatusOK)
		return strings.Contains(fmt.Sprint(out["threads"]), id)
	}
	if !listed() {
		t.Fatal("the new conversation is not listed")
	}

	want(t, f, dana, "POST", "/api/deletions/"+id+"/approve", "", http.StatusForbidden)
	want(t, f, dana, "GET", "/api/deletions", "", http.StatusForbidden)

	want(t, f, dana, "POST", "/api/threads/"+id+"/archive", "", http.StatusOK)
	if listed() {
		t.Fatal("an archived conversation is still listed")
	}
	queue := fmt.Sprint(want(t, f, admin, "GET", "/api/deletions", "", http.StatusOK)["deletions"])
	if !strings.Contains(queue, id) || !strings.Contains(queue, "due:false") {
		t.Fatalf("queue = %s", queue)
	}
	// Not due yet: approving is refused.
	want(t, f, admin, "POST", "/api/deletions/"+id+"/approve", "", http.StatusBadRequest)

	// Bringing it back cancels the deletion.
	want(t, f, dana, "DELETE", "/api/threads/"+id+"/archive", "", http.StatusOK)
	if !listed() {
		t.Fatal("an unarchived conversation is not listed")
	}

	want(t, f, dana, "POST", "/api/threads/"+id+"/archive", `{"delete_now":true}`, http.StatusOK)
	queue = fmt.Sprint(want(t, f, admin, "GET", "/api/deletions", "", http.StatusOK)["deletions"])
	if !strings.Contains(queue, "due:true") {
		t.Fatalf("asked for now, not due: %s", queue)
	}
	want(t, f, admin, "POST", "/api/deletions/"+id+"/approve", "", http.StatusNoContent)
	want(t, f, dana, "GET", "/api/threads/"+id, "", http.StatusNotFound)
	if strings.Contains(fmt.Sprint(want(t, f, admin, "GET", "/api/deletions", "", http.StatusOK)["deletions"]), id) {
		t.Fatal("a deleted conversation is still queued")
	}
}

func TestDeletionQueueIsBoundedAndRefillsAfterApproval(t *testing.T) {
	f := newFixture(t)
	admin := f.person(t, "acme", "root", "Root", "humagen-admins")
	for index := 0; index <= platform.MaxDeletionQueue; index++ {
		// Bulk fixture creation does not exercise the per-person post rate limit.
		thread, err := f.p.Threads.Create("acme", threads.Thread{ID: fmt.Sprintf("batch-%03d", index), Channel: "general", Visibility: threads.Shared, CreatedBy: "root"}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		id := thread.ID
		want(t, f, admin, "POST", "/api/threads/"+id+"/archive", fmt.Sprintf(`{"delete_now":%t}`, index != 0), http.StatusOK)
	}
	queue := want(t, f, admin, "GET", "/api/deletions", "", http.StatusOK)["deletions"].([]any)
	if len(queue) != platform.MaxDeletionQueue {
		t.Fatalf("queue size = %d", len(queue))
	}
	for _, row := range queue {
		if row.(map[string]any)["due"] != true {
			t.Fatal("a waiting item displaced an item due for review")
		}
	}
	id := queue[0].(map[string]any)["id"].(string)
	want(t, f, admin, "POST", "/api/deletions/"+id+"/approve", "", http.StatusNoContent)
	queue = want(t, f, admin, "GET", "/api/deletions", "", http.StatusOK)["deletions"].([]any)
	if len(queue) != platform.MaxDeletionQueue || queue[len(queue)-1].(map[string]any)["due"] != false {
		t.Fatal("approval did not expose the next waiting conversation")
	}
}
