import assert from "node:assert/strict";
import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { AgentDetail, BUILTIN_LOCKED, type Tab } from "./AgentDetail.tsx";
import { Agents } from "./Agents.tsx";
import type { Group, Note, ProjectView, Revision } from "./api.ts";
import { policyWords } from "./Governance.tsx";

const writer: Revision = {
  rev: 2,
  spec: {
    name: "humagen-writer",
    description: "Writes a document.",
    provider: "offline",
    model: "offline",
    system_prompt: "Write it.",
    access: { spawn: [] },
  },
  created_at: new Date().toISOString(),
  created_by: "humagen",
  hash: "h2",
};

const field: ProjectView = {
  name: "Field",
  kind: "team",
  members: [{ who: "*", role: "member" }],
  experts: [],
  agents: { enable: [] },
  access: { role: "owner", from: "", via: "projects:govern" },
  actions: [],
  version: 1,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
  depth: 1,
  path: [{ name: "Field", kind: "team" }],
  children: 0,
  conversations: 0,
  active: 0,
  schedules: 0,
  notes: 0,
  proposed: 0,
  unread: 0,
  row_agents: [],
  mine: false,
};

const older: Revision = {
  ...writer,
  rev: 1,
  hash: "h1",
  spec: { ...writer.spec, system_prompt: "Write." },
};
const notes: Note[] = [
  {
    id: "n1",
    text: "Releases go out on Tuesdays.",
    source: "person",
    status: "approved",
    by: "ada",
    at: new Date().toISOString(),
  },
  {
    id: "n2",
    text: "Ada prefers short plans.",
    source: "consolidated",
    status: "pending",
    at: new Date().toISOString(),
  },
];

const personal: ProjectView = {
  ...field,
  name: "personal-ada",
  title: "Personal",
  personal_owner: "ada",
  path: [{ name: "personal-ada", kind: "team" }],
};

const noop = () => {};
const render = (builtin: boolean, canWrite = true, startOn: Tab = "prompt", home?: string) =>
  renderToStaticMarkup(
    <AgentDetail
      revision={writer}
      agents={[writer]}
      sources={[]}
      suites={[]}
      rules={[]}
      alerts={[]}
      stats={undefined}
      workspaceDefault="humagen-writer"
      tree={[field, personal]}
      home={home}
      personalProject="personal-ada"
      canWrite={canWrite}
      builtin={builtin}
      canEval={false}
      canAlert={false}
      onBack={noop}
      onChanged={noop}
      onRulesChanged={noop}
      onSuitesChanged={noop}
      onOpenRun={noop}
      tab={startOn}
      seed={{ history: [writer, older], notes }}
    />,
  );

test("a built-in agent opens read only, and says why", () => {
  const html = render(true);
  assert.ok(html.includes(BUILTIN_LOCKED), "the explanation is on the page");
  assert.doesNotMatch(html, /Save as Revision/);
  // Every field of the prompt is disabled.
  assert.match(html, /<textarea[^>]*disabled=""[^>]*>Writes a document\./);
  assert.match(html, /id="agent-prompt"[^>]*disabled=""|disabled=""[^>]*id="agent-prompt"/);
});

test("the same agent, not built in, is editable by a writer", () => {
  const html = render(false);
  assert.match(html, /Save as Revision 3/);
  assert.doesNotMatch(html, /agent-locked/);
  assert.doesNotMatch(html, /<textarea[^>]*disabled=""[^>]*>Writes a document\./);
});

test("someone who may not write sees no save, with or without the lock", () => {
  assert.doesNotMatch(render(false, false), /Save as Revision/);
  assert.doesNotMatch(render(false, false), /agent-locked/);
});

const text = (s: string) => s.replace(/<[^>]+>/g, " ").replace(/\s+/g, " ");
// The controls, by what they hold: the model on the Model tab, who sees it on the Access tab.
const model = (html: string) =>
  /<button[^>]*aria-label="Model, primary: offline"[^>]*>/.exec(html)?.[0] ?? "";
