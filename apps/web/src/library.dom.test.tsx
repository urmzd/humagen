import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { MemoryLibrary } from "./MemoryLibrary.tsx";
import { PersonalAgents } from "./PersonalAgents.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import { SkillsLibrary } from "./SkillsLibrary.tsx";
import { skillFileText } from "./skill-files.ts";

const memoryFolders = {
  folders: [
    {
      id: "personal",
      scope: "personal",
      title: "Personal",
      notes: 1,
      pending: 0,
      can_write: true,
      can_propose: true,
      can_promote: true,
      management: "opt_in_required",
      can_chat: true,
      chat_project: "personal-canonical",
    },
    {
      id: "workspace",
      scope: "workspace",
      title: "Company",
      notes: 0,
      pending: 0,
      can_write: false,
      can_propose: true,
      can_promote: true,
      management: "manual",
      can_chat: true,
      chat_project: "",
    },
  ],
};
const chooseVersion = (version: number) =>
  fireEvent.click(screen.getByRole("button", { name: `Preview Version ${version}` }));
const skillTab = async (name: string) =>
  fireEvent.mouseDown(await screen.findByRole("tab", { name }), { button: 0, ctrlKey: false });

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});
const me = {
  principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
  permissions: ["projects:read", "projects:write", "runs:read", "runs:write", "agents:read"],
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
      assert.match(
        skillFileText(
          (
            value.definition as unknown as import("../../../packages/client/src/generated/types.ts").LibrarySkillDefinition
          ).files![0]!,
        )!,
        /After/,
      );
      assert.equal("scope" in value.definition, false);
      assert.equal("version" in value.definition, false);
      assert.equal("tests" in value.definition, false);
      current = { ...first, instructions: "After", version: 2 };
      return current;
    },
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /brief.*Version 1/s }));
  await skillTab("Contents");
  fireEvent.click(screen.getByRole("button", { name: "Source" }));
  const editor = (await screen.findByLabelText("File Contents")) as HTMLTextAreaElement;
  fireEvent.change(editor, { target: { value: editor.value.replace("Before", "After") } });
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
    "GET /api/library/memory": () => memoryFolders,
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
  fireEvent.click(await screen.findByText(original.text));
  const edit = await screen.findByRole("button", { name: "Edit" });
  fireEvent.click(edit);
  assert.equal(
    (
      screen.getByRole("link", { name: original.source_name ?? "Open Source" }) as HTMLAnchorElement
    ).getAttribute("href"),
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
    "GET /api/library/memory": () => memoryFolders,
    "GET /api/me/memory": () => ({ notes: [note] }),
    "POST /api/memory/promote": (body) => {
      assert.deepEqual(body, { scope: "personal", owner: "", id: "m-private", destination: "~" });
      return { ...note, id: "m-new", status: "pending", source: "person", by: "ada" };
    },
  });
  render(<MemoryLibrary />);
  fireEvent.click(await screen.findByText(note.text));
  fireEvent.click(screen.getByText(defaultCopy.memoryLibrary.promote, { selector: "summary" }));
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
    await screen.findByText(first.instructions);
    await skillTab("Versions");
    chooseVersion(2);
    await waitFor(() => assert.equal((location as { version: number })?.version, 2));
    await skillTab("Overview");
    await screen.findByText(draft.instructions);
    assert.deepEqual(location, {
      scope: "workspace",
      project: undefined,
      skill: first.id,
      version: 2,
      libraryTab: "overview",
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
    "GET /api/projects/personal/agents": () => ({ available: { all: false, names: ["reviewer"] } }),
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
  await skillTab("Versions");
  const publish = (await screen.findByRole("button", {
    name: "Publish Version",
  })) as HTMLButtonElement;
  assert.equal(publish.disabled, true);
  await screen.findByText(defaultCopy.skillsLibrary.publishNeedsTest);
  fireEvent.change(screen.getByLabelText("Test Request"), {
    target: { value: "Validate the proposed output" },
  });
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: "Test Skill" }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Test Skill" }));
  await screen.findByText(defaultCopy.skillsLibrary.testPending);
  assert.equal(publish.disabled, true);
  status = "completed";
  await waitFor(() => assert.equal(publish.disabled, false), { timeout: 2500 });
  chooseVersion(1);
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
  await skillTab("Versions");
  await screen.findByText(defaultCopy.skillsLibrary.publishNeedsTest);
  assert.equal(
    (screen.getByRole("button", { name: "Publish Version" }) as HTMLButtonElement).disabled,
    true,
  );
});

