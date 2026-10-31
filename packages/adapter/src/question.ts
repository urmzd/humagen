import {
  ActionId,
  type Approval,
  type BudgetGrant,
  type Clarification,
  type Interrupt,
  type UiAction,
} from "@humagen/core";

/**
 * A question a run is parked on, as one shape for every surface: on the run
 * it is an interrupt (humagen's approval or clarification card), in the inbox
 * it is the same card inside a message part. Both are built here, so a
 * question looks and answers the same in the console and in Slack, whether
 * it is read on the run or in the inbox.
 */

/**
 * A question as the platform sends it: `data.question` on `run.parked.v1`,
 * `question` on an inbox part. The generated `Question` is one.
 */
export interface WireQuestion {
  id: string;
  kind: string;
  agent?: string;
  prompt: string;
  options?: readonly string[];
  /**
   * The call an approval is about. `acts_for_person` says it runs with the
   * credential of the person the run acts for: only that person may change
   * its arguments, so no surface offers anyone an edit of it.
   */
  tool?: { name: string; call_id?: string; arguments?: unknown; schema?: unknown; acts_for_person?: boolean };
  asked_of?: string;
  expires_at?: string;
  /**
   * A `hold` question's hold: the platform, not the agent, asked. A hold whose
   * topic is `budget` holds a run at a limit, with the limit in `data`.
   */
  hold?: { id: string; topic?: string; data?: unknown };
  /** A `budget` inbox part's limit, as the platform keeps it on the part. */
  budget?: WireBudget;
}

/** A run held at a limit: the scope and limit, where it stood, and the grant. Money is in micro-USD. */
export interface WireBudget {
  scope_kind: string;
  scope_name?: string;
  limit: string;
  used: number;
  max: number;
  granted?: number;
  grant: number;
}

/** Micro-USD as dollars and cents, as the platform writes it. */
function money(micros: number): string {
  const cents = Math.floor(micros / 10_000);
  return `$${Math.floor(cents / 100)}.${String(cents % 100).padStart(2, "0")}`;
}

/** A budget hold's limit, from the inbox part or the hold's data on the run. */
function wireBudget(q: WireQuestion): WireBudget | undefined {
  if (q.budget) return q.budget;
  const d = q.hold?.topic === "budget" ? q.hold.data : undefined;
  if (!d || typeof d !== "object") return undefined;
  const o = d as Record<string, unknown>;
  const scope = (o.scope ?? {}) as Record<string, unknown>;
  const n = (v: unknown) => (typeof v === "number" ? v : 0);
  return {
    scope_kind: typeof scope.kind === "string" ? scope.kind : "",
    ...(typeof scope.name === "string" ? { scope_name: scope.name } : {}),
    limit: typeof o.limit === "string" ? o.limit : "",
    used: n(o.used),
    max: n(o.max),
    ...(n(o.granted) > 0 ? { granted: n(o.granted) } : {}),
    grant: n(o.grant),
  };
}

/** A budget hold's limit, ready to show. */
export function budgetGrantOf(b: WireBudget): BudgetGrant {
  const tokens = b.limit === "tokens";
  const amount = (v: number) => (tokens ? `${v.toLocaleString("en-US")} tokens` : money(v));
  const scope =
    b.scope_kind === "tenant" || !b.scope_kind ? "the workspace" : `${b.scope_kind} ${b.scope_name ?? ""}`.trim();
  return {
    scope,
    limit: tokens ? "tokens" : "spend",
    used: amount(b.used),
    max: amount(b.max),
    grant: amount(b.grant),
    ...(b.granted ? { granted: amount(b.granted) } : {}),
  };
}

/** True for a question the platform asked of a run held at a limit. */
export const isBudgetQuestion = (q: WireQuestion) =>
  q.kind === "budget" || (q.kind === "hold" && q.hold?.topic === "budget");

/** How a question was settled: a person's answer, or what happened instead. */
export interface Settled {
  /** approve, deny, choose, reply, expired, stopped or closed. */
  action: string;
  /** Who answered, as a person reads it: their name, or `system`. */
  by?: string;
  at: string;
  /** The option chosen, the words replied, a note on a decision, or why nobody answered. */
  note?: string;
}

/** The runtime's answer (`data.answer` on `run.resumed.v1`) as a settlement. */
export function settledOf(answer: {
  decision?: string;
  choice?: string;
  text?: string;
  note?: string;
  by?: string;
  /** What whoever answered is called; `by` is their subject, which is never shown. */
  by_name?: string;
  at?: string;
  expired?: boolean;
}): Settled {
  const at = answer.at ?? "";
  const by = answer.by_name || answer.by;
  if (answer.expired) {
    // The same words the platform settles the inbox message with.
    const went = answer.choice || answer.text;
    const note =
      answer.decision === "deny"
        ? "Nobody answered in time, so it was denied"
        : went
          ? `Nobody answered in time, so it went with ${went}`
          : "Nobody answered in time";
    return { action: "expired", at, note };
  }
  if (answer.decision === "approve" || answer.decision === "deny") {
    return { action: answer.decision, by, at, ...(answer.note ? { note: answer.note } : {}) };
  }
  if (answer.choice) return { action: "choose", by, at, note: answer.choice };
  return { action: "reply", by, at, note: answer.text ?? "" };
}

/** An inbox part's outcome as a settlement, whoever decided it by what they are called. */
export function settledFromOutcome(outcome: { action?: unknown; by?: unknown; by_name?: unknown; at?: unknown; note?: unknown }): Settled | undefined {
  const action = typeof outcome.action === "string" ? outcome.action : "";
  if (!action) return undefined;
  const text = (v: unknown) => (typeof v === "string" ? v : "");
  const by = text(outcome.by) === "system" ? "system" : text(outcome.by_name) || text(outcome.by);
  return { action, by: by || undefined, at: text(outcome.at), ...(text(outcome.note) ? { note: text(outcome.note) } : {}) };
}

