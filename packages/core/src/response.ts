/**
 * One response, folded from its stream. A person's message gets exactly one
 * assistant message; every agent brought in is a tool call inside it, and
 * that agent's own blocks stream nested under the call. The root scope is
 * plain Anthropic events; nested work arrives as `humagen.nested`, status as
 * `humagen.tool_status`, transient lines as `humagen.progress`. Live and
 * replayed streams fold the same way. No React.
 */

export type ResponseToolKind = "agent" | "tool" | "context" | "approval" | "provider";
export type ResponseToolStatus =
  | "queued"
  | "running"
  | "awaiting_approval"
  | "awaiting_input"
  | "done"
  | "error"
  | "denied"
  | "stopped";

export interface ResponseCitation {
  title?: string;
  url?: string;
  text?: string;
}

/** What a person is asked, at any depth: approve a call, pick, or reply. */
export interface ResponseQuestion {
  kind: string;
  prompt: string;
  choices?: string[];
  who?: string[] | string;
}

export interface ResponseToolResult {
  text: string;
  sources: ResponseCitation[];
  isError: boolean;
}

export interface ResponseTool {
  type: "tool";
  id: string;
  name: string;
  kind: ResponseToolKind;
  title?: string;
  input: Record<string, unknown>;
  /** For an agent call, the agent it brought in. */
  agent?: string;
  /** For an agent call, the run behind it. */
  runId?: string;
  depth: number;
  status: ResponseToolStatus;
  elapsedMs?: number;
  question?: ResponseQuestion;
  /** The ids from the depth-1 call down to this one, as status sent them. */
  path?: string[];
  progress?: string;
  /** For an approval, who decided and how. */
  decidedBy?: string;
  decision?: string;
  result?: ResponseToolResult;
  /** An agent call's own blocks, in its own index space. */
  content: ResponseBlock[];
  /** Something under this call needs a person or failed: shown on the call so it is never hidden. */
  attention?: { status: ResponseToolStatus; question?: ResponseQuestion; path: string[] };
  /** Kept from the reader by the audience rule. */
  hidden?: boolean;
}

export type ResponseBlock =
  | { type: "text"; text: string; citations: ResponseCitation[]; refusal?: boolean }
  | { type: "thinking"; text: string }
  | ResponseTool
  /** A tool result's slot, kept so indices hold; the result lives on its call. */
  | { type: "result"; toolUseId: string }
  /** A block the reader may not read, or one this client does not know. */
  | { type: "hidden" };

export type ResponseStatus = "running" | "done" | "stopped" | "failed";

export interface ResponseState {
  id?: string;
  requestId?: string;
  rootRun?: string;
  agent?: string;
  ref?: string;
  traceUrl?: string;
  status: ResponseStatus;
  stopReason?: string;
  usage?: Record<string, unknown>;
  costUsd?: number;
  byModel?: Record<string, unknown>;
  content: ResponseBlock[];
  /** Every tool call at every depth, by its minted id. */
  tools: Record<string, ResponseTool>;
  progress?: string;
  error?: string;
  /** Milliseconds from the response's start, from `t_ms` on recorded events. */
  tMs?: number;
  /** Tool inputs still streaming, by scope and index. */
  partial: Record<string, string>;
}

/** One event of the stream, as it arrives: an Anthropic event, or one of Humagen's. */
export type ResponseEvent = { type: string; t_ms?: number } & Record<string, unknown>;

export function emptyResponse(): ResponseState {
  return { status: "running", content: [], tools: {}, partial: {} };
}

const TERMINAL: ResponseToolStatus[] = ["done", "error", "denied", "stopped"];
const AWAITING: ResponseToolStatus[] = ["awaiting_approval", "awaiting_input"];

export const isAwaiting = (s: ResponseToolStatus) => AWAITING.includes(s);
export const isTerminal = (s: ResponseToolStatus) => TERMINAL.includes(s);

type Scope = { content: ResponseBlock[]; depth: number; key: string };

function obj(v: unknown): Record<string, unknown> {
  return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
}
const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);

