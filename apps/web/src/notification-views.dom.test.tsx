import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { Mailbox } from "@humagen/web";
import { cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { stubFetch } from "./dom.ts";
import { InboxPage } from "./InboxPage.tsx";
import { leaves, marked, unreadBadge } from "./inbox.ts";
import type { MailCounts, MailMark, MailView, PlatformMessage, Transport } from "./mail.ts";
import { DrawerButton } from "./Sidebar.tsx";

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

test("the Inbox alone shows the unread count, and the view is the host's to keep", async () => {
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
  assert.match(segment(vc.inbox).textContent ?? "", /1$/);
  assert.equal(within(views()).queryByRole("button", { name: vc.unread }), null);
  assert.equal(segment(vc.hidden).textContent, vc.hidden);
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
  assert.equal(screen.queryByRole("checkbox", { name: vc.selectAll }), null);
  fireEvent.click(screen.getByRole("checkbox", { name: vc.select("Signups published") }));
  const bulk = screen.getByRole("toolbar", { name: vc.bulk });
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
  fireEvent.click(screen.getByRole("button", { name: vc.markAllRead }));
  await waitFor(() => assert.deepEqual(box.readAll, ["inbox"]));
  await screen.findByText(vc.readAllDone(1));
});

test("a new view starts with nothing picked", () => {
  const messages = [message("m1", "Signups published"), message("m2", "Checklist updated")];
  const views = [
    { id: "inbox", label: vc.inbox, count: 2 },
    { id: "unread", label: vc.unread, count: 2 },
  ];
  const at = (view: string) => (
    <Mailbox
      heading={false}
      messages={messages}
      onAction={() => {}}
      views={views}
      view={view}
      onView={() => {}}
      onMark={() => {}}
    />
  );
  const { rerender } = render(at("inbox"));
  const bulk = () => screen.getByRole("toolbar", { name: vc.bulk });
  fireEvent.click(screen.getByRole("checkbox", { name: vc.select("Signups published") }));
  assert.ok(within(bulk()).getByText(vc.selected(1)));
  // The same mailbox, kept mounted, shown another view.
  rerender(at("unread"));
  assert.equal(screen.queryByRole("toolbar", { name: vc.bulk }), null);
  assert.equal(screen.queryByRole("checkbox", { name: vc.selectAll }), null);
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

test("the badge says what the Unread view says: unread announcements and notices alike", async () => {
  net = stubFetch({});
  // Fresh seed data: one announcement and two artifact notices, all unread.
  // The platform counts the announcement in updates, the notices in unread.
  const seeded = [
    message("a1", "Welcome to Humagen", { kind: "announcement", template: "announcement" }),
    message("n1", "lead published signups.csv"),
    message("n2", "lead updated launch-checklist.md"),
  ];
  const via: Transport = {
    list: async () => ({
      messages: seeded,
      next: null,
      open: 2,
      unread: 2,
      updates: 1,
      inbox: 3,
      hidden: 0,
    }),
    get: async (id) => {
      const found = seeded.find((m) => m.id === id);
      if (!found) throw new Error("not found");
      return found;
    },
    send: async (m) => m,
    mark: async () => ({
      messages: [],
      skipped: [],
      counts: { open: 2, unread: 2, updates: 1, inbox: 3, hidden: 0 },
    }),
  };
  const reported: { open: number; unread: number; updates: number }[] = [];
  render(
    <InboxPage
      signal={0}
      onOpen={() => {}}
      openId={null}
      via={via}
      onCounts={(c) => reported.push(c)}
    />,
  );
  await waitFor(() => assert.ok(row("n2")));
  const unread = within(views()).getByRole("button", { name: new RegExp(`^${vc.inbox}`) });
  assert.match(unread.textContent ?? "", /3$/);
  await waitFor(() => assert.equal(unreadBadge(reported.at(-1) ?? { unread: 0 }), 3));
  // The sidebar draws exactly that number.
  const { container } = render(
    <DrawerButton
      open={false}
      inboxCount={unreadBadge(reported.at(-1) ?? { unread: 0 })}
      onOpen={() => {}}
    />,
  );
  assert.equal(
    container.querySelector("button")?.getAttribute("aria-label"),
    "Open sidebar, 3 unread",
  );
});

test("notification rows lead with the title and agent initials, without a kind chip or duplicate sender", async () => {
  net = stubFetch({});
  const box = inbox([message("m1", "lead published signups.csv")]);
  render(<InboxPage signal={0} onOpen={() => {}} openId={null} via={box.via} />);
  await waitFor(() => assert.ok(row("m1")));
  const item = row("m1")!;
  assert.ok(within(item).getByText("Published signups.csv"));
  assert.equal(within(item).queryByText("lead published signups.csv"), null);
  assert.equal(item.querySelector('[data-aui="kind"]'), null);
  assert.equal(item.querySelector('[data-aui="template-mark"]'), null);
  assert.match(item.textContent ?? "", /lead · lead published signups.csv body/);
  assert.equal(screen.queryByText(/2 open|1 open/), null);
  assert.equal(box.reads.length, 0, "an empty pane never silently reads a preview");
  assert.ok(screen.getByText(defaultCopy.mailbox.pick));
  assert.ok(screen.getByText(defaultCopy.mailbox.shortcuts));
});

test("the legacy unread view is an Inbox filter and keeps a selected message readable", async () => {
  net = stubFetch({});
  const box = inbox([message("m1", "One"), message("m2", "Two", { unread: false })]);
  render(<InboxPage signal={0} onOpen={() => {}} openId={null} via={box.via} view="unread" />);
  await waitFor(() => assert.ok(row("m1")));
  assert.equal(row("m2"), null);
  const unread = screen.getByRole("button", { name: vc.unread, pressed: true });
  assert.ok(within(views()).getByRole("button", { name: /^Inbox/, pressed: true }));
  fireEvent.click(row("m1")!);
  await waitFor(() => assert.deepEqual(box.reads, ["m1"]));
  assert.ok(
    within(screen.getByRole("region", { name: defaultCopy.mailbox.reading })).getByText(
      "One body.",
    ),
  );
  fireEvent.click(unread);
  await waitFor(() => assert.ok(row("m2")));
  assert.equal(
    screen.getByRole("button", { name: vc.unread }).getAttribute("aria-pressed"),
    "false",
  );
  assert.equal(box.asked.at(-1), "inbox");
});

test("j/k navigate, u marks unread, e hides with Undo, and typing never triggers shortcuts", async () => {
  net = stubFetch({});
  const box = inbox([message("m1", "One"), message("m2", "Two")]);
  render(<InboxPage signal={0} onOpen={() => {}} openId={null} via={box.via} />);
  await waitFor(() => assert.ok(row("m1")));
  const search = screen.getByRole("searchbox", { name: defaultCopy.inboxControls.search });
  for (const key of ["j", "k", "e", "u"]) fireEvent.keyDown(search, { key });
  assert.deepEqual(box.reads, []);
  assert.deepEqual(box.calls, []);
  fireEvent.keyDown(row("m1")!, { key: "j" });
  await waitFor(() => assert.deepEqual(box.reads, ["m1"]));
  fireEvent.keyDown(row("m1")!, { key: "j" });
  await waitFor(() => assert.deepEqual(box.reads, ["m1", "m2"]));
  fireEvent.keyDown(row("m2")!, { key: "k" });
  fireEvent.keyDown(row("m1")!, { key: "u" });
  await waitFor(() => assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "unread" }));
  const pane = screen.getByRole("region", { name: defaultCopy.mailbox.reading });
  assert.equal(within(pane).queryByRole("button", { name: vc.markRead }), null);
  fireEvent.click(within(pane).getByRole("button", { name: defaultCopy.mailbox.back }));
  fireEvent.click(row("m1")!);
  await waitFor(() => assert.deepEqual(box.reads, ["m1", "m2", "m1"]));
  fireEvent.keyDown(row("m1")!, { key: "e" });
  await waitFor(() => assert.equal(row("m1"), null));
  assert.deepEqual(box.calls.at(-1), { ids: ["m1"], mark: "hide" });
  fireEvent.click(
    within(await screen.findByTestId("undo-toast")).getByRole("button", { name: vc.undo }),
  );
  await waitFor(() => assert.ok(row("m1")));
});
