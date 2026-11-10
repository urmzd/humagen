package httpapi

import (
	"fmt"
	"testing"
)

// exerciseSessionVoiceContract checks voice mode's two routes against the
// spec, in a workspace of their own: off until a voice provider is set up,
// then a recording heard and a run's answer spoken.
func exerciseSessionVoiceContract(t *testing.T, c *contract) {
	t.Helper()
	f := c.f
	token := f.person(t, "voice-contract", "vic", "Vic", "humagen-admins")
	c.call(token, "POST", "/api/agents", "/api/agents", helperSpec, 201)
	root := c.call(token, "POST", "/api/sessions", "/api/sessions", `{"agent":"helper","project":"~","text":"Read me the plan"}`, 201)
	id := root.str("id")
	runID := ""
	for _, msg := range sliceOf(root.field("messages")) {
		if run := obj(obj(msg)["run"]); run != nil {
			runID, _ = run["id"].(string)
		}
	}
	if runID == "" {
		t.Fatal("voice session has no run")
	}
	c.settled(token, runID)
	hear := "/api/sessions/{id}/voice/transcriptions"
	speak := "/api/sessions/{id}/voice/speech"
	path := "/api/sessions/" + id + "/voice"
	octets := []string{"Content-Type", "application/octet-stream"}
	body := fmt.Sprintf(`{"run":%q,"from":0}`, runID)

	// Off until the workspace has a voice provider.
	c.call(token, "POST", hear, path+"/transcriptions", webmVoice, 409, octets...)
	c.call(token, "POST", speak, path+"/speech", body, 409)
	c.call(token, "PUT", "/api/voice/provider", "/api/voice/provider", `{"kind":"groq","key":"gsk_contract"}`, 200)

	c.call(token, "POST", hear, path+"/transcriptions", webmVoice, 200, octets...)
	c.call(token, "POST", hear, path+"/transcriptions", "not audio", 400, octets...)
	c.call(token, "POST", hear, "/api/sessions/s_missing/voice/transcriptions", webmVoice, 404, octets...)
	spoken := c.call(token, "POST", speak, path+"/speech", body, 200)
	if spoken.str("text") == "" {
		t.Fatalf("nothing was spoken of a finished answer: %v", spoken)
	}
	c.call(token, "POST", speak, path+"/speech", `{"run":"r_missing","from":0}`, 404)
	c.call(token, "POST", speak, path+"/speech", `{"from":0}`, 400)
	session := c.call(token, "GET", "/api/sessions/{id}", "/api/sessions/"+id, "", 200)
	if v := obj(session.field("voice")); v == nil || fmt.Sprint(v["calls"]) != "2" {
		t.Fatalf("session voice usage = %v", session.field("voice"))
	}
	// A spoken message is put right by its author; a typed one is not.
	posted := c.call(token, "POST", "/api/sessions/{id}/messages", "/api/sessions/"+id+"/messages", `{"text":"Book the room for Fryday","spoken":true}`, 201)
	mid := ""
	for _, m := range sliceOf(posted.field("messages")) {
		if msg := obj(obj(m)["message"]); msg != nil && msg["text"] == "Book the room for Fryday" {
			mid, _ = msg["id"].(string)
		}
	}
	if mid == "" {
		t.Fatal("the spoken message is not in the session")
	}
	edit := "/api/sessions/{id}/messages/{mid}"
	c.call(token, "PATCH", edit, "/api/sessions/"+id+"/messages/"+mid, `{"text":"Book the room for Friday"}`, 200)
	c.call(token, "PATCH", edit, "/api/sessions/"+id+"/messages/m-1", `{"text":"Something else"}`, 400)
	other := f.person(t, "voice-contract", "wes", "Wes", "humagen-admins")
	c.call(other, "PATCH", edit, "/api/sessions/"+id+"/messages/"+mid, `{"text":"Not mine"}`, 403)
	c.call(token, "DELETE", "/api/voice/provider", "/api/voice/provider", "", 204)
}