function citationOf(v: unknown): ResponseCitation {
  const c = obj(v);
  const source = obj(c.source);
  return {
    title: str(c.title) ?? str(c.document_title) ?? str(source.title),
    url: str(c.url) ?? str(c.source) ?? str(source.url),
    text: str(c.cited_text) ?? str(c.text),
  };
}

/** A tool result's content: its text, and the sources an agent call carries as search results. */
function resultOf(content: unknown, isError: boolean): ResponseToolResult {
  if (typeof content === "string") return { text: content, sources: [], isError };
  const parts = Array.isArray(content) ? content : [];
  const text: string[] = [];
  const sources: ResponseCitation[] = [];
  for (const p of parts) {
    const b = obj(p);
    if (b.type === "text" && typeof b.text === "string") text.push(b.text);
    else if (b.type === "search_result") sources.push({ title: str(b.title), url: str(b.source) });
  }
  return { text: text.join("\n\n"), sources, isError };
}

/** The answer: the root's text after its last tool block, or the last agent's result. */
export function answerOf(state: ResponseState): { text: string; from?: string } {
  const root = state.content;
  let last = -1;
  root.forEach((b, i) => {
    if (b.type === "tool" || b.type === "result") last = i;
  });
  const text = root
    .slice(last + 1)
    .flatMap((b) => (b.type === "text" ? [b.text] : []))
    .join("")
    .trim();
  if (text) return { text };
  const agents = allTools(state).filter((t) => t.kind === "agent" && t.result?.text.trim());
  const fallback = agents[agents.length - 1];
  return fallback
    ? { text: fallback.result?.text.trim() ?? "", from: fallback.agent }
    : { text: "" };
}

/** Every tool call at every depth, in the order they started. */
export function allTools(state: ResponseState): ResponseTool[] {
  const out: ResponseTool[] = [];
  const walk = (blocks: ResponseBlock[]) => {
    for (const b of blocks)
      if (b.type === "tool") {
        out.push(b);
        walk(b.content);
      }
  };
  walk(state.content);
  return out;
}

/** "Worked 42s · 3 agents · 7 tools": how long, how many agents and how many tools. */
export function worked(
  state: ResponseState,
  now?: number,
): {
  ms: number;
  agents: number;
  tools: number;
} {
  const tools = allTools(state);
  const longest = Math.max(0, ...tools.map((t) => t.elapsedMs ?? 0));
  return {
    ms: state.tMs ?? now ?? longest,
    agents: tools.filter((t) => t.kind === "agent").length,
    tools: tools.filter((t) => t.kind !== "agent" && t.kind !== "approval").length,
  };
}

/** Calls waiting on a person, at any depth. */
export function waiting(state: ResponseState): ResponseTool[] {
  return allTools(state).filter((t) => isAwaiting(t.status));
}

/** What changed that a person should hear: a call waiting, failing, or the response ending. */
export function changes(before: ResponseState, after: ResponseState): string[] {
  const out: string[] = [];
  for (const t of allTools(after)) {
    const was = before.tools[t.id]?.status;
    if (was === t.status) continue;
    const who = t.agent ?? t.title ?? t.name;
    if (isAwaiting(t.status)) out.push(`${who} is waiting for you`);
    else if (t.status === "error") out.push(`${who} failed`);
    else if (t.status === "denied") out.push(`${who} was denied`);
  }
  if (before.status !== after.status && after.status !== "running")
    out.push(
      after.status === "done"
        ? "Done"
        : after.status === "stopped"
          ? "Stopped"
          : `Failed${after.error ? `: ${after.error}` : ""}`,
    );
  return out;
}

function clone(state: ResponseState): ResponseState {
  // A structured clone keeps shared references, so `tools` still points into
  // the copied tree.
  return structuredClone(state);
}

/** Fold one event into a response. Returns a new state; the old one is unchanged. */
export function foldResponse(prev: ResponseState, event: ResponseEvent): ResponseState {
  const state = clone(prev);
  applyEvent(state, event);
  return state;
}

