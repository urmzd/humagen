import type { ArtifactKind, Citation, Interrupt, Task, Turn, TurnEvent } from "@humagen/core";
import type { RunEvent } from "@humagen/client/types";
import { interruptOf, settledOf, type WireQuestion } from "./question.ts";

/**
 * The one place the platform's events meet humagen's event model, for every
 * surface: the console and the Slack app both read runs through this file, so
 * the same run is the same turn in both. Everything above it speaks TurnEvent,
 * and everything below it speaks the API's event union.
 */

/**
 * An event as the platform's event stream and `listRunEvents` send it: an
 * envelope whose `data` is the body for its `type`. The type is generated
 * from api/openapi.yaml, so a change to the contract fails to typecheck here.
 */
export type { RunEvent };

export type ResponseFormat =
  | { type: "text"; max_chars?: number }
  | { type: "summary"; max_words?: number }
  | { type: "json_schema"; schema: Record<string, unknown> | boolean };

export type InputPart =
  | { type: "text"; text: string }
  | { type: "conversation_range"; thread: string; from: string; through: string }
  | { type: "artifact"; id: string; version: number }
  | { type: "run_result"; run: string };

export interface AgentCallInput {
  messages: { role: "user" | "assistant"; content: InputPart[] }[];
  response_format: ResponseFormat;
}

export interface PreparedCallInput {
  input: AgentCallInput;
  messages: { role: "user" | "assistant"; text: string }[];
}


/** Money in integer millionths of a currency, from saige's price catalog. */
export interface Cost {
  micros: number;
  currency: string;
  as_of?: string;
}

/** What some model calls used. `cost` is a lower bound when `unpriced_calls` is set. */
export interface Usage {
  model_calls?: number;
  requests?: number;
  prompt_tokens?: number;
  cached_prompt_tokens?: number;
  cache_write_tokens?: number;
  completion_tokens?: number;
  cost?: Cost;
  other_costs?: Cost[];
  unpriced_calls?: number;
}

/** What a run used: its own calls, the attached runs it rolled up, and how full its context was. */
export interface RunUsage {
  own: Usage;
  children?: Usage;
  total: Usage;
  context?: { model?: string; window?: number; tokens?: number };
}

/** The API's read-confined public citation, never a private retrieval proof. */
export interface RunCitation {
  id: string;
  title: string;
  url?: string;
  snippet?: string;
  mode?: "indexed" | "live";
  copy_at?: string;
  refresh_failed?: boolean;
}

export function citationOf(c: RunCitation): Citation {
  return {
    id: c.id,
    title: c.title,
    ...(c.url ? { url: c.url } : {}),
    ...(c.snippet ? { snippet: c.snippet } : {}),
    ...citationMetadata(c),
  };
}

function citationMetadata(c: {
  mode?: unknown;
  copy_at?: unknown;
  refresh_failed?: unknown;
}): Pick<Citation, "mode" | "copyAt" | "refreshFailed"> {
  if (c.mode !== "indexed" && c.mode !== "live") return {};
  return {
    mode: c.mode,
    ...(typeof c.copy_at === "string" &&
    /^\d{4}-\d{2}-\d{2}T/.test(c.copy_at) &&
    Number.isFinite(Date.parse(c.copy_at))
      ? { copyAt: c.copy_at }
      : {}),
    ...(typeof c.refresh_failed === "boolean" ? { refreshFailed: c.refresh_failed } : {}),
  };
}

