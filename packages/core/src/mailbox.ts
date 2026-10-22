import { ActionId, type UiAction } from "./actions";
import { type InboxItem, type InboxItemState, SERVICE_TONE, type SystemAlert } from "./inbox";
import {
  type Approval,
  type Artifact,
  type Citation,
  type Clarification,
  type Failure,
  isRetryable,
  type Tone,
} from "./types";

/**
 * The inbox as a mailbox. A message is an envelope (who, what, when, where it
 * came from) around an ordered list of parts. Each part is one way of reading
 * or resolving: approve a call, pick an option, type a reply, keep some
 * memories, raise a budget, acknowledge an alert. Alerts decide what to watch
 * for; the inbox is where what they raise gets resolved, where it can be.
 *
 * Parts resolve through the shared action contract (`UiAction`), so a web
 * click and a Slack click mean the same thing, and `resolveMessage` applies
 * either to the model.
 */

export type SenderKind = "agent" | "rule" | "system" | "person";

export interface Sender {
  kind: SenderKind;
  name: string;
  avatarUrl?: string;
}

/** What the message is about, and where to go for the whole story. */
export interface MessageSource {
  kind: "turn" | "run" | "rule" | "policy" | "project";
  label: string;
  id?: string;
  url?: string;
}

/** How a part ended: what happened, who did it, and when. */
export interface Resolution {
  /** A short machine word: "approved", "denied", "acknowledged", "kept", "raised", "retried". */
  outcome: string;
  by: string;
  at: string;
  /** What was typed or chosen, in words a reader can follow. */
  detail?: string;
}

/** Something the agent wants to remember across conversations. */
export interface MemoryProposal {
  id: string;
  text: string;
  /** Where it would apply: "This project", "All conversations". */
  scope?: string;
}

/** Something counted against a limit. */
export interface Meter {
  used: number;
  limit: number;
  /** "usd" is shown as money; anything else follows the number ("runs", "concurrent runs"). */
  unit: string;
  /** "day", "month", "session". Leave out for limits with no period, like concurrency. */
  period?: string;
  /** What the blocked run is expected to add. */
  needs?: number;
}

/** "$48.20" for money, "40 runs" for everything else. */
export function formatAmount(value: number, unit: string): string {
  return unit === "usd" ? `$${value.toFixed(2)}` : `${value} ${unit}`;
}

interface PartBase {
  /** Unique within its message. Actions name it as their `target`. */
  id: string;
  /**
   * What this reader may do to the part, when the host checks permissions.
   * Leave out to allow everything; an empty list makes it read-only for them.
   */
  allowed?: ActionId[];
}

export type MessagePart =
  /** Prose. Read only. */
  | (PartBase & { type: "text"; text: string })
  /** Approve or deny a tool call, with what it will do shown. Its record is `approval.decision`. */
  | (PartBase & { type: "approval"; approval: Approval })
  /** Pick from offered options. Its record is `clarification.answer`. */
  | (PartBase & { type: "choice"; clarification: Clarification })
  /** A free-text answer. */
  | (PartBase & { type: "reply"; prompt: string; placeholder?: string; resolution?: Resolution })
  /** Choose which proposed memories to keep. */
  | (PartBase & {
      type: "memory";
      proposals: MemoryProposal[];
      /** Ids kept, once decided. */
      kept?: string[];
      resolution?: Resolution;
    })
  /**
   * A run blocked on a limit (spend, runs, concurrency): allow it once, or
   * raise the limit. Acknowledge is offered only when `allowed` lists it
   * explicitly, never when `allowed` is left out.
   */
  | (PartBase & {
      type: "budget";
      meter: Meter;
      /**
       * Limits offered, in the meter's unit. When given, these are the only
       * amounts a raise takes; leave out to let the reader type one.
       */
      raiseTo?: number[];
      /** The most a typed raise may be, in the meter's unit. */
      raiseMax?: number;
      resolution?: Resolution;
    })
  /** Something a rule is watching: acknowledge it, snooze it, or open the rule. */
  | (PartBase & { type: "alert"; alert: SystemAlert; ruleUrl?: string; resolution?: Resolution })
  /** A failed run: retry it or open it. */
  | (PartBase & { type: "failure"; failure: Failure; resolution?: Resolution })
  /** Sources. Read only. */
  | (PartBase & { type: "reference"; citations: Citation[] })
  /** A file. Read only. */
  | (PartBase & { type: "artifact"; artifact: Artifact })
  /**
   * A part the host app defines. `name` picks its renderer; surfaces without
   * one show `fallback`. It counts as resolvable only when the host says so.
   */
  | (PartBase & {
      type: "custom";
      name: string;
      data: unknown;
      fallback: string;
      resolvable?: boolean;
      resolution?: Resolution;
    });

