package knowledge

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/urmzd/orchagent/flow/mcptools"
)

// OneDrive is read through Microsoft Graph v1.0 with the reader's delegated
// token. A source's origin is a folder's item ID, or "root" for the top of the
// person's drive.
//
// List children: https://learn.microsoft.com/en-us/graph/api/driveitem-list-children
// Download:      https://learn.microsoft.com/en-us/graph/api/driveitem-get-content
const oneDriveAPI = "https://graph.microsoft.com/v1.0/"

// oneDriveText lists the extensions read as UTF-8 text. Graph converts Office
// documents to PDF (and a few formats to HTML or JPG), never to plain text, so
// Word, Excel and PowerPoint files are listed as unsupported.
// https://learn.microsoft.com/en-us/graph/api/driveitem-get-content-format
var oneDriveText = []string{".txt", ".md", ".markdown", ".csv", ".json", ".html", ".htm"}

// oneDriveDownloadDomains are where Graph sends a content read: a
// preauthenticated URL that is read without the bearer token. A redirect to
// any other host is refused.
var oneDriveDownloadDomains = []string{"1drv.com", "sharepoint.com", "microsoftpersonalcontent.com"}

// oneDrivePages bounds how many listing pages one inventory follows.
const oneDrivePages = 20

// oneDriveItem holds the driveItem fields the listing selects.
// https://learn.microsoft.com/en-us/graph/api/resources/driveitem
type oneDriveItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	WebURL string `json:"webUrl"`
	File   *struct {
		MimeType string `json:"mimeType"`
	} `json:"file"`
	Folder     json.RawMessage `json:"folder"`
	Package    json.RawMessage `json:"package"`
	RemoteItem json.RawMessage `json:"remoteItem"`
}

func facet(raw json.RawMessage) bool { return len(raw) > 0 && string(raw) != "null" }

// oneDriveResources lists a folder's direct children, following @odata.nextLink
// within Graph. A folder with more children than one import can hold fails
// rather than returning a partial inventory.
func (p HTTPProvider) oneDriveResources(ctx context.Context, origin string, secret mcptools.Secret) ([]Resource, error) {
	address := oneDriveAPI + "me/drive/items/" + origin + "/children"
	if origin == "root" {
		address = oneDriveAPI + "me/drive/root/children"
	}
	address += "?$select=id,name,size,webUrl,file,folder,package,remoteItem&$top=200"
	resources := []Resource{}
	for page := 0; address != ""; page++ {
		if page == oneDrivePages {
			return nil, ProviderError{Unsupported}
		}
		var listing struct {
			Next  string         `json:"@odata.nextLink"`
			Value []oneDriveItem `json:"value"`
		}
		if _, err := p.read(ctx, secret, address, &listing); err != nil {
			return nil, err
		}
		if listing.Value == nil || len(resources)+len(listing.Value) > MaxImportDocuments {
			return nil, ProviderError{Unsupported}
		}
		for _, item := range listing.Value {
			r, err := item.resource()
			if err != nil {
				return nil, err
			}
			resources = append(resources, r)
		}
		address = listing.Next
		if address != "" && !strings.HasPrefix(address, oneDriveAPI) {
			return nil, ProviderError{Unsupported}
		}
	}
	return resources, nil
}

func (item oneDriveItem) resource() (Resource, error) {
	link, err := url.Parse(item.WebURL)
	if !oneDriveID.MatchString(item.ID) || len(item.Name) > 4096 || item.Size < 0 || len(item.WebURL) > 2048 || err != nil || link.Scheme != "https" || link.Host == "" || link.User != nil {
		return Resource{}, ProviderError{Unsupported}
	}
	r := Resource{ID: item.ID, Name: item.Name, URI: item.WebURL, Kind: "file"}
	switch {
	case facet(item.RemoteItem):
		// Its ID is valid only in the drive that holds it.
		r.Kind, r.UnsupportedReason = "shared", "Shared from another drive; add it from the drive that holds it"
	case facet(item.Folder):
		r.Kind = "folder"
	case facet(item.Package):
		r.Kind = "package"
	case item.File != nil:
		if item.File.MimeType != "" && len(item.File.MimeType) <= 128 {
			r.Kind = item.File.MimeType
		}
		if item.Size > MaxImportBytes {
			r.UnsupportedReason = "File is larger than the 8 MiB import limit"
		} else {
			r.Supported = slices.Contains(oneDriveText, strings.ToLower(path.Ext(item.Name)))
		}
	}
	if !r.Supported && r.UnsupportedReason == "" {
		r.UnsupportedReason = "Format is not supported for an indexed text copy"
	}
	return r, nil
}

// oneDriveContent reads one file's bytes. Graph answers /content with 302 Found
// and a preauthenticated download URL, which is read without the bearer token.
// The text must be UTF-8.
func (p HTTPProvider) oneDriveContent(ctx context.Context, secret mcptools.Secret, id string) ([]byte, error) {
	if secret.Empty() {
		return nil, ProviderError{AuthExpired}
	}
	response, err := p.send(ctx, secret, oneDriveAPI+"me/drive/items/"+id+"/content", true)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		location, err := response.Location()
		if err != nil || !oneDriveDownload(location) {
			return nil, ProviderError{Unavailable}
		}
		download, err := p.send(ctx, secret, location.String(), false)
		if err != nil {
			return nil, err
		}
		defer download.Body.Close()
		if download.StatusCode < 200 || download.StatusCode >= 300 {
			return nil, ProviderError{p.failure(download)}
		}
		response = download
	default:
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return nil, ProviderError{p.failure(response)}
		}
	}
	body, err := decodeBody(response, secret, nil)
	if err != nil {
		return nil, err
	}
	if !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		return nil, ProviderError{Unsupported}
	}
	return body, nil
}

// oneDriveDownload accepts only an HTTPS download host under a OneDrive or
// SharePoint domain: no credentials in the URL, no IP address, no other port.
func oneDriveDownload(u *url.URL) bool {
	host := strings.ToLower(u.Hostname())
	if u.Scheme != "https" || u.User != nil || u.Port() != "" || net.ParseIP(host) != nil {
		return false
	}
	for _, domain := range oneDriveDownloadDomains {
		if strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}
