import { describe, expect, it } from "vitest";
import { replay, type TurnEvent } from "./events";
import {
  fileEdit,
  fileEdits,
  formatArgs,
  formatResult,
  lineDiff,
  paragraphsAtSteps,
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

describe("turnChanges", () => {
  it("lists each file once, in the order first written, with its lines across the turn", async () => {
    const { turnChanges } = await import("./timeline");
    const done = { status: "complete" as const };
    const changes = turnChanges([
      call("1", "file_write", { path: "plan.md", content: "a\nb\n" }, done),
      call("2", "file_read", { path: "plan.md" }, done),
      call("3", "edit_file", { path: "src/a.go", old_string: "x", new_string: "y\nz" }, done),
      call("4", "file_write", { path: "plan.md", content: "a\nc\nd\n" }, done),
      call("5", "edit_file", { path: "src/a.go", old_string: "q", new_string: "" }, done),
      call("6", "file_write", { path: "x.md", content: "no" }, { status: "error" }),
    ]);
    expect(changes.map((c) => [c.path, c.added, c.removed, c.calls])).toEqual([
      // Written whole twice: from nothing to what it left.
      ["plan.md", 3, 0, ["1", "4"]],
      // Edits add up.
      ["src/a.go", 2, 2, ["3", "5"]],
    ]);
  });
});

describe("paragraphsAtSteps", () => {
  const at = (...positions: number[]) => positions.map((p) => ({ at: p }));

  it("breaks the words into paragraphs where a step came between them", () => {
    const text = "I'll look it up on the web.Here are three changes.";
    expect(paragraphsAtSteps(text, at(27))).toBe(
      "I'll look it up on the web.\n\nHere are three changes.",
    );
    // Spaces or a single line break at the step become the break.
    expect(paragraphsAtSteps("Before. \nAfter.", at(7))).toBe("Before.\n\nAfter.");
  });

  it("adds nothing where the words already break, before any words, or before more words come", () => {
    expect(paragraphsAtSteps("Before.\n\nAfter.", at(7))).toBe("Before.\n\nAfter.");
    expect(paragraphsAtSteps("After.", at(0))).toBe("After.");
    expect(paragraphsAtSteps("Before.", at(7))).toBe("Before.");
  });

  it("never breaks inside a code fence", () => {
    const text = "```sh\nmake\nmake test\n```";
    expect(paragraphsAtSteps(text, at(10))).toBe(text);
  });

  it("lays out a prefix as a prefix of the whole, so a stream can append to it", () => {
    const text = "I'll look it up.Here it is. Then I checked.And it holds.";
    const steps = at(16, 43);
    const whole = paragraphsAtSteps(text, steps);
    for (let n = 0; n <= text.length; n++) {
      expect(whole.startsWith(paragraphsAtSteps(text.slice(0, n), steps).trimEnd())).toBe(true);
    }
  });
});

describe("paragraphsAtSteps, where a break would harm", () => {
  const lay = (text: string, ...at: number[]) =>
    paragraphsAtSteps(
      text,
      at.map((a) => ({ at: a })),
    );

  it("makes exactly one blank line when the words already end in a newline", () => {
    expect(lay("Let me check.\nFound it.", 14)).toBe("Let me check.\n\nFound it.");
    expect(lay("Let me check.\r\nFound it.", 15)).not.toMatch(/\n\s*\n\s*\n/);
  });

  it("keeps a numbered list, a bulleted list, a quote or a table going on after the step together", () => {
    expect(lay("1. one\n2. two", 7)).toBe("1. one\n2. two");
    expect(lay("- one\n- two", 6)).toBe("- one\n- two");
    expect(lay("> quoted line\n> continues", 14)).toBe("> quoted line\n> continues");
    const table = "| a | b |\n|---|---|\n| 1 | 2 |\n| 3 | 4 |";
    expect(lay(table, 30)).toBe(table);
    // A list followed by words that do not go on with it still gets its break.
    expect(lay("- one\nDone.", 6)).toBe("- one\n\nDone.");
  });

  it("tracks a fence by its character and length", () => {
    // A backtick line inside a tilde fence does not close it.
    const tilde = "~~~\n```\nstill code\n~~~\nafter";
    expect(lay(tilde, 9)).toBe(tilde);
    // A shorter fence inside a longer one does not close it.
    const longer = "````md\n```\ninner\n```\nstill code\n````";
    expect(lay(longer, 23)).toBe(longer);
    // After the real close, the break is made.
    expect(lay("~~~\ncode\n~~~\nAfter.", 12)).toBe("~~~\ncode\n~~~\n\nAfter.");
  });

  it("never breaks inside a link, inline code, a table row or a word: at the line's end, or not at all", () => {
    expect(lay("see [the docs](https://x.test/d) now\nNext.", 9)).toBe(
      "see [the docs](https://x.test/d) now\n\nNext.",
    );
    expect(lay("see [the docs](https://x.test/d) now", 9)).toBe(
      "see [the docs](https://x.test/d) now",
    );
    expect(lay("run `go test` now\nNext.", 7)).toBe("run `go test` now\n\nNext.");
    expect(lay("| a | b |\n|---|---|\n| 1 | 2 |", 24)).toBe("| a | b |\n|---|---|\n| 1 | 2 |");
    expect(lay("hello world", 3)).toBe("hello world");
  });
});

describe("paragraphsAtSteps, generated transcripts (ported from the reviewer of #393)", () => {
  function rnd(seed: number) {
    let x = seed >>> 0 || 1;
    return () => {
      x ^= x << 13;
      x ^= x >>> 17;
      x ^= x << 5;
      return (x >>> 0) / 2 ** 32;
    };
  }
  const pieces = [
    "word",
    " ",
    "\n",
    "\n\n",
    "```",
    "`",
    "- ",
    "| a |",
    "> ",
    "  ",
    "\t",
    "[x](u)",
    ".",
    "~~~",
    "1. ",
  ];

  it("lays out a prefix as a prefix of the whole, for 4,000 transcripts and every prefix", () => {
    const fails: string[] = [];
    for (let seed = 1; seed <= 4000 && fails.length < 5; seed++) {
      const r = rnd(seed);
      let text = "";
      const tasks: { at: number }[] = [];
      const n = 3 + Math.floor(r() * 12);
      for (let i = 0; i < n; i++) {
        text += pieces[Math.floor(r() * pieces.length)];
        if (r() < 0.3) tasks.push({ at: text.length });
      }
      const whole = paragraphsAtSteps(text, tasks);
      expect(whole.replace(/\s+/g, "")).toBe(text.replace(/\s+/g, ""));
      // A break adds one blank line, never two.
      if (!/\n[ \t]*\n[ \t]*\n/.test(text)) expect(whole).not.toMatch(/\n[ \t]*\n[ \t]*\n/);
      for (let k = 0; k <= text.length; k++) {
        const part = paragraphsAtSteps(text.slice(0, k), tasks).replace(/\s+$/, "");
        if (!whole.startsWith(part)) {
          fails.push(`${JSON.stringify(text)} at=${JSON.stringify(tasks)} k=${k}`);
          break;
        }
      }
    }
    expect(fails).toEqual([]);
  });
});
