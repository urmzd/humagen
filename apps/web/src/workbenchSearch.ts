/**
 * What the workbench's Search tab finds: the things that belong to one
 * session, from what its page already holds (the session's summary and each
 * answer's calls) and one read of the session itself (its linked sessions
 * and what people attached). Nothing here asks the server to search. Pure;
 * no React.
 */
import { defaultCopy } from "@humagen/components";
import { allTools, type ResponseState, type ResponseTool } from "@humagen/core";
import { browserSteps, browserTool, callsOfResponses } from "./browserSteps.ts";
import { fuzzy, type Hit } from "./commands.ts";
import type { Route, WorkbenchTabId } from "./route.ts";
import type { SessionSummary } from "./sessionSummary.ts";
import type { Reference as Ref } from "./trace.ts";

/** The groups results are listed in, in this order. */
export const SEARCH_GROUPS = [
  "files",
  "pulls",
  "links",
  "commands",
  "browser",
  "tools",
  "agents",
  "attachments",
] as const;
export type SearchGroup = (typeof SEARCH_GROUPS)[number];

/** Where a result opens. */
export type SearchTarget =
  /** A saved file, in the Workspace pane. */
  | { kind: "file"; artifact: string }
  /** Somewhere outside the console, in a new browser tab. */
  | { kind: "url"; url: string }
  /** A page of the console, or another view of this conversation. */
  | { kind: "route"; route: Route }
  /** A workbench tab on a selection, or `fallback` while that tab is not built. */
  | { kind: "tab"; tab: WorkbenchTabId; ref?: string; fallback?: SearchTarget };

/** One thing the session has. */
export interface SearchItem {
  /** Unique across the session. */
  key: string;
  group: SearchGroup;
  /** What it is called: the words a query is matched against first. */
  title: string;
  /** A second line: what kind of thing it is, or what was run. Matched too, less closely. */
  detail?: string;
  /** How many times, for a tool or an agent. */
  count?: number;
  /** For an agent, its name, so the row can draw its mark. */
  agent?: string;
  target?: SearchTarget;
}

/** An item a query matched, with the letters of its title to mark. */
export interface SearchHit {
  item: SearchItem;
  hits: Hit[];
}

/** A group's matches, closest first. */
export interface SearchResults {
  group: SearchGroup;
  hits: SearchHit[];
}

/** A session as `GET /api/sessions/{id}` answers, as far as Search reads it. */
export interface SessionRead {
  links?: { target: string; kind?: string }[];
  children?: { id: string; agent?: string; title?: string }[];
  messages?: {
    id: string;
    message?: { id?: string; attachments?: { name?: string; alt?: string; media_type: string }[] };
  }[];
}

/** How many calls of one kind are listed: a long session's oldest commands give way to its newest. */
export const MAX_CALLS = 200;

// The same names sessionSummary.ts sorts under Environment, which keeps its
// own copy private: a call in the sandbox. A step in the browser is whatever
// the Browser tab takes for one (browserSteps.ts), so the two cannot disagree.
const SANDBOX = /^(sandbox_|bash$|shell$|run_command$|exec$|python$|code_execution$)/;

const note = defaultCopy.workbenchSearch.note;

const str = (v: unknown): string | undefined =>
  typeof v === "string" && v.trim() ? v.trim() : undefined;
const oneLine = (text: string, max = 160) => {
  const flat = text.replace(/\s+/g, " ").trim();
  return flat.length > max ? `${flat.slice(0, max - 1)}…` : flat;
};

/** What a sandbox call ran, when its input says. */
function commandOf(t: ResponseTool): string | undefined {
  return (
    str(t.input.command) ??
    str(t.input.cmd) ??
    str(t.input.code) ??
    str(t.input.script) ??
    (Array.isArray(t.input.argv) ? t.input.argv.filter((a) => typeof a === "string").join(" ") : "")
  );
}

/** What a browser step did, when its input says. */
function stepOf(t: ResponseTool): string | undefined {
  const action = str(t.input.action);
  const where = str(t.input.url) ?? str(t.input.selector) ?? str(t.input.text);
  return [action, where].filter(Boolean).join(" ") || undefined;
}

