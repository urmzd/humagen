import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentGeneral, AgentSettingsNav, groupOf, SETTINGS_GROUPS } from "./AgentSettings.tsx";
import { configureAgentHues, setAgentLooks } from "./agentHues.ts";
import type { AgentControl, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
  configureAgentHues(undefined);
});

const barbara = {
  spec: {
    name: "barbara",
    description: "Customer performance reports.",
    provider: "anthropic",
    model: "claude-sonnet-5-5",
    system_prompt: "Keep the reports current.",
  },
  rev: 3,
  hash: "h",
  created_by: "ada",
  created_at: "2026-11-20T00:00:00Z",
} as Revision;
const control = { paused: false, version: 2, updated_by: "ada" } as AgentControl;

test("every section is in exactly one group, and each keeps its own address", () => {
  const all = SETTINGS_GROUPS.flatMap((g) => g.tabs);
  assert.equal(new Set(all).size, all.length);
  assert.deepEqual(
    SETTINGS_GROUPS.map((g) => g.id),
    ["general", "access", "tools", "behavior"],
  );
  assert.equal(groupOf("slack"), "access");
  assert.equal(groupOf("memory"), "behavior");
  const picked: string[] = [];
  render(
    <AgentSettingsNav
      name="barbara"
      tab="general"
      onTab={(t) => picked.push(t)}
      has={(t) => t !== "memory"}
      counts={{ queue: 2 }}
    />,
  );
  const nav = screen.getByRole("navigation", { name: "Agent settings" });
  assert.ok(nav.textContent?.includes("Access"));
  assert.ok(nav.textContent?.includes("Behavior"));
  assert.ok(!nav.textContent?.includes("Memory"));
  const slack = screen.getByRole("link", { name: "Slack" }) as HTMLAnchorElement;
  assert.equal(slack.getAttribute("href"), "/agents/barbara/slack");
  assert.equal(screen.getByRole("link", { name: "General" }).getAttribute("aria-current"), "page");
  fireEvent.click(slack);
  // On a phone the sections are one picker.
  fireEvent.change(screen.getByRole("combobox", { name: "Section" }), {
    target: { value: "prompt" },
  });
  assert.deepEqual(picked, ["slack", "prompt"]);
});

test("General shows the agent, what it completed, its Slack app and its instructions", () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  const opened: string[] = [];
  render(
    <AgentGeneral
      revision={barbara}
      canWrite
      onTab={(t) => opened.push(t)}
      onRetired={() => {}}
      seed={{ completed: 12, control, slack: null }}
    />,
  );
  assert.equal(screen.getByTestId("general-name").textContent, "Barbara");
  const outcome = screen.getByTestId("general-outcome") as HTMLAnchorElement;
  assert.equal(outcome.textContent, "12 runs completed this week");
  assert.match(String(outcome.getAttribute("href")), /agent=barbara/);
  assert.ok(screen.getByText("Answers through the workspace's Slack app"));
  fireEvent.click(screen.getByRole("button", { name: "Set Up" }));
  fireEvent.click(screen.getByRole("button", { name: "Edit" }));
  assert.deepEqual(opened, ["slack", "prompt"]);
});

