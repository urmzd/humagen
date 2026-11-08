package slack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Conversation types, as Slack's conversation types name them.
const (
	PublicChannel  = "public_channel"
	PrivateChannel = "private_channel"
	GroupDM        = "mpim"
	DirectMessage  = "im"
)

// MaxMembers is the most members a Conversation lists. A larger private
// conversation is refused rather than cut, so nobody reads as a member who
// was left out.
const MaxMembers = 500

// Conversation is who can read a Slack conversation: its type and, for a
// private channel or a group DM, the Slack ids of its members.
type Conversation struct {
	Type    string
	Members []string
}

var errTooManyMembers = errors.New("slack: the conversation has more members than are read")

// Conversation reads a conversation's type with conversations.info and, for
// a private channel or a group DM, its members with conversations.members,
// with this installation's bot token.
func (c *Connector) Conversation(ctx context.Context, channel string) (Conversation, error) {
	var info struct {
		Channel struct {
			IsIM      bool `json:"is_im"`
			IsMPIM    bool `json:"is_mpim"`
			IsPrivate bool `json:"is_private"`
		} `json:"channel"`
	}
	if err := c.call(ctx, "conversations.info", url.Values{"channel": {channel}}, &info); err != nil {
		return Conversation{}, err
	}
	switch ch := info.Channel; {
	case ch.IsIM:
		return Conversation{Type: DirectMessage}, nil
	case ch.IsMPIM:
		return c.withMembers(ctx, channel, GroupDM)
	case ch.IsPrivate:
		return c.withMembers(ctx, channel, PrivateChannel)
	}
	return Conversation{Type: PublicChannel}, nil
}

func (c *Connector) withMembers(ctx context.Context, channel, kind string) (Conversation, error) {
	out := Conversation{Type: kind}
	cursor := ""
	for {
		q := url.Values{"channel": {channel}, "limit": {"200"}}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		var page struct {
			Members  []string `json:"members"`
			Metadata struct {
				NextCursor string `json:"next_cursor"`
			} `json:"response_metadata"`
		}
		if err := c.call(ctx, "conversations.members", q, &page); err != nil {
			return Conversation{}, err
		}
		out.Members = append(out.Members, page.Members...)
		if len(out.Members) > MaxMembers {
			return Conversation{}, errTooManyMembers
		}
		if cursor = page.Metadata.NextCursor; cursor == "" {
			return out, nil
		}
	}
}

// call makes one Web API read with the bot token and decodes its answer,
// failing on Slack's ok: false.
func (c *Connector) call(ctx context.Context, method string, q url.Values, out any) error {
	token := c.cfg.FileToken
	if c.cfg.FileTokenSource != nil {
		t, err := c.cfg.FileTokenSource(ctx)
		if err != nil {
			return fmt.Errorf("slack: %s: token: %w", method, err)
		}
		token = t
	}
	if token == "" {
		return fmt.Errorf("slack: %s: no bot token", method)
	}
	base := strings.TrimSuffix(c.cfg.WebAPI, "/")
	if base == "" {
		base = "https://slack.com/api"
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slack: %s: status %d", method, resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	var status struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		return fmt.Errorf("slack: %s: %w", method, err)
	}
	if !status.OK {
		return fmt.Errorf("slack: %s: %s", method, status.Error)
	}
	return json.Unmarshal(raw, out)
}
