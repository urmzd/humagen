import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
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
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import type { Me } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { MailboxView } from "./InboxPage.tsx";
import type { PlatformMessage, Transport } from "./mail.ts";
import { NotificationsPage, useNotifications } from "./Notifications.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import { href, type Route, useRoute } from "./route.ts";
import { Sidebar } from "./Sidebar.tsx";

let net: ReturnType<typeof stubFetch> | undefined;
let restoreInterval: (() => void) | undefined;
afterEach(() => {
  if (!net) return;
  cleanup();
  restoreInterval?.();
  restoreInterval = undefined;
  net.restore();
  net = undefined;
});

const me: Me = {
  principal: { kind: "user", subject: "ada", tenant: "acme", roles: ["operator"] },
  permissions: ["runs:read"],
  first_admin: false,
};
const delivered: NativeNotification = {
  id: "ntf-one",
  mode: "digest",
  at: "2026-10-26T12:00:00Z",
  items: [
    { inbox: "in-one", kind: "mention", subject: "Review the release", at: "2026-10-26T11:00:00Z" },
  ],
};

const original: PlatformMessage = {
  id: "in-one",
  subject: "Review the release",
  from: { kind: "person", name: "Ada" },
  at: "2026-10-26T11:00:00Z",
  parts: [{ id: "body", type: "text", text: "The release is ready for review." }],
  kind: "mention",
  about: [{ kind: "thread", id: "thread-one", label: "Release conversation" }],
  budgets: {},
  sender: "ada",
};
const feed: Transport = {
  list: async () => ({ messages: [original], next: null, open: 1, unread: 0 }),
  get: async () => original,
  send: async (message) => message,
};
function Notifications({
  onGo = () => {},
  openId = "in-one",
}: {
  onGo?: (route: Route) => void;
  openId?: string | null;
}) {
  const notifications = useNotifications(true, me.principal.subject);
  return (
    <NotificationsPage
      notifications={notifications}
      onGo={onGo}
      signal={0}
      viewer="Ada"
      openId={openId}
      via={feed}
      onOpen={(source) => onGo({ page: "conversation", id: source.id })}
    />
  );
}

function Navigation({
  open = true,
  allowed = true,
  human = true,
  inboxCount = 0,
  route = { page: "notifications" },
  onGo = () => {},
}: {
  open?: boolean;
  allowed?: boolean;
  human?: boolean;
  inboxCount?: number;
  route?: Route;
  onGo?: (route: Route) => void;
}) {
  return (
    <Sidebar
      open={open}
      onToggle={() => {}}
      route={route}
      go={onGo}
      me={human ? me : { ...me, principal: { ...me.principal, kind: "machine" } }}
      prefs={null}
      can={(permission) => allowed && ["runs:read", "alerts:read"].includes(permission)}
      threads={[]}
      pinned={[]}
      onPin={async () => {}}
      inboxCount={inboxCount}
      notificationCount={2}
      live
      onSignOut={() => {}}
    />
  );
}

test("Notifications is one active navigation destination in the sidebar and icon rail", () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  const { rerender } = render(<Navigation />);
  let link = screen.getByRole("link", { name: "Alerts" });
  assert.equal(link.getAttribute("href"), "/alerts");
  assert.equal(link.getAttribute("aria-current"), "page");
  assert.doesNotMatch(link.getAttribute("aria-label") ?? "", /need you/i);
  assert.equal(!!link.closest("details"), false);
  assert.equal(screen.queryAllByRole("link", { name: /^Needs You/ }).length, 0);
  rerender(<Navigation open={false} />);
  link = screen.getByRole("link", { name: "Alerts" });
  assert.equal(link.getAttribute("href"), "/alerts");
  assert.equal(link.getAttribute("aria-current"), "page");
  assert.equal(!!link.closest("details"), false);
  assert.equal(screen.getAllByRole("link", { name: /^Alerts/ }).length, 1);
});

test("Notifications hides without run access and gives machines the same feed", () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  const { rerender } = render(<Navigation allowed={false} />);
  assert.equal(!!screen.queryByRole("link", { name: /Alerts/ }), false);
  rerender(<Navigation human={false} />);
  assert.equal(screen.getByRole("link", { name: "Alerts" }).getAttribute("href"), "/alerts");
});

