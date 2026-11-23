import { defaultCopy } from "@humagen/components/copy";
import type {
  ArtifactKind,
  Citation,
  Failure,
  FailureCause,
  FailureKind,
  Interrupt,
  Task,
  Turn,
  TurnEvent,
} from "@humagen/core";
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
  /**
   * Connections the run, or an agent it called, needed and the person it
   * acts for had not made, by provider.
   */
  needs_connection?: { provider: string; name?: string }[];
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
  /** Every question a parked run waits on, when a batch of tool calls parked it on more than one. */
  questions?: WireQuestion[];
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
/**
 * fiber hands a refused or failed connector action back to the model as text,
 * so the agent can react, and the call ends with that text as its result and
 * no error. It is still a call that failed.
 */
const FAILED_RESULT = /^action failed:/i;
/** The tool an agent publishes a file with. */
const PUBLISH = "publish_artifact";
/**
 * The tool an agent calls another agent with, and the names it had on runs
 * recorded before: call_agent, and delegate_task for a call not waited on. A
 * call not waited on is now the agent tool with background set; either way
 * the run it started says so (lifecycle "delegated").
 */
const CALL_AGENTS = new Set(["agent", "call_agent", "delegate_task"]);
/** On runs recorded before, the tool that called one agent was named after it. */
const DELEGATE = /^tag_(.+)$/;

