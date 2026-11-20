package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Slack's Web API, and the client an upload goes through. Tests point them
// at a fake server.
var (
	slackAPIBase = "https://slack.com/api"
	uploadClient = &http.Client{Timeout: 60 * time.Second}
	// uploadHost says whether Slack's upload address may be sent the bytes:
	// only Slack's own, over HTTPS, whatever an answer says.
	uploadHost = func(u *url.URL) bool {
		return u.Scheme == "https" && (u.Host == "files.slack.com" || strings.HasSuffix(u.Host, ".slack.com"))
	}
)

// MaxClipBytes bounds a clip UploadClip sends.
const MaxClipBytes = 25 << 20

// ErrUpload is an upload Slack refused or could not take.
var ErrUpload = errors.New("slack: upload")

// UploadClip uploads audio as a file in a Slack thread, with this
// installation's bot token (which needs files:write): Slack's external
// upload, an address for the bytes, the bytes, then the file shared in the
// channel's thread. A spoken answer to a voice clip is uploaded this way.
func (c *Connector) UploadClip(ctx context.Context, channel, threadTS, name, title string, audio []byte) error {
	switch {
	case channel == "" || name == "":
		return fmt.Errorf("%w: a channel and a file name are needed", ErrUpload)
	case len(audio) == 0 || len(audio) > MaxClipBytes:
		return fmt.Errorf("%w: a clip is 1 byte to %d", ErrUpload, MaxClipBytes)
	}
	token, err := c.botToken(ctx)
	if err != nil {
		return err
	}
	var got struct {
		UploadURL string `json:"upload_url"`
		FileID    string `json:"file_id"`
	}
	if err := slackForm(ctx, token, "files.getUploadURLExternal", url.Values{"filename": {name}, "length": {strconv.Itoa(len(audio))}}, &got); err != nil {
		return err
	}
	to, err := url.Parse(got.UploadURL)
	if err != nil || got.FileID == "" || !uploadHost(to) {
		return fmt.Errorf("%w: Slack gave no upload address of its own", ErrUpload)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, to.String(), bytes.NewReader(audio))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	res, err := uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpload, err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	_ = res.Body.Close()
	if res.StatusCode >= 300 {
		return fmt.Errorf("%w: the bytes were refused (%d)", ErrUpload, res.StatusCode)
	}
	files, err := json.Marshal([]map[string]string{{"id": got.FileID, "title": title}})
	if err != nil {
		return err
	}
	share := url.Values{"files": {string(files)}, "channel_id": {channel}}
	if threadTS != "" {
		share.Set("thread_ts", threadTS)
	}
	return slackForm(ctx, token, "files.completeUploadExternal", share, nil)
}

// botToken is the token this installation reads and writes files with.
func (c *Connector) botToken(ctx context.Context) (string, error) {
	if c.cfg.FileTokenSource != nil {
		t, err := c.cfg.FileTokenSource(ctx)
		if err != nil || t == "" {
			return "", fmt.Errorf("%w: no token: %v", ErrUpload, err)
		}
		return t, nil
	}
	if c.cfg.FileToken == "" {
		return "", fmt.Errorf("%w: no token", ErrUpload)
	}
	return c.cfg.FileToken, nil
}

// slackForm calls one Web API method with a form, and reads its answer into
// out when out is not nil.
func slackForm(ctx context.Context, token, method string, form url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, slackAPIBase+"/"+method, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := uploadClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUpload, method, err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrUpload, method, err)
	}
	var head struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal(raw, &head) != nil || !head.OK {
		return fmt.Errorf("%w: %s: %s", ErrUpload, method, head.Error)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
