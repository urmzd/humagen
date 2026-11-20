package app

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	storeListJSON    = `{"packages":[{"name":"humagen/core","title":"Core","description":"The writer and the analyst.","version":"1.2.0","installed":true,"agents":[{"name":"writer","description":"","installed":true,"customized":true,"update_available":true,"taken":false},{"name":"analyst","description":"","installed":true,"customized":false,"update_available":false,"taken":false}],"missing":[]},{"name":"humagen/review","title":"Review","description":"The code reviewer.","version":"2.0.0","installed":false,"agents":[{"name":"code-reviewer","description":"","installed":false,"customized":false,"update_available":false,"taken":false}],"missing":["code-reviewer"],"extra":"kept"}]}`
	storeInstallJSON = `{"package":{"name":"humagen/review","title":"Review","description":"","version":"2.0.0","installed":true,"agents":[],"missing":[]},"added":["code-reviewer"]}`
	storeRemoveJSON  = `{"package":{"name":"humagen/review","title":"Review","description":"","version":"2.0.0","installed":false,"agents":[],"missing":["code-reviewer"]},"retired":["code-reviewer"],"customized":[]}`
)

// storeAPI plays the store routes of the API. The token "reader" may only
// list, and any package but core and review is unknown.
func storeAPI(t *testing.T, requests *[]string) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*requests = append(*requests, r.Method+" "+r.URL.RequestURI()+" "+r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		problem := func(status int, detail string) {
			w.WriteHeader(status)
			fmt.Fprintf(w, `{"status":%d,"detail":%q}`, status, detail)
		}
		if r.Method != http.MethodGet && r.Header.Get("Authorization") == "Bearer reader" {
			problem(http.StatusForbidden, "agents:write is required")
			return
		}
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /api/store/packages":
			fmt.Fprint(w, storeListJSON)
		case "POST /api/store/packages/review/install", "POST /api/store/packages/humagen%2Freview/install":
			fmt.Fprint(w, storeInstallJSON)
		case "DELETE /api/store/packages/review":
			fmt.Fprint(w, storeRemoveJSON)
		case "DELETE /api/store/packages/core":
			if r.URL.Query().Get("force") != "true" {
				problem(http.StatusConflict, "writer has changes made in the workspace")
				return
			}
			fmt.Fprint(w, storeRemoveJSON)
		default:
			problem(http.StatusNotFound, "no such package")
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func TestStoreAPI(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		token   string
		request string
		output  string
		// fails holds what the error must say; empty is success.
		fails []string
	}{
		{name: "list text", args: []string{"list"}, request: "GET /api/store/packages ", output: "NAME            VERSION  INSTALLED  UPDATE AVAILABLE  MISSING        TITLE\nhumagen/core    1.2.0    yes        writer            -              Core\nhumagen/review  2.0.0    no         -                 code-reviewer  Review\n"},
		{name: "list json", args: []string{"list", "--json"}, token: "secret-token", request: "GET /api/store/packages Bearer secret-token", output: storeListJSON + "\n"},
		{name: "install", args: []string{"install", "review"}, request: "POST /api/store/packages/review/install ", output: "Installed humagen/review 2.0.0, added: code-reviewer\n"},
		{name: "install full name, flag last", args: []string{"install", "humagen/review", "--json"}, request: "POST /api/store/packages/humagen%2Freview/install ", output: storeInstallJSON + "\n"},
		{name: "remove", args: []string{"remove", "review"}, request: "DELETE /api/store/packages/review ", output: "Removed humagen/review, retired: code-reviewer\n"},
		{name: "remove json", args: []string{"remove", "--json", "review"}, request: "DELETE /api/store/packages/review ", output: storeRemoveJSON + "\n"},
		{name: "remove customized", args: []string{"remove", "core"}, request: "DELETE /api/store/packages/core ", fails: []string{"HTTP 409", "writer has changes", "pass --force"}},
		{name: "remove forced", args: []string{"remove", "core", "--force"}, request: "DELETE /api/store/packages/core?force=true ", output: "Removed humagen/review, retired: code-reviewer\n"},
		{name: "forbidden", args: []string{"install", "review"}, token: "reader", request: "POST /api/store/packages/review/install Bearer reader", fails: []string{"HTTP 403", "agents:write", "--token REF or HUMAGEN_TOKEN"}},
		{name: "unknown package", args: []string{"install", "nope"}, request: "POST /api/store/packages/nope/install ", fails: []string{"HTTP 404", "no package named nope", "humagen store list"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := storeAPI(t, &requests)
			// HUMAGEN_URL also verifies that the environment default is used.
			t.Setenv("HUMAGEN_URL", server.URL)
			t.Setenv("HUMAGEN_TOKEN", tt.token)
			var out bytes.Buffer
			err := storeCmd(tt.args, &out)
			if len(requests) != 1 || requests[0] != tt.request {
				t.Errorf("requests = %q; want %q", requests, tt.request)
			}
			if len(tt.fails) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				if out.String() != tt.output {
					t.Errorf("output = %q; want %q", out.String(), tt.output)
				}
				return
			}
			if err == nil {
				t.Fatalf("no error; output %q", out.String())
			}
			for _, want := range tt.fails {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not say %q", err, want)
				}
			}
			if tt.token != "" && strings.Contains(err.Error()+out.String(), tt.token) {
				t.Errorf("the token is printed: %q", err)
			}
			if out.Len() != 0 {
				t.Errorf("output on failure = %q", out.String())
			}
		})
	}
}

