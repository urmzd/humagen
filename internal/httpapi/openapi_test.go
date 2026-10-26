package httpapi

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	gotypesconst "go/constant"
	"go/importer"
	"go/parser"
	gotoken "go/token"
	"go/types"
	"io"
	"net/http"
	"os"
	"os/exec"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/urmzd/fiber/pkg/agentrt"
	"github.com/urmzd/fiber/pkg/agentrt/payload"
	"github.com/urmzd/fiber/pkg/agentspec"
	"github.com/urmzd/fiber/pkg/connectors/cron"
	"github.com/urmzd/fiber/pkg/envelope"
	"github.com/urmzd/fiber/pkg/runtime"
	"github.com/urmzd/fiber/pkg/toolprovider"
	"github.com/urmzd/saige/agent/agenttest"
	saigetypes "github.com/urmzd/saige/agent/types"
	"go.yaml.in/yaml/v4"

	"github.com/urmzd/fiber/pkg/attachment"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/artifacts"
	"github.com/urmzd/humagen/pkg/audit"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/clients"
	"github.com/urmzd/humagen/pkg/credentials"
	"github.com/urmzd/humagen/pkg/evals"
	"github.com/urmzd/humagen/pkg/inbox"
	"github.com/urmzd/humagen/pkg/journal"
	"github.com/urmzd/humagen/pkg/knowledge"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/notifications"
	"github.com/urmzd/humagen/pkg/pick"
	"github.com/urmzd/humagen/pkg/policy"
	"github.com/urmzd/humagen/pkg/prefs"
	"github.com/urmzd/humagen/pkg/projects"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/humagen/pkg/usage"
)

// The API contract is api/openapi.yaml. These tests are what keep it true:
// the routes it lists are the routes the server registers, each with the
// permission the server checks, its schemas have the fields the Go types
// marshal, and real responses validate against it.

const specPath = "../../api/openapi.yaml"

// Permission words the spec uses for routes that check no named permission.
const (
	permPublic        = "public"
	permAuthenticated = "authenticated"
)

type specOp struct {
	method, path string
	raw          map[string]any
	// schemas is the compiled body schema per documented status code. A
	// code with no JSON body maps to nil.
	schemas map[string]*jsonschema.Schema
}

func (o specOp) key() string { return o.method + " " + o.path }

type apiSpec struct {
	doc map[string]any
	c   *jsonschema.Compiler
	ops map[string]specOp
}

var httpMethods = []string{"get", "put", "post", "delete", "patch", "head", "options"}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

// loadSpec reads the contract and compiles every response body schema in it.
func loadSpec(t *testing.T) *apiSpec {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatal(err)
	}
	var viaYAML any
	if err := yaml.Unmarshal(raw, &viaYAML); err != nil {
		t.Fatalf("the spec is not YAML: %v", err)
	}
	// Through JSON, so every value is one the schema compiler expects.
	asJSON, err := json.Marshal(viaYAML)
	if err != nil {
		t.Fatalf("the spec does not survive JSON: %v", err)
	}
	parsed, err := jsonschema.UnmarshalJSON(bytes.NewReader(asJSON))
	if err != nil {
		t.Fatal(err)
	}
	s := &apiSpec{doc: obj(parsed), ops: map[string]specOp{}}
	closeObjects(s.doc)

	// Response schemas are copied under one flat key each, so they compile
	// by a pointer with no path template in it. Their $refs still resolve:
	// they point into this same document.
	hoisted := map[string]any{}
	s.doc["x-compiled"] = hoisted
	type pending struct {
		op   string
		code string
		at   string
	}
	var todo []pending
	for path, item := range obj(s.doc["paths"]) {
		for _, m := range httpMethods {
			raw := obj(obj(item)[m])
			if raw == nil {
				continue
			}
			op := specOp{method: strings.ToUpper(m), path: path, raw: raw, schemas: map[string]*jsonschema.Schema{}}
			for code, res := range obj(raw["responses"]) {
				op.schemas[code] = nil
				if ref, isRef := obj(res)["$ref"].(string); isRef {
					res = obj(obj(obj(s.doc["components"])["responses"])[strings.TrimPrefix(ref, "#/components/responses/")])
				}
				schema, has := obj(obj(obj(res)["content"])["application/json"])["schema"]
				if !has {
					continue
				}
				at := "s" + strconv.Itoa(len(hoisted))
				hoisted[at] = schema
				todo = append(todo, pending{op.key(), code, at})
			}
			s.ops[op.key()] = op
		}
	}
	s.c = jsonschema.NewCompiler()
	s.c.AssertFormat()
	s.c.AssertContent()
	if err := s.c.AddResource("openapi.json", s.doc); err != nil {
		t.Fatal(err)
	}
	for _, p := range todo {
		compiled, err := s.c.Compile("openapi.json#/x-compiled/" + p.at)
		if err != nil {
			t.Fatalf("%s %s: the response schema does not compile: %v", p.op, p.code, err)
		}
		s.ops[p.op].schemas[p.code] = compiled
	}
	return s
}

// closeObjects makes every object schema in the test's copy of the spec
// refuse properties it does not name, so a key added to a handler's map or
// inline struct fails validation instead of reaching clients unnoticed. Only
// this copy changes: the published spec stays open, as JSON Schema is by
// default, so a client keeps accepting fields added later.
//
// A schema used as a member of an allOf is left open, because it sees only
// its own properties; the schema that composes it is closed instead, and
// unevaluatedProperties there counts what every member evaluated.
func closeObjects(doc map[string]any) {
	members := map[string]bool{}
	var collect func(v any)
	collect = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			for _, part := range sliceOf(x["allOf"]) {
				if ref, isRef := obj(part)["$ref"].(string); isRef {
					members[ref] = true
				}
			}
			for _, child := range x {
				collect(child)
			}
		case []any:
			for _, child := range x {
				collect(child)
			}
		}
	}
	collect(doc)
	var walk func(v any, member bool)
	walk = func(v any, member bool) {
		switch x := v.(type) {
		case map[string]any:
			_, hasProps := x["properties"]
			_, hasExtra := x["additionalProperties"]
			_, composes := x["allOf"]
			if !member && !hasExtra && (hasProps || composes) {
				x["unevaluatedProperties"] = false
			}
			for key, child := range x {
				walk(child, key == "allOf")
			}
		case []any:
			for _, child := range x {
				// The elements of an allOf are its members.
				walk(child, member)
			}
		}
	}
	for name, schema := range obj(obj(doc["components"])["schemas"]) {
		walk(schema, members["#/components/schemas/"+name])
	}
	walk(doc["paths"], false)
}

func (s *apiSpec) component(t *testing.T, name string) *jsonschema.Schema {
	t.Helper()
	compiled, err := s.c.Compile("openapi.json#/components/schemas/" + name)
	if err != nil {
		t.Fatalf("schema %s does not compile: %v", name, err)
	}
	return compiled
}

type route struct{ method, path, permission string }

// registeredRoutes reads the route table out of this package's source, with
// types: every call of (*http.ServeMux).Handle or HandleFunc, whatever the
// receiver is called, with the permission its (*Server).authed is given. It
// fails on anything it cannot resolve to a constant (a pattern or permission
// computed at run time, a method value taken from a mux, a route on
// http.DefaultServeMux, an authed handler not passed straight to a mux), so a
// route cannot slip past the contract by being written differently.
func registeredRoutes(t *testing.T) []route {
	t.Helper()
	fset := gotoken.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0) //nolint:staticcheck // one directory of syntax is all this needs
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, pkg := range pkgs {
		for _, f := range pkg.Files {
			files = append(files, f)
		}
	}
	info := &types.Info{Types: map[ast.Expr]types.TypeAndValue{}, Uses: map[*ast.Ident]types.Object{}, Selections: map[*ast.SelectorExpr]*types.Selection{}}
	conf := types.Config{Importer: exportImporter(t, fset)}
	if _, err := conf.Check("github.com/urmzd/humagen/internal/httpapi", fset, files, info); err != nil {
		t.Fatalf("type-checking the package: %v", err)
	}

	isFunc := func(obj types.Object, names ...string) bool {
		fn, ok := obj.(*types.Func)
		return ok && slices.Contains(names, fn.FullName())
	}
	register := []string{"(*net/http.ServeMux).Handle", "(*net/http.ServeMux).HandleFunc", "net/http.Handle", "net/http.HandleFunc"}
	authed := "(*github.com/urmzd/humagen/internal/httpapi.Server).authed"
	constant := func(e ast.Expr) (string, bool) {
		tv := info.Types[e]
		if tv.Value == nil || tv.Value.Kind() != gotypesconst.String {
			return "", false
		}
		return gotypesconst.StringVal(tv.Value), true
	}

	// Every place the registering functions or authed are named, and which
	// of those are the callee of a call this reader understood.
	named := map[*ast.Ident]string{}
	for id, obj := range info.Uses {
		switch {
		case isFunc(obj, register...):
			named[id] = "a route registration"
		case isFunc(obj, authed):
			named[id] = "a use of authed"
		}
	}
	understood := map[*ast.Ident]bool{}
	callee := func(call *ast.CallExpr) *ast.Ident {
		switch fn := call.Fun.(type) {
		case *ast.SelectorExpr:
			return fn.Sel
		case *ast.Ident:
			return fn
		}
		return nil
	}

	var out []route
	for _, file := range files {
		ast.Inspect(file, func(n ast.Node) bool {
			call, isCall := n.(*ast.CallExpr)
			if !isCall {
				return true
			}
			id := callee(call)
			if id == nil || !isFunc(info.Uses[id], register...) {
				return true
			}
			where := fset.Position(call.Pos()).String()
			if isFunc(info.Uses[id], "net/http.Handle", "net/http.HandleFunc") {
				t.Fatalf("%s: a route on http.DefaultServeMux is not served by Server.Handler", where)
			}
			understood[id] = true
			pattern, ok := constant(call.Args[0])
			if !ok {
				t.Fatalf("%s: a route whose pattern is not a constant cannot be checked against the spec", where)
			}
			method, path, hasMethod := strings.Cut(pattern, " ")
			if !hasMethod {
				// "/api/" and "/" are fallbacks, not operations.
				return true
			}
			r := route{method: method, path: path, permission: permPublic}
			if inner, isCall := call.Args[1].(*ast.CallExpr); isCall {
				if aid := callee(inner); aid != nil && isFunc(info.Uses[aid], authed) {
					understood[aid] = true
					perm, ok := constant(inner.Args[0])
					if !ok {
						t.Fatalf("%s: the permission this route checks is not a constant", where)
					}
					r.permission = permAuthenticated
					if perm != "" {
						r.permission = perm
					}
				}
			}
			out = append(out, r)
			return true
		})
	}
	for id, what := range named {
		if !understood[id] {
			t.Errorf("%s: %s the route reader cannot follow. Register routes as mux.HandleFunc(\"METHOD /path\", s.authed(perm, h))", fset.Position(id.Pos()), what)
		}
	}
	if len(out) < 50 {
		t.Fatalf("read only %d routes from the source, so the reader is broken", len(out))
	}
	return out
}

// exportImporter imports packages from the compiler's export data, which
// `go list -export` builds or finds in the build cache. It is the standard
// library's own importer pointed at the module's dependencies, so reading the
// route table with types needs no dependency beyond the go command.
func exportImporter(t *testing.T, fset *gotoken.FileSet) types.Importer {
	t.Helper()
	cmd := exec.Command("go", "list", "-export", "-deps", "-f", "{{.ImportPath}}={{.Export}}", ".")
	// What the go command said is the only account of why it failed.
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -export: %v\n%s", err, stderr.Bytes())
	}
	exports := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if path, file, ok := strings.Cut(line, "="); ok && file != "" {
			exports[path] = file
		}
	}
	return importer.ForCompiler(fset, "gc", func(path string) (io.ReadCloser, error) {
		file, ok := exports[path]
		if !ok {
			return nil, fmt.Errorf("no export data for %s", path)
		}
		return os.Open(file)
	})
}

// isIngress reports a route that Slack and GitHub call, which the client
// contract leaves out on purpose.
func isIngress(path string) bool { return strings.HasPrefix(path, "/connectors/") }

func TestSpecListsExactlyTheRegisteredRoutes(t *testing.T) {
	s := loadSpec(t)
	registered := map[string]route{}
	for _, r := range registeredRoutes(t) {
		if isIngress(r.path) {
			if _, documented := s.ops[r.method+" "+r.path]; documented {
				t.Errorf("%s %s is webhook ingress and does not belong in the client contract", r.method, r.path)
			}
			continue
		}
		registered[r.method+" "+r.path] = r
	}
	for key := range registered {
		if _, documented := s.ops[key]; !documented {
			t.Errorf("%s is registered and missing from %s", key, specPath)
		}
	}

	declared := map[string]bool{}
	for _, tag := range s.doc["tags"].([]any) {
		declared[obj(tag)["name"].(string)] = true
	}
	ids := map[string]string{}
	for key, op := range s.ops {
		r, exists := registered[key]
		if !exists {
			t.Errorf("%s is in the spec and the server registers no such route", key)
			continue
		}
		id, _ := op.raw["operationId"].(string)
		if id == "" {
			t.Errorf("%s has no operationId", key)
		}
		if other, taken := ids[id]; taken {
			t.Errorf("%s and %s share the operationId %q", key, other, id)
		}
		ids[id] = key
		tags, _ := op.raw["tags"].([]any)
		if len(tags) == 0 {
			t.Errorf("%s has no tag", key)
		}
		for _, tag := range tags {
			if !declared[tag.(string)] {
				t.Errorf("%s uses tag %q, which the spec does not declare", key, tag)
			}
		}
		if got, _ := op.raw["x-permission"].(string); got != r.permission {
			t.Errorf("%s: x-permission is %q, the route checks %q", key, got, r.permission)
		}
		security, overrides := op.raw["security"].([]any)
		if open := overrides && len(security) == 0; open != (r.permission == permPublic) {
			t.Errorf("%s: `security: []` must be set on public routes and only on them", key)
		}
		// oag v0.20.6 reads a query parameter whose schema is a $ref as an
		// object and writes it one character at a time, so the client would
		// send nonsense. Query enums are written inline until that is fixed.
		for _, p := range sliceOf(op.raw["parameters"]) {
			if _, isRef := obj(obj(p)["schema"])["$ref"]; isRef && obj(p)["in"] == "query" {
				t.Errorf("%s: query parameter %q has a $ref schema, which the client generator mishandles", key, obj(p)["name"])
			}
		}
		for _, name := range pathParams(op.path) {
			if !declaresPathParam(s, op, name) {
				t.Errorf("%s does not declare its path parameter %q", key, name)
			}
		}
	}

	var enum []string
	for _, v := range obj(obj(obj(s.doc["components"])["schemas"])["Permission"])["enum"].([]any) {
		enum = append(enum, v.(string))
	}
	var real []string
	for _, p := range auth.Permissions() {
		real = append(real, string(p))
	}
	sort.Strings(enum)
	sort.Strings(real)
	if !slices.Equal(enum, real) {
		t.Errorf("the Permission enum is %v, the server's permissions are %v", enum, real)
	}
}

// refusesUnknownFields reports an operation that takes a body and documents
// 400, so a body with a field it does not know must be refused.
func refusesUnknownFields(op specOp) bool {
	_, documents := op.schemas["400"]
	return documents && obj(op.raw["requestBody"]) != nil && !strings.Contains(op.path, "photo")
}

func pathParams(path string) []string {
	var out []string
	for _, seg := range strings.Split(path, "/") {
		if strings.HasPrefix(seg, "{") {
			out = append(out, strings.Trim(seg, "{}"))
		}
	}
	return out
}

func declaresPathParam(s *apiSpec, op specOp, name string) bool {
	params, _ := op.raw["parameters"].([]any)
	for _, p := range params {
		param := obj(p)
		if ref, isRef := param["$ref"].(string); isRef {
			param = obj(obj(obj(s.doc["components"])["parameters"])[strings.TrimPrefix(ref, "#/components/parameters/")])
		}
		if param["in"] == "path" && param["name"] == name && param["required"] == true {
			return true
		}
	}
	return false
}

// The source says which permission a route names. This asks the running
// server, operation by operation, which permission it actually enforces.
func TestSpecPermissionsAreTheOnesTheServerEnforces(t *testing.T) {
	s := loadSpec(t)
	f := newFixture(t)
	nothing := f.machine(t, "acme")
	for key, op := range s.ops {
		perm, _ := op.raw["x-permission"].(string)
		if perm == permPublic {
			continue
		}
		path := op.path
		for _, name := range pathParams(path) {
			path = strings.ReplaceAll(path, "{"+name+"}", "x")
		}
		if res, _ := f.do(t, nil, op.method, path, "", ""); res.StatusCode != http.StatusUnauthorized {
			t.Errorf("%s without a credential = %d, want 401", key, res.StatusCode)
		}
		res, out := f.do(t, nil, op.method, path, nothing, "{}")
		if perm == permAuthenticated {
			if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusUnauthorized {
				t.Errorf("%s is documented as open to any signed-in caller and answered %d", key, res.StatusCode)
			}
			continue
		}
		if want := "you need " + perm; res.StatusCode != http.StatusForbidden || out["error"] != want {
			t.Errorf("%s with no permissions = %d %q, want 403 %q", key, res.StatusCode, out["error"], want)
		}
	}
}

// jsonField is one field as encoding/json writes it.
type jsonField struct {
	name     string
	optional bool
}

// jsonFields lists a struct's JSON fields, flattening embedded structs the way
// encoding/json does.
func jsonFields(t reflect.Type) []jsonField {
	var out []jsonField
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if f.Anonymous && tag == "" {
			inner := f.Type
			if inner.Kind() == reflect.Pointer {
				inner = inner.Elem()
			}
			out = append(out, jsonFields(inner)...)
			continue
		}
		if !f.IsExported() || tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if name == "" {
			name = f.Name
		}
		out = append(out, jsonField{name: name, optional: strings.Contains(opts, "omitempty") || strings.Contains(opts, "omitzero")})
	}
	return out
}

// schemaFields flattens a component schema's properties and required list,
// following allOf and local $refs.
func schemaFields(s *apiSpec, schema map[string]any, props map[string]bool, required map[string]bool) {
	if ref, isRef := schema["$ref"].(string); isRef {
		schemaFields(s, obj(obj(obj(s.doc["components"])["schemas"])[strings.TrimPrefix(ref, "#/components/schemas/")]), props, required)
	}
	for _, part := range sliceOf(schema["allOf"]) {
		schemaFields(s, obj(part), props, required)
	}
	for name := range obj(schema["properties"]) {
		props[name] = true
	}
	for _, name := range sliceOf(schema["required"]) {
		required[name.(string)] = true
	}
}

func sliceOf(v any) []any { s, _ := v.([]any); return s }

