package platform_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/urmzd/humagen/internal/platform"
	"github.com/urmzd/humagen/internal/wire"
	"github.com/urmzd/humagen/pkg/auth"
	"github.com/urmzd/humagen/pkg/call"
	"github.com/urmzd/humagen/pkg/runs"
	"github.com/urmzd/humagen/pkg/threads"
	"github.com/urmzd/saige/flow/isolation"
	"github.com/urmzd/saige/flow/isolation/isolationtest"
)

func commandThread(t *testing.T, p *platform.Platform) threads.Thread {
	t.Helper()
	return startThread(t, p, operator, platform.ThreadInput{Title: "Commands", Text: "Notes", PostOnly: true, Visibility: threads.Private}).Thread
}

func TestAPersonsCommandRunsInAnInstanceOfItsOwnAndKeepsItsFiles(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	th := commandThread(t, p)
	if err := p.PutFile(tenant, th.ID, "notes.md", "first"); err != nil {
		t.Fatal(err)
	}
	if boxes, err := p.Sandboxes(t.Context(), operator, th.ID); err != nil || !boxes.Session.Commands {
		t.Fatalf("commands not offered: %+v %v", boxes.Session, err)
	}
	res, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: " cat notes.md; echo second > made.txt; rm notes.md; exit 3 "})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.TimedOut || res.Output != "first" || res.Truncated || res.Command != "cat notes.md; echo second > made.txt; rm notes.md; exit 3" {
		t.Fatalf("result = %+v", res)
	}
	changed := map[string]bool{}
	for _, f := range res.Files {
		changed[f.Path] = f.Removed
	}
	if removed, ok := changed["notes.md"]; !ok || !removed {
		t.Errorf("the removal was not reported: %+v", res.Files)
	}
	if removed, ok := changed["made.txt"]; !ok || removed {
		t.Errorf("the new file was not reported: %+v", res.Files)
	}
	dir := p.SandboxDir(tenant, th.ID)
	if b, err := os.ReadFile(filepath.Join(dir, "made.txt")); err != nil || string(b) != "second\n" {
		t.Errorf("made.txt = %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "notes.md")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("notes.md was kept: %v", err)
	}
	// One instance, for that command alone: no network, no tool servers, and
	// destroyed once the command ended.
	all := leaser.Instances()
	if len(all) != 1 || !all[0].Closed() || len(leaser.Open()) != 0 {
		t.Fatalf("instances = %d, open = %d", len(all), len(leaser.Open()))
	}
	if req := all[0].Request(); req.Tenant != tenant || req.Connectors || len(req.Egress) != 0 {
		t.Errorf("lease = %+v", req)
	}
	execs := all[0].Execs()
	if len(execs) != 1 || execs[0].In != isolation.Workload || len(execs[0].Env) != 0 {
		t.Errorf("execs = %+v", execs)
	}
}

func TestAPersonsCommandIsRefusedWhereItCouldReachTheHostOrIsNotTheirs(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	run := platform.PersonCommand{Command: "echo hi"}
	t.Run("an unisolated deployment", func(t *testing.T) {
		p := start(t, call.Limits{})
		th := commandThread(t, p)
		if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, run); !errors.Is(err, platform.ErrUnisolated) {
			t.Fatalf("err = %v", err)
		}
		if boxes, _ := p.Sandboxes(t.Context(), operator, th.ID); boxes.Session.Commands {
			t.Fatal("commands offered with no sandbox")
		}
	})
	t.Run("a sandbox that is not contained, which is the zero value", func(t *testing.T) {
		p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "local (not isolated)"}})
		th := commandThread(t, p)
		if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, run); !errors.Is(err, platform.ErrUnisolated) {
			t.Fatalf("err = %v", err)
		}
		if boxes, _ := p.Sandboxes(t.Context(), operator, th.ID); boxes.Session.Commands {
			t.Fatal("commands offered on the host")
		}
	})
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	th := commandThread(t, p)
	t.Run("another workspace", func(t *testing.T) {
		other := auth.Principal{Subject: operator.Subject, Tenant: "globex", Kind: auth.User, Roles: []auth.Role{auth.Admin}}
		if _, err := p.RunPersonCommand(t.Context(), other, th.ID, run); !errors.Is(err, threads.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("a reader", func(t *testing.T) {
		if _, err := p.RunPersonCommand(t.Context(), viewer, th.ID, run); !errors.Is(err, auth.ErrForbidden) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no command, or too long a one", func(t *testing.T) {
		for _, c := range []string{"", "   ", strings.Repeat("a", platform.MaxPersonCommand+1), "echo \x00"} {
			if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: c}); !errors.Is(err, platform.ErrSandbox) {
				t.Errorf("%q: err = %v", c[:min(len(c), 12)], err)
			}
		}
	})
	t.Run("while work is under way", func(t *testing.T) {
		tag := wire.NewEvent(wire.Raise{ID: "command-busy", Tenant: tenant, Agent: "lead", Origin: runs.OriginAPI, Text: "Work", Context: map[string]string{runs.MetaThread: th.ID}, At: time.Now()})
		if err := p.Runs.Record("command-busy", tag, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, run); !errors.Is(err, platform.ErrWorkspaceConflict) {
			t.Fatalf("err = %v", err)
		}
	})
	if n := len(leaser.Instances()); n != 0 {
		t.Fatalf("a refused command leased %d instances", n)
	}
}

func TestAPersonsCommandIsBoundedInOutputAndOnePerConversation(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	th := commandThread(t, p)
	res, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "i=0; while [ $i -lt 4000 ]; do echo line-$i; i=$((i+1)); done"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Output) > platform.MaxPersonCommandOutput || !strings.HasSuffix(res.Output, "line-3999\n") {
		t.Fatalf("truncated = %v, %d bytes, ends %q", res.Truncated, len(res.Output), res.Output[max(0, len(res.Output)-20):])
	}
	// A second command in the conversation waits its turn by being refused.
	started, done := make(chan struct{}), make(chan error, 1)
	gate := filepath.Join(t.TempDir(), "gate")
	go func() {
		close(started)
		_, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "while [ ! -e " + gate + " ]; do sleep 0.05; done"})
		done <- err
	}()
	<-started
	deadline := time.Now().Add(5 * time.Second)
	for len(leaser.Open()) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "echo second"}); !errors.Is(err, platform.ErrWorkspaceConflict) {
		t.Errorf("a second command at once: %v", err)
	}
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "echo third"}); err != nil {
		t.Fatalf("after the first ended: %v", err)
	}
}

func TestAPersonsCommandIsStoppedAtItsTimeLimit(t *testing.T) {
	leaser := &isolationtest.Leaser{Dir: t.TempDir()}
	p := startWith(t, platform.Config{Sandbox: isolation.Sandbox{Leaser: leaser, Name: "test"}, SandboxContained: true})
	p.SetPersonCommandTimeout(300 * time.Millisecond)
	th := commandThread(t, p)
	res, err := p.RunPersonCommand(t.Context(), operator, th.ID, platform.PersonCommand{Command: "echo started; echo kept > kept.txt; while :; do :; done"})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.ExitCode != -1 || res.Output != "started\n" {
		t.Fatalf("result = %+v", res)
	}
	// What it wrote before the limit is still the conversation's.
	if b, err := os.ReadFile(filepath.Join(p.SandboxDir(tenant, th.ID), "kept.txt")); err != nil || string(b) != "kept\n" {
		t.Errorf("kept.txt = %q %v", b, err)
	}
	if len(leaser.Open()) != 0 {
		t.Error("the instance outlived its command")
	}
}