test("a newly saved personal draft tests with an eligible personal agent in the canonical personal project", async () => {
  const skill = {
    id: "sk-personal-test",
    scope: { kind: "personal", subject: "ada" },
    name: "personal-test",
    description: "Personal review",
    instructions: "Review privately",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  let created = false;
  let tested: unknown;
  net = stubFetch({
    "GET /api/me": () => ({ ...me, personal_project: "~personal-canonical" }),
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({
      agents: [
        {
          rev: 1,
          spec: {
            name: "foreign-agent",
            provider: "configured",
            model: "real",
            system_prompt: "Other",
          },
        },
        {
          rev: 1,
          spec: {
            name: "personal-agent",
            provider: "configured",
            model: "real",
            system_prompt: "Personal",
          },
        },
      ],
      homes: { "foreign-agent": "Another", "personal-agent": "personal-canonical" },
    }),
    "GET /api/projects/personal-canonical/agents": () => ({
      available: { all: false, names: ["personal-agent"] },
    }),
    "GET /api/library/skills": () => ({ skills: created ? [skill] : [] }),
    "POST /api/library/skills": () => {
      created = true;
      return skill;
    },
    "GET /api/library/skills/sk-personal-test": () => ({ revisions: [skill] }),
    "GET /api/library/skills/sk-personal-test/versions/1/agents": () => ({
      agents: [],
      more: false,
    }),
    "POST /api/library/skills/sk-personal-test/versions/1/test": (body) => {
      tested = body;
      return {
        id: "personal-test-run",
        agent: "personal-agent",
        thread: "personal-test-thread",
        status: "accepted",
      };
    },
  });
  render(<SkillsLibrary />);
  const create = (await screen.findByRole("button", { name: "New Skill" })) as HTMLButtonElement;
  await waitFor(() => assert.equal(create.disabled, false));
  fireEvent.click(create);
  fireEvent.change(await screen.findByLabelText("Name"), { target: { value: skill.name } });
  fireEvent.change(screen.getByLabelText("Purpose"), { target: { value: skill.description } });
  fireEvent.click(screen.getByRole("button", { name: "Create Skill Folder" }));
  await waitFor(() =>
    assert.equal(
      (screen.getByRole("button", { name: "Save Draft" }) as HTMLButtonElement).disabled,
      false,
    ),
  );
  fireEvent.click(screen.getByRole("button", { name: "Save Draft" }));
  await skillTab("Versions");
  await screen.findByLabelText("Test Request");
  fireEvent.change(screen.getByLabelText("Test Agent"), { target: { value: "personal-agent" } });
  fireEvent.change(screen.getByLabelText("Test Request"), {
    target: { value: "Validate my private skill" },
  });
  fireEvent.click(screen.getByRole("button", { name: "Test Skill" }));
  await waitFor(() => assert.ok(tested));
  assert.deepEqual(tested, {
    agent: "personal-agent",
    project: "personal-canonical",
    text: "Validate my private skill",
  });
  assert.equal(screen.queryByRole("option", { name: "foreign-agent" }), null);
});

test("memory failures never look empty and a retry recovers without reading unrelated APIs", async () => {
  let failed = true;
  net = stubFetch({
    "GET /api/library/memory": () => memoryFolders,
    "GET /api/me/memory": () =>
      failed
        ? new Response(JSON.stringify({ error: "unavailable" }), { status: 503 })
        : {
            notes: [
              {
                id: "m-recovered",
                text: "Recovered private preference",
                source: "person",
                status: "approved",
                at: "2026-10-26T12:00:00Z",
              },
            ],
          },
  });
  render(<MemoryLibrary />);
  await screen.findByText(defaultCopy.memoryLibrary.loadFailed);
  assert.equal(screen.queryByText(defaultCopy.memoryLibrary.empty), null);
  failed = false;
  fireEvent.click(screen.getByRole("button", { name: "Retry" }));
  await screen.findByText("Recovered private preference");
  assert.ok(
    !net.seen.some((request) => ["/api/me", "/api/projects", "/api/agents"].includes(request.path)),
  );
  const chat = screen.getByRole("link", { name: "Chat With Memory" }) as HTMLAnchorElement;
  const link = new URL(chat.href);
  assert.equal(link.searchParams.get("project"), "personal-canonical");
  assert.equal(link.searchParams.get("channel"), null);
  assert.ok(!link.href.includes("Recovered"));
});

test("finding another memory chain preserves the active correction draft", async () => {
  const note = {
    id: "m-edit",
    text: "Keep this memory",
    source: "person",
    status: "approved",
    at: "2026-10-26T12:00:00Z",
  };
  net = stubFetch({
    "GET /api/library/memory": (_, query) => ({
      folders: query.get("q")
        ? [{ ...memoryFolders.folders[0], selected_only: true, management: "configured" }]
        : memoryFolders.folders,
    }),
    "GET /api/me/memory": () => ({ notes: [note] }),
  });
  render(<MemoryLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: "Edit" }));
  fireEvent.change(screen.getByLabelText(defaultCopy.memoryLibrary.text), {
    target: { value: "Unsaved correction stays here" },
  });
  fireEvent.change(screen.getByPlaceholderText("Find a Chain"), {
    target: { value: "another chain" },
  });
  await waitFor(() =>
    assert.ok(net?.seen.some((request) => request.path.includes("q=another+chain"))),
  );
  await screen.findByText(defaultCopy.memoryLibrary.configured);
  assert.equal(
    (screen.getByLabelText(defaultCopy.memoryLibrary.text) as HTMLTextAreaElement).value,
    "Unsaved correction stays here",
  );
  assert.equal(screen.queryByText(defaultCopy.memoryLibrary.chainMissing), null);
});

