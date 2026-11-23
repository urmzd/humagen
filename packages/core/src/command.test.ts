import { describe, expect, it } from "vitest";
import { callFailed, commandResult, failedCommands, recoveredCalls } from "./command";
import { workTurn } from "./fixtures/work";
import type { Task } from "./types";

const shell = (
  id: string,
  command: string,
  output: string | undefined,
  over: Partial<Task> = {},
): Task => ({
  id,
  title: "Shell",
  tool: "shell",
  status: "complete",
  input: JSON.stringify({ command }),
  output,
  ...over,
});

describe("a command read from its call", () => {
  it("takes the exit code from the first line and leaves the rest as output", () => {
    expect(commandResult(shell("a", "go test ./...", "exit 3\nFAIL pkg\n"))).toEqual({
      command: "go test ./...",
      exitCode: 3,
      output: "FAIL pkg\n",
      failed: true,
    });
    expect(commandResult(shell("a", "true", "exit 0\n"))).toMatchObject({
      exitCode: 0,
      failed: false,
      output: "",
    });
  });

  it("has no exit code when the result names none", () => {
    const r = commandResult(shell("a", "make", "ok  \tpkg\t2.1s"));
    expect(r).toEqual({ command: "make", output: "ok  \tpkg\t2.1s", failed: false });
    // Output that only mentions an exit is not an exit line.
    expect(
      commandResult(shell("a", "make", "the job will exit 1 later"))?.exitCode,
    ).toBeUndefined();
  });

  it("is failed when the call itself failed, with no exit code", () => {
    const r = commandResult(
      shell("a", "make", "sandbox: the command did not run", { status: "error" }),
    );
    expect(r).toMatchObject({ failed: true });
    expect(r?.exitCode).toBeUndefined();
  });

  it("is nothing for a call still running, a call that is not a command, or another agent", () => {
    expect(commandResult(shell("a", "make", undefined, { status: "in_progress" }))).toBeUndefined();
    expect(
      commandResult({
        title: "File read",
        tool: "file_read",
        status: "complete",
        input: '{"path":"a"}',
        output: "exit 1\n",
      }),
    ).toBeUndefined();
    expect(
      commandResult(shell("a", "make", "exit 1\n", { subagent: { name: "helper" } })),
    ).toBeUndefined();
    expect(
      commandResult({ title: "Shell", tool: "shell", status: "complete", input: "{}" }),
    ).toBeUndefined();
  });

  it("counts a non-zero exit as a failed call", () => {
    expect(callFailed(shell("a", "make", "exit 2\n"))).toBe(true);
    expect(callFailed(shell("a", "make", "exit 0\n"))).toBe(false);
  });
});

describe("the failed commands a turn leaves standing", () => {
  it("leaves out one the same command later passed", () => {
    const left = failedCommands(workTurn.tasks);
    expect(left.map((c) => [c.id, c.command, c.exitCode])).toEqual([
      ["w10", "pnpm exec biome check src", 2],
    ]);
  });

  it("keeps one that was never run again with success", () => {
    const tasks = [
      shell("a", "make", "exit 1\nboom"),
      shell("b", "make", "exit 1\nboom"),
      shell("c", "ls", "exit 0\n"),
    ];
    expect(failedCommands(tasks).map((c) => c.id)).toEqual(["a", "b"]);
  });
});

const read = (id: string, address: string, over: Partial<Task> = {}): Task => ({
  id,
  title: "Read GitHub context",
  tool: "github_read_context",
  status: "complete",
  input: JSON.stringify({ address }),
  output: "{}",
  ...over,
});
const refused = { status: "error" as const, output: "connector: undeliverable" };

