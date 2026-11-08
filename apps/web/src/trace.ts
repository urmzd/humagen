/**
 * A message's trace: what ran to answer it, nested under the run that
 * answered. Built from what the platform records today (a run's tree, its
 * spans and its events) in the shape `GET /api/sessions/{id}/trace` is
 * agreed to return, so the page reads one shape whichever source fills it.
 * No React.
 */
import type { Run, RunEvent } from "./adapter.ts";
import type { Span } from "./api.ts";

/** One tool call: what it was asked and what it said, where the reader may see them. */
export type TraceTool = {
  id: string;
  name: string;
  at: string;
  ms?: number;
  input?: Record<string, unknown>;
  output?: string;
  error?: string;
  /** The platform kept its input or output from the reader. */
  withheld: boolean;
};

/**
 * A `get_context` call: what the agent asked for in words, the origin it
 * pulled from (bound by the platform to the message's own place), and what
 * came back.
 */
export type TraceContext = {
  id: string;
  request: string;
  /** The origin it read, such as `slack://T/C/ts`. Empty until the platform says. */
  reference: string;
  /** The first lines of what came back. */
  summary: string;
  sources: { title: string; url?: string }[];
  truncated: boolean;
  error?: string;
  withheld: boolean;
};

export type TraceSource = { title: string; url?: string };

/** A gated action and how it was decided. `ask` is one still waiting on someone. */
export type TraceApproval = {
  id: string;
  action: string;
  decision: "allow" | "deny" | "ask";
  by?: string;
  note?: string;
  at?: string;
};

export type TraceNode = {
  run: string;
  agent: string;
  status: string;
  started_at: string;
  ended_at?: string;
  ms: number;
  /** Micro-USD, own spend plus what it rolled up. Absent when nothing was metered. */
  cost?: number;
  /** Why the platform chose this agent, when the caller left it to the platform. */
  pick?: string;
  tools: TraceTool[];
  context: TraceContext[];
  sources: TraceSource[];
  approvals: TraceApproval[];
  children: TraceNode[];
};

/** The most runs one trace reads events for. A deeper tree still shows its runs, without their tools. */
export const TRACE_RUN_CAP = 24;
/** The most characters of a tool's output kept for the page. */
export const OUTPUT_CAP = 4000;

const ENDED = new Set(["completed", "failed", "stopped"]);
const isContext = (name: string) => name === "get_context";

function msOf(run: Run): number {
  const end = ENDED.has(run.status) ? Date.parse(run.updated_at) : Date.now();
  return Math.max(end - Date.parse(run.created_at), 0);
}

function costOf(run: Run): number | undefined {
  const total = run.usage?.total?.cost;
  if (total && (total.currency || "USD") === "USD") return total.micros;
  return run.spent ? run.spent : undefined;
}

function clip(text: string, cap = OUTPUT_CAP): string {
  return text.length > cap ? `${text.slice(0, cap)}…` : text;
}

/** The first two non-empty lines of some text, at most 240 characters. */
export function firstLines(text: string): string {
  const lines = text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .slice(0, 2)
    .join(" ");
  return lines.length > 240 ? `${lines.slice(0, 240)}…` : lines;
}

type ContextResult = {
  text?: string;
  sources?: { title?: string; url?: string }[];
  reference?: string;
  truncated?: boolean;
};

function parseContext(result: string | undefined): ContextResult {
  if (!result) return {};
  try {
    const v = JSON.parse(result) as unknown;
    return v && typeof v === "object" ? (v as ContextResult) : { text: result };
  } catch {
    return { text: result };
  }
}

type Calls = Map<string, { call?: RunEvent; done?: RunEvent }>;

