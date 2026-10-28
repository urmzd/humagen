import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import {
  act,
  cleanup,
  fireEvent,
  render,
  renderHook,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import type { Me, Preferences, ProjectView, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs, usePreferences } from "./preferences.ts";
import type { Route } from "./route.ts";
import { Sidebar } from "./Sidebar.tsx";

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const saved: Preferences = {
  ...unreadPrefs,
  display_name: "Ada",
  theme: "dark",
  accent: "green",
  pinned: ["t0"],
  muted: ["Initech"],
};
// The server saves what it is sent over what it has.
const save = (body: unknown) => ({ ...saved, ...(body as Partial<Preferences>) });
const puts = () =>
  net?.seen.filter((r) => r.method === "PUT" && r.path === "/api/preferences") ?? [];
const reads = () =>
  net?.seen.filter((r) => r.method === "GET" && r.path === "/api/preferences").length ?? 0;

test("a pin made while the saved preferences are read is made once they are, over the saved name, theme and accent", async () => {
  let answer = () => {};
  const answered = new Promise<void>((r) => (answer = r));
  net = stubFetch({
    "GET /api/preferences": () => answered.then(() => saved),
    "PUT /api/preferences": save,
  });
  const { result } = renderHook(() => usePreferences());
  assert.equal(result.current.prefsRead, "reading");
  let pinned = Promise.resolve();
  act(() => {
    pinned = result.current.pin("t1", true);
  });
  await act(() => Promise.resolve());
  // Nothing is saved over preferences not read yet, and the pin is not dropped either.
  assert.equal(puts().length, 0);
  answer();
  await act(() => pinned);
  assert.deepEqual(
    puts().map((r) => r.body),
    [{ display_name: "Ada", theme: "dark", accent: "green", pinned: ["t1", "t0"] }],
  );
  assert.deepEqual(result.current.prefs.pinned, ["t1", "t0"]);
  assert.equal(result.current.prefsRead, "read");
  assert.equal(reads(), 1, "the preferences are read once");
});

test("after the saved preferences could not be read, a mute reads them again, and fails, changing nothing, while they still cannot be", async () => {
  const routes: Record<string, (body: unknown) => unknown> = { "PUT /api/preferences": save };
  // GET /api/preferences is not stubbed, so it answers 404.
  net = stubFetch(routes);
  const { result } = renderHook(() => usePreferences());
  await act(() => Promise.resolve());
  assert.equal(result.current.prefsRead, "failed");

  let muted: Promise<void> = Promise.resolve();
  act(() => {
    muted = result.current.mute("Globex", true);
  });
  await act(() => muted.catch(() => {}));
  await assert.rejects(muted);
  assert.equal(reads(), 2, "the mute read the preferences again");
  assert.equal(puts().length, 0);
  assert.deepEqual(result.current.prefs.muted, []);

  // The server answers again: the next mute is saved over what it has.
  routes["GET /api/preferences"] = () => saved;
  await act(() => result.current.mute("Globex", true));
  assert.deepEqual(
    puts().map((r) => r.body),
    [{ display_name: "Ada", theme: "dark", accent: "green", muted: ["Initech", "Globex"] }],
  );
  assert.equal(result.current.prefsRead, "read");
});

/** Opens a conversation's ⋯ menu and picks Pin or Unpin, as a person does. */
async function choose(title: string, item: "Pin" | "Unpin", init?: object) {
  // The row can be drawn again while the saved preferences arrive, so the
  // menu is opened on whichever trigger is on the page by then.
  let entry: HTMLElement | undefined;
  await waitFor(() => {
    const trigger = screen.getByRole("button", { name: `Manage ${title}` });
    if (trigger.getAttribute("aria-expanded") !== "true") {
      trigger.focus();
      fireEvent.click(trigger);
    }
    entry = screen.getByRole("menuitem", { name: new RegExp(`^${item}`) });
  });
  fireEvent.click(entry!, init);
}
const manage = (title: string) => screen.findByRole("button", { name: `Manage ${title}` });
const pinnedRegion = () => screen.queryByRole("region", { name: "Pinned" });

test("a pin that cannot be saved goes back, and the sidebar says so beside the task", async () => {
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({ threads: [task("t1", "Quarterly report")], next: null }),
  });
  render(<Pins />);
  await choose("Quarterly report", "Pin");
  const note = await screen.findByText("Could not pin. Try again.");
  // The row sits over the section's label, which sticks above the first row, so the note is not hidden under it.
  assert.ok(
    note.closest("li")?.className.split(" ").includes("z-20"),
    "the row saying it is not raised",
  );
  // PUT /api/preferences is not stubbed, so the save failed and the task is not pinned.
  assert.equal(puts().length, 1);
  assert.ok(!pinnedRegion(), "Pinned is still shown");
});

