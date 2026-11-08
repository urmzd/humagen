package bigquery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// restFake is BigQuery's REST jobs methods for project acme-data.
type restFake struct {
	t         *testing.T
	statement string
	estimate  string
	read      []restTable
	jobError  string

	mu     sync.Mutex
	seen   []string
	bodies []map[string]any
	pages  []url.Values
	waits  int
}

const restSchema = `{"fields":[{"name":"region","type":"STRING"},{"name":"n","type":"INTEGER"},{"name":"at","type":"TIMESTAMP"},{"name":"tags","type":"STRING","mode":"REPEATED"},{"name":"info","type":"RECORD","fields":[{"name":"ok","type":"BOOLEAN"},{"name":"score","type":"FLOAT"}]}]}`

func restRowJSON(region, n string) string {
	return `{"f":[{"v":"` + region + `"},{"v":"` + n + `"},{"v":"1700000000000000"},{"v":[{"v":"a"},{"v":"b"}]},{"v":{"f":[{"v":"true"},{"v":"1.5"}]}}]}`
}

func newRESTFake(t *testing.T) *restFake {
	return &restFake{t: t, statement: "SELECT", estimate: "1000", read: []restTable{{"acme-data", "billing", "invoices"}}}
}

func (f *restFake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer reader-token" {
		f.t.Errorf("%s %s without the reader's token", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	f.seen = append(f.seen, r.Method+" "+r.URL.Path)
	q := r.URL.Query()
	reply := func(s string) { _, _ = w.Write([]byte(s)) }
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/bigquery/v2/projects/acme-data/queries":
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			f.t.Error(err)
		}
		f.bodies = append(f.bodies, body)
		if body["dryRun"] == true {
			reply(`{"statementType":"` + f.statement + `","totalBytesProcessed":"` + f.estimate + `"}`)
			return
		}
		reply(`{"jobReference":{"projectId":"acme-data","jobId":"job_1","location":"US"},"jobComplete":false,"rows":[` + restRowJSON("early", "0") + `]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/bigquery/v2/projects/acme-data/queries/job_1" && q.Get("location") == "US":
		if q.Get("maxResults") == "0" {
			f.waits++
			reply(`{"jobComplete":` + map[bool]string{true: "true", false: "false"}[f.waits > 1] + `}`)
			return
		}
		f.pages = append(f.pages, q)
		if q.Get("pageToken") == "" {
			reply(`{"jobComplete":true,"pageToken":"p2","schema":` + restSchema + `,"rows":[` + restRowJSON("emea", "3") + `,` + restRowJSON("apac", "5") + `]}`)
			return
		}
		reply(`{"jobComplete":true,"schema":` + restSchema + `,"rows":[` + restRowJSON("amer", "7") + `,` + restRowJSON("anz", "9") + `]}`)
	case r.Method == http.MethodGet && r.URL.Path == "/bigquery/v2/projects/acme-data/jobs/job_1" && q.Get("location") == "US":
		tables, _ := json.Marshal(f.read)
		status := `{"state":"DONE"}`
		if f.jobError != "" {
			status = `{"state":"DONE","errorResult":{"reason":"bytesBilledLimitExceeded","message":"` + f.jobError + `"}}`
		}
		reply(`{"status":` + status + `,"statistics":{"query":{"statementType":"SELECT","referencedTables":` + string(tables) + `,"totalBytesBilled":"10485760"}}}`)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

// ran reports whether anything but a dry run was asked of BigQuery.
func (f *restFake) ran() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.seen) > len(f.bodies) || len(f.bodies) > 1
}

// toServer sends a request meant for BigQuery to the test server.
type toServer struct{ url *url.URL }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = s.url.Scheme, s.url.Host
	return http.DefaultTransport.RoundTrip(r)
}

// connectREST is a connector whose queries reach f through the OAuth
// token transport, as dial builds it.
func connectREST(t *testing.T, cfg Config, f *restFake, token func(context.Context) (string, error)) *Connector {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	target, _ := url.Parse(srv.URL)
	hc := &http.Client{Transport: &tokenTransport{source: token, base: toServer{target}}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	c, err := newWith(cfg, func(context.Context, Config) (api, error) {
		return &rest{api: newFake(), http: hc, base: restBase + "acme-data"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func readerToken(context.Context) (string, error) { return "reader-token", nil }

func TestOAuthQueryRunsThroughJobsQuery(t *testing.T) {
	f := newRESTFake(t)
	c := connectREST(t, Config{Project: "acme-data", Datasets: []string{"billing"}, MaxBytesBilled: 1 << 20, MaxRows: 3}, f, readerToken)
	r, err := c.Query(context.Background(), "SELECT * FROM billing.invoices")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.Columns, ",") != "region,n,at,tags,info" || len(r.Rows) != 3 || !r.Truncated || r.EstimatedBytes != 1000 || r.BilledBytes != 10<<20 {
		t.Fatalf("result = %+v", r)
	}
	want := []any{"emea", int64(3), time.UnixMicro(1700000000000000).UTC(), []any{"a", "b"}, map[string]any{"ok": true, "score": 1.5}}
	if !reflect.DeepEqual(r.Rows[0], want) || r.Rows[2][0] != "amer" {
		t.Fatalf("rows = %#v", r.Rows)
	}
	for _, s := range f.seen {
		if strings.HasSuffix(s, "/jobs") {
			t.Fatalf("called jobs.insert: %s", s)
		}
	}
	dry, run := f.bodies[0], f.bodies[1]
	if dry["dryRun"] != true || dry["useLegacySql"] != false || dry["query"] != "SELECT * FROM billing.invoices" {
		t.Fatalf("dry run = %v", dry)
	}
	// The cap the dry run was held to is the one BigQuery enforces, and no
	// row comes back with the run itself.
	if run["dryRun"] != nil || run["maximumBytesBilled"] != "1048576" || run["maxResults"] != float64(0) || run["jobCreationMode"] != "JOB_CREATION_REQUIRED" || run["jobTimeoutMs"] == nil || run["labels"] == nil {
		t.Fatalf("run = %v", run)
	}
	if f.waits != 2 || len(f.pages) != 2 || f.pages[0].Get("maxResults") != "3" || f.pages[1].Get("maxResults") != "1" || f.pages[1].Get("pageToken") != "p2" || f.pages[0].Get("formatOptions.useInt64Timestamp") != "true" {
		t.Fatalf("waits %d, pages %v", f.waits, f.pages)
	}
}

func TestOAuthQueryChecksTablesBeforeReadingRows(t *testing.T) {
	for name, read := range map[string][]restTable{
		"other dataset":            {{"acme-data", "hr", "salaries"}},
		"other project":            {{"public", "billing", "x"}},
		"too many tables to check": make([]restTable, maxListedTables),
	} {
		f := newRESTFake(t)
		f.read = read
		_, err := connectREST(t, Config{Project: "acme-data", Datasets: []string{"billing"}}, f, readerToken).Query(context.Background(), "SELECT 1")
		if !errors.Is(err, ErrOutOfScope) {
			t.Errorf("%s: %v", name, err)
		}
		if len(f.pages) != 0 {
			t.Errorf("%s: read rows", name)
		}
	}
}

func TestOAuthQueryIsRefusedBeforeItRuns(t *testing.T) {
	for name, tc := range map[string]struct {
		statement, estimate string
		want                error
	}{
		"dml":        {"DELETE", "0", ErrNotReadOnly},
		"script":     {"SCRIPT", "0", ErrNotReadOnly},
		"too costly": {"SELECT", "1000", ErrTooCostly},
	} {
		f := newRESTFake(t)
		f.statement, f.estimate = tc.statement, tc.estimate
		_, err := connectREST(t, Config{Project: "acme-data", MaxBytesBilled: 999}, f, readerToken).Query(context.Background(), "SELECT 1")
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
		if f.ran() {
			t.Errorf("%s: the query ran: %v", name, f.seen)
		}
	}
}

func TestOAuthQueryFailsClosed(t *testing.T) {
	f := newRESTFake(t)
	f.jobError = "Query exceeded limit for bytes billed"
	_, err := connectREST(t, Config{Project: "acme-data"}, f, readerToken).Query(context.Background(), "SELECT 1")
	if err == nil || !strings.Contains(err.Error(), f.jobError) || len(f.pages) != 0 {
		t.Fatalf("failed job = %v, %d pages read", err, len(f.pages))
	}
	f = newRESTFake(t)
	_, err = connectREST(t, Config{Project: "acme-data"}, f, func(context.Context) (string, error) { return "", errors.New("private") }).Query(context.Background(), "SELECT 1")
	if err == nil || strings.Contains(err.Error(), "private") || len(f.seen) != 0 {
		t.Fatalf("no token = %v, %v", err, f.seen)
	}
}

func TestOAuthDialsTheRESTQueryPath(t *testing.T) {
	a, err := dial(context.Background(), Config{Project: "acme-data", TokenSource: readerToken})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	r, ok := a.(*rest)
	if !ok || r.base != "https://bigquery.googleapis.com/bigquery/v2/projects/acme-data" {
		t.Fatalf("dialed %T", a)
	}
}
