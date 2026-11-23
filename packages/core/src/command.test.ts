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
  it("is recovered by a later success of the same tool, even at another address", () => {
    const calls = [
      read("a", "urmzd/humagen", refused),
      read("b", "github://urmzd/humagen/pull/541"),
    ];
    expect([...recoveredCalls(calls)]).toEqual(["a"]);
  });

  it("is recovered by a later success at the same target through another tool", () => {
    const fetch: Task = {
      id: "b",
      title: "Fetch",
      tool: "web_fetch",
      status: "complete",
      input: JSON.stringify({ url: "https://github.com/urmzd/humagen/pull/541" }),
    };
    const calls = [read("a", "https://github.com/urmzd/humagen/pull/541", refused), fetch];
    expect([...recoveredCalls(calls)]).toEqual(["a"]);
  });

  it("stays failed when nothing later succeeded, or only an earlier call did", () => {
    expect(recoveredCalls([read("a", "x/y", refused)]).size).toBe(0);
    expect(recoveredCalls([read("a", "x/y"), read("b", "x/y", refused)]).size).toBe(0);
    expect(recoveredCalls([read("a", "x/y", refused), read("b", "x/y", refused)]).size).toBe(0);
    // A later call still running has not succeeded yet.
    expect(
      recoveredCalls([read("a", "x/y", refused), read("b", "x/y", { status: "in_progress" })]).size,
    ).toBe(0);
  });

  it("counts a result that says the action failed as a failure, and not as a recovery", () => {
    const said = { output: "action failed: not granted" };
    expect([...recoveredCalls([read("a", "x/y", said), read("b", "x/z")])]).toEqual(["a"]);
    expect(recoveredCalls([read("a", "x/y", refused), read("b", "x/z", said)]).size).toBe(0);
  });

  it("needs the same command line for a command", () => {
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
    // A rerun that exits non-zero did not recover it.
    expect(
      recoveredCalls([
        shell("a", "go test ./...", undefined, broken),
        shell("b", "go test ./...", "exit 1\n"),
      ]).size,
    ).toBe(0);
  });
});
