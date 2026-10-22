package legacy_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/legacy"
)

// adopt runs AdoptEnv over environ and returns what it set and noted.
func adopt(environ ...string) (map[string]string, []string) {
	set := map[string]string{}
	var buf bytes.Buffer
	n := legacy.NewNotes(&buf)
	legacy.AdoptEnv(environ, func(k, v string) error { set[k] = v; return nil }, n)
	return set, n.All()
}

func TestAdoptEnv(t *testing.T) {
	for name, tc := range map[string]struct {
		environ []string
		set     map[string]string
		notes   []string
	}{
		"only the old name": {
			[]string{"WARP_POSTGRES_URL=pg"},
			map[string]string{"HUMAGEN_POSTGRES_URL": "pg"},
			[]string{"WARP_POSTGRES_URL is the old name of HUMAGEN_POSTGRES_URL; it still works, rename it"},
		},
		"a blank new name": {
			[]string{"WARP_POSTGRES_URL=pg", "HUMAGEN_POSTGRES_URL= "},
			map[string]string{"HUMAGEN_POSTGRES_URL": "pg"},
			[]string{"WARP_POSTGRES_URL is the old name of HUMAGEN_POSTGRES_URL; it still works, rename it"},
		},
		"both, equal": {
			[]string{"WARP_POSTGRES_URL=pg", "HUMAGEN_POSTGRES_URL=pg"},
			map[string]string{},
			[]string{"WARP_POSTGRES_URL is set beside HUMAGEN_POSTGRES_URL and is ignored; remove it"},
		},
		"both, different: the new one wins": {
			[]string{"WARP_POSTGRES_URL=old", "HUMAGEN_POSTGRES_URL=new"},
			map[string]string{},
			[]string{"HUMAGEN_POSTGRES_URL and WARP_POSTGRES_URL differ; using HUMAGEN_POSTGRES_URL"},
		},
		"a blank old name": {[]string{"WARP_POSTGRES_URL="}, map[string]string{}, nil},
		"a bare prefix":    {[]string{"WARP_=x"}, map[string]string{}, nil},
		// The Warp terminal's own variables are not this program's settings.
		"another program's WARP_ variables": {
			[]string{"WARP_IS_LOCAL_SHELL_SESSION=1", "WARP_USE_SSH_WRAPPER=1", "WARP_HONOR_PS1=0"},
			map[string]string{}, nil,
		},
		// References read either name; they are noted, never copied.
		"a person's credential": {
			[]string{"WARP_PERSON_ANA_ISSUES=t"},
			map[string]string{},
			[]string{"WARP_PERSON_ANA_ISSUES is read as HUMAGEN_PERSON_ANA_ISSUES; it still works, rename it (references to env:WARP_PERSON_ANA_ISSUES keep working)"},
		},
		"sorted": {
			[]string{"WARP_TEST=x", "WARP_REDIS_URL=r", "WARP_HOME=h"},
			map[string]string{"HUMAGEN_REDIS_URL": "r", "HUMAGEN_HOME": "h"},
			[]string{
				"WARP_HOME is the old name of HUMAGEN_HOME; it still works, rename it",
				"WARP_REDIS_URL is the old name of HUMAGEN_REDIS_URL; it still works, rename it",
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			set, notes := adopt(tc.environ...)
			if len(set) != len(tc.set) {
				t.Errorf("set %v, want %v", set, tc.set)
			}
			for k, v := range tc.set {
				if set[k] != v {
					t.Errorf("set %v, want %v", set, tc.set)
				}
			}
			if strings.Join(notes, "\n") != strings.Join(tc.notes, "\n") {
				t.Errorf("notes\n%s\nwant\n%s", strings.Join(notes, "\n"), strings.Join(tc.notes, "\n"))
			}
		})
	}
}

// A note names variables and never prints a value.
func TestAdoptEnvNeverPrintsAValue(t *testing.T) {
	var buf bytes.Buffer
	legacy.AdoptEnv([]string{"WARP_OIDC_CLIENT_SECRET=hunter2", "WARP_POSTGRES_URL=postgres://u:hunter2@db", "HUMAGEN_REDIS_URL=hunter2", "WARP_REDIS_URL=other"},
		func(string, string) error { return nil }, legacy.NewNotes(&buf))
	if strings.Contains(buf.String(), "hunter2") || strings.Contains(buf.String(), "other") {
		t.Fatalf("a note printed a value:\n%s", buf.String())
	}
}

