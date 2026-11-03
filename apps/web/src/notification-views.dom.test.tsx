import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { InboxPage } from "./InboxPage.tsx";
import { leaves, marked } from "./inbox.ts";
import type { MailCounts, MailMark, MailView, PlatformMessage, Transport } from "./mail.ts";

const vc = defaultCopy.inboxViews;

let net: ReturnType<typeof stubFetch> | undefined;
afterEach(() => {
  cleanup();
  net?.restore();
  net = undefined;
});

const message = (
  id: string,
  subject: string,
  extra: Partial<PlatformMessage> = {},
): PlatformMessage => ({
  id,
  subject,
  from: { kind: "agent", name: "lead" },
  at: "2026-11-02T12:00:00Z",
  parts: [{ id: "body", type: "text", text: `${subject} body.` }],
  kind: "artifact",
  about: [],
  budgets: {},
  sender: "lead",
  unread: true,
  ...extra,
});

/**
 * A reader's inbox held in memory, as the platform keeps it: their own marks
 * decide which view each message is in.
 */
function inbox(seed: PlatformMessage[]) {
  const all = new Map(seed.map((m) => [m.id, { ...m }]));
  const calls: { ids: string[]; mark: MailMark }[] = [];
  const asked: (MailView | undefined)[] = [];
  const readAll: MailView[] = [];
  const reads: string[] = [];
  const inView = (m: PlatformMessage, view?: MailView) => {
    if (m.deleted) return false;
    switch (view) {
      case "hidden":
        return Boolean(m.hidden);
      case "unread":
        return !m.hidden && Boolean(m.unread) && !m.dismissedAt;
      case "done":
        return !m.hidden && Boolean(m.dismissedAt);
      default:
        return !m.hidden && !m.dismissedAt;
    }
  };
  const counts = (): MailCounts => {
    const list = [...all.values()];
    return {
      open: list.filter((m) => inView(m, "inbox")).length,
      unread: list.filter((m) => inView(m, "unread")).length,
      updates: 0,
      inbox: list.filter((m) => inView(m, "inbox")).length,
      hidden: list.filter((m) => inView(m, "hidden")).length,
    };
  };
  const via: Transport = {
    list: async (q) => {
      asked.push(q.view);
      const c = counts();
      return {
        messages: [...all.values()].filter((m) => inView(m, q.view)),
        next: null,
        ...c,
      };
    },
    get: async (id) => {
      const m = all.get(id);
      if (!m) throw new Error("not found");
      return m;
    },
    send: async (m, r) => {
      if (r.call === "read") {
        reads.push(m.id);
        const held = all.get(m.id);
        if (held) held.unread = false;
      }
      return all.get(m.id) ?? m;
    },
    mark: async (ids, mark) => {
      calls.push({ ids, mark });
      for (const id of ids) {
        const m = all.get(id);
        if (m) all.set(id, marked(m, mark));
      }
      return {
        messages: ids.map((id) => all.get(id)).filter((m): m is PlatformMessage => !!m),
        skipped: [],
        counts: counts(),
      };
    },
    readAll: async (view) => {
      readAll.push(view);
      for (const m of all.values()) if (inView(m, view)) m.unread = false;
      return { marked: 1, more: false, counts: counts() };
    },
  };
  return { via, calls, asked, readAll, reads, all };
}

const views = () => screen.getByRole("group", { name: vc.label });
const row = (id: string) => document.querySelector<HTMLElement>(`[data-message="${id}"]`);

test("a mark leaves the view it takes a message out of, and nothing else", () => {
  assert.equal(leaves("inbox", "hide"), true);
  assert.equal(leaves("inbox", "read"), false);
  assert.equal(leaves("unread", "read"), true);
  assert.equal(leaves("hidden", "unhide"), true);
  assert.equal(leaves("hidden", "hide"), false);
  assert.equal(leaves("done", "delete"), true);
  const m = message("m1", "One");
  assert.equal(marked(m, "read").unread, false);
  assert.equal(marked(m, "hide").hidden, true);
  assert.equal(marked(marked(m, "delete"), "restore").deleted, false);
});

test("the Inbox is the default view, each view says its count, and the view is the host's to keep", async () => {
  net = stubFetch({});
  const box = inbox([
    message("m1", "Signups published"),
    message("m2", "Checklist updated", { unread: false }),
    message("m3", "Old notice", { hidden: true }),
  ]);
  const chosen: MailView[] = [];
  const { rerender } = render(
    <InboxPage
      signal={0}
      onOpen={() => {}}
      openId={null}
      via={box.via}
      view="inbox"
      onView={(v) => chosen.push(v)}
    />,
  );
  await waitFor(() => assert.ok(row("m1")));
  assert.equal(box.asked.at(-1), "inbox");
  const segment = (name: string) =>
    within(views()).getByRole("button", { name: new RegExp(`^${name}`) });
  assert.equal(segment(vc.inbox).getAttribute("aria-pressed"), "true");
  assert.match(segment(vc.inbox).textContent ?? "", /2$/);
  assert.match(segment(vc.unread).textContent ?? "", /1$/);
  assert.match(segment(vc.hidden).textContent ?? "", /1$/);
  assert.equal(row("m3"), null, "what the reader hid is not in their Inbox");
  // Unread rows are marked in words, beside the dot, and read rows are not.
  assert.ok(within(row("m1") as HTMLElement).getByText(defaultCopy.mailbox.unread));
  assert.equal(within(row("m2") as HTMLElement).queryByText(defaultCopy.mailbox.unread), null);

  fireEvent.click(segment(vc.hidden));
  assert.deepEqual(chosen, ["hidden"]);
  rerender(
    <InboxPage
      signal={0}
      onOpen={() => {}}
      openId={null}
      via={box.via}
      view="hidden"
      onView={(v) => chosen.push(v)}
    />,
  );
  await waitFor(() => assert.ok(row("m3")));
  assert.equal(box.asked.at(-1), "hidden");
  assert.equal(row("m1"), null);
});

