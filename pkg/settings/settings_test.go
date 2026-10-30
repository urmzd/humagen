package settings

import (
	"math/rand/v2"
	"slices"
	"testing"
)

var (
	toolPool  = []string{"file_read", "file_write", "mcp:github", "mcp:jira", "publish"}
	modelPool = []string{"anthropic/a", "anthropic/b", "openai/c", "anthropic/*", "openai/*"}
	hostPool  = []string{"mcp.example.com", "tools.example.org", "x.test"}
	prefPool  = []string{"", "anthropic/a", "anthropic/b", "openai/c", "google/d", "policy/fast"}
)

func pick(r *rand.Rand, pool []string) []string {
	switch r.IntN(4) {
	case 0:
		return nil
	case 1:
		return []string{}
	}
	var out []string
	for _, x := range pool {
		if r.IntN(2) == 0 {
			out = append(out, x)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func randomLayer(r *rand.Rand) Layer {
	var l Layer
	l.Ceilings.Tools = pick(r, toolPool)
	l.Ceilings.Models = pick(r, modelPool)
	l.Ceilings.ToolServers = pick(r, hostPool)
	if r.IntN(2) == 0 {
		d := r.IntN(5)
		l.Ceilings.CallDepth = &d
	}
	l.Preferences.Model = prefPool[r.IntN(len(prefPool))]
	if r.IntN(2) == 0 {
		l.Preferences.CompactionTokens = MinCompactionTokens * (1 + r.IntN(8))
	}
	return l
}

func randomChain(r *rand.Rand) []Layer {
	chain := make([]Layer, 1+r.IntN(4))
	for i := range chain {
		chain[i] = randomLayer(r)
	}
	return chain
}

// within reports whether ceilings a allow nothing that b does not. A level's
// tool servers left out narrow nothing; only the workspace's are default
// deny, and a resolved list is never left out.
func within(a, b Ceilings) bool {
	for _, x := range toolPool {
		if a.AllowsTool(x) && !b.AllowsTool(x) {
			return false
		}
	}
	for _, x := range append(slices.Clone(prefPool[1:]), "anthropic/zzz", "openai/zzz") {
		if x == "policy/fast" {
			continue
		}
		if a.AllowsModel(x) && !b.AllowsModel(x) {
			return false
		}
	}
	for _, h := range hostPool {
		if b.ToolServers != nil && a.AllowsToolServer(h) && !b.AllowsToolServer(h) {
			return false
		}
	}
	return a.Depth(100) <= b.Depth(100)
}

// A level added anywhere on the chain never widens the ceilings, and each
// level's own ceilings hold for the run.
func TestALevelOnlyTightens(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for range 2000 {
		chain := randomChain(r)
		before := Resolve(chain[0], chain[1:]...)
		extra := randomLayer(r)
		at := 1 + r.IntN(len(chain))
		longer := slices.Insert(slices.Clone(chain), at, extra)
		after := Resolve(longer[0], longer[1:]...)
		if after.Ceilings.ToolServers == nil {
			t.Fatal("resolved tool servers left out, which would read as no narrowing")
		}
		if !within(after.Ceilings, before.Ceilings) {
			t.Fatalf("adding %+v at %d widened %+v to %+v", extra, at, before.Ceilings, after.Ceilings)
		}
		for _, l := range append(chain, extra) {
			if !within(after.Ceilings, l.Ceilings) {
				t.Fatalf("resolved %+v escapes the level %+v", after.Ceilings, l.Ceilings)
			}
		}
	}
}

// The strictest wins whatever the order the ceilings are met in.
func TestCeilingsDoNotDependOnOrder(t *testing.T) {
	r := rand.New(rand.NewPCG(3, 4))
	for range 2000 {
		chain := randomChain(r)
		shuffled := slices.Clone(chain[1:])
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		a, b := Resolve(chain[0], chain[1:]...).Ceilings, Resolve(chain[0], shuffled...).Ceilings
		if !within(a, b) || !within(b, a) {
			t.Fatalf("order changed the ceilings: %+v != %+v", a, b)
		}
	}
}

// A resolved preference is always one the ceilings allow, and it is the
// nearest level's that they allow.
func TestThePreferenceIsTheNearestAllowed(t *testing.T) {
	r := rand.New(rand.NewPCG(5, 6))
	for range 2000 {
		chain := randomChain(r)
		got := Resolve(chain[0], chain[1:]...)
		want := ""
		for i := len(chain) - 1; i >= 0; i-- {
			if m := chain[i].Preferences.Model; m != "" && got.Ceilings.AllowsModel(m) {
				want = m
				break
			}
		}
		if got.Preferences.Model != want {
			t.Fatalf("model = %q, want %q from %+v", got.Preferences.Model, want, chain)
		}
		if want != "" && !got.Ceilings.AllowsModel(want) {
			t.Fatalf("model %q outside %+v", want, got.Ceilings)
		}
	}
}

// Narrowing a resolved context never widens it, and drops a preference the
// new ceilings refuse.
func TestNarrowOnlyNarrows(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 8))
	for range 2000 {
		chain := randomChain(r)
		parent := Resolve(chain[0], chain[1:]...)
		c := randomLayer(r).Ceilings
		child := parent.Narrow(c)
		if !within(child.Ceilings, parent.Ceilings) || !within(child.Ceilings, c) {
			t.Fatalf("narrowing %+v by %+v gave %+v", parent.Ceilings, c, child.Ceilings)
		}
		if m := child.Preferences.Model; m != "" && (!child.Ceilings.AllowsModel(m) || m != parent.Preferences.Model) {
			t.Fatalf("narrowed model %q from %q under %+v", m, parent.Preferences.Model, child.Ceilings)
		}
	}
}

func TestExamples(t *testing.T) {
	two, one := 2, 1
	got := Resolve(
		Layer{Ceilings: Ceilings{Models: []string{"anthropic/*"}, CallDepth: &two, ToolServers: []string{"mcp.example.com", "x.test"}}, Preferences: Preferences{Model: "anthropic/a"}},
		Layer{Ceilings: Ceilings{Tools: []string{"file_read", "mcp:github"}, ToolServers: []string{"mcp.example.com"}}},
		Layer{Ceilings: Ceilings{Tools: []string{"mcp:github"}, CallDepth: &one}, Preferences: Preferences{Model: "openai/c", CompactionTokens: 50_000}},
	)
	if !slices.Equal(got.Ceilings.Tools, []string{"mcp:github"}) || got.Ceilings.Depth(5) != 1 {
		t.Fatalf("ceilings = %+v", got.Ceilings)
	}
	// The person's openai model is outside the workspace's ceiling, so the
	// workspace's own preference stands.
	if got.Preferences.Model != "anthropic/a" || got.Preferences.CompactionTokens != 50_000 {
		t.Fatalf("preferences = %+v", got.Preferences)
	}
	if got.Ceilings.AllowsToolServer("other.example.com") || !got.Ceilings.AllowsToolServer("mcp.example.com") {
		t.Fatal("tool servers")
	}
	// A person cannot open tool servers the workspace did not.
	if Resolve(Layer{}, Layer{Ceilings: Ceilings{ToolServers: []string{"x.test"}}}).Ceilings.AllowsToolServer("x.test") {
		t.Fatal("a level below opened tool servers")
	}
	// Nothing listed anywhere: tools and models narrow nothing, and tool
	// servers allow none.
	none := Resolve(Layer{})
	if !none.Ceilings.AllowsTool("anything") || !none.Ceilings.AllowsModel("x/y") || none.Ceilings.AllowsToolServer("mcp.example.com") || none.Ceilings.Depth(3) != 3 {
		t.Fatalf("empty chain = %+v", none)
	}
	// An empty list allows nothing.
	empty := Resolve(Layer{Ceilings: Ceilings{Tools: []string{}, Models: []string{}}})
	if empty.Ceilings.AllowsTool("file_read") || empty.Ceilings.AllowsModel("anthropic/a") {
		t.Fatalf("empty lists = %+v", empty)
	}
}

func TestCheck(t *testing.T) {
	neg, deep := -1, MaxCallDepth+1
	for name, c := range map[string]struct {
		l  Layer
		ok bool
	}{
		"zero":             {Layer{}, true},
		"full":             {Layer{Ceilings: Ceilings{Tools: []string{"a"}, Models: []string{"anthropic/*", "openai/gpt"}, ToolServers: []string{"mcp.example.com"}}, Preferences: Preferences{Model: "policy/fast", CompactionTokens: 4096}}, true},
		"model no slash":   {Layer{Ceilings: Ceilings{Models: []string{"claude"}}}, false},
		"policy ceiling":   {Layer{Ceilings: Ceilings{Models: []string{"policy/fast"}}}, false},
		"twice":            {Layer{Ceilings: Ceilings{Tools: []string{"a", "a"}}}, false},
		"host with scheme": {Layer{Ceilings: Ceilings{ToolServers: []string{"https://x"}}}, false},
		"host upper":       {Layer{Ceilings: Ceilings{ToolServers: []string{"X.test"}}}, false},
		"negative depth":   {Layer{Ceilings: Ceilings{CallDepth: &neg}}, false},
		"deep":             {Layer{Ceilings: Ceilings{CallDepth: &deep}}, false},
		"tiny compaction":  {Layer{Preferences: Preferences{CompactionTokens: 10}}, false},
		"bad model pref":   {Layer{Preferences: Preferences{Model: "claude"}}, false},
	} {
		if err := c.l.Check(); (err == nil) != c.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDeclaredOnce(t *testing.T) {
	seen := map[string]bool{}
	for _, s := range Declared {
		if seen[s.Name] || s.Kind != Ceiling && s.Kind != Preference {
			t.Fatalf("%+v", s)
		}
		seen[s.Name] = true
	}
}