test("the Notifications badge counts unread work without switching destinations", () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  const visited: Route[] = [];
  const onGo = (route: Route) => visited.push(route);
  const { rerender } = render(<Navigation inboxCount={3} route={{ page: "inbox" }} onGo={onGo} />);
  let link = screen.getByRole("link", { name: "Alerts, 3 need you" });
  assert.equal(link.getAttribute("href"), "/alerts");
  assert.equal(link.getAttribute("aria-current"), "page");
  fireEvent.click(link, { metaKey: true });
  assert.deepEqual(visited, [], "modified clicks keep the native link destination");
  fireEvent.click(link);
  assert.deepEqual(visited, [{ page: "alerts" }]);
  rerender(<Navigation inboxCount={3} open={false} onGo={onGo} />);
  link = screen.getByRole("link", { name: "Alerts, 3 need you" });
  assert.equal(link.getAttribute("href"), "/alerts");
  rerender(<Navigation inboxCount={0} onGo={onGo} />);
  link = screen.getByRole("link", { name: "Alerts" });
  assert.equal(link.getAttribute("href"), "/alerts");
  assert.equal(link.hasAttribute("aria-label"), false, "delivery count is not an actionable badge");
});

test("searching updates filters the list without closing its selected reader", () => {
  net = stubFetch({});
  const message: PlatformMessage = {
    id: "selected-update",
    subject: "Launch update",
    from: { kind: "person", name: "Ada" },
    at: "2026-10-26T12:00:00Z",
    parts: [{ type: "text", id: "body", text: "Retained selected detail." }],
    kind: "announcement",
    about: [],
    budgets: {},
    sender: "ada",
  };
  const selected: (string | null)[] = [];
  const props = {
    messages: [message],
    selectedId: message.id,
    onSelect: (id: string | null) => selected.push(id),
    onAction: () => {},
    onOpen: () => {},
  };
  const { rerender } = render(<MailboxView {...props} />);
  const listed = () => Boolean(document.querySelector('[data-message="selected-update"]'));
  assert.equal(listed(), true);
  assert.ok(
    within(screen.getByRole("region", { name: "Message" })).getByText("Retained selected detail."),
  );
  rerender(<MailboxView {...props} query="unmatched query" />);
  assert.ok(
    within(screen.getByRole("region", { name: "Message" })).getByText("Retained selected detail."),
  );
  assert.equal(listed(), false);
  assert.deepEqual(selected, [], "searching never changes the selected message address");
  rerender(<MailboxView {...props} query="" />);
  assert.equal(listed(), true);
  assert.ok(
    within(screen.getByRole("region", { name: "Message" })).getByText("Retained selected detail."),
  );
});

test("a disabled notification reader makes no requests and removes a previously visible delivery", async () => {
  net = stubFetch({ "GET /api/me/notifications": () => ({ notifications: [delivered] }) });
  const { result, rerender } = renderHook(({ enabled }) => useNotifications(enabled, "ada"), {
    initialProps: { enabled: false },
  });
  await act(() => Promise.resolve());
  assert.equal(net.seen.length, 0);
  assert.deepEqual(result.current.rows, []);
  rerender({ enabled: true });
  await waitFor(() => assert.equal(result.current.rows.length, 1));
  rerender({ enabled: false });
  assert.deepEqual(result.current.rows, []);
  assert.equal(
    net.seen.length,
    1,
    "the host already knows identity, so delivery does not reread /api/me",
  );
});

test("changing reader ignores an earlier reader's late notification response", async () => {
  const answers: ((value: { notifications: NativeNotification[] }) => void)[] = [];
  net = stubFetch({
    "GET /api/me/notifications": () => new Promise((resolve) => answers.push(resolve)),
  });
  const { result, rerender } = renderHook(({ subject }) => useNotifications(true, subject), {
    initialProps: { subject: "ada" },
  });
  await waitFor(() => assert.equal(answers.length, 1));
  rerender({ subject: "grace" });
  await waitFor(() => assert.equal(answers.length, 2));
  await act(async () => answers[1]?.({ notifications: [{ ...delivered, id: "ntf-grace" }] }));
  await waitFor(() => assert.equal(result.current.rows[0]?.id, "ntf-grace"));
  await act(async () => answers[0]?.({ notifications: [delivered] }));
  assert.deepEqual(
    result.current.rows.map((row) => row.id),
    ["ntf-grace"],
  );
});

