package snowflake

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func connection(t *testing.T) *Connector {
	t.Helper()
	c, err := New(Config{Account: "acme-test", Database: "ANALYTICS", Warehouse: "READ_WH", Role: "READER", Credential: "env:SNOWFLAKE_TEST_TOKEN", MaxRows: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SNOWFLAKE_TEST_TOKEN", "private-token")
	return c
}
func TestQueriesRejectWritesAndBoundResults(t *testing.T) {
	c := connection(t)
	calls := 0
	c.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Authorization") != "Bearer private-token" {
			t.Fatal("missing owner credential")
		}
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "LIMIT 2") || !strings.Contains(string(b), `"role":"READER"`) {
			t.Fatalf("unbounded request: %s", b)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[["one"],["two"]],"resultSetMetaData":{"numRows":2,"rowType":[{"name":"VALUE"}]}}`))}, nil
	})
	for _, sql := range []string{"DELETE FROM t", "SELECT 1; DELETE FROM t", "SELECT SYSTEM$SEND_EMAIL('x')", "SELECT 1 -- comment", "SELECT 1 /*comment*/"} {
		if _, err := c.Query(context.Background(), sql); err == nil {
			t.Fatalf("accepted %q", sql)
		}
	}
	if calls != 0 {
		t.Fatal("rejected query reached upstream")
	}
	r, err := c.Query(context.Background(), "SELECT 1")
	if err != nil || len(r.Rows) != 1 || !r.Truncated {
		t.Fatalf("result=%+v, err=%v", r, err)
	}
}
func TestCrawlFailsWholeOnPartialResult(t *testing.T) {
	c := connection(t)
	c.client.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[],"resultSetMetaData":{"numRows":2,"partitionInfo":[{},{}]}}`))}, nil
	})
	if rows, err := c.Crawl(context.Background()); err == nil || rows != nil {
		t.Fatalf("partial crawl=%v %v", rows, err)
	}
}
func TestCrawlMapsMetadata(t *testing.T) {
	c := connection(t)
	c.client.Transport = transport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[["PUBLIC","CUSTOMERS","BASE TABLE","Customer records","ID","NUMBER","NO","Customer ID"]],"resultSetMetaData":{"numRows":1}}`))}, nil
	})
	r, err := c.Crawl(context.Background())
	if err != nil || len(r) != 1 || r[0].Path != "ANALYTICS.PUBLIC.CUSTOMERS" || r[0].Schema[0].Description != "Customer ID" {
		t.Fatalf("metadata=%+v err=%v", r, err)
	}
}

type readerKey struct{}

func TestTokenSourceSendsTheReadersToken(t *testing.T) {
	for _, tc := range []struct{ tokenType, role, header string }{
		{"", "", TokenOAuth},
		{TokenProgrammatic, "", TokenProgrammatic},
		{TokenOAuth, "ANALYST", TokenOAuth},
	} {
		c, err := New(Config{Account: "acme-test", Database: "ANALYTICS", Warehouse: "READ_WH", Role: tc.role, TokenType: tc.tokenType, TokenSource: func(ctx context.Context) (string, error) {
			return "token-for-" + ctx.Value(readerKey{}).(string), nil
		}})
		if err != nil {
			t.Fatal(err)
		}
		c.client.Transport = transport(func(r *http.Request) (*http.Response, error) {
			if got := r.Header.Get("Authorization"); got != "Bearer token-for-ana" {
				t.Errorf("authorization = %q", got)
			}
			if got := r.Header.Get("X-Snowflake-Authorization-Token-Type"); got != tc.header {
				t.Errorf("token type = %q, want %q", got, tc.header)
			}
			b, _ := io.ReadAll(r.Body)
			if hasRole := strings.Contains(string(b), `"role"`); hasRole != (tc.role != "") {
				t.Errorf("role %q sent as %s", tc.role, b)
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[["one"]],"resultSetMetaData":{"numRows":1,"rowType":[{"name":"VALUE"}]}}`))}, nil
		})
		ctx := context.WithValue(context.Background(), readerKey{}, "ana")
		if r, err := c.Query(ctx, "SELECT 1"); err != nil || len(r.Rows) != 1 {
			t.Fatalf("result=%+v, err=%v", r, err)
		}
	}
}

func TestTokenSourceFailsClosed(t *testing.T) {
	refused := errors.New("reconnect")
	for _, source := range []func(context.Context) (string, error){
		func(context.Context) (string, error) { return "", refused },
		func(context.Context) (string, error) { return "", nil },
	} {
		c, err := New(Config{Account: "acme-test", Database: "ANALYTICS", Warehouse: "READ_WH", TokenSource: source})
		if err != nil {
			t.Fatal(err)
		}
		c.client.Transport = transport(func(*http.Request) (*http.Response, error) {
			t.Fatal("request sent without a token")
			return nil, nil
		})
		_, err = c.Query(context.Background(), "SELECT 1")
		if err == nil {
			t.Fatal("query ran without a token")
		}
		if _, failed := source(context.Background()); failed != nil && !errors.Is(err, refused) {
			t.Fatalf("token source error lost: %v", err)
		}
	}
}

func TestConfigNeedsExactlyOneToken(t *testing.T) {
	source := func(context.Context) (string, error) { return "t", nil }
	base := Config{Account: "acme-test", Database: "ANALYTICS", Warehouse: "READ_WH", Role: "READER"}
	for name, edit := range map[string]func(*Config){
		"none":                 func(*Config) {},
		"both":                 func(c *Config) { c.Credential, c.TokenSource = "env:X", source },
		"owner without role":   func(c *Config) { c.Credential, c.Role = "env:X", "" },
		"reader with bad role": func(c *Config) { c.TokenSource, c.Role = source, "READER;DROP" },
		"unknown token type":   func(c *Config) { c.TokenSource, c.TokenType = source, "KEYPAIR_JWT" },
		"lowercase token type": func(c *Config) { c.TokenSource, c.TokenType = source, "oauth" },
	} {
		cfg := base
		edit(&cfg)
		if _, err := New(cfg); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
