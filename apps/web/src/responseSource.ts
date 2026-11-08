/**
 * Where a response's stream comes from. The console folds every response the
 * same way (`@humagen/core`'s `foldResponse`); only the source differs:
 *
 * - `liveSource`: `GET /api/messages/{id}/stream?depth=all`, server-sent events,
 *   live while the response runs and replayed after.
 * - `replaySource`: a recorded stream, such as a fixture.
 * - `runSource`: until the message stream ships, today's run, its call tree and
 *   its events, sent as the stream would send them. It reads at most
 *   `RUN_SOURCE_CAP` runs' events; it goes when every response has a stream.
 */
import type { ResponseEvent } from "@humagen/core";
import type { Run, RunEvent } from "./adapter.ts";
import { api } from "./api.ts";

export type ResponseSource = {
  /** A stable key: the same key is the same stream. */
  key: string;
  /** Events arrive as they happen, so changes are worth announcing. */
  live?: boolean;
  /** Start sending events; the returned function stops. `onEnd` follows the last event. */
  open: (onEvent: (event: ResponseEvent) => void, onEnd: () => void) => () => void;
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

/** The most runs whose events `runSource` reads for one response. */
export const RUN_SOURCE_CAP = 24;

export type RunLoader = {
  tree: (id: string) => Promise<Run[]>;
  events: (id: string) => Promise<{ events: RunEvent[] }>;
};

const runLoader: RunLoader = { tree: api.tree, events: api.runEvents };

const ENDED = new Set(["completed", "failed", "stopped"]);

/** Today's run and the runs it called, as the response stream sends them. */
export function runSource(run: Run, loader: RunLoader = runLoader): ResponseSource {
  return {
    key: `run:${run.id}:${run.status}`,
    open: (onEvent, onEnd) => {
      let live = true;
      eventsOfRun(run, loader)
        .then((events) => {
          if (!live) return;
          for (const e of events) onEvent(e);
        })
        .catch(() => {})
        .finally(() => live && onEnd());
      return () => {
        live = false;
      };
    },
  };
}

// An ended run's stream does not change, so it is read once per page load.
const read = new Map<string, Promise<ResponseEvent[]>>();

export function eventsOfRun(run: Run, loader: RunLoader = runLoader): Promise<ResponseEvent[]> {
  const key = `${run.id}:${run.status}`;
  const cached = loader === runLoader && ENDED.has(run.status) ? read.get(key) : undefined;
  if (cached) return cached;
  const p = (async () => {
    const all = await loader.tree(run.id).catch(() => [run]);
    const tree = all.some((r) => r.id === run.id) ? all : [run, ...all];
    const ids = under(run.id, tree).slice(0, RUN_SOURCE_CAP);
    const got = await Promise.all(
      ids.map((id) =>
        loader
          .events(id)
          .then((r) => [id, r.events] as const)
          .catch(() => [id, [] as RunEvent[]] as const),
      ),
    );
    return runEvents(run, tree, Object.fromEntries(got));
  })();
  if (loader === runLoader && ENDED.has(run.status)) read.set(key, p);
  return p;
}

/** The run and every run under it, parents first. */
function under(root: string, runs: Run[]): string[] {
  const out = [root];
  for (let i = 0; i < out.length; i++)
    for (const r of runs) if (r.parent === out[i] && !out.includes(r.id)) out.push(r.id);
  return out;
}

const statusOf = (r: Run) =>
  r.status === "completed"
    ? "done"
    : r.status === "failed"
      ? "error"
      : r.status === "stopped"
        ? "stopped"
        : r.status === "parked"
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

/**
 * A run and the runs under it as stream events: the run's tool calls and the
 * agents it called are tool calls in its scope, in the order they started;
 * each called run's own work is nested under its call; the run's answer is its
 * text, last. Every tool id is `htu_<run>_<n>`, as the platform mints them.
 */
export function runEvents(
  root: Run,
  runs: Run[],
  events: Record<string, readonly RunEvent[]>,
): ResponseEvent[] {
  const t0 = Date.parse(root.created_at);
  const ms = (iso?: string) => (iso ? Math.max(0, Date.parse(iso) - t0) : undefined);
  const out: ResponseEvent[] = [
    {
      type: "message_start",
      t_ms: 0,
      message: {
        id: root.id,
        role: "assistant",
        content: [],
        humagen: { root_run: root.id, agent: root.agent },
      },
    },
  ];

  const scope = (run: Run, parent: string | undefined, depth: number) => {
    const emit = (e: ResponseEvent) =>
      out.push(
        parent
          ? {
              type: "humagen.nested",
              t_ms: e.t_ms,
              parent_tool_use_id: parent,
              agent_run_id: run.id,
              depth,
              event: e,
            }
          : e,
      );
    const status = (id: string, s: string, at?: string, extra: Record<string, unknown> = {}) =>
      out.push({
        type: "humagen.tool_status",
        t_ms: ms(at),
        tool_use_id: id,
        path: [id],
        status: s,
        ...extra,
      });
    let index = 0;
    let n = 0;
    if (run.picked)
      emit({
        type: "content_block_start",
        index: index++,
        content_block: { type: "thinking", thinking: run.picked },
      });

    // What happened in this run, in time order: its tool calls and the runs it called.
    type Step =
      | { at: string; kind: "call"; id: string; name: string; input?: unknown; done?: RunEvent }
      | { at: string; kind: "agent"; child: Run };
    const steps: Step[] = [];
    const calls = new Map<string, Step & { kind: "call" }>();
    const approvals: { id: string; tool?: string; at: string; prompt: string }[] = [];
    const answers = new Map<string, { decision?: string; by?: string; at: string }>();
    for (const e of events[run.id] ?? []) {
      if (e.type === "run.delta.tool.call.v1") {
        if (!calls.has(e.data.id)) {
          const step = {
            at: e.time,
            kind: "call" as const,
            id: e.data.id,
            name: e.data.name,
            input: e.data.arguments,
          };
          calls.set(e.data.id, step);
          steps.push(step);
        }
      } else if (e.type === "run.delta.tool.done.v1") {
        const step = calls.get(e.data.id);
        if (step) step.done = e;
        else {
          const s = {
            at: e.time,
            kind: "call" as const,
            id: e.data.id,
            name: e.data.name ?? "",
            done: e as RunEvent,
          };
          calls.set(e.data.id, s);
          steps.push(s);
        }
      } else if (e.type === "run.parked.v1" && e.data.question?.kind === "approval") {
        const q = e.data.question;
        approvals.push({
          id: q.id,
          tool: q.tool?.call_id ?? q.tool?.name,
          at: q.at ?? e.time,
          prompt: q.prompt,
        });
      } else if (e.type === "run.resumed.v1" && e.data.answer) {
        const a = e.data.answer;
        answers.set(a.question_id, {
          decision: a.decision,
          by: a.by_name || a.by,
          at: a.at ?? e.time,
        });
      }
    }
    for (const child of runs)
      if (child.parent === run.id) steps.push({ at: child.created_at, kind: "agent", child });
    steps.sort((a, b) => a.at.localeCompare(b.at));

    for (const step of steps) {
      const id = `htu_${run.id}_${++n}`;
      if (step.kind === "agent") {
        const c = step.child;
        emit({
          type: "content_block_start",
          t_ms: ms(c.created_at),
          index: index++,
          content_block: {
            type: "server_tool_use",
            id,
            name: "agent",
            kind: "agent",
            title: titleOf(c.text),
            input: { agent: c.agent, title: titleOf(c.text), brief: c.text },
          },
        });
        scope(c, id, depth + 1);
        if (ENDED.has(c.status)) {
          emit({
            type: "content_block_start",
            t_ms: ms(c.updated_at),
            index: index++,
            content_block: {
              type: "server_tool_result",
              tool_use_id: id,
              is_error: c.status !== "completed",
              content: [
                { type: "text", text: c.answer ?? c.error ?? "" },
                ...(c.citations ?? []).map((x) => ({
                  type: "search_result",
                  source: x.url ?? "",
                  title: x.title,
                  content: [],
                })),
              ],
            },
          });
        }
        status(id, statusOf(c), c.updated_at, {
          elapsed_ms: Math.max(0, Date.parse(c.updated_at) - Date.parse(c.created_at)),
          title: titleOf(c.text),
        });
        continue;
      }
      const kind = step.name === "get_context" ? "context" : "tool";
      emit({
        type: "content_block_start",
        t_ms: ms(step.at),
        index: index++,
        content_block: {
          type: "server_tool_use",
          id,
          name: step.name,
          kind,
          input: step.input ?? {},
        },
      });
      const approval = approvals.find((a) => a.tool === step.id || a.tool === step.name);
      if (approval) {
        const answered = answers.get(approval.id);
        status(
          id,
          answered ? (answered.decision === "deny" ? "denied" : "running") : "awaiting_approval",
          answered?.at ?? approval.at,
          {
            question: answered
              ? undefined
              : { kind: "approval", prompt: approval.prompt, choices: ["Approve", "Deny"] },
            ...(answered?.by ? { decided_by: answered.by, decision: answered.decision } : {}),
          },
        );
      }
      const done = step.done?.type === "run.delta.tool.done.v1" ? step.done.data : undefined;
      if (done) {
        emit({
          type: "content_block_start",
          t_ms: ms(step.done?.time),
          index: index++,
          content_block: {
            type: "server_tool_result",
            tool_use_id: id,
            is_error: !!done.error,
            content: done.error ?? done.result ?? "",
          },
        });
        status(id, done.error ? "error" : "done", step.done?.time, {
          elapsed_ms: Math.max(0, Date.parse(step.done?.time ?? step.at) - Date.parse(step.at)),
        });
      }
    }
    // The run's answer is its text, after its work.
    if (run.answer || (parent === undefined && run.status === "failed" && run.error)) {
      emit({
        type: "content_block_start",
        t_ms: ms(run.updated_at),
        index: index++,
        content_block: {
          type: "text",
          text: run.answer ?? "",
          citations: (run.citations ?? []).map((c) => ({ title: c.title, url: c.url })),
        },
      });
    }
  };

  scope(root, undefined, 0);
  if (ENDED.has(root.status)) {
    out.push({
      type: "message_delta",
      t_ms: ms(root.updated_at),
      delta: {
        stop_reason:
          root.status === "completed"
            ? "end_turn"
            : root.status === "stopped"
              ? "stopped"
              : "failed",
      },
    });
    const cost = root.usage?.total?.cost;
    const models: Record<string, object> = {};
    for (const list of Object.values(events))
      for (const e of list)
        if (e.type === "run.usage.v1" && e.data.model) models[e.data.model] = {};
    out.push({
      type: "humagen.usage",
      by_model: models,
      ...(cost && (cost.currency || "USD") === "USD"
        ? { cost_usd: cost.micros / 1_000_000 }
        : root.spent
          ? { cost_usd: root.spent / 1_000_000 }
          : {}),
    });
    if (root.status === "failed" && root.error)
      out.push({ type: "error", error: { message: root.error } });
    out.push({ type: "message_stop", t_ms: ms(root.updated_at) });
  }
  return out;
}
