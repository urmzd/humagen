import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { MemoryLibrary } from "./MemoryLibrary.tsx";
import { NotificationPanel } from "./Notifications.tsx";
import { PersonalAgents } from "./PersonalAgents.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import { SkillsLibrary } from "./SkillsLibrary.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
const me = {
  principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
  permissions: ["projects:read", "projects:write", "runs:write"],
  personal_project: "~personal",
};
const personal = {
  briefing: "",
  notifications: {},
  quiet_start: "",
  quiet_end: "",
  timezone: "UTC",
  remember: false,
  use_personal_memory_in_projects: false,
};

test("editing a skill creates an exact new draft without resubmitting revision metadata", async () => {
  const first = {
    id: "sk-one",
    scope: { kind: "personal", subject: "ada" },
    name: "brief",
    description: "Briefing",
    instructions: "Before",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  let current = first;
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [current] }),
    "GET /api/library/skills/sk-one": () => ({
      revisions: current.version === 1 ? [first] : [first, current],
    }),
    "POST /api/library/skills": (body) => {
      const value = body as { scope: object; definition: typeof first; version: number };
      assert.equal(value.version, 1);
      assert.equal(value.definition.instructions, "After");
      assert.equal("scope" in value.definition, false);
      assert.equal("version" in value.definition, false);
      assert.equal("tests" in value.definition, false);
      current = { ...first, instructions: "After", version: 2 };
      return current;
    },
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /brief.*Version 1/s }));
  fireEvent.change(await screen.findByLabelText("Instructions"), { target: { value: "After" } });
  fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));
  await waitFor(() =>
    assert.equal(net?.seen.filter((request) => request.method === "POST").length, 1),
  );
});

test("personal briefing saves only writable profile fields", async () => {
  const profile = {
    display_name: "Ada",
    theme: "system",
    accent: "purple",
    has_photo: false,
    photo_version: 0,
    pinned: [],
    muted: [],
    personal,
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/preferences": () => profile,
    "PUT /api/preferences": (body) => {
      assert.deepEqual(Object.keys(body as object).sort(), [
        "accent",
        "display_name",
        "personal",
        "theme",
      ]);
      return { ...profile, ...(body as object) };
    },
  });
  let savedName = "";
  render(
    <PersonalPreferences
      section="profile"
      onSaved={(value) => {
        savedName = value.display_name;
      }}
    />,
  );
  fireEvent.change(await screen.findByLabelText(defaultCopy.personalPreferences.displayName), {
    target: { value: "Nadia" },
  });
  fireEvent.change(await screen.findByLabelText(defaultCopy.personalPreferences.briefing), {
    target: { value: "Start with the conclusion" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalPreferences.save }));
  await screen.findByRole("status");
  assert.equal(savedName, "Nadia");
  assert.equal(
    (
      net.seen.find((request) => request.method === "PUT")?.body as {
        personal: { briefing: string };
      }
    ).personal.briefing,
    "Start with the conclusion",
  );
});

test("dismissing a delivered notification acknowledges only its projection", async () => {
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/me/notifications": () => ({
      notifications: [
        {
          id: "ntf-one",
          mode: "digest",
          at: "2026-10-26T12:00:00Z",
          items: [
            {
              inbox: "in-one",
              kind: "mention",
              subject: "Review the release",
              at: "2026-10-26T11:00:00Z",
            },
          ],
        },
      ],
    }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  render(<NotificationPanel />);
  const summary = await screen.findByText("Notifications");
  fireEvent.click(summary);
  await screen.findByText("Review the release");
  fireEvent.click(screen.getByRole("button", { name: "Dismiss Notification" }));
  await waitFor(() => assert.equal(!!screen.queryByText("Review the release"), false));
  assert.deepEqual(
    net.seen.filter((request) => request.method === "POST").map((request) => request.path),
    ["/api/me/notifications/ntf-one/ack"],
  );
});

