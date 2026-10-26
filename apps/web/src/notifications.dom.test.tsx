import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { act, cleanup, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { NativeNotification } from "../../../packages/client/src/generated/types.ts";
import type { Me } from "./api.ts";
import { stubFetch } from "./dom.ts";
import { NotificationsPage, useNotifications } from "./Notifications.tsx";
import { PersonalPreferences } from "./PersonalPreferences.tsx";
import type { Route } from "./route.ts";
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
  items: [{ inbox: "in-one", kind: "mention", subject: "Review the release", at: "2026-10-26T11:00:00Z" }],
};

function Notifications({ onGo = () => {} }: { onGo?: (route: Route) => void }) {
  const notifications = useNotifications(true, me.principal.subject);
  return <NotificationsPage notifications={notifications} onGo={onGo} />;
}

function Navigation({ open = true, allowed = true, human = true }: { open?: boolean; allowed?: boolean; human?: boolean }) {
  return <Sidebar open={open} onToggle={() => {}} route={{ page: "notifications" }} go={() => {}}
    me={human ? me : { ...me, principal: { ...me.principal, kind: "machine" } }} prefs={null}
    can={permission => allowed && permission === "runs:read"} threads={[]} pinned={[]}
    onPin={async () => {}} inboxCount={0} notificationCount={2} live onSignOut={() => {}} />;
}

test("notifications are a normal active navigation destination in both the sidebar and icon rail", () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  const { rerender } = render(<Navigation />);
  let link = screen.getByRole("link", { name: /Notifications.*2/ });
  assert.equal(link.getAttribute("href"), "/notifications");
  assert.equal(link.getAttribute("aria-current"), "page");
  assert.doesNotMatch(link.getAttribute("aria-label") ?? "", /need you/i);
  assert.equal(!!link.closest("details"), false);
  rerender(<Navigation open={false} />);
  link = screen.getByRole("link", { name: /Notifications.*2/ });
  assert.equal(link.getAttribute("href"), "/notifications");
  assert.equal(link.getAttribute("aria-current"), "page");
  assert.equal(!!link.closest("details"), false);
});

test("notification navigation is hidden without the permission or human identity required by delivery", () => {
  net = stubFetch({ "GET /api/threads": () => ({ threads: [], next: null }) });
  const { rerender } = render(<Navigation allowed={false} />);
  assert.equal(!!screen.queryByRole("link", { name: /Notifications/ }), false);
  rerender(<Navigation human={false} />);
  assert.equal(!!screen.queryByRole("link", { name: /Notifications/ }), false);
});

test("a disabled notification reader makes no requests and removes a previously visible delivery", async () => {
  net = stubFetch({ "GET /api/me/notifications": () => ({ notifications: [delivered] }) });
  const { result, rerender } = renderHook(({ enabled }) => useNotifications(enabled, "ada"), { initialProps: { enabled: false } });
  await act(() => Promise.resolve());
  assert.equal(net.seen.length, 0);
  assert.deepEqual(result.current.rows, []);
  rerender({ enabled: true });
  await waitFor(() => assert.equal(result.current.rows.length, 1));
  rerender({ enabled: false });
  assert.deepEqual(result.current.rows, []);
  assert.equal(net.seen.length, 1, "the host already knows identity, so delivery does not reread /api/me");
});

