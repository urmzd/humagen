package knowledge

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/mcptools"
)

// oneDriveFake serves Microsoft Graph and its download hosts from one httptest
// server. Requests keep the Host header of the address the provider asked for.
func oneDriveFake(t *testing.T, handler http.HandlerFunc) HTTPProvider {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	inner := server.Client().Transport
	return HTTPProvider{Kind: "onedrive", Client: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" {
			t.Errorf("plain HTTP request to %s", r.URL.Host)
		}
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return inner.RoundTrip(r)
	})}}
}

// requests records the host and path of each request a fake answers.
type requests struct {
	mu   sync.Mutex
	seen []string
}

func (q *requests) add(r *http.Request) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.seen = append(q.seen, r.Host+r.URL.Path)
}
func (q *requests) has(part string) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, s := range q.seen {
		if strings.Contains(s, part) {
			return true
		}
	}
	return false
}

const graphHost = "graph.microsoft.com"

func item(id, name, mime string, size int) string {
	return fmt.Sprintf(`{"id":%q,"name":%q,"size":%d,"webUrl":"https://onedrive.live.com/?id=%s","file":{"mimeType":%q}}`, id, name, size, id, mime)
}

// folder answers a two-page listing of folder F!1, a 302 for each content read
// and the text from the download host.
func folder(t *testing.T, seen *requests, texts map[string]string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		seen.add(r)
		switch {
		case r.Host == graphHost && r.URL.Path == "/v1.0/me/drive/items/F!1/children":
			if r.Header.Get("Authorization") != "Bearer graph-secret" {
				t.Errorf("listing without credential: %s", r.URL)
			}
			if r.URL.Query().Get("$skiptoken") == "" {
				if !strings.Contains(r.URL.RawQuery, "$select=") {
					t.Errorf("first page without selected fields: %s", r.URL)
				}
				fmt.Fprintf(w, `{"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/drive/items/F!1/children?$skiptoken=two","value":[%s,%s,%s,%s]}`,
					item("A!2", "notes.md", "text/markdown", 20),
					item("A!3", "plan.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", 900),
					`{"id":"A!4","name":"archive","webUrl":"https://onedrive.live.com/?id=A!4","folder":{"childCount":3}}`,
					`{"id":"A!5","name":"shared.txt","webUrl":"https://onedrive.live.com/?id=A!5","remoteItem":{"id":"B!9"}}`)
				return
			}
			fmt.Fprintf(w, `{"value":[%s,%s]}`, item("A!6", "Data.CSV", "text/csv", 30), item("A!7", "huge.txt", "text/plain", MaxImportBytes+1))
		case r.Host == graphHost && strings.HasSuffix(r.URL.Path, "/content"):
			if r.Header.Get("Authorization") != "Bearer graph-secret" {
				t.Errorf("content read without credential")
			}
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1.0/me/drive/items/"), "/content")
			w.Header().Set("Location", "https://public.dm.files.1drv.com/y4m/"+url.PathEscape(id))
			w.WriteHeader(http.StatusFound)
		case r.Host == "public.dm.files.1drv.com":
			if r.Header.Get("Authorization") != "" {
				t.Errorf("bearer token sent to the download host")
			}
			text, ok := texts[strings.TrimPrefix(r.URL.Path, "/y4m/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(text))
		default:
			t.Errorf("unexpected request %s%s", r.Host, r.URL)
			http.NotFound(w, r)
		}
	}
}

func TestOneDriveListsPagedChildrenAndReadsSelectedText(t *testing.T) {
	seen := &requests{}
	p := oneDriveFake(t, folder(t, seen, map[string]string{"A!2": "release notes graph-secret", "A!6": "a,b\n1,2"}))
	secret := mcptools.NewSecret("graph-secret")
	resources, err := p.Resources(t.Context(), "F!1", secret)
	if err != nil || len(resources) != 6 {
		t.Fatalf("inventory %+v %v", resources, err)
	}
	want := map[string]struct {
		supported bool
		kind      string
		reason    string
	}{
		"A!2": {true, "text/markdown", ""},
		"A!3": {false, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", "Format is not supported"},
		"A!4": {false, "folder", "Format is not supported"},
		"A!5": {false, "shared", "Shared from another drive"},
		"A!6": {true, "text/csv", ""},
		"A!7": {false, "text/plain", "larger than"},
	}
	for _, r := range resources {
		w := want[r.ID]
		if r.Supported != w.supported || r.Kind != w.kind || !strings.Contains(r.UnsupportedReason, w.reason) || r.URI != "https://onedrive.live.com/?id="+r.ID {
			t.Errorf("resource %+v, want %+v", r, w)
		}
	}
	docs, err := p.FetchSelected(t.Context(), "F!1", []string{"A!2", "A!6"}, secret)
	if err != nil || len(docs) != 2 || docs[0].ID != "A!2" || docs[0].Title != "notes.md" || docs[1].Text != "a,b\n1,2" {
		t.Fatalf("selected %+v %v", docs, err)
	}
	if strings.Contains(docs[0].Text, "graph-secret") || !strings.Contains(docs[0].Text, mcptools.Redacted) {
		t.Fatal("echoed credential kept in the text", docs[0].Text)
	}
	if seen.has("A!3") || seen.has("A!7") {
		t.Fatal("unselected file was read")
	}
	if _, err := p.FetchSelected(t.Context(), "F!1", []string{"A!3"}, secret); FailureCause(err) != Unsupported {
		t.Fatal("selected Office file imported", err)
	}
	if _, err := p.FetchSelected(t.Context(), "F!1", []string{"Z!1"}, secret); FailureCause(err) != AuthExpired {
		t.Fatal("missing selected file silently omitted", err)
	}
	if _, err := p.Fetch(t.Context(), "F!1", secret); FailureCause(err) != Unsupported {
		t.Fatal("full copy silently skipped unsupported files", err)
	}
	if _, err := p.Fetch(t.Context(), "https://internal/secret", secret); FailureCause(err) != Unsupported {
		t.Fatal("arbitrary origin accepted")
	}
}

func TestOneDriveRootListsTheTopOfTheDrive(t *testing.T) {
	p := oneDriveFake(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Host != graphHost || r.URL.Path != "/v1.0/me/drive/root/children" {
			t.Errorf("unexpected request %s%s", r.Host, r.URL.Path)
		}
		fmt.Fprintf(w, `{"value":[%s]}`, item("01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K", "readme.txt", "text/plain", 5))
	})
	resources, err := p.Resources(t.Context(), "root", mcptools.NewSecret("graph-secret"))
	if err != nil || len(resources) != 1 || !resources[0].Supported {
		t.Fatalf("%+v %v", resources, err)
	}
}

func TestOneDriveContentRefusesUnsafeRedirectsAndBinary(t *testing.T) {
	for _, tc := range []struct {
		name, location, body string
		cause                Cause
	}{
		{"another host", "https://attacker.test/file", "", Unavailable},
		{"plain HTTP", "http://public.dm.files.1drv.com/file", "", Unavailable},
		{"credentials in URL", "https://user:pass@public.dm.files.1drv.com/file", "", Unavailable},
		{"other port", "https://public.dm.files.1drv.com:8443/file", "", Unavailable},
		{"look-alike domain", "https://files.1drv.com.attacker.test/file", "", Unavailable},
		{"binary", "https://contoso-my.sharepoint.com/download", "PK\x03\x04\x00\xff", Unsupported},
		{"direct body", "", "plain text", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := &requests{}
			p := oneDriveFake(t, func(w http.ResponseWriter, r *http.Request) {
				seen.add(r)
				switch {
				case r.Host == graphHost && strings.HasSuffix(r.URL.Path, "/children"):
					fmt.Fprintf(w, `{"value":[%s]}`, item("A!2", "notes.txt", "text/plain", 10))
				case r.Host == graphHost && tc.location != "":
					w.Header().Set("Location", tc.location)
					w.WriteHeader(http.StatusFound)
				default:
					_, _ = w.Write([]byte(tc.body))
				}
			})
			docs, err := p.FetchSelected(t.Context(), "F!1", []string{"A!2"}, mcptools.NewSecret("graph-secret"))
			if tc.cause == "" {
				if err != nil || len(docs) != 1 || docs[0].Text != tc.body {
					t.Fatalf("%+v %v", docs, err)
				}
				return
			}
			if FailureCause(err) != tc.cause {
				t.Fatalf("cause %v, want %s", err, tc.cause)
			}
			if seen.has("attacker") {
				t.Fatal("followed a redirect off OneDrive")
			}
		})
	}
}

