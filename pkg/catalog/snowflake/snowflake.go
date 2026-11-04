// Package snowflake lists warehouse metadata and runs bounded SELECT queries
// through Snowflake's SQL API. Use a role with USAGE and SELECT grants only.
package snowflake

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/urmzd/humagen/pkg/catalog"
	"github.com/urmzd/humagen/pkg/credref"
)

const System = "snowflake"
const MaxResultBytes = 256 << 10

var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,254}$`)
var account = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]{0,200}$`)

// Config uses an OAuth token reference belonging to this connector's owner.
// Role must have read-only grants. No default credential chain is used.
type Config struct {
	Account    string        `json:"account"`
	Database   string        `json:"database"`
	Warehouse  string        `json:"warehouse"`
	Role       string        `json:"role"`
	Credential string        `json:"credential"`
	MaxRows    int           `json:"max_rows,omitempty"`
	Timeout    time.Duration `json:"-"`
}

type Connector struct {
	cfg    Config
	client *http.Client
}

func New(c Config) (*Connector, error) {
	if !account.MatchString(c.Account) || strings.Contains(c.Account, "..") || !identifier.MatchString(c.Database) || !identifier.MatchString(c.Warehouse) || !identifier.MatchString(c.Role) || c.Credential == "" {
		return nil, errors.New("snowflake: account, database, warehouse, read-only role and credential reference are required")
	}
	if c.MaxRows == 0 {
		c.MaxRows = 200
	}
	if c.Timeout == 0 {
		c.Timeout = 60 * time.Second
	}
	if c.MaxRows < 1 || c.MaxRows > 10000 || c.Timeout < time.Second || c.Timeout > 10*time.Minute {
		return nil, errors.New("snowflake: invalid row or timeout limit")
	}
	return &Connector{cfg: c, client: &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

type response struct {
	Data     [][]*string `json:"data"`
	Handle   string      `json:"statementHandle"`
	Metadata struct {
		Rows    int `json:"numRows"`
		Columns []struct {
			Name string `json:"name"`
		} `json:"rowType"`
		Partitions []json.RawMessage `json:"partitionInfo"`
	} `json:"resultSetMetaData"`
}

func (c *Connector) execute(ctx context.Context, sql string, limit int) (catalog.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()
	token, err := credref.Default.Resolve(ctx, c.cfg.Credential)
	if err != nil {
		return catalog.Result{}, err
	}
	body, _ := json.Marshal(map[string]any{"statement": sql, "database": c.cfg.Database, "warehouse": c.cfg.Warehouse, "role": c.cfg.Role, "timeout": int(c.cfg.Timeout.Seconds()), "parameters": map[string]string{"MULTI_STATEMENT_COUNT": "1", "QUERY_TAG": "humagen_catalog"}})
	base := "https://" + c.cfg.Account + ".snowflakecomputing.com/api/v2/statements"
	method, address := http.MethodPost, base
	var result response
	for {
		var payload io.Reader
		if method == http.MethodPost {
			payload = bytes.NewReader(body)
		}
		req, err := http.NewRequestWithContext(ctx, method, address, payload)
		if err != nil {
			return catalog.Result{}, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Snowflake-Authorization-Token-Type", "OAUTH")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		res, err := c.client.Do(req)
		if err != nil {
			return catalog.Result{}, errors.New("snowflake: request failed")
		}
		b, readErr := io.ReadAll(io.LimitReader(res.Body, 8<<20))
		_ = res.Body.Close()
		if readErr != nil || len(b) >= 8<<20 {
			return catalog.Result{}, errors.New("snowflake: response exceeds limit")
		}
		if res.StatusCode != 200 && res.StatusCode != 202 {
			return catalog.Result{}, fmt.Errorf("snowflake: request returned HTTP %d", res.StatusCode)
		}
		result = response{}
		if json.Unmarshal(b, &result) != nil {
			return catalog.Result{}, errors.New("snowflake: malformed response")
		}
		if res.StatusCode == 200 {
			break
		}
		if !regexp.MustCompile(`^[a-zA-Z0-9-]+$`).MatchString(result.Handle) {
			return catalog.Result{}, errors.New("snowflake: invalid statement handle")
		}
		method, address = http.MethodGet, base+"/"+result.Handle
		select {
		case <-ctx.Done():
			return catalog.Result{}, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
	out := catalog.Result{Columns: []string{}, Rows: [][]any{}}
	for _, col := range result.Metadata.Columns {
		out.Columns = append(out.Columns, col.Name)
	}
	size := 0
	for _, row := range result.Data {
		values := make([]any, len(row))
		for i, v := range row {
			if v != nil {
				values[i] = *v
				size += len(*v)
			}
		}
		if len(out.Rows) == limit || size > MaxResultBytes {
			out.Truncated = true
			break
		}
		out.Rows = append(out.Rows, values)
	}
	out.Truncated = out.Truncated || result.Metadata.Rows > len(out.Rows) || len(result.Metadata.Partitions) > 1
	return out, nil
}

// Query accepts only a single SELECT without comments, dollar strings, or
// statement separators. The SELECT-only warehouse role remains authoritative.
func (c *Connector) Query(ctx context.Context, sql string) (catalog.Result, error) {
	sql = strings.TrimSpace(sql)
	if len(sql) < 7 || len(sql) > 20000 || !strings.EqualFold(sql[:7], "SELECT ") || strings.ContainsAny(sql, ";\x00") || strings.Contains(sql, "--") || strings.Contains(sql, "/*") || strings.Contains(sql, "$") {
		return catalog.Result{}, errors.New("snowflake: one SELECT statement is required")
	}
	return c.execute(ctx, "SELECT * FROM ("+sql+") AS humagen_result LIMIT "+strconv.Itoa(c.cfg.MaxRows+1), c.cfg.MaxRows)
}

// Crawl reads schema metadata only, failing whole when it cannot list it all.
func (c *Connector) Crawl(ctx context.Context) ([]catalog.Entry, error) {
	sql := `SELECT c.TABLE_SCHEMA,c.TABLE_NAME,t.TABLE_TYPE,t.COMMENT,c.COLUMN_NAME,c.DATA_TYPE,c.IS_NULLABLE,c.COMMENT FROM INFORMATION_SCHEMA.COLUMNS c JOIN INFORMATION_SCHEMA.TABLES t ON c.TABLE_CATALOG=t.TABLE_CATALOG AND c.TABLE_SCHEMA=t.TABLE_SCHEMA AND c.TABLE_NAME=t.TABLE_NAME WHERE c.TABLE_SCHEMA <> 'INFORMATION_SCHEMA' ORDER BY c.TABLE_SCHEMA,c.TABLE_NAME,c.ORDINAL_POSITION LIMIT 50001`
	r, err := c.execute(ctx, sql, 50000)
	if err != nil {
		return nil, err
	}
	if r.Truncated {
		return nil, errors.New("snowflake: catalog exceeds crawl bounds")
	}
	out := []catalog.Entry{}
	indices := map[string]int{}
	for _, row := range r.Rows {
		if len(row) != 8 {
			return nil, errors.New("snowflake: malformed metadata")
		}
		text := func(i int) string {
			if row[i] == nil {
				return ""
			}
			return fmt.Sprint(row[i])
		}
		path := c.cfg.Database + "." + text(0) + "." + text(1)
		index, ok := indices[path]
		if !ok {
			index = len(out)
			indices[path] = index
			kind := "table"
			if text(2) == "VIEW" {
				kind = "view"
			}
			out = append(out, catalog.Entry{System: System, Kind: kind, Path: path, Title: text(1), Description: text(3)})
		}
		out[index].Schema = append(out[index].Schema, catalog.Column{Name: text(4), Type: text(5), Mode: text(6), Description: text(7)})
		if len(out) > catalog.MaxEntries || len(out[index].Schema) > catalog.MaxColumns {
			return nil, errors.New("snowflake: catalog exceeds crawl bounds")
		}
	}
	return out, nil
}
