import { describe, expect, it } from "vitest";
import { replay, type TurnEvent } from "./events";
import {
  fileEdit,
  fileEdits,
  formatArgs,
  formatResult,
  lineDiff,
  shortAddress,
  timeline,
  toolFailed,
  toolKind,
  toolTarget,
} from "./timeline";
import type { Task } from "./types";

const call = (id: string, tool: string, args: unknown, over: Partial<Task> = {}): Task => ({
  id,
  title: tool,
  tool,
  status: "in_progress",
  input: JSON.stringify(args),
  ...over,
});

// Text, tools, text, tools, text: three model steps, as the stream sends them.
const steps: TurnEvent[] = [
  { type: "turn.started", turnId: "t", agent: { name: "A" } },
  { type: "text.delta", text: "Let me look at " },
  { type: "text.delta", text: "the repo." },
  { type: "task.upserted", task: call("c1", "file_read", { path: "a.go" }) },
  { type: "task.upserted", task: call("c2", "file_read", { path: "b.go" }) },
  {
    type: "task.upserted",
    task: call("c1", "file_read", { path: "a.go" }, { status: "complete", output: "package a" }),
  },
  {
    type: "task.upserted",
    task: call("c2", "file_read", { path: "b.go" }, { status: "complete", output: "package b" }),
  },
  { type: "reasoning.delta", text: "a.go needs a fix" },
  { type: "text.delta", text: "\n\nFound it, fixing." },
  {
    type: "task.upserted",
    task: call("c3", "file_write", { path: "a.go", content: "package a\n\nfunc A() {}\n" }),
  },
  {
    type: "task.upserted",
    task: call(
      "c3",
      "file_write",
      { path: "a.go", content: "package a\n\nfunc A() {}\n" },
      { status: "complete" },
    ),
  },
  { type: "text.delta", text: "\n\nDone." },
  { type: "turn.completed", durationMs: 4000 },
];

describe("timeline", () => {
  it("keeps text and tool runs in the order the stream sent them", () => {
    const t = timeline(replay(steps));
    expect(t.segments.map((s) => s.kind)).toEqual(["text", "tools", "thought", "text", "tools"]);
    const [first, reads, thought, middle, writes] = t.segments;
    expect(first).toMatchObject({ text: "Let me look at the repo." });
    expect(reads?.kind === "tools" && reads.tasks.map((x) => x.id)).toEqual(["c1", "c2"]);
    expect(thought).toMatchObject({ text: "a.go needs a fix" });
    expect(middle).toMatchObject({ text: "\n\nFound it, fixing." });
    expect(writes?.kind === "tools" && writes.tasks.map((x) => x.id)).toEqual(["c3"]);
    expect(t.tail).toBe("\n\nDone.");
    expect(t.rest).toEqual([]);
  });

  it("does not move a step when it finishes after more words arrived", () => {
    const t = timeline(
      replay([
        { type: "turn.started", turnId: "t", agent: { name: "A" } },
        { type: "text.delta", text: "One." },
        { type: "task.upserted", task: call("c1", "bash", { command: "ls" }) },
        { type: "text.delta", text: " Two." },
        {
          type: "task.upserted",
          task: call("c1", "bash", { command: "ls" }, { status: "complete" }),
        },
      ]),
    );
    expect(t.segments.map((s) => s.kind)).toEqual(["text", "tools"]);
    expect(t.tail).toBe(" Two.");
  });

  it("puts a call to another agent on its own, between the runs around it", () => {
    const t = timeline(
      replay([
        { type: "turn.started", turnId: "t", agent: { name: "A" } },
        { type: "task.upserted", task: call("c1", "bash", { command: "ls" }) },
        { type: "task.upserted", task: { ...call("c2", "tag_b", {}), subagent: { name: "b" } } },
        { type: "task.upserted", task: call("c3", "bash", { command: "pwd" }) },
      ]),
    );
    expect(t.segments.map((s) => s.kind)).toEqual(["tools", "call", "tools"]);
  });

  it("places steps recorded without a place after the answer", () => {
    const t = timeline({ text: "Answer", tasks: [call("c1", "bash", {})] });
    expect(t.segments).toEqual([]);
    expect(t.tail).toBe("Answer");
    expect(t.rest.map((s) => s.kind)).toEqual(["tools"]);
  });
});

