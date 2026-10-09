import type { UiAction } from "./actions";

/**
 * The agent UI model. Every surface (web, Slack, ...) renders these types and
 * nothing else, so a concept is defined once and drawn many ways.
 */

/** Semantic color/severity. Maps to Slack alert levels and web tone tokens. */
export type Tone = "ok" | "warn" | "bad" | "info" | "neutral";

/** Lifecycle of a unit of work. Mirrors Slack task_card/plan statuses. */
export type TaskStatus = "pending" | "in_progress" | "complete" | "error";

export interface Citation {
  id: string;
  title: string;
  url?: string;
  snippet?: string;
  /** Short origin label, e.g. "Confluence" or "github.com". */
  source?: string;
}

/** A tool call, shown to people as a task. Never carries raw tool JSON for display. */
export interface Task {
  id: string;
  /** Human label, e.g. "Searching the docs". */
  title: string;
  status: TaskStatus;
  /** Tool identifier, kept for debugging and web detail views. */
  tool?: string;
  details?: string;
  output?: string;
  sources?: Citation[];
}

/**
 * Every approval settles exactly once. Anything other than "approved" means the
 * action did not run: an unanswered gate must never become a yes.
 */
export type Decision =
  | { kind: "pending" }
  | { kind: "approved"; by: string; at: string; reason?: string }
  | { kind: "denied"; by: string; at: string; reason?: string }
  | { kind: "expired"; at: string }
  | { kind: "cancelled"; at: string };

export interface Field {
  label: string;
  value: string;
}

/** A gated side effect (create, send, delete, spend) awaiting a human decision. */
export interface Approval {
  type: "approval";
  id: string;
  /**
   * First line: the action and its scope, by name not id.
   * "Send this summary to #eng (212 members)", never "Ready to proceed?".
   */
  summary: string;
  tool: string;
  /** Specific verb for the approve button, Title Case: "Roll Back", "Send Summary". */
  approveLabel?: string;
  /** Human-readable preview of exactly what will happen. Supplied by the tool; never raw JSON. */
  preview?: Field[];
  /** Raw arguments, kept for audit and web detail views. Not shown when `preview` is set. */
  args?: Record<string, unknown>;
  /** Destructive actions get a danger button and a confirm dialog. */
  destructive?: boolean;
  /** Offer an Edit button so the user can change the proposal instead of only rejecting. */
  editable?: boolean;
  /** Whose identity the action uses: "you", "the app on behalf of Dana". */
  actingAs?: string;
  expiresAt?: string;
  /** Bump when the proposal changes, so clicks on an older copy can be rejected. */
  revision?: number;
  decision: Decision;
}

export interface ClarificationOption {
  id: string;
  /** What the agent will do, Title Case: "Summarize Last Week". */
  label: string;
  /** One line of context shown above the buttons. */
  description?: string;
}

export type Answer =
  | { kind: "pending" }
  | { kind: "answered"; by: string; at: string; optionIds: string[]; other?: string }
  | { kind: "declined"; by: string; at: string; reason?: string };

/** The agent needs the human to pick or type something before continuing. */
export interface Clarification {
  type: "clarification";
  id: string;
  question: string;
  options: ClarificationOption[];
  multiple?: boolean;
  allowOther?: boolean;
  answer: Answer;
}

export type Interrupt = Approval | Clarification;

/** Out-of-band message: errors, slow-progress notes, policy notes. Replaces ad-hoc ":warning:" text. */
export interface Notice {
  id: string;
  tone: Tone;
  /** States the level in words ("Deploy failed"); color and emoji never carry it alone. */
  title?: string;
  /** Name the cause and the fix, not "Something went wrong". */
  body: string;
  /** Recovery options: two or three next steps, not a dead end. */
  actions?: NoticeAction[];
}

export interface NoticeAction {
  label: string;
  action: UiAction;
}

/**
 * Why a run failed, by who can fix it. Each kind gets its own title, body, and
 * recovery action, so users never see a bare "Something went wrong".
 */
export type FailureKind =
  /** The user must fix a credential: show a Reconnect action. */
  | "auth_required"
  /** A temporary limit: say when to try again. */
  | "rate_limited"
  /** The model provider or a dependency is down. */
  | "unavailable"
  /** A step ran too long. */
  | "timeout"
  /** A tool ran and failed; the failing task carries the detail. */
  | "tool_error"
  /** Blocked by policy or permissions the user can't change. */
  | "forbidden"
  /** A bug on our side. */
  | "internal";

export interface Failure {
  kind: FailureKind;
  /** Plain-language cause, e.g. "The deploys service timed out after 30s". */
  message: string;
  /** System to name in copy: "Linear", "the model provider". */
  service?: string;
  retryAfterSeconds?: number;
  /** Correlation id to quote when reporting a bug. */
  reference?: string;
}

/** A shell command the agent ran. Coding agents show these; others rarely need to. */
export interface CommandRun {
  type: "command";
  id: string;
  command: string;
  cwd?: string;
  status: TaskStatus;
  exitCode?: number;
  durationMs?: number;
  /** Combined output. Surfaces show the tail and say how much was cut. */
  output?: string;
}

/** A code excerpt or file the agent produced or is referring to. */
export interface CodeSnippet {
  type: "code";
  id: string;
  code: string;
  language?: string;
  /** File path or title shown above the code. */
  path?: string;
}

export type Attachment = CommandRun | CodeSnippet;

export type TurnStatus = "queued" | "working" | "waiting" | "done" | "failed" | "stopped";

export type Rating = "up" | "down";

export interface Agent {
  name: string;
  avatarUrl?: string;
}

/** One agent response, from first event to final state. */
export interface Turn {
  id: string;
  agent: Agent;
  status: TurnStatus;
  /** Answer text, standard markdown. Cite inline with `[[citation-id]]` markers. */
  text: string;
  reasoning?: string;
  /** Plan title when tasks are shown as a plan rather than a timeline. */
  planTitle?: string;
  tasks: Task[];
  citations: Citation[];
  interrupts: Interrupt[];
  notices: Notice[];
  /** Commands and code shown after the answer. */
  attachments?: Attachment[];
  feedback?: Rating;
  /** Set when status is "failed". */
  failure?: Failure;
  /** Wall time so far; a finished plan is titled with it ("Worked for 12s"). */
  durationMs?: number;
  /** Deep link to the full session, if the host has one. */
  sessionUrl?: string;
  /** Footer disclaimer, owned by the host so copy lives in one place. */
  disclaimer?: string;
}

export interface SuggestedPrompt {
  /** Short label shown on the chip. */
  title: string;
  /** Full message sent when picked. */
  message: string;
}

/**
 * What the input area offers. The web renders its own prompt bar; Slack has a
 * native composer, so only the suggestions carry over (setSuggestedPrompts).
 */
export interface PromptConfig {
  placeholder?: string;
  suggestions?: SuggestedPrompt[];
}

/**
 * Inline time token for text fields. Each surface localizes it: Slack shows it
 * in the reader's own timezone, the web uses the browser locale.
 */
export function timeToken(iso: string): string {
  return `{{time:${iso}}}`;
}

export const TIME_TOKEN = /\{\{time:([^}]+)\}\}/g;
