import { describe, expect, it } from "vitest";
import * as fx from "./fixtures";
import { type HomeNotification, notificationsFromWaiting, orderNotifications } from "./home";

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
