import type { UiAction } from "./actions";

/**
 * The agent UI model. Every surface (web, Slack, ...) renders these types and
 * nothing else, so a concept is defined once and drawn many ways.
 */

/** Semantic color/severity. Maps to Slack alert levels and web tone tokens. */
export type Tone = "ok" | "warn" | "bad" | "info" | "neutral";

/**
 * Lifecycle of a unit of work. The first four mirror Slack task_card statuses;
 * `waiting` (blocked on an approval) and `denied` (a person said no) are ours,
 * and renderers map them onto what each surface supports.
 */
export type TaskStatus = "pending" | "in_progress" | "waiting" | "complete" | "denied" | "error";

/**
 * What the agent is doing right now, for ambient indicators (the web orb, the
 * Slack status line). `waiting` means blocked on a person and must not animate.
 */
export type AgentActivity =
  | "idle"
  | "thinking"
  | "searching"
  | "calling"
  | "delegating"
  | "composing"
  | "waiting";

export interface Citation {
  /** Recorded retrieval facts from the platform. Absent means unproven history. */
  mode?: "indexed" | "live";
  copyAt?: string;
  refreshFailed?: boolean;
  id: string;
  title: string;
  url?: string;
  snippet?: string;
  /** Short origin label, e.g. "Confluence" or "github.com". */
  source?: string;
}

/** The agent a step was handed to, and the run it did the work in. */
export interface Subagent {
  name: string;
  /** The child run. With it the step is clickable: `openRun` carries it as the target. */
  runId?: string;
  /** One line: what it was asked, then what it returned once done. */
  summary?: string;
  steps?: number;
  tokens?: number;
  durationMs?: number;
  /** A plain link to the run, for surfaces or hosts with no drawer. */
  url?: string;
  /** The agent's own hue (0 to 360), so it reads as someone else inside this turn. */
  hue?: number;
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
  /** Bounded arguments for an explicit technical detail view. */
  input?: string;
  durationMs?: number;
  sources?: Citation[];
  /** The approval gating this call, when status is `waiting` or `denied`. */
  interruptId?: string;
  /**
   * A delegated run: another agent did this step. Surfaces draw it as that
   * agent, not as a tool call, and opening it emits `ActionId.openRun`.
   */
  subagent?: Subagent;
  /**
   * Where in the answer this step happened: the length of the turn's text
   * when it started. Set once, so steps and words read in the order they came.
   */
  at?: number;
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
  | { kind: "cancelled"; at: string }
  /** The person asked a question instead of deciding; the call stays halted. */
  | { kind: "asked"; by: string; at: string; question: string };

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
  /**
   * The call runs with the credentials of the person the run acts for. Every
   * surface says so, and only that person may change its arguments, so no
   * card offers an edit of it: a card cannot tell who is reading it.
   */
  actsForPerson?: boolean;
  expiresAt?: string;
  /** Bump when the proposal changes, so clicks on an older copy can be rejected. */
  revision?: number;
  /** Who may decide. Others can see, ask, or stop, but never approve. */
  initiator?: string;
  /** Offer "Always allow {tool}" once trust is earned. */
  rememberable?: boolean;
  /** Set when a sub-agent raised the gate: "Raised by Deploy agent". */
  origin?: string;
  decision: Decision;
}

