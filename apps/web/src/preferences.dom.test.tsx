import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor, within } from "@testing-library/react";
import type { Me, Preferences, ThreadSummary } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { unreadPrefs, usePreferences } from "./preferences.ts";
import { Sidebar } from "./Sidebar.tsx";
import type { Route } from "./route.ts";

// The loader imports every DOM file into one process, where a top-level hook
// runs after other files' tests too, so it acts only on what this file set up.
let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  net.restore();
  net = undefined;
});

const saved: Preferences = { ...unreadPrefs, display_name: "Ada", theme: "dark", accent: "green", pinned: ["t0"], muted: ["Initech"] };
// The server saves what it is sent over what it has.
const save = (body: unknown) => ({ ...saved, ...(body as Partial<Preferences>) });
const puts = () => net?.seen.filter((r) => r.method === "PUT" && r.path === "/api/preferences") ?? [];
const reads = () => net?.seen.filter((r) => r.method === "GET" && r.path === "/api/preferences").length ?? 0;

test("a pin made while the saved preferences are read is made once they are, over the saved name, theme and accent", async () => {
  let answer = () => {};
  const answered = new Promise<void>((r) => (answer = r));
  net = stubFetch({ "GET /api/preferences": () => answered.then(() => saved), "PUT /api/preferences": save });
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

test("a pin that cannot be saved goes back, and the sidebar says so beside the task", async () => {
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "GET /api/threads": () => ({ threads: [task("t1", "Quarterly report")], next: null }),
  });
  render(<Pins />);
  fireEvent.click(await screen.findByRole("button", { name: "Pin Quarterly report" }));
  const note = await screen.findByText("Could not pin. Try again.");
  // The row sits over the section's label, which sticks above the first row, so the note is not hidden under it.
  assert.ok(note.closest("li")?.className.split(" ").includes("z-20"), "the row saying it is not raised");
  // PUT /api/preferences is not stubbed, so the save failed and the task is not pinned.
  assert.equal(puts().length, 1);
  assert.ok(screen.getByRole("button", { name: "Pin Quarterly report" }));
  assert.ok(!screen.queryByRole("button", { name: "Unpin Quarterly report" }));
});

test("Pin says so when the saved preferences cannot be read, and works once they can be", async () => {
  const routes: Record<string, (body: unknown) => unknown> = {
    "GET /api/threads": () => ({ threads: [task("t1", "Quarterly report")], next: null }),
    "PUT /api/preferences": save,
  };
  net = stubFetch(routes);
  render(<Pins />);
  fireEvent.click(await screen.findByRole("button", { name: "Pin Quarterly report" }));
  await screen.findByText("Could not pin. Try again.");
  assert.equal(puts().length, 0);

  routes["GET /api/preferences"] = () => saved;
  fireEvent.click(screen.getByRole("button", { name: "Pin Quarterly report" }));
  await screen.findByRole("button", { name: "Unpin Quarterly report" });
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
  assert.deepEqual(result.current.prefs.pinned, ["t0"], "an earlier acknowledgement cannot flash the pin back");
  assert.deepEqual((puts()[1]?.body as Partial<Preferences>).pinned, ["t0"]);
  await act(async () => answers[1]?.({ ...saved, pinned: ["t0"] }));
  await act(() => Promise.all([pinned, unpinned]));
  assert.deepEqual(result.current.prefs.pinned, ["t0"]);
});

test("a refused pin rolls back only itself while the next pin is preserved and saved", async () => {
  const answers: ((value: Preferences | Response) => void)[] = [];
  net = stubFetch({
    "GET /api/preferences": () => saved,
    "PUT /api/preferences": () => new Promise<Preferences | Response>((resolve) => answers.push(resolve)),
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
  assert.ok(await rejected instanceof Error);
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
  const pin = await screen.findByRole("button", { name: "Pin Quarterly report" });
  pin.focus();
  fireEvent.click(pin);
  const unpin = await screen.findByRole("button", { name: "Unpin Quarterly report" });
  await waitFor(() => assert.equal(document.activeElement === unpin, true));
  const pinned = screen.getByRole("region", { name: "Pinned" });
  assert.ok(within(pinned).getByRole("button", { name: /^Quarterly report/ }).getAttribute("aria-current") === "page");
  assert.equal(screen.getAllByRole("button", { name: /^Quarterly report/ }).length, 1);
  assert.equal(visited.length, 0, "pinning does not navigate away from the active conversation");
  fireEvent.click(unpin);
  const restored = await screen.findByRole("button", { name: "Pin Quarterly report" });
  await waitFor(() => assert.equal(document.activeElement === restored, true));
  assert.equal(screen.queryByRole("region", { name: "Pinned" }), null);
  assert.equal(screen.getAllByRole("button", { name: /^Quarterly report/ }).length, 1);
  assert.equal(screen.getByRole("button", { name: /^Quarterly report/ }).getAttribute("aria-current"), "page");
  assert.deepEqual(puts().map((request) => (request.body as Partial<Preferences>).pinned), [["t1", "t0"], ["t0"]]);
});

test("unpinning the active conversation keeps it visible when it is older than the recent page", async () => {
  const current = task("t1", "Older active conversation");
  net = stubFetch({
    "GET /api/preferences": () => ({ ...saved, pinned: ["t1"] }),
    "PUT /api/preferences": save,
    "GET /api/threads": () => ({ threads: [], next: null }),
  });
  render(<Pins threads={[current]} route={{ page: "conversation", id: "t1" }} />);
  const unpin = await screen.findByRole("button", { name: "Unpin Older active conversation" });
  unpin.focus();
  fireEvent.click(unpin);
  const pin = await screen.findByRole("button", { name: "Pin Older active conversation" });
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
  const unpin = await screen.findByRole("button", { name: "Unpin Older pinned conversation" });
  const filter = screen.getByRole("button", { name: "Filter conversations" });
  unpin.focus();
  // Native keyboard activation dispatches a click without a pointer click count.
  fireEvent.click(unpin, { detail: 0 });
  await waitFor(() => assert.equal(document.activeElement === filter, true));
  assert.equal(Boolean(screen.queryByRole("region", { name: "Pinned" })), false);
  assert.equal(screen.queryAllByRole("button", { name: /Older pinned conversation/ }).length, 0);
  assert.deepEqual(visited, [], "unpinning does not open the disappearing conversation");
  assert.deepEqual((puts()[0]?.body as Partial<Preferences>).pinned, []);
});

const me: Me = {
  principal: { subject: "ada", tenant: "acme", kind: "user", name: "Ada", roles: ["member"] },
  permissions: ["runs:read", "runs:write"],
};

// The sidebar with its pins kept as the console keeps them.
function Pins({ threads = [], route = { page: "new" }, onGo = () => {} }: { threads?: ThreadSummary[]; route?: Route; onGo?: (route: Route) => void }) {
  const { prefs, pin } = usePreferences();
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
      pinned={prefs.pinned}
      onPin={pin}
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
