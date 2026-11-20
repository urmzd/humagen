package slack

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

// fakeSlack answers Slack's two upload methods and takes the bytes.
func fakeSlack(t *testing.T, completeErr string) (*httptest.Server, func() []string, func() map[string]url.Values, func() []byte) {
	t.Helper()
	var mu sync.Mutex
	var order []string
	forms := map[string]url.Values{}
	var bytes []byte
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/files.getUploadURLExternal", "/api/files.completeUploadExternal":
			if r.Header.Get("Authorization") != "Bearer xoxb-test" {
				t.Errorf("%s without the bot token", r.URL.Path)
			}
			_ = r.ParseForm()
			method := strings.TrimPrefix(r.URL.Path, "/api/")
			order = append(order, method)
			forms[method] = r.PostForm
			if method == "files.getUploadURLExternal" {
				_, _ = io.WriteString(w, `{"ok":true,"upload_url":"`+srv.URL+`/upload/v1/abc","file_id":"F42"}`)
				return
			}
			if completeErr != "" {
				_, _ = io.WriteString(w, `{"ok":false,"error":"`+completeErr+`"}`)
				return
			}
			_, _ = io.WriteString(w, `{"ok":true}`)
		case "/upload/v1/abc":
			order = append(order, "upload")
			bytes, _ = io.ReadAll(r.Body)
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	base, client, host := slackAPIBase, uploadClient, uploadHost
	slackAPIBase, uploadClient = srv.URL+"/api", srv.Client()
	uploadHost = func(u *url.URL) bool { return strings.HasPrefix(u.String(), srv.URL) }
	t.Cleanup(func() { slackAPIBase, uploadClient, uploadHost = base, client, host })
	return srv,
		func() []string { mu.Lock(); defer mu.Unlock(); return append([]string(nil), order...) },
		func() map[string]url.Values { mu.Lock(); defer mu.Unlock(); return forms },
		func() []byte { mu.Lock(); defer mu.Unlock(); return bytes }
}

func TestAClipIsUploadedIntoTheThread(t *testing.T) {
	_, order, forms, got := fakeSlack(t, "")
	c := &Connector{cfg: Config{FileToken: "xoxb-test"}}
	if err := c.UploadClip(context.Background(), "C1", "1700000000.000100", "answer.mp3", "Spoken answer", []byte("ID3-audio")); err != nil {
		t.Fatal(err)
	}
	if o := order(); strings.Join(o, ",") != "files.getUploadURLExternal,upload,files.completeUploadExternal" {
		t.Fatalf("order %v", o)
	}
	f := forms()
	if g := f["files.getUploadURLExternal"]; g.Get("filename") != "answer.mp3" || g.Get("length") != "9" {
		t.Fatalf("get form %v", g)
	}
	if c := f["files.completeUploadExternal"]; c.Get("channel_id") != "C1" || c.Get("thread_ts") != "1700000000.000100" || c.Get("files") != `[{"id":"F42","title":"Spoken answer"}]` {
		t.Fatalf("complete form %v", c)
	}
	if string(got()) != "ID3-audio" {
		t.Fatalf("bytes %q", got())
	}
}

func TestAnUploadSlackRefusesSaysSo(t *testing.T) {
	_, _, _, _ = fakeSlack(t, "not_in_channel")
	c := &Connector{cfg: Config{FileTokenSource: func(context.Context) (string, error) { return "xoxb-test", nil }}}
	err := c.UploadClip(context.Background(), "C1", "", "answer.wav", "Spoken answer", []byte("RIFF"))
	if !errors.Is(err, ErrUpload) || !strings.Contains(err.Error(), "not_in_channel") {
		t.Fatalf("err = %v", err)
	}
	for _, bad := range []struct {
		c     *Connector
		audio []byte
	}{{&Connector{cfg: Config{}}, []byte("x")}, {&Connector{cfg: Config{FileToken: "t"}}, nil}} {
		if err := bad.c.UploadClip(context.Background(), "C1", "", "a.mp3", "", bad.audio); !errors.Is(err, ErrUpload) {
			t.Fatalf("err = %v", err)
		}
	}
}

// An upload address that is not Slack's is never sent the bytes.
func TestTheBytesGoOnlyToSlack(t *testing.T) {
	_, order, _, _ := fakeSlack(t, "")
	uploadHost = func(u *url.URL) bool { return u.Scheme == "https" && u.Host == "files.slack.com" }
	c := &Connector{cfg: Config{FileToken: "xoxb-test"}}
	if err := c.UploadClip(context.Background(), "C1", "", "a.mp3", "", []byte("x")); !errors.Is(err, ErrUpload) {
		t.Fatalf("err = %v", err)
	}
	if o := order(); len(o) != 1 {
		t.Fatalf("calls after a foreign address: %v", o)
	}
}
