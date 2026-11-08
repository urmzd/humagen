import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { Automations } from "./Automations.tsx";
import { stubFetch } from "./dom.ts";

const can = () => true;
const agents = [
  {
    rev: 1,
    spec: {
      name: "lead",
      provider: "anthropic",
      model: "claude-haiku-5-5",
      system_prompt: "Help.",
    },
    created_at: "2026-11-01T00:00:00Z",
    created_by: "owner",
    hash: "one",
  },
];
const connection = (id: string, surface: "slack" | "github") => ({
  id,
  name: id,
  surface,
  identity: surface === "slack" ? { team: "T0TEAM1" } : { owners: ["acme"] },
  enabled: true,
  version: 1,
  created_at: "2026-11-01T00:00:00Z",
  updated_at: "2026-11-01T00:00:00Z",
});
afterEach(cleanup);

test("Scheduled is schedules only: one New Schedule, no GitHub event rules and no inline links", () => {
  const { restore } = stubFetch({});
  try {
    render(
      <Automations
        schedules={[]}
        agents={[]}
        projects={[]}
        can={can}
        onChanged={() => {}}
        onSchedule={() => {}}
      />,
    );
    assert.ok(screen.getByRole("heading", { name: "Scheduled", level: 1 }));
    assert.ok(screen.getByRole("button", { name: "New Schedule" }));
    assert.equal(screen.queryByRole("button", { name: /GitHub/ }), null);
    assert.equal(screen.queryByRole("link", { name: "Open Sessions" }), null);
    assert.ok(screen.getByText("Nothing scheduled yet"));
  } finally {
    restore();
  }
});

test("readers see no New Schedule", () => {
  const { restore } = stubFetch({});
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
    assert.equal(screen.queryByRole("button", { name: "New Schedule" }), null);
  } finally {
    restore();
  }
});

test("a schedule sends its answer to a Slack channel through the connection picked", async () => {
  let saved: Record<string, unknown> | undefined;
  const { restore } = stubFetch({
    "GET /api/clients": () => ({
      clients: [connection("team-slack", "slack"), connection("acme-github", "github")],
      caps: {},
    }),
    "POST /api/schedules": (body) => {
      saved = body as Record<string, unknown>;
      return saved;
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
    fireEvent.change(screen.getByLabelText("Name"), { target: { value: "nightly" } });
    fireEvent.change(screen.getByLabelText("What to ask"), {
      target: { value: "What failed overnight?" },
    });
    fireEvent.click(await screen.findByRole("button", { name: /^Send to:/ }));
    fireEvent.click(await screen.findByRole("option", { name: /A Slack channel/ }));
    fireEvent.change(screen.getByLabelText("Slack channel id"), { target: { value: "c0alerts1" } });
    fireEvent.click(screen.getByRole("button", { name: "Create Schedule" }));
    await waitFor(() => assert.ok(saved));
    assert.equal(saved?.deliver, "slack");
    assert.equal(saved?.deliver_client, "team-slack");
    assert.equal(saved?.deliver_to, "C0ALERTS1");
  } finally {
    restore();
  }
});

test("a schedule's card says where its answer goes", () => {
  const { restore } = stubFetch({});
  try {
    render(
      <Automations
        schedules={[
          {
            name: "nightly",
            expression: "0 9 * * *",
            agent: "lead",
            text: "Report.",
            deliver: "github",
            deliver_client: "acme-github",
            deliver_to: "acme/app#12",
            paused: false,
            next: [],
            recent: [],
          },
        ]}
        agents={agents}
        projects={[]}
        can={can}
        onChanged={() => {}}
        onSchedule={() => {}}
      />,
    );
    assert.ok(screen.getByText("GitHub acme/app#12"));
    assert.ok(screen.getByText("Active"));
  } finally {
    restore();
  }
});