// A schema names the fields its Go type marshals, and requires exactly the
// ones the type always writes. This is what catches a field added to a struct
// and not to the contract.
func TestSpecSchemasHaveTheFieldsTheGoTypesMarshal(t *testing.T) {
	s := loadSpec(t)
	// written says the schema is something a caller sends: the server fills
	// in what is left out, so only the field names are compared.
	type shape struct {
		typ     any
		written bool
	}
	shapes := map[string]shape{
		"Principal": {typ: auth.Principal{}}, "AccessDecision": {typ: accessDecision{}}, "AccessHeldRole": {typ: heldRole{}}, "AgentSpec": {typ: agentspec.AgentSpec{}}, "AgentSource": {typ: agentspec.Source{}},
		"AgentAccess": {typ: agentspec.Access{}}, "Revision": {typ: revisionView{}}, "Choice": {typ: pick.Choice{}},
		"Note": {typ: noteView{}}, "LibraryScope": {typ: library.Scope{}}, "LibrarySkillDefinition": {typ: library.Definition{}}, "LibrarySkill": {typ: library.Revision{}}, "LibraryProvenance": {typ: library.Provenance{}}, "AuditRecord": {typ: audit.Record{}}, "PersonalPreferences": {typ: prefs.Personal{}}, "NativeNotification": {typ: platform.Notification{}}, "NativeNotificationItem": {typ: platform.NotificationItem{}}, "Actor": {typ: envelope.Actor{}}, "Run": {typ: runs.Run{}},
		"ThreadSummary": {typ: platform.ThreadSummary{}},
		"Thread":        {typ: threads.Thread{}}, "ThreadParticipant": {typ: threads.Participant{}}, "ThreadMention": {typ: threads.Mention{}},
		"ThreadAnswerRef": {typ: threads.AnswerRef{}}, "ThreadMessage": {typ: threads.Message{}}, "ThreadTurn": {typ: platform.ThreadTurn{}}, "ThreadAnnouncement": {typ: threads.Announcement{}}, "ThreadAsk": {typ: platform.Ask{}},
		"ThreadAnswers": {typ: answers{}}, "ThreadPosted": {typ: platform.Posted{}}, "ThreadNotice": {typ: threads.Notice{}},
		"ThreadInput": {typ: platform.ThreadInput{}, written: true}, "ThreadMessageInput": {typ: platform.MessageInput{}, written: true},
		"ThreadUpdate": {typ: platform.ThreadUpdate{}, written: true}, "ThreadParticipantsUpdate": {typ: platform.ParticipantsUpdate{}, written: true},
		"ThreadShare": {typ: platform.ThreadShare{}, written: true}, "ThreadNotAdded": {typ: platform.NotAdded{}},
		"Channel": {typ: projects.Channel{}}, "ClientBinding": {typ: projects.Binding{}}, "ChannelView": {typ: channelView{}},
		"ChannelInput": {typ: platform.ChannelInput{}, written: true}, "ChannelUpdate": {typ: platform.ChannelUpdate{}, written: true},
		"ProjectExpert": {typ: projects.Expert{}}, "ProjectExperts": {typ: platform.Experts{}}, "Project": {typ: projects.Project{}}, "ProjectView": {typ: projectView{}},
		"ProjectAccount": {typ: projects.AccountInfo{}}, "ProjectStep": {typ: pathStep{}}, "ProjectLimit": {typ: projectLimit{}},
		"ProjectDetail": {typ: projectDetail{}}, "ProjectChange": {typ: projectChangeView{}},
		"ProjectContext": {typ: platform.ProjectContext{}}, "ContextLayer": {typ: platform.ContextLayer{}},
		"ProjectKnowledge": {typ: platform.ProjectKnowledge{}}, "ProjectKnowledgeSource": {typ: projects.KnowledgeSource{}}, "ProjectKnowledgeInput": {typ: platform.KnowledgeInput{}, written: true},
		"ProjectSkill": {typ: platform.SkillDetail{}}, "ProjectSkillRef": {typ: projects.SkillRef{}}, "ProjectSkills": {typ: platform.ProjectSkills{}}, "ProjectSkillsInput": {typ: platform.SkillsInput{}, written: true},
		"MentionKind": {typ: platform.MentionKind{}}, "MentionItem": {typ: platform.MentionItem{}},
		"ProjectInput": {typ: platform.ProjectInput{}, written: true}, "ProjectUpdate": {typ: platform.ProjectUpdate{}, written: true},
		"ProjectAgents": {typ: projects.Agents{}}, "AgentAvailability": {typ: projects.Availability{}}, "AgentOffer": {typ: platform.Offer{}},
		"DefaultResolved": {typ: platform.Nearer{}}, "ProjectAgentsView": {typ: platform.ProjectAgents{}},
		"ProjectAgentsInput": {typ: platform.AgentsInput{}, written: true}, "AgentPlace": {typ: platform.AgentPlace{}}, "AgentHome": {typ: agentHome{}},
		"Person":        {typ: personView{}},
		"PersonProject": {typ: personProject{}},
		"ProjectEntry":  {typ: projects.Entry{}}, "ProjectKindSpec": {typ: projects.KindSpec{}}, "ProjectAccess": {typ: projects.Access{}},
		"ProjectInheritedOwner": {typ: inheritedOwner{}}, "ProjectMember": {typ: memberView{}}, "ProjectMembers": {typ: projectMembersView{}},
		"ProjectLimitInput": {typ: projectLimitBody{}, written: true}, "InboxCounts": {typ: inbox.Counts{}},
		"AnnouncementInput": {typ: struct {
			Subject  string `json:"subject"`
			Text     string `json:"text"`
			Severity string `json:"severity,omitempty"`
		}{}, written: true},
		"Settings": {typ: platform.Settings{}}, "Preferences": {typ: prefs.Preferences{}},
		"ToolCredentials": {typ: platform.ToolCredentials{}}, "ToolCredential": {typ: platform.ToolCredential{}}, "ToolCredentialInput": {typ: credentials.Input{}, written: true},
		"RunFeedback": {typ: shownFeedback{}}, "Attachment": {typ: attachment.Attachment{}},
		"Schedule": {typ: schedule{}}, "ScheduleView": {typ: scheduleView{}},
		"InboxNotice": {typ: inboxNotice{}}, "InboxFrom": {typ: inbox.From{}}, "InboxSource": {typ: inbox.Source{}},
		"InboxOutcome": {typ: inbox.Outcome{}}, "InboxLimits": {typ: inbox.Limits{}}, "InboxAlertPart": {typ: inbox.AlertPart{}},
		"InboxFailurePart": {typ: inbox.FailurePart{}}, "InboxNoteRef": {typ: inbox.NoteRef{}}, "InboxMemoryPart": {typ: inbox.MemoryPart{}},
		"InboxBudgetPart": {typ: inbox.BudgetPart{}}, "InboxPart": {typ: inbox.Part{}}, "InboxQuestionPart": {typ: inbox.QuestionPart{}}, "InboxQuestionTool": {typ: inbox.QuestionTool{}}, "InboxMessage": {typ: inbox.Message{}}, "InboxMentionPart": {typ: inbox.MentionPart{}},
		"InboxChange": {typ: inbox.Change{}},
		"CallPolicy":  {typ: callPolicyView{}}, "CallPolicyInput": {typ: callPolicyBody{}, written: true}, "Reach": {typ: platform.Reach{}},
		"Artifact": {typ: artifacts.Artifact{}}, "StorageShare": {typ: artifacts.Share{}},
		"InboxDraftPart": {typ: inbox.DraftPart{}}, "InboxDelivery": {typ: inbox.Delivery{}}, "InboxDeliveryInput": {typ: deliveryBody{}, written: true}, "ClientHome": {typ: clients.Home{}},
		"AnalyticsGroup": {typ: runs.Group{}}, "AnalyticsBucket": {typ: runs.Bucket{}}, "Summary": {typ: runs.Summary{}},
		"JournalEntry": {typ: journal.Entry{}}, "Span": {typ: journal.Span{}},
		"AlertRule": {typ: ruleView{}}, "Alert": {typ: alertView{}},
		"EvalCheck": {typ: evals.Check{}}, "EvalCase": {typ: evals.Case{}}, "EvalSuite": {typ: evals.Suite{}, written: true},
		"EvalVerdict": {typ: evals.Verdict{}}, "EvalHit": {typ: evals.Hit{}}, "EvalResult": {typ: evals.Result{}},
		"EvalRun": {typ: evals.Run{}}, "EvalSuiteView": {typ: suiteView{}, written: true},
		"KnowledgeVariant": {typ: knowledge.Variant{}}, "KnowledgeSource": {typ: knowledge.Source{}},
		"KnowledgeDocument": {typ: knowledge.Document{}}, "KnowledgeHit": {typ: knowledge.Hit{}},
		"PolicyScope": {typ: policy.Scope{}}, "Policy": {typ: policyView{}}, "PolicyChange": {typ: policyChangeView{}},
		"Measure": {typ: measure{}}, "ScopeUsage": {typ: scopeUsage{}}, "UsageEntry": {typ: usage.Entry{}},
		"ClientIdentity": {typ: clients.Identity{}}, "SecretRef": {typ: clients.SecretRef{}}, "SecretInput": {typ: clients.SecretInput{}},
		"SlackCommand": {typ: clients.Command{}}, "SlackOptions": {typ: clients.SlackOptions{}}, "GitHubOptions": {typ: clients.GitHubOptions{}},
		"ClientView": {typ: clientView{}}, "ClientCaps": {typ: clients.Caps{}}, "ClientInput": {typ: clientBody{}, written: true},
		"TextPayload": {typ: payload.Text{}}, "ThinkingPayload": {typ: payload.Thinking{}}, "ToolCallPayload": {typ: payload.ToolCall{}},
		"ToolDonePayload": {typ: payload.ToolDone{}}, "Provenance": {typ: payload.Provenance{}}, "CitationPayload": {typ: payload.Citation{}},
		"LifecyclePayload": {typ: payload.Lifecycle{}}, "Question": {typ: payload.Question{}}, "Answer": {typ: struct {
			payload.Answer
			// What a route hands out beside the subject (shownEvent).
			ByName string `json:"by_name,omitempty"`
		}{}},
		"QuestionTool": {typ: payload.QuestionTool{}}, "ChildQuestion": {typ: payload.ChildQuestion{}}, "ActAs": {typ: platform.ActAs{}}, "ActionPayload": {typ: payload.Action{}},
		"StoppedPayload": {typ: payload.Stopped{}}, "Cost": {typ: payload.Cost{}}, "Usage": {typ: payload.Usage{}},
		"ContextUsage": {typ: payload.ContextUsage{}}, "RunUsage": {typ: payload.RunUsage{}},
		"UsageCallPayload": {typ: payload.UsageCall{}}, "UsageChildPayload": {typ: payload.UsageChild{}},
		"PolicyCost": {typ: policy.Cost{}}, "UsageSpend": {typ: usage.Spend{}}, "SpendMeasure": {typ: spendMeasure{}},
		"AnalyticsUsage": {typ: runs.GroupUsage{}}, "UsageCallEvent": {typ: envelope.Event{}},
		// Every variant of the event union is the same Go struct.
		"TextDeltaEvent": {typ: envelope.Event{}}, "RunCompletedEvent": {typ: envelope.Event{}}, "ActionTakenEvent": {typ: envelope.Event{}},
	}
	schemas := obj(obj(s.doc["components"])["schemas"])
	for name, sh := range shapes {
		schema := obj(schemas[name])
		if schema == nil {
			t.Errorf("the spec has no schema %s", name)
			continue
		}
		props, required := map[string]bool{}, map[string]bool{}
		schemaFields(s, schema, props, required)
		typ := reflect.TypeOf(sh.typ)
		seen := map[string]bool{}
		for _, f := range jsonFields(typ) {
			seen[f.name] = true
			if !props[f.name] {
				t.Errorf("%s: %s marshals %q and the schema has no such property", name, typ, f.name)
				continue
			}
			if !sh.written && required[f.name] == f.optional {
				t.Errorf("%s.%s: required is %v in the schema, and the Go field's omitempty is %v", name, f.name, required[f.name], f.optional)
			}
		}
		for prop := range props {
			if !seen[prop] {
				t.Errorf("%s: the schema has %q and %s has no such field", name, prop, typ)
			}
		}
	}

	// The event union names every kind the runtime declares.
	var kinds []string
	for _, v := range obj(schemas["EventKind"])["enum"].([]any) {
		kinds = append(kinds, v.(string))
	}
	declared := []string{
		string(envelope.KindText), string(envelope.KindThinking), string(envelope.KindToolCall), string(envelope.KindToolDone),
		string(envelope.KindCitation), string(envelope.KindAccepted), string(envelope.KindStarted), string(envelope.KindParked),
		string(envelope.KindResumed), string(envelope.KindCompleted), string(envelope.KindFailed), string(envelope.KindChildParked), string(envelope.KindStopped), string(envelope.KindActionTaken),
		string(envelope.KindUsageCall), string(envelope.KindUsageChild),
		string(envelope.KindAgentCreated), string(envelope.KindAgentRevised), string(envelope.KindAgentDeleted),
	}
	if !slices.Equal(kinds, declared) {
		t.Errorf("EventKind is %v, the runtime declares %v", kinds, declared)
	}
	for _, union := range []string{"RunEvent", "ManifestEvent"} {
		mapping := obj(obj(obj(schemas[union])["discriminator"])["mapping"])
		for _, kind := range declared {
			if _, mapped := mapping[kind]; !mapped {
				t.Errorf("%s has no variant for kind %q", union, kind)
			}
		}
		if len(mapping) != len(declared) || len(sliceOf(obj(schemas[union])["oneOf"])) != len(declared) {
			t.Errorf("%s does not have exactly one variant per kind", union)
		}
	}
}

// contract checks real responses against the spec.
type contract struct {
	t    *testing.T
	f    *fixture
	spec *apiSpec
	seen map[string]bool
}

type reply struct {
	status int
	header http.Header
	raw    []byte
	body   any
}

func (r reply) field(name string) any { return obj(r.body)[name] }
func (r reply) str(name string) string {
	s, _ := r.field(name).(string)
	return s
}

// call sends a request to the operation named by its path template, and fails
// unless the answer has the wanted status, that status is documented for the
// operation, and the body validates against its schema.
func (c *contract) call(tok, method, template, path, body string, want int, hdr ...string) reply {
	c.t.Helper()
	out, _ := c.attempt(tok, method, template, path, body, want, true, hdr...)
	return out
}

// attempt is call that, when strict is false, reports a status other than the
// wanted one instead of failing, for a request that can lose a race.
func (c *contract) attempt(tok, method, template, path, body string, want int, strict bool, hdr ...string) (reply, bool) {
	c.t.Helper()
	op, known := c.spec.ops[method+" "+template]
	if !known {
		c.t.Fatalf("%s %s is not an operation in the spec", method, template)
	}
	if !matches(template, path) {
		c.t.Fatalf("%s is not an instance of %s", path, template)
	}
	req, err := http.NewRequest(method, c.f.srv.URL+path, strings.NewReader(body))
	if err != nil {
		c.t.Fatal(err)
	}
	if tok != "" {
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	raw, _ := io.ReadAll(res.Body)
	out := reply{status: res.StatusCode, header: res.Header, raw: raw}
	if res.StatusCode != want {
		if !strict {
			return out, false
		}
		c.t.Fatalf("%s %s = %d %s, want %d", method, path, res.StatusCode, raw, want)
	}
	schema, documented := op.schemas[strconv.Itoa(res.StatusCode)]
	if !documented {
		c.t.Errorf("%s %s answered %d, which the spec does not document for it", method, template, res.StatusCode)
		return out, true
	}
	c.seen[op.key()+" "+strconv.Itoa(res.StatusCode)] = true
	if schema == nil {
		if res.StatusCode == http.StatusNoContent && len(raw) != 0 {
			c.t.Errorf("%s %s answered 204 with a body", method, path)
		}
		return out, true
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		c.t.Errorf("%s %s: content type is %q, the spec says application/json", method, path, ct)
	}
	out.body, err = jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		c.t.Fatalf("%s %s: the body is not JSON: %s", method, path, raw)
	}
	if err := schema.Validate(out.body); err != nil {
		c.t.Errorf("%s %s (%d) does not match the spec:\n%v\nbody: %s", method, path, res.StatusCode, err, raw)
	}
	return out, true
}

// callFromLiveRun makes a call from a run of lead that is still going. A
// run on the offline provider ends quickly, so a call can find its caller
// already over; a fresh caller is started until one is caught alive.
func (c *contract) callFromLiveRun(tok, body string, want int) (caller, called reply) {
	c.t.Helper()
	for range 20 {
		lead := c.call(tok, "POST", "/api/runs", "/api/runs", `{"agent":"lead","text":"Plan it"}`, http.StatusAccepted)
		if out, ok := c.attempt(tok, "POST", "/api/runs/{id}/calls", "/api/runs/"+lead.str("id")+"/calls", body, want, false); ok {
			return lead, out
		}
	}
	c.t.Fatalf("no call from a live run answered %d", want)
	return reply{}, reply{}
}

func matches(template, path string) bool {
	path, _, _ = strings.Cut(path, "?")
	want, got := strings.Split(template, "/"), strings.Split(path, "/")
	if len(want) != len(got) {
		return false
	}
	for i := range want {
		if !strings.HasPrefix(want[i], "{") && want[i] != got[i] {
			return false
		}
	}
	return true
}

func needsQuery(op specOp) bool {
	for _, p := range sliceOf(op.raw["parameters"]) {
		if obj(p)["in"] == "query" && obj(p)["required"] == true {
			return true
		}
	}
	return false
}

// sweep reads every operation that is a GET with no parameters in its path and
// a JSON answer. Run on an empty workspace and on a busy one, it is what finds
// a list that is null when the spec says array.
func (c *contract) sweep(tok string) {
	c.t.Helper()
	for _, op := range c.spec.ops {
		if op.method != http.MethodGet || len(pathParams(op.path)) > 0 || op.schemas["200"] == nil || !strings.HasPrefix(op.path, "/api/") {
			continue
		}
		if op.path == "/api/events" || needsQuery(op) {
			continue
		}
		if op.path == "/api/read-audit" || op.path == "/api/audit" || op.path == "/api/me/memory" || op.path == "/api/me/notifications" {
			c.call(tok, "GET", op.path, op.path, "", http.StatusForbidden)
			continue
		}
		c.call(tok, "GET", op.path, op.path, "", http.StatusOK)
	}
}

