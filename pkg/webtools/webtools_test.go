package webtools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"strings"
	"testing"
)

// TestLive asks the real search and reads a real page. It runs only with
// HUMAGEN_WEB_LIVE=1, since it needs the internet and the search may refuse
// a client it takes for a program.
func TestLive(t *testing.T) {
	if os.Getenv("HUMAGEN_WEB_LIVE") != "1" {
		t.Skip("set HUMAGEN_WEB_LIVE=1 to reach the real web")
	}
	c := New()
	results, err := c.Search(context.Background(), "golang net/netip package documentation", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("the search returned nothing")
	}
	for _, r := range results {
		t.Logf("%s\n  %s\n  %s", r.Title, r.URL, r.Snippet)
	}
	page, err := c.Fetch(context.Background(), "https://example.com/")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%s (%s): %q", page.Title, page.URL, page.Text)
	if page.Title != "Example Domain" || !strings.Contains(page.Text, "documentation examples") {
		t.Errorf("page = %+v", page)
	}
}

func TestPublicRefusesInternalAddresses(t *testing.T) {
	for _, tc := range []struct {
		addr string
		want bool
	}{
		{"93.184.216.34", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.1.2.3", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // a cloud's metadata address
		{"100.64.0.1", false},
		{"0.0.0.0", false},
		{"fd00::1", false},
		{"fe80::1", false},
		{"::ffff:127.0.0.1", false},
		{"64:ff9b::7f00:1", false},
		{"224.0.0.1", false},
	} {
		if got := public(netip.MustParseAddr(tc.addr)); got != tc.want {
			t.Errorf("public(%s) = %v, want %v", tc.addr, got, tc.want)
		}
	}
}

// The guard runs when the connection is dialled, so a server on this host
// is refused however it is named.
func TestFetchRefusesThisHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprint(w, "secret")
	}))
	defer srv.Close()
	_, err := New().Fetch(context.Background(), srv.URL)
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("fetching %s: %v, want a refusal", srv.URL, err)
	}
}

func TestFetchRefusesWhatIsNotAWebAddress(t *testing.T) {
	c := New(allowInternal())
	for _, address := range []string{"", "file:///etc/passwd", "ftp://example.com/x", "http://", "https://user:pass@example.com/", "gopher://example.com"} {
		if _, err := c.Fetch(context.Background(), address); !errors.Is(err, ErrRefused) {
			t.Errorf("Fetch(%q): %v, want a refusal", address, err)
		}
	}
}

func TestFetchReadsAPageAsText(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/page":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, `<!doctype html><html><head><title> The  Title </title><style>p{color:red}</style></head>
<body><nav>Menu</nav><h1>Heading</h1><p>First   paragraph with a <a href="/x">link</a>.</p>
<script>alert("no")</script><ul><li>One</li><li>Two</li></ul><form><button>Buy</button><p>In a form</p></form></body></html>`)
		case "/moved":
			http.Redirect(w, r, "/page", http.StatusFound)
		case "/json":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"a":1}`)
		case "/binary":
			w.Header().Set("Content-Type", "application/pdf")
			fmt.Fprint(w, "%PDF")
		case "/long":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, strings.Repeat("x", MaxText+10))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := New(allowInternal())
	ctx := context.Background()

	page, err := c.Fetch(ctx, srv.URL+"/moved")
	if err != nil {
		t.Fatal(err)
	}
	if page.Title != "The Title" || page.URL != srv.URL+"/page" {
		t.Errorf("title %q at %q", page.Title, page.URL)
	}
	want := "Menu\nHeading\nFirst paragraph with a link.\nOne\nTwo\nIn a form"
	if page.Text != want {
		t.Errorf("text = %q, want %q", page.Text, want)
	}

	if page, err = c.Fetch(ctx, srv.URL+"/json"); err != nil || page.Text != `{"a":1}` {
		t.Errorf("json: %q, %v", page.Text, err)
	}
	if _, err = c.Fetch(ctx, srv.URL+"/binary"); !errors.Is(err, ErrRefused) {
		t.Errorf("binary: %v, want a refusal", err)
	}
	if _, err = c.Fetch(ctx, srv.URL+"/missing"); err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("missing: %v, want the status", err)
	}
	if page, err = c.Fetch(ctx, srv.URL+"/long"); err != nil || !page.Truncated || len(page.Text) != MaxText {
		t.Errorf("long: %d bytes, truncated %v, %v", len(page.Text), page.Truncated, err)
	}
}

// A redirect is checked like the first address.
func TestFetchRefusesARedirectOffTheWeb(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
	}))
	defer srv.Close()
	if _, err := New(allowInternal()).Fetch(context.Background(), srv.URL); !errors.Is(err, ErrRefused) {
		t.Fatalf("%v, want a refusal", err)
	}
}

const resultsPage = `<html><body><form action="/html/" method="post"></form>
<div class="result results_links"><h2 class="result__title">
<a rel="nofollow" class="result__a" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F&amp;rut=abc">The Go <b>Programming</b> Language</a></h2>
<a class="result__snippet" href="x">Docs for  <b>Go</b>.</a></div>
<div class="result result--ad"><a class="result__a" href="//duckduckgo.com/y.js?ad_provider=x">An ad</a></div>
<div class="result"><a class="result__a" href="https://example.org/direct">Direct</a>
<a class="result__snippet">Second snippet</a></div>
<div class="result"><a class="result__a" href="//duckduckgo.com/l/?uddg=javascript%3Aalert(1)">Bad</a></div>
</body></html>`

func TestSearchReadsResults(t *testing.T) {
	var asked string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		asked = r.Method + " " + r.Form.Get("q")
		switch r.Form.Get("q") {
		case "nothing":
			fmt.Fprint(w, `<html><body><form action="/html/"></form><div class="no-results">No results.</div></body></html>`)
		case "challenge":
			fmt.Fprint(w, `<html><body><div class="anomaly-modal">Select all squares</div></body></html>`)
		default:
			fmt.Fprint(w, resultsPage)
		}
	}))
	defer srv.Close()
	c := New(allowInternal(), WithSearchURL(srv.URL))
	ctx := context.Background()

	got, err := c.Search(ctx, "  go   docs ", 0)
	if err != nil {
		t.Fatal(err)
	}
	if asked != "POST go docs" {
		t.Errorf("asked %q", asked)
	}
	want := []Result{
		{Title: "The Go Programming Language", URL: "https://go.dev/doc/", Snippet: "Docs for Go."},
		{Title: "Direct", URL: "https://example.org/direct", Snippet: "Second snippet"},
	}
	if len(got) != len(want) {
		t.Fatalf("results = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("result %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got, err = c.Search(ctx, "go docs", 1); err != nil || len(got) != 1 {
		t.Errorf("limit 1: %d results, %v", len(got), err)
	}
	if got, err = c.Search(ctx, "nothing", 5); err != nil || len(got) != 0 {
		t.Errorf("no results: %+v, %v", got, err)
	}
	if _, err = c.Search(ctx, "challenge", 5); err == nil {
		t.Error("a challenge page was read as results")
	}
	if _, err = c.Search(ctx, "   ", 5); !errors.Is(err, ErrRefused) {
		t.Errorf("empty query: %v, want a refusal", err)
	}
}
