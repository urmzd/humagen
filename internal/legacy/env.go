package legacy

import (
	"os"
	"slices"
	"strings"
)

// Prefix is what every setting's name starts with. OldPrefix is what it
// started with before the rename, and is still read.
const (
	Prefix    = "HUMAGEN_"
	OldPrefix = "WARP_"
)

// settings are the variables the server reads, without the prefix. Only
// these are copied from their old names at start: other programs export
// WARP_ variables of their own (the Warp terminal's WARP_IS_LOCAL_SHELL_SESSION),
// which are none of this program's business.
var settings = []string{
	"HOME", "WEB_DIR", "SLACK_APP_DIR", "AGENT_MODEL",
	"POSTGRES_URL", "REDIS_URL", "AUTHZ_POLICY",
	"S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY",
	"OIDC_ISSUER", "OIDC_CLIENT_ID", "OIDC_CLIENT_SECRET", "OIDC_AUDIENCE",
	"SLACK_TOKEN", "SLACK_SIGNING_SECRET", "SLACK_TEAM", "SLACK_APP", "SLACK_BOTS",
	"SLACK_DEFAULT_AGENT", "SLACK_SESSIONS", "SLACK_COMMANDS",
	"GITHUB_READ_REPOSITORIES", "GITHUB_TOKEN", "GITHUB_WEBHOOK_SECRET", "GITHUB_OWNERS",
	"GITHUB_APP", "GITHUB_INSTALLATION", "GITHUB_BOTS", "GITHUB_AUTO_LABELS",
	"GITHUB_CHECKS", "GITHUB_GUIDES", "GITHUB_MUTATIONS", "GITHUB_LOGIN",
	"GITHUB_DEFAULT_AGENT",
}

// families start the names of variables a reference reads rather than a
// setting: people's credentials (PERSON_{subject}_...). They are read under
// either prefix by Lookup, so they are noted at start but never copied. The
// references deploy carries keep the name WARP_REF_n and are not noted.
var families = []string{"PERSON_"}

// Settings are the full names of the variables the server reads.
func Settings() []string {
	out := make([]string, len(settings))
	for i, s := range settings {
		out[i] = Prefix + s
	}
	return out
}

// IsSetting reports whether name is one of Settings, under either prefix.
func IsSetting(name string) bool {
	return slices.Contains(settings, strings.TrimPrefix(Canonical(name), Prefix))
}

// Canonical is name under the new prefix: WARP_X is HUMAGEN_X, and any other
// name is itself. Names are compared through it, so a grant, a start or a
// reference means the same variable whichever prefix it was written with.
func Canonical(name string) string {
	if rest, ok := strings.CutPrefix(name, OldPrefix); ok && rest != "" {
		return Prefix + rest
	}
	return name
}

// OldName is name under the old prefix: HUMAGEN_X is WARP_X. A name under
// neither prefix has no old name, and gives "".
func OldName(name string) string {
	if rest, ok := strings.CutPrefix(Canonical(name), Prefix); ok && rest != "" {
		return OldPrefix + rest
	}
	return ""
}

// SameVar reports whether a and b name one variable.
func SameVar(a, b string) bool { return Canonical(a) == Canonical(b) }

// LookupEnv is Lookup over this process's environment.
func LookupEnv(name string) (string, bool) { return Lookup(os.LookupEnv, name) }

// Getenv is LookupEnv's value.
func Getenv(name string) string {
	v, _ := LookupEnv(name)
	return v
}

// Lookup reads a variable a reference or a setting names. A name under
// either prefix reads HUMAGEN_X when it is set and not blank, else WARP_X:
// the new name wins, whichever one the reference was written with, which is
// the rule AdoptEnv's notes state. Any other name is read as written.
func Lookup(look func(string) (string, bool), name string) (string, bool) {
	if old := OldName(name); old != "" {
		if v, ok := look(Canonical(name)); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
		if v, ok := look(old); ok && strings.TrimSpace(v) != "" {
			return v, true
		}
	}
	return look(name)
}

// AdoptEnv copies each setting set only under its old name to its new one,
// with setenv, and notes each old name in use. It never unsets an old name:
// references stored before the rename name them, and child processes are
// given this environment. Blank counts as unset. Notes name variables and
// never print a value.
func AdoptEnv(environ []string, setenv func(k, v string) error, n *Notes) {
	env := map[string]string{}
	for _, kv := range environ {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	var notes []string
	for k, v := range env {
		rest, ok := strings.CutPrefix(k, OldPrefix)
		if !ok || strings.TrimSpace(v) == "" {
			continue
		}
		setting := slices.Contains(settings, rest)
		family := slices.ContainsFunc(families, func(f string) bool { return strings.HasPrefix(rest, f) && len(rest) > len(f) })
		if !setting && !family {
			continue
		}
		newName := Prefix + rest
		cur := env[newName]
		switch {
		case strings.TrimSpace(cur) == "":
			if setting {
				if setenv(newName, v) != nil {
					continue
				}
				notes = append(notes, k+" is the old name of "+newName+"; it still works, rename it")
			} else {
				notes = append(notes, k+" is read as "+newName+"; it still works, rename it (references to env:"+k+" keep working)")
			}
		case cur == v:
			notes = append(notes, k+" is set beside "+newName+" and is ignored; remove it")
		default:
			notes = append(notes, newName+" and "+k+" differ; using "+newName)
		}
	}
	slices.Sort(notes)
	for _, s := range notes {
		n.Add("%s", s)
	}
}

// NoteNamed notes each variable in names, a grant or a reference, that is
// read under its old name because its new one is not set.
func NoteNamed(look func(string) (string, bool), names []string, n *Notes) {
	var notes []string
	for _, name := range names {
		old := OldName(name)
		if old == "" {
			continue
		}
		cur, curOK := look(Canonical(name))
		was, wasOK := look(old)
		hasNew := curOK && strings.TrimSpace(cur) != ""
		hasOld := wasOK && strings.TrimSpace(was) != ""
		switch {
		case hasNew && hasOld && cur != was:
			notes = append(notes, Canonical(name)+" and "+old+" differ; using "+Canonical(name))
		case !hasNew && hasOld:
			notes = append(notes, old+" is read as "+Canonical(name)+"; it still works, rename it (references to env:"+old+" keep working)")
		}
	}
	slices.Sort(notes)
	for _, s := range notes {
		n.Add("%s", s)
	}
}

// SignInSecret reports whether name, a variable a reference names, is the
// server's own sign-in configuration, which no client or person may read:
// anything starting HUMAGEN_OIDC or WARP_OIDC. The old name is held too,
// since AdoptEnv copies it into the new one.
func SignInSecret(name string) bool {
	return strings.HasPrefix(Canonical(name), Prefix+"OIDC")
}

// EnvPrefixOf is the prefix name starts with, new or old, or "".
func EnvPrefixOf(name string) string {
	switch {
	case strings.HasPrefix(name, Prefix):
		return Prefix
	case strings.HasPrefix(name, OldPrefix):
		return OldPrefix
	}
	return ""
}