export type PartType = MessagePart["type"];

export interface InboxMessage {
  id: string;
  subject: string;
  from: Sender;
  /** When it arrived, ISO. */
  at: string;
  unread?: boolean;
  /** How loud it is in the list. Words always back it up; color never carries it alone. */
  severity?: Tone;
  source?: MessageSource;
  /** Anything else it touches (the rule, the agent, the project), after `source`. */
  sources?: MessageSource[];
  /**
   * The message's tag, when its parts don't say it well: a host's custom part
   * would otherwise read "Message". Shown in the list and above the parts.
   */
  tag?: string;
  /**
   * How the message is laid out (see `messageTemplate`). Leave out to let it
   * follow from the parts; a host sets it when it knows better, such as a
   * limit that leads with an alert part.
   */
  template?: TemplateId;
  parts: MessagePart[];
  /** Set by the reader, not by resolving: put away, or hidden until a time. */
  dismissedAt?: string;
  snoozedUntil?: string;
}

const RESOLVABLE: Record<Exclude<PartType, "custom">, boolean> = {
  text: false,
  reference: false,
  artifact: false,
  approval: true,
  choice: true,
  reply: true,
  memory: true,
  budget: true,
  alert: true,
  failure: true,
};

/**
 * What an action names as its `target` to reach this part. Approvals and
 * choices answer to their interrupt's id, because the shared cards already
 * address them that way inside a turn.
 */
export function partTarget(part: MessagePart): string {
  if (part.type === "approval") return part.approval.id;
  if (part.type === "choice") return part.clarification.id;
  return part.id;
}

/**
 * True when `target` names this part: by its own id, by its interrupt's id,
 * or, for a failure, by the failure kind the shared failure card sends.
 */
export function partMatches(part: MessagePart, target: string): boolean {
  return (
    target === part.id ||
    target === partTarget(part) ||
    (part.type === "failure" && target === part.failure.kind)
  );
}

/** True when the reader may take this action on the part (see `allowed`). */
export function canAct(part: MessagePart, id: ActionId): boolean {
  return part.allowed === undefined || part.allowed.includes(id);
}

/** True when the part asks something of a person. Read-only parts can only be dismissed. */
export function isResolvable(part: MessagePart): boolean {
  return part.type === "custom" ? part.resolvable === true : RESOLVABLE[part.type];
}

/** The part's record, or null while it is still waiting (or never asked for anything). */
export function partResolution(part: MessagePart): Resolution | null {
  switch (part.type) {
    case "approval": {
      const d = part.approval.decision;
      if (d.kind === "pending" || d.kind === "asked") return null;
      return { outcome: d.kind, by: "by" in d ? d.by : "", at: d.at };
    }
    case "choice": {
      const a = part.clarification.answer;
      if (a.kind === "pending") return null;
      const picked = part.clarification.options
        .filter((o) => a.kind === "answered" && a.optionIds.includes(o.id))
        .map((o) => o.label);
      const said = a.kind === "answered" && a.other ? [`"${a.other}"`] : [];
      const detail = [...picked, ...said].join(", ");
      return { outcome: a.kind, by: a.by, at: a.at, ...(detail ? { detail } : {}) };
    }
    case "text":
    case "reference":
    case "artifact":
      return null;
    default:
      return part.resolution ?? null;
  }
}

/** Parts still waiting on a person. */
export function openParts(message: InboxMessage): MessagePart[] {
  return message.parts.filter((p) => isResolvable(p) && partResolution(p) === null);
}

/**
 * Where the message stands. It is resolved once every part that asked for
 * something has its record. A message with nothing to resolve stays open
 * until the reader dismisses it.
 */
export function messageState(message: InboxMessage, now?: string): InboxItemState {
  if (message.dismissedAt) return "dismissed";
  if (message.snoozedUntil && (!now || message.snoozedUntil > now)) return "snoozed";
  const asks = message.parts.filter(isResolvable);
  return asks.length > 0 && openParts(message).length === 0 ? "resolved" : "open";
}