/** A run as the platform's index reports it. */
export interface Run {
  /** Explicit empty lists clear prior streamed references after a privacy projection. */
  citations?: RunCitation[];
  id: string;
  tenant: string;
  agent: string;
  rev: number;
  origin: string;
  text: string;
  input?: PreparedCallInput;
  /** Recorded human involvement; absent when its complete history is unknown. */
  assistance?: "none" | "needed";
  /**
   * Who raised the run. `user` is the person it acts for, by subject: the
   * one whose own credentials its tools use. Absent, or on a bot, it acts for
   * nobody.
   */
  actor?: { id: string; display?: string; bot?: boolean; user?: string };
  root: string;
  parent?: string;
  depth: number;
  mode?: "subagent" | "handoff" | "delegate";
  /** The schedule that raised the run, if one did. */
  schedule?: string;
  /** The conversation the run belongs to. */
  thread: string;
  /** The turn this one follows. Runs sharing a prev are branches. */
  prev?: string;
  /** Why the agent was chosen, when the platform picked it. */
  picked?: string;
  /** How its thread's selected agents started it, when they did: one that decided to join, or each of them. Kept whatever the selection becomes. `best_fit` is how a run written before `join` reads. */
  routed?: "join" | "all" | "best_fit";
  project?: string;
  /** The project's channel the conversation is in. Absent is `general`. */
  channel?: string;
  /** The person's message in a shared thread the run answers. Runs answering one message are side by side, not branches. */
  message?: string;
  /**
   * The project's ancestors, nearest first and the workspace left out, pinned
   * when the run was admitted: what it counts toward besides its project.
   */
  within?: string[];
  lease: number;
  /** Micro-USD leased to calls that have not ended. */
  granted: number;
  /** Micro-USD priced in US dollars: the run's own calls and the calls it made that have ended. */
  spent?: number;
  /** What the run used, as the runtime counts it. Absent until its first model call. */
  usage?: RunUsage;
  calls: number;
  /** Stopped is ended from outside: by a person, or because the run it was attached to ended. */
  status: "accepted" | "running" | "parked" | "completed" | "failed" | "stopped";
  /** Set on a run another run called. Attached runs end with their caller, delegated ones do not. */
  lifecycle?: "attached" | "delegated";
  /** Why a stopped run was stopped. */
  cause?: string;
  /** What a parked run is waiting on a person for. */
  question?: WireQuestion;
  /** The trigger id the run was accepted with. Its `id` is derived from it and the workspace. */
  tag_id?: string;
  /** Asked to stop and not ended yet. It ends stopped, or completed or failed if it finished first. */
  stopping?: boolean;
  /** When the run was first asked to stop, while it is stopping. */
  stop_requested_at?: string;
  /** The run has its answer and is waiting for runs attached to it. */
  finishing?: boolean;
  answer?: string;
  outcome?: {kind: "recommendation" | "decision" | "update"; summary: string; decisions?: string[]; next_action?: string};
  error?: string;
  reason?: string;
  created_at: string;
  updated_at: string;
}

/** What the platform reports in an action's `data` when an agent publishes a file. */
interface Published {
  data?: {
    id: string;
    title: string;
    kind: ArtifactKind;
    mime: string;
    size: number;
    version: number;
    description?: string;
    preview?: string;
  };
}
/** The tool an agent publishes a file with. */
const PUBLISH = "publish_artifact";
/** The tool an agent hands work to another agent with, without waiting for it. */
const HAND_ON = "delegate_task";
const CALL_AGENT = "call_agent";
/** fiber names the tool that hands work to another agent after that agent. */
const DELEGATE = /^tag_(.+)$/;

/** The agent a call_agent or delegate_task call names in its arguments. */
function calledAgent(input?: string): string | undefined {
  try {
    const args = JSON.parse(input ?? "") as { agent?: unknown };
    return typeof args.agent === "string" && args.agent ? args.agent : undefined;
  } catch {
    return undefined;
  }
}

/** The agent a tool call hands work to, when the tool is a call to another agent. */
export function delegateOf(tool?: string): string | undefined {
  return DELEGATE.exec(tool ?? "")?.[1];
}

/**
 * Returns a function that turns one run's envelopes into TurnEvents. It is
 * stateful because two facts span events: whether text already streamed (the
 * completed event repeats the whole answer), and each tool call's name.
 */