/** "browser_navigate" as "browser navigate": a tool's name as a person reads it. */
const spoken = (name: string) =>
  name
    .replace(/^mcp__/, "")
    .replace(/__|[_.]/g, " ")
    .trim();

/**
 * Everything the session has to find. `thread` is the conversation the
 * results open views of; `runs` are the ids of the answers its page can open
 * (a call in any other run has nowhere to open, so it opens nothing).
 */
export function searchItems(input: {
  thread: string;
  runs: string[];
  responses?: ResponseState[];
  summary?: SessionSummary;
  origin?: Ref;
  session?: SessionRead;
}): SearchItem[] {
  const { thread, summary, origin, session } = input;
  const out: SearchItem[] = [];
  const seen = new Set<string>();
  const add = (item: SearchItem) => {
    if (seen.has(item.key) || !item.title) return;
    seen.add(item.key);
    // A second line that repeats the first says nothing.
    out.push(item.detail === item.title ? { ...item, detail: undefined } : item);
  };
  const known = new Set(input.runs);
  /** The answer's activity, at the call. */
  const atCall = (response: ResponseState, call: string): SearchTarget | undefined => {
    const run = response.rootRun ?? response.id;
    return run && known.has(run)
      ? {
          kind: "route",
          route: { page: "conversation", id: thread, panel: "response", run, step: call },
        }
      : undefined;
  };

  for (const o of summary?.outputs ?? []) {
    if (o.artifactId)
      add({
        key: `file:${o.artifactId}`,
        group: "files",
        title: o.title,
        detail: o.type,
        target: { kind: "file", artifact: o.artifactId },
      });
    else if (o.url)
      add({
        key: `made:${o.url}`,
        group: o.kind === "pr" || o.kind === "issue" ? "pulls" : "links",
        title: o.title,
        detail: o.type,
        target: { kind: "url", url: o.url },
      });
  }

  if (origin?.url)
    add({
      key: `origin:${origin.url}`,
      group: "links",
      title: origin.label,
      detail: note.origin,
      target: { kind: "url", url: origin.url },
    });
  for (const child of session?.children ?? [])
    add({
      key: `session:${child.id}`,
      group: "links",
      title: child.title || child.id,
      detail: note.child,
      agent: child.agent,
      target: { kind: "route", route: { page: "conversation", id: child.id } },
    });
  for (const link of session?.links ?? [])
    add({
      key: `session:${link.target}`,
      group: "links",
      title: link.target,
      detail: link.kind === "evidence" ? note.evidence : note.context,
      target: { kind: "route", route: { page: "conversation", id: link.target } },
    });
  for (const key of ["web", "context"] as const)
    for (const item of summary?.used[key] ?? [])
      if (item.url)
        add({
          key: `page:${item.url}`,
          group: "links",
          title: item.label,
          detail: item.label === item.url ? undefined : item.url,
          target: { kind: "url", url: item.url },
        });

  // Calls, newest first: the last command run is the one most often wanted.
  const commands: SearchItem[] = [];
  const steps: SearchItem[] = [];
  const tools = new Map<string, SearchItem>();
  const agents = new Map<string, SearchItem>();
  const responses = input.responses ?? [];
  // Each browser step as the Browser tab words it, with the page it left.
  const worded = new Map(browserSteps(callsOfResponses(responses)).map((s) => [s.id, s]));
  for (let i = responses.length - 1; i >= 0; i--) {
    const response = responses[i];
    if (!response) continue;
    const calls = allTools(response);
    for (let j = calls.length - 1; j >= 0; j--) {
      const t = calls[j];
      if (!t || t.hidden) continue;
      const fallback = atCall(response, t.id);
      if (t.kind === "agent") {
        const name = t.agent ?? t.name;
        const had = agents.get(name);
        if (had) had.count = (had.count ?? 1) + 1;
        else
          agents.set(name, {
            key: `agent:${name}`,
            group: "agents",
            title: name,
            agent: name,
            count: 1,
            target: fallback,
          });
        continue;
      }
      if (SANDBOX.test(t.name)) {
        const ran = commandOf(t);
        if (commands.length < MAX_CALLS)
          commands.push({
            key: `call:${t.id}`,
            group: "commands",
            title: oneLine(ran || t.title || spoken(t.name)),
            detail: ran ? t.title || spoken(t.name) : undefined,
            target: { kind: "tab", tab: "terminal", ref: t.id, fallback },
          });
      } else if (browserTool(t.name)) {
        const did = stepOf(t);
        const step = worded.get(t.id);
        const page = step?.title ?? step?.url;
        if (steps.length < MAX_CALLS)
          steps.push({
            key: `call:${t.id}`,
            group: "browser",
            title: oneLine(t.title || step?.label || did || spoken(t.name)),
            detail: t.title && did ? oneLine(did) : page ? oneLine(page) : spoken(t.name),
            target: { kind: "tab", tab: "browser", ref: t.id, fallback },
          });
      }
      const had = tools.get(t.name);
      if (had) had.count = (had.count ?? 1) + 1;
      else
        tools.set(t.name, {
          key: `tool:${t.name}`,
          group: "tools",
          title: spoken(t.name),
          detail: t.name === spoken(t.name) ? undefined : t.name,
          count: 1,
          target: fallback,
        });
    }
  }
  for (const item of [...commands, ...steps, ...tools.values(), ...agents.values()]) add(item);

  for (const m of session?.messages ?? []) {
    const id = m.message?.id ?? m.id;
    (m.message?.attachments ?? []).forEach((a, n) => {
      add({
        key: `attachment:${id}:${n}`,
        group: "attachments",
        title: a.name || a.alt || a.media_type,
        detail: a.name || a.alt ? a.media_type : undefined,
        target: { kind: "route", route: { page: "conversation", id: thread, message: id } },
      });
    });
  }
  return out;
}