/** Fold one event into a response in place, for a live stream that renders by version. */
export function applyResponse(state: ResponseState, event: ResponseEvent): ResponseState {
  applyEvent(state, event);
  return state;
}

/** Fold a whole stream, such as a recorded trace. */
export function foldAll(events: Iterable<ResponseEvent>, from = emptyResponse()): ResponseState {
  const state = clone(from);
  for (const e of events) applyEvent(state, e);
  return state;
}

function scopeOf(state: ResponseState, parent?: string): Scope | undefined {
  if (!parent) return { content: state.content, depth: 1, key: "root" };
  const tool = state.tools[parent];
  return tool ? { content: tool.content, depth: tool.depth + 1, key: parent } : undefined;
}

function applyEvent(state: ResponseState, event: ResponseEvent) {
  if (typeof event.t_ms === "number") state.tMs = Math.max(state.tMs ?? 0, event.t_ms);
  switch (event.type) {
    case "message_start": {
      const m = obj(event.message);
      const h = obj(m.humagen);
      state.id = str(m.id) ?? state.id;
      state.requestId = str(h.request_id) ?? state.requestId;
      state.rootRun = str(h.root_run) ?? state.rootRun;
      state.agent = str(h.agent) ?? state.agent;
      state.ref = str(h.ref) ?? state.ref;
      state.traceUrl = str(h.trace_url) ?? state.traceUrl;
      return;
    }
    case "content_block_start":
    case "content_block_delta":
    case "content_block_stop":
      blockEvent(state, scopeOf(state), event);
      return;
    case "message_delta": {
      const d = obj(event.delta);
      const reason = str(d.stop_reason);
      if (reason) {
        state.stopReason = reason;
        state.status =
          reason === "stopped" ? "stopped" : reason === "failed" ? "failed" : state.status;
      }
      if (event.usage) state.usage = obj(event.usage);
      return;
    }
    case "message_stop":
      if (state.status === "running") state.status = "done";
      return;
    case "error": {
      const e = obj(event.error);
      state.error = str(e.message) ?? str(event.message) ?? "Something went wrong";
      state.status = "failed";
      return;
    }
    case "humagen.usage": {
      state.byModel = obj(event.by_model);
      if (typeof event.cost_usd === "number") state.costUsd = event.cost_usd;
      return;
    }
    case "humagen.progress": {
      const parent = str(event.parent_tool_use_id);
      const text = str(event.text);
      if (parent && state.tools[parent]) state.tools[parent].progress = text;
      else state.progress = text;
      return;
    }
    case "humagen.tool_status": {
      statusEvent(state, event);
      return;
    }
    case "humagen.nested": {
      const parent = str(event.parent_tool_use_id);
      const tool = parent ? state.tools[parent] : undefined;
      if (!tool) return;
      const runId = str(event.agent_run_id);
      if (runId) tool.runId = runId;
      const inner = obj(event.event) as ResponseEvent;
      if (
        inner.type === "content_block_start" ||
        inner.type === "content_block_delta" ||
        inner.type === "content_block_stop"
      )
        blockEvent(state, scopeOf(state, parent), inner);
      return;
    }
    default:
      // ping, and anything newer than this client: passed over.
      return;
  }
}

function statusEvent(state: ResponseState, event: ResponseEvent) {
  const id = str(event.tool_use_id);
  const status = str(event.status) as ResponseToolStatus | undefined;
  if (!id || !status) return;
  const path = Array.isArray(event.path)
    ? event.path.filter((p): p is string => typeof p === "string")
    : [];
  const target = state.tools[path[path.length - 1] ?? id] ?? state.tools[id];
  const question = event.question
    ? (obj(event.question) as unknown as ResponseQuestion)
    : undefined;
  if (target) {
    target.status = status;
    if (typeof event.elapsed_ms === "number") target.elapsedMs = event.elapsed_ms;
    const title = str(event.title);
    if (title) target.title = title;
    target.question = isAwaiting(status) ? question : undefined;
    const by = str(event.decided_by);
    if (by) {
      target.decidedBy = by;
      target.decision = str(event.decision);
    }
    if (path.length) target.path = path;
  }
  // Anything awaiting or failing below a depth-1 call is shown on that call too.
  const lane = state.tools[id];
  if (lane && path.length > 1) {
    lane.attention =
      isAwaiting(status) || status === "error" ? { status, question, path } : undefined;
  } else if (lane && lane === target && !isAwaiting(status) && status !== "error") {
    lane.attention = undefined;
  }
}