/** True when `user` may approve or deny. Without an initiator, anyone may. */
export function canDecide(approval: Approval, user: string): boolean {
  return approval.initiator === undefined || approval.initiator === user;
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
  | { kind: "declined"; by: string; at: string; reason?: string }
  /** "I'm not sure": the agent should proceed with its best judgment or ask differently. */
  | { kind: "unsure"; by: string; at: string };

/** The agent needs the human to pick or type something before continuing. */
export interface Clarification {
  type: "clarification";
  id: string;
  question: string;
  options: ClarificationOption[];
  multiple?: boolean;
  allowOther?: boolean;
  /** Offer "I'm not sure" as an answer. */
  allowUnsure?: boolean;
  /**
   * Set when the reader is not someone it is for: whom it is for, in words.
   * The card shows the question and this line, and no way to answer it.
   */
  forOthers?: string;
  answer: Answer;
}

export type Interrupt = Approval | Clarification;

/** Out-of-band message: errors, slow-progress notes, policy notes. Replaces ad-hoc ":warning:" text. */
/**
 * Notice kinds with their own rules:
 * - silence: "Still working, 2m so far". Fires once per turn; never suggests retrying
 *   (a retry would bill a duplicate turn).
 * - model_fallback / retried: muted record of what the runtime did on its own.
 * - spend_blocked: the next turn can't run until budget is added.
 */
export type NoticeKind = "general" | "silence" | "model_fallback" | "retried" | "spend_blocked";

export interface Notice {
  id: string;
  kind?: NoticeKind;
  tone: Tone;
  /** States the level in words ("Deploy failed"); color and emoji never carry it alone. */
  title?: string;
  /** Name the cause and the fix, not "Something went wrong". */
  body: string;
  /** Recovery options: two or three next steps, not a dead end. */
  actions?: NoticeAction[];
  /** Who sees it. "requester" is ephemeral on Slack and hidden from other viewers on the web. */
  audience?: "everyone" | "requester";
  dismissible?: boolean;
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
  /** Correlation id to quote when reporting a bug; surfaces offer it as a copy chip. */
  reference?: string;
  /** Whether re-running the turn can work. Retry is only offered when true. Default by kind. */
  retryable?: boolean;
  /**
   * Why this reader is not offered Retry on a turn that can be retried, by
   * someone else. Said in place of Retry, and in place of saying the turn
   * can't be retried.
   */
  retryNotOffered?: string;
  /** HTTP status from the failing call, when there was one. */
  httpStatus?: number;
}

const RETRYABLE: Record<FailureKind, boolean> = {
  auth_required: false,
  rate_limited: true,
  unavailable: true,
  timeout: true,
  tool_error: true,
  forbidden: false,
  internal: true,
};

export function isRetryable(failure: Failure): boolean {
  return failure.retryable ?? RETRYABLE[failure.kind];
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

export type ArtifactKind =
  | "document"
  | "spreadsheet"
  | "presentation"
  | "image"
  | "pdf"
  | "html"
  | "code"
  | "data"
  | "archive";

/**
 * Lifecycle of a generated file. `waiting` is blocked on an approval and must
 * not animate; `denied` means a person said no, so nothing was created.
 */
export type ArtifactPhase =
  | "queued"
  | "generating"
  | "waiting"
  | "finalizing"
  | "ready"
  | "failed"
  | "denied";

/** A file the agent created or changed. Surfaces show it, preview it, and offer a download. */
export interface Artifact {
  type: "artifact";
  id: string;
  /** The immutable storage record, when the UI id follows a publishing task. */
  artifactId?: string;
  /** File name as the user will save it: "refunds-q3.xlsx". */
  title: string;
  kind: ArtifactKind;
  phase: ArtifactPhase;
  mimeType?: string;
  sizeBytes?: number;
  version?: number;
  /** Where to view it (web preview page, Slack permalink). */
  url?: string;
  /** Direct download. On Slack, hosts usually upload the file and use its permalink. */
  downloadUrl?: string;
  /** One or two lines describing the content: "3 sheets: Summary, Refunds, Disputes". */
  description?: string;
  /** Short text preview for documents and code. */
  previewText?: string;
  thumbnailUrl?: string;
  /** Why it failed, in plain language. */
  error?: string;
}

export type Attachment = CommandRun | CodeSnippet | Artifact;

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 ** 2) return `${Math.round(n / 1024)} KB`;
  if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`;
  return `${(n / 1024 ** 3).toFixed(1)} GB`;
}

export type TurnStatus = "queued" | "working" | "waiting" | "done" | "failed" | "stopped";

/**
 * Stop is two-phase: the UI stops locally at once, the runtime confirms later.
 * Other viewers may still see the turn finish until it is confirmed.
 */
export type StopState = "requested" | "acknowledged" | "failed" | "confirmed";

export interface Usage {
  model?: string;
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  /** Omitted when the model has no price; never shown as $0.00. */
  costUsd?: number;
  latencyMs?: number;
}

/** The turn moved to another agent. */
export interface Handoff {
  to: string;
  reason?: string;
}

export type Rating = "up" | "down";

export interface Agent {
  name: string;
  avatarUrl?: string;
}

/** One agent response, from first event to final state. */
/** Authored result framing. Tool/call records alone establish execution. */
export interface Outcome {
  kind: "recommendation" | "decision" | "update";
  summary: string;
  decisions?: string[];
  nextAction?: string;
}

export interface Thought {
  at: number;
  after: number;
  text: string;
}

export interface Turn {
  outcome?: Outcome;
  id: string;
  agent: Agent;
  status: TurnStatus;
  /** Answer text, standard markdown. Cite inline with `[[citation-id]]` markers. */
  text: string;
  reasoning?: string;
  /**
   * The reasoning in pieces, each where it happened: `at` is the length of
   * the text and `after` the number of steps when the piece began. With
   * `Task.at`, it puts thinking, steps and words back in the order they came.
   */
  thoughts?: Thought[];
  /** Plan title when tasks are shown as a plan rather than a timeline. */
  planTitle?: string;
  tasks: Task[];
  citations: Citation[];
  interrupts: Interrupt[];
  notices: Notice[];
  /** Commands and code shown after the answer. */
  attachments?: Attachment[];
  feedback?: Rating;
  /** What the person added after rating. Optional: the rating stands without it. */
  feedbackComment?: string;
  /** A quick reason picked with "Not helpful" (see FEEDBACK_REASONS). */
  feedbackReason?: string;
  /** Set when status is "failed". */
  failure?: Failure;
  /** Set while or after a stop is in flight. */
  stop?: StopState;
  activity?: AgentActivity;
  /**
   * The live output was closed while the run keeps going (a run quiet for a
   * long time): the host still follows it and shows the end when it comes.
   * Surfaces say so in words and show no motion, since nothing is happening to show.
   */
  detached?: boolean;
  usage?: Usage;
  handoff?: Handoff;
  /** Wall time so far; a finished plan is titled with it ("Worked for 12s"). */
  durationMs?: number;
  /** Deep link to the full session, if the host has one. */
  sessionUrl?: string;
  /**
   * Where the run is coming from: the app, environment or trigger that started
   * it ("Incident console", "Nightly eval"). Shown next to the session link.
   */
  source?: { label: string; url?: string };
  /** Replaces the default footer ("<agent name> is AI…") when the host needs its own wording. */
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
