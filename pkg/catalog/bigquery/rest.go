package bigquery

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"
)

// restBase is where the REST methods live, before the project.
const restBase = "https://bigquery.googleapis.com/bigquery/v2/projects/"

const (
	// maxResponse bounds one REST answer. BigQuery limits a page to 10 MB.
	maxResponse = 12 << 20
	// maxPages bounds how many result pages one query reads.
	maxPages = 20
	// waitMs is how long one wait for a query's completion lasts.
	waitMs = 10_000
)

// jobID is a BigQuery job ID, at most 1024 characters.
var jobID = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// rest runs queries through the REST methods jobs.query, jobs.get and
// jobs.getQueryResults, which the bigquery.readonly OAuth scope
// authorizes. The client library runs them through jobs.insert, which
// needs the full bigquery scope. Listing stays on the embedded api.
//
// A dry run through jobs.query reports the statement type and bytes but no
// tables, so Run checks the tables the finished job read before it fetches
// any row.
type rest struct {
	api
	http     *http.Client
	base     string // restBase and the project
	location string
}

type restTable struct {
	ProjectID string `json:"projectId"`
	DatasetID string `json:"datasetId"`
	TableID   string `json:"tableId"`
}

type restField struct {
	Name   string      `json:"name"`
	Type   string      `json:"type"`
	Mode   string      `json:"mode"`
	Fields []restField `json:"fields"`
}

type restRow struct {
	F []struct {
		V any `json:"v"`
	} `json:"f"`
}

func (r *rest) DryRun(ctx context.Context, sql string) (plan, error) {
	var out struct {
		StatementType       string `json:"statementType"`
		TotalBytesProcessed string `json:"totalBytesProcessed"`
	}
	if err := r.call(ctx, http.MethodPost, "/queries", nil, r.request(sql, map[string]any{"dryRun": true}), &out); err != nil {
		return plan{}, err
	}
	n, err := strconv.ParseInt(out.TotalBytesProcessed, 10, 64)
	if err != nil || out.StatementType == "" {
		return plan{}, errors.New("no statistics from the dry run")
	}
	return plan{StatementType: out.StatementType, Bytes: n}, nil
}

func (r *rest) Run(ctx context.Context, sql string, maxBytes int64, maxRows int, admit func([]string) error) (answer, error) {
	extra := map[string]any{"maximumBytesBilled": strconv.FormatInt(maxBytes, 10), "maxResults": 0, "jobCreationMode": "JOB_CREATION_REQUIRED"}
	if dl, ok := ctx.Deadline(); ok {
		extra["jobTimeoutMs"] = strconv.FormatInt(max(time.Until(dl).Milliseconds(), 1), 10)
	}
	var started struct {
		JobComplete  bool `json:"jobComplete"`
		JobReference struct {
			JobID    string `json:"jobId"`
			Location string `json:"location"`
		} `json:"jobReference"`
	}
	if err := r.call(ctx, http.MethodPost, "/queries", nil, r.request(sql, extra), &started); err != nil {
		return answer{}, err
	}
	ref := started.JobReference
	if len(ref.JobID) > 1024 || !jobID.MatchString(ref.JobID) {
		return answer{}, errors.New("the query returned no job")
	}
	path, at := "/queries/"+ref.JobID, url.Values{}
	if ref.Location == "" {
		ref.Location = r.location
	}
	if ref.Location != "" {
		at.Set("location", ref.Location)
	}
	for done := started.JobComplete; !done; {
		var w struct {
			JobComplete bool `json:"jobComplete"`
		}
		if err := r.call(ctx, http.MethodGet, path, with(at, "maxResults", "0", "timeoutMs", strconv.Itoa(waitMs)), nil, &w); err != nil {
			return answer{}, err
		}
		done = w.JobComplete
	}
	var job struct {
		Status struct {
			State       string `json:"state"`
			ErrorResult *struct {
				Message string `json:"message"`
			} `json:"errorResult"`
		} `json:"status"`
		Statistics struct {
			Query struct {
				ReferencedTables []restTable `json:"referencedTables"`
				TotalBytesBilled string      `json:"totalBytesBilled"`
			} `json:"query"`
		} `json:"statistics"`
	}
	if err := r.call(ctx, http.MethodGet, "/jobs/"+ref.JobID, at, nil, &job); err != nil {
		return answer{}, err
	}
	if job.Status.ErrorResult != nil {
		return answer{}, fmt.Errorf("the query failed: %s", clip(job.Status.ErrorResult.Message))
	}
	if job.Status.State != "DONE" {
		return answer{}, errors.New("the query did not finish")
	}
	tables := make([]string, 0, len(job.Statistics.Query.ReferencedTables))
	for _, t := range job.Statistics.Query.ReferencedTables {
		tables = append(tables, t.ProjectID+"."+t.DatasetID+"."+t.TableID)
	}
	if err := admit(tables); err != nil {
		return answer{}, err
	}
	a := answer{Rows: [][]any{}}
	a.Billed, _ = strconv.ParseInt(job.Statistics.Query.TotalBytesBilled, 10, 64)
	var fields []restField
	token := ""
	for page := 0; ; page++ {
		if page == maxPages {
			a.Truncated = true
			break
		}
		q := with(at, "maxResults", strconv.Itoa(maxRows-len(a.Rows)), "formatOptions.useInt64Timestamp", "true")
		if token != "" {
			q.Set("pageToken", token)
		}
		var res struct {
			JobComplete bool   `json:"jobComplete"`
			PageToken   string `json:"pageToken"`
			Schema      struct {
				Fields []restField `json:"fields"`
			} `json:"schema"`
			Rows []restRow `json:"rows"`
		}
		if err := r.call(ctx, http.MethodGet, path, q, nil, &res); err != nil {
			return answer{}, err
		}
		if !res.JobComplete {
			return answer{}, errors.New("the query did not finish")
		}
		if page == 0 {
			if len(res.Schema.Fields) == 0 {
				return answer{}, errors.New("malformed query results")
			}
			fields = res.Schema.Fields
			for _, f := range fields {
				a.Columns = append(a.Columns, f.Name)
			}
		}
		for _, row := range res.Rows {
			if len(a.Rows) == maxRows {
				a.Truncated = true
				break
			}
			a.Rows = append(a.Rows, record(fields, row))
		}
		if kept, cut := clipRows(a.Rows); cut {
			a.Rows, a.Truncated = kept, true
			break
		}
		token = res.PageToken
		if token == "" {
			break
		}
		if len(a.Rows) == maxRows {
			a.Truncated = true
			break
		}
	}
	return a, nil
}