describe("tool facts", () => {
  it("knows a call's kind from its name", () => {
    expect(toolKind({ tool: "bash", title: "" })).toBe("run");
    expect(toolKind({ tool: "file_write", title: "" })).toBe("write");
    expect(toolKind({ tool: "file_read", title: "" })).toBe("read");
    expect(toolKind({ tool: "github_read_context", title: "" })).toBe("read");
    expect(toolKind({ tool: "web_search", title: "" })).toBe("search");
    expect(toolKind({ tool: "web_fetch", title: "" })).toBe("fetch");
    expect(toolKind({ tool: "grep", title: "" })).toBe("search");
    expect(toolKind({ tool: "record_outcome", title: "" })).toBe("tool");
  });

  it("names the target a call was pointed at", () => {
    expect(toolTarget(call("1", "bash", { command: "go test ./..." }))).toBe("go test ./...");
    expect(toolTarget(call("1", "bash", { command: ["git", "status"] }))).toBe("git status");
    expect(
      toolTarget(call("1", "x", { address: "https://github.com/urmzd/humagen/pull/178" })),
    ).toBe("urmzd/humagen#178");
    expect(toolTarget(call("1", "x", { other: 1 }))).toBeUndefined();
    expect(shortAddress("https://github.com/urmzd/humagen")).toBe("urmzd/humagen");
  });

  it("reads a failed action from its result", () => {
    expect(
      toolFailed({ status: "complete", output: "action failed: operations: action denied" }),
    ).toBe(true);
    expect(toolFailed({ status: "error" })).toBe(true);
    expect(toolFailed({ status: "complete", output: "ok" })).toBe(false);
  });

  it("formats arguments and results for reading", () => {
    expect(formatArgs(call("1", "bash", { command: "ls -la" }))).toBe("ls -la");
    expect(formatArgs(call("1", "x", { a: 1 }))).toBe('{\n  "a": 1\n}');
    expect(formatResult('{"ok":true}')).toBe('{\n  "ok": true\n}');
    expect(formatResult("plain")).toBe("plain");
  });
});

describe("file edits", () => {
  it("counts lines a write added", () => {
    expect(
      fileEdit(
        call("1", "file_write", { path: "notes.md", content: "a\nb\nc\n" }, { status: "complete" }),
      ),
    ).toEqual({
      path: "notes.md",
      change: "wrote",
      added: 3,
      removed: 0,
    });
  });

  it("says created when the tool or its result does", () => {
    expect(
      fileEdit(call("1", "create_file", { path: "plan.md", content: "x" }, { status: "complete" }))
        ?.change,
    ).toBe("created");
    expect(
      fileEdit(
        call(
          "1",
          "write_file",
          { path: "plan.md", content: "x" },
          { status: "complete", output: "Created plan.md" },
        ),
      )?.change,
    ).toBe("created");
  });

  it("counts an edit's old and new text", () => {
    expect(
      fileEdit(
        call(
          "1",
          "edit_file",
          { path: "a.go", old_string: "a\nb", new_string: "a\nc\nd" },
          { status: "complete" },
        ),
      ),
    ).toEqual({ path: "a.go", change: "updated", added: 2, removed: 1 });
  });

  it("counts a patch", () => {
    expect(
      fileEdit(
        call(
          "1",
          "apply_patch",
          { path: "a.go", patch: "--- a\n+++ b\n-x\n+y\n+z" },
          { status: "complete" },
        ),
      ),
    ).toEqual({ path: "a.go", change: "updated", added: 2, removed: 1 });
  });

  it("is nothing for a failed, refused or non-file call", () => {
    expect(
      fileEdit(call("1", "file_write", { path: "a", content: "x" }, { status: "error" })),
    ).toBeUndefined();
    expect(
      fileEdit(
        call(
          "1",
          "file_write",
          { path: "a", content: "x" },
          { status: "complete", output: "action failed: no" },
        ),
      ),
    ).toBeUndefined();
    expect(
      fileEdit(call("1", "publish_artifact", { path: "a", content: "x" }, { status: "complete" })),
    ).toBeUndefined();
    expect(
      fileEdit(call("1", "github_comment", { path: "a", body: "x" }, { status: "complete" })),
    ).toBeUndefined();
    expect(fileEdit(call("1", "file_read", { path: "a" }, { status: "complete" }))).toBeUndefined();
  });

  it("counts a second write against the first", () => {
    const edits = fileEdits([
      call("1", "file_write", { path: "n.md", content: "a\nb\n" }, { status: "complete" }),
      call("2", "file_write", { path: "n.md", content: "a\nc\nd\n" }, { status: "complete" }),
    ]);
    expect(edits.get("2")).toEqual({ path: "n.md", change: "updated", added: 2, removed: 1 });
  });

  it("diffs lines", () => {
    expect(lineDiff("", "a\nb")).toEqual({ added: 2, removed: 0 });
    expect(lineDiff("a\nb\nc", "a\nc")).toEqual({ added: 0, removed: 1 });
  });
});

describe("the multi-step fixture", () => {
  it("reads as text, tools, text, tools, text, tools, answer", async () => {
    const { multiStepTurn } = await import("./fixtures");
    const t = timeline(multiStepTurn);
    expect(t.segments.map((s) => s.kind)).toEqual([
      "text",
      "tools",
      "text",
      "tools",
      "text",
      "tools",
    ]);
    expect(t.tail.trim().startsWith("The driver is sound.")).toBe(true);
  });
});