test("Pin says so when the saved preferences cannot be read, and works once they can be", async () => {
  const routes: Record<string, (body: unknown) => unknown> = {
    "GET /api/threads": () => ({ threads: [task("t1", "Quarterly report")], next: null }),
    "PUT /api/preferences": save,
  };
  net = stubFetch(routes);
  render(<Pins />);
  await choose("Quarterly report", "Pin");
  await screen.findByText("Could not pin. Try again.");
  assert.equal(puts().length, 0);

  routes["GET /api/preferences"] = () => saved;
  await choose("Quarterly report", "Pin");
  await screen.findByRole("region", { name: "Pinned" });
  assert.ok(!screen.queryByText("Could not pin. Try again."));
  assert.deepEqual(
    puts().map((r) => r.body),
    [{ display_name: "Ada", theme: "dark", accent: "green", pinned: ["t1", "t0"] }],
  );
});

test("a rapid pin and unpin show the latest intent while their writes finish in order", async () => {
  const answers: ((value: Preferences) => void)[] = [];
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "PUT /api/preferences": () => new Promise<Preferences>((resolve) => answers.push(resolve)),
  });
  const { result } = renderHook(() => usePreferences());
  await waitFor(() => assert.equal(result.current.prefsRead, "read"));
  let pinned = Promise.resolve();
  let unpinned = Promise.resolve();
  await act(async () => {
    pinned = result.current.pin("t1", true);
    await Promise.resolve();
  });
  await waitFor(() => assert.equal(answers.length, 1));
  assert.deepEqual(result.current.prefs.pinned, ["t1", "t0"]);
  await act(async () => {
    unpinned = result.current.pin("t1", false);
    await Promise.resolve();
  });
  assert.deepEqual(result.current.prefs.pinned, ["t0"], "the queued unpin is visible immediately");
  assert.equal(answers.length, 1, "the newer write waits for the earlier acknowledgement");
  await act(async () => answers[0]?.({ ...saved, pinned: ["t1", "t0"] }));
  await waitFor(() => assert.equal(answers.length, 2));
  assert.deepEqual(
    result.current.prefs.pinned,
    ["t0"],
    "an earlier acknowledgement cannot flash the pin back",
  );
  assert.deepEqual((puts()[1]?.body as Partial<Preferences>).pinned, ["t0"]);
  await act(async () => answers[1]?.({ ...saved, pinned: ["t0"] }));
  await act(() => Promise.all([pinned, unpinned]));
  assert.deepEqual(result.current.prefs.pinned, ["t0"]);
});

test("a refused pin rolls back only itself while the next pin is preserved and saved", async () => {
  const answers: ((value: Preferences | Response) => void)[] = [];
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "PUT /api/preferences": () =>
      new Promise<Preferences | Response>((resolve) => answers.push(resolve)),
  });
  const { result } = renderHook(() => usePreferences());
  await waitFor(() => assert.equal(result.current.prefsRead, "read"));
  let rejected: Promise<unknown> = Promise.resolve();
  let pinned = Promise.resolve();
  await act(async () => {
    rejected = result.current.pin("t1", true).catch((error) => error);
    pinned = result.current.pin("t2", true);
    await Promise.resolve();
  });
  await waitFor(() => assert.equal(answers.length, 1));
  assert.deepEqual(result.current.prefs.pinned, ["t2", "t1", "t0"]);
  await act(async () => answers[0]?.(new Response(null, { status: 503 })));
  await waitFor(() => assert.equal(answers.length, 2));
  assert.deepEqual(result.current.prefs.pinned, ["t2", "t0"]);
  assert.deepEqual((puts()[1]?.body as Partial<Preferences>).pinned, ["t2", "t0"]);
  await act(async () => answers[1]?.({ ...saved, pinned: ["t2", "t0"] }));
  await act(() => pinned);
  assert.ok((await rejected) instanceof Error);
  assert.deepEqual(result.current.prefs.pinned, ["t2", "t0"]);
});