test("the name is renamed in place, and a taken one is said plainly", async () => {
  setAgentLooks({ barbara: { display_name: "Barbara", shape: "ghost" } });
  const sent: unknown[] = [];
  net = stubFetch({
    "PUT /api/agents/barbara/look": (body) => {
      sent.push(body);
      const b = body as { look: { display_name?: string } };
      if (b.look.display_name === "Lead")
        return new Response(JSON.stringify({ error: "taken" }), { status: 409 });
      return { agent: "barbara", look: b.look, hue: 0, taken: [], choices: {} };
    },
  });
  render(
    <AgentGeneral
      revision={barbara}
      canWrite
      onTab={() => {}}
      onRetired={() => {}}
      seed={{ completed: 0, control, slack: null }}
    />,
  );
  assert.ok(screen.getByText("No runs completed this week"));
  fireEvent.click(screen.getByRole("button", { name: "Rename" }));
  const box = screen.getByRole("textbox", { name: "Display name" });
  fireEvent.change(box, { target: { value: "Lead" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  assert.ok(await screen.findByText("Another agent is called that. Pick another name."));
  fireEvent.change(box, { target: { value: "Barb" } });
  fireEvent.click(screen.getByRole("button", { name: "Save" }));
  await waitFor(() => assert.equal(screen.getByTestId("general-name").textContent, "Barb"));
  assert.deepEqual(sent[1], { look: { display_name: "Barb", shape: "ghost" } });
});

test("Pause stops it and Resume undoes it; Retire asks first and then retires it", async () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  const calls: { method: string; path: string; body?: unknown }[] = [];
  net = stubFetch({
    "PUT /api/agents/barbara/control": (body) => {
      calls.push({ method: "PUT", path: "control", body });
      return { ...control, paused: (body as { paused: boolean }).paused, version: 3 };
    },
    "DELETE /api/agents/barbara": (_b, q) => {
      calls.push({ method: "DELETE", path: `barbara?rev=${q.get("rev")}` });
      return undefined;
    },
  });
  let retired = 0;
  render(
    <AgentGeneral
      revision={barbara}
      canWrite
      onTab={() => {}}
      onRetired={() => retired++}
      seed={{ completed: 0, control, slack: "slack-barbara" }}
    />,
  );
  assert.ok(screen.getByText("Answers as slack-barbara"));
  assert.ok(screen.getByText(/Stop Barbara from answering/));
  fireEvent.click(screen.getByRole("button", { name: "Pause" }));
  assert.ok(await screen.findByRole("button", { name: "Resume" }));
  assert.ok(screen.getByText(/Barbara is paused and answers nobody/));
  fireEvent.click(screen.getByRole("button", { name: "Retire" }));
  assert.ok(screen.getByText("Retire Barbara?"));
  fireEvent.click(screen.getByRole("button", { name: "Keep It" }));
  assert.equal(retired, 0);
  fireEvent.click(screen.getByRole("button", { name: "Retire" }));
  fireEvent.click(screen.getByRole("button", { name: "Retire" }));
  await waitFor(() => assert.equal(retired, 1));
  assert.deepEqual(calls, [
    { method: "PUT", path: "control", body: { paused: true, version: 2 } },
    { method: "DELETE", path: "barbara?rev=3" },
  ]);
});

test("the platform's own agent can be paused but never retired, and a reader changes nothing", () => {
  const view = render(
    <AgentGeneral
      revision={barbara}
      canWrite
      builtin
      onTab={() => {}}
      onRetired={() => {}}
      seed={{ completed: 0, control, slack: null }}
    />,
  );
  assert.ok(
    screen.getByText("The platform writes this agent again, so it can be paused, not retired."),
  );
  assert.equal(screen.queryByRole("button", { name: "Retire" }), null);
  assert.ok(screen.getByRole("button", { name: "Pause" }));
  view.rerender(
    <AgentGeneral
      revision={barbara}
      canWrite={false}
      onTab={() => {}}
      onRetired={() => {}}
      seed={{ completed: 0, control, slack: null }}
    />,
  );
  assert.equal(screen.queryByRole("button", { name: "Pause" }), null);
  assert.equal(screen.queryByRole("button", { name: "Rename" }), null);
  assert.ok(screen.getByText("Only the agent's owners can change these."));
});

test("Retire's confirmation takes keyboard focus, and Keep It gives it back", () => {
  render(
    <AgentGeneral
      revision={barbara}
      canWrite
      onTab={() => {}}
      onRetired={() => {}}
      seed={{ completed: 0, control, slack: null }}
    />,
  );
  // Nothing takes focus until the confirmation is asked for.
  assert.equal(document.activeElement, document.body);
  const retire = screen.getByRole("button", { name: "Retire" });
  retire.focus();
  fireEvent.click(retire);
  // The answer that changes nothing holds focus, so Enter twice retires nothing.
  assert.equal(document.activeElement, screen.getByRole("button", { name: "Keep It" }));
  fireEvent.click(screen.getByRole("button", { name: "Keep It" }));
  assert.equal(document.activeElement, screen.getByRole("button", { name: "Retire" }));
});

test("a failed read of whether it is paused says so, and Retry reads it again", async () => {
  let reads = 0;
  net = stubFetch({
    "GET /api/agents/barbara/control": () =>
      ++reads === 1 ? new Response(JSON.stringify({ error: "boom" }), { status: 503 }) : control,
    "GET /api/agents/barbara/slack-app": () => ({ agent: "barbara" }),
    "GET /api/analytics": () => ({ by_agent: [] }),
  });
  render(<AgentGeneral revision={barbara} canWrite onTab={() => {}} onRetired={() => {}} />);
  const said = await screen.findByRole("alert");
  assert.match(
    said.textContent ?? "",
    /^Whether barbara is paused could not be read, so it cannot be paused or resumed yet\./i,
  );
  assert.equal((screen.getByRole("button", { name: "Pause" }) as HTMLButtonElement).disabled, true);
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: "Pause" }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  assert.equal(screen.queryByRole("alert"), null);
  assert.equal(reads, 2);
});