export function adapter(
  opts: {
    /** An agent's colour, 0 to 360, so a call to it reads as that agent. */
    hue?: (agent: string) => number;
    /**
     * Where a platform path such as `/api/artifacts/a1/content` is reached from
     * this surface. The console is served by the platform, so it leaves paths
     * as they are. A surface elsewhere returns an absolute address, or
     * undefined when it has none to offer, and the link is left out.
     */
    href?: (path: string) => string | undefined;
  } = {},
): (e: RunEvent) => TurnEvent[] {
  const href = opts.href ?? ((path: string) => path);
  let streamed = false;
  // When the run was accepted, so its end can say how long it worked.
  let acceptedAt: number | undefined;
  const tools = new Map<string, string>();
  // Publish calls that have not reported yet, oldest first. A file is shown
  // from the moment the agent starts publishing it, and the report that
  // follows fills in the same card rather than adding a second one.
  const publishing: string[] = [];

  // Helper calls link to their own run. Their complete result stays in
  // execution history rather than becoming another answer preview.
  // The run each call to another agent started, by tool call. The platform
  // says which run it was on the caller's stream, after the call opens.
  const started = new Map<string, { run: string; agent: string; lifecycle?: string }>();
  // Calls to another agent that have not been told their run yet, per tool.
  const calling = new Map<string, string[]>();
  // What each task last looked like, so a later fact adds to it.
  const shown = new Map<string, Partial<Task>>();
  // Questions this run asked that have no answer yet, and the tool call an
  // approval holds.
  const asked = new Map<string, { question: WireQuestion; interrupt: Interrupt }>();

  const toolStarted = new Map<string, number>();
  const task = (id: string, name: string | undefined, rest: Partial<Task>): Task => {
    const all = { ...shown.get(id), ...rest };
    shown.set(id, all);
    const run = started.get(id);
    // A call that never started a run (refused, or failed on the way) is
    // still a call to that agent: its arguments name it.
    const to =
      delegateOf(name) ??
      run?.agent ??
      (name === CALL_AGENT || name === HAND_ON ? calledAgent(all.input) : undefined);
    return to
      ? {
          id,
          title: to,
          status: "in_progress",
          tool: name,
          ...all,
          subagent: {
            name: to,
            runId: run?.run,
            summary: all.status === "error" ? "Helper failed. Open Activity for details."
              : run?.lifecycle === "delegated" ? "Background work. Open Activity."
              : all.status === "complete" ? "Result returned to caller" : undefined,
            hue: opts.hue?.(to),
          },
        }
      : { id, title: title(name), status: "in_progress", tool: name, ...all };
  };

  return (e) => {
    switch (e.type) {
      case "run.accepted.v1":
        streamed = false;
        tools.clear();
        toolStarted.clear();
        started.clear();
        calling.clear();
        shown.clear();
        asked.clear();
        acceptedAt = Date.parse(e.time);
        return [{ type: "turn.started", turnId: e.run ?? "", agent: { name: e.agent ?? "" } }];
      case "run.started.v1":
        return [{ type: "activity", activity: "thinking" }];
      case "run.resumed.v1": {
        const a = e.data?.answer;
        const open = a?.question_id ? asked.get(a.question_id) : undefined;
        if (!a || !open) return [{ type: "activity", activity: "thinking" }];
        asked.delete(a.question_id);
        const settled = settledOf(a);
        const out: TurnEvent[] = [];
        const resolved = interruptOf(open.question, settled);
        if (resolved) out.push({ type: "interrupt.resolved", interrupt: resolved });
        const call = open.question.tool?.call_id;
        if (call && open.question.kind === "approval") {
          const denied = settled.action !== "approve";
          out.push({ type: "task.upserted", task: task(call, open.question.tool?.name, { status: denied ? "denied" : "in_progress" }) });
        }
        out.push({ type: "activity", activity: "thinking" });
        return out;
      }
      case "run.parked.v1": {
        // Blocked on a person. humagen does not animate waiting.
        const q: WireQuestion | undefined = e.data?.question;
        const interrupt = q?.id ? interruptOf(q, undefined) : null;
        if (!q || !interrupt) return [{ type: "activity", activity: "waiting" }];
        asked.set(q.id, { question: q, interrupt });
        const out: TurnEvent[] = [{ type: "interrupt.raised", interrupt }];
        // The call an approval holds reads "waiting", not "running". It is not
        // tied to the approval (`interruptId`): humagen then gives the web inline
        // row controls and Slack the card, and the two surfaces would differ.
        // humagen: one rendering of a task-owned approval on both surfaces.
        const call = q.tool?.call_id;
        if (call && q.kind === "approval") {
          tools.set(call, q.tool?.name ?? "tool");
          out.push({ type: "task.upserted", task: task(call, q.tool?.name, { status: "waiting" }) });
        }
        out.push({ type: "activity", activity: "waiting" });
        return out;
      }
      case "run.child.parked.v1": {
        // A run this one waits on is waiting on a person. The question is
        // that run's, answered there or in the inbox: here the step that
        // called it reads waiting.
        const child = e.data?.run_id;
        const id = [...started].find(([, s]) => s.run === child)?.[0];
        const out: TurnEvent[] = [];
        if (id) out.push({ type: "task.upserted", task: task(id, tools.get(id), { status: "waiting" }) });
        out.push({ type: "activity", activity: "waiting" });
        return out;
      }
      case "run.delta.text.v1": {
        const text = e.data?.text ?? "";
        if (!text) return [];
        // The first words of an answer are the moment it starts composing.
        const first = !streamed;
        streamed = true;
        return first
          ? [
              { type: "activity", activity: "composing" },
              { type: "text.delta", text },
            ]
          : [{ type: "text.delta", text }];
      }
      case "run.delta.thinking.v1": {
        const text = e.data?.text ?? "";
        return text ? [{ type: "reasoning.delta", text }] : [];
      }
      case "run.delta.tool.call.v1": {
        const t = e.data;
        if (!t?.id) return [];
        tools.set(t.id, t.name ?? "tool");
        if (t.phase === "executing" && Number.isFinite(Date.parse(e.time))) toolStarted.set(t.id, Date.parse(e.time));
        const out: TurnEvent[] = [
          { type: "activity", activity: doing(t.name) },
          { type: "task.upserted", task: task(t.id, t.name, t.arguments === undefined ? {} : { input: JSON.stringify(t.arguments, null, 2).slice(0, 32_000) }) },
        ];
        if (t.name && (delegateOf(t.name) || t.name === HAND_ON || t.name === CALL_AGENT)) {
          const pending = calling.get(t.name) ?? [];
          if (!pending.includes(t.id) && !started.has(t.id)) calling.set(t.name, [...pending, t.id]);
        }
        if (t.name === PUBLISH) {
          publishing.push(t.id);
          out.push({
            type: "attachment.upserted",
            attachment: { type: "artifact", id: t.id, title: "Publishing a file", kind: "document", phase: "generating" },
          });
        }
        return out;
      }
      case "run.delta.tool.done.v1": {
        const t = e.data;
        if (!t?.id) return [];
        const name = t.name ?? tools.get(t.id);
        const start = toolStarted.get(t.id);
        const duration = start !== undefined ? Date.parse(e.time) - start : undefined;
        return [
          { type: "activity", activity: "thinking" },
          { type: "task.upserted", task: task(t.id, name, { status: t.error ? "error" : "complete", output: t.error || t.result, ...(t.arguments === undefined ? {} : { input: JSON.stringify(t.arguments, null, 2).slice(0, 32_000) }), ...(duration !== undefined && Number.isFinite(duration) && duration >= 0 ? { durationMs: duration } : {}) }) },
        ];
      }
      case "run.action.taken.v1": {
        const p = e.data ?? { name: "" };
        const outcome = p.data as Run["outcome"] | undefined;
        if (p.name === "record_outcome" && outcome) return [{type:"outcome.updated",outcome:{kind:outcome.kind,summary:outcome.summary,decisions:outcome.decisions,nextAction:outcome.next_action}}];
        const called = p.data as { run?: string; agent?: string; lifecycle?: string; tool_call_id?: string } | undefined;
        if (p.name && called?.run && (delegateOf(p.name) || p.name === HAND_ON || p.name === CALL_AGENT)) {
          // Which run a call to another agent started. From here the step
          // can be opened as that run.
          const pending = calling.get(p.name) ?? [];
          const id = called.tool_call_id ?? pending[0];
          if (!id || tools.get(id) !== p.name) return [];
          calling.set(p.name, pending.filter((candidate) => candidate !== id));
          started.set(id, { run: called.run, agent: called.agent ?? delegateOf(p.name) ?? "", lifecycle: called.lifecycle });
          return [{ type: "task.upserted", task: task(id, p.name, {}) }];
        }
        if (p.name !== PUBLISH) return [];
        // The card the call opened, or a new one if this is all that is left
        // of the run's record.
        const a = (p as Published).data;
        const id = publishing.shift() ?? a?.id ?? `published-${e.seq}`;
        if (p.error || !a) {
          return [
            {
              type: "attachment.upserted",
              attachment: {
                type: "artifact",
                id,
                title: p.summary || "File",
                kind: "document",
                phase: "failed",
                error: lastLine(p.error) || "The file could not be published.",
              },
            },
          ];
        }
        const content = `/api/artifacts/${encodeURIComponent(a.id)}/content`;
        return [
          {
            type: "attachment.upserted",
            attachment: {
              type: "artifact",
              id,
              title: a.title,
              artifactId: a.id,
              kind: a.kind,
              phase: "ready",
              mimeType: a.mime,
              sizeBytes: a.size,
              version: a.version,
              // Inline is honored only for types that cannot run script. For
              // everything else this link downloads, like the one below it.
              url: href(`${content}?inline=1`),
              downloadUrl: href(content),
              description: a.description,
              previewText: a.preview,
            },
          },
        ];
      }
      case "run.delta.citation.v1": {
        const c = e.data;
        if (!c) return [];
        return [
          {
            type: "citation.added",
            citation: {
              id: c.label ?? String(c.index ?? e.seq),
              title: c.source?.document_title ?? c.source?.section_heading ?? "Source",
              url: c.source?.source_uri,
              snippet: c.text,
              ...citationMetadata(c),
            },
          },
        ];
      }
      case "run.completed.v1": {
        const out: TurnEvent[] = [];
        const text = e.data?.text;
        if (!streamed && text) out.push({ type: "text.delta", text });
        const ms = acceptedAt === undefined ? Number.NaN : Date.parse(e.time) - acceptedAt;
        out.push(Number.isFinite(ms) && ms >= 0 ? { type: "turn.completed", durationMs: ms } : { type: "turn.completed" });
        return out;
      }
      case "run.stopped.v1": {
        // The run was stopped and the platform says so on its stream. Until it
        // does, a stopped run is ended from its record (see `settled`). The
        // body carries what the agent had said, for a reader that did not
        // see it stream.
        const out: TurnEvent[] = [];
        const text = e.data?.text;
        if (!streamed && text) out.push({ type: "text.delta", text });
        // A question nobody answered is settled with the run.
        for (const { question } of asked.values()) {
          const settled = interruptOf(question, { action: "stopped", by: "system", at: e.time, note: "The run was stopped" });
          if (settled) out.push({ type: "interrupt.resolved", interrupt: settled });
        }
        asked.clear();
        out.push({ type: "turn.stopped" }, { type: "stop.updated", stop: "confirmed" });
        return out;
      }
      case "run.failed.v1": {
        const l = e.data;
        // A failed attempt may still be retried by the durable engine. Only a
        // terminal failure ends the turn.
        if (!l?.terminal) return [];
        return [
          {
            type: "turn.failed",
            failure: { kind: "internal", message: lastLine(l.error) || "The run failed.", reference: e.run ?? "" },
          },
        ];
      }
      default:
        // Usage, compaction and any type this build does not know
        // (UnknownEvent) say nothing to a turn.
        return [];
    }
  };
}

