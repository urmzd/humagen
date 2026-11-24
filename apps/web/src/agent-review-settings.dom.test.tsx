import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { AgentDetail } from "./AgentDetail.tsx";
import type { AgentSpec, Revision } from "./api.ts";
import { stubFetch } from "./dom.ts";

const review = defaultCopy.agentReview;
const loadable = defaultCopy.agentLoadableSkills;

// Fields the two controls never touch, which a save must send back as they were.
const others = {
  description: "Reviews pull requests",
  tools: ["github_read_context"],
  access: { spawn: ["helper"] },
  approvals: ["github_read_context"],
  tagged_only: true,
} satisfies Partial<AgentSpec>;

function revisionOf(extra: Partial<AgentSpec> = {}): Revision {
  return {
    spec: {
      name: "reviewer",
      provider: "anthropic",
      model: "claude-sonnet-5-5",
      system_prompt: "Review the change.",
      ...others,
      ...extra,
    },
    rev: 4,
    hash: "revision4",
    created_by: "ada",
    created_at: "2026-11-20T00:00:00Z",
  };
}

const skill = (name: string, description: string, scope: object) => ({
  id: `skill-${name}`,
  name,
  description,
  scope,
  version: 1,
  published_at: "2026-11-01T00:00:00Z",
  by: "ada",
  at: "2026-11-01T00:00:00Z",
  tests: [],
});

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

