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

test("the builder saves instructions and conditions through the installation API", async () => {
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
    await screen.findByText(/agent:review/);
    fireEvent.click(screen.getByRole("button", { name: "New GitHub Automation" }));
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "failed-check" } });
    fireEvent.change(screen.getByLabelText("Repository"), { target: { value: "acme/app" } });
    fireEvent.change(screen.getByLabelText("Target agent"), { target: { value: "lead" } });
    fireEvent.change(screen.getByLabelText("Instructions"), {
      target: { value: "Investigate this failure." },
    });
    fireEvent.click(screen.getByRole("button", { name: "Save Automation" }));
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
    assert.equal(screen.queryByRole("button", { name: "New Schedule" }), null);
    assert.equal(screen.queryByRole("button", { name: "New GitHub Automation" }), null);
  } finally {
    restore();
  }
});