/** What a tool call looks like from outside: looking something up, handing work on, or acting. */
function doing(tool?: string): "searching" | "delegating" | "calling" {
  const name = (tool ?? "").toLowerCase();
  if (/^(delegate|call|agent|tag)[_:.]|subagent|handoff/.test(name)) return "delegating";
  if (/search|recall|retriev|lookup|find|read|fetch|get/.test(name)) return "searching";
  return "calling";
}

/** Runtime errors nest one per line, and the cause is the last. */
function lastLine(error?: string): string {
  const lines = (error ?? "").split("\n").filter((l) => l.trim());
  return lines[lines.length - 1]?.trim() ?? "";
}

/** A tool name as a person would read it: "github.comment" becomes "Github comment". */
function title(name?: string): string {
  if (!name) return "Tool call";
  const words = name.replace(/[._-]+/g, " ").trim();
  return words.charAt(0).toUpperCase() + words.slice(1);
}

/**
 * The person a run acted for, by subject, or "" for nobody: what decides
 * whose credentials its tools used, as humagen reads the run's actor. A bot acts
 * for nobody, whatever it names.
 */
export function actedFor(run: Pick<Run, "actor">): string {
  return run.actor && !run.actor.bot ? (run.actor.user ?? "") : "";
}

/**
 * The turn to show before any event arrives, built from the run record. Old
 * runs fall out of the bus's retention, so their record is all there is.
 */