// request is a jobs.query body in GoogleSQL with Humagen's labels.
func (r *rest) request(sql string, extra map[string]any) map[string]any {
	body := map[string]any{"query": sql, "useLegacySql": false, "labels": labels}
	if r.location != "" {
		body["location"] = r.location
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// call sends one REST request under the project and decodes its answer.
func (r *rest) call(ctx context.Context, method, path string, query url.Values, body, out any) error {
	address := r.base + path
	if len(query) > 0 {
		address += "?" + query.Encode()
	}
	var payload io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		payload = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, address, payload)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := r.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	b, readErr := io.ReadAll(io.LimitReader(res.Body, maxResponse+1))
	_ = res.Body.Close()
	if readErr != nil || len(b) > maxResponse {
		return errors.New("response exceeds limit")
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, clip(e.Error.Message))
	}
	if json.Unmarshal(b, out) != nil {
		return errors.New("malformed response")
	}
	return nil
}

// with copies q and sets the key and value pairs kv.
func with(q url.Values, kv ...string) url.Values {
	out := url.Values{}
	for k, v := range q {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		out.Set(kv[i], kv[i+1])
	}
	return out
}

// clip bounds an upstream message carried in an error.
func clip(s string) string {
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// record turns a REST row into values as the client library gives them.
func record(fields []restField, row restRow) []any {
	out := make([]any, len(fields))
	for i, f := range fields {
		if i < len(row.F) {
			out[i] = cell(f, row.F[i].V)
		}
	}
	return out
}

// cell decodes one REST value by its field: integers, floats, booleans and
// timestamps as typed values, records and arrays element by element, and
// everything else as the text BigQuery sends, as plain does for the client
// library's NUMERIC, DATE, TIME and DATETIME.
func cell(f restField, v any) any {
	if v == nil {
		return nil
	}
	if f.Mode == "REPEATED" {
		items, _ := v.([]any)
		one := f
		one.Mode = ""
		out := make([]any, 0, len(items))
		for _, item := range items {
			m, _ := item.(map[string]any)
			out = append(out, cell(one, m["v"]))
		}
		return out
	}
	if f.Type == "RECORD" || f.Type == "STRUCT" {
		m, _ := v.(map[string]any)
		cells, _ := m["f"].([]any)
		out := make(map[string]any, len(f.Fields))
		for i, sub := range f.Fields {
			if i < len(cells) {
				c, _ := cells[i].(map[string]any)
				out[sub.Name] = cell(sub, c["v"])
			}
		}
		return out
	}
	s, ok := v.(string)
	if !ok {
		return v
	}
	switch f.Type {
	case "INTEGER", "INT64":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return n
		}
	case "FLOAT", "FLOAT64":
		if x, err := strconv.ParseFloat(s, 64); err == nil && !math.IsInf(x, 0) && !math.IsNaN(x) {
			return x
		}
	case "BOOLEAN", "BOOL":
		return s == "true"
	case "TIMESTAMP":
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.UnixMicro(n).UTC()
		}
	}
	return s
}
