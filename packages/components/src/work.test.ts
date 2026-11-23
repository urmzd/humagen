import type { Task } from "@humagen/core";
import { workTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { workSummary } from "./work";

const call = (id: string, tool: string, args: unknown, over: Partial<Task> = {}): Task => ({
  id,
  title: tool.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase()),
  tool,
  status: "complete",
  input: JSON.stringify(args),
  ...over,
});

describe("workSummary", () => {
  it("counts a turn's calls by kind, in the order each kind first came", () => {
    expect(workSummary(workTurn.tasks)).toBe(
      "Ran 2 searches, read 3 files, ran 3 commands (2 failed), updated 2 files (+4)",
    );
  });

  it("says a failure a later call recovered as retried, and one it did not as failed", () => {
    const refused = { status: "error" as const, output: "connector: undeliverable" };
    const retried = [
      call("1", "github_read_context", { address: "urmzd/humagen" }, refused),
      call("2", "github_read_context", { address: "github://urmzd/humagen/pull/541" }),
    ];
    expect(workSummary(retried)).toBe("Checked 2 sources (1 retried)");
    expect(workSummary([...retried].reverse())).toBe("Checked 2 sources (1 failed)");
  });

  it("says lines added and removed across every file written", () => {
    const tasks = [
      call("1", "file_edit", { path: "a.ts", old_string: "a\nb", new_string: "a\nc\nd" }),
      call("2", "file_edit", { path: "b.ts", old_string: "x", new_string: "y" }),
      call("3", "shell", { command: "make" }, { output: "exit 0\n" }),
    ];
    expect(workSummary(tasks)).toBe("Updated 2 files (+3 -2), ran a command");
  });

  it("names the one file a turn wrote, however many times", () => {
    const tasks = [
      call("1", "file_edit", { path: "src/a.ts", old_string: "a", new_string: "b" }),
      call("2", "file_edit", { path: "src/a.ts", old_string: "c", new_string: "d\ne" }),
      call("3", "file_read", { path: "src/a.ts" }),
    ];
    expect(workSummary(tasks)).toBe("Updated src/a.ts (+3 -2), read a file");
  });

  it("counts a call that failed with its kind, a non-zero exit among them", () => {
    const tasks = [
      call("1", "shell", { command: "make" }, { output: "exit 2\nboom" }),
      // The page fetched first: no later fetch recovered the one that failed.
      call("3", "web_fetch", { url: "https://example.org" }),
      call(
        "2",
        "web_fetch",
        { url: "https://example.com" },
        { status: "error", output: "timeout" },
      ),
    ];
    expect(workSummary(tasks)).toBe("Ran a command (1 failed), fetched 2 pages (1 failed)");
  });

  it("leaves calls to other agents out: each has its own row", () => {
    const tasks = [
      call("1", "file_read", { path: "a.ts" }),
      call("2", "agent", { agent: "billing" }, { subagent: { name: "billing" } }),
    ];
    expect(workSummary(tasks)).toBe("Read a file");
    expect(workSummary(tasks.slice(1))).toBe("");
  });

  it("names a lone tool of no known kind and counts several", () => {
    expect(workSummary([call("1", "jira_triage", {})])).toBe("Used Jira triage");
    expect(workSummary([call("1", "jira_triage", {}), call("2", "jira_triage", {})])).toBe(
      "Used 2 tools",
    );
  });
});
