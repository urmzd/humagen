package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/urmzd/humagen/internal/httpapi"
	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/library"
	"github.com/urmzd/humagen/pkg/state"
	"github.com/urmzd/humagen/pkg/state/statetest"
)

// generatedFile is a generated stand-in for a large schema or source file:
// exactly n bytes of text.
func generatedFile(n int, seed string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<!-- generated test fixture %s -->\n", seed)
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "<element name=\"%s-%d\"/>\n", seed, i)
	}
	return b.String()[:n]
}

// An import against the real server, on each store, drafts a skill that
// ships a supporting file as large as the library takes, with the file's
// exact bytes, and names each skill over a bound under failed: a file one
// byte over, and a package over its total.
func TestImportSkillsDraftsALargeSupportingFileAndNamesWhatIsOver(t *testing.T) {
	statetest.Each(t, func(t *testing.T, open func(*testing.T) state.Store) {
		p, err := platform.New(platform.Config{Workspace: t.TempDir(), Limits: call.Limits{MaxDepth: 2, MaxFanOut: 4}, Backends: platform.Backends{State: open(t)}})
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- p.Run(ctx) }()
		dev := auth.Principal{Subject: "dev", Tenant: "acme", Kind: auth.User, Name: "Developer", Roles: []auth.Role{auth.Admin}}
		server := httptest.NewServer((&httpapi.Server{P: p, Authn: &httpapi.Authn{Dev: &dev}, Log: slog.New(slog.DiscardHandler)}).Handler())
		t.Cleanup(func() {
			server.Close()
			cancel()
			if err := <-done; err != nil {
				t.Errorf("platform shutdown: %v", err)
			}
		})

		dir := t.TempDir()
		large := generatedFile(library.MaxPackageFileBytes, "schema")
		// schema-kit holds as many bytes as a package may.
		md := fmt.Sprintf(skillMD, "schema-kit", "schema-kit", "schema-kit")
		putFile(t, dir, "schema-kit/SKILL.md", md)
		putFile(t, dir, "schema-kit/schemas/main.xsd", large)
		putFile(t, dir, "schema-kit/scripts/convert.py", generatedFile(library.MaxPackageBytes-len(md)-len(large), "source"))
		putFile(t, dir, "oversize-file/SKILL.md", fmt.Sprintf(skillMD, "oversize-file", "oversize-file", "oversize-file"))
		putFile(t, dir, "oversize-file/schemas/main.xsd", generatedFile(library.MaxPackageFileBytes+1, "over"))
		putFile(t, dir, "oversize-package/SKILL.md", fmt.Sprintf(skillMD, "oversize-package", "oversize-package", "oversize-package"))
		for i := range 3 {
			putFile(t, dir, fmt.Sprintf("oversize-package/schemas/part%d.xsd", i), generatedFile(800<<10, fmt.Sprint("part", i)))
		}
		// many-files holds as many files as a package may, SKILL.md
		// included; too-many-files one more.
		for _, name := range []string{"many-files", "too-many-files"} {
			putFile(t, dir, name+"/SKILL.md", fmt.Sprintf(skillMD, name, name, name))
		}
		for i := range library.MaxPackageFiles - 1 {
			putFile(t, dir, fmt.Sprintf("many-files/schemas/part%03d.xsd", i), generatedFile(512, fmt.Sprint("many", i)))
		}
		for i := range library.MaxPackageFiles {
			putFile(t, dir, fmt.Sprintf("too-many-files/schemas/part%03d.xsd", i), generatedFile(512, fmt.Sprint("too many", i)))
		}

		out, err := runImport(t, "skills", dir, "--server", server.URL)
		if err == nil || err.Error() != "3 skills were not drafted" {
			t.Fatalf("err = %v\n%s", err, out)
		}
		for _, want := range []string{
			"Added 2 skills and updated 0 as drafts in the personal library; 0 already there, 0 in conflict, 3 failed.",
			fmt.Sprintf("failed too-many-files: more than %d files", library.MaxPackageFiles),
			fmt.Sprintf("failed oversize-file: main.xsd is over %d bytes", library.MaxPackageFileBytes),
			fmt.Sprintf("failed oversize-package: more than %d bytes in all", library.MaxPackageBytes),
		} {
			if !strings.Contains(out, want) {
				t.Errorf("out lacks %q:\n%s", want, out)
			}
		}
		if rows, err := p.SkillLibrary.History("acme", library.Scope{Kind: "personal", Subject: "dev"}, "many-files"); err != nil || len(rows) != 1 || len(rows[0].Files) != library.MaxPackageFiles {
			t.Fatalf("many-files = %v", err)
		}
		rows, err := p.SkillLibrary.History("acme", library.Scope{Kind: "personal", Subject: "dev"}, "schema-kit")
		if err != nil || len(rows) != 1 {
			t.Fatalf("schema-kit = %d revisions, %v", len(rows), err)
		}
		var got []byte
		for _, f := range rows[0].Files {
			if f.Path == "schemas/main.xsd" {
				got = f.Data
			}
		}
		if sha256.Sum256(got) != sha256.Sum256([]byte(large)) {
			t.Fatalf("the large file was not kept whole: %d bytes", len(got))
		}
		// The same folder again is present: the large file reads back as sent.
		out, err = runImport(t, "skills", dir, "--only", "schema-kit", "--server", server.URL)
		if err != nil || !strings.Contains(out, "Added 0 skills and updated 0 as drafts in the personal library; 1 already there, 0 in conflict, 0 failed.") {
			t.Fatalf("a second import = %v\n%s", err, out)
		}
		// Each change is one more full revision until the skill holds
		// library.MaxSkillBytes of files; the history the import reads to
		// say what changed is then the largest a skill has. The next change
		// is named under failed, and nothing is written. The store holds
		// this bound on both backends (pkg/library); this is the import's
		// side of it, run once.
		if !strings.HasSuffix(t.Name(), "/memory") {
			return
		}
		fits := library.MaxSkillBytes / library.MaxPackageBytes
		for v := 2; v <= fits+1; v++ {
			putFile(t, dir, "schema-kit/schemas/main.xsd", generatedFile(library.MaxPackageFileBytes, fmt.Sprint("schema v", v)))
			out, err = runImport(t, "skills", dir, "--only", "schema-kit", "--server", server.URL)
			if v <= fits && (err != nil || !strings.Contains(out, "Added 0 skills and updated 1 as drafts")) {
				t.Fatalf("revision %d = %v\n%s", v, err, out)
			}
		}
		if want := fmt.Sprintf("failed schema-kit: the library holds as many bytes of files of it as it may (%d in all its revisions, and this folder's would make %d); nothing was written. A skill's revisions are never removed: to keep importing this folder, give it another name in its SKILL.md, or import it into another scope with --scope", library.MaxSkillBytes, library.MaxSkillBytes+library.MaxPackageBytes); err == nil || !strings.Contains(out, want) {
			t.Fatalf("revision %d = %v, want %q\n%s", fits+1, err, want, out)
		}
		if rows, err := p.SkillLibrary.History("acme", library.Scope{Kind: "personal", Subject: "dev"}, "schema-kit"); err != nil || len(rows) != fits {
			t.Fatalf("schema-kit = %d revisions, %v", len(rows), err)
		}
	})
}

