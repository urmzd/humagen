import { describe, expect, it } from "vitest";
import { ActionId, type UiAction } from "./actions";
import * as fx from "./fixtures";
import {
  filterMessages,
  type InboxMessage,
  isResolvable,
  messageFromItem,
  messageKind,
  messagePreview,
  messageState,
  openMessageCount,
  openParts,
  partAction,
  partResolution,
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

  it("counts and filters", () => {
    expect(unreadCount(fx.mailMessages)).toBe(5);
    expect(openMessageCount(fx.mailMessages)).toBe(10);
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
});