/** One run's tool calls, `get_context` calls, sources and approvals, from its events. */
export function readEvents(events: RunEvent[]): {
  tools: TraceTool[];
  context: TraceContext[];
  sources: TraceSource[];
  approvals: TraceApproval[];
} {
  const calls: Calls = new Map();
  const sources: TraceSource[] = [];
  const approvals = new Map<string, TraceApproval>();
  for (const e of events) {
    switch (e.type) {
      case "run.delta.tool.call.v1": {
        const c = calls.get(e.data.id) ?? {};
        // The executing phase repeats the requested one; the first is when it was asked.
        if (!c.call) c.call = e;
        calls.set(e.data.id, c);
        break;
      }
      case "run.delta.tool.done.v1": {
        const c = calls.get(e.data.id) ?? {};
        c.done = e;
        calls.set(e.data.id, c);
        break;
      }
      case "run.delta.citation.v1": {
        const s = e.data.source;
        const title = s.document_title || e.data.label || s.source_uri || "";
        if (title && !sources.some((x) => x.title === title))
          sources.push({ title, url: s.source_uri || undefined });
        break;
      }
      case "run.parked.v1": {
        const q = e.data.question;
        if (q?.kind === "approval")
          approvals.set(q.id, {
            id: q.id,
            action: q.tool?.name ?? q.prompt,
            decision: "ask",
            at: q.at ?? e.time,
          });
        break;
      }
      case "run.resumed.v1": {
        const a = e.data.answer;
        const asked = a && approvals.get(a.question_id);
        if (a && asked && a.decision)
          approvals.set(a.question_id, {
            ...asked,
            decision: a.decision === "approve" ? "allow" : "deny",
            by: a.by_name || a.by,
            note: a.note,
            at: a.at ?? e.time,
          });
        break;
      }
    }
  }
  const tools: TraceTool[] = [];
  const context: TraceContext[] = [];
  for (const [id, { call, done }] of calls) {
    const c = call?.type === "run.delta.tool.call.v1" ? call.data : undefined;
    const d = done?.type === "run.delta.tool.done.v1" ? done.data : undefined;
    const name = c?.name ?? d?.name ?? "";
    const input = c?.arguments ?? d?.arguments;
    // Bytes without the arguments themselves: the platform sent their size and kept them back.
    const withheld = !input && (c?.arguments_bytes ?? d?.arguments_bytes ?? 0) > 0;
    if (isContext(name)) {
      const out = parseContext(d?.result);
      context.push({
        id,
        request: typeof input?.request === "string" ? input.request : "",
        reference: out.reference ?? "",
        summary: firstLines(out.text ?? ""),
        sources: (out.sources ?? [])
          .filter((s) => s.title || s.url)
          .map((s) => ({ title: s.title || s.url || "", url: s.url })),
        truncated: !!out.truncated,
        error: d?.error,
        withheld: withheld || (!!d && d.result === undefined && !d.error),
      });
      continue;
    }
    const start = call?.time ?? done?.time ?? "";
    const end = done?.time;
    tools.push({
      id,
      name,
      at: start,
      ms: start && end ? Math.max(Date.parse(end) - Date.parse(start), 0) : undefined,
      input,
      output: d?.result !== undefined ? clip(d.result) : undefined,
      error: d?.error,
      withheld,
    });
  }
  tools.sort((a, b) => a.at.localeCompare(b.at));
  return { tools, context, sources, approvals: [...approvals.values()] };
}

/** Tool spans for a run whose events were not read: the call tree still says what it used. */
function toolsFromSpans(spans: Span[], run: string): TraceTool[] {
  return spans
    .filter((s) => s.run_id === run && s.kind === "tool")
    .map((s, i) => ({
      id: `${run}:${i}`,
      name: s.name,
      at: s.start,
      ms: s.end ? Math.max(Date.parse(s.end) - Date.parse(s.start), 0) : undefined,
      error: s.error,
      withheld: false,
    }));
}

/**
 * The trace of one answer: the run at `root` and every run under it, each
 * with what it did. `events` holds the events read per run; a run without
 * them falls back to its spans. A run of the same call tree whose parent was
 * not read hangs under the root; runs of other turns are left out.
 */
export function buildTrace(
  root: string,
  runs: Run[],
  spans: Span[],
  events: Record<string, RunEvent[]>,
): TraceNode | undefined {
  const byId = new Map(runs.map((r) => [r.id, r]));
  const top = byId.get(root);
  if (!top) return undefined;
  // Only runs under this answer: its own call tree. A run whose parent is
  // missing from what was read still belongs when the tree it names is this one.
  const under = (r: Run): boolean => {
    const guard = new Set<string>();
    let at: Run | undefined = r;
    while (at && !guard.has(at.id)) {
      if (at.id === root) return true;
      guard.add(at.id);
      if (!at.parent) return false;
      const up = byId.get(at.parent);
      if (!up) return at.root === root;
      at = up;
    }
    return false;
  };
  const kids = new Map<string, Run[]>();
  for (const r of runs) {
    if (r.id === root || !under(r)) continue;
    const parent = r.parent && byId.has(r.parent) ? r.parent : root;
    kids.set(parent, [...(kids.get(parent) ?? []), r]);
  }
  const seen = new Set<string>();
  const node = (run: Run): TraceNode => {
    seen.add(run.id);
    const read = events[run.id];
    const what = read
      ? readEvents(read)
      : { tools: toolsFromSpans(spans, run.id), context: [], sources: [], approvals: [] };
    // A run's own citations are its sources; events add any it cited while streaming.
    const sources: TraceSource[] = (run.citations ?? []).map((c) => ({
      title: c.title,
      url: c.url,
    }));
    for (const s of what.sources) if (!sources.some((x) => x.title === s.title)) sources.push(s);
    return {
      run: run.id,
      agent: run.agent,
      status: run.status,
      started_at: run.created_at,
      ended_at: ENDED.has(run.status) ? run.updated_at : undefined,
      ms: msOf(run),
      cost: costOf(run),
      pick: run.picked || undefined,
      tools: what.tools,
      context: what.context,
      sources,
      approvals: what.approvals,
      children: (kids.get(run.id) ?? [])
        .filter((r) => !seen.has(r.id))
        .sort((a, b) => a.created_at.localeCompare(b.created_at))
        .map(node),
    };
  };
  return node(top);
}

/** Every node in a trace, parents first. */
export function walk(node: TraceNode): TraceNode[] {
  return [node, ...node.children.flatMap(walk)];
}

