import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentDefaultSkills, PublishedSkillPicker } from "./AgentDefaultSkills.tsx";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const copy = defaultCopy.agentSkillDefaults;
const published = {
  id: "skill-checklist",
  name: "checklist",
  description: "Review releases",
  instructions: "Use checks",
  tools: [],
  scope: { kind: "workspace" },
  version: 1,
  published_at: "2026-10-26T12:00:00Z",
  by: "ada",
  at: "2026-10-26T12:00:00Z",
  tests: [],
};
const draft = { ...published, version: 2, published_at: undefined, instructions: "Draft only" };
function routes(extra: Parameters<typeof stubFetch>[0] = {}) {
  return {
    "GET /api/me": () => ({
      principal: { subject: "ada" },
      permissions: ["agents:read", "agents:write", "projects:read"],
    }),
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents/lead/skills": () => ({
      agent: "lead",
      agent_revision: 3,
      version: 2,
      skills: [],
    }),
    "GET /api/library/skills": () => ({ skills: [draft] }),
    "GET /api/library/skills/skill-checklist": () => ({ revisions: [draft, published] }),
    ...extra,
  };
}
test("default skill editor saves exact published revision with agent and binding CAS", async () => {
  let input: unknown;
  net = stubFetch(
    routes({
      "PUT /api/agents/lead/skills": (body) => {
        input = body;
        return {
          agent: "lead",
          agent_revision: 3,
          version: 3,
          skills: (body as { skills: unknown }).skills,
        };
      },
    }),
  );
  render(<AgentDefaultSkills agent="lead" agentRevision={3} canWrite />);
  const choice = await screen.findByRole("checkbox", { name: /checklist Version 1/ });
  assert.equal(screen.queryByRole("checkbox", { name: /Version 2/ }), null);
  fireEvent.click(choice);
  fireEvent.click(screen.getByRole("button", { name: copy.save }));
  await screen.findByText(copy.saved);
  assert.deepEqual(input, {
    agent_revision: 3,
    version: 2,
    skills: [{ id: "skill-checklist", version: 1 }],
  });
});
test("failed save retains selection and does not claim a persisted binding", async () => {
  net = stubFetch(
    routes({
      "PUT /api/agents/lead/skills": () =>
        new Response(JSON.stringify({ error: "Version changed" }), { status: 409 }),
    }),
  );
  render(<AgentDefaultSkills agent="lead" agentRevision={4} canWrite />);
  const choice = await screen.findByRole("checkbox", { name: /checklist Version 1/ });
  await screen.findByText(copy.stale);
  fireEvent.click(choice);
  fireEvent.click(screen.getByRole("button", { name: copy.save }));
  await screen.findByText("Version changed");
  assert.equal((choice as HTMLInputElement).checked, true);
  assert.equal(screen.queryByText(copy.saved), null);
  fireEvent.click(screen.getByRole("button", { name: copy.reload }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("checkbox", { name: /checklist Version 1/ }) as HTMLInputElement).checked,
      false,
    ),
  );
});
test("read-only default skills offer no mutation and keep exact saved revision", async () => {
  net = stubFetch(
    routes({
      "GET /api/agents/lead/skills": () => ({
        agent: "lead",
        agent_revision: 3,
        version: 2,
        skills: [{ id: "skill-checklist", version: 1 }],
      }),
    }),
  );
  render(<AgentDefaultSkills agent="lead" agentRevision={3} />);
  const choice = await screen.findByRole("checkbox", { name: /checklist Version 1/ });
  assert.equal((choice as HTMLInputElement).disabled, true);
  assert.equal(screen.queryByRole("button", { name: copy.save }), null);
  assert.match(
    screen.getByRole("link", { name: defaultCopy.agentSkillDefaults.inspect("checklist",1) }).getAttribute("href") ?? "",
    /scope=workspace/,
  );
  const link=screen.getByRole("link",{name:copy.inspect("checklist",1)});
  assert.equal(link.getAttribute("target"),"_blank");
  assert.ok(link.getAttribute("href")?.includes("version=1"));
  assert.equal(
    net.seen.some((r) => r.method !== "GET"),
    false,
  );
});

test("unsaved-agent picker only reads published skills and emits exact references", async () => {
  let selected: unknown;
  net = stubFetch(routes());
  render(
    <PublishedSkillPicker
      home=""
      refs={[]}
      onChange={(refs) => {
        selected = refs;
      }}
    />,
  );
  fireEvent.click(await screen.findByRole("checkbox", { name: /checklist Version 1/ }));
  assert.deepEqual(selected, [{ id: "skill-checklist", version: 1 }]);
  assert.equal(
    net.seen.some((r) => r.path.startsWith("/api/agents/") || r.method !== "GET"),
    false,
  );
});
