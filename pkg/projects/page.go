package projects

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Query narrows a page of projects.
type Query struct {
	// Q keeps projects whose name, title, description or customer holds it,
	// ignoring case. At most MaxQuery characters.
	Q string
	// Kind keeps projects of exactly this kind.
	Kind Kind
	// Limit is at most MaxList, DefaultList when zero.
	Limit int
	// After is the cursor a page returned as next: the page starts after it.
	After string
	// Keep, when set, keeps only the projects it accepts, such as the ones
	// a reader may see or the ones below a project.
	Keep func(Project) bool
}

// path is a project's names from the top level down to it, the root left
// out. Comparing paths name by name, a prefix first, is tree order.
func (t *Tree) path(name string) []string {
	var out []string
	for n := t.nodes[name]; !n.p.IsRoot(); n = t.nodes[n.p.Parent] {
		out = append(out, n.p.Name)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func comparePaths(a, b []string) int {
	for i := range min(len(a), len(b)) {
		if c := compareNames(a[i], b[i]); c != 0 {
			return c
		}
	}
	return len(a) - len(b)
}

func encodePath(p []string) string {
	raw, _ := json.Marshal(p)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodePath(c string) ([]string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(c)
	var p []string
	if err == nil {
		err = json.Unmarshal(raw, &p)
	}
	if err != nil || len(p) == 0 {
		return nil, fmt.Errorf("%w: not a cursor this listing gave", ErrInvalid)
	}
	for _, n := range p {
		if !ValidName(n) {
			return nil, fmt.Errorf("%w: not a cursor this listing gave", ErrInvalid)
		}
	}
	return p, nil
}

// matches reports whether a project holds q in a field people search,
// ignoring case. q is already lower case.
func (p Project) matches(q string) bool {
	if q == "" {
		return true
	}
	fields := []string{p.Name, p.Title, p.Description}
	if p.Account != nil {
		fields = append(fields, p.Account.Customer)
	}
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

// Page returns one page of projects in tree order, the root left out, and
// the cursor for the page after it, "" at the end. The cursor is where the
// last row sat in the tree, so a project deleted or added between pages
// neither breaks the paging nor shifts it. A cursor this listing did not
// give is ErrInvalid.
func (t *Tree) Page(q Query) ([]Project, string, error) {
	if utf8.RuneCountInString(q.Q) > MaxQuery {
		return nil, "", fmt.Errorf("%w: a search is at most %d characters", ErrInvalid, MaxQuery)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = DefaultList
	}
	limit = min(limit, MaxList)
	var after []string
	if q.After != "" {
		var err error
		if after, err = decodePath(q.After); err != nil {
			return nil, "", err
		}
	}
	search := strings.ToLower(q.Q)
	out := []Project{}
	var last []string
	for _, name := range t.order[1:] {
		p := t.nodes[name].p
		if q.Kind != "" && p.Kind != q.Kind || !p.matches(search) || q.Keep != nil && !q.Keep(p) {
			continue
		}
		at := t.path(name)
		if after != nil && comparePaths(at, after) <= 0 {
			continue
		}
		if len(out) == limit {
			return out, encodePath(last), nil
		}
		out = append(out, p.clone())
		last = at
	}
	return out, "", nil
}