const access = (html: string) =>
  /<button[^>]*aria-label="Access: [^"]*"[^>]*>/.exec(html)?.[0] ?? "";

test("A built-in's spec is locked, and it is the workspace's", () => {
  assert.match(model(render(true, true, "model")), /disabled=""/);
  // Every switch of the spec's own is locked.
  const locked = render(true, true, "access");
  const specSwitches = locked.match(/<input type="checkbox" class="humagen-switch[^>]*>/g) ?? [];
  assert.ok(
    specSwitches.length > 0 && specSwitches.every((s) => s.includes('disabled=""')),
    "the spec's switches are locked",
  );
  // The platform's own agents are the workspace's, whatever home is stored.
  assert.match(access(locked), /aria-label="Access: Workspace"[^>]*disabled=""/);
  assert.match(
    access(render(true, true, "access", "Field")),
    /aria-label="Access: Workspace"[^>]*disabled=""/,
  );
  assert.doesNotMatch(model(render(false, true, "model")), /\sdisabled(?:=|\s|>)/);
  assert.doesNotMatch(access(render(false, true, "access")), /disabled=""/);
  // Without the right to write, neither changes.
  assert.match(model(render(false, false, "model")), /disabled=""/);
  assert.match(access(render(false, false, "access")), /disabled=""/);
});

test("Access: who sees the agent follows its home, and nothing says where it is enabled", () => {
  const shown = (home?: string) =>
    /aria-label="Access: ([^"]*)"/.exec(render(false, true, "access", home))?.[1];
  assert.equal(shown(), "Workspace");
  assert.equal(shown("~"), "Workspace");
  assert.equal(shown("personal-ada"), "Private");
  assert.equal(shown("~personal-ada"), "Private");
  assert.equal(shown("Field"), "Members of Field");
  const html = text(render(false, true, "access", "Field"));
  assert.match(html, /Who sees it/);
  assert.doesNotMatch(html, /Where it answers|Lives in|Available (in|everywhere)|enabled in/i);
});

test("Access: the workspace default says so", () => {
  assert.match(text(render(false, true, "prompt")), /Workspace Default/);
  assert.doesNotMatch(text(render(false, true, "prompt")), /Baseline/);
});

test("Access: calls are picked from a list, and approving a call to another agent is not offered", () => {
  const html = text(render(false, true, "access"));
  assert.match(html, /Agents it may call/);
  assert.doesNotMatch(html, /Approve calls to/);
});

test("Revisions: restore is offered for an older revision, never for a built-in", () => {
  assert.match(text(render(false, true, "revisions")), /Restore as Revision 3/);
  assert.match(text(render(true, true, "revisions")), /Revision 1/);
  assert.doesNotMatch(text(render(true, true, "revisions")), /Restore as Revision/);
  assert.doesNotMatch(text(render(false, false, "revisions")), /Restore as Revision/);
});

test("Memory: a writer keeps a built-in's memory, as the server allows; a reader cannot", () => {
  for (const builtin of [true, false]) {
    const html = render(builtin, true, "memory");
    const words = text(html);
    assert.match(words, /Remember/, `builtin ${builtin}`);
    assert.match(words, /Forget/, `builtin ${builtin}`);
    assert.match(words, /Discard All/, `builtin ${builtin}`);
    assert.doesNotMatch(
      /<input type="checkbox" class="mt-1"[^>]*>/.exec(html)?.[0] ?? "",
      /disabled/,
    );
  }
  const reader = render(false, false, "memory");
  assert.doesNotMatch(text(reader), /Remember|Forget|Discard All/);
  assert.match(/<input type="checkbox" class="mt-1"[^>]*>/.exec(reader)?.[0] ?? "", /disabled=""/);
});

