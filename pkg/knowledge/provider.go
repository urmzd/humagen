package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/urmzd/humagen/internal/mcptools"
)

// Provider imports one explicitly selected resource with the current reader's credential.
// It must honor context cancellation and fail rather than return a truncated
// snapshot. Implementations must not log credentials.
type Provider interface {
	Fetch(context.Context, string, mcptools.Secret) ([]ImportDocument, error)
}

// ProviderError exposes a safe cause, never the upstream response body.
type ProviderError struct{ Cause Cause }

func (e ProviderError) Error() string { return "knowledge: provider " + string(e.Cause) }

// FailureCause sanitizes transport, credential and provider failures.
func FailureCause(err error) Cause {
	var p ProviderError
	if errors.As(err, &p) {
		switch p.Cause {
		case AuthExpired, Quota, Unsupported, Unavailable, Interrupted:
			return p.Cause
		}
		return Unavailable
	}
	return Unavailable
}

// HTTPProvider reads Drive or OneDrive folders, or Notion pages. Only official
// API origins are used. Client is injectable for boundary tests; redirects are
// never followed implicitly.
type HTTPProvider struct {
	Kind   string
	Client *http.Client
}

// send performs one GET. The bearer credential is attached only when bearer is
// set, so a preauthenticated download URL never receives it.
func (p HTTPProvider) send(ctx context.Context, secret mcptools.Secret, address string, bearer bool) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, ProviderError{Unsupported}
	}
	if bearer {
		req.Header.Set("Authorization", "Bearer "+secret.Reveal())
	}
	if p.Kind == "notion" {
		req.Header.Set("Notion-Version", "2022-06-28")
	}
	client := http.Client{Timeout: 20 * time.Second}
	if p.Client != nil {
		client = *p.Client
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ProviderError{Interrupted}
		}
		return nil, ProviderError{Unavailable}
	}
	return response, nil
}

// failure maps an unsuccessful status to a bounded cause. Only a short prefix
// of the body is read, to tell a rate limit from a denial, and it is never returned.
func (p HTTPProvider) failure(response *http.Response) Cause {
	switch response.StatusCode {
	case 401, 403, 404:
		if response.StatusCode == 403 && p.limited(response) {
			return Quota
		}
		return AuthExpired
	case 429:
		return Quota
	case 509:
		// OneDrive answers 509 when the bandwidth cap is exceeded.
		if p.Kind == "onedrive" {
			return Quota
		}
	case 400:
		return Unsupported
	}
	return Unavailable
}

// limited reports whether a 403 is a rate or quota limit rather than a denial.
func (p HTTPProvider) limited(response *http.Response) bool {
	body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	switch p.Kind {
	case "drive":
		var failure struct {
			Error struct{ Errors []struct{ Reason string } }
		}
		if json.Unmarshal(body, &failure) == nil {
			for _, entry := range failure.Error.Errors {
				switch entry.Reason {
				case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
					return true
				}
			}
		}
	case "onedrive":
		var failure struct {
			Error struct {
				Code  string `json:"code"`
				Inner struct {
					Code string `json:"code"`
				} `json:"innererror"`
			} `json:"error"`
		}
		if json.Unmarshal(body, &failure) == nil {
			return failure.Error.Code == "activityLimitReached" || failure.Error.Inner.Code == "throttledRequest"
		}
	}
	return false
}

func (p HTTPProvider) read(ctx context.Context, secret mcptools.Secret, address string, out any) ([]byte, error) {
	if secret.Empty() {
		return nil, ProviderError{AuthExpired}
	}
	response, err := p.send(ctx, secret, address, true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, ProviderError{p.failure(response)}
	}
	return decodeBody(response, secret, out)
}