test("a personal memory writer without promotion permission cannot share it through a promotion control", async () => {
  const note = {
    id: "m-limited",
    text: "Private context stays private",
    source: "person",
    status: "approved",
    at: "2026-10-26T12:00:00Z",
  };
  net = stubFetch({
    "GET /api/library/memory": () => ({
      folders: memoryFolders.folders.map((folder) => ({ ...folder, can_promote: false })),
    }),
    "GET /api/me/memory": () => ({ notes: [note] }),
  });
  render(<MemoryLibrary />);
  fireEvent.click(await screen.findByText(note.text));
  assert.equal(screen.queryByText(defaultCopy.memoryLibrary.promote), null);
  assert.ok(screen.getByRole("button", { name: "Edit" }));
});

test("a dirty skill package keeps its scope until it is explicitly saved or discarded", async () => {
  const row = {
    id: "sk-dirty",
    scope: { kind: "personal", subject: "ada" },
    name: "review",
    description: "Review a release",
    instructions: "Original instructions",
    tools: [],
    version: 1,
    by: "ada",
    at: "2026-10-26T12:00:00Z",
    tests: [],
  };
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/projects": () => ({ projects: [], next: null }),
    "GET /api/agents": () => ({ agents: [] }),
    "GET /api/library/skills": () => ({ skills: [row] }),
    "GET /api/library/skills/sk-dirty": () => ({ revisions: [row] }),
  });
  render(<SkillsLibrary />);
  fireEvent.click(await screen.findByRole("button", { name: /review.*Version 1/s }));
  await skillTab("Contents");
  fireEvent.click(screen.getByRole("button", { name: "Source" }));
  const editor = (await screen.findByLabelText("File Contents")) as HTMLTextAreaElement;
  fireEvent.change(editor, {
    target: { value: editor.value.replace("Original instructions", "Unsaved instructions") },
  });
  const company = screen.getByRole("button", { name: "Company" }) as HTMLButtonElement;
  assert.equal(company.disabled, true);
  fireEvent.click(company);
  assert.match(editor.value, /Unsaved instructions/);
  assert.ok(!net.seen.some((request) => request.path.includes("scope=workspace")));
  fireEvent.click(screen.getByRole("button", { name: "Discard Changes" }));
  assert.equal(company.disabled, false);
  assert.match(editor.value, /Original instructions/);
});