function blockEvent(state: ResponseState, scope: Scope | undefined, event: ResponseEvent) {
  if (!scope) return;
  const i = typeof event.index === "number" ? event.index : -1;
  if (i < 0) return;
  if (event.type === "content_block_start") {
    const b = obj(event.content_block);
    const prior = scope.content[i];
    delete state.partial[`${scope.key}:${i}`];
    const hidden = b.humagen_redacted === true || b.type === "redacted";
    let block: ResponseBlock;
    switch (b.type) {
      case "text":
        block = {
          type: "text",
          text: str(b.text) ?? "",
          citations: Array.isArray(b.citations) ? b.citations.map(citationOf) : [],
          ...(obj(b.humagen).refusal ? { refusal: true } : {}),
        };
        break;
      case "thinking":
        block = { type: "thinking", text: str(b.thinking) ?? "" };
        break;
      case "server_tool_use":
      case "tool_use": {
        const id = str(b.id) ?? `${scope.depth}:${i}`;
        const input = obj(b.input);
        // A resent block (resume) keeps what its call already gathered.
        const kept = prior?.type === "tool" && prior.id === id ? prior : undefined;
        block = {
          type: "tool",
          id,
          name: str(b.name) ?? "",
          kind: (str(b.kind) as ResponseToolKind) ?? "tool",
          title: str(b.title) ?? kept?.title,
          input,
          agent: str(input.agent) ?? kept?.agent,
          runId: kept?.runId,
          depth: scope.depth,
          status: kept?.status ?? "running",
          elapsedMs: kept?.elapsedMs,
          question: kept?.question,
          path: kept?.path,
          progress: kept?.progress,
          result: kept?.result,
          content: kept?.content ?? [],
          attention: kept?.attention,
          ...(hidden ? { hidden: true } : {}),
        };
        break;
      }
      case "server_tool_result":
      case "tool_result": {
        const toolId = str(b.tool_use_id) ?? "";
        const isError = b.is_error === true;
        const tool = state.tools[toolId];
        if (tool) {
          tool.result = resultOf(b.content, isError);
          if (!isTerminal(tool.status) || tool.status === "done")
            tool.status = isError ? (tool.status === "stopped" ? "stopped" : "error") : "done";
        }
        block = { type: "result", toolUseId: toolId };
        break;
      }
      default:
        block = { type: "hidden" };
    }
    scope.content[i] = hidden && block.type !== "tool" ? { type: "hidden" } : block;
    if (block.type === "tool") state.tools[block.id] = block;
    return;
  }
  const block = scope.content[i];
  if (!block) return;
  if (event.type === "content_block_delta") {
    const d = obj(event.delta);
    switch (d.type) {
      case "text_delta":
        if (block.type === "text") block.text += str(d.text) ?? "";
        return;
      case "citations_delta":
        if (block.type === "text") block.citations.push(citationOf(d.citation));
        return;
      case "thinking_delta":
        if (block.type === "thinking") block.text += str(d.thinking) ?? "";
        return;
      case "input_json_delta":
        state.partial[`${scope.key}:${i}`] =
          (state.partial[`${scope.key}:${i}`] ?? "") + (str(d.partial_json) ?? "");
        return;
      default:
        return;
    }
  }
  if (event.type === "content_block_stop" && block.type === "tool") {
    const json = state.partial[`${scope.key}:${i}`];
    delete state.partial[`${scope.key}:${i}`];
    if (json) {
      try {
        block.input = obj(JSON.parse(json));
        block.agent = str(block.input.agent) ?? block.agent;
        block.title = block.title ?? str(block.input.title);
      } catch {
        // A partial input that never completed stays as it started.
      }
    }
  }
}