function open({
  tab,
  extra,
  canWrite = true,
  permissions = ["agents:read", "agents:write", "projects:read"],
}: {
  tab: "review" | "context";
  extra?: Partial<AgentSpec>;
  canWrite?: boolean;
  permissions?: string[];
}) {
  const revision = revisionOf(extra);
  const saved: { spec: AgentSpec; expected_rev: number }[] = [];
  const scopes: string[] = [];
  net = stubFetch({
    "GET /api/agents/reviewer/history": () => ({ revisions: [revision] }),
    "GET /api/agents/reviewer/skills": () => ({
      agent: "reviewer",
      agent_revision: 4,
      version: 1,
      skills: [],
    }),
    "GET /api/me": () => ({ principal: { subject: "ada" }, permissions }),
    "GET /api/projects": () => ({
      projects: [{ name: "platform", title: "Platform", kind: "team" }],
      next: null,
    }),
    "GET /api/library/skills": (_, query) => {
      scopes.push(`${query.get("scope")}:${query.get("project") ?? ""}`);
      return query.get("scope") === "project"
        ? { skills: [skill("deploy-check", "Check a deploy", { kind: "project" })] }
        : {
            skills: [
              skill("secure-review", "Read a change for unsafe patterns", { kind: "workspace" }),
              skill("go-style", "Go conventions", { kind: "workspace" }),
            ],
          };
    },
    "PUT /api/agents/reviewer": (body) => {
      saved.push(body as (typeof saved)[number]);
      return { ...revision, rev: 5, spec: saved.at(-1)?.spec };
    },
  });
  const noop = () => {};
  render(
    <AgentDetail
      revision={revision}
      tab={tab}
      home="platform"
      agents={[revision]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      canWrite={canWrite}
      builtin={false}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
    />,
  );
  return { saved, scopes };
}

const save = () => fireEvent.click(screen.getByRole("button", { name: "Save as Revision 5" }));
const select = (label: string) => screen.getByLabelText(label) as HTMLSelectElement;

test("review settings default to never approving and blocking on errors", () => {
  open({ tab: "review" });
  assert.equal(select(review.approve).value, "never");
  assert.equal(select(review.blockOn).value, "error");
  assert.ok(screen.getByText(review.approveHint.never));
  assert.ok(screen.getByText(review.blockOnHint));
  assert.equal(
    (screen.getByRole("button", { name: "Save as Revision 5" }) as HTMLButtonElement).disabled,
    true,
    "nothing is unsaved until a choice changes",
  );
});

test("review settings save approve and block_on with the revision and keep every other field", async () => {
  const { saved } = open({ tab: "review", extra: { skills: { loadable: ["go-style"] } } });
  fireEvent.change(select(review.approve), { target: { value: "when_clean" } });
  assert.ok(screen.getByText(review.approveHint.when_clean));
  fireEvent.change(select(review.blockOn), { target: { value: "warning" } });
  save();
  await waitFor(() => assert.equal(saved.length, 1));
  assert.equal(saved[0]?.expected_rev, 4);
  assert.deepEqual(saved[0]?.spec, {
    ...revisionOf().spec,
    skills: { loadable: ["go-style"] },
    review: { approve: "when_clean", block_on: "warning" },
  });
});

test("choosing the defaults again leaves the review policy out and keeps the rest", async () => {
  const { saved } = open({
    tab: "review",
    extra: { review: { approve: "when_clean", block_on: "info" } },
  });
  assert.equal(select(review.approve).value, "when_clean");
  assert.equal(select(review.blockOn).value, "info");
  fireEvent.change(select(review.approve), { target: { value: "never" } });
  fireEvent.change(select(review.blockOn), { target: { value: "error" } });
  save();
  await waitFor(() => assert.equal(saved.length, 1));
  assert.deepEqual(saved[0]?.spec, revisionOf().spec);
});

test("without the right to change the agent, its review settings are shown and cannot change", () => {
  open({ tab: "review", canWrite: false, extra: { review: { approve: "when_clean" } } });
  assert.equal(select(review.approve).value, "when_clean");
  assert.ok(select(review.approve).disabled);
  assert.ok(select(review.blockOn).disabled);
});

test("loadable skills start empty and offer the workspace's and the home project's library skills", async () => {
  const { scopes } = open({ tab: "context" });
  const section = screen.getByRole("region", { name: loadable.title });
  assert.ok(within(section).getByText(loadable.empty));
  assert.ok(within(section).getByText(loadable.hint));
  fireEvent.click(
    await within(section).findByRole("button", { name: `${loadable.add}: ${loadable.title}` }),
  );
  await screen.findByRole("button", { name: /^deploy-check/ });
  assert.ok(screen.getByRole("button", { name: /^go-style/ }));
  assert.ok(screen.getByRole("button", { name: /^secure-review/ }));
  assert.ok(scopes.includes("workspace:"));
  assert.ok(scopes.includes("project:platform"));
});

test("a picked loadable skill saves to skills.loadable and keeps every other field", async () => {
  const { saved } = open({ tab: "context", extra: { review: { approve: "when_clean" } } });
  const section = screen.getByRole("region", { name: loadable.title });
  fireEvent.click(
    await within(section).findByRole("button", { name: `${loadable.add}: ${loadable.title}` }),
  );
  fireEvent.click(await screen.findByRole("button", { name: /^secure-review/ }));
  assert.ok(within(section).getByRole("list", { name: loadable.title }));
  save();
  await waitFor(() => assert.equal(saved.length, 1));
  assert.deepEqual(saved[0]?.spec, {
    ...revisionOf().spec,
    review: { approve: "when_clean" },
    skills: { loadable: ["secure-review"] },
  });
});

test("removing the last loadable skill leaves skills out, and a name outside these libraries is kept", async () => {
  const { saved } = open({
    tab: "context",
    extra: { skills: { loadable: ["go-style", "release-notes"] } },
  });
  const section = screen.getByRole("region", { name: loadable.title });
  await within(section).findByText(loadable.elsewhere);
  fireEvent.click(within(section).getByRole("button", { name: "Remove go-style" }));
  save();
  await waitFor(() => assert.equal(saved.length, 1));
  assert.deepEqual(saved[0]?.spec.skills, { loadable: ["release-notes"] });
  fireEvent.click(within(section).getByRole("button", { name: "Remove release-notes" }));
  fireEvent.click(screen.getByRole("button", { name: "Save as Revision 5" }));
  await waitFor(() => assert.equal(saved.length, 2));
  assert.deepEqual(saved[1]?.spec, revisionOf().spec);
});

test("without access to projects, saved loadable skills are shown and kept", async () => {
  open({
    tab: "context",
    permissions: ["agents:read", "agents:write"],
    extra: { skills: { loadable: ["go-style"] } },
  });
  const section = screen.getByRole("region", { name: loadable.title });
  await within(section).findByText(loadable.unreadable);
  assert.ok(within(section).getByText("go-style"));
  assert.equal(within(section).queryByText("No longer exists."), null);
});