test("a stale polling response cannot restore a dismissed notification", async () => {
  const originalInterval = globalThis.setInterval;
  let poll = () => {};
  globalThis.setInterval = ((handler: () => void, delay: number) => {
    if (delay === 5000) {
      poll = handler;
      return originalInterval(() => {}, delay);
    }
    return originalInterval(handler, delay);
  }) as typeof setInterval;
  restoreInterval = () => {
    globalThis.setInterval = originalInterval;
  };
  let read = 0;
  let answer: (value: { notifications: NativeNotification[] }) => void = () => {};
  net = stubFetch({
    "GET /api/me/notifications": () =>
      ++read === 1
        ? { notifications: [delivered] }
        : new Promise((resolve) => {
            answer = resolve;
          }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  const { result } = renderHook(() => useNotifications(true, "ada"));
  await waitFor(() => assert.equal(result.current.rows.length, 1));
  await act(async () => poll());
  assert.equal(read, 2);
  await act(async () => result.current.dismiss(delivered.id));
  assert.deepEqual(result.current.rows, []);
  await act(async () => answer({ notifications: [delivered] }));
  assert.deepEqual(
    result.current.rows,
    [],
    "a read begun before dismissal cannot bring back its delivery",
  );
});

test("one Notifications feed opens original work and keeps preferences separate", async () => {
  net = stubFetch({ "GET /api/me/notifications": () => ({ notifications: [delivered] }) });
  const visited: Route[] = [];
  render(<Notifications openId={null} onGo={(route) => visited.push(route)} />);
  await screen.findByRole("heading", { name: defaultCopy.notifications.title });
  await waitFor(() => assert.ok(document.querySelector('[data-message="in-one"]')));
  const row = document.querySelector<HTMLElement>('[data-message="in-one"]')!;
  fireEvent.click(row);
  const reader = await screen.findByRole("region", { name: "Message" });
  assert.ok(within(reader).getByText("The release is ready for review."));
  fireEvent.click(
    within(reader).getByRole("button", { name: defaultCopy.notifications.openSource.thread }),
  );
  const preferences = screen.getByRole("link", { name: defaultCopy.notifications.settings });
  assert.equal(preferences.getAttribute("href"), "/settings/notifications");
  fireEvent.click(preferences);
  assert.deepEqual(visited, [
    { page: "conversation", id: "thread-one" },
    { page: "you", section: "notifications" },
  ]);
  assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1);
  assert.equal(
    screen.queryAllByRole("tab").length,
    0,
    "the feed has no Needs You or Updates split",
  );
  assert.equal(
    document.querySelectorAll('[data-message="in-one"]').length,
    1,
    "delivery projections do not duplicate messages",
  );
});

test("dismissing a delivery acknowledges only its projection and preserves original work", async () => {
  net = stubFetch({
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  render(<Notifications />);
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.notifications.dismiss }));
  await waitFor(() =>
    assert.equal(
      !!screen.queryByRole("button", { name: defaultCopy.notifications.dismiss }),
      false,
    ),
  );
  assert.ok(
    document.querySelector('[data-message="in-one"]'),
    "acknowledging delivery does not resolve or hide its message",
  );
  assert.deepEqual(
    net.seen.filter((request) => request.method === "POST").map((request) => request.path),
    ["/api/me/notifications/ntf-one/ack"],
  );
});

test("a refused delivery dismissal keeps the notification available to try again", async () => {
  let refuse = true;
  net = stubFetch({
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
    "POST /api/me/notifications/ntf-one/ack": () =>
      refuse ? new Response(null, { status: 503 }) : {},
  });
  render(<Notifications />);
  fireEvent.click(await screen.findByRole("button", { name: defaultCopy.notifications.dismiss }));
  await screen.findByText(defaultCopy.notifications.dismissFailed);
  assert.equal(!!screen.queryByRole("button", { name: /try again|retry/i }), false);
  assert.ok(document.querySelector('[data-message="in-one"]'));
  refuse = false;
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.notifications.dismiss }));
  await waitFor(() =>
    assert.equal(
      !!screen.queryByRole("button", { name: defaultCopy.notifications.dismiss }),
      false,
    ),
  );
  assert.equal(net.seen.filter((request) => request.method === "POST").length, 2);
});

test("a failed delivery read leaves the unified feed available and offers retry", async () => {
  let refuse = true;
  net = stubFetch({
    "GET /api/me/notifications": () =>
      refuse ? new Response(null, { status: 503 }) : { notifications: [] },
  });
  render(<Notifications />);
  await screen.findByText(defaultCopy.notifications.deliveryFailed);
  assert.ok(document.querySelector('[data-message="in-one"]'));
  refuse = false;
  fireEvent.click(screen.getByRole("button", { name: /try again|retry/i }));
  await waitFor(() =>
    assert.equal(!!screen.queryByText(defaultCopy.notifications.deliveryFailed), false),
  );
  assert.equal(net.seen.filter((request) => request.path === "/api/me/notifications").length, 2);
});