test("memory correction retains scope and never resubmits source provenance", async () => {
  const original = {
    id: "m-one",
    text: "Ship on Tuesday",
    source: "artifact",
    status: "approved",
    kind: "decision",
    at: "2026-10-26T12:00:00Z",
    source_name: "Release notes",
    source_url: "/conversations/th-one/deliverables/art-one/provenance",
  };
  let current = original;
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/me/memory": () => ({ notes: [current] }),
    "PUT /api/me/memory/m-one": (body) => {
      const input = body as { text: string; kind: string; expires_at: null };
      assert.deepEqual(Object.keys(input).sort(), ["expires_at", "kind", "text"]);
      assert.equal(input.kind, "decision");
      current = { ...original, text: input.text };
      return current;
    },
  });
  render(<MemoryLibrary />);
  const edit = await screen.findByRole("button", { name: "Edit" });
  fireEvent.click(edit);
  assert.equal(
    (screen.getByRole("link", { name: "Open Source" }) as HTMLAnchorElement).getAttribute("href"),
    original.source_url,
  );
  fireEvent.change(screen.getByLabelText(defaultCopy.memoryLibrary.text), {
    target: { value: "Ship on Wednesday" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Save Correction" }));
  await screen.findByText("Ship on Wednesday");
  assert.equal(net.seen.filter((request) => request.method === "PUT").length, 1);
});

test("memory promotion selects an existing note and never copies private provenance", async () => {
  const note = {
    id: "m-private",
    text: "Review quarterly goals",
    source: "consolidated",
    status: "approved",
    kind: "decision",
    by: "private-run",
    at: "2026-10-26T12:00:00Z",
  };
  net = stubFetch({
    "GET /api/me": () => ({ ...me, permissions: [...me.permissions, "projects:write"] }),
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/me/memory": () => ({ notes: [note] }),
    "POST /api/memory/promote": (body) => {
      assert.deepEqual(body, { scope: "personal", owner: "", id: "m-private", destination: "~" });
      return { ...note, id: "m-new", status: "pending", source: "person", by: "ada" };
    },
  });
  render(<MemoryLibrary />);
  await screen.findByText(note.text);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.memoryLibrary.promote }));
  await screen.findByText(defaultCopy.memoryLibrary.promotionSaved);
  assert.equal(net.seen.filter((r) => r.method === "POST").length, 1);
});

test("personal agents may move to an owned file-managed project after audience review", async () => {
  let moved = false;
  net = stubFetch({
    "GET /api/me": () => ({ ...me, permissions: [...me.permissions, "agents:write"] }),
    "GET /api/agents": () => ({
      agents: [{ rev: 1, spec: { name: "briefing-helper", description: "Briefs the owner" } }],
      homes: { "briefing-helper": moved ? "Launch" : "personal" },
    }),
    "GET /api/agents/briefing-helper/control": () => ({
      agent: "briefing-helper",
      version: 0,
      paused: false,
    }),
    "GET /api/projects": () => ({
      projects: [
        {
          name: "Launch",
          title: "Launch",
          access: { role: "owner" },
          actions: ["notes"],
          managed: true,
        },
      ],
      next: null,
    }),
    "PUT /api/agents/briefing-helper/home": (body) => {
      assert.deepEqual(body, { project: "Launch" });
      moved = true;
      return { agent: "briefing-helper", project: "Launch" };
    },
  });
  render(<PersonalAgents />);
  await screen.findByRole("heading", { name: "briefing-helper" });
  fireEvent.change(screen.getByLabelText(defaultCopy.personalAgents.project), {
    target: { value: "Launch" },
  });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalAgents.review }));
  assert.equal(
    net.seen.some((r) => r.method === "PUT"),
    false,
  );
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.personalAgents.confirm }));
  await waitFor(() => assert.equal(moved, true));
});

test("skill deep link opens the pinned published version and manual selection keeps its address", async () => {
  const first = {
    id: "sk-pinned",
    scope: { kind: "workspace" },
    name: "pinned",
    description: "Pinned skill",
    instructions: "Exact published instructions",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    published_at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  const draft = {
    ...first,
    version: 2,
    published_at: undefined,
    instructions: "New draft instructions",
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [draft] }),
    "GET /api/library/skills/sk-pinned": () => ({ revisions: [first, draft] }),
  });
  const initial = window.location.href;
  window.history.replaceState(
    null,
    "",
    "/library/skills?scope=workspace&skill=sk-pinned&version=1",
  );
  try {
    let location: unknown;
    render(
      <SkillsLibrary
        onLocationChange={(next) => {
          location = next;
        }}
      />,
    );
    const instructions = (await screen.findByLabelText("Instructions")) as HTMLTextAreaElement;
    assert.equal(instructions.value, first.instructions);
    fireEvent.change(screen.getByLabelText("Version"), { target: { value: "2" } });
    await waitFor(() => assert.equal(instructions.value, draft.instructions));
    assert.deepEqual(location, {
      scope: "workspace",
      project: undefined,
      skill: first.id,
      version: 2,
    });
  } finally {
    window.history.replaceState(null, "", initial);
  }
});