/** The kind of the message for filters and icons: what it asks for first, else what it leads with. */
export function messageKind(message: InboxMessage): PartType {
  return (message.parts.find(isResolvable) ?? message.parts[0])?.type ?? "text";
}

/**
 * A message's template: how it is laid out in the list and the reading pane,
 * and which icon and words it leads with. Each kind of message reads the way
 * its kind needs: an alert leads with its rule and severity, an artifact with
 * the file, a result with the answer, an announcement with its project and
 * who posted it.
 */
export type TemplateId =
  | "alert"
  | "approval"
  | "question"
  | "failure"
  | "memory"
  | "limit"
  | "artifact"
  | "result"
  | "note"
  | "mention"
  | "announcement";

export const TEMPLATES: readonly TemplateId[] = [
  "alert",
  "approval",
  "question",
  "failure",
  "memory",
  "limit",
  "artifact",
  "result",
  "note",
  "mention",
  "announcement",
];

const TEMPLATE_OF_PART: Record<PartType, TemplateId> = {
  alert: "alert",
  approval: "approval",
  choice: "question",
  reply: "question",
  failure: "failure",
  memory: "memory",
  budget: "limit",
  artifact: "artifact",
  reference: "note",
  text: "note",
  custom: "note",
};

/** The message's template: the one it names, else what its first ask (or first part) is. */
export function messageTemplate(message: InboxMessage): TemplateId {
  if (message.template && TEMPLATES.includes(message.template)) return message.template;
  // An update that carries a file is about the file, whatever prose comes first.
  if (!messageAsks(message) && message.parts.some((p) => p.type === "artifact")) return "artifact";
  const t = TEMPLATE_OF_PART[messageKind(message)];
  // Prose from an agent is its answer; from anyone else it is a note.
  return t === "note" && message.from.kind === "agent" ? "result" : t;
}

/**
 * True when the message asks something of a person, as opposed to an update
 * that is only there to be read: a published file, a run's answer, a note.
 * The list keeps the two apart, so updates never bury a decision.
 */
export function messageAsks(message: InboxMessage): boolean {
  return message.parts.some(isResolvable);
}

/** One line for the list, under the subject. */
export function messagePreview(message: InboxMessage): string {
  for (const part of message.parts) {
    const line = partLine(part);
    if (line) return line.replace(/\s+/g, " ").trim();
  }
  return "";
}

