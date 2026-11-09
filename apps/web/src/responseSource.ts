/**
 * Where a response's stream comes from. The console folds every response the
 * same way (`@humagen/core`'s `foldResponse`); only the source differs:
 *
 * - `liveSource`: `GET /api/messages/{id}/stream?depth=all`, server-sent events,
 *   live while the response runs and replayed after.
 * - `replaySource`: a recorded stream, such as a fixture.
 * - `traceSource`: an answer's trace (`GET /api/messages/{id}/trace`), sent
 *   as its stream would send it. While the answer runs, it is read again when
 *   its session reports work by its runs, at most every `TRACE_REFRESH_MS`,
 *   until every response has a live stream.
 */
import type { ResponseEvent } from "@humagen/core";
import type { Run } from "./adapter.ts";
import {
  api,
  type MessageTrace,
  subscribe,
  type TraceApproval,
  type TraceNode,
  type TraceQuestion,
} from "./api.ts";
import { followSession } from "./sessionStream.ts";

export type ResponseSource = {
  /** A stable key: the same key is the same stream. */
  key: string;
  /** Events arrive as they happen, so changes are worth announcing. */
  live?: boolean;
  /**
   * Start sending events; the returned function stops. `onEnd` follows the
   * first full response. A source that reads its response again sends the
   * whole of it to `onSnapshot`, which replaces what came before.
   */
  open: (
    onEvent: (event: ResponseEvent) => void,
    onEnd: () => void,
    onSnapshot?: (events: ResponseEvent[]) => void,
  ) => () => void;
};

/** A recorded stream, sent at once. */
export function replaySource(key: string, events: ResponseEvent[]): ResponseSource {
  return {
    key,
    open: (onEvent, onEnd) => {
      for (const e of events) onEvent(e);
      onEnd();
      return () => {};
    },
  };
}

/** The live stream of a message, every depth. */
export function liveSource(messageId: string): ResponseSource {
  return {
    key: `live:${messageId}`,
    live: true,
    open: (onEvent, onEnd) => {
      const es = new EventSource(`/api/messages/${encodeURIComponent(messageId)}/stream?depth=all`);
      const take = (m: MessageEvent<string>) => {
        try {
          const e = JSON.parse(m.data) as ResponseEvent;
          onEvent(e);
          if (e.type === "message_stop" || e.type === "error") {
            es.close();
            onEnd();
          }
        } catch {
          // A line this client cannot read is passed over.
        }
      };
      es.onmessage = take;
      es.onerror = () => {
        es.close();
        onEnd();
      };
      return () => es.close();
    },
  };
}

/** Follows a session's run events: each event's run and type. Returns a way to stop. */
export type Follow = (thread: string, onRun: (run: string, type: string) => void) => () => void;

const followThread: Follow = (thread, onRun) => {
  try {
    return thread.startsWith("s_")
      ? followSession(thread, { run: (e) => onRun(e.run ?? "", e.type) })
      : subscribe({ thread }, { onEvent: (e) => onRun(e.run ?? "", e.type) });
  } catch {
    return () => {};
  }
};

export type TraceLoader = {
  trace: (id: string) => Promise<MessageTrace>;
  /** How a running answer hears of new work. Without it, a trace is read once. */
  follow?: Follow;
};

const traceLoader: TraceLoader = { trace: api.messageTrace, follow: followThread };
export { traceLoader as defaultTraceLoader };

/** How often, at most, a running answer's trace is read again. */
export const TRACE_REFRESH_MS = 2000;

const ENDED = new Set(["completed", "failed", "stopped"]);

/** An answer's address on the messages API: its session and its run. */
export const messageIdOf = (run: Pick<Run, "id" | "thread">) =>
  run.thread ? `${run.thread}:${run.id}` : run.id;

/** Every run in a trace. */
export function runsOf(trace: MessageTrace): Set<string> {
  const out = new Set<string>();
  const walk = (nodes: TraceNode[]) => {
    for (const n of nodes) {
      out.add(n.run);
      walk(n.children);
    }
  };
  walk(trace.nodes);
  return out;
}

