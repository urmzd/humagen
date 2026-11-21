import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { AgentHomeTab } from "./AgentHomeTab.tsx";
import { configureAgentHues, setAgentLooks } from "./agentHues.ts";
import type { Revision } from "./api.ts";

afterEach(() => {
  cleanup();
  configureAgentHues(undefined);
});

const barbara: Revision = {
  spec: {
    name: "barbara",
    description: "Customer performance reports.",
    provider: "anthropic",
    model: "claude-sonnet-5-5",
    system_prompt: "Keep the reports current.",
  },
  rev: 1,
  hash: "h",
  created_by: "ada",
  created_at: "2026-11-20T00:00:00Z",
};

test("an agent's home says who it is, asks it, and offers its starters as conversations", () => {
  setAgentLooks({
    barbara: {
      display_name: "Barbara",
      greeting: "I keep customer performance reports current.",
      starters: [
        {
          title: "Summarize the latest reports",
          prompt: "Summarize the latest reports and flag gaps.",
        },
      ],
    },
  });
  let looked = 0;
  render(
    <AgentHomeTab
      revision={barbara}
      stats={{ name: "barbara", runs: 3, completed: 2, failed: 1 } as never}
      canWrite
      onLook={() => looked++}
    />,
  );
  assert.equal(
    screen.getByTestId("agent-home-greeting").textContent,
    "I keep customer performance reports current.",
  );
  const ask = screen.getByText("Ask Barbara") as HTMLAnchorElement;
  assert.equal(ask.getAttribute("href"), "/?agent=barbara");
  const starter = screen
    .getByText("Summarize the latest reports")
    .closest("a") as HTMLAnchorElement;
  assert.match(String(starter.getAttribute("href")), /agent=barbara/);
  assert.match(String(starter.getAttribute("href")), /ask=Summarize/);
  assert.ok(screen.getByText("3 runs in the last day, 1 failed"));
  fireEvent.click(screen.getByText("Edit Look"));
  assert.equal(looked, 1);
});

test("an agent nobody introduced falls back to its description, and offers no starters", () => {
  setAgentLooks({}, ["barbara"]);
  render(<AgentHomeTab revision={barbara} canWrite={false} />);
  assert.equal(
    screen.getByTestId("agent-home-greeting").textContent,
    "Customer performance reports.",
  );
  assert.ok(screen.getByText("It offers no starters yet."));
  assert.equal(screen.queryByText("Edit Look"), null);
});
