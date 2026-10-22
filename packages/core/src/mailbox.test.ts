import { describe, expect, it } from "vitest";
import { ActionId, type UiAction } from "./actions";
import * as fx from "./fixtures";
import {
  filterMessages,
  type InboxMessage,
  isAnnouncement,
  isResolvable,
  messageAsks,
  messageFromItem,
  messageKind,
  messagePreview,
  messageState,
  messageTemplate,
  openMessageCount,
  openParts,
  parseAmount,
  partAction,
  partResolution,
  raiseAmount,
  resolveMessage,
  sortMessages,
  unreadCount,
} from "./mailbox";

const who = { by: "Dana", at: "2026-10-08T16:00:00Z" };
const get = (id: string) => fx.mailMessages.find((m) => m.id === id) as InboxMessage;
const act = (m: InboxMessage, id: ActionId, extra = {}): UiAction => {
  const part = openParts(m)[0] ?? m.parts[0];
  if (!part) throw new Error("no part");
  return partAction(m, part, id, extra);
};

describe("mailbox", () => {
  it("is open until every part that asks for something has a record", () => {
    const m = get("m-approval");
    expect(messageState(m)).toBe("open");
    expect(openParts(m).map((p) => p.type)).toEqual(["approval"]);
    const done = resolveMessage(m, act(m, ActionId.approve), who);
    expect(messageState(done)).toBe("resolved");
    expect(partResolution(done.parts[1] as never)).toEqual({ outcome: "approved", ...who });
    expect(done.unread).toBe(false);
  });

  it("leaves a read-only message open until it is dismissed", () => {
    const m = get("m-fyi");
    expect(m.parts.some(isResolvable)).toBe(false);
    expect(messageState(m)).toBe("open");
    expect(openMessageCount([m])).toBe(0);
    const gone = resolveMessage(
      m,
      { id: ActionId.dismiss, payload: { turnId: m.id, target: m.id } },
      who,
    );
    expect(messageState(gone)).toBe("dismissed");
  });

  it("brings back what the reader hid for themselves alone, and nothing else", () => {
    const m = get("m-fyi");
    const unhide = { id: ActionId.unhide, payload: { turnId: m.id, target: m.id } };
    const hidden = { ...m, dismissedAt: "2026-10-08T16:00:00Z", hidden: true };
    const back = resolveMessage(hidden, unhide, who);
    expect(messageState(back)).toBe("open");
    expect(back.hidden).toBeUndefined();
    const snoozed = { ...m, snoozedUntil: "2999-01-01T00:00:00Z", hidden: true };
    expect(messageState(resolveMessage(snoozed, unhide, who))).toBe("open");
    // Set aside for everyone, it is not the reader's to bring back.
    const everyone = { ...m, dismissedAt: "2026-10-08T16:00:00Z" };
    expect(resolveMessage(everyone, unhide, who)).toBe(everyone);
  });

  it("resolves each kind of part its own way", () => {
    const choice = get("m-choice");
    expect(
      partResolution(
        resolveMessage(choice, act(choice, ActionId.clarify, { option: "prod" }), who)
          .parts[0] as never,
      ),
    ).toMatchObject({ outcome: "answered", detail: "Check Production" });

    const reply = get("m-reply");
    const replied = resolveMessage(reply, act(reply, ActionId.reply, { text: "Dana" }), who);
    expect(partResolution(replied.parts[1] as never)).toMatchObject({
      outcome: "replied",
      detail: '"Dana"',
    });

    const memory = get("m-memory");
    const kept = resolveMessage(
      memory,
      {
        ...act(memory, ActionId.keepMemories),
        payload: { ...act(memory, ActionId.keepMemories).payload, selection: ["mem1", "mem3"] },
      },
      who,
    );
    expect(kept.parts[0]).toMatchObject({
      kept: ["mem1", "mem3"],
      resolution: { outcome: "kept" },
    });

    const budget = get("m-budget");
    expect(
      resolveMessage(budget, act(budget, ActionId.raiseBudget, { option: "75" }), who).parts[0],
    ).toMatchObject({ resolution: { outcome: "raised", detail: "$75.00" } });
    const runs = get("m-runs");
    expect(messagePreview(runs)).toBe("200 runs of 200 runs this day");
    expect(
      resolveMessage(runs, act(runs, ActionId.raiseBudget, { option: "300" }), who).parts[0],
    ).toMatchObject({ resolution: { detail: "300 runs" } });

    const alert = get("m-alert");
    expect(messageState(resolveMessage(alert, act(alert, ActionId.acknowledge), who))).toBe(
      "resolved",
    );

    const failure = get("m-failure");
    expect(messageState(resolveMessage(failure, act(failure, ActionId.reconnect), who))).toBe(
      "resolved",
    );
  });

  it("ignores actions for another message, a settled part, or the wrong kind of part", () => {
    const m = get("m-budget");
    expect(resolveMessage(m, act(get("m-alert"), ActionId.acknowledge), who)).toBe(m);
    expect(resolveMessage(m, act(m, ActionId.approve), who)).toBe(m);
    const done = resolveMessage(m, act(m, ActionId.allowOnce), who);
    expect(
      resolveMessage(
        done,
        partAction(done, done.parts[0] as never, ActionId.raiseBudget, { option: "100" }),
        who,
      ),
    ).toBe(done);
  });

  it("honours what the reader is allowed to do", () => {
    const m = get("m-budget");
    const part = {
      ...(m.parts[0] as InboxMessage["parts"][number]),
      allowed: [ActionId.allowOnce],
    };
    const limited = { ...m, parts: [part] };
    expect(resolveMessage(limited, act(limited, ActionId.raiseBudget, { option: "75" }), who)).toBe(
      limited,
    );
    expect(messageState(resolveMessage(limited, act(limited, ActionId.allowOnce), who))).toBe(
      "resolved",
    );
  });

  it("reaches a failure part by its id or by the kind the shared card sends", () => {
    const m = get("m-failure");
    for (const target of ["failure", "auth_required"]) {
      const done = resolveMessage(
        m,
        { id: ActionId.acknowledge, payload: { turnId: m.id, target } },
        who,
      );
      expect(done.parts[0]).toMatchObject({ resolution: { outcome: "acknowledged" } });
    }
  });

  it("keeps a question open: asking is not deciding", () => {
    const m = get("m-approval");
    const asked = resolveMessage(m, act(m, ActionId.ask, { text: "Config too?" }), who);
    expect(messageState(asked)).toBe("open");
  });

  it("snoozes until a time, then returns", () => {
    const m = { ...get("m-alert"), snoozedUntil: "2026-10-08T17:00:00Z" };
    expect(messageState(m, "2026-10-08T16:00:00Z")).toBe("snoozed");
    expect(messageState(m, "2026-10-08T18:00:00Z")).toBe("open");
  });

  it("sorts outages first, then decisions, and resolved last", () => {
    const order = sortMessages(fx.mailMessages).map((m) => m.id);
    expect(order[0]).toBe("m-alert");
    expect(order[1]).toBe("m-approval");
    expect(order.at(-1)).toBe("m-done");
  });

  it("acknowledges a limit, or raises it to a typed amount, when the host allows it", () => {
    const limit = get("m-limit");
    const who = { by: "Dana", at: "2026-10-08T15:10:00Z" };
    const payload = { turnId: limit.id, target: "spend" };
    const acked = resolveMessage(limit, { id: ActionId.acknowledge, payload }, who);
    expect(acked.parts[0]).toMatchObject({ resolution: { outcome: "acknowledged", by: "Dana" } });
    const raised = resolveMessage(
      limit,
      { id: ActionId.raiseBudget, payload: { ...payload, option: "400.5" } },
      who,
    );
    expect(raised.parts[0]).toMatchObject({ resolution: { outcome: "raised", detail: "$400.50" } });
    // Allow This Run isn't in its list, so it does nothing.
    expect(resolveMessage(limit, { id: ActionId.allowOnce, payload }, who)).toBe(limit);
  });

  it("raises only to an amount the part can take", () => {
    const limit = get("m-limit");
    const who = { by: "Dana", at: "2026-10-08T15:10:00Z" };
    const raise = (m: InboxMessage, target: string, option: string) =>
      resolveMessage(
        m,
        { id: ActionId.raiseBudget, payload: { turnId: m.id, target, option } },
        who,
      );
    for (const bad of ["250", "100", "abc", "1e3", "-300", "400.555", "Infinity"]) {
      expect(raise(limit, "spend", bad), bad).toBe(limit);
    }
    const capped: InboxMessage = {
      ...limit,
      parts: limit.parts.map((p) => (p.type === "budget" ? { ...p, raiseMax: 500 } : p)),
    };
    expect(raise(capped, "spend", "501")).toBe(capped);
    expect(raise(capped, "spend", "500")).not.toBe(capped);
    // A part that offers amounts takes only those.
    const offered = get("m-budget");
    expect(raise(offered, "limit", "80")).toBe(offered);
    expect(raise(offered, "limit", "75").parts[0]).toMatchObject({
      resolution: { detail: "$75.00" },
    });
    expect(raiseAmount({ meter: { used: 1, limit: 5, unit: "runs" } }, "6.5")).toBeUndefined();
    expect(parseAmount(" 12.50 ", "usd")).toBe(12.5);
    expect(parseAmount("0x10", "runs")).toBeUndefined();
  });

  it("counts and filters", () => {
    expect(unreadCount(fx.mailMessages)).toBe(5);
    expect(openMessageCount(fx.mailMessages)).toBe(11);
    expect(filterMessages(fx.mailMessages, { kind: "approval", state: "open" })).toHaveLength(2);
    expect(filterMessages(fx.mailMessages, { state: "resolved" }).map((m) => m.id)).toEqual([
      "m-done",
    ]);
    expect(
      filterMessages(fx.mailMessages, { unread: true, kind: ["alert", "budget"] }),
    ).toHaveLength(2);
    expect(messageKind(get("m-approval"))).toBe("approval");
    expect(messagePreview(get("m-budget"))).toBe("$48.20 of $50.00 this month");
  });

  it("reads the older inbox items as one-part messages", () => {
    const messages = fx.inboxItems.map((item) => messageFromItem(item));
    expect(messages).toHaveLength(fx.inboxItems.length);
    for (const [i, item] of fx.inboxItems.entries()) {
      const m = messages[i] as InboxMessage;
      expect(m.parts).toHaveLength(1);
      expect(messageState(m)).toBe(item.state);
    }
    // Actions stay addressed the way turns address them: by the interrupt's id.
    const approval = messages.find((m) => messageKind(m) === "approval") as InboxMessage;
    const done = resolveMessage(
      approval,
      { id: ActionId.approve, payload: { turnId: approval.id, target: "a1" } },
      who,
    );
    expect(messageState(done)).toBe("resolved");
  });

  it("lays each kind of message out with its own template", () => {
    const get = (id: string) => fx.mailMessages.find((m) => m.id === id) as InboxMessage;
    expect(messageTemplate(get("m-approval"))).toBe("approval");
    expect(messageTemplate(get("m-choice"))).toBe("question");
    expect(messageTemplate(get("m-reply"))).toBe("question");
    expect(messageTemplate(get("m-memory"))).toBe("memory");
    expect(messageTemplate(get("m-budget"))).toBe("limit");
    expect(messageTemplate(get("m-alert"))).toBe("alert");
    expect(messageTemplate(get("m-failure"))).toBe("failure");
    // Prose and a file, with nothing to decide: it is about the file.
    expect(messageTemplate(get("m-fyi"))).toBe("artifact");
    expect(messageAsks(get("m-fyi"))).toBe(false);
    expect(messageAsks(get("m-approval"))).toBe(true);
    // An agent's prose is its answer; a rule's is a note. A named template wins.
    const text = { ...get("m-fyi"), parts: [{ type: "text" as const, id: "t", text: "Done." }] };
    expect(messageTemplate(text)).toBe("result");
    expect(messageTemplate({ ...text, from: { kind: "rule", name: "Gate" } })).toBe("note");
    expect(messageTemplate({ ...get("m-alert"), template: "limit" })).toBe("limit");
  });

  it("lays an announcement out as one, and never as something that needs the reader", () => {
    const news: InboxMessage = {
      id: "m-news",
      subject: "Renewal signed",
      from: { kind: "person", name: "Dana" },
      at: "2026-10-21T09:00:00Z",
      template: "announcement",
      source: { kind: "project", id: "Globex", label: "Project Globex" },
      parts: [{ type: "text", id: "p1", text: "Globex renewed for two years." }],
    };
    expect(messageTemplate(news)).toBe("announcement");
    expect(messageAsks(news)).toBe(false);
    expect(openMessageCount([news])).toBe(0);
    expect(isAnnouncement(news)).toBe(true);
  });

  it("tells an agent's draft apart from an announcement, though both are laid out as one", () => {
    const draft: InboxMessage = {
      id: "m-draft",
      subject: "Weekly recap",
      from: { kind: "agent", name: "scribe" },
      at: "2026-10-21T09:00:00Z",
      template: "announcement",
      parts: [
        {
          type: "draft",
          id: "d1",
          draft: { subject: "Weekly recap", agent: "scribe", project: "Field" },
        },
      ],
    };
    expect(messageTemplate(draft)).toBe("announcement");
    expect(isAnnouncement(draft)).toBe(false);
    expect(isAnnouncement(get("m-fyi"))).toBe(false);
    // Set aside, an announcement is the reader's own to bring back; a draft is set aside for every owner.
    const news = {
      ...draft,
      id: "m-news",
      from: { kind: "person" as const, name: "Dana" },
      parts: [{ type: "text" as const, id: "p1", text: "Hi." }],
    };
    const dismiss = (m: InboxMessage) =>
      resolveMessage(m, { id: ActionId.dismiss, payload: { turnId: m.id, target: m.id } }, who);
    const snooze = (m: InboxMessage) =>
      resolveMessage(
        m,
        {
          id: ActionId.snooze,
          payload: { turnId: m.id, target: m.id, option: "2999-01-01T00:00:00Z" },
        },
        who,
      );
    expect(dismiss(news).hidden).toBe(true);
    expect(snooze(news).hidden).toBe(true);
    expect(dismiss(draft).hidden).toBeUndefined();
    expect(messageState(dismiss(draft))).toBe("dismissed");
  });
});
