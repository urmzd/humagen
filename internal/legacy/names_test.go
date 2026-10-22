package legacy_test

import (
	"bytes"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The product was called warp. A name from then appears only where it is
// read on purpose, where stored data keeps it, or where a line says why.
// Anything else is a slip of the old name back in.

// wholeFiles may name warp anywhere, each for its reason.
var wholeFiles = []struct{ pattern, why string }{
	{"CHANGELOG.md", "release history, never edited"},
	{"docs/renaming.md", "the history of names"},
	{"docs/upgrading-from-warp.md", "what changed, for operators"},
	{"internal/legacy/*", "the one Go package that reads old names"},
	{"apps/*/src/legacy.ts", "the one module per app that reads old names"},
	{"deploy/compose/*", "compose identities existing stacks keep"},
	{"deploy/compose/*/*", "compose specs people edit in place"},
	{"deploy/flux/*/*", "Flux objects the example prunes by name"},
	{"deploy/flux/*/*/*", "Flux objects the example prunes by name"},
	{"deploy/helm/check.sh", "checks that a warp release keeps its names"},
	{".gitignore", "where deploy wrote before the rename"},
	{".dockerignore", "where deploy wrote before the rename"},
	{"*_test.go", "tests exercise the old names"},
	{"*.test.ts", "tests exercise the old names"},
	{"*.test.tsx", "tests exercise the old names"},
}

// keptTokens are names stored data or deployments depend on, allowed anywhere.
var keptTokens = regexp.MustCompile(strings.Join([]string{
	`(?:^|[^\w.])_?warp\.(?:thread|prev|ask|picked|project|routed|channel|message|within|sources|skills|client|call_input|deliver|hook|for|keeps_notes|place\.project|place\.thread|root|parent|depth|mode|lease|connections|github\.mutation\.v1)\b`,
	`(?:^|[^\w.])warp\.\*`,
	`"warp\."`,
	`\bwarp_state(?:_pkey)?\b`,
	`\bwarp-(?:writer|analyst|builder)\b`,
	`/var/lib/warp\b`, `/etc/warp\b`, `/usr/share/warp\b`, `/usr/local/bin/warp\b`,
	`\bwarp/workspace\b`, `\bwarp-slack\b`, `\bwarp(?:_warp)?-data\b`, `\bwarp_postgres-data\b`, `\bwarp-env\b`,
	`\b(?:config|dev)/warp\.yaml\b`,
	`\bupgrading-from-warp\b`,
	`nameOverride: warp\b`,
	`\bWARP_REF_(?:n|\d+)\b`,
}, "|"))

// fileTokens are allowed in one file or tree.
var fileTokens = []struct {
	pattern string
	re      *regexp.Regexp
	why     string
}{
	{"pkg/spec/platform.go", regexp.MustCompile(`json:"warp_(?:client|token),omitempty"`), "the strict decoder reads the old fields"},
	{"deploy/helm/humagen/*", regexp.MustCompile(`"warp"|\bwarp chart\b|Set to warp\b`), "the guard for a release the warp chart installed"},
	{"deploy/helm/humagen/*/*", regexp.MustCompile(`"warp"|\bwarp chart\b|Set to warp\b`), "the guard for a release the warp chart installed"},
	{"internal/app/deploy.go", regexp.MustCompile(`\bwarp chart\b`), "deploy keeps a warp release's names"},
	{"docs/deploy.md", regexp.MustCompile("`warp`"), "Flux object names the example keeps"},
	{"Dockerfile", regexp.MustCompile(`\.\.\. warp\b`), "the command's old name still runs it"},
	{"docs/integrations.md", regexp.MustCompile(`Warp integrations`), "the owner's 1Password item, named before the rename"},
}

// explains is a phrase that says why a name from before the rename is
// there. It excuses only a mention of the old name near it, within reach,
// never the rest of its line.
var explains = regexp.MustCompile(`(?i)before the (?:product was )?renam\w*|\bold\b|\bformerly\b|\bwas called\b|\brenamed from\b|\bwarp chart\b`)

// reach is how far, in bytes, an explanation excuses a mention of the old name.
const reach = 24

var word = regexp.MustCompile(`(?i)warp`)

// unexplained reports whether line names warp somewhere no explanation
// reaches.
func unexplained(line string) bool {
	why := explains.FindAllStringIndex(line, -1)
	for _, m := range word.FindAllStringIndex(line, -1) {
		near := false
		for _, e := range why {
			if m[1] >= e[0]-reach && m[0] <= e[1]+reach {
				near = true
				break
			}
		}
		if !near {
			return true
		}
	}
	return false
}

func TestNoNewOldName(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("git", "-C", root, "ls-files", "-z").Output()
	if err != nil {
		t.Fatalf("git ls-files: %v (the check reads the checkout's files)", err)
	}
	var bad []string
	for _, p := range strings.Split(strings.TrimRight(string(out), "\x00"), "\x00") {
		if p == "" || matchesAny(p, wholeFiles) {
			continue
		}
		if word.MatchString(p) {
			bad = append(bad, p+": the path")
		}
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil || bytes.IndexByte(b, 0) >= 0 {
			continue
		}
		for i, line := range strings.Split(string(b), "\n") {
			if !word.MatchString(line) {
				continue
			}
			rest := keptTokens.ReplaceAllString(line, "")
			for _, f := range fileTokens {
				if ok, _ := path.Match(f.pattern, p); ok {
					rest = f.re.ReplaceAllString(rest, "")
				}
			}
			if unexplained(rest) {
				bad = append(bad, p+":"+itoa(i+1)+": "+strings.TrimSpace(line))
			}
		}
	}
	if len(bad) > 0 {
		t.Errorf("the old name appears outside the places that keep it (AGENTS.md, Old names live in one place):\n%s", strings.Join(bad, "\n"))
	}
}

func matchesAny(p string, list []struct{ pattern, why string }) bool {
	for _, f := range list {
		if ok, _ := path.Match(f.pattern, p); ok {
			return true
		}
		if ok, _ := path.Match(f.pattern, path.Base(p)); ok && !strings.Contains(f.pattern, "/") {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	var b [20]byte
	i := len(b)
	for {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
		if n == 0 {
			return string(b[i:])
		}
	}
}