test("pin and unpin move one active conversation and retain focus on its action", async () => {
  const current = task("t1", "Quarterly report");
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "PUT /api/preferences": save,
    "GET /api/threads": () => ({ threads: [current], next: null }),
  });
  const visited: Route[] = [];
  render(<Pins route={{ page: "conversation", id: "t1" }} onGo={(route) => visited.push(route)} />);
  await choose("Quarterly report", "Pin");
  const pinned = await screen.findByRole("region", { name: "Pinned" });
  await waitFor(() =>
    assert.equal(
      document.activeElement ===
        within(pinned).getByRole("button", { name: "Manage Quarterly report" }),
      true,
    ),
  );
  assert.ok(
    within(pinned)
      .getByRole("button", { name: /^Quarterly report/ })
      .getAttribute("aria-current") === "page",
  );
  assert.equal(screen.getAllByRole("button", { name: /^Quarterly report/ }).length, 1);
  assert.equal(visited.length, 0, "pinning does not navigate away from the active conversation");
  await choose("Quarterly report", "Unpin");
  await waitFor(() => assert.ok(!pinnedRegion(), "Pinned is still shown"));
  const restored = await manage("Quarterly report");
  await waitFor(() => assert.equal(document.activeElement === restored, true));
  assert.equal(screen.queryByRole("region", { name: "Pinned" }), null);
  assert.equal(screen.getAllByRole("button", { name: /^Quarterly report/ }).length, 1);
  assert.equal(
    screen.getByRole("button", { name: /^Quarterly report/ }).getAttribute("aria-current"),
    "page",
  );
  assert.deepEqual(
    puts().map((request) => (request.body as Partial<Preferences>).pinned),
    [["t1", "t0"], ["t0"]],
  );
});