// A run starting may be one the answer just called, which its next trace shows.
const STARTS = new Set(["run.accepted.v1", "run.started.v1"]);

/**
 * While answers run in `thread`, calls `changed` when the session reports
 * work by a run `mine` names, or a run starting, at most every `wait`
 * milliseconds and never while the last call is still reading. Returns a
 * way to stop.
 */
export function followTraces(
  thread: string,
  mine: (run: string) => boolean,
  changed: () => Promise<unknown>,
  follow: Follow,
  wait = TRACE_REFRESH_MS,
): () => void {
  let last = Date.now();
  let timer: ReturnType<typeof setTimeout> | undefined;
  let reading = false;
  let again = false;
  let on = true;
  const read = () => {
    timer = undefined;
    if (!on) return;
    if (reading) {
      again = true;
      return;
    }
    reading = true;
    last = Date.now();
    changed()
      .catch(() => {})
      .finally(() => {
        reading = false;
        if (again && on) {
          again = false;
          soon();
        }
      });
  };
  const soon = () => {
    if (timer || !on) return;
    timer = setTimeout(read, Math.max(0, wait - (Date.now() - last)));
  };
  const stop = follow(thread, (run, type) => {
    if (mine(run) || STARTS.has(type)) soon();
  });
  return () => {
    on = false;
    clearTimeout(timer);
    stop();
  };
}

/** An answer's trace, as its stream would send it, kept current while it runs. */
export function traceSource(
  run: Run,
  loader: TraceLoader = traceLoader,
  wait = TRACE_REFRESH_MS,
): ResponseSource {
  return {
    key: `trace:${run.id}:${run.status}`,
    open: (onEvent, onEnd, onSnapshot) => {
      let live = true;
      let stop = () => {};
      if (ENDED.has(run.status) || !loader.follow || !onSnapshot || !run.thread) {
        eventsOfTrace(run, loader)
          .then((events) => {
            if (!live) return;
            for (const e of events) onEvent(e);
          })
          .catch(() => {})
          .finally(() => live && onEnd());
        return () => {
          live = false;
        };
      }
      const follow = loader.follow;
      const id = messageIdOf(run);
      let runs = new Set([run.id]);
      const ended = (t: MessageTrace) => ENDED.has(t.nodes[0]?.status ?? "");
      const again = () =>
        loader.trace(id).then((t) => {
          if (!live) return;
          runs = runsOf(t);
          onSnapshot(traceEvents(t));
          if (ended(t)) stop();
        });
      loader
        .trace(id)
        .then((t) => {
          if (!live) return;
          runs = runsOf(t);
          for (const e of traceEvents(t)) onEvent(e);
          onEnd();
          if (!ended(t)) stop = followTraces(run.thread, (r) => runs.has(r), again, follow, wait);
        })
        .catch(() => live && onEnd());
      return () => {
        live = false;
        stop();
      };
    },
  };
}

// An ended run's trace does not change, so it is read once per page load.
const read = new Map<string, Promise<ResponseEvent[]>>();

export function eventsOfTrace(
  run: Run,
  loader: TraceLoader = traceLoader,
): Promise<ResponseEvent[]> {
  const key = `${run.id}:${run.status}`;
  const cache = loader === traceLoader && ENDED.has(run.status);
  const cached = cache ? read.get(key) : undefined;
  if (cached) return cached;
  const p = loader.trace(messageIdOf(run)).then(traceEvents);
  if (cache) {
    read.set(key, p);
    p.catch(() => read.delete(key));
  }
  return p;
}

/**
 * The old ways one agent called another. Each became a run of its own, which
 * the stream shows as the agent call, so the tool call that started it is
 * not shown a second time.
 */
const DELEGATION = new Set([
  "call_agent",
  "session_call",
  "session_listen",
  "delegate_task",
  "handoff",
]);
const isDelegation = (name: string) => DELEGATION.has(name) || name.startsWith("tag_");

const statusOf = (s: TraceNode["status"]) =>
  s === "completed"
    ? "done"
    : s === "failed"
      ? "error"
      : s === "stopped"
        ? "stopped"
        : s === "parked"
          ? "awaiting_input"
          : "running";