test("a call policy reads as a sentence about who may call and who approves", () => {
  assert.equal(
    policyWords({ callers: "anyone", approval: "never" }),
    "Any agent that lists it may call it. Calls run without asking anyone.",
  );
  assert.equal(
    policyWords({ callers: "none", approval: "always" }),
    "No agent may call it. Calls run without asking anyone.",
  );
  assert.equal(
    policyWords({
      callers: "listed",
      allow: ["lead"],
      approval: "always",
      approvers: ["role:manager", "ada"],
      expires_seconds: 14400,
    }),
    "Only lead may call it. Every call waits for Managers, ada to approve it, for up to 4 hours.",
  );
  assert.equal(
    policyWords({
      callers: "anyone",
      approval: "unlisted",
      trusted: ["lead"],
      approvers: [],
      expires_seconds: 86400,
    }),
    "Any agent that lists it may call it. A call from anyone but lead waits for a manager or an admin to approve it, for up to 1 day.",
  );
});

test("the directory keeps empty and populated statistics in the same three fields", () => {
  const group: Group = {
    name: writer.spec.name,
    runs: 4,
    completed: 3,
    failed: 1,
    stopped: 0,
    active: 0,
    completed_alone: 3,
    needed_person: 0,
    human_help_unknown: 0,
    avg_ms: 2000,
    handoffs_out: 0,
    p50_ms: 2000,
    p95_ms: 3000,
  };
  const directory = (stats: Map<string, Group>) =>
    renderToStaticMarkup(
      <Agents
        agents={[writer]}
        stats={stats}
        homes={{ [writer.spec.name]: "~" }}
        canWrite
        onOpen={noop}
        onNew={noop}
      />,
    );
  const empty = directory(new Map());
  const active = directory(new Map([[writer.spec.name, group]]));
  const cells = (html: string) => {
    const body = /<tbody>([\s\S]*?)<\/tbody>/.exec(html)?.[1] ?? "";
    return [...body.matchAll(/<td[^>]*>([\s\S]*?)<\/td>/g)].map((m) => text(m[1]!).trim());
  };
  for (const html of [empty, active]) {
    const headings = [...html.matchAll(/<th\b[^>]*>([\s\S]*?)<\/th>/g)].map((m) =>
      text(m[1]!).trim(),
    );
    assert.deepEqual(headings, ["Agent", "Working", "Waiting", "Runs (24h)", "Success", "Median"]);
    assert.equal((html.match(/<th\b[^>]*scope="col"/g) ?? []).length, 6);
    assert.equal(cells(html).length, headings.length, "every row aligns with the same columns");
    // Every agent reads alike: no Built in tag and no model column; the store says where one came from.
    assert.match(cells(html)[0]!, /^humagen-writer\s*Writes a document\./);
    assert.doesNotMatch(html, /Built in/);
    assert.match(html, /<p[^>]*class="truncate[^"]*">Writes a document\.<\/p>/);
    assert.doesNotMatch(html, /title="Writes a document\."/);
    assert.doesNotMatch(
      html,
      /Lives in|Members of|Private|Built in, read only|No runs in the last/,
    );
    assert.doesNotMatch(html, /humagen-tape|humagen-stroke/);
  }
  assert.deepEqual(cells(empty).slice(-3), ["–", "–", "–"]);
  assert.deepEqual(cells(active).slice(-3), ["4", "75%", "2.0s"]);
});

test("the directory tags an agent with who sees it: Private, or the members of its project", () => {
  const named = (name: string): Revision => ({ ...writer, spec: { ...writer.spec, name } });
  const html = renderToStaticMarkup(
    <Agents
      agents={[named("shared"), named("mine"), named("everyone")]}
      stats={new Map()}
      homes={{ shared: "Field", mine: "personal-ada", everyone: "~" }}
      tree={[field, personal]}
      personalProject="personal-ada"
      canWrite
      onOpen={noop}
      onNew={noop}
    />,
  );
  const row = (name: string) =>
    text(html.split("<tr").find((r) => r.includes(`>${name}</button>`)) ?? "");
  assert.match(row("shared"), /Members of Field/);
  assert.match(row("mine"), /Private/);
  assert.doesNotMatch(row("everyone"), /Members of|Private|Workspace/);
  assert.doesNotMatch(html, /Lives in/);
});
