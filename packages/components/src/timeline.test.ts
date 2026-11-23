import { recoveredCalls, type Task } from "@humagen/core";
import { describe, expect, it } from "vitest";
import { callLine, clipTarget, toolsLine } from "./timeline";

const call = (id: string, tool: string, args: unknown, over: Partial<Task> = {}): Task => ({
  id,
  title: tool.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase()),
  tool,
  status: "complete",
  input: JSON.stringify(args),
  ...over,
});

describe("toolsLine", () => {
  it("counts a run of calls that all failed, without naming them as done", () => {
    const failed = { status: "error" as const, output: "no such file" };
    const line = toolsLine([
      call("1", "file_write", { path: "a.md" }, failed),
      call("2", "file_write", { path: "a.md" }, failed),
    ]);
    expect(line.text).toBe("2 steps failed");
    expect(line.failed).toBe(0);
  });

  it("says what a lone call was pointed at", () => {
    expect(
      toolsLine([
        call("1", "github_read_context", { address: "https://github.com/urmzd/humagen/pull/178" }),
      ]).text,
    ).toBe("Checked urmzd/humagen#178");
    expect(toolsLine([call("1", "file_read", { path: "pkg/isolate/docker/docker.go" })]).text).toBe(
      "Read docker.go",
    );
    expect(toolsLine([call("1", "bash", { command: "go test ./..." })]).text).toBe(
      "Ran go test ./...",
    );
    expect(toolsLine([call("1", "web_search", { query: "slack block kit limits" })]).text).toBe(
      "Searched slack block kit limits",
    );
  });

  it("counts a run of one kind", () => {
    const reads = ["a.go", "b.go", "c.go"].map((p, i) => call(String(i), "file_read", { path: p }));
    expect(toolsLine(reads).text).toBe("Read 3 files");
    expect(
      toolsLine([call("1", "bash", { command: "ls" }), call("2", "bash", { command: "pwd" })]).text,
    ).toBe("Ran 2 commands");
  });

  it("names each kind of a mix in the order it came", () => {
    expect(
      toolsLine([
        call("1", "file_read", { path: "a" }),
        call("2", "file_read", { path: "b" }),
        call("3", "bash", { command: "ls" }),
      ]).text,
    ).toBe("Read 2 files, ran a command");
  });

  it("counts calls when there are too many kinds to name", () => {
    expect(
      toolsLine([
        call("1", "file_read", { path: "a" }),
        call("2", "bash", { command: "ls" }),
        call("3", "web_search", { query: "q" }),
        call("4", "web_fetch", { url: "https://example.com" }),
      ]).text,
    ).toBe("Used 4 tools");
  });

  it("says a file written as the file, with its counts", () => {
    const line = toolsLine([
      call("1", "file_write", { path: "scratchpads/notes.md", content: "a\nb\nc\n" }),
    ]);
    expect(line.text).toBe("Wrote scratchpads/notes.md");
    expect(line.file).toMatchObject({ path: "scratchpads/notes.md", added: 3, removed: 0 });
    expect(line.counts).toBe("+3");
    const edit = toolsLine([
      call("1", "edit_file", { path: "a.go", old_string: "x", new_string: "y\nz" }),
    ]);
    expect(edit.text).toBe("Updated a.go");
    expect(edit.counts).toBe("+2 -1");
  });

  it("says one file written twice once, with every line counted", () => {
    const line = toolsLine([
      call("1", "file_write", { path: "n.md", content: "a\n" }),
      call("2", "file_write", { path: "n.md", content: "a\nb\n" }),
    ]);
    expect(line.text).toBe("Updated n.md");
    expect(line.file?.path).toBe("n.md");
  });

  it("counts failures without changing the line", () => {
    const line = toolsLine([
      call("1", "file_read", { path: "a" }),
      call("2", "file_read", { path: "b" }, { output: "action failed: operations: action denied" }),
    ]);
    expect(line.text).toBe("Read 2 files");
    expect(line.failed).toBe(1);
  });

  it("says a failure a later call recovered as retried, not failed", () => {
    // The production case: a bare owner/repo was refused, then the full address read.
    const refused = { status: "error" as const, output: "connector: undeliverable" };
    const line = toolsLine([
      call("1", "github_read_context", { address: "urmzd/humagen" }, refused),
      call("2", "github_read_context", { address: "github://urmzd/humagen/pull/541" }),
    ]);
    expect(line).toMatchObject({ text: "Checked 2 sources", failed: 0, retried: 1 });
  });

  it("keeps a failure no later call recovered as failed", () => {
    const refused = { status: "error" as const, output: "connector: undeliverable" };
    const line = toolsLine([
      call("1", "github_read_context", { address: "github://urmzd/humagen/pull/541" }),
      call("2", "github_read_context", { address: "urmzd/humagen" }, refused),
    ]);
    expect(line).toMatchObject({ failed: 1, retried: 0 });
  });

  it("counts a retry in a later run of the turn when given the turn's recovered calls", () => {
    const refused = { status: "error" as const, output: "connector: undeliverable" };
    const first = call("1", "github_read_context", { address: "urmzd/humagen" }, refused);
    const later = call("2", "github_read_context", { address: "github://urmzd/humagen/pull/541" });
    expect(toolsLine([first])).toMatchObject({ failed: 1, retried: 0 });
    expect(toolsLine([first], undefined, undefined, recoveredCalls([first, later]))).toMatchObject({
      text: "Checked urmzd/humagen",
      failed: 0,
      retried: 1,
    });
  });

  it("says which call is running", () => {
    const line = toolsLine([
      call("1", "file_read", { path: "a" }),
      call("2", "bash", { command: "ls" }, { status: "in_progress" }),
    ]);
    expect(line).toMatchObject({ text: "Running bash", running: true });
  });

  it("says a call waits on a person, and names the waiting call once", () => {
    const line = toolsLine([
      call("1", "file_read", { path: "a" }),
      call("2", "deploy", {}, { status: "waiting" }),
    ]);
    expect(line.waiting).toBe(true);
    expect(line.text).toBe("Deploy");
  });
});

describe("callLine", () => {
  it("names a tool of no known kind by its title", () => {
    expect(callLine(call("1", "record_outcome", {}))).toBe("Record outcome");
  });
});

describe("clipTarget", () => {
  it("keeps the end of a long path and cuts other text", () => {
    expect(
      clipTarget(
        "a/very/long/path/to/some/deeply/nested/folder/structure/that/goes/on/file.ts",
        30,
      ),
    ).toBe("…/that/goes/on/file.ts");
    expect(clipTarget("x".repeat(80), 10)).toBe(`${"x".repeat(9)}…`);
  });
});

describe("a file written again later in the turn", () => {
  it("counts against what the earlier run left there", async () => {
    const { fileEdits } = await import("@humagen/core");
    const first = call("1", "file_write", { path: "plan.md", content: "a\nb\n" });
    const read = call("2", "file_read", { path: "plan.md" });
    const second = call("3", "file_write", { path: "plan.md", content: "a\nc\nd\n" });
    const line = toolsLine([second], undefined, fileEdits([first, read, second]));
    expect(line.text).toBe("Updated plan.md");
    expect(line.counts).toBe("+2 -1");
  });
});