test("unpinning the active conversation keeps it visible when it is older than the recent page", async () => {
  const current = task("t1", "Older active conversation");
  net = stubFetch({
    "GET /api/preferences": () => ({ ...saved, pinned: ["t1"] }),
    "PUT /api/preferences": save,
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  render(<Pins threads={[current]} route={{ page: "conversation", id: "t1" }} />);
  await choose("Older active conversation", "Unpin");
  await waitFor(() => assert.ok(!pinnedRegion(), "Pinned is still shown"));
  const pin = await manage("Older active conversation");
  await waitFor(() => assert.equal(document.activeElement === pin, true));
  assert.equal(screen.queryByRole("region", { name: "Pinned" }), null);
  assert.equal(screen.getAllByRole("button", { name: /^Older active conversation/ }).length, 1);
});

test("keyboard unpin returns to the conversation filter when an older unselected row leaves the page", async () => {
  const older = task("t1", "Older pinned conversation");
  net = stubFetch({
    "GET /api/preferences": () => ({ ...saved, pinned: [older.id] }),
    "PUT /api/preferences": save,
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  const visited: Route[] = [];
  render(<Pins threads={[older]} onGo={(route) => visited.push(route)} />);
  await manage("Older pinned conversation");
  const filter = screen.getByRole("button", { name: "Filter conversations" });
  // Native keyboard activation dispatches a click without a pointer click count.
  await choose("Older pinned conversation", "Unpin", { detail: 0 });
  await waitFor(() => assert.equal(document.activeElement === filter, true));
  assert.equal(Boolean(screen.queryByRole("region", { name: "Pinned" })), false);
  assert.equal(screen.queryAllByRole("button", { name: /Older pinned conversation/ }).length, 0);
  assert.deepEqual(visited, [], "unpinning does not open the disappearing conversation");
  assert.deepEqual((puts()[0]?.body as Partial<Preferences>).pinned, []);
});

test("project and conversation pins persist independently through unpinning and reopening", async () => {
  let stored = { ...saved, pinned: [] as string[], pinned_projects: [] as string[] };
  net = stubFetch({
    "GET /api/preferences": () => stored,
    "PUT /api/preferences": (body) => (stored = { ...stored, ...(body as Partial<typeof stored>) }),
    "GET /api/threads": () => ({
      threads: [{ ...task("t1", "Quarterly report"), project: "Launch" }],
      next: null,
    }),
  });
  render(<Pins />);
  fireEvent.click(await screen.findByRole("button", { name: "Pin project Launch" }));
  await screen.findByRole("link", { name: "Launch" });
  await choose("Quarterly report", "Pin");
  await waitFor(() => assert.deepEqual(stored.pinned, ["t1"]));
  assert.deepEqual(stored.pinned_projects, ["Launch"]);
  assert.deepEqual(stored.pinned, ["t1"]);
  cleanup();
  render(<Pins />);
  const pinned = within(await screen.findByRole("region", { name: "Pinned" }));
  assert.equal(
    pinned.getByRole("link", { name: "Launch" }).getAttribute("href"),
    "/projects/Launch",
  );
  fireEvent.click(pinned.getByRole("button", { name: "Unpin project Launch" }));
  await waitFor(() => assert.deepEqual(stored.pinned_projects, []));
  assert.deepEqual(stored.pinned, ["t1"]);
  await choose("Quarterly report", "Unpin");
  await waitFor(() => assert.deepEqual(stored.pinned, []));
});

test("keyboard project pinning moves focus to its shortcut and unpinning returns to the project group", async () => {
  let stored: Preferences = { ...saved, pinned: [], pinned_projects: [] };
  net = stubFetch({
    "GET /api/preferences": () => stored,
    "PUT /api/preferences": (body) => (stored = { ...stored, ...(body as Partial<Preferences>) }),
    "GET /api/threads": () => ({
      threads: [{ ...task("t1", "Quarterly report"), project: "Launch" }],
      next: null,
    }),
  });
  render(<Pins />);
  const pin = await screen.findByRole("button", { name: "Pin project Launch" });
  pin.focus();
  fireEvent.click(pin, { detail: 0 });
  const shortcut = within(await screen.findByRole("region", { name: "Pinned" }));
  const unpin = shortcut.getByRole("button", { name: "Unpin project Launch" });
  await waitFor(() => assert.equal(document.activeElement === unpin, true));
  fireEvent.click(unpin, { detail: 0 });
  const restored = await screen.findByRole("button", { name: "Pin project Launch" });
  await waitFor(() => assert.equal(document.activeElement === restored, true));
  assert.equal(restored.isConnected, true);
  assert.equal(Boolean(screen.queryByRole("region", { name: "Pinned" })), false);
  await waitFor(() => assert.deepEqual(stored.pinned_projects, []));
  assert.deepEqual(stored.pinned, [], "project pinning leaves conversation pins unchanged");
});

test("keyboard unpinning a project without recent conversations returns focus to Projects", async () => {
  let stored: Preferences = { ...saved, pinned: [], pinned_projects: ["Launch"] };
  net = stubFetch({
    "GET /api/preferences": () => stored,
    "PUT /api/preferences": (body) => (stored = { ...stored, ...(body as Partial<Preferences>) }),
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  render(<Pins />);
  const unpin = await screen.findByRole("button", { name: "Unpin project Launch" });
  const projects = screen.getByRole("link", { name: "Projects" });
  unpin.focus();
  fireEvent.click(unpin, { detail: 0 });
  await waitFor(() => assert.equal(document.activeElement === projects, true));
  assert.equal(projects.isConnected, true);
  assert.equal(Boolean(screen.queryByRole("region", { name: "Pinned" })), false);
  assert.equal(screen.queryAllByRole("button", { name: /pin project Launch/i }).length, 0);
  await waitFor(() => assert.deepEqual(stored.pinned_projects, []));
});

test("a refused project pin rolls back and says so beside the project", async () => {
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({
      threads: [{ ...task("t1", "Quarterly report"), project: "Launch" }],
      next: null,
    }),
  });
  render(<Pins />);
  fireEvent.click(await screen.findByRole("button", { name: "Pin project Launch" }));
  await screen.findByText("Could not pin. Try again.");
  assert.ok(screen.getByRole("button", { name: "Pin project Launch" }));
  assert.ok(!screen.queryByRole("link", { name: "Launch" }));
});

test("overlapping project and conversation saves keep both lists", async () => {
  let release = () => {};
  const first = new Promise<void>((resolve) => {
    release = resolve;
  });
  let stored: Preferences = { ...saved, pinned_projects: [] };
  let writes = 0;
  net = stubFetch({
    "GET /api/preferences": () => stored,
    "PUT /api/preferences": async (body) => {
      if (++writes === 1) await first;
      stored = { ...stored, ...(body as Partial<Preferences>) };
      return stored;
    },
  });
  const { result } = renderHook(() => usePreferences());
  await waitFor(() => assert.equal(result.current.prefsRead, "read"));
  let project = Promise.resolve();
  let conversation = Promise.resolve();
  act(() => {
    project = result.current.pinProject("Launch", true);
    conversation = result.current.pin("t1", true);
  });
  await waitFor(() => assert.equal(writes, 1));
  release();
  await act(() => Promise.all([project, conversation]));
  assert.deepEqual(result.current.prefs.pinned_projects, ["Launch"]);
  assert.deepEqual(result.current.prefs.pinned, ["t1", "t0"]);
});

test("conversation rows identify multiple agents and people, with the full names accessible", async () => {
  const conversation = {
    ...task("t1", "Launch checklist"),
    agents: ["lead", "helper", "lead"],
    people: ["ada", "mira", "jon", "sam", "mira"],
  };
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({ threads: [conversation], next: null }),
  });
  render(<Pins />);
  const agents = await screen.findByRole("group", { name: "Agents: lead, helper" });
  assert.equal(agents.textContent, "lead+1");
  const people = screen.getByRole("group", { name: "People: Ada, mira, jon, sam" });
  assert.ok(people.textContent?.includes("4 people"));
  assert.equal(
    people.querySelectorAll("[aria-hidden] > span").length,
    3,
    "only three avatars fit in a compact row",
  );
  assert.ok(agents.closest("button")?.textContent?.includes("Launch checklist"));
});

test("people-only and agent-only conversations show only their actual participants", async () => {
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({
      threads: [
        { ...task("people", "Team discussion"), agents: [] },
        { ...task("agents", "Agent work"), people: [] },
      ],
      next: null,
    }),
  });
  render(<Pins />);
  const person = await screen.findByRole("group", { name: "People: Ada" });
  assert.ok(person.textContent?.includes("You"));
  assert.ok(!person.closest("button")?.querySelector('[aria-label^="Agents:"]'));
  const agent = screen.getByRole("group", { name: "Agents: lead" });
  assert.ok(!agent.closest("button")?.querySelector('[aria-label^="People:"]'));
});