/** The first line of a brief, as an agent call's title: at most 60 characters. */
export function titleOf(text: string): string {
  const line =
    text
      .split("\n")
      .find((l) => l.trim())
      ?.trim() ?? "";
  return line.length > 60 ? `${line.slice(0, 59).trimEnd()}…` : line;
}

// What a delegation asked, from its arguments: a brief, or its first message's text.
const BRIEF = ["brief", "task", "message", "text", "prompt", "request", "question"];
const textOf = (content: unknown): string =>
  typeof content === "string"
    ? content
    : Array.isArray(content)
      ? content
          .map((p) => (typeof p?.text === "string" ? p.text : ""))
          .filter(Boolean)
          .join("\n")
      : "";
const briefOf = (input?: Record<string, unknown>) => {
  for (const k of BRIEF) if (typeof input?.[k] === "string" && input[k]) return input[k] as string;
  const messages = Array.isArray(input?.messages) ? input.messages : [];
  for (const m of messages) {
    const text = textOf(m?.content);
    if (text) return text;
  }
  return "";
};

/**
 * A message's trace as stream events: each run's tool calls, context reads
 * and the runs it called are calls in its scope, in the order they started;
 * a called run's own work is nested under its call; a run's answer is its
 * text, last. Ids are the platform's (`htu_<run>_<seq>`), so a trace and a
 * live stream name the same call the same way. Runs the reader may not read
 * are private steps.
 */