test("missing pinned skill version does not fall back to another revision", async () => {
  const draft = {
    id: "sk-pinned",
    scope: { kind: "workspace" },
    name: "pinned",
    description: "Pinned skill",
    instructions: "New draft instructions",
    tools: [],
    version: 2,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [draft] }),
    "GET /api/library/skills/sk-pinned": () => ({ revisions: [draft] }),
  });
  const initial = window.location.href;
  window.history.replaceState(
    null,
    "",
    "/library/skills?scope=workspace&skill=sk-pinned&version=1",
  );
  try {
    render(<SkillsLibrary />);
    await screen.findByText(defaultCopy.skillsLibrary.versionUnavailable);
    assert.equal(screen.queryByLabelText("Version"), null);
  } finally {
    window.history.replaceState(null, "", initial);
  }
});

test("skill Used By lists actual exact agent bindings separately from test runs", async () => {
  const skill = {
    id: "sk-usage",
    scope: { kind: "workspace" },
    name: "review",
    description: "Review",
    instructions: "Review carefully",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    published_at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [skill] }),
    "GET /api/library/skills/sk-usage": () => ({ revisions: [skill] }),
    "GET /api/library/skills/sk-usage/versions/1/agents": () => ({
      agents: [{ agent: "production-reviewer", agent_revision: 3, needs_review: true }],
      more: false,
    }),
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /review.*Version 1/s }));
  const link = await screen.findByRole("link", { name: "production-reviewer" });
  assert.equal(link.getAttribute("href"), "/agents/production-reviewer/context");
  assert.equal(link.getAttribute("target"), "_blank");
  await screen.findByText(defaultCopy.skillsLibrary.bindingNeedsReview);
  assert.equal(screen.queryByText(defaultCopy.skillsLibrary.testUses), null);
});

test("publishing waits for a completed readable test of the selected exact revision", async () => {
  const first = {
    id: "sk-tested",
    scope: { kind: "personal", subject: "ada" },
    name: "tested",
    description: "Tested skill",
    instructions: "Review carefully",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [] as string[],
  };
  const second = {
    ...first,
    version: 2,
    instructions: "Different revision",
    tests: [] as string[],
  };
  let status = "accepted";
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({
      agents: [
        {
          rev: 1,
          spec: {
            name: "reviewer",
            provider: "configured-provider",
            model: "real-model",
            system_prompt: "Review",
          },
        },
      ],
    }),
    "GET /api/library/skills": () => ({ skills: [second] }),
    "GET /api/library/skills/sk-tested": () => ({
      revisions: [first, second],
    }),
    "GET /api/library/skills/sk-tested/versions/1/agents": () => ({ agents: [], more: false }),
    "GET /api/library/skills/sk-tested/versions/2/agents": () => ({ agents: [], more: false }),
    "POST /api/library/skills/sk-tested/versions/2/test": () => {
      second.tests = ["native-test"];
      return { id: "native-test", agent: "reviewer", thread: "native-test-conversation", status };
    },
    "GET /api/runs/native-test": () => ({
      id: "native-test",
      agent: "reviewer",
      thread: "native-test-conversation",
      status,
    }),
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /tested.*Version 2/s }));
  const publish = (await screen.findByRole("button", {
    name: "Publish Version",
  })) as HTMLButtonElement;
  assert.equal(publish.disabled, true);
  await screen.findByText(defaultCopy.skillsLibrary.publishNeedsTest);
  fireEvent.change(screen.getByLabelText("Test Request"), {
    target: { value: "Validate the proposed output" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Test Skill" }));
  await screen.findByText(defaultCopy.skillsLibrary.testPending);
  assert.equal(publish.disabled, true);
  status = "completed";
  await waitFor(() => assert.equal(publish.disabled, false), { timeout: 2500 });
  fireEvent.change(screen.getByLabelText("Version"), { target: { value: "1" } });
  await waitFor(() => assert.equal(publish.disabled, true));
  assert.equal(
    net.seen.some((request) => request.path.endsWith("/publish")),
    false,
  );
});

test("an unreadable recorded test cannot enable skill publication", async () => {
  const skill = {
    id: "sk-unread-test",
    scope: { kind: "personal", subject: "ada" },
    name: "unread-test",
    description: "Tested skill",
    instructions: "Review carefully",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: ["hidden-test"],
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [skill] }),
    "GET /api/library/skills/sk-unread-test": () => ({ revisions: [skill] }),
    "GET /api/library/skills/sk-unread-test/versions/1/agents": () => ({ agents: [], more: false }),
    "GET /api/runs/hidden-test": () =>
      new Response(JSON.stringify({ error: "not found" }), { status: 404 }),
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /unread-test.*Version 1/s }));
  await screen.findByText(defaultCopy.skillsLibrary.publishNeedsTest);
  assert.equal(
    (screen.getByRole("button", { name: "Publish Version" }) as HTMLButtonElement).disabled,
    true,
  );
});