// A folder whose files add up to more than a package holds is refused at the
// first file past the bound, named under failed with it: the files after it
// are not read, and none is read past the largest the library takes.
func TestImportSkillsStopsReadingAFolderPastThePackageBound(t *testing.T) {
	dir := t.TempDir()
	read := map[string]int{}
	counted := readSkillFile
	readSkillFile = func(path string, max int64) ([]byte, error) {
		data, err := counted(path, max)
		rel, _ := filepath.Rel(dir, path)
		read[filepath.ToSlash(rel)] += len(data)
		return data, err
	}
	t.Cleanup(func() { readSkillFile = counted })
	putFile(t, dir, "heavy/SKILL.md", fmt.Sprintf(skillMD, "heavy", "heavy", "heavy"))
	for i := range 8 {
		putFile(t, dir, fmt.Sprintf("heavy/schemas/part%d.xsd", i), generatedFile(900<<10, fmt.Sprint("heavy", i)))
	}
	api := &importServer{}
	server := api.start(t)
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err == nil || !strings.Contains(out, fmt.Sprintf("failed heavy: more than %d bytes in all", library.MaxPackageBytes)) || len(api.skills) != 0 {
		t.Fatalf("err = %v\n%s", err, out)
	}
	total := 0
	for path, n := range read {
		total += n
		if strings.HasPrefix(path, "heavy/schemas/part") && path >= "heavy/schemas/part2.xsd" {
			t.Errorf("%s was read past the bound (%d bytes)", path, n)
		}
	}
	if read["heavy/schemas/part1.xsd"] != 900<<10 || total > library.MaxPackageBytes {
		t.Fatalf("read %d bytes of the folder, more than the %d a package holds: %v", total, library.MaxPackageBytes, read)
	}
}

// A skill the server lists as unreadable, such as one a later release wrote,
// is named under failed and nothing is sent for it; the rest of the scope is
// imported as usual.
func TestImportSkillsNamesASkillTheServerCannotRead(t *testing.T) {
	var drafts []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/library/skills":
			fmt.Fprint(w, `{"skills":[],"unreadable":["depot"]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/api/library/skills":
			var body struct{ Definition library.Definition }
			_ = json.NewDecoder(r.Body).Decode(&body)
			drafts = append(drafts, body.Definition.Name)
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	dir := depot(t, "The code word is HERON.")
	putFile(t, dir, "ship/SKILL.md", fmt.Sprintf(skillMD, "ship", "ship", "ship"))
	out, err := runImport(t, "skills", dir, "--server", server.URL)
	if err == nil || !strings.Contains(out, "failed depot: the library holds a record of it this server cannot read, such as one a later release wrote; nothing was written") || strings.Join(drafts, ",") != "ship" {
		t.Fatalf("err = %v, drafts = %v\n%s", err, drafts, out)
	}
}