/**
 * The items a query matches, grouped, each group closest first. It ranks as
 * the composer's `/` menu does (`fuzzy` in commands.ts): a title that starts
 * with the query, then one with a word that does, then one that holds it,
 * then its letters in order. A second line counts only when it holds the
 * query whole, and after every title that does. Letters in order with gaps
 * are a last resort for a slip of the hand: they count only when nothing in
 * the session holds the query whole, since a long title holds most short
 * words that way. Items equally close keep their order. An empty query lists
 * everything.
 */
export function searchSession(items: SearchItem[], query: string): SearchResults[] {
  const q = query.trim();
  const groups = new Map<SearchGroup, { hit: SearchHit; score: number; at: number }[]>();
  items.forEach((item, at) => {
    let score = 0;
    let hits: Hit[] = [];
    if (q) {
      const title = fuzzy(item.title, q);
      const detail = item.detail ? fuzzy(item.detail, q) : null;
      if (title && title.score < 3) {
        score = title.score;
        hits = title.hits;
      } else if (detail && detail.score < 3) score = 2.5 + detail.score / 10;
      else if (title) {
        score = title.score;
        hits = title.hits;
      } else return;
    }
    const list = groups.get(item.group) ?? [];
    list.push({ hit: { item, hits }, score, at });
    groups.set(item.group, list);
  });
  const whole = [...groups.values()].some((list) => list.some((x) => x.score < 3));
  return SEARCH_GROUPS.flatMap((group) => {
    const list = (groups.get(group) ?? []).filter((x) => !whole || x.score < 3);
    if (!list.length) return [];
    list.sort((a, b) => a.score - b.score || a.at - b.at);
    return [{ group, hits: list.map((x) => x.hit) }];
  });
}

/** A target with every tab that is not built replaced by where it opens meanwhile. */
export function resolveTarget(
  target: SearchTarget | undefined,
  built: (tab: WorkbenchTabId) => boolean,
): SearchTarget | undefined {
  let at = target;
  while (at?.kind === "tab" && !built(at.tab)) at = at.fallback;
  return at;
}