// decodeBody reads a bounded body, redacts an echoed credential and decodes it into out.
func decodeBody(response *http.Response, secret mcptools.Secret, out any) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(response.Body, MaxImportBytes+1))
	if err != nil || len(b) > MaxImportBytes {
		return nil, ProviderError{Unsupported}
	}
	if !secret.Empty() {
		b = []byte(strings.ReplaceAll(string(b), secret.Reveal(), mcptools.Redacted))
	}
	if out != nil && json.Unmarshal(b, out) != nil {
		return nil, ProviderError{Unsupported}
	}
	return b, nil
}

// Fetch reads only the configured folder/page, without granting access to a workspace.
func (p HTTPProvider) Fetch(ctx context.Context, origin string, secret mcptools.Secret) ([]ImportDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if !validResourceID(p.Kind, origin) {
		return nil, ProviderError{Unsupported}
	}
	var docs []ImportDocument
	var err error
	switch p.Kind {
	case "drive", "onedrive":
		docs, err = p.folderFiles(ctx, origin, nil, secret)
	case "notion":
		docs, err = p.notion(ctx, origin, secret)
	default:
		return nil, ProviderError{Unsupported}
	}
	if err != nil {
		return nil, err
	}
	// Decode first as well: JSON escapes must not bypass credential redaction.
	if !secret.Empty() {
		for i := range docs {
			docs[i].ID = strings.ReplaceAll(docs[i].ID, secret.Reveal(), mcptools.Redacted)
			docs[i].Title = strings.ReplaceAll(docs[i].Title, secret.Reveal(), mcptools.Redacted)
			docs[i].URI = strings.ReplaceAll(docs[i].URI, secret.Reveal(), mcptools.Redacted)
			docs[i].Text = strings.ReplaceAll(docs[i].Text, secret.Reveal(), mcptools.Redacted)
		}
	}
	return docs, nil
}
func (p HTTPProvider) notion(ctx context.Context, origin string, secret mcptools.Secret) ([]ImportDocument, error) {
	queue := []string{origin}
	seen := map[string]bool{}
	var text strings.Builder
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] || !resourceID.MatchString(id) || len(seen) >= 100 {
			return nil, ProviderError{Unsupported}
		}
		seen[id] = true
		// Block content is keyed by its type in Notion's response.
		var response struct {
			HasMore bool              `json:"has_more"`
			Results []json.RawMessage `json:"results"`
		}
		_, err := p.read(ctx, secret, "https://api.notion.com/v1/blocks/"+id+"/children?page_size=100", &response)
		if err != nil {
			return nil, err
		}
		if response.Results == nil || response.HasMore {
			return nil, ProviderError{Unsupported}
		}
		for _, raw := range response.Results {
			var block struct {
				ID, Type    string
				HasChildren bool `json:"has_children"`
			}
			if json.Unmarshal(raw, &block) != nil {
				return nil, ProviderError{Unsupported}
			}
			var fields map[string]json.RawMessage
			_ = json.Unmarshal(raw, &fields)
			var content struct {
				RichText []struct {
					Plain string `json:"plain_text"`
				} `json:"rich_text"`
			}
			if json.Unmarshal(fields[block.Type], &content) != nil {
				return nil, ProviderError{Unsupported}
			}
			switch block.Type {
			case "paragraph", "heading_1", "heading_2", "heading_3", "bulleted_list_item", "numbered_list_item", "quote", "callout", "toggle", "to_do", "code":
				for _, part := range content.RichText {
					text.WriteString(part.Plain)
				}
				text.WriteByte('\n')
			case "divider", "table_of_contents", "column_list", "column":
			default:
				return nil, ProviderError{Unsupported}
			}
			if text.Len() > MaxImportBytes {
				return nil, ProviderError{Unsupported}
			}
			if block.HasChildren {
				queue = append(queue, block.ID)
			}
		}
	}
	if strings.TrimSpace(text.String()) == "" {
		return []ImportDocument{}, nil
	}
	return []ImportDocument{{ID: origin, Title: "Notion page " + origin, URI: "https://www.notion.so/" + strings.ReplaceAll(origin, "-", ""), Text: text.String()}}, nil
}
