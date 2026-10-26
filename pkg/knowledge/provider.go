package knowledge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/urmzd/fiber/pkg/mcptools"
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

// HTTPProvider reads Drive folders or Notion pages. Only official API origins are used.
// Client is injectable for boundary tests; redirects are always rejected.
type HTTPProvider struct {
	Kind   string
	Client *http.Client
}

func (p HTTPProvider) read(ctx context.Context, secret mcptools.Secret, address string, out any) ([]byte, error) {
	if secret.Empty() {
		return nil, ProviderError{AuthExpired}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, ProviderError{Unsupported}
	}
	req.Header.Set("Authorization", "Bearer "+secret.Reveal())
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
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		cause := Unavailable
		switch response.StatusCode {
		case 401, 403, 404:
			cause = AuthExpired
			if response.StatusCode == 403 && p.Kind == "drive" {
				body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
				var failure struct {
					Error struct{ Errors []struct{ Reason string } }
				}
				if json.Unmarshal(body, &failure) == nil {
					for _, entry := range failure.Error.Errors {
						switch entry.Reason {
						case "rateLimitExceeded", "userRateLimitExceeded", "quotaExceeded", "dailyLimitExceeded":
							cause = Quota
						}
					}
				}
			}
		case 429:
			cause = Quota
		case 400:
			cause = Unsupported
		}
		return nil, ProviderError{cause}
	}
	b, err := io.ReadAll(io.LimitReader(response.Body, MaxImportBytes+1))
	if err != nil || len(b) > MaxImportBytes {
		return nil, ProviderError{Unsupported}
	}
	b = []byte(strings.ReplaceAll(string(b), secret.Reveal(), mcptools.Redacted))
	if out != nil && json.Unmarshal(b, out) != nil {
		return nil, ProviderError{Unsupported}
	}
	return b, nil
}

// Fetch reads only the configured folder/page, without granting access to a workspace.
func (p HTTPProvider) Fetch(ctx context.Context, origin string, secret mcptools.Secret) ([]ImportDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if !resourceID.MatchString(origin) {
		return nil, ProviderError{Unsupported}
	}
	var docs []ImportDocument
	var err error
	switch p.Kind {
	case "drive":
		docs, err = p.drive(ctx, origin, secret)
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
func (p HTTPProvider) drive(ctx context.Context, origin string, secret mcptools.Secret) ([]ImportDocument, error) {
	var listing struct {
		Next       string `json:"nextPageToken"`
		Incomplete bool   `json:"incompleteSearch"`
		Files      []struct{ ID, Name, MimeType, WebViewLink string }
	}
	q := url.Values{"q": {"'" + origin + "' in parents and trashed = false"}, "pageSize": {"200"}, "fields": {"nextPageToken,incompleteSearch,files(id,name,mimeType,webViewLink)"}}
	_, err := p.read(ctx, secret, "https://www.googleapis.com/drive/v3/files?"+q.Encode(), &listing)
	if err != nil {
		return nil, err
	}
	if listing.Files == nil || listing.Next != "" || listing.Incomplete || len(listing.Files) > MaxImportDocuments {
		return nil, ProviderError{Unsupported}
	}
	docs := []ImportDocument{}
	size := 0
	for _, f := range listing.Files {
		if !resourceID.MatchString(f.ID) {
			return nil, ProviderError{Unsupported}
		}
		address := "https://www.googleapis.com/drive/v3/files/" + f.ID
		switch f.MimeType {
		case "application/vnd.google-apps.document":
			address += "/export?mimeType=text%2Fplain"
		case "text/plain", "text/markdown", "text/csv":
			address += "?alt=media"
		default:
			return nil, ProviderError{Unsupported}
		}
		text, err := p.read(ctx, secret, address, nil)
		if err != nil {
			return nil, err
		}
		size += len(text)
		if size > MaxImportBytes {
			return nil, ProviderError{Unsupported}
		}
		if strings.TrimSpace(string(text)) == "" {
			continue
		}
		docs = append(docs, ImportDocument{ID: f.ID, Title: f.Name, URI: "https://drive.google.com/file/d/" + f.ID + "/view", Text: string(text)})
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
