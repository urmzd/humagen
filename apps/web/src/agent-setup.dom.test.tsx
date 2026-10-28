import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AgentSetupWizard } from "./AgentSetup.tsx";
import type { Me } from "./api.ts";
import { stubFetch } from "./dom.ts";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});
const me = {
  principal: { tenant: "acme", subject: "ada", kind: "user", name: "Ada", roles: [] },
  permissions: ["agents:write", "projects:read"],
  personal_project: "personal-opaque",
} as Me;
function mount() {
  return render(
    <AgentSetupWizard
      me={me}
      agents={[]}
      projects={[]}
      sources={[]}
      policies={[]}
      canBill={false}
      onSaved={() => {}}
      onActivated={() => {}}
      onBack={() => {}}
      onModels={() => {}}
    />,
  );
}
test("setup keeps the personal home selected before project hydration and saves an inactive real-provider draft", async () => {
  let posted: unknown;
  const published = {
    id: "published-checklist",
    name: "checklist",
    description: "Review the evidence",
    instructions: "Review all evidence",
    tools: [],
    scope: { kind: "workspace" },
    version: 1,
    published_at: "2026-10-01T00:00:00Z",
    by: "ada",
    at: "2026-10-01T00:00:00Z",
    tests: [],
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/action-policy": () => ({ rules: {}, effective: {} }),
    "GET /api/me/action-policy": () => ({ rules: {}, effective: {} }),
    "GET /api/projects/personal-opaque/action-policy": () => ({
      rules: {},
      effective: {
        write_files: {
          level: "never",
          locked: true,
          sources: [{ kind: "workspace", label: "Workspace", level: "never" }],
        },
      },
    }),
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/library/skills": () => ({
      skills: [{ ...published, version: 2, published_at: undefined }],
    }),
    "GET /api/library/skills/published-checklist": () => ({
      revisions: [published, { ...published, version: 2, published_at: undefined }],
    }),
    "GET /api/people": () => ({ people: [{ subject: "ben", handle: "ben", name: "Ben" }] }),
    "GET /api/model-providers": () => ({ providers: [{ name: "anthropic", declared: true }] }),
    "POST /api/agent-setups": (body) => {
      posted = body;
      return {
        plan: body,
        name: "helper",
        owner: "ada",
        phase: "configured",
        version: 1,
        revision: 1,
        revision_hash: "hash",
      };
    },
  });
  mount();
  const project = screen.getByLabelText(
    new RegExp(defaultCopy.agentSetup.project),
  ) as HTMLSelectElement;
  assert.equal(project.value, me.personal_project);
  assert.equal(project.selectedOptions[0]?.textContent, defaultCopy.projects.personalTitle);
  fireEvent.change(screen.getByLabelText(defaultCopy.agentSetup.template), {
    target: { value: "writer" },
  });
  fireEvent.change(screen.getByLabelText(new RegExp(defaultCopy.agentSetup.name)), {
    target: { value: "helper" },
  });
  await waitFor(() => assert.ok(screen.getByRole("option", { name: "anthropic" })));
  fireEvent.change(screen.getByLabelText(new RegExp(defaultCopy.agentSetup.provider)), {
    target: { value: "anthropic" },
  });
  fireEvent.change(screen.getByLabelText(defaultCopy.agentSetup.model), {
    target: { value: "claude-haiku-5-5" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.agentSetup.next }));
  fireEvent.click(await screen.findByRole("checkbox", { name: /checklist Version 1/ }));
  await waitFor(() =>
    assert.ok(
      (
        screen.getByRole("combobox", {
          name: defaultCopy.agentSetup.categories.write_files,
        }) as HTMLSelectElement
      ).disabled,
    ),
  );
  assert.equal(
    (
      screen.getByRole("combobox", {
        name: defaultCopy.agentSetup.categories.write_files,
      }) as HTMLSelectElement
    ).value,
    "never",
  );
  fireEvent.change(screen.getByLabelText(new RegExp(defaultCopy.agentSetup.manager)), {
    target: { value: "Ben" },
  });
  await waitFor(() =>
    assert.ok(
      net?.seen.some((request) => request.path.startsWith("/api/people?")),
      JSON.stringify(net?.seen),
    ),
  );
  fireEvent.click(await screen.findByRole("button", { name: "Ben" }));
  fireEvent.click(screen.getByRole("checkbox", { name: defaultCopy.agentSetup.selfApproval }));
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.agentSetup.next }));
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.agentSetup.save }));
  await waitFor(() => assert.ok(posted));
  assert.equal((posted as { project: string }).project, me.personal_project);
  assert.equal((posted as { spec: { provider: string } }).spec.provider, "anthropic");
  assert.deepEqual((posted as { skills: { id: string; version: number }[] }).skills, [
    { id: "published-checklist", version: 1 },
  ]);
  assert.deepEqual((posted as { spec: { approvers: string[] } }).spec.approvers, ["ada", "ben"]);
  assert.equal((posted as { manager: string }).manager, "ben");
  assert.ok((posted as { spec: { tools: string[] } }).spec.tools.includes("publish_artifact"));
  assert.ok(!(posted as { spec: { tools: string[] } }).spec.tools.includes("publish"));
  await screen.findByRole("button", { name: defaultCopy.agentSetup.test });
  assert.ok(
    (screen.getByRole("button", { name: defaultCopy.agentSetup.activate }) as HTMLButtonElement)
      .disabled,
  );
});

test("People & Access leaves sharing to the header's Share and offers no second Share", async () => {
  const { ConversationAccess } = await import("./ConversationAccess.tsx");
  net = stubFetch({
    "GET /api/me": () => ({
      principal: { subject: "ada" },
      handle: "ada",
      permissions: ["runs:write"],
    }),
    "GET /api/projects/Launch": () => ({ project: { access: { role: "owner" } } }),
  });
  render(
    <ConversationAccess
      thread={{
        id: "thread-1",
        project: "Launch",
        channel: "general",
        title: "Reviewed work",
        visibility: "private",
        participants: [{ kind: "person", name: "ada", owner: true, at: "2026-10-01T00:00:00Z" }],
        created_by: "ada",
        created_at: "2026-10-01T00:00:00Z",
        updated_at: "2026-10-01T00:00:00Z",
        version: 1,
      }}
    />,
  );
  await screen.findByRole("button", { name: defaultCopy.conversationAccess.add });
  assert.equal(screen.queryByRole("button", { name: /share/i }), null);
  assert.ok(net.seen.every((request) => request.method === "GET"));
});