test("Archive hides a conversation at once, and Delete asks once more before queueing it for an admin", async () => {
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({
      threads: [task("t1", "Quarterly report"), task("t2", "Old draft")],
      next: null,
    }),
    "POST /api/threads/t1/archive": () => ({}),
    "POST /api/threads/t2/archive": () => ({}),
  });
  render(<Pins />);
  fireEvent.click(await manage("Quarterly report"));
  fireEvent.click(await screen.findByRole("menuitem", { name: /^Archive/ }));
  await waitFor(() =>
    assert.ok(
      !screen.queryByRole("button", { name: /^Quarterly report/ }),
      "the archived row is still listed",
    ),
  );
  assert.ok(screen.getByText(/Archived/));

  fireEvent.click(await manage("Old draft"));
  fireEvent.click(await screen.findByRole("menuitem", { name: /^Delete/ }));
  assert.ok(screen.getByText(/removed once an admin approves/));
  assert.ok(
    screen.queryByRole("button", { name: /^Old draft/ }),
    "nothing happens before the second Delete",
  );
  fireEvent.click(screen.getByRole("button", { name: "Delete" }));
  await waitFor(() => assert.ok(!screen.queryByRole("button", { name: /^Old draft/ })));
  const archives = net.seen.filter((r) => r.method === "POST" && r.path.endsWith("/archive"));
  assert.deepEqual(
    archives.map((r) => [r.path, (r.body as { delete_now: boolean }).delete_now]),
    [
      ["/api/threads/t1/archive", false],
      ["/api/threads/t2/archive", true],
    ],
  );
});

const me: Me = {
  principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
  permissions: ["runs:read", "runs:write", "projects:read"],
};

// The sidebar with its pins kept as the console keeps them.
function Pins({
  threads = [],
  route = { page: "new" },
  onGo = () => {},
}: {
  threads?: ThreadSummary[];
  route?: Route;
  onGo?: (route: Route) => void;
}) {
  const { prefs, pin, pinProject } = usePreferences();
  const noop = () => {};
  return (
    <Sidebar
      open
      onToggle={noop}
      route={route}
      go={onGo}
      me={me}
      prefs={prefs}
      can={(p) => me.permissions.includes(p)}
      threads={threads}
      projects={[{ name: "Launch", title: "Launch" } as ProjectView]}
      pinned={prefs.pinned}
      onPin={pin}
      onPinProject={pinProject}
      inboxCount={0}
      live
      onSignOut={noop}
    />
  );
}

function task(id: string, title: string): ThreadSummary {
  return {
    id,
    title,
    named: true,
    origin: "api",
    channel: "general",
    agents: ["lead"],
    people: ["ada"],
    visibility: "shared",
    messages: 0,
    turns: 1,
    branches: 1,
    active: 0,
    failed: 0,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}