test("hiding from a row moves it to Hidden and offers Undo, which brings it back", async () => {
  net = stubFetch({});
  const box = inbox([message("m1", "Signups published"), message("m2", "Checklist updated")]);
  render(<InboxPage signal={0} onOpen={() => {}} openId={null} via={box.via} />);
  await waitFor(() => assert.ok(row("m1")));
  const actions = screen.getByRole("group", { name: vc.actions("Signups published") });
  fireEvent.click(within(actions).getByRole("button", { name: `${vc.hide}: Signups published` }));
  await waitFor(() => assert.equal(row("m1"), null));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "hide" });
  const toast = await screen.findByTestId("undo-toast");
  assert.ok(within(toast).getByText(vc.hiddenDone(1)));
  fireEvent.click(within(toast).getByRole("button", { name: vc.undo }));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "unhide" });
  await waitFor(() => assert.ok(row("m1")));
  assert.equal(screen.queryByTestId("undo-toast"), null);
});

test("delete asks for no confirmation: it goes, and Undo restores it", async () => {
  net = stubFetch({});
  const box = inbox([message("m1", "Signups published")]);
  render(<InboxPage signal={0} onOpen={() => {}} openId="m1" via={box.via} />);
  const pane = await screen.findByRole("region", { name: defaultCopy.mailbox.reading });
  // Opening it read it, so the pane offers to mark it unread again; it offers no Snooze.
  await waitFor(() => assert.deepEqual(box.reads, ["m1"]));
  await within(pane).findByRole("button", { name: vc.markUnread });
  assert.equal(within(pane).queryByRole("button", { name: defaultCopy.mailbox.snooze }), null);
  assert.equal(screen.queryByRole("dialog"), null);
  fireEvent.click(within(pane).getByRole("button", { name: vc.delete }));
  await waitFor(() => assert.equal(row("m1"), null));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "delete" });
  assert.equal(screen.queryByRole("alertdialog"), null, "no confirm dialog");
  const toast = await screen.findByTestId("undo-toast");
  assert.ok(within(toast).getByText(vc.deletedDone(1)));
  fireEvent.click(within(toast).getByRole("button", { name: vc.undo }));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "restore" });
  await waitFor(() => assert.ok(row("m1")));
});

test("rows are selected to mark many at once, by click or with x, and Mark All Read reads the view", async () => {
  net = stubFetch({});
  const box = inbox([
    message("m1", "Signups published"),
    message("m2", "Checklist updated"),
    message("m3", "Launch ready"),
  ]);
  render(<InboxPage signal={0} onOpen={() => {}} openId={null} via={box.via} />);
  await waitFor(() => assert.ok(row("m3")));
  const bulk = screen.getByRole("toolbar", { name: vc.bulk });
  fireEvent.click(screen.getByRole("checkbox", { name: vc.select("Signups published") }));
  // x on a focused row picks it too.
  fireEvent.click(row("m2") as HTMLElement);
  fireEvent.keyDown(row("m2") as HTMLElement, { key: "x" });
  await waitFor(() => assert.ok(within(bulk).getByText(vc.selected(2))));
  fireEvent.click(within(bulk).getByRole("button", { name: vc.hide }));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1", "m2"].sort(), mark: "hide" });
  await waitFor(() => assert.equal(row("m1"), null));
  assert.equal(row("m2"), null);
  assert.ok(row("m3"));
  // With nothing picked, the toolbar offers Mark All Read for the view.
  fireEvent.click(
    within(screen.getByRole("toolbar", { name: vc.bulk })).getByRole("button", {
      name: vc.markAllRead,
    }),
  );
  await waitFor(() => assert.deepEqual(box.readAll, ["inbox"]));
  await screen.findByText(vc.readAllDone(1));
});

test("a row in a view says no state word: Hidden and Done are views, not labels", async () => {
  net = stubFetch({});
  const box = inbox([
    message("m1", "Release notes", { dismissedAt: "2026-11-02T13:00:00Z", unread: false }),
  ]);
  render(
    <InboxPage
      signal={0}
      onOpen={() => {}}
      openId={null}
      via={box.via}
      view="done"
      onView={() => {}}
    />,
  );
  await waitFor(() => assert.ok(row("m1")));
  const text = row("m1")?.textContent ?? "";
  assert.doesNotMatch(text, /dismissed|hidden/i);
});
