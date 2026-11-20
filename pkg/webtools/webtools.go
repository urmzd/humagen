// Package webtools reads the public web for an agent: a search, and one
// page as text. It needs no account and no key.
//
// Both go out from the server, so what they may reach is bounded here. A
// request is http or https, to an address on the public internet: a
// loopback, private, link-local or otherwise internal address is refused
// when the connection is dialled, after the name is resolved, so a name
// that resolves inward and a redirect that points inward are refused too.
// No proxy is used, since a proxy would dial for the request and hide where
// it goes. A page is read up to MaxBody and returned up to MaxText.
package webtools

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Bounds on one call.
const (
	// MaxBody is how much of a response is read.
	MaxBody = 2 << 20
	// MaxText is how many bytes of a page's text are returned.
	MaxText = 40_000
	// MaxResults is how many search results are returned at most.
	MaxResults = 10
	// MaxQuery bounds a search's words, and MaxURL an address.
	MaxQuery = 400
	MaxURL   = 2048
	// Timeout is how long one call may take, redirects included.
	Timeout      = 20 * time.Second
	maxRedirects = 5
)

// DefaultSearchURL is the search the client asks: DuckDuckGo's page for
// clients without scripts, which needs no key.
const DefaultSearchURL = "https://html.duckduckgo.com/html/"

// userAgent names the client plainly.
const userAgent = "Mozilla/5.0 (compatible; humagen-webtools/1.0)"

// ErrRefused is a request this package will not make: an address that is
// not the public web's, or a kind of content it cannot return as text.
var ErrRefused = errors.New("webtools: refused")

// Client searches and reads. The zero value is not usable: use New.
type Client struct {
	http      *http.Client
	searchURL string
}

// Option configures a Client.
type Option func(*config)

type config struct {
	searchURL     string
	allowInternal bool
}

// WithSearchURL asks another search page of the same shape.
func WithSearchURL(u string) Option { return func(c *config) { c.searchURL = u } }

// allowInternal lets a test reach a server on this host.
func allowInternal() Option { return func(c *config) { c.allowInternal = true } }

// New returns a client.
func New(opts ...Option) *Client {
	cfg := config{searchURL: DefaultSearchURL}
	for _, o := range opts {
		o(&cfg)
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !cfg.allowInternal {
		dialer.Control = publicOnly
	}
	return &Client{
		searchURL: cfg.searchURL,
		http: &http.Client{
			Timeout: Timeout,
			Transport: &http.Transport{
				Proxy:                 nil,
				DialContext:           dialer.DialContext,
				TLSHandshakeTimeout:   10 * time.Second,
				ResponseHeaderTimeout: 15 * time.Second,
				MaxIdleConns:          8,
				IdleConnTimeout:       30 * time.Second,
			},
			CheckRedirect: func(r *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("%w: more than %d redirects", ErrRefused, maxRedirects)
				}
				return checkURL(r.URL)
			},
		},
	}
}

// publicOnly refuses a connection to an address that is not on the public
// internet. It runs on the address the name resolved to.
func publicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %q is not an address", ErrRefused, address)
	}
	if !public(ap.Addr()) {
		return fmt.Errorf("%w: %s is not an address on the public internet", ErrRefused, ap.Addr())
	}
	return nil
}

// internal are ranges that are not loopback, private or link-local by the
// standard library's account and are still not the public internet.
var internal = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // carrier-grade NAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), // benchmarking
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64, which reaches IPv4
}

func public(a netip.Addr) bool {
	a = a.Unmap()
	if !a.IsValid() || a.IsLoopback() || a.IsPrivate() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() ||
		a.IsMulticast() || a.IsUnspecified() || a.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range internal {
		if p.Contains(a) {
			return false
		}
	}
	return true
}

// checkURL refuses what is not an http or https address with a host.
func checkURL(u *url.URL) error {
	switch {
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("%w: %q is not an http or https address", ErrRefused, u.Redacted())
	case u.Hostname() == "":
		return fmt.Errorf("%w: the address names no host", ErrRefused)
	case u.User != nil:
		return fmt.Errorf("%w: an address may not carry a user or password", ErrRefused)
	}
	return nil
}

// Page is one address, read as text.
type Page struct {
	// URL is where the text came from, after redirects.
	URL   string `json:"url"`
	Title string `json:"title,omitempty"`
	// Type is the content type the server gave.
	Type string `json:"content_type"`
	Text string `json:"text"`
	// Truncated reports that the page had more than was returned.
	Truncated bool `json:"truncated,omitempty"`
}

