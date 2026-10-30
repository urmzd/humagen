import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import type { Me } from "./api.ts";
import { stubFetch } from "./dom.ts";
import type { UpdateState, Updates } from "./pwa.ts";
import { DrawerButton, drawerSwiped, Sidebar } from "./Sidebar.tsx";
import { UpdatePrompt } from "./UpdatePrompt.tsx";

// The phone's drawer and the installed app's update prompt, driven as a
// person would: keys, taps and swipes.

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const me: Me = {
  principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
  permissions: ["runs:read"],
  first_admin: false,
};

/** The console's frame on a phone: the drawer, its button and the page under it. */
function Phone({ startOpen = false }: { startOpen?: boolean }) {
  const [open, setOpen] = useState(startOpen);
  return (
    <>
      <Sidebar
        open={open}
        drawer={open}
        onToggle={() => setOpen((was) => !was)}
        route={{ page: "new" }}
        go={() => {}}
        me={me}
        prefs={null}
        can={(permission) => permission === "runs:read"}
        threads={[]}
        pinned={[]}
        onPin={async () => {}}
        inboxCount={0}
        live
        onSignOut={() => {}}
      />
      <DrawerButton open={open} inboxCount={0} onOpen={() => setOpen(true)} />
      <main inert={open || undefined}>
        <button type="button">Page action</button>
      </main>
    </>
  );
}

test("the phone's drawer takes focus, makes the page inert, and Escape puts it away", async () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  render(<Phone />);
  fireEvent.click(screen.getByTestId("drawer-button"));
  const close = await screen.findByRole("button", { name: "Close sidebar" });
  assert.equal(document.activeElement, close, "focus moves into the drawer");
  assert.equal(document.querySelector("main")?.hasAttribute("inert"), true);
  assert.ok(screen.getByRole("button", { name: defaultCopy.sidebar.dismiss }));

  fireEvent.keyDown(document, { key: "Escape" });
  await waitFor(() => assert.equal(screen.queryByRole("button", { name: "Close sidebar" }), null));
  assert.equal(document.querySelector("main")?.hasAttribute("inert"), false);
  await waitFor(() =>
    assert.equal(
      document.activeElement?.getAttribute("data-testid"),
      "drawer-button",
      "focus returns to the button that opened it",
    ),
  );
});

test("an Escape something inside the drawer already used does not close it", async () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  render(<Phone startOpen />);
  const close = await screen.findByRole("button", { name: "Close sidebar" });
  close.addEventListener("keydown", (e) => e.preventDefault());
  fireEvent.keyDown(close, { key: "Escape" });
  assert.ok(screen.getByRole("button", { name: "Close sidebar" }));
});

test("a swipe toward the edge closes the drawer; scrolling its lists does not", async () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  render(<Phone startOpen />);
  const drawer = await screen.findByRole("complementary", { name: "Navigation" });
  // Mostly down: a scroll.
  fireEvent.touchStart(drawer, { touches: [{ clientX: 200, clientY: 300 }] });
  fireEvent.touchEnd(drawer, { changedTouches: [{ clientX: 150, clientY: 500 }] });
  assert.ok(screen.getByRole("button", { name: "Close sidebar" }));
  // Left and level: a swipe.
  fireEvent.touchStart(drawer, { touches: [{ clientX: 220, clientY: 300 }] });
  fireEvent.touchEnd(drawer, { changedTouches: [{ clientX: 120, clientY: 310 }] });
  await waitFor(() => assert.equal(screen.queryByRole("button", { name: "Close sidebar" }), null));
});

test("drawerSwiped: far enough left, and more sideways than up or down", () => {
  assert.equal(drawerSwiped({ x: 200, y: 0 }, { x: 140, y: 10 }), true);
  assert.equal(drawerSwiped({ x: 200, y: 0 }, { x: 160, y: 0 }), false, "too short");
  assert.equal(drawerSwiped({ x: 100, y: 0 }, { x: 200, y: 0 }), false, "the wrong way");
  assert.equal(drawerSwiped({ x: 200, y: 0 }, { x: 120, y: 70 }), false, "too steep");
});

/** A store the test moves by hand. */
function fakeUpdates(): Updates & { offer(): void; accepted: number } {
  const listeners = new Set<() => void>();
  let state: UpdateState = { waiting: false };
  const set = (next: UpdateState) => {
    state = next;
    for (const fn of listeners) fn();
  };
  const u = {
    accepted: 0,
    subscribe(fn: () => void) {
      listeners.add(fn);
      return () => listeners.delete(fn);
    },
    get: () => state,
    accept() {
      u.accepted++;
    },
    dismiss() {
      set({ waiting: false });
    },
    offer() {
      set({ waiting: true });
    },
  };
  return u;
}

test("the update prompt shows only for a waiting version, and Reload and Later do what they say", async () => {
  const updates = fakeUpdates();
  const { container } = render(<UpdatePrompt updates={updates} />);
  assert.equal(container.textContent, "", "nothing until a new version is waiting");
  updates.offer();
  const status = await screen.findByRole("status");
  assert.equal(status.textContent, defaultCopy.app.updateReady);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.app.reload }));
  assert.equal(updates.accepted, 1);
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.app.later }));
  await waitFor(() => assert.equal(screen.queryByRole("status"), null));
});

test("the update prompt waits on a person, so it does not animate", async () => {
  const updates = fakeUpdates();
  updates.offer();
  const { container } = render(<UpdatePrompt updates={updates} />);
  await screen.findByRole("status");
  for (const el of container.querySelectorAll("*")) {
    const cls = el.getAttribute("class") ?? "";
    assert.doesNotMatch(cls, /animate-|humagen-(fade|pop|drawer|note|rise)/, cls);
  }
});