/** What a trace adds up to, for its one-line summary. */
export function totals(node: TraceNode): {
  agents: number;
  tools: number;
  context: number;
  sources: number;
  approvals: number;
  withheld: boolean;
} {
  const all = walk(node);
  return {
    agents: new Set(all.map((n) => n.agent)).size,
    tools: all.reduce((n, x) => n + x.tools.length, 0),
    context: all.reduce((n, x) => n + x.context.length, 0),
    sources: all.reduce((n, x) => n + x.sources.length, 0),
    approvals: all.reduce((n, x) => n + x.approvals.length, 0),
    withheld: all.some((x) => x.tools.some((t) => t.withheld) || x.context.some((c) => c.withheld)),
  };
}

/** A reference an agent read or a session started on, as a person reads it, with a link when it has one. */
export type Reference = {
  kind: "slack" | "github" | "schedule" | "url" | "other";
  label: string;
  url?: string;
};

/**
 * `slack://T/C/ts`, `github://o/r/pull/N`, `github://o/r/issue/N`,
 * `schedule://name`, or a web address. Slack threads open in Slack's own
 * web client by team, channel and thread; a channel id starting with D is a
 * direct message.
 */
export function referenceOf(ref: string): Reference {
  const slack = /^slack:\/\/([^/]+)\/([^/]+)(?:\/([^/]+))?$/.exec(ref);
  if (slack) {
    const [, team = "", channel = "", ts] = slack;
    return {
      kind: "slack",
      label: channel.startsWith("D") ? "Slack DM" : ts ? "Slack thread" : "Slack channel",
      url: slackUrl(team, channel, ts),
    };
  }
  const gh = /^github:\/\/([^/]+)\/([^/]+)\/(pull|issue|issues)\/(\d+)$/.exec(ref);
  if (gh) {
    const [, owner, repo, kind, n] = gh;
    const path = kind === "pull" ? "pull" : "issues";
    return {
      kind: "github",
      label: `${owner}/${repo} ${kind === "pull" ? "PR" : "issue"} #${n}`,
      url: `https://github.com/${owner}/${repo}/${path}/${n}`,
    };
  }
  const repo = /^github:\/\/([^/]+)\/([^/]+)$/.exec(ref);
  if (repo) {
    const [, owner, name] = repo;
    return {
      kind: "github",
      label: `${owner}/${name}`,
      url: `https://github.com/${owner}/${name}`,
    };
  }
  if (ref.startsWith("schedule://"))
    return { kind: "schedule", label: `Schedule ${ref.slice("schedule://".length)}` };
  if (/^https?:\/\//.test(ref)) {
    try {
      const u = new URL(ref);
      return { kind: "url", label: `${u.host}${u.pathname === "/" ? "" : u.pathname}`, url: ref };
    } catch {
      return { kind: "other", label: ref };
    }
  }
  return { kind: "other", label: ref };
}

/** A Slack thread, or channel, in Slack's web client. Without a team only the channel can be opened. */
export function slackUrl(team: string, channel: string, ts?: string): string {
  const e = encodeURIComponent;
  if (!team) return `https://slack.com/app_redirect?channel=${e(channel)}`;
  return ts
    ? `https://app.slack.com/client/${e(team)}/${e(channel)}/thread/${e(channel)}-${e(ts)}`
    : `https://app.slack.com/client/${e(team)}/${e(channel)}`;
}

/** Where a session started and who can read it there, for its header. */
export type Place = {
  kind: Reference["kind"] | "console";
  label: string;
  url?: string;
  /** The origin as a reference, such as `slack://T/C/ts`: what `get_context` reads by default. */
  origin?: string;
  audience: "dm" | "channel" | "private" | "shared";
};

/**
 * A session's place, from its thread: a Slack thread by its surface (the
 * team, when known, opens it in Slack's web client), a PR, issue or schedule
 * by the root session's parent, or else the console. Slack audience is a DM
 * for a D-prefixed channel and a channel otherwise, until the platform says
 * which kind of channel it is.
 */
export function placeOf(
  thread: {
    surface?: { client: string; channel: string; ts: string };
    session?: { parent: string };
    visibility: "shared" | "private";
  },
  team = "",
): Place {
  const s = thread.surface;
  if (s) {
    const dm = s.channel.startsWith("D");
    return {
      kind: "slack",
      label: dm ? "Slack DM" : "Slack thread",
      url: slackUrl(team, s.channel, s.ts),
      origin: `slack://${team || s.client}/${s.channel}/${s.ts}`,
      audience: dm ? "dm" : "channel",
    };
  }
  const parent = thread.session?.parent ?? "";
  if (/^(slack|github|schedule):\/\//.test(parent)) {
    const ref = referenceOf(parent);
    return {
      ...ref,
      origin: parent,
      audience:
        ref.kind === "slack"
          ? ref.label === "Slack DM"
            ? "dm"
            : "channel"
          : thread.visibility === "private"
            ? "private"
            : "shared",
    };
  }
  return {
    kind: "console",
    label: "Console",
    audience: thread.visibility === "private" ? "private" : "shared",
  };
}