// Fetch reads one address and returns its text. HTML is reduced to what a
// reader sees; plain text and JSON are returned as they are. Anything else
// is refused.
func (c *Client) Fetch(ctx context.Context, address string) (Page, error) {
	address = strings.TrimSpace(address)
	if address == "" || len(address) > MaxURL {
		return Page{}, fmt.Errorf("%w: give one address of at most %d characters", ErrRefused, MaxURL)
	}
	u, err := url.Parse(address)
	if err != nil {
		return Page{}, fmt.Errorf("%w: %q is not an address", ErrRefused, address)
	}
	if err := checkURL(u); err != nil {
		return Page{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Page{}, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,text/plain,application/json;q=0.9,*/*;q=0.1")
	res, err := c.http.Do(req)
	if err != nil {
		return Page{}, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return Page{}, fmt.Errorf("webtools: %s answered %s", res.Request.URL.Host, res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBody+1))
	if err != nil {
		return Page{}, err
	}
	page := Page{URL: res.Request.URL.String(), Type: res.Header.Get("Content-Type")}
	if len(body) > MaxBody {
		body, page.Truncated = body[:MaxBody], true
	}
	kind, _, _ := mime.ParseMediaType(page.Type)
	switch {
	case kind == "text/html" || kind == "application/xhtml+xml" || kind == "" && looksLikeHTML(body):
		page.Title, page.Text = readable(body)
	case strings.HasPrefix(kind, "text/") || kind == "application/json" || strings.HasSuffix(kind, "+json") || kind == "application/xml" || strings.HasSuffix(kind, "+xml"):
		page.Text = strings.ToValidUTF8(string(body), "")
	default:
		return Page{}, fmt.Errorf("%w: %s is %s, which is not text", ErrRefused, res.Request.URL.Host, cmpOr(kind, "of no stated type"))
	}
	if len(page.Text) > MaxText {
		page.Text, page.Truncated = strings.ToValidUTF8(page.Text[:MaxText], ""), true
	}
	return page, nil
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func looksLikeHTML(b []byte) bool {
	head := strings.ToLower(string(b[:min(len(b), 512)]))
	return strings.Contains(head, "<html") || strings.Contains(head, "<!doctype html")
}

// skipped are elements whose content a reader does not see.
var skipped = map[atom.Atom]bool{
	atom.Script: true, atom.Style: true, atom.Noscript: true, atom.Svg: true, atom.Template: true,
	atom.Iframe: true, atom.Head: true, atom.Select: true, atom.Button: true,
}

// blocks are elements that start a new line.
var blocks = map[atom.Atom]bool{
	atom.P: true, atom.Div: true, atom.Br: true, atom.Li: true, atom.Tr: true, atom.Section: true,
	atom.Article: true, atom.Header: true, atom.Footer: true, atom.H1: true, atom.H2: true, atom.H3: true,
	atom.H4: true, atom.H5: true, atom.H6: true, atom.Pre: true, atom.Blockquote: true, atom.Table: true,
	atom.Ul: true, atom.Ol: true, atom.Dt: true, atom.Dd: true, atom.Main: true, atom.Nav: true, atom.Aside: true,
}

// readable is a page's title and the text a reader sees, one block a line.
func readable(body []byte) (title, text string) {
	doc, err := html.Parse(strings.NewReader(strings.ToValidUTF8(string(body), "")))
	if err != nil {
		return "", ""
	}
	var b strings.Builder
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.DataAtom == atom.Title && title == "" {
				title = squash(inner(n))
			}
			if skipped[n.DataAtom] {
				// The head is skipped but for its title.
				if n.DataAtom == atom.Head {
					for c := n.FirstChild; c != nil; c = c.NextSibling {
						if c.Type == html.ElementNode && c.DataAtom == atom.Title && title == "" {
							title = squash(inner(c))
						}
					}
				}
				return
			}
			if blocks[n.DataAtom] {
				b.WriteByte('\n')
			}
		}
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
		if n.Type == html.ElementNode && blocks[n.DataAtom] {
			b.WriteByte('\n')
		}
	}
	walk(doc)
	var lines []string
	for _, line := range strings.Split(b.String(), "\n") {
		if line = squash(line); line != "" {
			lines = append(lines, line)
		}
	}
	return title, strings.Join(lines, "\n")
}

// inner is the text inside a node.
func inner(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// squash is s with each run of white space as one space.
func squash(s string) string { return strings.Join(strings.Fields(s), " ") }

// Result is one search result.
type Result struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet,omitempty"`
}

// Search asks the search page for query and returns at most limit results,
// MaxResults when limit is not between 1 and MaxResults. No results is an
// empty list, not an error; a page that is not a list of results, such as a
// challenge to prove the client is a person, is an error.
func (c *Client) Search(ctx context.Context, query string, limit int) ([]Result, error) {
	query = squash(query)
	if query == "" || len(query) > MaxQuery {
		return nil, fmt.Errorf("%w: give words to search for, at most %d characters", ErrRefused, MaxQuery)
	}
	if limit < 1 || limit > MaxResults {
		limit = MaxResults
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.searchURL, strings.NewReader(url.Values{"q": {query}}.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, MaxBody))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("webtools: the search answered %s", res.Status)
	}
	results, listed := parseResults(body)
	if !listed {
		return nil, errors.New("webtools: the search did not answer with results; it may be asking the client to prove it is a person. Try again later")
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// parseResults reads the search page. listed reports that the page is a
// list of results, which an empty one also is.
func parseResults(body []byte) (out []Result, listed bool) {
	doc, err := html.Parse(strings.NewReader(strings.ToValidUTF8(string(body), "")))
	if err != nil {
		return nil, false
	}
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			class := attr(n, "class")
			switch {
			case hasClass(class, "no-results"), n.DataAtom == atom.Form && strings.Contains(attr(n, "action"), "/html"):
				listed = true
			case n.DataAtom == atom.A && hasClass(class, "result__a"):
				listed = true
				if target := resultURL(attr(n, "href")); target != "" {
					out = append(out, Result{Title: squash(inner(n)), URL: target})
				}
			case hasClass(class, "result__snippet") && len(out) > 0 && out[len(out)-1].Snippet == "":
				out[len(out)-1].Snippet = squash(inner(n))
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return out, listed
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(class, want string) bool {
	for _, c := range strings.Fields(class) {
		if c == want {
			return true
		}
	}
	return false
}

// resultURL is the address a result leads to. The page links through a
// redirect of its own, which carries the address in uddg; an advertisement
// is left out.
func resultURL(href string) string {
	if strings.HasPrefix(href, "//") {
		href = "https:" + href
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if strings.HasSuffix(u.Hostname(), "duckduckgo.com") {
		if strings.Contains(u.Path, "y.js") {
			return ""
		}
		target := u.Query().Get("uddg")
		if target == "" {
			return ""
		}
		if u, err = url.Parse(target); err != nil {
			return ""
		}
	}
	if checkURL(u) != nil {
		return ""
	}
	return u.String()
}