test("the views replace the status filter, the Inbox is the default, and the type filter keeps the selection", async () => {
  net = stubFetch({});
  const queries: Parameters<Transport["list"]>[0][] = [];
  const via: Transport = {
    ...feed,
    list: async (query) => {
      queries.push(query);
      return { messages: query.kind ? [] : [original], next: null, open: 1, unread: 0 };
    },
  };
  render(
    <NotificationsPage signal={0} onGo={() => {}} onOpen={() => {}} openId="in-one" via={via} />,
  );
  await screen.findByRole("region", { name: "Message" });
  assert.equal(queries.at(-1)?.view, "inbox", "the Inbox is the first view");
  assert.equal(queries.at(-1)?.state, undefined);
  assert.equal(screen.queryAllByRole("button", { name: "Open" }).length, 0);
  assert.equal(screen.queryAllByRole("combobox").length, 1);
  assert.equal(screen.queryByRole("combobox", { name: "Status" }), null);
  fireEvent.change(screen.getByRole("combobox", { name: "Type" }), { target: { value: "alert" } });
  await waitFor(() => assert.equal(queries.at(-1)?.kind, "alert"));
  await waitFor(() => assert.equal(!!document.querySelector('[data-message="in-one"]'), false));
  assert.ok(
    within(screen.getByRole("region", { name: "Message" })).getByText(
      "The release is ready for review.",
    ),
  );
  fireEvent.change(screen.getByRole("combobox", { name: "Type" }), { target: { value: "" } });
  await waitFor(() => {
    assert.equal(queries.at(-1)?.view, "inbox");
    assert.equal(queries.at(-1)?.kind, undefined);
  });
});

test("notification preferences do not mount a second delivery feed", async () => {
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/preferences": () => ({
      display_name: "Ada",
      theme: "system",
      accent: "purple",
      has_photo: false,
      photo_version: 0,
      pinned: [],
      muted: [],
      personal: {
        briefing: "",
        notifications: {},
        quiet_start: "",
        quiet_end: "",
        timezone: "UTC",
        remember: false,
        use_personal_memory_in_projects: false,
      },
    }),
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
  });
  render(<PersonalPreferences section="notifications" />);
  await screen.findByText(defaultCopy.personalPreferences.priorityHint);
  assert.equal(
    net.seen.some((request) => request.path === "/api/me/notifications"),
    false,
  );
  assert.equal(!!screen.queryByText("Review the release"), false);
});

test("old notification addresses open the alert center and alert filters survive reload", async () => {
  const previous = window.location.pathname + window.location.search;
  net = stubFetch({});
  function Routed() {
    const [route, go] = useRoute();
    return (
      <>
        <output aria-label="Address">{href(route)}</output>
        <button
          type="button"
          onClick={() =>
            go({ page: "alerts", priority: "urgent", state: "open" }, { replace: true })
          }
        >
          Urgent alerts
        </button>
      </>
    );
  }
  try {
    window.history.replaceState(null, "", "/notifications?message=old");
    render(<Routed />);
    assert.equal(screen.getByLabelText("Address").textContent, "/alerts");
    fireEvent.click(screen.getByRole("button", { name: "Urgent alerts" }));
    await waitFor(() =>
      assert.equal(
        screen.getByLabelText("Address").textContent,
        "/alerts?state=open&priority=urgent",
      ),
    );
    cleanup();
    render(<Routed />);
    assert.equal(
      screen.getByLabelText("Address").textContent,
      "/alerts?state=open&priority=urgent",
    );
  } finally {
    window.history.replaceState(null, "", previous);
  }
});

test("Alert Rules is a quiet page action beside notification settings", async () => {
  net = stubFetch({});
  render(
    <NotificationsPage signal={0} onGo={() => {}} onOpen={() => {}} openId={null} via={feed} />,
  );
  const rules = screen.getByRole("link", { name: defaultCopy.notifications.alertRules });
  assert.equal(rules.getAttribute("href"), "/alerts/watching");
  assert.equal(
    rules.closest("header"),
    screen.getByRole("link", { name: defaultCopy.notifications.settings }).closest("header"),
  );
  assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1);
});

test("notification detail offers the source as a primary next step", async () => {
  net = stubFetch({});
  const sources: PlatformMessage["about"] = [
    { kind: "artifact", id: "file-one", label: "signups.csv" },
    { kind: "thread", id: "conversation-one", label: "Launch" },
    { kind: "run", id: "run-one", label: "Publication" },
  ];
  for (const source of sources) {
    const opened: PlatformMessage["about"] = [];
    const msg = { ...original, about: [source] };
    const via: Transport = {
      ...feed,
      list: async () => ({ messages: [msg], next: null, open: 1, unread: 0 }),
      get: async () => msg,
    };
    const mounted = render(
      <NotificationsPage
        signal={0}
        onGo={() => {}}
        onOpen={(value) => opened.push(value)}
        openId={msg.id}
        via={via}
      />,
    );
    const next = await screen.findByRole("button", {
      name: defaultCopy.notifications.openSource[source.kind],
    });
    assert.equal(next.getAttribute("data-variant"), "default");
    fireEvent.click(next);
    assert.deepEqual(opened, [source]);
    mounted.unmount();
  }
});