func TestStoreRefusedBeforeAnyRequest(t *testing.T) {
	tests := []struct {
		name string
		args []string
		url  string
		want string
	}{
		{name: "no server address", args: []string{"list", "--server", ""}, want: "no server address; set --server URL or HUMAGEN_URL"},
		{name: "server address not a URL", args: []string{"list"}, url: "localhost:8080", want: "--server (or HUMAGEN_URL) must be an http or https URL"},
		{name: "no verb", want: "needs a verb"},
		{name: "unknown verb", args: []string{"upgrade"}, want: "unknown store verb"},
		{name: "update", args: []string{"update", "core"}, want: "no call that takes a package update"},
		{name: "no name", args: []string{"install"}, want: "needs one package name"},
		{name: "two names", args: []string{"remove", "core", "review"}, want: "needs one package name"},
		{name: "list with a name", args: []string{"list", "core"}, want: "unexpected arguments"},
		{name: "force on install", args: []string{"install", "core", "--force"}, want: "flag provided but not defined"},
		{name: "raw token", args: []string{"list", "--token", "raw-secret-value"}, want: "--token must be a credential reference"},
		{name: "token unset", args: []string{"list", "--token", "env:HUMAGEN_STORE_TEST_UNSET"}, want: "resolve --token"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var requests []string
			server := storeAPI(t, &requests)
			t.Setenv("HUMAGEN_URL", or(tt.url, server.URL))
			t.Setenv("HUMAGEN_TOKEN", "")
			var out bytes.Buffer
			err := storeCmd(tt.args, &out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("error = %v; want %q", err, tt.want)
			}
			if err != nil && strings.Contains(err.Error(), "raw-secret-value") {
				t.Errorf("the token is printed: %q", err)
			}
			if len(requests) != 0 {
				t.Errorf("requests = %q; want none", requests)
			}
		})
	}
}

func TestStoreUnreachableServerSaysHowToSetIt(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	t.Setenv("HUMAGEN_TOKEN", "")
	err := storeCmd([]string{"list", "--server", server.URL}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "--server URL or HUMAGEN_URL") {
		t.Errorf("error = %v", err)
	}
}

func TestStoreHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"list", "--help"}, {"remove", "--help"}} {
		var out bytes.Buffer
		if err := storeCmd(args, &out); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%v: error = %v; want flag.ErrHelp", args, err)
		}
		if !strings.Contains(out.String(), "usage: humagen store") {
			t.Errorf("%v: help = %q", args, out.String())
		}
	}
}