describe("a failed call a later call recovered", () => {
  it("is recovered by the same tool reading the same target again", () => {
    const calls = [read("a", "acme/docs", refused), read("b", "github://acme/docs")];
    expect([...recoveredCalls(calls)]).toEqual(["a"]);
    const spelled = [
      read("a", "https://github.com/acme/docs/pull/7", refused),
      read("b", "github://acme/docs/pull/7"),
    ];
    expect([...recoveredCalls(spelled)]).toEqual(["a"]);
  });

  it("is recovered when the same tool narrows a refused repository to one of its items", () => {
    // The production case: urmzd/humagen refused, then the pull request read.
    const calls = [
      read("a", "urmzd/humagen", refused),
      read("b", "github://urmzd/humagen/pull/541"),
    ];
    expect([...recoveredCalls(calls)]).toEqual(["a"]);
  });

  it("is recovered by the same write at the same target", () => {
    const write = (id: string, path: string, over: Partial<Task> = {}): Task => ({
      id,
      title: "Write file",
      tool: "write_file",
      status: "complete",
      input: JSON.stringify({ path, content: "x" }),
      ...over,
    });
    expect([...recoveredCalls([write("a", "a.go", refused), write("b", "a.go")])]).toEqual(["a"]);
  });

  describe("stays failed", () => {
    const call = (id: string, tool: string, args: unknown, over: Partial<Task> = {}): Task => ({
      id,
      title: tool,
      tool,
      status: "complete",
      input: JSON.stringify(args),
      ...over,
    });
    it("when the same tool then read another repository", () => {
      expect(recoveredCalls([read("a", "acme/secret", refused), read("b", "acme/docs")]).size).toBe(
        0,
      );
      // Another repository's item does not narrow the refused one.
      expect(
        recoveredCalls([read("a", "acme/secret", refused), read("b", "acme/secret-other#1")]).size,
      ).toBe(0);
    });
    it("when a failed write is followed by a read of the same file", () => {
      const calls = [
        call("a", "write_file", { path: "a.go" }, refused),
        call("b", "read_file", { path: "a.go" }),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
    });
    it("when a comment on one pull request failed and another succeeded", () => {
      const calls = [
        call("a", "github_comment", { address: "github://acme/docs/pull/1" }, refused),
        call("b", "github_comment", { address: "github://acme/docs/pull/2" }),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
    });
    it("when a failed merge is followed by a read of the same pull request", () => {
      const calls = [
        call("a", "github_merge", { address: "github://acme/docs/pull/1" }, refused),
        read("b", "github://acme/docs/pull/1"),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
    });
    it("when a write to a path failed and one to the same path in another case succeeded", () => {
      const calls = [
        call("a", "write_file", { path: "src/Foo.go" }, refused),
        call("b", "write_file", { path: "src/foo.go" }),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
    });
    it("when a file read failed and another file at the same address was read", () => {
      const at = "github://acme/docs/pull/7";
      const calls = [
        call("a", "github_read_file", { address: at, path: "secret.txt" }, refused),
        call("b", "github_read_file", { address: at, path: "readme.md" }),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
      // The same file read again at that address, spelled another way, recovers it.
      const again = call("c", "github_read_file", {
        address: "https://github.com/ACME/docs/pull/7",
        path: "secret.txt",
      });
      expect([...recoveredCalls([...calls, again])]).toEqual(["a"]);
    });
    it("when a write to one file failed and a write to another succeeded", () => {
      const calls = [
        call("a", "write_file", { path: "a.go" }, refused),
        call("b", "write_file", { path: "b.go" }),
      ];
      expect(recoveredCalls(calls).size).toBe(0);
    });
    it("when nothing later succeeded, or only an earlier call did", () => {
      expect(recoveredCalls([read("a", "x/y", refused)]).size).toBe(0);
      expect(recoveredCalls([read("a", "x/y"), read("b", "x/y", refused)]).size).toBe(0);
      expect(recoveredCalls([read("a", "x/y", refused), read("b", "x/y", refused)]).size).toBe(0);
      expect(
        recoveredCalls([read("a", "x/y", refused), read("b", "x/y", { status: "in_progress" })])
          .size,
      ).toBe(0);
      // A later result that says the action failed is no recovery.
      expect(
        recoveredCalls([
          read("a", "x/y", refused),
          read("b", "x/y", { output: "action failed: not granted" }),
        ]).size,
      ).toBe(0);
    });
  });

  it("needs the same command line and a zero exit for a command", () => {
    const broken = { status: "error" as const, output: "spawn failed" };
    expect(
      recoveredCalls([shell("a", "go test ./...", undefined, broken), shell("b", "ls", "exit 0\n")])
        .size,
    ).toBe(0);
    expect([
      ...recoveredCalls([
        shell("a", "go test ./...", undefined, broken),
        shell("b", "go test ./...", "exit 0\n"),
      ]),
    ]).toEqual(["a"]);
    expect(
      recoveredCalls([
        shell("a", "go test ./...", undefined, broken),
        shell("b", "go test ./...", "exit 1\n"),
      ]).size,
    ).toBe(0);
  });
});