test("changing reader ignores an earlier reader's late notification response", async () => {
  const answers: ((value: { notifications: NativeNotification[] }) => void)[] = [];
  net = stubFetch({ "GET /api/me/notifications": () => new Promise(resolve => answers.push(resolve)) });
  const { result, rerender } = renderHook(({ subject }) => useNotifications(true, subject), { initialProps: { subject: "ada" } });
  await waitFor(() => assert.equal(answers.length, 1));
  rerender({ subject: "grace" });
  await waitFor(() => assert.equal(answers.length, 2));
  await act(async () => answers[1]?.({ notifications: [{ ...delivered, id: "ntf-grace" }] }));
  await waitFor(() => assert.equal(result.current.rows[0]?.id, "ntf-grace"));
  await act(async () => answers[0]?.({ notifications: [delivered] }));
  assert.deepEqual(result.current.rows.map(row => row.id), ["ntf-grace"]);
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
  restoreInterval = () => { globalThis.setInterval = originalInterval; };
  let read = 0;
  let answer: (value: { notifications: NativeNotification[] }) => void = () => {};
  net = stubFetch({
    "GET /api/me/notifications": () => ++read === 1 ? { notifications: [delivered] } : new Promise(resolve => { answer = resolve; }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  const { result } = renderHook(() => useNotifications(true, "ada"));
  await waitFor(() => assert.equal(result.current.rows.length, 1));
  await act(async () => poll());
  assert.equal(read, 2);
  await act(async () => result.current.dismiss(delivered.id));
  assert.deepEqual(result.current.rows, []);
  await act(async () => answer({ notifications: [delivered] }));
  assert.deepEqual(result.current.rows, [], "a read begun before dismissal cannot bring back its delivery");
});

test("notification page opens the original inbox message and keeps preferences a distinct address", async () => {
  net = stubFetch({ "GET /api/me/notifications": () => ({ notifications: [delivered] }) });
  const visited: Route[] = [];
  render(<Notifications onGo={route => visited.push(route)} />);
  await screen.findByRole("heading", { name: defaultCopy.notifications.title });
  const message = await screen.findByRole("link", { name: "Review the release" });
  assert.equal(message.getAttribute("href"), "/inbox?message=in-one");
  const preferences = screen.getByRole("link", { name: defaultCopy.notifications.settings });
  assert.equal(preferences.getAttribute("href"), "/you/notifications");
  fireEvent.click(message);
  fireEvent.click(preferences);
  assert.deepEqual(visited, [{ page: "inbox", message: "in-one" }, { page: "you", section: "notifications" }]);
  assert.equal(screen.getAllByRole("heading", { level: 1 }).length, 1);
  assert.equal(!!message.closest("details"), false);
});

test("dismissing a delivered notification acknowledges only its projection", async () => {
  net = stubFetch({
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
    "POST /api/me/notifications/ntf-one/ack": () => ({}),
  });
  render(<Notifications />);
  await screen.findByRole("link", { name: "Review the release" });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.notifications.dismiss }));
  await waitFor(() => assert.equal(!!screen.queryByText("Review the release"), false));
  assert.deepEqual(net.seen.filter(request => request.method === "POST").map(request => request.path), ["/api/me/notifications/ntf-one/ack"]);
});

test("a refused dismissal keeps the notification available to try again", async () => {
  let refuse = true;
  net = stubFetch({
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
    "POST /api/me/notifications/ntf-one/ack": () => refuse ? new Response(null, { status: 503 }) : {},
  });
  render(<Notifications />);
  await screen.findByRole("link", { name: "Review the release" });
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.notifications.dismiss }));
  await screen.findByText(defaultCopy.notifications.dismissFailed);
  assert.equal(!!screen.queryByRole("button", { name: /try again|retry/i }), false, "a refused acknowledgement retries through Dismiss Notification, not a feed reload");
  assert.ok(screen.getByRole("link", { name: "Review the release" }));
  refuse = false;
  fireEvent.click(screen.getByRole("button", { name: defaultCopy.notifications.dismiss }));
  await waitFor(() => assert.equal(!!screen.queryByText("Review the release"), false));
  assert.equal(net.seen.filter(request => request.method === "POST").length, 2);
});

test("a failed notification read offers a working retry and then displays the empty state", async () => {
  let refuse = true;
  net = stubFetch({ "GET /api/me/notifications": () => refuse ? new Response(null, { status: 503 }) : { notifications: [] } });
  render(<Notifications />);
  await screen.findByText(defaultCopy.notifications.failed);
  refuse = false;
  fireEvent.click(screen.getByRole("button", { name: /try again|retry/i }));
  await screen.findByText(defaultCopy.notifications.empty);
  assert.equal(!!screen.queryByText(defaultCopy.notifications.failed), false);
  assert.equal(net.seen.filter(request => request.path === "/api/me/notifications").length, 2);
});

test("notification preferences do not mount a second delivery feed", async () => {
  net = stubFetch({
    "GET /api/me": () => me,
    "GET /api/preferences": () => ({ display_name: "Ada", theme: "system", accent: "purple", has_photo: false, photo_version: 0, pinned: [], muted: [], personal: { briefing: "", notifications: {}, quiet_start: "", quiet_end: "", timezone: "UTC", remember: false, use_personal_memory_in_projects: false } }),
    "GET /api/me/notifications": () => ({ notifications: [delivered] }),
  });
  render(<PersonalPreferences section="notifications" />);
  await screen.findByText(defaultCopy.notifications.description);
  assert.equal(net.seen.some(request => request.path === "/api/me/notifications"), false);
  assert.equal(!!screen.queryByText("Review the release"), false);
});