export function turnOf(run: Run): Turn {
  const status =
    run.status === "completed"
      ? "done"
      : run.status === "failed"
        ? "failed"
        : run.status === "stopped"
          ? "stopped"
        : run.status === "parked"
          ? "waiting"
          : run.status === "running"
            ? "working"
            : "queued";
  return {
    id: run.id,
    agent: { name: run.agent },
    status,
    text: run.answer ?? "",
    tasks: [],
    citations: (run.citations ?? []).map(citationOf),
    ...(run.outcome ? {outcome:{kind:run.outcome.kind,summary:run.outcome.summary,decisions:run.outcome.decisions,nextAction:run.outcome.next_action}} : {}),
    // The record says what a parked run waits on, for a reader who missed
    // the event that raised it.
    interrupts: (() => {
      const i = run.status === "parked" && run.question ? interruptOf(run.question, undefined) : null;
      return i ? [i] : [];
    })(),
    notices: [],
    activity: run.status === "parked" ? "waiting" : undefined,
    // A finished run worked from its creation to its last change.
    ...(() => {
      const ms = Date.parse(run.updated_at) - Date.parse(run.created_at);
      return status === "done" && Number.isFinite(ms) && ms >= 0 ? { durationMs: ms } : {};
    })(),
    failure:
      run.status === "failed"
        ? { kind: "internal", message: lastLine(run.error) || "The run failed.", reference: run.id }
        : undefined,
  };
}