export function traceEvents(trace: MessageTrace): ResponseEvent[] {
  const [root, ...rest] = trace.nodes;
  if (!root) return [];
  const t0 = Date.parse(root.started_at);
  const ms = (iso?: string) => (iso ? Math.max(0, Date.parse(iso) - t0) : undefined);
  const out: ResponseEvent[] = [
    {
      type: "message_start",
      t_ms: 0,
      message: {
        // The message's own id, which its questions are answered on.
        id: trace.message || root.run,
        role: "assistant",
        content: [],
        humagen: { root_run: root.run, agent: root.agent },
      },
    },
  ];
  const models: Record<string, object> = {};

  const scope = (node: TraceNode, above: string[], extra: TraceNode[] = [], hidden = 0) => {
    const parent = above[above.length - 1];
    const depth = above.length;
    if (node.model) models[node.model] = {};
    const emit = (e: ResponseEvent) =>
      out.push(
        parent
          ? {
              type: "humagen.nested",
              t_ms: e.t_ms,
              parent_tool_use_id: parent,
              agent_run_id: node.run,
              depth,
              event: e,
            }
          : e,
      );
    const status = (id: string, s: string, at?: string, more: Record<string, unknown> = {}) =>
      out.push({
        type: "humagen.tool_status",
        t_ms: ms(at),
        tool_use_id: id,
        path: [...above, id],
        status: s,
        ...more,
      });
    const ended = ENDED.has(node.status);
    // A waiting approval is answered by the call it gates; one that gates
    // no call shows that it waits, and is answered where it was asked.
    const asked = (a: TraceApproval) =>
      a.tool ? { kind: "approval", prompt: a.action, choices: ["Approve", "Deny"] } : undefined;
    // An approval as its call's status: waiting, or how it was decided.
    const decide = (id: string, a: TraceApproval) => {
      if (a.decision === "ask")
        status(id, ended ? "stopped" : "awaiting_approval", a.at, {
          ...(ended || !asked(a) ? {} : { question: asked(a) }),
        });
      else
        status(id, a.decision === "deny" ? "denied" : "running", a.at, {
          ...(a.by
            ? { decided_by: a.by, decision: a.decision === "deny" ? "deny" : "approve" }
            : {}),
        });
    };
    let index = 0;
    if (node.pick?.reason)
      emit({
        type: "content_block_start",
        index: index++,
        content_block: { type: "thinking", thinking: node.pick.reason },
      });
    for (let i = 0; i < hidden; i++)
      emit({ type: "content_block_start", index: index++, content_block: { type: "redacted" } });

    // Each called run takes the id of the delegation that started it.
    const delegations = node.tools.filter((t) => isDelegation(t.name));
    const used = new Set<string>();
    const callOf = (child: TraceNode) => {
      const free = delegations.filter((d) => !used.has(d.id));
      const hit =
        free.find((d) => d.input?.agent === child.agent) ??
        free.find((d) => d.at <= child.started_at) ??
        free[0];
      if (hit) used.add(hit.id);
      return hit;
    };
    type Step =
      | { at: string; kind: "tool"; tool: TraceNode["tools"][number] }
      | { at: string; kind: "context"; read: TraceNode["context"][number] }
      | { at: string; kind: "agent"; child: TraceNode };
    const steps: Step[] = [
      ...node.tools
        .filter((t) => !isDelegation(t.name))
        .map((tool) => ({ at: tool.at, kind: "tool" as const, tool })),
      ...node.context.map((read) => ({ at: read.at, kind: "context" as const, read })),
      ...[...node.children, ...extra].map((child) => ({
        at: child.started_at,
        kind: "agent" as const,
        child,
      })),
    ].sort((a, b) => a.at.localeCompare(b.at));
    const gated = new Set<string>();
    const approvalOf = (id: string) => {
      const a = node.approvals.find((x) => x.tool === id);
      if (a) gated.add(a.id);
      return a;
    };
    // A question the run asked a person with ask_person, on the call that asked it.
    const questions = node.questions ?? [];
    const askedAt = new Set<string>();
    const questionOf = (id: string) => {
      const q = questions.find((x) => x.tool === id);
      if (q) askedAt.add(q.id);
      return q;
    };
    // Waiting while it is asked, answered by the call that asked it; once
    // answered, who answered shows on the call.
    const ask = (id: string, q: TraceQuestion) => {
      if (q.status === "asked" && !ended)
        status(id, "awaiting_input", q.at, {
          ...(q.tool ? { question: { kind: q.kind, prompt: q.prompt, choices: q.options } } : {}),
        });
      else if (q.status === "answered" && q.by)
        status(id, "running", q.at, { decided_by: q.by, decision: q.answer ?? "" });
    };

    for (const step of steps) {
      if (step.kind === "agent") {
        const c = step.child;
        const call = callOf(c);
        const id = call?.id ?? `htu_${node.run}_${c.run}`;
        const title = titleOf(briefOf(call?.input));
        emit({
          type: "content_block_start",
          t_ms: ms(c.started_at),
          index: index++,
          content_block: {
            type: "server_tool_use",
            id,
            name: "agent",
            kind: "agent",
            ...(title ? { title } : {}),
            input: { agent: c.agent, ...(title ? { title, brief: briefOf(call?.input) } : {}) },
          },
        });
        scope(c, [...above, id]);
        if (ENDED.has(c.status))
          emit({
            type: "content_block_start",
            t_ms: ms(c.ended_at),
            index: index++,
            content_block: {
              type: "server_tool_result",
              tool_use_id: id,
              is_error: c.status !== "completed",
              content: [
                { type: "text", text: c.answer ?? c.error ?? "" },
                ...c.sources.map((x) => ({
                  type: "search_result",
                  source: x.url ?? "",
                  title: x.title,
                  content: [],
                })),
              ],
            },
          });
        status(id, statusOf(c.status), c.ended_at ?? c.started_at, {
          elapsed_ms: c.ms,
          ...(title ? { title } : {}),
        });
        // A delegation that waited on a person: how it was decided, on its call.
        const a = approvalOf(id);
        if (a?.by)
          status(id, statusOf(c.status), a.at, {
            decided_by: a.by,
            decision: a.decision === "deny" ? "deny" : "approve",
          });
        continue;
      }
      if (step.kind === "context") {
        const r = step.read;
        emit({
          type: "content_block_start",
          t_ms: ms(r.at),
          index: index++,
          content_block: {
            type: "server_tool_use",
            id: r.id,
            name: "get_context",
            kind: "context",
            input: r.withheld ? {} : { request: r.request },
            ...(r.withheld ? { humagen_redacted: true } : {}),
          },
        });
        const a = approvalOf(r.id);
        if (a) decide(r.id, a);
        if (r.ms !== undefined || r.error) {
          emit({
            type: "content_block_start",
            index: index++,
            content_block: {
              type: "server_tool_result",
              tool_use_id: r.id,
              is_error: !!r.error,
              content: r.error
                ? r.error
                : [
                    { type: "text", text: r.summary },
                    ...r.sources.map((x) => ({
                      type: "search_result",
                      source: x.url ?? "",
                      title: x.title,
                      content: [],
                    })),
                  ],
            },
          });
          status(r.id, r.error ? "error" : "done", r.at, { elapsed_ms: r.ms ?? 0 });
        }
        continue;
      }
      const t = step.tool;
      emit({
        type: "content_block_start",
        t_ms: ms(t.at),
        index: index++,
        content_block: {
          type: "server_tool_use",
          id: t.id,
          name: t.name,
          kind: "tool",
          input: t.input ?? {},
          ...(t.withheld ? { humagen_redacted: true } : {}),
        },
      });
      const a = approvalOf(t.id);
      if (a) decide(t.id, a);
      const q = questionOf(t.id);
      if (q) ask(t.id, q);
      if (t.ms !== undefined || t.error) {
        emit({
          type: "content_block_start",
          index: index++,
          content_block: {
            type: "server_tool_result",
            tool_use_id: t.id,
            is_error: !!t.error,
            content: t.error ?? t.output ?? "",
          },
        });
        status(t.id, t.error ? "error" : "done", t.at, { elapsed_ms: t.ms ?? 0 });
      } else if (ended && !a) status(t.id, "stopped", node.ended_at);
    }
    // A question whose call is not shown is a call of its own.
    for (const q of questions) {
      if (askedAt.has(q.id)) continue;
      const id = q.tool ?? `htu_${node.run}_q_${q.id}`;
      emit({
        type: "content_block_start",
        t_ms: ms(q.at),
        index: index++,
        content_block: {
          type: "server_tool_use",
          id,
          name: "ask_person",
          kind: "tool",
          title: q.prompt,
          input: {},
        },
      });
      ask(id, q);
      if (q.status !== "asked") status(id, q.status === "answered" ? "done" : "stopped", q.at);
      else if (ended) status(id, "stopped", node.ended_at);
    }
    // An approval that gates no call shown is a call of its own.
    for (const a of node.approvals) {
      if (gated.has(a.id)) continue;
      const id = a.tool ?? `htu_${node.run}_q_${a.id}`;
      emit({
        type: "content_block_start",
        t_ms: ms(a.at),
        index: index++,
        content_block: {
          type: "server_tool_use",
          id,
          name: a.action,
          kind: "approval",
          title: a.action,
          input: {},
        },
      });
      decide(id, a);
      if (a.decision === "allow") status(id, "done", a.at);
    }
    // The run's answer is its text, after its work.
    if (node.answer || (!parent && node.status === "failed" && node.error))
      emit({
        type: "content_block_start",
        t_ms: ms(node.ended_at),
        index: index++,
        content_block: {
          type: "text",
          text: node.answer ?? "",
          citations: node.sources.map((c) => ({ title: c.title, url: c.url })),
        },
      });
  };

  scope(root, [], rest, trace.hidden);
  if (ENDED.has(root.status)) {
    out.push({
      type: "message_delta",
      t_ms: ms(root.ended_at),
      delta: {
        stop_reason:
          root.status === "completed"
            ? "end_turn"
            : root.status === "stopped"
              ? "stopped"
              : "failed",
      },
    });
    out.push({
      type: "humagen.usage",
      by_model: models,
      ...(root.cost ? { cost_usd: root.cost / 1_000_000 } : {}),
    });
    if (root.status === "failed" && root.error)
      out.push({ type: "error", error: { message: root.error } });
    out.push({ type: "message_stop", t_ms: ms(root.ended_at) });
  }
  return out;
}