func TestOneDriveFailuresMapToBoundedCauses(t *testing.T) {
	for _, tc := range []struct {
		status int
		body   string
		cause  Cause
	}{
		{401, `{"error":{"code":"unauthenticated","message":"sensitive"}}`, AuthExpired},
		{403, `{"error":{"code":"accessDenied","message":"sensitive"}}`, AuthExpired},
		{403, `{"error":{"code":"activityLimitReached","message":"sensitive"}}`, Quota},
		{403, `{"error":{"code":"accessDenied","innerError":{"code":"throttledRequest"}}}`, Quota},
		{404, `{"error":{"code":"itemNotFound","message":"sensitive"}}`, AuthExpired},
		{429, `{"error":{"code":"TooManyRequests","message":"sensitive"}}`, Quota},
		{509, `{"error":{"code":"bandwidthLimitExceeded","message":"sensitive"}}`, Quota},
		{400, `{"error":{"code":"invalidRequest","message":"sensitive"}}`, Unsupported},
		{503, `{"error":{"code":"serviceNotAvailable","message":"sensitive"}}`, Unavailable},
		{500, `sensitive`, Unavailable},
	} {
		t.Run(fmt.Sprint(tc.status, tc.body), func(t *testing.T) {
			p := oneDriveFake(t, func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			_, err := p.Resources(t.Context(), "F!1", mcptools.NewSecret("graph-secret"))
			if FailureCause(err) != tc.cause || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("listing: %v, want %s", err, tc.cause)
			}
		})
	}
	// The download host's refusal is mapped the same way.
	p := oneDriveFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/children"):
			fmt.Fprintf(w, `{"value":[%s]}`, item("A!2", "notes.txt", "text/plain", 10))
		case r.Host == graphHost:
			w.Header().Set("Location", "https://public.dm.files.1drv.com/expired")
			w.WriteHeader(http.StatusFound)
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	})
	if _, err := p.FetchSelected(t.Context(), "F!1", []string{"A!2"}, mcptools.NewSecret("graph-secret")); FailureCause(err) != AuthExpired {
		t.Fatal("download refusal", err)
	}
	if _, err := p.Resources(t.Context(), "F!1", mcptools.Secret{}); FailureCause(err) != AuthExpired {
		t.Fatal("read without a credential", err)
	}
	refused := HTTPProvider{Kind: "onedrive", Client: &http.Client{Transport: transportFunc(func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("token in transport error")
	})}}
	if _, err := refused.Fetch(t.Context(), "F!1", mcptools.NewSecret("graph-secret")); err == nil || strings.Contains(err.Error(), "token") {
		t.Fatal(err)
	}
}