func (c *contract) settled(tok, id string) reply {
	c.t.Helper()
	for range 400 {
		run := c.call(tok, "GET", "/api/runs/{id}", "/api/runs/"+id, "", http.StatusOK)
		if s := run.str("status"); s == "completed" || s == "failed" || s == "stopped" {
			return run
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.t.Fatalf("run %s never ended", id)
	return reply{}
}

func TestResponsesMatchTheSpec(t *testing.T) {
	// An agent on the "ollama" provider asks a person, so a run can park on
	// a question here. Every other agent is offline.
	f := newFixtureWith(t, platform.Config{AgentOptions: []agentrt.Option{
		agentrt.WithProvider(agentrt.ProviderOllama, func(agentrt.Spec) (saigetypes.Provider, error) {
			return &agenttest.ScriptedProvider{Responses: [][]saigetypes.Delta{
				agenttest.ToolCallResponse("call-ask", "ask_person", map[string]any{"question": "Which colour?", "options": []any{"red", "blue"}}),
				agenttest.TextResponse("Red."),
			}}, nil
		}),
	},
		// A tool that acts as each person, and one the workspace reaches as
		// itself. Neither is reached here.
		Ext: platform.Extensions{ToolProviders: []platform.ToolProviderSeed{
			{Provider: toolprovider.Provider{Tenant: "acme", Name: "issues", Transport: toolprovider.TransportHTTP, Endpoint: "https://issues.example.com/mcp", CredentialScope: toolprovider.ScopePerson}},
			{Provider: toolprovider.Provider{Tenant: "acme", Name: "wiki", Transport: toolprovider.TransportHTTP, Endpoint: "https://wiki.example.com/mcp", CredentialRef: "env:WIKI_TOKEN"}},
		}},
		CredentialRefs:   map[string][]string{"acme": {"op://People/{subject}/"}},
		CredentialValues: map[string]bool{"acme": true},
	})
	c := &contract{t: t, f: f, spec: loadSpec(t), seen: map[string]bool{}}
	acme, globex := f.machine(t, "acme", everything...), f.machine(t, "globex", everything...)
	auditAdmin := f.person(t, "acme", "audit-admin", "Audit admin", "humagen-admins")
	c.call(auditAdmin, "GET", "/api/read-audit", "/api/read-audit", "", http.StatusOK)
	libraryDraft := c.call(auditAdmin, "POST", "/api/library/skills", "/api/library/skills", `{"scope":{"kind":"workspace"},"version":0,"definition":{"name":"library-brief","description":"Briefing","instructions":"Be concise","tools":[]}}`, http.StatusCreated)
	libraryID := libraryDraft.str("id")
	c.call(auditAdmin, "POST", "/api/library/skills", "/api/library/skills", `{"scope":{"kind":"workspace"},"version":0,"definition":{"name":"library-brief","description":"Briefing","instructions":"Be concise","tools":[]}}`, http.StatusConflict)
	c.call(auditAdmin, "POST", "/api/library/skills", "/api/library/skills", `{"scope":{"kind":"project","project":"Missing"},"version":0,"definition":{"name":"missing","description":"Missing","instructions":"Be concise","tools":[]}}`, http.StatusNotFound)
	c.call(auditAdmin, "GET", "/api/library/skills", "/api/library/skills?scope=invalid", "", http.StatusBadRequest)
	c.call(auditAdmin, "GET", "/api/library/skills", "/api/library/skills?scope=project&project=Missing", "", http.StatusNotFound)

	c.call(auditAdmin, "GET", "/api/library/skills/{id}", "/api/library/skills/"+libraryID, "", http.StatusOK)
	c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/publish", "/api/library/skills/"+libraryID+"/versions/1/publish", "", http.StatusConflict)

	c.call(auditAdmin, "GET", "/api/audit", "/api/audit", "", http.StatusOK)
	c.call(auditAdmin, "GET", "/api/me/notifications", "/api/me/notifications", "", http.StatusOK)
	notificationPolicy := prefs.DefaultPersonal()
	notificationPolicy.Notifications["alert"] = prefs.Off
	notificationAt := time.Now()
	for start := 0; start < notifications.MaxEntries; start += 500 {
		candidates := []notifications.Candidate{}
		for n := start; n < start+500; n++ {
			candidates = append(candidates, notifications.Candidate{Message: fmt.Sprintf("capacity-%d", n), Kind: "alert", Stamp: notificationAt})
		}
		if _, err := f.p.Notifications.Reconcile("acme", "audit-admin", "", candidates, notificationPolicy, notificationAt, notificationAt.Add(-time.Hour), func(string) (notifications.Candidate, bool) { return notifications.Candidate{}, false }); err != nil {
			t.Fatal(err)
		}
	}
	notificationPolicy.Notifications["alert"] = prefs.Immediate
	if _, err := f.p.Prefs.SetPersonal("acme", "audit-admin", notificationPolicy); err != nil {
		t.Fatal(err)
	}
	notice, _, err := f.p.Inbox.Raise("acme", "notification-contract", inbox.Message{Kind: inbox.KindAlert, To: "audit-admin", Subject: "Native console notification", Parts: []inbox.Part{{Type: inbox.PartAlert, Alert: &inbox.AlertPart{ID: "notice", Rule: "rule", Kind: "run_failed", Title: "Notice", Severity: "warning"}}}}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	c.call(auditAdmin, "GET", "/api/me/notifications", "/api/me/notifications", "", http.StatusTooManyRequests)
	if err := f.p.Notifications.Retain("acme", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	delivered := c.call(auditAdmin, "GET", "/api/me/notifications", "/api/me/notifications", "", http.StatusOK)
	nativeDeliveries := sliceOf(delivered.field("notifications"))
	if len(nativeDeliveries) != 1 {
		t.Fatalf("notification delivery: %s", delivered.raw)
	}
	c.call(auditAdmin, "POST", "/api/me/notifications/{id}/ack", "/api/me/notifications/"+obj(nativeDeliveries[0])["id"].(string)+"/ack", "", http.StatusNoContent)
	if got, err := f.p.Inbox.Get("acme", notice.ID, "audit-admin"); err != nil || !got.Unread {
		t.Fatalf("delivery changed inbox: %+v %v", got, err)
	}

	c.call(auditAdmin, "GET", "/api/audit", "/api/audit?limit=501", "", http.StatusBadRequest)
	c.call(auditAdmin, "GET", "/api/read-audit", "/api/read-audit?limit=501", "", http.StatusBadRequest)

	// A workspace with nothing in it, then every refusal that needs no setup.
	c.call(auditAdmin, "GET", "/api/me/memory", "/api/me/memory", "", http.StatusOK)
	personalNote := c.call(auditAdmin, "POST", "/api/me/memory", "/api/me/memory", `{"text":"Prefer concise answers","kind":"preference"}`, http.StatusCreated)
	c.call(auditAdmin, "POST", "/api/me/memory/approve", "/api/me/memory/approve", `{"ids":[]}`, http.StatusOK)
	c.call(auditAdmin, "DELETE", "/api/me/memory/{id}", "/api/me/memory/"+personalNote.str("id"), "", http.StatusNoContent)
	c.call(auditAdmin, "DELETE", "/api/me/memory/{id}", "/api/me/memory/missing", "", http.StatusNotFound)
	c.sweep(globex)
	c.call("", "GET", "/api/me", "/api/me", "", http.StatusUnauthorized)
	c.call(f.machine(t, "acme"), "GET", "/api/agents", "/api/agents", "", http.StatusForbidden)
	c.call(f.machine(t, "acme"), "GET", "/api/me", "/api/me", "", http.StatusOK)
	c.call(f.machine(t, "acme"), "GET", "/api/access/explain", "/api/access/explain?permission=runs:read", "", http.StatusOK)
	c.call(acme, "GET", "/api/access/explain", "/api/access/explain?permission=runs:read", "", http.StatusOK)
	c.call(acme, "GET", "/api/access/explain", "/api/access/explain?permission=everything", "", http.StatusBadRequest)

	// A person's own credentials for the tools that act as them. A value
	// goes in and never comes back out.
	ana := f.iss.Sign(t, "ana", audience, map[string]any{"org": "acme", "groups": []any{"humagen-admins"}})
	const mine = "/api/me/credentials/{provider}"
	if list := c.call(ana, "GET", "/api/me/credentials", "/api/me/credentials", "", http.StatusOK); len(sliceOf(list.field("providers"))) != 1 || list.field("values") != true {
		t.Errorf("the tools that act as ana = %s", list.raw)
	}
	c.call(ana, "PUT", mine, "/api/me/credentials/issues", `{"ref":"op://People/ana/issues"}`, http.StatusOK)
	if stored := c.call(ana, "PUT", mine, "/api/me/credentials/issues", `{"value":"tok-ana-contract"}`, http.StatusOK); strings.Contains(string(stored.raw), "tok-ana-contract") || stored.str("kind") != "value" {
		t.Errorf("a stored value = %s", stored.raw)
	}
	if listed := c.call(ana, "GET", "/api/me/credentials", "/api/me/credentials", "", http.StatusOK); strings.Contains(string(listed.raw), "tok-ana-contract") {
		t.Errorf("a stored value was listed: %s", listed.raw)
	}
	c.call(ana, "PUT", mine, "/api/me/credentials/issues", `{"ref":"op://Infra/db/password"}`, http.StatusBadRequest)
	c.call(ana, "PUT", mine, "/api/me/credentials/issues", `{"ref":"op://People/ben/issues"}`, http.StatusBadRequest)
	c.call(ana, "PUT", mine, "/api/me/credentials/issues", `{"ref":"op://People/a/b","value":"v"}`, http.StatusBadRequest)
	c.call(ana, "PUT", mine, "/api/me/credentials/wiki", `{"ref":"op://People/ana/wiki"}`, http.StatusBadRequest)
	c.call(ana, "PUT", mine, "/api/me/credentials/ghost", `{"ref":"op://People/ana/ghost"}`, http.StatusNotFound)
	c.call(acme, "PUT", mine, "/api/me/credentials/issues", `{"ref":"op://People/svc/issues"}`, http.StatusForbidden)
	c.call(acme, "GET", "/api/me/credentials", "/api/me/credentials", "", http.StatusOK)
	c.call(ana, "DELETE", mine, "/api/me/credentials/issues", "", http.StatusNoContent)
	c.call(ana, "DELETE", mine, "/api/me/credentials/issues", "", http.StatusNotFound)

	// Agents, revisions and what they remember.
	c.call(acme, "GET", "/api/agent-hues", "/api/agent-hues?names=missing", "", http.StatusOK)
	c.call(acme, "GET", "/api/project-agent-hues", "/api/project-agent-hues?names=missing", "", http.StatusOK)
	c.call(acme, "GET", "/api/project-agent-hues", "/api/project-agent-hues?names="+strings.Repeat("x,", 100)+"x", "", http.StatusBadRequest)
	c.call(f.machine(t, "acme"), "GET", "/api/project-agent-hues", "/api/project-agent-hues?names=missing", "", http.StatusForbidden)
	c.call(acme, "GET", "/api/agent-hues", "/api/agent-hues?names="+strings.Repeat("x,", 100)+"x", "", http.StatusBadRequest)
	c.call(f.machine(t, "acme"), "GET", "/api/agent-hues", "/api/agent-hues?names=missing", "", http.StatusForbidden)

	c.call(acme, "POST", "/api/agents", "/api/agents", helperSpec, http.StatusCreated)
	libraryTest := c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/test", "/api/library/skills/"+libraryID+"/versions/1/test", `{"agent":"helper","project":"~","text":"Try the skill"}`, http.StatusAccepted)
	c.settled(auditAdmin, libraryTest.str("id"))
	c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/publish", "/api/library/skills/"+libraryID+"/versions/1/publish", "", http.StatusOK)
	c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/promote", "/api/library/skills/"+libraryID+"/versions/1/promote", `{"scope":{"kind":"personal"},"version":0}`, http.StatusCreated)
	c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/promote", "/api/library/skills/"+libraryID+"/versions/1/promote", `{"scope":{"kind":"personal"},"version":0}`, http.StatusConflict)

	c.call(acme, "POST", "/api/agents", "/api/agents", helperSpec, http.StatusConflict)
	c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"helper","metadata":{}}`, http.StatusBadRequest)
	revised := `{"expected_rev":1,"spec":{"name":"helper","description":"Helps.","model":"offline","provider":"offline","system_prompt":"Help well."}}`
	c.call(acme, "PUT", "/api/agents/{name}", "/api/agents/helper", revised, http.StatusOK)
	c.call(acme, "PUT", "/api/agents/{name}", "/api/agents/helper", revised, http.StatusConflict)
	c.call(acme, "PUT", "/api/agents/{name}", "/api/agents/ghost", strings.ReplaceAll(revised, "helper", "ghost"), http.StatusNotFound)
	c.call(acme, "GET", "/api/agents/{name}/history", "/api/agents/helper/history", "", http.StatusOK)
	c.call(globex, "GET", "/api/agents/{name}/history", "/api/agents/helper/history", "", http.StatusNotFound)
	// Who may call it, and whom it may call.
	c.call(acme, "GET", "/api/agents/{name}/call-policy", "/api/agents/helper/call-policy", "", http.StatusOK)
	c.call(acme, "PUT", "/api/agents/{name}/call-policy", "/api/agents/helper/call-policy",
		`{"callers":"listed","allow":["helper"],"approval":"unlisted","trusted":["helper"],"approvers":["role:manager"],"expires_seconds":3600}`, http.StatusOK)
	c.call(acme, "PUT", "/api/agents/{name}/call-policy", "/api/agents/helper/call-policy", `{"callers":"listed","allow":["ghost"],"approval":"never"}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/agents/{name}/call-policy", "/api/agents/helper/call-policy", `{"callers":"anyone","approval":"never"}`, http.StatusOK)
	c.call(acme, "GET", "/api/agents/{name}/reachable", "/api/agents/helper/reachable", "", http.StatusOK)
	note := c.call(acme, "POST", "/api/agents/{name}/memory", "/api/agents/helper/memory", `{"text":"Answer briefly."}`, http.StatusCreated)
	c.call(acme, "GET", "/api/agents/{name}/memory", "/api/agents/helper/memory", "", http.StatusOK)
	c.call(acme, "POST", "/api/agents/{name}/memory/approve", "/api/agents/helper/memory/approve", `{"ids":[]}`, http.StatusOK)
	c.call(acme, "DELETE", "/api/agents/{name}/memory/{id}", "/api/agents/helper/memory/"+note.str("id"), "", http.StatusNoContent)
	c.call(acme, "GET", "/api/pick", "/api/pick?q=help+me", "", http.StatusOK)
	c.call(acme, "GET", "/api/pick", "/api/pick?q=help+me&project=~", "", http.StatusOK)
	c.call(acme, "GET", "/api/pick", "/api/pick?q=help+me&project=Ghost", "", http.StatusNotFound)
	// A tag names an agent or a person, so an agent cannot take a reserved
	// name or a person's handle.
	c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"auto","model":"offline","provider":"offline","system_prompt":"x"}`, http.StatusConflict)
	f.p.SeePerson(auth.Principal{Subject: "okta|dana", Tenant: "acme", Kind: auth.User, Name: "Dana Scully", Roles: []auth.Role{auth.Viewer}})
	if taken := c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"dana-scully","model":"offline","provider":"offline","system_prompt":"x"}`, http.StatusConflict); !strings.Contains(taken.str("error"), "a person goes by @dana-scully") {
		t.Errorf("an agent named like a person's handle = %s", taken.raw)
	}

	// What a Slack surface does: start a turn under its own idempotency key,
	// continue the conversation, read the run and its tree, and stop it.
	first := c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"Say hello"}`, http.StatusAccepted, "Idempotency-Key", "slack-evt-1")
	again := c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"Say hello"}`, http.StatusAccepted, "Idempotency-Key", "slack-evt-1")
	id := first.str("id")
	if id != again.str("id") || id != runtime.RunID("acme", "api-8-svc-acme-slack-evt-1") {
		t.Errorf("the same key gave runs %q and %q", id, again.str("id"))
	}
	if actor := obj(first.field("actor")); actor["bot"] != true {
		t.Errorf("a machine's run is not marked as a bot's: %v", actor)
	}
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"  "}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"hi","metadata":{"channel":"C1"}}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"ghost","text":"hi"}`, http.StatusNotFound)
	// A surface asks for a person only through a client that maps them.
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"hi","act_as":{"client":"nope","user":"U0X"}}`, http.StatusForbidden)
	if done := c.settled(acme, id); done.str("status") != "completed" || done.str("answer") == "" {
		t.Fatalf("the first turn = %s", done.raw)
	}
	c.call(acme, "PUT", "/api/runs/{id}/feedback", "/api/runs/"+id+"/feedback", `{"rating":"down","reason":"incomplete","comment":"Please add sources"}`, http.StatusOK)
	c.call(acme, "GET", "/api/runs/{id}/feedback", "/api/runs/"+id+"/feedback", "", http.StatusOK)
	c.call(acme, "GET", "/api/runs/{id}/feedback", "/api/runs/"+id+"/feedback?after=ada@example.com", "", http.StatusBadRequest)
	next := c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"And again","after":"`+id+`"}`, http.StatusAccepted)
	if next.str("thread") != first.str("thread") || next.str("prev") != id {
		t.Errorf("the follow-up is not in the first turn's conversation: %s", next.raw)
	}
	c.settled(acme, next.str("id"))
	c.call(acme, "GET", "/api/runs/{id}/tree", "/api/runs/"+id+"/tree", "", http.StatusOK)
	c.call(acme, "GET", "/api/runs/{id}/trace", "/api/runs/"+id+"/trace", "", http.StatusOK)
	c.call(acme, "GET", "/api/runs/{id}/events", "/api/runs/"+id+"/events", "", http.StatusOK)
	c.call(acme, "GET", "/api/runs/{id}/manifest", "/api/runs/"+id+"/manifest", "", http.StatusOK)
	c.call(acme, "GET", "/api/threads/{id}", "/api/threads/"+first.str("thread"), "", http.StatusOK)
	c.call(acme, "GET", "/api/threads", "/api/threads?limit=1", "", http.StatusOK)
	c.call(acme, "GET", "/api/threads", "/api/threads?limit=0", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/threads", "/api/threads?before=nonsense", "", http.StatusBadRequest)
	stopped := c.call(acme, "POST", "/api/runs/{id}/stop", "/api/runs/"+id+"/stop", "", http.StatusOK)
	if stopped.str("status") != "completed" {
		t.Errorf("stopping a run that had ended changed it: %s", stopped.raw)
	}
	// A run still working is asked and answered 202 while it stops. The
	// offline model can finish first, so a run that has already ended (200)
	// is tried again with a new one.
	asked := false
	for range 20 {
		live := c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"Stop me"}`, http.StatusAccepted)
		out, ok := c.attempt(acme, "POST", "/api/runs/{id}/stop", "/api/runs/"+live.str("id")+"/stop", "", http.StatusAccepted, false)
		end := c.settled(acme, live.str("id"))
		if !ok {
			continue
		}
		asked = true
		if out.field("stopping") != true {
			t.Errorf("a run asked to stop is not stopping: %s", out.raw)
		}
		if s := end.str("status"); s != "stopped" && s != "completed" {
			t.Errorf("a run asked to stop ended %s", end.raw)
		}
		break
	}
	if !asked {
		t.Error("no stop found a run still working")
	}
	// A run that asks a person: its question on the run and in the inbox,
	// and answered in place.
	c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"asker","model":"x","provider":"ollama","system_prompt":"Ask.","tools":["ask_person"],"answerers":["svc-acme"]}`, http.StatusCreated)
	parked := c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"asker","text":"Pick"}`, http.StatusAccepted)
	var question string
	for deadline := time.Now().Add(10 * time.Second); question == "" && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		now := c.call(acme, "GET", "/api/runs/{id}", "/api/runs/"+parked.str("id"), "", http.StatusOK)
		question, _ = obj(now.field("question"))["id"].(string)
	}
	if question == "" {
		t.Fatal("the asking run never parked on its question")
	}
	// Its thread says, to someone it asks, that the question is theirs.
	room := c.call(acme, "GET", "/api/threads/{id}", "/api/threads/"+parked.str("thread"), "", http.StatusOK)
	var ask map[string]any
	for _, turn := range sliceOf(room.field("turns")) {
		if a := obj(obj(turn)["ask"]); a != nil {
			ask = a
		}
	}
	if ask["question"] != question || ask["yours"] != true || ask["for"] != "answerers" {
		t.Errorf("the parked run's ask = %v in %s", ask, room.raw)
	}
	c.call(acme, "PUT", "/api/runs/{id}/feedback", "/api/runs/"+parked.str("id")+"/feedback", `{"rating":"up"}`, http.StatusConflict)
	answer := "/api/runs/{id}/questions/{question}/answer"
	at := "/api/runs/" + parked.str("id") + "/questions/" + question + "/answer"
	c.call(acme, "GET", "/api/inbox", "/api/inbox?kind=clarification", "", http.StatusOK)
	c.call(acme, "POST", answer, at, `{"action":"choose","choice":"green"}`, http.StatusBadRequest)
	c.call(acme, "POST", answer, at, `{"action":"choose","choice":"red","act_as":{"client":"nope","user":"U0X"}}`, http.StatusForbidden)
	c.call(globex, "POST", answer, at, `{"action":"choose","choice":"red"}`, http.StatusNotFound)
	c.call(acme, "POST", answer, "/api/runs/"+parked.str("id")+"/questions/q-404/answer", `{"action":"choose","choice":"red"}`, http.StatusNotFound)
	settledQ := c.call(acme, "POST", answer, at, `{"action":"choose","choice":"red"}`, http.StatusOK)
	if o := obj(obj(sliceOf(settledQ.field("parts"))[0])["outcome"]); o["action"] != "choose" || o["note"] != "red" {
		t.Errorf("the answered question = %s", settledQ.raw)
	}
	c.settled(acme, parked.str("id"))
	// The run's events say who answered, by subject and by name, and the
	// answer validates against the spec's body for it.
	answeredBy := ""
	for _, e := range sliceOf(c.call(acme, "GET", "/api/runs/{id}/events", "/api/runs/"+parked.str("id")+"/events", "", http.StatusOK).field("events")) {
		if obj(e)["kind"] != "lifecycle.resumed" {
			continue
		}
		raw, _ := base64.StdEncoding.DecodeString(obj(e)["payload"].(string))
		var body struct {
			Answer struct {
				By     string `json:"by"`
				ByName string `json:"by_name"`
			} `json:"answer"`
		}
		if json.Unmarshal(raw, &body) == nil {
			answeredBy = body.Answer.By + " " + body.Answer.ByName
		}
	}
	if answeredBy != "svc-acme svc-acme" {
		t.Errorf("the run's events say it was answered by %q", answeredBy)
	}
	c.call(acme, "POST", "/api/runs/{id}/calls", "/api/runs/"+id+"/calls", `{"key":"k1","agent":"helper","text":"hi","mode":"subagent"}`, http.StatusForbidden)
	c.call(acme, "POST", "/api/runs/{id}/calls", "/api/runs/"+id+"/calls", `{"mode":7}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"lead","model":"offline","provider":"offline","system_prompt":"Lead.","access":{"spawn":["helper"]}}`, http.StatusCreated)
	lead, called := c.callFromLiveRun(acme, `{"key":"k1","agent":"helper","text":"Do it","mode":"delegate"}`, http.StatusAccepted)
	if called.str("parent") != lead.str("id") || called.str("lifecycle") != "delegated" {
		t.Errorf("the called run is not under its caller: %s", called.raw)
	}
	c.settled(acme, called.str("id"))
	c.settled(acme, lead.str("id"))

	// Another workspace's run is not there, whatever is asked of it.
	for _, op := range []struct{ method, template, suffix string }{
		{"GET", "/api/runs/{id}", ""}, {"GET", "/api/runs/{id}/tree", "/tree"}, {"GET", "/api/runs/{id}/trace", "/trace"},
		{"GET", "/api/runs/{id}/events", "/events"}, {"GET", "/api/runs/{id}/manifest", "/manifest"}, {"POST", "/api/runs/{id}/stop", "/stop"},
	} {
		c.call(globex, op.method, op.template, "/api/runs/"+id+op.suffix, "", http.StatusNotFound)
	}
	c.call(globex, "GET", "/api/threads/{id}", "/api/threads/"+first.str("thread"), "", http.StatusNotFound)
	c.call(globex, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"hi","after":"`+id+`"}`, http.StatusNotFound)

	// Limits: a refusal, the inbox message it raises, and resolving it.
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Launch","description":"The launch."}`, http.StatusCreated)
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Launch"}`, http.StatusConflict)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100,"max_concurrent":50}`, http.StatusOK)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"project","name":"Launch"},"period":"day","max_runs":1}`, http.StatusOK)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"project","name":"Ghost"},"period":"day","max_runs":1}`, http.StatusNotFound)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day"}`, http.StatusBadRequest)
	inProject := `{"agent":"helper","text":"go","project":"Launch"}`
	inLaunch := c.call(acme, "POST", "/api/runs", "/api/runs", inProject, http.StatusAccepted).str("id")
	c.settled(acme, inLaunch)
	refused := c.call(acme, "POST", "/api/runs", "/api/runs", inProject, http.StatusTooManyRequests)
	if refused.str("limit") != "runs" || refused.field("resets_at") == nil || refused.field("policy") == nil {
		t.Errorf("the refusal does not say what refused it: %s", refused.raw)
	}
	c.call(acme, "GET", "/api/usage", "/api/usage", "", http.StatusOK)
	c.call(acme, "GET", "/api/usage/notices", "/api/usage/notices", "", http.StatusOK)

	budget := c.call(acme, "GET", "/api/inbox", "/api/inbox?kind=budget&state=open&limit=10", "", http.StatusOK)
	list := sliceOf(budget.field("messages"))
	if len(list) == 0 {
		t.Fatalf("the refusal raised no message: %s", budget.raw)
	}
	msg := obj(list[0])
	mid := msg["id"].(string)
	parts := map[string]string{}
	for _, p := range sliceOf(msg["parts"]) {
		parts[obj(p)["type"].(string)] = obj(p)["id"].(string)
	}
	inboxPath := "/api/inbox/" + mid
	c.call(acme, "GET", "/api/inbox", "/api/inbox?state=nonsense", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?kind=approval&agent=helper&state=open", "", http.StatusOK)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?alert="+obj(msg["source"])["alert"].(string), "", http.StatusOK)
	c.call(acme, "GET", "/api/inbox/count", "/api/inbox/count", "", http.StatusOK)
	c.call(acme, "GET", "/api/inbox/{id}", inboxPath, "", http.StatusOK)
	c.call(globex, "GET", "/api/inbox/{id}", inboxPath, "", http.StatusNotFound)
	c.call(acme, "POST", "/api/inbox/{id}/read", inboxPath+"/read", `{}`, http.StatusOK)
	c.call(acme, "POST", "/api/inbox/{id}/read", inboxPath+"/read", ``, http.StatusBadRequest)
	c.call(acme, "POST", "/api/inbox/{id}/snooze", inboxPath+"/snooze", `{"until":"2999-01-01T00:00:00Z"}`, http.StatusOK)
	resolve := "/api/inbox/{id}/parts/{part}/resolve"
	reader := f.machine(t, "acme", "humagen:runs:read", "humagen:alerts:read")
	c.call(reader, "POST", resolve, inboxPath+"/parts/"+parts["alert"]+"/resolve", `{"action":"acknowledge"}`, http.StatusForbidden)
	c.call(acme, "POST", resolve, inboxPath+"/parts/"+parts["budget"]+"/resolve", `{"action":"approve"}`, http.StatusBadRequest)
	c.call(acme, "POST", resolve, inboxPath+"/parts/p99/resolve", `{"action":"acknowledge"}`, http.StatusNotFound)
	c.call(globex, "POST", resolve, inboxPath+"/parts/"+parts["alert"]+"/resolve", `{"action":"acknowledge"}`, http.StatusNotFound)
	c.call(acme, "POST", resolve, inboxPath+"/parts/"+parts["budget"]+"/resolve", `{"action":"raise_limit","limits":{"max_runs":5},"note":"Launch week."}`, http.StatusOK)
	c.call(acme, "POST", resolve, inboxPath+"/parts/"+parts["alert"]+"/resolve", `{"action":"acknowledge"}`, http.StatusOK)
	c.call(acme, "GET", "/api/inbox/{id}", inboxPath, "", http.StatusOK)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?state=all", "", http.StatusOK)
	c.call(acme, "POST", "/api/runs", "/api/runs", inProject, http.StatusAccepted)
	// A second refusal, to set aside rather than resolve.
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"agent","name":"helper"},"period":"day","max_concurrent":1,"max_runs":1}`, http.StatusOK)
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"over"}`, http.StatusTooManyRequests)
	open := c.call(acme, "GET", "/api/inbox", "/api/inbox", "", http.StatusOK)
	if left := sliceOf(open.field("messages")); len(left) > 0 {
		c.call(acme, "POST", "/api/inbox/{id}/dismiss", "/api/inbox/"+obj(left[0])["id"].(string)+"/dismiss", "", http.StatusOK)
	}
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=agent&name=helper", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=agent&name=helper", "", http.StatusNotFound)

	// Projects and their memory.
	shared := c.call(acme, "POST", "/api/projects/{name}/memory", "/api/projects/Launch/memory", `{"text":"We ship on Friday."}`, http.StatusCreated)
	c.call(acme, "GET", "/api/projects/{name}/memory", "/api/projects/Launch/memory", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}/memory", "/api/projects/Ghost/memory", "", http.StatusNotFound)
	c.call(acme, "POST", "/api/projects/{name}/memory/approve", "/api/projects/Launch/memory/approve", `{"ids":[]}`, http.StatusOK)
	c.call(acme, "DELETE", "/api/projects/{name}/memory/{id}", "/api/projects/Launch/memory/"+shared.str("id"), "", http.StatusNoContent)
	c.call(acme, "POST", "/api/projects/{name}/memory", "/api/projects/~/memory", `{"text":"We sell warehouse robots."}`, http.StatusCreated)
	c.call(acme, "GET", "/api/projects/{name}/memory", "/api/projects/~/memory", "", http.StatusOK)

	// The tree: a team, an account under it, the workspace itself, and what
	// changed on each.
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Field","title":"Field engineering","kind":"team","instructions":"Never promise dates."}`, http.StatusCreated)
	account := c.call(acme, "POST", "/api/projects", "/api/projects",
		`{"name":"Globex","parent":"Field","kind":"account","account":{"customer":"Globex Inc","external_id":"sf:0015g","url":"https://crm.example.com/globex"}}`, http.StatusCreated)
	if account.field("depth") != json.Number("2") || len(sliceOf(account.field("path"))) != 2 {
		t.Errorf("an account under Field is not two levels down: %s", account.raw)
	}
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Initech","parent":"Globex","kind":"account"}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Nowhere","parent":"Ghost"}`, http.StatusNotFound)
	c.call(acme, "GET", "/api/projects", "/api/projects?parent=~", "", http.StatusOK)
	if within := c.call(acme, "GET", "/api/projects", "/api/projects?within=Field", "", http.StatusOK); len(sliceOf(within.field("projects"))) != 2 {
		t.Errorf("within Field = %s, want Field and Globex", within.raw)
	}
	c.call(acme, "GET", "/api/projects", "/api/projects?parent=Ghost", "", http.StatusNotFound)
	detail := c.call(acme, "GET", "/api/projects/{name}", "/api/projects/Globex", "", http.StatusOK)
	if ancestors := sliceOf(detail.field("ancestors")); len(ancestors) != 2 || obj(ancestors[0])["name"] != "" || obj(ancestors[1])["name"] != "Field" {
		t.Errorf("Globex's ancestors are not the workspace then Field: %s", detail.raw)
	}
	c.call(acme, "GET", "/api/projects/{name}", "/api/projects/Launch", "", http.StatusOK)
	root := c.call(acme, "GET", "/api/projects/{name}", "/api/projects/~", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}", "/api/projects/Ghost", "", http.StatusNotFound)
	if updated := c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/Field",
		`{"version":1,"title":"Field","description":"Customer work.","instructions":"Never promise dates.","agent_posts":"review"}`, http.StatusOK); updated.str("agent_posts") != "review" {
		t.Errorf("Field reviews its agents' posts: %s", updated.raw)
	}
	c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/Field", `{"version":1,"title":"Stale"}`, http.StatusConflict)
	c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/Field", `{"version":2,"parent":"Launch"}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/Field", `{"version":2,"agent_posts":"sometimes"}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/~",
		`{"version":`+string(obj(root.field("project"))["version"].(json.Number))+`,"instructions":"Answer plainly."}`, http.StatusOK)
	c.call(acme, "PUT", "/api/projects/{name}", "/api/projects/Ghost", `{"version":1}`, http.StatusNotFound)
	page := c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/Field/history?limit=1", "", http.StatusOK)
	if cursor := page.str("next"); cursor == "" {
		t.Errorf("a project changed twice has one page of one: %s", page.raw)
	} else if last := c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/Field/history?limit=1&before="+cursor, "", http.StatusOK); last.field("next") != nil {
		t.Errorf("the second page of two is not the last: %s", last.raw)
	}
	c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/~/history", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/Field/history?limit=0", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/Field/history?before=nonsense", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/projects/{name}/history", "/api/projects/Ghost/history", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/threads", "/api/threads?within=~&project=Launch", "", http.StatusOK)
	c.call(acme, "GET", "/api/threads", "/api/threads?project=Ghost", "", http.StatusNotFound)
	c.call(globex, "GET", "/api/threads", "/api/threads?project=Launch", "", http.StatusNotFound)
	// Which agents a project turns on, and where an agent lives.
	c.call(acme, "GET", "/api/projects/{name}/agents", "/api/projects/~/agents", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}/agents", "/api/projects/Ghost/agents", "", http.StatusNotFound)
	agentsAt := "/api/projects/{name}/agents"
	fieldAgents := c.call(acme, "GET", agentsAt, "/api/projects/Field/agents", "", http.StatusOK)
	v, _ := strconv.Atoi(fmt.Sprint(fieldAgents.field("version")))
	set := c.call(acme, "PUT", agentsAt, "/api/projects/Field/agents", fmt.Sprintf(`{"version":%d,"enable":["helper"],"default":"helper"}`, v), http.StatusOK)
	if d := obj(set.field("default_resolved")); d["agent"] != "helper" || d["from"] != "project:Field" {
		t.Errorf("Field's default = %s", set.raw)
	}
	c.call(acme, "PUT", agentsAt, "/api/projects/Field/agents", fmt.Sprintf(`{"version":%d,"enable":[]}`, v), http.StatusConflict)
	c.call(acme, "PUT", agentsAt, "/api/projects/Field/agents", fmt.Sprintf(`{"version":%d,"enable":["ghost"]}`, v+1), http.StatusBadRequest)
	c.call(acme, "PUT", agentsAt, "/api/projects/Ghost/agents", `{"version":1}`, http.StatusNotFound)
	// The workspace's own lists hold the workspace default: projects:govern.
	c.call(f.machine(t, "acme", "humagen:projects:write"), "PUT", agentsAt, "/api/projects/~/agents", `{"version":1,"enable":["*"]}`, http.StatusForbidden)
	c.call(acme, "POST", "/api/agents", "/api/agents", `{"name":"fieldbot","model":"offline","provider":"offline","system_prompt":"Field work."}`, http.StatusCreated)
	home := "/api/agents/{name}/home"
	if lives := c.call(acme, "PUT", home, "/api/agents/fieldbot/home", `{"project":"Field"}`, http.StatusOK); lives.str("home") != "Field" {
		t.Errorf("fieldbot's home = %s", lives.raw)
	}
	if inUse := c.call(acme, "PUT", home, "/api/agents/helper/home", `{"project":"Launch"}`, http.StatusConflict); !strings.Contains(inUse.str("error"), "the default of Field") {
		t.Errorf("homing an agent Field defaults to = %s", inUse.raw)
	}
	c.call(acme, "PUT", home, "/api/agents/humagen-writer/home", `{"project":"Field"}`, http.StatusBadRequest)
	c.call(acme, "PUT", home, "/api/agents/ghost/home", `{"project":"Field"}`, http.StatusNotFound)

	// The directory.
	if _, err := f.p.Prefs.SetPhoto("acme", "okta|dana", []byte("\x89PNG\r\n\x1a\n"+strings.Repeat("\x00", 32))); err != nil {
		t.Fatal(err)
	}
	f.p.SeePerson(auth.Principal{Subject: "okta|fox", Tenant: "acme", Kind: auth.User, Name: "Fox"})
	page1 := c.call(acme, "GET", "/api/people", "/api/people?limit=1", "", http.StatusOK)
	if people := sliceOf(page1.field("people")); len(people) != 1 || page1.field("next") == nil {
		t.Errorf("the first page of people = %s", page1.raw)
	}
	// Whoever else has signed in by now, such as ana above, Dana is found by
	// her name, with her picture.
	dana := c.call(acme, "GET", "/api/people", "/api/people?q=dana&limit=1", "", http.StatusOK)
	if people := sliceOf(dana.field("people")); len(people) != 1 || obj(people[0])["handle"] != "dana-scully" || obj(people[0])["picture"] == nil {
		t.Errorf("Dana in the directory = %s", dana.raw)
	}
	c.call(acme, "GET", "/api/people", "/api/people?q=fox", "", http.StatusOK)
	c.call(acme, "GET", "/api/people", "/api/people?limit=51", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/people", "/api/people?before=Not+A+Handle", "", http.StatusBadRequest)
	if photo := c.call(acme, "GET", "/api/people/{handle}/photo", "/api/people/dana-scully/photo", "", http.StatusOK); photo.header.Get("Content-Type") != "image/png" {
		t.Errorf("a person's picture is served as %q", photo.header.Get("Content-Type"))
	}
	c.call(acme, "GET", "/api/people/{handle}/photo", "/api/people/fox/photo", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/people/{handle}/photo", "/api/people/ghost/photo", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/projects", "/api/projects?q=globex%20inc&kind=account&limit=1", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects", "/api/projects?before=nonsense", "", http.StatusBadRequest)

	// Who holds a role, a move, and an owner's tighten-only limit.
	members := c.call(acme, "GET", "/api/projects/{name}/members", "/api/projects/Globex/members", "", http.StatusOK)
	version := func(r reply) int { n, _ := strconv.Atoi(string(r.field("version").(json.Number))); return n }
	mv := version(members)
	c.call(acme, "GET", "/api/projects/{name}/members", "/api/projects/Ghost/members", "", http.StatusNotFound)
	mset := c.call(acme, "PUT", "/api/projects/{name}/members", "/api/projects/Globex/members",
		fmt.Sprintf(`{"version":%d,"members":[{"who":"*","role":"viewer"},{"who":"okta|ana","role":"owner"},{"who":"okta|dana","role":"member"}]}`, mv), http.StatusOK)
	// A person's own projects are listed in the directory, as the schema says.
	if dir := c.call(acme, "GET", "/api/people", "/api/people", "", http.StatusOK); !strings.Contains(string(dir.raw), `"projects":[{"name":"Globex"`) {
		t.Errorf("the directory does not list Dana's projects: %s", dir.raw)
	}
	c.call(acme, "PUT", "/api/projects/{name}/members", "/api/projects/Globex/members", fmt.Sprintf(`{"version":%d,"members":[{"who":"*","role":"viewer"}]}`, mv), http.StatusConflict)
	// Members only: read by its members, and by projects:govern.
	closed := c.call(acme, "PUT", "/api/projects/{name}/members", "/api/projects/Globex/members", fmt.Sprintf(`{"version":%d,"members":[{"who":"okta|ana","role":"owner"}]}`, version(mset)), http.StatusOK)
	c.call(acme, "PUT", "/api/projects/{name}/members", "/api/projects/Globex/members", fmt.Sprintf(`{"version":%d,"members":[{"who":"*","role":"ruler"}]}`, version(closed)), http.StatusBadRequest)
	ops := c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Ops"}`, http.StatusCreated)
	moved := c.call(acme, "POST", "/api/projects/{name}/move", "/api/projects/Ops/move", fmt.Sprintf(`{"version":%d,"parent":"Field"}`, version(ops)), http.StatusOK)
	if moved.str("parent") != "Field" || moved.field("depth") != json.Number("2") {
		t.Errorf("Ops moved under Field = %s", moved.raw)
	}
	c.call(acme, "POST", "/api/projects/{name}/move", "/api/projects/Ops/move", fmt.Sprintf(`{"version":%d,"parent":""}`, version(ops)), http.StatusConflict)
	field := c.call(acme, "GET", "/api/projects/{name}", "/api/projects/Field", "", http.StatusOK)
	c.call(acme, "POST", "/api/projects/{name}/move", "/api/projects/Field/move",
		fmt.Sprintf(`{"version":%s,"parent":"Ops"}`, obj(field.field("project"))["version"].(json.Number)), http.StatusBadRequest)
	c.call(acme, "POST", "/api/projects/{name}/move", "/api/projects/Ops/move", fmt.Sprintf(`{"version":%d,"parent":"Ghost"}`, version(moved)), http.StatusNotFound)
	c.call(acme, "PUT", "/api/projects/{name}/limits", "/api/projects/Field/limits", `{"period":"day","max_runs":50}`, http.StatusOK)
	if limit := c.call(acme, "PUT", "/api/projects/{name}/limits", "/api/projects/Globex/limits", `{"period":"day","max_runs":5}`, http.StatusOK); limit.str("authority") != "owner" {
		t.Errorf("an owner's limit = %s", limit.raw)
	}
	c.call(acme, "PUT", "/api/projects/{name}/limits", "/api/projects/Globex/limits", `{"period":"day","max_runs":60}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/projects/{name}/limits", "/api/projects/~/limits", `{"period":"day","max_runs":5}`, http.StatusForbidden)
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Vault"}`, http.StatusCreated)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"project","name":"Vault"},"period":"day","max_runs":9}`, http.StatusOK)
	c.call(acme, "PUT", "/api/projects/{name}/limits", "/api/projects/Vault/limits", `{"period":"day","max_runs":5}`, http.StatusConflict)
	c.call(acme, "DELETE", "/api/projects/{name}/limits", "/api/projects/Vault/limits", "", http.StatusConflict)
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=project&name=Vault", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/projects/{name}/limits", "/api/projects/Vault/limits", "", http.StatusNotFound)
	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Vault", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/projects/{name}/limits", "/api/projects/Globex/limits", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/projects/{name}/limits", "/api/projects/Field/limits", "", http.StatusNoContent)

	// A project's inbox and its announcements.
	news := c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Field/announcements",
		`{"subject":"Field offsite","text":"We meet in Lisbon in May.","severity":"info"}`, http.StatusCreated)
	if news.str("kind") != "announcement" || news.field("unread") != false {
		t.Errorf("an announcement, as its author has it = %s", news.raw)
	}
	c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Field/announcements", `{"subject":"","text":"Nothing to say."}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Ghost/announcements", `{"subject":"Hi","text":"Hi."}`, http.StatusNotFound)
	for range 19 {
		c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Ops/announcements", `{"subject":"Again","text":"Once more."}`, http.StatusCreated)
	}
	c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Ops/announcements", `{"subject":"Again","text":"Once more."}`, http.StatusCreated)
	c.call(acme, "POST", "/api/projects/{name}/announcements", "/api/projects/Ops/announcements", `{"subject":"Again","text":"Once more."}`, http.StatusTooManyRequests)
	if page := c.call(acme, "GET", "/api/projects/{name}/inbox", "/api/projects/Field/inbox?kind=announcement&children=true&limit=5", "", http.StatusOK); len(sliceOf(page.field("messages"))) != 5 || page.str("next") == "" {
		t.Errorf("Field's inbox with its sub-projects = %s", page.raw)
	}
	c.call(acme, "GET", "/api/projects/{name}/inbox", "/api/projects/~/inbox?state=all", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}/inbox", "/api/projects/Field/inbox?kind=gossip", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/projects/{name}/inbox", "/api/projects/Field/inbox?before=nonsense", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/projects/{name}/inbox", "/api/projects/Ghost/inbox", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?project=Field&kind=announcement", "", http.StatusOK)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?project=Ghost", "", http.StatusNotFound)
	// Hidden for the caller alone, and brought back. A message that is not
	// an announcement is set aside for everyone, so it is not.
	newsPath := "/api/inbox/" + news.str("id")
	if hid := c.call(acme, "POST", "/api/inbox/{id}/dismiss", newsPath+"/dismiss", "", http.StatusOK); hid.str("state") != "dismissed" || hid.field("hidden") != true {
		t.Errorf("an announcement hidden for the caller = %s", hid.raw)
	}
	if back := c.call(acme, "POST", "/api/inbox/{id}/unhide", newsPath+"/unhide", "", http.StatusOK); back.str("state") != "open" || back.field("hidden") != nil {
		t.Errorf("an announcement brought back = %s", back.raw)
	}
	c.call(acme, "POST", "/api/inbox/{id}/unhide", newsPath+"/unhide", "", http.StatusOK)
	c.call(acme, "POST", "/api/inbox/{id}/unhide", inboxPath+"/unhide", "", http.StatusBadRequest)
	c.call(acme, "DELETE", "/api/projects/{name}/announcements/{id}", "/api/projects/Ops/announcements/"+news.str("id"), "", http.StatusNotFound)
	c.call(acme, "DELETE", "/api/projects/{name}/announcements/{id}", "/api/projects/Field/announcements/"+news.str("id"), "", http.StatusNoContent)

	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Field", "", http.StatusConflict)
	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/~", "", http.StatusBadRequest)
	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Globex", "", http.StatusNoContent)
	// A workspace holds at most 10 company projects.
	initech := f.machine(t, "initech", everything...)
	for i := range 10 {
		c.call(initech, "POST", "/api/projects", "/api/projects", fmt.Sprintf(`{"name":"Company %d","kind":"company"}`, i), http.StatusCreated)
	}
	c.call(initech, "POST", "/api/projects", "/api/projects", `{"name":"One too many","kind":"company"}`, http.StatusTooManyRequests)
	// A conversation in the workspace that names no agent, where nothing
	// answers untagged messages, starts nothing.
	initechRoot := c.call(initech, "GET", agentsAt, "/api/projects/~/agents", "", http.StatusOK)
	c.call(initech, "PUT", agentsAt, "/api/projects/~/agents", fmt.Sprintf(`{"version":%v,"enable":["*"]}`, initechRoot.field("version")), http.StatusOK)
	c.call(initech, "POST", "/api/runs", "/api/runs", `{"text":"hi"}`, http.StatusConflict)
	c.call(initech, "GET", "/api/pick", "/api/pick?q=anything", "", http.StatusConflict)

	// Schedules.
	c.call(acme, "POST", "/api/schedules", "/api/schedules", `{"name":"nightly","expression":"0 3 * * *","agent":"helper","text":"Report.","project":"Launch","deliver":"inbox"}`, http.StatusCreated)
	c.call(acme, "POST", "/api/schedules", "/api/schedules", `{"name":"nightly","expression":"0 3 * * *","agent":"helper","text":"Report."}`, http.StatusConflict)
	c.call(acme, "POST", "/api/schedules", "/api/schedules", `{"name":"bad","expression":"whenever","agent":"helper","text":"Report."}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/schedules", "/api/schedules", `{"name":"lost","expression":"0 3 * * *","agent":"ghost","text":"Report."}`, http.StatusNotFound)
	c.call(acme, "POST", "/api/schedules/{name}/pause", "/api/schedules/nightly/pause", "", http.StatusOK)
	c.call(acme, "POST", "/api/schedules/{name}/resume", "/api/schedules/nightly/resume", "", http.StatusOK)
	c.call(acme, "POST", "/api/schedules/{name}/run", "/api/schedules/nightly/run", "", http.StatusAccepted)
	c.call(globex, "POST", "/api/schedules/{name}/run", "/api/schedules/nightly/run", "", http.StatusNotFound)
	c.call(acme, "PUT", "/api/schedules/{name}", "/api/schedules/nightly", `{"expression":"0 4 * * 1-5","agent":"helper","text":"Report again.","project":"Launch","description":"Weekday report"}`, http.StatusOK)
	c.call(acme, "PUT", "/api/schedules/{name}", "/api/schedules/nightly", `{"expression":"whenever","agent":"helper","text":"Report."}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/schedules/{name}", "/api/schedules/ghost", `{"expression":"0 3 * * *","agent":"helper","text":"Report."}`, http.StatusNotFound)
	// One the platform file declares is changed and deleted in the file.
	if _, err := f.p.Schedules.Declare(cron.Schedule{Name: "from-file", Expression: "41 8 * * 1", Agent: "helper", Text: "Recap.", Tenant: "acme",
		Meta: map[string]string{runs.MetaProject: "Launch", platform.MetaDeliver: platform.DeliverProject}}, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if listed := c.call(acme, "GET", "/api/schedules", "/api/schedules", "", http.StatusOK); !strings.Contains(string(listed.raw), `"managed":"file"`) {
		t.Errorf("the file's schedule does not say so: %s", listed.raw)
	}
	c.call(acme, "PUT", "/api/schedules/{name}", "/api/schedules/from-file", `{"expression":"0 3 * * *","agent":"helper","text":"Mine."}`, http.StatusConflict)
	c.call(acme, "DELETE", "/api/schedules/{name}", "/api/schedules/from-file", "", http.StatusConflict)
	if err := f.p.Schedules.Release("acme", "from-file"); err != nil {
		t.Fatal(err)
	}
	c.call(acme, "DELETE", "/api/schedules/{name}", "/api/schedules/from-file", "", http.StatusNoContent)

	// Alerts, knowledge and evals.
	c.call(acme, "POST", "/api/alert-rules", "/api/alert-rules", `{"name":"any","kind":"run_failed","severity":"critical"}`, http.StatusCreated)
	c.call(acme, "POST", "/api/alert-rules", "/api/alert-rules", `{"name":"any","kind":"run_failed","severity":"critical"}`, http.StatusConflict)
	c.call(acme, "POST", "/api/alert-rules", "/api/alert-rules", `{"name":"odd","kind":"nonsense","severity":"critical"}`, http.StatusBadRequest)
	c.call(acme, "POST", "/api/alert-rules", "/api/alert-rules", `{"name":"spend","kind":"spend","project":"Launch","threshold_micros":5000000,"window_seconds":86400,"severity":"warning","notify":["role:admin"],"cooldown_seconds":3600,"description":"Launch spend"}`, http.StatusCreated)
	c.call(acme, "PUT", "/api/alert-rules/{name}", "/api/alert-rules/spend", `{"kind":"spend","threshold_micros":9000000,"window_seconds":86400,"severity":"critical","paused":true}`, http.StatusOK)
	c.call(acme, "PUT", "/api/alert-rules/{name}", "/api/alert-rules/spend", `{"kind":"spend","window_seconds":86400,"severity":"critical"}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/alert-rules/{name}", "/api/alert-rules/ghost", `{"kind":"run_failed","severity":"critical"}`, http.StatusNotFound)
	alertList := c.call(acme, "GET", "/api/alerts", "/api/alerts", "", http.StatusOK)
	if fired := sliceOf(alertList.field("alerts")); len(fired) > 0 {
		c.call(acme, "POST", "/api/alerts/{id}/ack", "/api/alerts/"+obj(fired[0])["id"].(string)+"/ack", "", http.StatusOK)
	}
	c.call(acme, "POST", "/api/alerts/{id}/ack", "/api/alerts/nope/ack", "", http.StatusNotFound)

	c.call(acme, "POST", "/api/knowledge", "/api/knowledge", `{"name":"handbook","description":"How we work."}`, http.StatusCreated)
	c.call(acme, "POST", "/api/knowledge", "/api/knowledge", `{"name":"handbook"}`, http.StatusConflict)
	c.call(acme, "GET", "/api/knowledge/{name}/documents", "/api/knowledge/handbook/documents", "", http.StatusOK)
	c.call(acme, "GET", "/api/knowledge/search", "/api/knowledge/search?q=zebra", "", http.StatusOK)
	c.call(acme, "GET", "/api/knowledge/search", "/api/knowledge/search?q=", "", http.StatusBadRequest)
	doc := c.call(acme, "POST", "/api/knowledge/{name}/documents", "/api/knowledge/handbook/documents", `{"title":"Refunds","uri":"https://example.com/refunds","text":"Refunds are issued within five days of a request."}`, http.StatusCreated)
	c.call(acme, "GET", "/api/knowledge/{name}/documents", "/api/knowledge/handbook/documents", "", http.StatusOK)
	c.call(globex, "GET", "/api/knowledge/{name}/documents", "/api/knowledge/handbook/documents", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/knowledge/search", "/api/knowledge/search?q=refunds&source=handbook", "", http.StatusOK)
	c.call(acme, "POST", "/api/knowledge/{name}/variants", "/api/knowledge/handbook/variants", `{"name":"lean"}`, http.StatusCreated)
	docPath := "/api/knowledge/handbook/documents/" + doc.str("id")
	c.call(acme, "PUT", "/api/knowledge/{name}/documents/{id}", docPath, `{"excluded":true}`, http.StatusOK)

	c.call(acme, "POST", "/api/evals", "/api/evals", `{"name":"smoke","target":"agent","subject":"helper","cases":[{"id":"says-offline","input":"hello","checks":[{"kind":"contains","value":"offline provider"}]}]}`, http.StatusCreated)
	c.call(acme, "POST", "/api/evals", "/api/evals", `{"name":"lost","target":"agent","subject":"ghost","cases":[{"id":"a","input":"hello","checks":[{"kind":"contains","value":"x"}]}]}`, http.StatusNotFound)
	c.call(acme, "PUT", "/api/evals/{name}", "/api/evals/smoke", `{"name":"smoke","description":"The basics.","target":"agent","subject":"helper","cases":[{"id":"says-offline","input":"hello","checks":[{"kind":"contains","value":"offline provider"}]}]}`, http.StatusOK)
	c.call(acme, "POST", "/api/evals/{name}/run", "/api/evals/smoke/run", "", http.StatusAccepted)
	for range 400 {
		runs := c.call(acme, "GET", "/api/evals/{name}/runs", "/api/evals/smoke/runs", "", http.StatusOK)
		if latest := sliceOf(runs.field("runs")); len(latest) > 0 && obj(latest[0])["status"] != "running" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	c.call(acme, "POST", "/api/evals/{name}/run", "/api/evals/nope/run", "", http.StatusNotFound)

	// Clients. No answer carries a secret, which clients_test.go checks.
	c.call(acme, "POST", "/api/clients", "/api/clients", slackBody("support", "A01CONTRACT", "helper"), http.StatusCreated)
	c.call(acme, "POST", "/api/clients", "/api/clients", slackBody("support", "A01CONTRACT", "helper"), http.StatusConflict)
	c.call(acme, "POST", "/api/clients", "/api/clients", `{"id":"odd","surface":"fax"}`, http.StatusBadRequest)
	// A GitHub installation names the login its app is mentioned by.
	gh := `{"id":"gh","name":"GitHub","surface":"github","identity":{"owners":["acme"]},"default_agent":"helper","github":{"login":"humagen[bot]"},` +
		`"credentials":{"token":{"value":"` + botToken + `"},"signing_secret":{"value":"` + signingSecret + `"}}}`
	if got := c.call(acme, "POST", "/api/clients", "/api/clients", gh, http.StatusCreated); obj(got.field("github"))["login"] != "humagen[bot]" {
		t.Errorf("the GitHub client's login is %s", got.raw)
	}
	c.call(acme, "POST", "/api/clients", "/api/clients", strings.Replace(strings.Replace(gh, `"id":"gh"`, `"id":"gh-two"`, 1), "humagen[bot]", "@humagen", 1), http.StatusBadRequest)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"client","name":"support"},"period":"day","max_runs":3}`, http.StatusOK)
	c.call(acme, "GET", "/api/clients/{id}", "/api/clients/support", "", http.StatusOK)
	c.call(globex, "GET", "/api/clients/{id}", "/api/clients/support", "", http.StatusNotFound)
	// A Slack app's machine records where it posted a message; nobody else can.
	app := `{"id":"app","name":"App","surface":"slack","identity":{"app_id":"A01APP","team":"T01ABCDEF"},"default_agent":"helper","slack":{"machine":"svc-acme"},` +
		`"credentials":{"token":{"value":"` + botToken + `"},"signing_secret":{"value":"` + signingSecret + `"}}}`
	c.call(acme, "POST", "/api/clients", "/api/clients", app, http.StatusCreated)
	deliveries := "/api/inbox/{id}/deliveries/{client}"
	c.call(acme, "PUT", deliveries, inboxPath+"/deliveries/app", `{"channel":"C0123ABC","ts":"1700000000.000100"}`, http.StatusNoContent)
	c.call(acme, "PUT", deliveries, inboxPath+"/deliveries/app", `{"channel":"","ts":"1700000000.000100"}`, http.StatusBadRequest)
	c.call(acme, "PUT", deliveries, inboxPath+"/deliveries/support", `{"channel":"C0123ABC","ts":"1700000000.000100"}`, http.StatusNotFound)
	if got := c.call(acme, "GET", "/api/inbox/{id}", inboxPath, "", http.StatusOK); obj(obj(obj(got.field("message"))["deliveries"])["app"])["ts"] != "1700000000.000100" {
		t.Errorf("the machine does not see its own receipt: %s", got.raw)
	}
	// Where the app files the threads it starts outside a bound channel.
	homed := strings.Replace(strings.Replace(app, `"slack":`, `"home":{"project":"Launch"},"slack":`, 1), `"machine":"svc-acme"`, `"machine":"svc-acme","people":{"U01ANA":"00u-ana"}`, 1)
	c.call(acme, "PUT", "/api/clients/{id}", "/api/clients/app", homed, http.StatusOK)
	c.call(acme, "PUT", "/api/clients/{id}", "/api/clients/app", strings.Replace(homed, "Launch", "Ghost", 1), http.StatusBadRequest)
	c.call(acme, "PUT", "/api/clients/{id}", "/api/clients/app", strings.Replace(homed, `"project":"Launch"`, `"project":"Launch","channel":"nowhere"`, 1), http.StatusBadRequest)
	// The machine reads a mapped person's inbox for them, and nobody else's.
	// Here the policy grants ana nothing by subject, so she reads nothing
	// through Slack; platform tests cover a person who may.
	c.call(acme, "GET", "/api/inbox/count", "/api/inbox/count?act_as_client=app&act_as_user=U01ANA", "", http.StatusForbidden)
	c.call(acme, "GET", "/api/inbox", "/api/inbox?act_as_client=app&act_as_user=U01NOBODY", "", http.StatusForbidden)
	c.call(acme, "GET", "/api/inbox/count", "/api/inbox/count?act_as_client=app", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/inbox/{id}", inboxPath+"?act_as_client=app", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/inbox/count", "/api/inbox/count?act_as_client=support&act_as_user=U01ANA", "", http.StatusNotFound)
	// And lists her projects the same way: acting_projects_test.go covers a
	// person who may.
	c.call(acme, "GET", "/api/projects", "/api/projects?act_as_client=app&act_as_user=U01ANA", "", http.StatusForbidden)
	c.call(acme, "GET", "/api/projects", "/api/projects?act_as_user=U01ANA", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/projects", "/api/projects?act_as_client=support&act_as_user=U01ANA", "", http.StatusNotFound)
	c.call(acme, "PUT", "/api/clients/{id}", "/api/clients/support", `{"name":"Support","surface":"slack","identity":{"app_id":"A01CONTRACT","team":"T01ABCDEF"},"default_agent":"helper","agents":["*"],"slack":{"sessions":true},"enabled":false}`, http.StatusOK)

	// Channels, experts and shared threads. A person posts; a credential
	// posts only for a person it acts for. From here Ana signs in as a
	// person the directory knows by her handle.
	ana = f.person(t, "acme", "00u-ana", "Ana", "humagen-admins")
	vic := f.person(t, "acme", "00u-vic", "Vic", "humagen-viewers")
	bo := f.person(t, "acme", "00u-bo", "Bo", "humagen-admins")
	channels := "/api/projects/{name}/channels"
	c.call(acme, "GET", channels, "/api/projects/Field/channels", "", http.StatusOK)
	c.call(acme, "GET", channels, "/api/projects/~/channels", "", http.StatusOK)
	c.call(acme, "POST", channels, "/api/projects/Field/channels", `{"name":"renewals","title":"Renewals","purpose":"Renewal work","default":"helper"}`, http.StatusCreated)
	c.call(acme, "POST", channels, "/api/projects/Field/channels", `{"name":"renewals"}`, http.StatusConflict)
	c.call(acme, "POST", channels, "/api/projects/Field/channels", `{"name":"Not A Slug"}`, http.StatusBadRequest)
	c.call(acme, "POST", channels, "/api/projects/Field/channels", `{"name":"ghosts","default":"ghost"}`, http.StatusBadRequest)
	channel := "/api/projects/{name}/channels/{channel}"
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/renewals", `{"version":1,"title":"Renewals","purpose":"Renewals and pricing","default":"helper"}`, http.StatusOK)
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/renewals", `{"version":1,"purpose":"stale"}`, http.StatusConflict)
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/renewals", `{"version":2,"default":"ghost"}`, http.StatusBadRequest)
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/nowhere", `{"version":1}`, http.StatusNotFound)
	c.call(acme, "DELETE", channel, "/api/projects/Field/channels/general", "", http.StatusConflict)
	// A channel bound to a Slack channel of the app: one Slack channel, one channel.
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/renewals", `{"version":2,"title":"Renewals","purpose":"Renewals and pricing","default":"helper","slack":{"client":"app","channel":"C01RENEW"}}`, http.StatusOK)
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/general", `{"version":0,"slack":{"client":"app","channel":"C01RENEW"}}`, http.StatusConflict)
	c.call(acme, "PUT", channel, "/api/projects/Field/channels/general", `{"version":0,"slack":{"client":"ghost","channel":"C01GEN"}}`, http.StatusBadRequest)
	bindings := c.call(acme, "GET", "/api/clients/{id}/bindings", "/api/clients/app/bindings", "", http.StatusOK)
	if b := sliceOf(bindings.field("bindings")); len(b) != 1 || obj(b[0])["channel"] != "renewals" || obj(b[0])["bound_at"] == "" || obj(b[0])["bound_at"] == nil {
		t.Errorf("the app's bindings are %s, each saying when it was bound", bindings.raw)
	}
	c.call(globex, "GET", "/api/clients/{id}/bindings", "/api/clients/app/bindings", "", http.StatusNotFound)
	c.call(acme, "POST", "/api/projects", "/api/projects", `{"name":"Crowded"}`, http.StatusCreated)
	for i := range projects.MaxChannels - 1 {
		c.call(acme, "POST", channels, "/api/projects/Crowded/channels", fmt.Sprintf(`{"name":"c%d"}`, i), http.StatusCreated)
	}
	c.call(acme, "POST", channels, "/api/projects/Crowded/channels", `{"name":"one-more"}`, http.StatusTooManyRequests)
	c.call(acme, "DELETE", channel, "/api/projects/Crowded/channels/c0", "", http.StatusNoContent)
	experts := "/api/projects/{name}/experts"
	got := c.call(acme, "GET", experts, "/api/projects/Field/experts", "", http.StatusOK)
	expertsAt := version(got)
	c.call(acme, "PUT", experts, "/api/projects/Field/experts", fmt.Sprintf(`{"version":%d,"experts":[{"who":"00u-vic","topics":["billing"]}]}`, expertsAt), http.StatusOK)
	c.call(acme, "PUT", experts, "/api/projects/Field/experts", fmt.Sprintf(`{"version":%d,"experts":[]}`, expertsAt), http.StatusConflict)
	c.call(acme, "PUT", experts, "/api/projects/Field/experts", fmt.Sprintf(`{"version":%d,"experts":[{"who":"00u-vic","topics":["Not a topic"]}]}`, expertsAt+1), http.StatusBadRequest)
	// The knowledge sources a project names ground every run filed there
	// and below; each must be one the workspace has.
	c.call(acme, "GET", "/api/projects/{name}/context", "/api/projects/Field/context?agent=helper", "", http.StatusOK)
	c.call(acme, "GET", "/api/projects/{name}/context", "/api/projects/Field/context", "", http.StatusBadRequest)
	know := "/api/projects/{name}/knowledge"
	got = c.call(acme, "GET", know, "/api/projects/Field/knowledge", "", http.StatusOK)
	knownAt := version(got)
	got = c.call(acme, "PUT", know, "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%d,"knowledge":["handbook"]}`, knownAt), http.StatusOK)
	if g := sliceOf(got.field("given")); len(g) != 1 || obj(g[0])["name"] != "handbook" || obj(g[0])["from"] != "Field" {
		t.Errorf("Field's knowledge is %s", got.raw)
	}
	c.call(acme, "PUT", know, "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%d,"knowledge":[]}`, knownAt), http.StatusConflict)
	c.call(acme, "PUT", know, "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%d,"knowledge":["ghost"]}`, knownAt+1), http.StatusBadRequest)
	c.call(acme, "PUT", know, "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%d}`, knownAt+1), http.StatusBadRequest)
	// The skills a project declares are what a message posted there and
	// below can name; GET /api/skills lists them without instructions, and
	// GET /api/mentions lists the mention kinds and completes one.
	sk := "/api/projects/{name}/skills"
	got = c.call(acme, "GET", sk, "/api/projects/Field/skills", "", http.StatusOK)
	skillsAt := version(got)
	review := `{"name":"review","description":"Review a renewal.","argument_hint":"[account]","instructions":"Check the terms."}`
	got = c.call(acme, "PUT", sk, "/api/projects/Field/skills", fmt.Sprintf(`{"version":%d,"skills":[%s]}`, skillsAt, review), http.StatusOK)
	if o := sliceOf(got.field("offered")); len(o) != 1 || obj(o[0])["name"] != "review" || obj(o[0])["from"] != "Field" {
		t.Errorf("Field's skills are %s", got.raw)
	}
	c.call(acme, "PUT", sk, "/api/projects/Field/skills", fmt.Sprintf(`{"version":%d,"skills":[]}`, skillsAt), http.StatusConflict)
	c.call(acme, "PUT", sk, "/api/projects/Field/skills", fmt.Sprintf(`{"version":%d,"skills":[%s,%s]}`, skillsAt+1, review, review), http.StatusBadRequest)
	c.call(acme, "PUT", sk, "/api/projects/Field/skills", fmt.Sprintf(`{"version":%d}`, skillsAt+1), http.StatusBadRequest)
	got = c.call(acme, "GET", "/api/skills", "/api/skills?project=Field", "", http.StatusOK)
	if l := sliceOf(got.field("skills")); len(l) != 2 || obj(l[0])["instructions"] != nil || obj(l[1])["name"] != "library-brief" {
		t.Errorf("the skills in Field are %s", got.raw)
	}
	c.call(acme, "GET", "/api/skills", "/api/skills", "", http.StatusOK)
	c.call(acme, "GET", "/api/skills", "/api/skills?project=Nowhere", "", http.StatusNotFound)
	got = c.call(acme, "GET", "/api/mentions", "/api/mentions?project=Field", "", http.StatusOK)
	if k := sliceOf(got.field("kinds")); len(k) != 1 || obj(k[0])["kind"] != "skill" {
		t.Errorf("the mention kinds are %s", got.raw)
	}
	got = c.call(acme, "GET", "/api/mentions", "/api/mentions?project=Field&kind=skill&q=rev", "", http.StatusOK)
	if i := sliceOf(got.field("items")); len(i) != 1 || obj(i[0])["name"] != "review" {
		t.Errorf("completing skill:rev is %s", got.raw)
	}
	c.call(acme, "GET", "/api/mentions", "/api/mentions?project=Field&kind=team", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/mentions", "/api/mentions?project=Field&q=rev", "", http.StatusBadRequest)
	// A message that names a skill nobody declares is refused.
	c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Field","visibility":"private","text":"$ghost please"}`, http.StatusBadRequest)

	c.call(acme, "POST", "/api/threads", "/api/threads", `{"project":"Field","visibility":"shared","text":"hi"}`, http.StatusForbidden)
	// A conversation the console starts is private, and an untagged
	// message in it starts nothing.
	draft := c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Field","text":"hi"}`, http.StatusCreated)
	if th := obj(draft.field("thread")); th["visibility"] != "private" || draft.str("rule") != "untagged" || len(sliceOf(draft.field("runs"))) != 0 {
		t.Errorf("a conversation started with no visibility is %s", draft.raw)
	}
	c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Field","visibility":"private","text":"hi"}`, http.StatusCreated)
	c.call(auditAdmin, "GET", "/api/threads/{id}", "/api/threads/"+obj(draft.field("thread"))["id"].(string), "", http.StatusOK)
	auditRows := c.call(auditAdmin, "GET", "/api/read-audit", "/api/read-audit", "", http.StatusOK)
	c.call(auditAdmin, "GET", "/api/audit", "/api/audit", "", http.StatusOK)
	c.call(auditAdmin, "GET", "/api/me/notifications", "/api/me/notifications", "", http.StatusOK)
	c.call(auditAdmin, "GET", "/api/audit", "/api/audit?limit=501", "", http.StatusBadRequest)
	if len(sliceOf(auditRows.field("records"))) == 0 {
		t.Fatal("governance-only private read was not in the audit")
	}

	c.call(ana, "GET", "/api/discovery", "/api/discovery?q=Field&limit=5", "", http.StatusOK)
	c.call(ana, "GET", "/api/discovery", "/api/discovery?limit=51", "", http.StatusBadRequest)
	c.call(ana, "GET", "/api/discovery", "/api/discovery?kinds=unknown", "", http.StatusBadRequest)
	c.call(ana, "GET", "/api/discovery", "/api/discovery?project=UnknownProject", "", http.StatusNotFound)
	c.call(ana, "GET", "/api/projects/{name}/rollup", "/api/projects/Field/rollup", "", http.StatusOK)
	templates := c.call(ana, "GET", "/api/project-templates", "/api/project-templates", "", http.StatusOK)
	if len(sliceOf(templates.field("templates"))) != 3 {
		t.Fatalf("template catalog: %s", templates.raw)
	}
	createdInitiative := c.call(ana, "POST", "/api/projects", "/api/projects", `{"name":"Strategy","template":"initiative"}`, http.StatusCreated)
	if createdInitiative.field("initiative") != true || createdInitiative.str("template") != "initiative" {
		t.Fatalf("initiative marker missing: %s", createdInitiative.raw)
	}
	c.call(ana, "POST", "/api/projects", "/api/projects", `{"name":"UnknownTemplate","template":"ghost"}`, http.StatusBadRequest)
	copiedProject := c.call(ana, "POST", "/api/projects/{name}/fork", "/api/projects/Field/fork", `{"name":"Forked"}`, http.StatusAccepted)
	if copiedProject.str("state") != "ready" {
		t.Fatalf("project fork did not publish: %s", copiedProject.raw)
	}
	c.call(ana, "GET", "/api/project-forks/{name}", "/api/project-forks/Forked", "", http.StatusOK)
	c.call(auditAdmin, "GET", "/api/project-forks/{name}", "/api/project-forks/Forked", "", http.StatusNotFound)
	c.call(ana, "POST", "/api/projects/{name}/fork", "/api/projects/Field/fork", `{"name":"Launch"}`, http.StatusConflict)
	c.call(ana, "POST", "/api/projects/{name}/fork", "/api/projects/Field/fork", `{"name":""}`, http.StatusBadRequest)
	forkPath := "/api/threads/" + obj(draft.field("thread"))["id"].(string) + "/fork"
	forked := c.call(ana, "POST", "/api/threads/{id}/fork", forkPath, `{"through":"m-1","title":"Alternative"}`, http.StatusCreated)
	c.call(ana, "POST", "/api/threads/{id}/fork", forkPath, `{"through":"m-999"}`, http.StatusNotFound)
	c.call(ana, "POST", "/api/threads/{id}/bring-back", "/api/threads/"+forked.str("id")+"/bring-back", `{"run":"r00000000000000000000000000000000"}`, http.StatusNotFound)
	forkAnswer := c.call(ana, "POST", "/api/threads/{id}/messages", "/api/threads/"+forked.str("id")+"/messages", `{"text":"@helper alternative"}`, http.StatusCreated)
	forkRun := obj(sliceOf(forkAnswer.field("runs"))[0])["id"].(string)
	c.settled(ana, forkRun)
	c.call(ana, "POST", "/api/threads/{id}/bring-back", "/api/threads/"+forked.str("id")+"/bring-back", `{"run":"`+forkRun+`","text":"Alternative result"}`, http.StatusCreated)

	c.call(ana, "GET", "/api/find-help", "/api/find-help?q=renewal", "", http.StatusOK)
	c.call(ana, "GET", "/api/find-help", "/api/find-help", "", http.StatusBadRequest)
	placementRoute := "/api/threads/{id}/placement"
	draftID := obj(draft.field("thread"))["id"].(string)
	previewed := c.call(ana, "GET", placementRoute, "/api/threads/"+draftID+"/placement?project=Field&channel=renewals", "", http.StatusOK)
	publicationRoute := "/api/threads/{id}/publication"
	publicationPath := "/api/threads/" + draftID + "/publication"
	summaryPublished := c.call(ana, "POST", publicationRoute, publicationPath, fmt.Sprintf(`{"project":"Field","channel":"renewals","summary":"Reviewed summary for the project","placement_token":%q}`, previewed.str("token")), http.StatusCreated)
	if summaryPublished.str("id") == draftID || strings.Contains(string(summaryPublished.raw), draftID) {
		t.Fatal("summary publication copied private source linkage")
	}
	c.call(ana, "POST", publicationRoute, publicationPath, `{"project":"Field","summary":"reviewed","placement_token":"stale"}`, http.StatusConflict)
	c.call(ana, "POST", publicationRoute, publicationPath, `{"project":"Field","summary":"","placement_token":"stale"}`, http.StatusConflict)
	c.call(ana, "POST", publicationRoute, publicationPath, fmt.Sprintf(`{"project":"Field","channel":"renewals","summary":"","placement_token":%q}`, previewed.str("token")), http.StatusBadRequest)
	c.call(ana, "GET", placementRoute, "/api/threads/"+draftID+"/placement", "", http.StatusBadRequest)
	activeDraft := c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Field","text":"preview only"}`, http.StatusCreated)
	activeID := obj(activeDraft.field("thread"))["id"].(string)
	activeTag := envelope.Tag{ID: "placement-active", Tenant: "acme", Agent: "helper", Origin: runs.OriginAPI, Text: "active", Meta: map[string]string{runs.MetaProject: "Field", runs.MetaThread: activeID}, At: time.Now()}
	if err := c.f.p.Runs.Record(runtime.RunID("acme", activeTag.ID), activeTag, 1); err != nil {
		t.Fatal(err)
	}
	c.call(ana, "GET", placementRoute, "/api/threads/"+activeID+"/placement?project=Field&channel=renewals", "", http.StatusConflict)

	share, draftPath := "/api/threads/{id}/share", "/api/threads/"+obj(draft.field("thread"))["id"].(string)+"/share"
	c.call(ana, "POST", share, draftPath, `{"add":["nobody"]}`, http.StatusBadRequest)
	sharedDraft := c.call(ana, "POST", share, draftPath, `{"channel":"renewals"}`, http.StatusOK)
	if th := obj(sharedDraft.field("thread")); th["visibility"] != "shared" || th["channel"] != "renewals" || th["project"] != "Field" {
		t.Errorf("shared, the conversation is %s", sharedDraft.raw)
	}
	c.call(acme, "POST", share, draftPath, `{}`, http.StatusForbidden)
	c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Ghost","visibility":"shared","text":"hi"}`, http.StatusNotFound)
	// The app's machine posts a Slack person's message from a bound channel,
	// as that person, who must hold runs:write themselves: here the policy
	// grants ana nothing by subject (platform tests post as one who may).
	surface := `{"text":"<@UAPP> who has the PO?","act_as":{"client":"app","user":"U01ANA"},"surface":{"client":"app","channel":"C01RENEW","ts":"1700000000.000200","app_user":"UAPP"}}`
	c.call(acme, "POST", "/api/threads", "/api/threads", surface, http.StatusForbidden)
	c.call(ana, "POST", "/api/threads", "/api/threads", surface, http.StatusNotFound)
	c.call(acme, "POST", "/api/threads", "/api/threads", strings.Replace(surface, `"surface"`, `"project":"Field","surface"`, 1), http.StatusBadRequest)
	// A reply under the post of an announcement since withdrawn starts
	// nothing, in a Slack channel bound to the announcement's project.
	c.call(acme, "PUT", deliveries, "/api/inbox/"+news.str("id")+"/deliveries/app", `{"channel":"C01RENEW","ts":"1700000000.000300"}`, http.StatusNoContent)
	c.call(acme, "POST", "/api/threads", "/api/threads", `{"text":"<@UAPP> still on?","act_as":{"client":"app","user":"U01ANA"},"surface":{"client":"app","channel":"C01RENEW","ts":"1700000000.000300","message":"1700000000.000400","app_user":"UAPP"}}`, http.StatusGone)
	started := c.call(ana, "POST", "/api/threads", "/api/threads", `{"project":"Field","channel":"renewals","visibility":"shared","title":"Globex renewal","text":"@helper when does Globex renew? @vic @sme:billing"}`, http.StatusCreated)
	tid, _ := obj(started.field("thread"))["id"].(string)
	if started.str("rule") != "tagged" || len(sliceOf(started.field("runs"))) != 1 || len(sliceOf(started.field("notified"))) != 1 {
		t.Errorf("the first message did %s", started.raw)
	}
	if rs := sliceOf(started.field("runs")); len(rs) > 0 {
		c.settled(acme, obj(rs[0])["id"].(string))
	}
	threadPath := "/api/threads/" + tid
	// A conversation that has runs stays in its project.
	c.call(ana, "POST", share, threadPath+"/share", `{"project":"~"}`, http.StatusConflict)
	c.call(acme, "GET", "/api/threads", "/api/threads?project=Field&channel=renewals", "", http.StatusOK)
	c.call(acme, "GET", "/api/threads", "/api/threads?channel=Not+A+Slug", "", http.StatusBadRequest)
	turnPage := c.call(acme, "GET", "/api/threads/{id}", threadPath+"?limit=1", "", http.StatusOK)
	if cur, _ := turnPage.field("next").(string); cur == "" {
		t.Errorf("a page of one turn of two has no cursor: %s", turnPage.raw)
	} else {
		c.call(acme, "GET", "/api/threads/{id}", threadPath+"?before="+cur, "", http.StatusOK)
	}
	c.call(acme, "GET", "/api/threads/{id}", threadPath+"?before=nonsense", "", http.StatusBadRequest)
	messages := "/api/threads/{id}/messages"
	c.call(acme, "POST", messages, threadPath+"/messages", `{"text":"as myself"}`, http.StatusForbidden)
	c.call(ana, "POST", messages, threadPath+"/messages", `{"text":"noted","post_only":true}`, http.StatusCreated)
	c.call(ana, "POST", messages, threadPath+"/messages", `{"text":"x","after":"m-99"}`, http.StatusBadRequest)
	c.call(ana, "POST", messages, "/api/threads/th-0000000000000000/messages", `{"text":"x"}`, http.StatusNotFound)
	c.call(ana, "PUT", "/api/threads/{id}", threadPath, `{"version":1,"title":"Globex renewal","visibility":"shared","default":"helper"}`, http.StatusOK)
	c.call(ana, "PUT", "/api/threads/{id}", threadPath, `{"version":1,"title":"stale"}`, http.StatusConflict)
	c.call(ana, "PUT", "/api/threads/{id}", threadPath, `{"version":2,"default":"ghost"}`, http.StatusBadRequest)
	participants := "/api/threads/{id}/participants"
	c.call(ana, "PUT", participants, threadPath+"/participants", `{"version":2,"add":[{"kind":"person","name":"vic","at":"2026-01-01T00:00:00Z"}]}`, http.StatusOK)
	c.call(ana, "PUT", participants, threadPath+"/participants", `{"version":2,"remove":["vic"]}`, http.StatusConflict)
	c.call(ana, "PUT", participants, threadPath+"/participants", `{"version":3,"add":[{"kind":"person","name":"nobody","at":"2026-01-01T00:00:00Z"}]}`, http.StatusBadRequest)
	// A person's messages start no run, so a rate holds them: at most
	// PostsPerPerson a minute, in any thread, a new one included.
	quick := f.person(t, "acme", "00u-quick", "Quick", "humagen-admins")
	for range platform.PostsPerPerson {
		c.call(quick, "POST", messages, threadPath+"/messages", `{"text":"again","post_only":true}`, http.StatusCreated)
	}
	c.call(quick, "POST", messages, threadPath+"/messages", `{"text":"once more","post_only":true}`, http.StatusTooManyRequests)
	c.call(quick, "POST", "/api/threads", "/api/threads", `{"project":"Field","visibility":"shared","text":"a new one"}`, http.StatusTooManyRequests)
	// A mention, read by the person it tags: replied to, set aside, and
	// refused to a viewer, who may read it but not act on it.
	c.call(ana, "POST", messages, threadPath+"/messages", `{"text":"@bo one for you"}`, http.StatusCreated)
	c.call(ana, "POST", messages, threadPath+"/messages", `{"text":"@bo and another"}`, http.StatusCreated)
	partPath := func(m any) string {
		msg := obj(m)
		return "/api/inbox/" + fmt.Sprint(msg["id"]) + "/parts/" + fmt.Sprint(obj(sliceOf(msg["parts"])[0])["id"]) + "/resolve"
	}
	if ms := sliceOf(c.call(bo, "GET", "/api/inbox", "/api/inbox?kind=mention", "", http.StatusOK).field("messages")); len(ms) != 2 {
		t.Errorf("bo holds %d mentions, want 2", len(ms))
	} else {
		c.call(bo, "POST", resolve, partPath(ms[0]), `{"action":"reply","text":"On it."}`, http.StatusOK)
		c.call(bo, "POST", resolve, partPath(ms[1]), `{"action":"done"}`, http.StatusOK)
		c.call(bo, "POST", resolve, partPath(ms[1]), `{"action":"approve"}`, http.StatusBadRequest)
	}
	if ms := sliceOf(c.call(vic, "GET", "/api/inbox", "/api/inbox?kind=mention", "", http.StatusOK).field("messages")); len(ms) != 1 {
		t.Errorf("vic holds %d mentions, want 1", len(ms))
	} else {
		c.call(vic, "POST", resolve, partPath(ms[0]), `{"action":"done"}`, http.StatusForbidden)
	}
	// A thread holds 5,000 messages, and then is full.
	for {
		_, _, err := f.p.Threads.Post("acme", tid, threads.Message{By: "00u-ana", Text: "filler"}, time.Now())
		if errors.Is(err, threads.ErrFull) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	if full := c.call(ana, "POST", messages, threadPath+"/messages", `{"text":"one more"}`, http.StatusConflict); !strings.Contains(full.str("error"), "start a new thread") {
		t.Errorf("a full thread says %s", full.raw)
	}

	// Settings and the caller's own preferences.
	settings := c.call(acme, "GET", "/api/settings", "/api/settings", "", http.StatusOK)
	c.call(acme, "PUT", "/api/settings", "/api/settings", string(settings.raw), http.StatusOK)
	c.call(acme, "PUT", "/api/settings", "/api/settings", `{"baseline":"ghost","history":"recent","history_limit":10}`, http.StatusBadRequest)
	// Projects left settings for their own routes.
	c.call(acme, "PUT", "/api/settings", "/api/settings", `{"history":"recent","history_limit":10,"projects":[]}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"display_name":"Service","theme":"dark","accent":"teal","onboarded":true}`, http.StatusOK)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"theme":"neon"}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"display_name":"Service","theme":"dark","accent":"teal","muted":["Launch"]}`, http.StatusOK)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"display_name":"Service","theme":"dark","accent":"teal","muted":["~"]}`, http.StatusBadRequest)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"display_name":"Service","theme":"dark","accent":"teal","muted":["Ghost"]}`, http.StatusNotFound)
	c.call(acme, "PUT", "/api/preferences", "/api/preferences", `{"display_name":"Service","theme":"dark","accent":"teal","muted":[]}`, http.StatusOK)
	c.call(acme, "GET", "/api/preferences/photo", "/api/preferences/photo", "", http.StatusNotFound)
	png := "\x89PNG\r\n\x1a\n" + strings.Repeat("\x00", 32)
	c.call(acme, "PUT", "/api/preferences/photo", "/api/preferences/photo", png, http.StatusOK)
	if photo := c.call(acme, "GET", "/api/preferences/photo", "/api/preferences/photo", "", http.StatusOK); photo.header.Get("Content-Type") != "image/png" {
		t.Errorf("the picture is served as %q", photo.header.Get("Content-Type"))
	}
	c.call(acme, "PUT", "/api/preferences/photo", "/api/preferences/photo", "not a picture", http.StatusBadRequest)
	c.call(acme, "DELETE", "/api/preferences/photo", "/api/preferences/photo", "", http.StatusOK)

	// Published files.
	if err := f.p.PutFile("acme", first.str("thread"), "notes.txt", "Refunds fell by a third."); err != nil {
		t.Fatal(err)
	}
	published, err := f.p.Publish(t.Context(), platform.Publication{
		Tenant: "acme", Thread: first.str("thread"), Run: id, Agent: "helper", Project: "Launch", By: "agent:helper", Path: "notes.txt",
	})
	if err != nil {
		t.Fatal(err)
	}
	artifactPath := "/api/artifacts/" + published.ID
	c.call(acme, "GET", "/api/artifacts", "/api/artifacts?thread="+first.str("thread")+"&limit=5", "", http.StatusOK)
	c.call(acme, "GET", "/api/artifacts", "/api/artifacts?kind=nonsense", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/artifacts/{id}", artifactPath, "", http.StatusOK)
	c.call(globex, "GET", "/api/artifacts/{id}", artifactPath, "", http.StatusNotFound)
	c.call(acme, "GET", "/api/artifacts/{id}/versions", artifactPath+"/versions", "", http.StatusOK)
	if file := c.call(acme, "GET", "/api/artifacts/{id}/content", artifactPath+"/content", "", http.StatusOK); file.header.Get("Content-Type") != "application/octet-stream" || string(file.raw) != "Refunds fell by a third." {
		t.Errorf("the download is %q as %q", file.raw, file.header.Get("Content-Type"))
	}
	c.call(globex, "GET", "/api/artifacts/{id}/content", artifactPath+"/content", "", http.StatusNotFound)

	// The busy workspace, read end to end, then taken apart.
	c.call(acme, "GET", "/api/analytics", "/api/analytics?window=24h&bucket=1h", "", http.StatusOK)
	c.call(acme, "GET", "/api/analytics", "/api/analytics?window=9999h", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/journal", "/api/journal?run="+id+"&kind=lifecycle&limit=5", "", http.StatusOK)
	c.sweep(acme)

	for _, op := range c.spec.ops {
		// One that names an object is asked below, of an object made here.
		if !refusesUnknownFields(op) || len(pathParams(op.path)) > 0 {
			continue
		}
		caller := acme
		if strings.HasPrefix(op.path, "/api/me/memory") {
			caller = auditAdmin
		}
		c.call(caller, op.method, op.path, op.path, `{"unknown_field":1}`, http.StatusBadRequest)
	}

	// The rest, one by one.
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=nonsense", "", http.StatusNotFound)
	c.call(acme, "GET", "/api/journal", "/api/journal?window=forever", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/knowledge/search", "/api/knowledge/search?q=refunds&source=ghost", "", http.StatusNotFound)
	c.call(globex, "GET", "/api/pick", "/api/pick?q=anything", "", http.StatusConflict)
	c.call(acme, "POST", "/api/alert-rules", "/api/alert-rules", `{"name":"lost","kind":"run_failed","agent":"ghost","severity":"critical"}`, http.StatusNotFound)
	c.call(acme, "POST", "/api/evals", "/api/evals", `{"name":"smoke","target":"agent","subject":"helper","cases":[{"id":"a","input":"hi","checks":[{"kind":"contains","value":"x"}]}]}`, http.StatusConflict)
	c.call(globex, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_clients":1}`, http.StatusOK)
	c.call(globex, "POST", "/api/clients", "/api/clients", slackBody("one", "A01ONE", ""), http.StatusCreated)
	c.call(globex, "POST", "/api/clients", "/api/clients", slackBody("two", "A01TWO", ""), http.StatusTooManyRequests)
	// A lease over the limit is a money limit, so it is refused with 402.
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100,"max_concurrent":50,"max_lease":10}`, http.StatusOK)
	c.callFromLiveRun(acme, `{"key":"k402","agent":"helper","text":"hi","mode":"delegate","lease":1000}`, http.StatusPaymentRequired)
	// A workspace that has spent its spend limit refuses a new run with 402.
	// The offline provider prices nothing, so the spend is written to the
	// ledger as a priced call would be.
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100,"max_concurrent":50,"spend_limit":5000}`, http.StatusOK)
	if _, err := c.f.p.Usage.Append(usage.Entry{Kind: usage.Spent, Tenant: "acme", Agent: "helper", RunID: "r-spent", Key: "r-spent/a1/1",
		At: time.Now(), Spend: &usage.Spend{Micros: 5000, ModelCalls: 1, Tokens: 1000}}); err != nil {
		t.Fatal(err)
	}
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"hi"}`, http.StatusPaymentRequired)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100,"max_concurrent":50,"spend_limit":1000000}`, http.StatusOK)
	// With the agent at its day's limit, everything that would start it is
	// told so.
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"agent","name":"helper"},"period":"day","max_runs":1}`, http.StatusOK)
	c.call(acme, "POST", "/api/schedules/{name}/run", "/api/schedules/nightly/run", "", http.StatusTooManyRequests)
	c.callFromLiveRun(acme, `{"key":"k429","agent":"helper","text":"hi","mode":"delegate"}`, http.StatusTooManyRequests)
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=agent&name=helper", "", http.StatusNoContent)

	// Durable agent operations use the same contract and admission path.
	for _, path := range []string{"/api/agent-controls", "/api/agents/helper/control"} {
		template := path
		if path != "/api/agent-controls" {
			template = "/api/agents/{name}/control"
		}
		c.call(acme, "GET", template, path, "", http.StatusOK)
		c.call(acme, "PUT", template, path, `{"paused":false,"version":0}`, http.StatusOK)
		c.call(acme, "PUT", template, path, `{"paused":false,"version":0}`, http.StatusConflict)
	}
	for _, pair := range [][2]string{{"/api/action-policy", "/api/action-policy"}, {"/api/agents/{name}/action-policy", "/api/agents/helper/action-policy"}, {"/api/projects/{name}/action-policy", "/api/projects/Launch/action-policy"}, {"/api/me/action-policy", "/api/me/action-policy"}} {
		c.call(acme, "GET", pair[0], pair[1], "", http.StatusOK)
		c.call(acme, "PUT", pair[0], pair[1], `{"rules":{},"version":0}`, http.StatusOK)
		c.call(acme, "PUT", pair[0], pair[1], `{"rules":{},"version":0}`, http.StatusConflict)
	}
	assignmentBody := `{"id":"contract-assignment","agent":"helper","project":"","owner":"svc-acme","goal":"Inspect queue","done_when":"Report items","state":"requested","version":0}`
	c.call(acme, "POST", "/api/assignments", "/api/assignments", assignmentBody, http.StatusOK)
	c.call(acme, "POST", "/api/assignments", "/api/assignments", assignmentBody, http.StatusConflict)
	c.call(acme, "POST", "/api/assignments", "/api/assignments", strings.Replace(assignmentBody, `"helper"`, `"ghost"`, 1), http.StatusNotFound)
	c.call(acme, "GET", "/api/assignments", "/api/assignments?limit=201", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/handoffs", "/api/handoffs?limit=201", "", http.StatusBadRequest)
	c.call(acme, "GET", "/api/assignments/{id}", "/api/assignments/contract-assignment", "", http.StatusOK)
	c.call(acme, "PUT", "/api/assignments/{id}", "/api/assignments/contract-assignment", strings.Replace(assignmentBody, `"version":0`, `"version":1`, 1), http.StatusOK)
	c.call(acme, "PUT", "/api/assignments/{id}", "/api/assignments/contract-assignment", assignmentBody, http.StatusConflict)
	c.call(acme, "PUT", "/api/agents/{name}/control", "/api/agents/helper/control", `{"paused":true,"version":1}`, http.StatusOK)
	c.call(auditAdmin, "POST", "/api/library/skills/{id}/versions/{version}/test", "/api/library/skills/"+libraryID+"/versions/1/test", `{"agent":"helper","project":"~","text":"Paused test"}`, http.StatusConflict)

	c.call(acme, "POST", "/api/assignments/{id}/run", "/api/assignments/contract-assignment/run", "", http.StatusConflict)
	c.call(acme, "PUT", "/api/agents/{name}/control", "/api/agents/helper/control", `{"paused":false,"version":2}`, http.StatusOK)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"agent","name":"helper"},"period":"day","max_runs":1}`, http.StatusOK)
	c.call(acme, "POST", "/api/assignments/{id}/run", "/api/assignments/contract-assignment/run", "", http.StatusTooManyRequests)
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=agent&name=helper", "", http.StatusNoContent)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","spend_limit":1}`, http.StatusOK)
	c.call(acme, "POST", "/api/assignments/{id}/run", "/api/assignments/contract-assignment/run", "", http.StatusPaymentRequired)
	c.call(acme, "PUT", "/api/policies", "/api/policies", `{"scope":{"kind":"tenant"},"period":"day","max_runs":100,"max_concurrent":50,"spend_limit":1000000}`, http.StatusOK)
	c.call(acme, "POST", "/api/assignments/{id}/run", "/api/assignments/contract-assignment/run", "", http.StatusOK)
	c.call(acme, "GET", "/api/handoffs", "/api/handoffs", "", http.StatusOK)
	c.call(acme, "GET", "/api/agent-network", "/api/agent-network", "", http.StatusOK)

	// Two rules hold for every route that takes something: another
	// workspace's object is not found, and a body with a field the route
	// does not know is refused. Both are asked of every operation the spec
	// says can answer them, against the objects made above.
	ids := map[string]string{
		"name": "helper", "id": "x", "part": parts["alert"], "variant": "lean", "handle": "dana-scully", "channel": "general",
	}
	real := map[string]map[string]string{
		"/api/library/skills/{id}":                            {"id": libraryID},
		"/api/library/skills/{id}/versions/{version}/test":    {"id": libraryID, "version": "1"},
		"/api/library/skills/{id}/versions/{version}/publish": {"id": libraryID, "version": "1"},
		"/api/library/skills/{id}/versions/{version}/promote": {"id": libraryID, "version": "1"},

		"/api/projects/{name}/action-policy": {"name": "Launch"},
		"/api/assignments/{id}":              {"id": "contract-assignment"},
		"/api/assignments/{id}/run":          {"id": "contract-assignment"},
		"/api/agents/{name}/memory/{id}":     {"id": note.str("id")}, "/api/alert-rules/{name}": {"name": "any"},
		"/api/clients/{id}": {"id": "support"}, "/api/clients/{id}/bindings": {"id": "app"}, "/api/evals/{name}": {"name": "smoke"}, "/api/evals/{name}/runs": {"name": "smoke"},
		"/api/evals/{name}/run": {"name": "smoke"}, "/api/knowledge/{name}": {"name": "handbook"},
		"/api/knowledge/{name}/documents": {"name": "handbook"}, "/api/knowledge/{name}/documents/{id}": {"name": "handbook", "id": doc.str("id")},
		"/api/knowledge/{name}/variants": {"name": "handbook"}, "/api/knowledge/{name}/variants/{variant}": {"name": "handbook"},
		"/api/project-forks/{name}": {"name": "Forked"}, "/api/projects/{name}/fork": {"name": "Field"},
		"/api/projects/{name}": {"name": "Launch"}, "/api/projects/{name}/memory": {"name": "Launch"}, "/api/projects/{name}/history": {"name": "Launch"},
		"/api/projects/{name}/agents":         {"name": "Launch"},
		"/api/projects/{name}/rollup":         {"name": "Launch"},
		"/api/projects/{name}/memory/approve": {"name": "Launch"}, "/api/projects/{name}/memory/{id}": {"name": "Launch", "id": shared.str("id")},
		"/api/projects/{name}/members": {"name": "Launch"}, "/api/projects/{name}/move": {"name": "Launch"}, "/api/projects/{name}/limits": {"name": "Launch"},
		"/api/projects/{name}/inbox": {"name": "Launch"}, "/api/projects/{name}/announcements": {"name": "Launch"},
		"/api/projects/{name}/announcements/{id}": {"name": "Launch", "id": mid},
		"/api/schedules/{name}":                   {"name": "nightly"}, "/api/schedules/{name}/pause": {"name": "nightly"},
		"/api/schedules/{name}/resume": {"name": "nightly"}, "/api/schedules/{name}/run": {"name": "nightly"},
		"/api/inbox/{id}": {"id": mid}, "/api/inbox/{id}/read": {"id": mid}, "/api/inbox/{id}/dismiss": {"id": mid},
		"/api/inbox/{id}/snooze": {"id": mid}, "/api/inbox/{id}/parts/{part}/resolve": {"id": mid}, "/api/inbox/{id}/unhide": {"id": mid},
		"/api/artifacts/{id}": {"id": published.ID}, "/api/artifacts/{id}/versions": {"id": published.ID},
		"/api/artifacts/{id}/content": {"id": published.ID}, "/api/runs/{id}/calls": {"id": lead.str("id")},
		"/api/me/credentials/{provider}":      {"provider": "issues"},
		"/api/inbox/{id}/deliveries/{client}": {"id": mid, "client": "app"},
		"/api/projects/{name}/channels":       {"name": "Field"}, "/api/projects/{name}/channels/{channel}": {"name": "Field", "channel": "renewals"},
		"/api/projects/{name}/experts": {"name": "Field"}, "/api/projects/{name}/knowledge": {"name": "Field"}, "/api/projects/{name}/skills": {"name": "Field"}, "/api/threads/{id}": {"id": tid},
		"/api/threads/{id}/messages": {"id": tid}, "/api/threads/{id}/participants": {"id": tid},
		"/api/threads/{id}/share": {"id": tid},
		"/api/threads/{id}/fork":  {"id": tid}, "/api/threads/{id}/bring-back": {"id": tid},
		"/api/threads/{id}/publication": {"id": draftID},
	}
	valid := map[string]string{
		"/api/projects/{name}/fork": `{"name":"ForkedGhost"}`,
		"/api/runs/{id}/feedback":   `{"rating":"up"}`,
		"/api/agents/{name}":        revised, "/api/agents/{name}/memory": `{"text":"x"}`, "/api/agents/{name}/call-policy": `{"callers":"anyone","approval":"never"}`, "/api/agents/{name}/memory/approve": `{"ids":[]}`,
		"/api/clients/{id}": `{"name":"Support","surface":"slack","identity":{"team":"T01ABCDEF"}}`, "/api/evals/{name}": `{"name":"smoke","target":"agent","subject":"helper","cases":[]}`,
		"/api/inbox/{id}/read": `{}`, "/api/inbox/{id}/snooze": `{"until":"2999-01-01T00:00:00Z"}`,
		"/api/inbox/{id}/parts/{part}/resolve": `{"action":"acknowledge"}`, "/api/knowledge/{name}/documents": `{"title":"t","text":"words here"}`,
		"/api/knowledge/{name}/documents/{id}": `{"excluded":false}`, "/api/knowledge/{name}/variants": `{"name":"other"}`,
		"/api/projects/{name}": `{"version":1}`, "/api/projects/{name}/memory": `{"text":"x"}`, "/api/projects/{name}/memory/approve": `{"ids":[]}`,
		"/api/inbox/{id}/deliveries/{client}": `{"channel":"C0123ABC","ts":"1700000000.000100"}`, "/api/inbox/{id}/dismiss": ``, "/api/inbox/{id}/unhide": ``,
		"/api/projects/{name}/agents": `{"version":1}`, "/api/agents/{name}/home": `{"project":""}`,
		"/api/runs/{id}/calls":                       `{"key":"k9","agent":"helper","text":"hi","mode":"delegate"}`,
		"/api/runs/{id}/questions/{question}/answer": `{"action":"choose","choice":"red"}`,
		"/api/alert-rules/{name}":                    `{"kind":"run_failed","severity":"critical"}`,
		"/api/schedules/{name}":                      `{"expression":"0 3 * * *","agent":"helper","text":"Report."}`,
		"/api/me/credentials/{provider}":             `{"ref":"op://People/svc/issues"}`,
		"/api/projects/{name}/channels":              `{"name":"x"}`, "/api/projects/{name}/channels/{channel}": `{"version":1}`,
		"/api/projects/{name}/experts": `{"version":1,"experts":[]}`, "/api/projects/{name}/knowledge": `{"version":1,"knowledge":[]}`, "/api/projects/{name}/skills": `{"version":1,"skills":[]}`, "/api/threads/{id}": `{"version":1}`,
		"/api/threads/{id}/messages": `{"text":"x"}`, "/api/threads/{id}/participants": `{"version":1}`,
		"/api/threads/{id}/fork": `{"through":"m-1"}`, "/api/threads/{id}/bring-back": `{"run":"r00000000000000000000000000000000"}`,
		"/api/threads/{id}/share":      `{}`,
		"/api/projects/{name}/members": `{"version":1,"members":[{"who":"*","role":"member"}]}`, "/api/projects/{name}/move": `{"version":1,"parent":""}`,
		"/api/projects/{name}/limits": `{"period":"day","max_runs":1}`, "/api/projects/{name}/announcements": `{"subject":"Hi","text":"Hi."}`,
	}
	for _, op := range c.spec.ops {
		params := pathParams(op.path)
		if !strings.HasPrefix(op.path, "/api/") || len(params) == 0 {
			continue
		}
		path := op.path
		for _, name := range params {
			v := cmp.Or(ids[name], "x")
			if r, has := real[op.path][name]; has {
				v = r
			}
			path = strings.ReplaceAll(path, "{"+name+"}", v)
		}
		if _, documents := op.schemas["404"]; documents {
			c.call(globex, op.method, op.path, path, valid[op.path], http.StatusNotFound)
		}
		if refusesUnknownFields(op) {
			c.call(acme, op.method, op.path, path, `{"unknown_field":1}`, http.StatusBadRequest)
		}
	}

	c.call(acme, "DELETE", "/api/artifacts/{id}", artifactPath, "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/artifacts/{id}", artifactPath, "", http.StatusNotFound)
	c.call(acme, "DELETE", "/api/clients/{id}", "/api/clients/support", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/clients/{id}", "/api/clients/support", "", http.StatusNotFound)
	c.call(acme, "DELETE", "/api/evals/{name}", "/api/evals/smoke", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/knowledge/{name}/variants/{variant}", "/api/knowledge/handbook/variants/lean", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/knowledge/{name}/documents/{id}", docPath, "", http.StatusNoContent)
	// A source a project names is not deleted from under it: Field names
	// handbook, so the delete is refused, naming Field, until Field drops it.
	if inUse := c.call(acme, "DELETE", "/api/knowledge/{name}", "/api/knowledge/handbook", "", http.StatusConflict); !strings.Contains(inUse.str("error"), "Field") {
		t.Errorf("the refusal does not name the project that names the source: %s", inUse.raw)
	}
	fieldKnows := c.call(acme, "GET", know, "/api/projects/Field/knowledge", "", http.StatusOK)
	c.call(acme, "PUT", know, "/api/projects/Field/knowledge", fmt.Sprintf(`{"version":%d,"knowledge":[]}`, version(fieldKnows)), http.StatusOK)
	c.call(acme, "DELETE", "/api/knowledge/{name}", "/api/knowledge/handbook", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/alert-rules/{name}", "/api/alert-rules/any", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/schedules/{name}", "/api/schedules/nightly", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/schedules/{name}", "/api/schedules/nightly", "", http.StatusNotFound)
	// A project is deleted once nothing points at it: its limit goes first,
	// and a delete never drops a limit itself.
	if inUse := c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Launch", "", http.StatusConflict); !strings.Contains(inUse.str("error"), "its limits") {
		t.Errorf("the refusal does not name what points at the project: %s", inUse.raw)
	}
	c.call(acme, "DELETE", "/api/policies", "/api/policies?kind=project&name=Launch", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Launch", "", http.StatusNoContent)
	c.call(acme, "DELETE", "/api/projects/{name}", "/api/projects/Launch", "", http.StatusNotFound)
	// Its conversations are read, not continued.
	c.call(acme, "POST", "/api/runs", "/api/runs", `{"agent":"helper","text":"more","after":"`+inLaunch+`"}`, http.StatusConflict)
	c.call(acme, "DELETE", "/api/agents/{name}/memory/{id}", "/api/agents/ghost/memory/m1", "", http.StatusNotFound)

	// Every status code the contract documents was answered at least once,
	// or is listed in unexercised with the reason it is not.
	for key, op := range c.spec.ops {
		if !strings.HasPrefix(op.path, "/api/") || op.path == "/api/events" {
			continue
		}
		perm, _ := op.raw["x-permission"].(string)
		for code := range op.schemas {
			switch {
			case c.seen[key+" "+code], unexercised[key+" "+code] != "":
			case code == "401":
				// TestSpecPermissionsAreTheOnesTheServerEnforces sends every
				// operation without a credential.
			case code == "403" && perm != permAuthenticated:
				// The same test sends every operation with no permissions.
			case code == "413", code == "500":
				// Shared by every operation: a body over the cap, and a
				// failure on the server's side. Neither is per route.
			default:
				t.Errorf("%s documents %s and no test produced it. Exercise it, remove it, or list it in unexercised", key, code)
			}
		}
	}

	// Every JSON operation in the contract answered with its success status
	// at least once above. An operation added to the spec has to be added
	// here before this passes.
	for key, op := range c.spec.ops {
		if !strings.HasPrefix(op.path, "/api/") || op.path == "/api/events" {
			continue
		}
		ok := false
		for code := range op.schemas {
			if strings.HasPrefix(code, "2") && c.seen[key+" "+code] {
				ok = true
			}
		}
		if !ok {
			t.Errorf("%s was never checked against a real success response", key)
		}
	}
}

// unexercised lists documented status codes no test produces, each with why
// it is documented anyway. Keep it short: an entry is a code the server can
// send that is hard to provoke in a test, never a guess.
var unexercised = map[string]string{
	"POST /api/projects/{name}/fork 429":    "bounded private operation-plan capacity, covered by projectforks store tests",
	"POST /api/threads/{id}/fork 429":       "bounded workspace or project thread cap, covered by thread store tests",
	"POST /api/threads/{id}/bring-back 429": "bounded per-person posting rate and thread message cap, covered by platform and thread store tests",
	// A workspace holds at most 1,000 call policies, one per agent. Making
	// that many agents here would only test a counter, which
	// pkg/callpolicy's tests do.
	"PUT /api/agents/{name}/call-policy 429": "call policy cap",
	// The runtime here always has a bus to subscribe to and a ledger to
	// record in, so it can always carry a stop. TestAStopTheRuntimeCannotCarryIsA503
	// answers 503 from a server whose runtime cannot.
	"POST /api/runs/{id}/stop 503": "stop unavailable",
	// A client the server's configuration owns cannot be changed through the
	// API. Making one needs HUMAGEN_SLACK_* in the environment, which
	// pkg/clients and internal/platform test.
	"PUT /api/clients/{id} 409":    "managed client",
	"DELETE /api/clients/{id} 409": "managed client",
	// Busy: a second start while the first eval is still running. Its timing
	// depends on the offline provider's speed, so a test would be flaky.
	"POST /api/evals/{name}/run 409": "suite already running",
	// retry needs a failed turn a person asked, and the offline provider
	// never fails, so no test can raise one here.
	"POST /api/inbox/{id}/parts/{part}/resolve 429": "retry refused by a limit",
	"POST /api/inbox/{id}/parts/{part}/resolve 402": "retry refused by a limit",
	"POST /api/inbox/{id}/parts/{part}/resolve 409": "retry of a turn whose project was deleted",
	// A workspace remembers at most 1,000 deleted names. Filling that with
	// names that all still hold conversations takes 2,000 calls and a
	// conversation each, and would only test a counter, which pkg/projects'
	// tests do.
	"DELETE /api/projects/{name} 429": "tombstone cap",
	// A workspace holds at most 10,000 threads. Changing a conversation
	// from before threads writes its record, which that cap can refuse;
	// filling it here would only test a counter, which pkg/threads' tests do.
	"PUT /api/threads/{id} 429":              "thread cap",
	"PUT /api/threads/{id}/participants 429": "thread cap",
	// Sharing into another project counts the thread there, against the
	// 2,000 a project holds; pkg/threads' tests fill nothing so large.
	"POST /api/threads/{id}/share 429": "thread cap",
	// A spend refusal is the same answer through every route that starts a
	// run. createRun's 402 is exercised; a schedule's run now goes through
	// the same admission.
	"POST /api/schedules/{name}/run 402": "spend refusal, exercised through createRun",
	// A person holds one credential per provider and at most 64, and a
	// workspace lists at most 64 providers that take one. pkg/credentials
	// tests the cap.
	"PUT /api/me/credentials/{provider} 429": "credential cap",
}

// The event stream is server-sent events. Each message's data is validated
// against the union the spec gives the stream, and each event's payload is
// decoded from base64 and validated against the schema for its kind.
func TestEventStreamMatchesTheSpec(t *testing.T) {
	f := newFixture(t)
	s := loadSpec(t)
	event, notice, posted := s.component(t, "RunEvent"), s.component(t, "InboxNotice"), s.component(t, "ThreadNotice")
	tok := f.machine(t, "acme", everything...)
	f.do(t, nil, "POST", "/api/agents", tok, helperSpec)
	f.do(t, nil, "POST", "/api/projects", tok, `{"name":"Launch"}`)
	f.do(t, nil, "PUT", "/api/policies", tok, `{"scope":{"kind":"project","name":"Launch"},"period":"day","max_runs":1}`)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", f.srv.URL+"/api/events?topic=agent", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if ct := res.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type = %q", ct)
	}

	// One run that completes, then one a limit refuses, which raises an
	// inbox message and so a notice on the same stream.
	body := `{"agent":"helper","text":"Say hello","project":"Launch"}`
	_, run := f.do(t, nil, "POST", "/api/runs", tok, body)
	if res, _ := f.do(t, nil, "POST", "/api/runs", tok, body); res.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("the second run = %d, want 429", res.StatusCode)
	}

	// A person's message in a thread is a notice on the stream too.
	if res, out := f.do(t, nil, "POST", "/api/threads", f.person(t, "acme", "00u-ana", "Ana", "humagen-admins"), `{"visibility":"shared","text":"hello room","post_only":true}`); res.StatusCode != http.StatusCreated {
		t.Fatalf("starting a thread = %d %v", res.StatusCode, out)
	}

	kinds, notices, posts, name := map[string]int{}, 0, 0, ""
	sc := bufio.NewScanner(res.Body)
	for sc.Scan() && (kinds["lifecycle.completed"] == 0 || notices == 0 || posts == 0) {
		line := sc.Text()
		if v, isName := strings.CutPrefix(line, "event: "); isName {
			name = v
			continue
		}
		data, isData := strings.CutPrefix(line, "data: ")
		if !isData {
			if line == "" {
				name = ""
			} else if !strings.HasPrefix(line, ":") {
				t.Errorf("the stream sent a line the spec does not describe: %q", line)
			}
			continue
		}
		value, err := jsonschema.UnmarshalJSON(strings.NewReader(data))
		if err != nil {
			t.Fatalf("data is not JSON: %q", data)
		}
		switch name {
		case "":
			if err := event.Validate(value); err != nil {
				t.Errorf("an event does not match RunEvent:\n%v\ndata: %s", err, data)
			}
			if obj(value)["run_id"] == run["id"] {
				kinds[obj(value)["kind"].(string)]++
			}
		case "inbox":
			if err := notice.Validate(value); err != nil {
				t.Errorf("a notice does not match InboxNotice:\n%v\ndata: %s", err, data)
			}
			notices++
		case "thread":
			if err := posted.Validate(value); err != nil {
				t.Errorf("a notice does not match ThreadNotice:\n%v\ndata: %s", err, data)
			}
			posts++
		default:
			t.Errorf("the stream sent an event named %q, which the spec does not describe", name)
		}
	}
	for _, want := range []string{"lifecycle.accepted", "lifecycle.started", "delta.text", "lifecycle.completed"} {
		if kinds[want] == 0 {
			t.Errorf("the run's stream had no %s: %v", want, kinds)
		}
	}
	if notices == 0 {
		t.Error("the stream carried no inbox notice")
	}
	if posts == 0 {
		t.Error("the stream carried no thread notice")
	}
}

// The test's copy of the spec refuses properties a schema does not name, at
// every level and through allOf, so a key added to a handler's map or inline
// struct fails TestResponsesMatchTheSpec.
func TestClosedSchemasRejectUnknownProperties(t *testing.T) {
	s := loadSpec(t)
	me := s.ops["GET /api/me"].schemas["200"]
	good := map[string]any{"principal": map[string]any{"subject": "a", "tenant": "t", "kind": "user"}, "permissions": nil, "first_admin": false}
	if err := me.Validate(good); err != nil {
		t.Fatalf("a valid answer is refused: %v", err)
	}
	for name, bad := range map[string]map[string]any{
		"at the top":   {"principal": good["principal"], "permissions": nil, "first_admin": false, "secret": "x"},
		"in an object": {"principal": map[string]any{"subject": "a", "tenant": "t", "kind": "user", "token": "x"}, "permissions": nil, "first_admin": false},
	} {
		if me.Validate(bad) == nil {
			t.Errorf("an unknown property %s is accepted", name)
		}
	}
	event := s.component(t, "RunEvent")
	base := map[string]any{"seq": json.Number("1"), "topic": "agent:a:r", "run_id": "r", "tenant": "t", "agent": "a", "rev": json.Number("1"), "origin": "api", "at": "2026-01-01T00:00:00Z", "kind": "delta.text"}
	if err := event.Validate(base); err != nil {
		t.Fatalf("a valid event is refused: %v", err)
	}
	base["extra"] = true
	if event.Validate(base) == nil {
		t.Error("an unknown property on an allOf variant is accepted")
	}
}
