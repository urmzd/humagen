import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Automations } from "./Automations.tsx";
import { stubFetch } from "./dom.ts";

const can = () => true;
const agents = [
  {
    rev: 1,
    spec: { name: "lead", provider: "offline", model: "offline", system_prompt: "Help." },
    created_at: "2026-11-01T00:00:00Z",
    created_by: "owner",
    hash: "one",
  },
];
const client = {
  id: "github",
  name: "GitHub",
  surface: "github",
  identity: { owners: ["acme"] },
  default_agent: "lead",
  enabled: true,
  version: 1,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
  github: { auto_labels: { "acme/app": { "agent:review": "lead" } } },
};
afterEach(cleanup);

test("one New Schedule makes a GitHub event automation through the installation API, keeping other settings", async () => {
  let saved: Record<string, unknown> | undefined;
  const { restore } = stubFetch({
    "GET /api/clients": () => ({ clients: [client], caps: {} }),
    "PUT /api/clients/github": (body) => {
      saved = body as Record<string, unknown>;
      return { ...client, ...saved };
    },
  });
  try {
    render(
      <Automations
        schedules={[]}
        agents={agents}
        projects={[]}
        can={can}
        onChanged={() => {}}
        onSchedule={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "New Schedule" }));
    fireEvent.click(screen.getByRole("button", { name: "On a GitHub event" }));
    await screen.findByLabelText("Repository");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "failed-check" } });
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "acme/app" } });
    fireEvent.click(screen.getByRole("button", { name: /^Target agent:/ }));
    fireEvent.click(screen.getByRole("option", { name: "lead" }));
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "Investigate this failure." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Schedule" }));
    await waitFor(() => assert.ok(saved));
    const github = saved?.github as { automations: unknown[]; auto_labels: unknown };
    assert.deepEqual(github.automations, [
      {
        name: "failed-check",
        repository: "acme/app",
        event: "check_run",
        action: "completed",
        conclusion: "failure",
        agent: "lead",
        instructions: "Investigate this failure.",
        paused: false,
        deliver: "github",
      },
    ]);
    assert.deepEqual(github.auto_labels, client.github.auto_labels);
  } finally {
    restore();
  }
});

test("readers see no create buttons and cannot load installations without read permission", () => {
  const { restore, seen } = stubFetch({});
  try {
    render(
      <Automations
        schedules={[]}
        agents={[]}
        projects={[]}
        can={() => false}
        onChanged={() => {}}
        onSchedule={() => {}}
      />,
    );
    assert.equal(seen.length, 0);
    assert.ok(screen.getByRole("heading", { name: "Scheduled", level: 1 }));
    assert.equal(screen.queryByRole("link", { name: "Open Sessions" }), null);
    assert.equal(screen.queryByRole("button", { name: "New Schedule" }), null);
    assert.ok(screen.getByText("Nothing scheduled yet"));
  } finally {
    restore();
  }
});

test("an automation can send its answers to a Slack channel, and its card says where", async () => {
  let saved: Record<string, unknown> | undefined;
  const slackApp = {
    ...client,
    id: "slack-app",
    name: "Team Slack",
    surface: "slack",
    github: undefined,
  };
  let github: Record<string, unknown> = client;
  const { restore } = stubFetch({
    "GET /api/clients": () => ({ clients: [github, slackApp], caps: {} }),
    "PUT /api/clients/github": (body) => {
      saved = body as Record<string, unknown>;
      github = { ...client, ...saved };
      return github;
    },
  });
  try {
    render(
      <Automations
        schedules={[]}
        agents={agents}
        projects={[]}
        can={can}
        onChanged={() => {}}
        onSchedule={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: "New Schedule" }));
    fireEvent.click(screen.getByRole("button", { name: "On a GitHub event" }));
    await screen.findByLabelText("Repository");
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "failed-check" } });
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "acme/app" } });
    fireEvent.click(screen.getByRole("button", { name: /^Target agent:/ }));
    fireEvent.click(screen.getByRole("option", { name: "lead" }));
    fireEvent.click(screen.getByRole("button", { name: /^Send to:/ }));
    fireEvent.click(screen.getByRole("option", { name: /A Slack channel/ }));
    fireEvent.change(screen.getByLabelText("Slack channel id"), { target: { value: "c0alerts1" } });
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "Investigate this failure." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Schedule" }));
    await waitFor(() => assert.ok(saved));
    const [rule] = (saved?.github as { automations: Record<string, unknown>[] }).automations;
    assert.equal(rule?.deliver, "slack");
    assert.equal(rule?.slack_client, "slack-app");
    assert.equal(rule?.slack_channel, "C0ALERTS1");
    assert.ok(await screen.findByText("Slack C0ALERTS1 via Team Slack"));
  } finally {
    restore();
  }
});
