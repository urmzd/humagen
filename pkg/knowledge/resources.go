package knowledge

import (
	"context"
	"github.com/urmzd/fiber/pkg/mcptools"
	"net/url"
	"slices"
	"strings"
	"time"
)

// Resource is real provider inventory, not a stored connection claim.
type Resource struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	URI               string `json:"uri"`
	Kind              string `json:"kind"`
	Supported         bool   `json:"supported"`
	UnsupportedReason string `json:"unsupported_reason,omitempty"`
}
type ResourceProvider interface {
	Resources(context.Context, string, mcptools.Secret) ([]Resource, error)
	FetchSelected(context.Context, string, []string, mcptools.Secret) ([]ImportDocument, error)
}

func (p HTTPProvider) Resources(ctx context.Context, origin string, secret mcptools.Secret) ([]Resource, error) {
	if !resourceID.MatchString(origin) {
		return nil, ProviderError{Unsupported}
	}
	resources := []Resource{}
	switch p.Kind {
	case "drive":
		var listing struct {
			Next       string `json:"nextPageToken"`
			Incomplete bool   `json:"incompleteSearch"`
			Files      []struct{ ID, Name, MimeType string }
		}
		q := url.Values{"q": {"'" + origin + "' in parents and trashed = false"}, "pageSize": {"200"}, "fields": {"nextPageToken,incompleteSearch,files(id,name,mimeType,webViewLink)"}}
		if _, err := p.read(ctx, secret, "https://www.googleapis.com/drive/v3/files?"+q.Encode(), &listing); err != nil {
			return nil, err
		}
		if listing.Files == nil || listing.Next != "" || listing.Incomplete || len(listing.Files) > MaxImportDocuments {
			return nil, ProviderError{Unsupported}
		}
		for _, f := range listing.Files {
			if !resourceID.MatchString(f.ID) || len(f.Name) > 4096 {
				return nil, ProviderError{Unsupported}
			}
			supported := slices.Contains([]string{"application/vnd.google-apps.document", "text/plain", "text/markdown", "text/csv"}, f.MimeType)
			r := Resource{ID: f.ID, Name: f.Name, Kind: f.MimeType, URI: "https://drive.google.com/file/d/" + f.ID + "/view", Supported: supported}
			if !supported {
				r.UnsupportedReason = "Format is not supported for an indexed text copy"
			}
			resources = append(resources, r)
		}
	case "notion":
		var page struct {
			Object     string
			ID         string
			URL        string
			Properties map[string]struct {
				Type  string
				Title []struct {
					PlainText string `json:"plain_text"`
				}
			}
		}
		if _, err := p.read(ctx, secret, "https://api.notion.com/v1/pages/"+origin, &page); err != nil {
			return nil, err
		}
		if page.Object != "page" || !resourceID.MatchString(page.ID) {
			return nil, ProviderError{Unsupported}
		}
		name := origin
		for _, prop := range page.Properties {
			if prop.Type == "title" {
				var text strings.Builder
				for _, part := range prop.Title {
					text.WriteString(part.PlainText)
				}
				if text.Len() > 0 {
					name = text.String()
				}
			}
		}
		resources = append(resources, Resource{ID: origin, Name: name, Kind: "notion_page", URI: "https://www.notion.so/" + strings.ReplaceAll(origin, "-", ""), Supported: true})
	default:
		return nil, ProviderError{Unsupported}
	}
	for i := range resources {
		resources[i].Name = strings.ReplaceAll(resources[i].Name, secret.Reveal(), mcptools.Redacted)
	}
	return SafeResources(resources, secret)
}
func (p HTTPProvider) FetchSelected(ctx context.Context, origin string, selected []string, secret mcptools.Secret) ([]ImportDocument, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	if len(selected) > MaxImportDocuments {
		return nil, ProviderError{Unsupported}
	}
	for _, id := range selected {
		if !resourceID.MatchString(id) {
			return nil, ProviderError{Unsupported}
		}
	}
	if p.Kind == "notion" {
		if len(selected) > 0 && (len(selected) != 1 || selected[0] != origin) {
			return nil, ProviderError{Unsupported}
		}
		return p.Fetch(ctx, origin, secret)
	}
	if p.Kind != "drive" {
		return nil, ProviderError{Unsupported}
	}
	return p.driveSelected(ctx, origin, selected, secret)
}
func (p HTTPProvider) driveSelected(ctx context.Context, origin string, selected []string, secret mcptools.Secret) ([]ImportDocument, error) {
	resources, err := p.Resources(ctx, origin, secret)
	if err != nil {
		return nil, err
	}
	docs := []ImportDocument{}
	found := map[string]bool{}
	size := 0
	for _, r := range resources {
		if len(selected) > 0 && !slices.Contains(selected, r.ID) {
			continue
		}
		found[r.ID] = true
		if !r.Supported {
			return nil, ProviderError{Unsupported}
		}
		address := "https://www.googleapis.com/drive/v3/files/" + r.ID
		if r.Kind == "application/vnd.google-apps.document" {
			address += "/export?mimeType=text%2Fplain"
		} else {
			address += "?alt=media"
		}
		body, err := p.read(ctx, secret, address, nil)
		if err != nil {
			return nil, err
		}
		size += len(body)
		if size > MaxImportBytes {
			return nil, ProviderError{Unsupported}
		}
		if strings.TrimSpace(string(body)) != "" {
			docs = append(docs, ImportDocument{ID: r.ID, Title: r.Name, URI: r.URI, Text: string(body)})
		}
	}
	for _, id := range selected {
		if !found[id] {
			return nil, ProviderError{AuthExpired}
		}
	}
	return docs, nil
}

// SafeResources bounds provider metadata and prevents echoed credentials from
// becoming display names or link targets, including decoded JSON escapes.
func SafeResources(resources []Resource, secret mcptools.Secret) ([]Resource, error) {
	if len(resources) > MaxImportDocuments {
		return nil, ProviderError{Unsupported}
	}
	out := slices.Clone(resources)
	for i, r := range out {
		if !resourceID.MatchString(r.ID) || len(r.Name) > 4096 || len(r.URI) > 2048 || len(r.Kind) > 128 || len(r.UnsupportedReason) > 256 {
			return nil, ProviderError{Unsupported}
		}
		parsed, err := url.Parse(r.URI)
		if err != nil || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" {
			return nil, ProviderError{Unsupported}
		}
		if !secret.Empty() {
			value := secret.Reveal()
			if strings.Contains(r.ID+r.URI, value) {
				return nil, ProviderError{Unsupported}
			}
			r.Name = strings.ReplaceAll(r.Name, value, mcptools.Redacted)
			r.Kind = strings.ReplaceAll(r.Kind, value, mcptools.Redacted)
			r.UnsupportedReason = strings.ReplaceAll(r.UnsupportedReason, value, mcptools.Redacted)
		}
		out[i] = r
	}
	return out, nil
}
