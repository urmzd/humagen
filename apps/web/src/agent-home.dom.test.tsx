import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentHomeTab, startersOf } from "./AgentHomeTab.tsx";
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
} as Revision;
const ready = { paused: false, version: 1, updated_by: "ada" } as AgentControl;

test("an agent's home introduces it in its own voice and offers its starters as sessions", () => {
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
  render(
    <AgentHomeTab
      revision={barbara}
      stats={{ name: "barbara", runs: 3, completed: 2, failed: 1 } as never}
      canWrite
      seed={{ control: ready }}
    />,
  );
  assert.ok(screen.getByText("Hi, I'm Barbara."));
  assert.equal(
    screen.getByTestId("agent-home-greeting").textContent,
    "I keep customer performance reports current.",
  );
  assert.ok(screen.getByRole("heading", { name: "Put Barbara to work" }));
  const starter = screen
    .getByText("Summarize the latest reports")
    .closest("a") as HTMLAnchorElement;
  assert.match(String(starter.getAttribute("href")), /agent=barbara/);
  assert.match(String(starter.getAttribute("href")), /ask=Summarize/);
  assert.ok(screen.getByText("3 runs in the last day, 1 failed"));
  // The composer is addressed to the agent.
  assert.ok(screen.getByPlaceholderText("Ask Barbara to…"));
  assert.equal(screen.queryByTestId("agent-home-paused"), null);
});

test("with no starters kept, the home offers its default skills as slash commands", () => {
  assert.deepEqual(
    startersOf(undefined, [
      { name: "weekly-report", description: "Writes the report. Then posts it." },
    ]),
    [{ title: "weekly-report", prompt: "/weekly-report ", line: "Writes the report." }],
  );
  assert.deepEqual(startersOf([{ title: "", prompt: "Do it" }], []), [
    { title: "Do it", prompt: "Do it", line: "Do it" },
  ]);
  setAgentLooks({}, ["barbara"]);
  render(
    <AgentHomeTab
      revision={barbara}
      canWrite={false}
      seed={{ skills: [{ name: "triage", description: "Sorts what came in." }], control: ready }}
    />,
  );
  assert.equal(
    screen.getByTestId("agent-home-greeting").textContent,
    "Customer performance reports.",
  );
  const starter = screen.getByText("triage").closest("a") as HTMLAnchorElement;
  assert.match(String(starter.getAttribute("href")), /ask=%2Ftriage/);
});

test("an agent with nothing to offer asks its owner for starters and hides the list from others", () => {
  setAgentLooks({}, ["barbara"]);
  let looked = 0;
  const view = render(
    <AgentHomeTab revision={barbara} canWrite onLook={() => looked++} seed={{ control: ready }} />,
  );
  fireEvent.click(screen.getByRole("button", { name: "Add Starters" }));
  assert.equal(looked, 1);
  view.rerender(<AgentHomeTab revision={barbara} canWrite={false} seed={{ control: ready }} />);
  assert.equal(screen.queryByText("Put barbara to work"), null);
});

test("a paused agent says so on its home", () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  render(
    <AgentHomeTab revision={barbara} canWrite seed={{ control: { ...ready, paused: true } }} />,
  );
  assert.equal(
    screen.getByTestId("agent-home-paused").textContent,
    "Barbara is paused and answers nobody until it is resumed.",
  );
});

test("the home's composer starts a session with the agent and opens it", async () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  const asked: unknown[] = [];
  net = stubFetch({
    "GET /api/agents/barbara/control": () => ready,
    "GET /api/agents/barbara/skills": () => ({
      agent: "barbara",
      agent_revision: 1,
      version: 0,
      skills: [],
    }),
    "POST /api/sessions": (body) => {
      asked.push(body);
      return { id: "s_1" };
    },
  });
  const opened: string[] = [];
  render(<AgentHomeTab revision={barbara} canWrite onOpenThread={(id) => opened.push(id)} />);
  const box = screen.getByPlaceholderText("Ask Barbara to…");
  fireEvent.input(box, { target: { value: "Summarize churn" } });
  fireEvent.change(box, { target: { value: "Summarize churn" } });
  fireEvent.keyDown(box, { key: "Enter" });
  await waitFor(() => assert.deepEqual(opened, ["s_1"]));
  // Homed in the workspace (no home, or "~"), it names no project.
  assert.deepEqual(asked, [{ agent: "barbara", text: "Summarize churn" }]);
});

const ask = (text: string) => {
  const box = screen.getByPlaceholderText("Ask Barbara to…");
  fireEvent.input(box, { target: { value: text } });
  fireEvent.change(box, { target: { value: text } });
  fireEvent.keyDown(box, { key: "Enter" });
};

test("the composer of an agent homed in a project starts the session in that project", async () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  const asked: unknown[] = [];
  net = stubFetch({
    "POST /api/sessions": (body) => {
      asked.push(body);
      return { id: `s_${asked.length}` };
    },
  });
  const opened: string[] = [];
  const view = render(
    <AgentHomeTab
      revision={barbara}
      home="Secret"
      canWrite
      onOpenThread={(id) => opened.push(id)}
      seed={{ control: ready }}
    />,
  );
  ask("Summarize churn");
  await waitFor(() => assert.deepEqual(opened, ["s_1"]));
  // A Personal home is written "~personal-x"; the workspace is "~".
  for (const [home, project] of [
    ["~personal-ada", "personal-ada"],
    ["~", undefined],
  ] as const) {
    view.rerender(
      <AgentHomeTab
        revision={barbara}
        home={home}
        canWrite
        onOpenThread={(id) => opened.push(id)}
        seed={{ control: ready }}
      />,
    );
    ask(`In ${home}`);
    await waitFor(() => assert.equal(asked.length, project ? 2 : 3));
  }
  assert.deepEqual(asked, [
    { agent: "barbara", text: "Summarize churn", project: "Secret" },
    { agent: "barbara", text: "In ~personal-ada", project: "personal-ada" },
    { agent: "barbara", text: "In ~" },
  ]);
});

test("a refused start says why, and a failed one says to try again", async () => {
  setAgentLooks({ barbara: { display_name: "Barbara" } });
  let answer = () =>
    new Response(JSON.stringify({ error: "you read Secret but may not post in it" }), {
      status: 403,
    });
  net = stubFetch({ "POST /api/sessions": () => answer() });
  const opened: string[] = [];
  render(
    <AgentHomeTab
      revision={barbara}
      home="Secret"
      canWrite={false}
      onOpenThread={(id) => opened.push(id)}
      seed={{ control: ready }}
    />,
  );
  ask("Summarize churn");
  const refused = await screen.findByRole("alert");
  assert.equal(
    refused.textContent,
    "The session was not started: you read Secret but may not post in it",
  );
  answer = () => new Response(JSON.stringify({ error: "boom" }), { status: 503 });
  ask("Summarize churn");
  await waitFor(() =>
    assert.equal(
      screen.getByRole("alert").textContent,
      "The session could not be started. Try again.",
    ),
  );
  assert.deepEqual(opened, []);
});