function partLine(part: MessagePart): string {
  switch (part.type) {
    case "text":
      return part.text.replace(/[*_`#>]/g, "");
    case "approval":
      return part.approval.summary;
    case "choice":
      return part.clarification.question;
    case "reply":
      return part.prompt;
    case "memory":
      return part.proposals.map((p) => p.text).join(" · ");
    case "budget":
      return `${formatAmount(part.meter.used, part.meter.unit)} of ${formatAmount(part.meter.limit, part.meter.unit)}${part.meter.period ? ` this ${part.meter.period}` : ""}`;
    case "alert":
      return `${part.alert.service}: ${part.alert.title}`;
    case "failure":
      return part.failure.message;
    case "reference":
      return part.citations.map((c) => c.title).join(" · ");
    case "artifact":
      return part.artifact.title;
    case "custom":
      return part.fallback;
  }
}

/** Higher sorts first: outages, then decisions people are blocked on, then the rest. */
export function messagePriority(message: InboxMessage, now?: string): number {
  if (messageState(message, now) !== "open") return 0;
  let top = 5;
  for (const part of openParts(message)) {
    const p =
      part.type === "alert"
        ? part.alert.status === "down"
          ? 50
          : part.alert.status === "degraded"
            ? 30
            : 10
        : part.type === "approval"
          ? part.approval.destructive
            ? 45
            : 40
          : part.type === "budget"
            ? 38
            : part.type === "choice" || part.type === "reply"
              ? 35
              : part.type === "failure"
                ? isRetryable(part.failure)
                  ? 20
                  : 25
                : 15;
    top = Math.max(top, p);
  }
  return top;
}

/** Open messages first by priority, newest first within a priority. */
export function sortMessages(messages: InboxMessage[], now?: string): InboxMessage[] {
  return messages
    .slice()
    .sort((a, b) => messagePriority(b, now) - messagePriority(a, now) || b.at.localeCompare(a.at));
}

export interface MessageFilter {
  /** Match the message's kind (see `messageKind`). */
  kind?: PartType | PartType[];
  state?: InboxItemState | InboxItemState[];
  unread?: boolean;
}

export function filterMessages(
  messages: InboxMessage[],
  filter: MessageFilter,
  now?: string,
): InboxMessage[] {
  const kinds = filter.kind === undefined ? null : [filter.kind].flat();
  const states = filter.state === undefined ? null : [filter.state].flat();
  return messages.filter(
    (m) =>
      (!kinds || kinds.includes(messageKind(m))) &&
      (!states || states.includes(messageState(m, now))) &&
      (filter.unread === undefined || Boolean(m.unread) === filter.unread),
  );
}

export function unreadCount(messages: InboxMessage[]): number {
  return messages.filter((m) => m.unread && !m.dismissedAt).length;
}

/** Messages with something still to resolve: the number for a badge. */
export function openMessageCount(messages: InboxMessage[], now?: string): number {
  return messages.filter((m) => messageState(m, now) === "open" && openParts(m).length > 0).length;
}

const ITEM_SUBJECT: Record<InboxItem["kind"], (i: never) => string> = {
  approval: (i: Extract<InboxItem, { kind: "approval" }>) => i.approval.summary,
  clarification: (i: Extract<InboxItem, { kind: "clarification" }>) => i.clarification.question,
  failure: (i: Extract<InboxItem, { kind: "failure" }>) => i.failure.message,
  alert: (i: Extract<InboxItem, { kind: "alert" }>) => `${i.alert.service}: ${i.alert.title}`,
};

/**
 * An `InboxItem` as a one-part message, so existing producers (`inboxFromTurns`)
 * feed the mailbox unchanged. The part id is the interrupt id, which keeps
 * actions addressed the way turns already address them.
 */
export function messageFromItem(item: InboxItem, from?: Sender): InboxMessage {
  // Failures and alerts have no record of their own, so a resolved item brings one.
  const resolution =
    item.state === "resolved" ? { resolution: { outcome: "resolved", by: "", at: item.at } } : {};
  const part: MessagePart =
    item.kind === "approval"
      ? { type: "approval", id: item.approval.id, approval: item.approval }
      : item.kind === "clarification"
        ? { type: "choice", id: item.clarification.id, clarification: item.clarification }
        : item.kind === "failure"
          ? { type: "failure", id: "failure", failure: item.failure, ...resolution }
          : {
              type: "alert",
              id: item.alert.id,
              alert: item.alert,
              ruleUrl: item.alert.url,
              ...resolution,
            };
  return {
    id: item.id,
    subject: (ITEM_SUBJECT[item.kind] as (i: InboxItem) => string)(item),
    from: from ?? {
      kind: item.kind === "alert" ? "system" : "agent",
      name: item.kind === "alert" ? item.alert.service : "Assistant",
    },
    at: item.at,
    severity:
      item.kind === "alert"
        ? SERVICE_TONE[item.alert.status]
        : item.kind === "failure"
          ? "bad"
          : item.kind === "approval" && item.approval.destructive
            ? "warn"
            : "neutral",
    source: {
      kind: item.kind === "alert" ? "rule" : "turn",
      label: item.source.label,
      id: item.source.turnId,
      url: item.source.url,
    },
    parts: [part],
    ...(item.state === "dismissed" ? { dismissedAt: item.at } : {}),
    ...(item.state === "snoozed" && item.snoozedUntil ? { snoozedUntil: item.snoozedUntil } : {}),
  };
}

/**
 * A typed amount in a unit, or undefined when it isn't one: plain digits,
 * with at most two decimals for money ("usd") and none for counts. Signs,
 * exponents, hex and separators are refused.
 */
export function parseAmount(text: string, unit: string): number | undefined {
  const t = text.trim();
  if (!(unit === "usd" ? /^\d+(\.\d{1,2})?$/ : /^\d+$/).test(t)) return undefined;
  const n = Number(t);
  return Number.isFinite(n) ? n : undefined;
}

/**
 * The new limit a raise asks for, or undefined when the part can't take it:
 * one of `raiseTo` when it offers amounts, otherwise a plain number in the
 * meter's unit (cents at most for money, whole for counts) above the limit
 * and no more than `raiseMax`.
 */
export function raiseAmount(
  part: { meter: Meter; raiseTo?: number[]; raiseMax?: number },
  option: string,
): number | undefined {
  const to = parseAmount(option, part.meter.unit);
  if (to === undefined) return undefined;
  if (part.raiseTo && part.raiseTo.length > 0) return part.raiseTo.includes(to) ? to : undefined;
  if (to <= part.meter.limit) return undefined;
  if (part.raiseMax !== undefined && to > part.raiseMax) return undefined;
  return to;
}

/** The action a part's control emits. `turnId` carries the message id; `target` the part id. */
export function partAction(
  message: InboxMessage,
  part: MessagePart,
  id: ActionId,
  extra: { option?: string; text?: string } = {},
): UiAction {
  return { id, payload: { turnId: message.id, target: partTarget(part), ...extra } };
}

/**
 * Apply an action to a message: the one place that says what each click does
 * to the model. Returns the same message when the action isn't for it, names
 * a part that is already settled, or doesn't fit that part.
 */
export function resolveMessage(
  message: InboxMessage,
  action: UiAction,
  who: { by: string; at: string },
): InboxMessage {
  const { id, payload } = action;
  if (payload.turnId !== message.id) return message;
  const { by, at } = who;
  const typed = payload.text ?? payload.option;
  if (id === ActionId.read) return message.unread ? { ...message, unread: false } : message;
  if (id === ActionId.dismiss) return { ...message, dismissedAt: at, unread: false };
  if (id === ActionId.snooze && !message.parts.some((p) => partMatches(p, payload.target))) {
    return { ...message, snoozedUntil: payload.option ?? at };
  }
  const done = (outcome: string, detail?: string): Resolution => ({
    outcome,
    by,
    at,
    ...(detail ? { detail } : {}),
  });
  const parts = message.parts.map((part): MessagePart => {
    if (!partMatches(part, payload.target) || !isResolvable(part) || partResolution(part)) {
      return part;
    }
    if (!canAct(part, id)) return part;
    switch (part.type) {
      case "approval":
        if (id === ActionId.approve) {
          return {
            ...part,
            approval: { ...part.approval, decision: { kind: "approved", by, at } },
          };
        }
        if (id === ActionId.deny) {
          return { ...part, approval: { ...part.approval, decision: { kind: "denied", by, at } } };
        }
        if (id === ActionId.ask && typed) {
          return {
            ...part,
            approval: {
              ...part.approval,
              decision: { kind: "asked", by, at, question: typed },
            },
          };
        }
        return part;
      case "choice": {
        const answer =
          id === ActionId.clarify && payload.option
            ? { kind: "answered" as const, by, at, optionIds: [payload.option] }
            : id === ActionId.clarifyOther && typed
              ? { kind: "answered" as const, by, at, optionIds: [], other: typed }
              : id === ActionId.unsure
                ? { kind: "unsure" as const, by, at }
                : null;
        return answer ? { ...part, clarification: { ...part.clarification, answer } } : part;
      }
      case "reply":
        return id === ActionId.reply && payload.text
          ? { ...part, resolution: done("replied", `"${payload.text}"`) }
          : part;
      case "memory": {
        if (id !== ActionId.keepMemories && id !== ActionId.discardMemories) return part;
        const kept = id === ActionId.keepMemories ? (payload.selection ?? []) : [];
        return {
          ...part,
          kept,
          resolution: done(kept.length > 0 ? "kept" : "discarded", `${kept.length} kept`),
        };
      }
      case "budget":
        if (id === ActionId.allowOnce) return { ...part, resolution: done("allowed_once") };
        if (id === ActionId.raiseBudget && payload.option) {
          const to = raiseAmount(part, payload.option);
          return to === undefined
            ? part
            : { ...part, resolution: done("raised", formatAmount(to, part.meter.unit)) };
        }
        if (id === ActionId.acknowledge) return { ...part, resolution: done("acknowledged") };
        return part;
      case "alert":
        if (id === ActionId.acknowledge) return { ...part, resolution: done("acknowledged") };
        if (id === ActionId.snooze) return { ...part, resolution: done("snoozed") };
        return part;
      case "failure":
        if (id === ActionId.retry) return { ...part, resolution: done("retried") };
        if (id === ActionId.acknowledge) return { ...part, resolution: done("acknowledged") };
        return id === ActionId.reconnect || id === ActionId.report
          ? { ...part, resolution: done("handled") }
          : part;
      case "custom":
        return { ...part, resolution: done(payload.option ?? "resolved", payload.text) };
      default:
        return part;
    }
  });
  const changed = parts.some((p, i) => p !== message.parts[i]);
  // Acting on a message means it has been read.
  return changed ? { ...message, parts, unread: false } : message;
}
