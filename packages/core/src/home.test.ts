import { describe, expect, it } from "vitest";
import * as fx from "./fixtures";
import {
  HOME_UPDATES_MAX,
  type HomeNotification,
  type HomeUpdate,
  notificationsFromWaiting,
  orderNotifications,
  orderUpdates,
} from "./home";

const n = (over: Partial<HomeNotification> & Pick<HomeNotification, "id">): HomeNotification => ({
  kind: "inbox",
  from: "docs-bot",
  summary: "wants something.",
  at: "2026-10-13T12:00:00Z",
  ...over,
});

describe("orderNotifications", () => {
  it.each([
    {
      name: "critical alerts, then the inbox, then other alerts",
      items: [
        n({ id: "warn", kind: "alert", severity: "warning", at: "2026-10-13T15:00:00Z" }),
        n({ id: "inbox", at: "2026-10-13T14:00:00Z" }),
        n({ id: "crit", kind: "alert", severity: "critical", at: "2026-10-13T09:00:00Z" }),
      ],
      want: ["crit", "inbox", "warn"],
    },
    {
      name: "newest first within a group",
      items: [
        n({ id: "old", at: "2026-10-12T09:00:00Z" }),
        n({ id: "new", at: "2026-10-13T09:00:00Z" }),
      ],
      want: ["new", "old"],
    },
    {
      name: "by id when two arrive together, so the order is stable",
      items: [n({ id: "b" }), n({ id: "a" })],
      want: ["a", "b"],
    },
    {
      name: "an alert with no severity sits with the warnings",
      items: [n({ id: "plain", kind: "alert" }), n({ id: "inbox", at: "2026-10-01T00:00:00Z" })],
      want: ["inbox", "plain"],
    },
    {
      name: "no time sorts last in its group",
      items: [n({ id: "none", at: "" }), n({ id: "timed" })],
      want: ["timed", "none"],
    },
  ])("orders $name", ({ items, want }) => {
    expect(orderNotifications(items).map((x) => x.id)).toEqual(want);
  });

  it("leaves its input alone", () => {
    const items = [n({ id: "b" }), n({ id: "a" })];
    orderNotifications(items);
    expect(items.map((x) => x.id)).toEqual(["b", "a"]);
  });
});

describe("notificationsFromWaiting", () => {
  it("reads the older waiting list as inbox items", () => {
    expect(
      notificationsFromWaiting([
        { id: "w1", messageId: "m1", from: "author", summary: "drafted a change." },
      ]),
    ).toEqual({
      items: [
        {
          kind: "inbox",
          at: "",
          id: "w1",
          messageId: "m1",
          from: "author",
          summary: "drafted a change.",
        },
      ],
    });
  });
});

describe("home fixtures", () => {
  it("give every notification a stable id and a url, unless the variant is about having none", () => {
    for (const [name, home] of Object.entries(fx.homes)) {
      const keys = home.notifications.items.map((x) => `${x.kind}:${x.id}`);
      expect(new Set(keys).size, name).toBe(keys.length);
      if (name !== "unlinked")
        for (const x of home.notifications.items) expect(x.url, name).toBeTruthy();
    }
  });
});

describe("orderUpdates", () => {
  const update = (id: string, at: string, unread: boolean): HomeUpdate => ({
    id,
    project: "Field",
    projectTitle: "Field engineering",
    subject: `Update ${id}`,
    from: { kind: "person", name: "Dana" },
    at,
    unread,
  });

  it("puts unread first, then the newest, and shows at most five", () => {
    const items = [
      update("a", "2026-10-20T09:00:00Z", false),
      update("b", "2026-10-21T09:00:00Z", false),
      update("c", "2026-10-19T09:00:00Z", true),
      update("d", "2026-10-21T10:00:00Z", true),
      update("e", "2026-10-18T09:00:00Z", false),
      update("f", "2026-10-17T09:00:00Z", false),
      update("g", "2026-10-21T10:00:00Z", true),
    ];
    const shown = orderUpdates(items).map((u) => u.id);
    expect(shown).toEqual(["d", "g", "c", "b", "a"]);
    expect(shown).toHaveLength(HOME_UPDATES_MAX);
  });

  it("keeps an update with no time last among its kind, and changes nothing it is given", () => {
    const items = [update("x", "", true), update("y", "2026-10-21T09:00:00Z", true)];
    expect(orderUpdates(items).map((u) => u.id)).toEqual(["y", "x"]);
    expect(items.map((u) => u.id)).toEqual(["x", "y"]);
  });
});