const NOBODY = "Nobody";

function decisionOf(s: Settled | undefined): Approval["decision"] {
  if (!s) return { kind: "pending" };
  switch (s.action) {
    case "approve":
      return { kind: "approved", by: s.by ?? "", at: s.at, ...(s.note ? { reason: s.note } : {}) };
    case "deny":
      return { kind: "denied", by: s.by ?? "", at: s.at, ...(s.note ? { reason: s.note } : {}) };
    case "expired":
      return { kind: "expired", at: s.at };
    default:
      // Stopped, or the run ended without an answer.
      return { kind: "cancelled", at: s.at };
  }
}

// humagen: Clarification.answer has no state for "nobody answered in time" or
// "the run was stopped", so both read as declined, with the reason in words.
function answerOf(s: Settled | undefined, options: readonly string[]): Clarification["answer"] {
  if (!s) return { kind: "pending" };
  switch (s.action) {
    case "choose":
      return { kind: "answered", by: s.by ?? "", at: s.at, optionIds: options.includes(s.note ?? "") ? [s.note ?? ""] : [], ...(options.includes(s.note ?? "") ? {} : { other: s.note ?? "" }) };
    case "reply":
      return { kind: "answered", by: s.by ?? "", at: s.at, optionIds: [], other: s.note ?? "" };
    case "expired":
      return { kind: "declined", by: NOBODY, at: s.at, reason: s.note ?? "Nobody answered in time" };
    default:
      return { kind: "declined", by: s.by && s.by !== "system" ? s.by : NOBODY, at: s.at, ...(s.note ? { reason: s.note } : {}) };
  }
}

/** The tool call's arguments, when they are an object. */
function argsOf(q: WireQuestion): Record<string, unknown> | undefined {
  const a = q.tool?.arguments;
  return a && typeof a === "object" && !Array.isArray(a) ? (a as Record<string, unknown>) : undefined;
}

/**
 * The question as humagen's interrupt. An approval is an approval card with the
 * call's arguments; a choice is a clarification with its options; a reply is
 * a clarification that takes words; a run held at a limit is an approval
 * that grants or stops. A question a person does not answer (a delegation,
 * a hold of another topic) is null.
 */
export function interruptOf(q: WireQuestion, settled?: Settled): Interrupt | null {
  const options = q.options ?? [];
  if (isBudgetQuestion(q)) {
    // A run held at a limit is an approval card the platform raised: approve
    // grants it more, deny stops it.
    const b = wireBudget(q);
    return {
      type: "approval",
      id: q.id,
      summary: q.prompt,
      tool: "",
      ...(b ? { budget: budgetGrantOf(b) } : {}),
      ...(q.expires_at ? { expiresAt: q.expires_at } : {}),
      decision: decisionOf(settled),
    };
  }
  switch (q.kind) {
    case "approval": {
      const args = argsOf(q);
      return {
        type: "approval",
        id: q.id,
        summary: q.prompt,
        tool: q.tool?.name ?? "",
        ...(args ? { args } : {}),
        ...(q.expires_at ? { expiresAt: q.expires_at } : {}),
        // Every surface says a call that acts as the person does, and none offers an edit of it.
        ...(q.tool?.acts_for_person === true ? { actsForPerson: true } : {}),
        // humagen: an editor for a tool's arguments from its schema, on web and in a Slack modal,
        // before `editable` can be offered. The API takes edited arguments already. Even then,
        // a call that acts as the person (`actsForPerson`) is never editable: only that
        // person may change it, and a card cannot tell who is reading it.
        decision: decisionOf(settled),
      };
    }
    case "choice":
    case "reply":
      return {
        type: "clarification",
        id: q.id,
        question: q.prompt,
        options: options.map((o) => ({ id: o, label: o })),
        ...(q.kind === "reply" ? { allowOther: true } : {}),
        answer: answerOf(settled, options),
      };
    default:
      return null;
  }
}

/** What answering a question asks of the platform: the run, the question, and the body of `answerQuestion`. */
export interface AnswerRequest {
  run: string;
  question: string;
  body: { action: "approve" | "deny" | "choose" | "reply"; choice?: string; text?: string; note?: string };
}

/**
 * A click on a question shown on a run, as an answer. `turnId` is the run and
 * `target` the question. Null for any other action, or a reply with no words.
 */
export function answerOfAction(action: UiAction): AnswerRequest | null {
  const { id, payload } = action;
  const at = { run: payload.turnId, question: payload.target };
  if (!at.run || !at.question) return null;
  switch (id) {
    case ActionId.approve:
      return { ...at, body: { action: "approve" } };
    case ActionId.deny:
      return { ...at, body: { action: "deny" } };
    case ActionId.clarify:
      return payload.option ? { ...at, body: { action: "choose", choice: payload.option } } : null;
    case ActionId.clarifyOther:
      return payload.text?.trim() ? { ...at, body: { action: "reply", text: payload.text.trim() } } : null;
    default:
      return null;
  }
}

/** True when an action answers a question: something only a person on the run's side can do. */
export const answers = (action: UiAction) =>
  action.id === ActionId.approve || action.id === ActionId.deny || action.id === ActionId.clarify || action.id === ActionId.clarifyOther;

/** The route an answer is sent to. */
export const answerPath = (r: AnswerRequest) =>
  `/api/runs/${encodeURIComponent(r.run)}/questions/${encodeURIComponent(r.question)}/answer`;