/** The agent a call names in its arguments. */
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
      (CALL_AGENTS.has(name ?? "") ? calledAgent(all.input) : undefined);
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
        // A run parked on several questions resumes once, with every answer.
        const answers: (Parameters<typeof settledOf>[0] & { question_id?: string })[] = e.data?.answers?.length
          ? e.data.answers
          : e.data?.answer
            ? [e.data.answer]
            : [];
        const out: TurnEvent[] = [];
        for (const a of answers) {
          const open = a?.question_id ? asked.get(a.question_id) : undefined;
          if (!a?.question_id || !open) continue;
          asked.delete(a.question_id);
          const settled = settledOf(a);
          const resolved = interruptOf(open.question, settled);
          if (resolved) out.push({ type: "interrupt.resolved", interrupt: resolved });
          const call = open.question.tool?.call_id;
          if (call && open.question.kind === "approval") {
            const denied = settled.action !== "approve";
            out.push({ type: "task.upserted", task: task(call, open.question.tool?.name, { status: denied ? "denied" : "in_progress" }) });
          }
        }
        out.push({ type: "activity", activity: "thinking" });
        return out;
      }
      case "run.parked.v1": {
        // Blocked on a person. humagen does not animate waiting. A batch of
        // tool calls that each wait parks the run once, on every question.
        const questions: WireQuestion[] = e.data?.questions?.length
          ? e.data.questions
          : e.data?.question
            ? [e.data.question]
            : [];
        const out: TurnEvent[] = [];
        for (const q of questions) {
          const interrupt = q?.id ? interruptOf(q, undefined) : null;
          if (!interrupt) continue;
          asked.set(q.id, { question: q, interrupt });
          out.push({ type: "interrupt.raised", interrupt });
          // The call an approval holds reads "waiting", not "running". It is not
          // tied to the approval (`interruptId`): humagen then gives the web inline
          // row controls and Slack the card, and the two surfaces would differ.
          // humagen: one rendering of a task-owned approval on both surfaces.
          const call = q.tool?.call_id;
          if (call && q.kind === "approval") {
            tools.set(call, q.tool?.name ?? "tool");
            out.push({ type: "task.upserted", task: task(call, q.tool?.name, { status: "waiting" }) });
          }
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
          { type: "task.upserted", task: task(t.id, t.name, { ...(t.arguments === undefined ? {} : { input: JSON.stringify(t.arguments, null, 2).slice(0, 32_000) }), ...(toolStarted.has(t.id) ? { startedAt: new Date(toolStarted.get(t.id) as number).toISOString() } : {}) }) },
        ];
        if (t.name && (delegateOf(t.name) || CALL_AGENTS.has(t.name))) {
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
        // A publish that failed before it could report leaves its card open: close it here.
        const unpublished = t.error && publishing.includes(t.id) ? [unpublishedFile(t.id, t.error)] : [];
        if (unpublished.length) publishing.splice(publishing.indexOf(t.id), 1);
        return [
          ...unpublished,
          { type: "activity", activity: "thinking" },
          { type: "task.upserted", task: task(t.id, name, { status: t.error || FAILED_RESULT.test(t.result ?? "") ? "error" : "complete", output: t.error || t.result, ...(t.arguments === undefined ? {} : { input: JSON.stringify(t.arguments, null, 2).slice(0, 32_000) }), ...(duration !== undefined && Number.isFinite(duration) && duration >= 0 ? { durationMs: duration } : {}) }) },
        ];
      }
      case "run.action.taken.v1": {
        const p = e.data ?? { name: "" };
        const outcome = p.data as Run["outcome"] | undefined;
        if (p.name === "record_outcome" && outcome) return [{type:"outcome.updated",outcome:{kind:outcome.kind,summary:outcome.summary,decisions:outcome.decisions,nextAction:outcome.next_action}}];
        const called = p.data as { run?: string; agent?: string; lifecycle?: string; tool_call_id?: string } | undefined;
        if (p.name && called?.run && (delegateOf(p.name) || CALL_AGENTS.has(p.name))) {
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
        out.push(...publishing.splice(0).map((id) => unpublishedFile(id)));
        out.push({ type: "turn.stopped" }, { type: "stop.updated", stop: "confirmed" });
        return out;
      }
      case "run.failed.v1": {
        const l = e.data;
        // A failed attempt may still be retried by the durable engine. Only a
        // terminal failure ends the turn.
        if (!l?.terminal) return [];
        return [
          ...publishing.splice(0).map((id) => unpublishedFile(id)),
          {
            type: "turn.failed",
            failure: failureOf(l.error, e.run ?? ""),
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

/** The card of a publish that did not happen: it failed, or the run ended first. */
function unpublishedFile(id: string, error?: string): TurnEvent {
  return {
    type: "attachment.upserted",
    attachment: { type: "artifact", id, title: "File", kind: "document", phase: "failed", error: lastLine(error) || "The file was not published." },
  };
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
      if (run.status !== "parked") return [];
      // A run parked by a batch of tool calls waits on several questions.
      const waits = run.questions?.length ? run.questions : run.question ? [run.question] : [];
      return waits.flatMap((q) => {
        const i = interruptOf(q, undefined);
        return i ? [i] : [];
      });
    })(),
    notices: [],
    ...(run.needs_connection?.length ? { needsConnection: needsOf(run) } : {}),
    activity: run.status === "parked" ? "waiting" : undefined,
    // A finished run worked from its creation to its last change.
    ...(() => {
      const ms = Date.parse(run.updated_at) - Date.parse(run.created_at);
      return status === "done" && Number.isFinite(ms) && ms >= 0 ? { durationMs: ms } : {};
    })(),
    failure:
      run.status === "failed"
        ? failureOf(run.error, run.id)
        : undefined,
  };
}

/** The connections a run's record says it needed, each named once, by its display name or else its provider. */
export function needsOf(run: Pick<Run, "needs_connection">): NonNullable<Turn["needsConnection"]> {
  const seen = new Set<string>();
  return (run.needs_connection ?? []).flatMap((n) => {
    if (!n.provider || seen.has(n.provider)) return [];
    seen.add(n.provider);
    return [{ provider: n.provider, name: n.name || n.provider }];
  });
}

/**
 * Whether the model provider is what refused: the error has a segment in the
 * runtime's own form for one, "provider anthropic (model m, status 401): …"
 * or "provider anthropic: …" (saige's ProviderError). The agent's handle and
 * the names of tools and servers are not read for it, and a provider segment
 * under a tool's or a server's own is theirs. Anything else is not said to be
 * the model provider's.
 */
function fromModelProvider(error: string): boolean {
  const text = error.replace(/\bagent "[^"]*" version [^:\s]*:/g, "");
  const segment = /(?:^|:\s+|\n\s*)provider [^\s:()]+(?: \(model [^)]*\))?:/i.exec(text);
  if (!segment) return false;
  return !/(?:^|:\s+|\n\s*)(?:tool\b[^:\n]*|mcp|connector):/i.test(text.slice(0, segment.index + 1));
}

/**
 * The cause of a run's failure, read from the runtime's error, for the plain
 * sentence. Specific causes come first: "durable: max attempts exhausted"
 * wraps the error of the last attempt, so it names the cause only when
 * nothing under it does, and an answer the app refused is that whatever the
 * app's reason was. A refused key or a rate limit is the model provider's
 * only when the error names one. The strings are humagen's and saige's own
 * (internal/platform/models.go, agent/types, durable/run, flow/connector,
 * flow/session).
 */
export function failureCause(error: string | undefined): FailureCause {
  const text = error ?? "";
  if (/needs a model provider/i.test(text)) return "no_model_provider";
  if (/undeliverable/i.test(text)) return "undeliverable";
  if (/budget|spend limit|insufficient (?:credit|fund)|(?:status|http)[: ]+402/i.test(text))
    return "budget";
  if (/credential|api[_ -]?key|secret reference|unauthorized|(?:status|http)[: ]+401/i.test(text))
    return fromModelProvider(text) ? "provider_credential" : "credential";
  if (/rate.?limit|too many requests|(?:status|http)[: ]+429/i.test(text))
    return fromModelProvider(text) ? "provider_rate_limited" : "rate_limited";
  if (/timed? ?out|deadline exceeded/i.test(text)) return "timeout";
  if (
    /permission denied|access denied|denied by (?:policy|grant)|egress: denied|forbidden|(?:status|http)[: ]+403/i.test(
      text,
    )
  )
    return "permission_denied";
  if (/unavailable|connection refused|(?:status|http)[: ]+50[234]/i.test(text))
    return "unavailable";
  if (/too many consecutive failed tool turns/i.test(text)) return "failed_steps";
  if (/max(?:imum)? iterations/i.test(text)) return "max_iterations";
  if (/max attempts exhausted/i.test(text)) return "max_attempts";
  return "unknown";
}

/**
 * How a failure is handled: who can fix it, and whether Retry can help. It
 * decides the actions a failure offers, apart from the words it is said in,
 * so a new sentence never takes Retry or Report Issue away. Only a run with
 * no model provider enabled is new here: nothing a person in the session
 * can do helps until an admin enables one.
 */
function handling(error: string | undefined): { kind: FailureKind; retryable?: boolean } {
  const text = error ?? "";
  if (/needs a model provider/i.test(text)) return { kind: "forbidden", retryable: false };
  if (/budget|spend limit|insufficient (?:credit|fund)|(?:status|http)[: ]+402/i.test(text))
    return { kind: "forbidden", retryable: false };
  if (/credential|api[_ -]?key|secret reference|unauthorized|(?:status|http)[: ]+401/i.test(text))
    return { kind: "forbidden", retryable: false };
  if (/rate.?limit|too many requests|(?:status|http)[: ]+429/i.test(text))
    return { kind: "rate_limited" };
  if (/timed? ?out|deadline exceeded/i.test(text)) return { kind: "timeout" };
  if (/permission denied|forbidden|(?:status|http)[: ]+403/i.test(text))
    return { kind: "forbidden", retryable: false };
  if (/unavailable|connection refused|(?:status|http)[: ]+50[234]/i.test(text))
    return { kind: "unavailable" };
  if (/too many consecutive failed tool turns/i.test(text)) return { kind: "tool_error" };
  return { kind: "internal" };
}

/** Text that names where a credential is kept, which stays out of every view of a failure. */
const CREDENTIAL_TEXT = /op:\/\/|1password|credential|api[_ -]?key|secret reference|unauthorized/i;

/** The causes whose error is offered behind Show details: those no plain sentence explains. */
const DETAILED: ReadonlySet<FailureCause> = new Set(["unknown", "max_attempts"]);

/**
 * An error as it may be shown: without the name and password in an address,
 * without an address's query, and without anything that reads as a key.
 */
export function scrubbed(error: string): string {
  return error
    .replace(/\b([a-z][a-z0-9+.-]*:\/\/)[^\s/@]+@/gi, "$1")
    .replace(
      /(\b[a-z][a-z0-9+.-]*:\/\/[^\s?#"'<>]+)\?[^\s"'<>)]*/gi,
      // A colon that ends the address, as in "…/file?sig=1: status 500", is the sentence's.
      (whole: string, address: string) => (whole.endsWith(":") ? `${address}:` : address),
    )
    .replace(/\bBearer\s+[A-Za-z0-9._~+/=-]+/gi, "Bearer [hidden]")
    .replace(
      /\b((?:api[_-]?key|access[_-]?token|token|secret|password|passwd)["']?\s*[=:]\s*)["']?[^\s"',;&]+["']?/gi,
      "$1[hidden]",
    )
    .replace(/\b(?:sk-|xox[a-z]-|gh[pousr]_|github_pat_|AKIA|eyJ)[A-Za-z0-9._-]{8,}/g, "[hidden]")
    .replace(/\b[A-Za-z0-9_-]{40,}\b/g, "[hidden]");
}

/**
 * A run's failure from the runtime's error: its cause, said in plain words
 * (`copy.runFailure`), and how it is handled. The error's last line is kept
 * as `detail`, for Show details, only where no sentence explains it,
 * scrubbed, and never when the error names where a credential is kept.
 */
export function failureOf(error: string | undefined, reference: string): Failure {
  const cause = failureCause(error);
  // The last line is the cause; the lines above it are not shown.
  const detail = lastLine(error);
  return {
    ...handling(error),
    cause,
    message: defaultCopy.runFailure.because[cause],
    ...(detail && DETAILED.has(cause) && !CREDENTIAL_TEXT.test(error ?? "")
      ? { detail: scrubbed(detail) }
      : {}),
    reference,
  };
}
