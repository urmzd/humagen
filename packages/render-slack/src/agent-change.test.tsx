import { TurnView } from "@humagen/components";
import type { Task, Turn } from "@humagen/core";
import { doneTurn } from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { renderToSlack } from "./index";

const preview = (tool: string, kind: string): Task => ({
  id: "preview",
  title: "Evaluate proposed change",
  status: "complete",
  tool,
  output: JSON.stringify({
    change: {
      id: "change-1",
      agent: "reviewer",
      kind,
      evaluated: true,
      passed: false,
      scores: [{ suite: "review-quality", before: 100, after: 50, passed: false }],
    },
    before_prompt: "Check the whole patch.",
    after_prompt: "Check only the summary.",
    tools: ["file_read", "person_tools"],
    connections: [
      { name: "Team Slack", service: "slack", scope: "workspace", status: "configured" },
    ],
    listens: [{ connection: "Team Slack", place: "#reviews", project: "Launch", enabled: false }],
  }),
});
describe("native recorded change review", () => {
  it.each([
    ["preview_agent_change", "agent", "reviewer"],
    ["preview_workspace_prompt", "workspace_prompt", "workspace instructions"],
  ])("renders %s prompt evidence and score regression before approval", (tool, kind, target) => {
    const turn: Turn = { ...doneTurn, tasks: [preview(tool, kind)] };
    const rendered = renderToSlack(<TurnView turn={turn} />);
    expect(rendered.warnings).toEqual([]);
    const blocks = JSON.stringify(rendered.pages);
    expect(blocks).toContain(`Proposed change to ${target}`);
    expect(blocks).toContain("Check the whole patch.");
    expect(blocks).toContain("Check only the summary.");
    expect(blocks).toContain("Before 100% → After 50%. Regression");
    expect(blocks).toContain("override reason");
    expect(blocks).toContain("file_read");
    expect(blocks).toContain("Personal connection tools");
    expect(blocks).not.toContain("person_tools");
    expect(blocks).toContain("configured");
    expect(blocks).toContain("#reviews");
    expect(blocks).toContain("Disabled");
    expect(blocks).not.toContain("Connected");
    expect(blocks).not.toContain('"text":"Apply"');
  });
  it("does not promote incomplete or unrelated tool output into an approval preview", () => {
    const task = preview("preview_agent_change", "agent");
    for (const hidden of [
      { ...task, status: "in_progress" as const },
      { ...task, tool: "file_read" },
      { ...task, output: '{"change":{}}' },
    ]) {
      const rendered = renderToSlack(<TurnView turn={{ ...doneTurn, tasks: [hidden] }} />);
      expect(JSON.stringify(rendered.pages)).not.toContain("Proposed change to reviewer");
    }
  });
});