// Every setting is adopted, and nothing else is one.
func TestSettingsAreKnown(t *testing.T) {
	for _, s := range legacy.Settings() {
		if !strings.HasPrefix(s, "HUMAGEN_") || !legacy.IsSetting(s) || !legacy.IsSetting(legacy.OldName(s)) {
			t.Errorf("%s is not a setting", s)
		}
	}
	if legacy.IsSetting("WARP_IS_LOCAL_SHELL_SESSION") || legacy.IsSetting("HUMAGEN_PERSON_ANA_X") {
		t.Error("IsSetting is too broad")
	}
}

func TestLookup(t *testing.T) {
	env := func(kv map[string]string) func(string) (string, bool) {
		return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		name string
		want string
		ok   bool
	}{
		"an old reference, only the new name set": {map[string]string{"HUMAGEN_X": "n"}, "WARP_X", "n", true},
		"a new reference, only the old name set":  {map[string]string{"WARP_X": "o"}, "HUMAGEN_X", "o", true},
		"an old reference, both set: new wins":    {map[string]string{"WARP_X": "o", "HUMAGEN_X": "n"}, "WARP_X", "n", true},
		"a new reference, both set: new wins":     {map[string]string{"WARP_X": "o", "HUMAGEN_X": "n"}, "HUMAGEN_X", "n", true},
		"a blank new name is unset":               {map[string]string{"WARP_X": "o", "HUMAGEN_X": " "}, "WARP_X", "o", true},
		"neither":                                 {map[string]string{}, "WARP_X", "", false},
		"another name is read as written":         {map[string]string{"ANTHROPIC_API_KEY": "k"}, "ANTHROPIC_API_KEY", "k", true},
		"another name is not mapped":              {map[string]string{"WARP_X": "o"}, "X", "", false},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := legacy.Lookup(env(tc.env), tc.name)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Lookup(%s) = %q, %v, want %q, %v", tc.name, got, ok, tc.want, tc.ok)
			}
		})
	}
	t.Setenv("HUMAGEN_LOOKUP_TEST", "n")
	if v, _ := legacy.LookupEnv("WARP_LOOKUP_TEST"); v != "n" {
		t.Errorf("LookupEnv = %q", v)
	}
}

func TestNames(t *testing.T) {
	for in, want := range map[string][2]string{
		"WARP_X":    {"HUMAGEN_X", "WARP_X"},
		"HUMAGEN_X": {"HUMAGEN_X", "WARP_X"},
		"WARP_":     {"WARP_", ""},
		"OTHER":     {"OTHER", ""},
	} {
		if got := legacy.Canonical(in); got != want[0] {
			t.Errorf("Canonical(%s) = %s", in, got)
		}
		if got := legacy.OldName(in); got != want[1] {
			t.Errorf("OldName(%s) = %s", in, got)
		}
	}
	if !legacy.SameVar("WARP_SLACK_TOKEN_DOCS", "HUMAGEN_SLACK_TOKEN_DOCS") || legacy.SameVar("WARP_A", "HUMAGEN_B") {
		t.Error("SameVar")
	}
}

// The sign-in secret is the server's own under either prefix: AdoptEnv copies
// the old one into the new.
func TestSignInSecret(t *testing.T) {
	for name, want := range map[string]bool{
		"HUMAGEN_OIDC_CLIENT_SECRET": true, "WARP_OIDC_CLIENT_SECRET": true, "HUMAGEN_OIDCX": true, "WARP_OIDC": true,
		"HUMAGEN_SLACK_TOKEN": false, "WARP_PERSON_OIDC": false, "OIDC_CLIENT_SECRET": false,
	} {
		if got := legacy.SignInSecret(name); got != want {
			t.Errorf("SignInSecret(%s) = %v", name, got)
		}
	}
}

func TestNoteNamed(t *testing.T) {
	look := func(k string) (string, bool) {
		v, ok := map[string]string{"WARP_SLACK_TOKEN_DOCS": "t", "HUMAGEN_GH": "g", "WARP_GH": "g"}[k]
		return v, ok
	}
	n := legacy.NewNotes(nil)
	legacy.NoteNamed(look, []string{"WARP_SLACK_TOKEN_DOCS", "HUMAGEN_GH", "OTHER"}, n)
	if got := n.All(); len(got) != 1 || !strings.HasPrefix(got[0], "WARP_SLACK_TOKEN_DOCS is read as HUMAGEN_SLACK_TOKEN_DOCS") {
		t.Errorf("notes %q", got)
	}
}