func TestOneDriveBoundsListingAndImport(t *testing.T) {
	page := func(ids int, next string) string {
		items := make([]string, ids)
		for i := range items {
			items[i] = item(fmt.Sprintf("P!%d", i), fmt.Sprintf("n%d.txt", i), "text/plain", 1)
		}
		out := `{"value":[` + strings.Join(items, ",") + `]`
		if next != "" {
			out += `,"@odata.nextLink":"` + next + `"`
		}
		return out + "}"
	}
	for _, tc := range []struct {
		name  string
		serve func(w http.ResponseWriter, r *http.Request)
	}{
		{"more children than one import holds", func(w http.ResponseWriter, r *http.Request) {
			next := ""
			if r.URL.Query().Get("$skiptoken") == "" {
				next = "https://graph.microsoft.com/v1.0/me/drive/items/F!1/children?$skiptoken=two"
			}
			fmt.Fprint(w, page(MaxImportDocuments/2+1, next))
		}},
		{"next page off Graph", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, page(1, "https://attacker.test/v1.0/children"))
		}},
		{"endless paging", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"value":[],"@odata.nextLink":"https://graph.microsoft.com/v1.0/me/drive/items/F!1/children?$skiptoken=again"}`)
		}},
		{"listing without value", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"error":null}`)
		}},
		{"unsafe item ID", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprintf(w, `{"value":[%s]}`, item("../secret", "n.txt", "text/plain", 1))
		}},
		{"link that is not HTTPS", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"value":[{"id":"A!1","name":"n.txt","webUrl":"javascript:alert(1)","file":{}}]}`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := oneDriveFake(t, tc.serve)
			if _, err := p.Resources(t.Context(), "F!1", mcptools.NewSecret("graph-secret")); FailureCause(err) != Unsupported {
				t.Fatal(err)
			}
		})
	}
	// Selected files whose text together passes the import limit fail as a whole.
	chunk := strings.Repeat("x", MaxImportBytes/2+1)
	p := oneDriveFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/children"):
			fmt.Fprintf(w, `{"value":[%s,%s]}`, item("A!1", "a.txt", "text/plain", len(chunk)), item("A!2", "b.txt", "text/plain", len(chunk)))
		case r.Host == graphHost:
			w.Header().Set("Location", "https://public.dm.files.1drv.com/file")
			w.WriteHeader(http.StatusFound)
		default:
			_, _ = w.Write([]byte(chunk))
		}
	})
	if _, err := p.FetchSelected(t.Context(), "F!1", []string{"A!1", "A!2"}, mcptools.NewSecret("graph-secret")); FailureCause(err) != Unsupported {
		t.Fatal("import passed the byte limit", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Fetch(ctx, "F!1", mcptools.NewSecret("graph-secret")); FailureCause(err) != Interrupted {
		t.Fatal("canceled read", err)
	}
}

func TestOneDriveItemIDsAreAcceptedOnlyForOneDrive(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	s := NewStore()
	if _, err := s.Create("tenant", "files", "", "ana", now); err != nil {
		t.Fatal(err)
	}
	cfg := SourceConfig{Mode: Indexed, Provider: "onedrive", Origin: "D4648F06C91D9D3D!54927", Owner: "ana", Resources: []string{"D4648F06C91D9D3D!54928"}}
	if _, err := s.Configure("tenant", "files", cfg, now); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []SourceConfig{
		{Mode: Indexed, Provider: "drive", Origin: "D4648F06C91D9D3D!54927", Owner: "ana"},
		{Mode: Indexed, Provider: "onedrive", Origin: "folder/../root", Owner: "ana"},
		{Mode: Indexed, Provider: "onedrive", Origin: "root", Owner: "ana", Resources: []string{"a?b"}},
	} {
		if _, err := s.Configure("tenant", "files", bad, now); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
