import { defaultCopy } from "@humagen/components";
import { CalendarIcon, GitHubIcon, MaximizeIcon, SlackIcon } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { type MouseEvent as ReactMouseEvent, useEffect, useRef, useState } from "react";
import type { Run, RunEvent } from "./adapter.ts";
import { api, type Span } from "./api.ts";
import { Pill, type ReferenceKind, Reference as SharedReference } from "./cards.tsx";
import { took, usd } from "./format.ts";
import { href } from "./route.ts";
import {
  agentsOf,
  buildTrace,
  type Chip,
  chipsOf,
  flatten,
  type Reference,
  TRACE_RUN_CAP,
  type TraceNode,
} from "./trace.ts";
import { AgentMark, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.trace;

/** Where a message's trace comes from. Tests pass their own. */
export type TraceLoader = {
  trace: (id: string) => Promise<{ runs: Run[]; spans: Span[] }>;
  events: (id: string) => Promise<{ events: RunEvent[] }>;
};

const live: TraceLoader = { trace: api.trace, events: api.runEvents };
const ENDED = new Set(["completed", "failed", "stopped"]);

/** The runs under `root`, parents first, at most `cap`: the ones whose events are read. */
function readable(root: string, runs: Run[], cap: number): string[] {
  const out = [root];
  for (let i = 0; i < out.length && out.length < cap; i++)
    for (const r of runs)
      if (r.parent === out[i] && !out.includes(r.id) && out.length < cap) out.push(r.id);
  return out;
}

/** One answer's trace: its call tree and spans, then the events of the runs in it. */
export async function loadTrace(
  run: Run,
  loader: TraceLoader = live,
): Promise<TraceNode | undefined> {
  const { runs, spans } = await loader.trace(run.id);
  const tree = runs.some((r) => r.id === run.id) ? runs : [run, ...runs];
  const read = await Promise.all(
    readable(run.id, tree, TRACE_RUN_CAP).map((id) =>
      loader
        .events(id)
        .then((r) => [id, r.events] as const)
        // A run whose events cannot be read still shows, with its spans.
        .catch(() => undefined),
    ),
  );
  const events: Record<string, RunEvent[]> = {};
  for (const r of read) if (r) events[r[0]] = r[1];
  return buildTrace(run.id, tree, spans, events);
}

// An ended run's trace does not change, so each is read once per page load.
const cache = new Map<string, Promise<TraceNode | undefined>>();

/**
 * An answer's trace, read once it has ended and come into view (or at once
 * when `eager`), so a long session reads only what is looked at.
 */
export function useTrace(
  run: Run,
  { eager = false, loader = live }: { eager?: boolean; loader?: TraceLoader } = {},
) {
  const at = useRef<HTMLDivElement>(null);
  const [node, setNode] = useState<TraceNode>();
  const [seen, setSeen] = useState(eager);
  const ended = ENDED.has(run.status);
  useEffect(() => {
    if (seen || !at.current || typeof IntersectionObserver === "undefined") return;
    const watch = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting)) {
        setSeen(true);
        watch.disconnect();
      }
    });
    watch.observe(at.current);
    return () => watch.disconnect();
  }, [seen]);
  useEffect(() => {
    if (!ended || !seen) return;
    let on = true;
    const key = loader === live ? run.id : "";
    let p = key ? cache.get(key) : undefined;
    if (!p) {
      p = loadTrace(run, loader).catch(() => undefined);
      if (key) cache.set(key, p);
    }
    p.then((n) => on && setNode(n));
    return () => {
      on = false;
    };
  }, [run, ended, seen, loader]);
  return { at, node };
}

function chipText(c: Chip): string {
  if (c.kind === "approval")
    return [words.decided[c.decision ?? "ask"], c.label, c.by ? words.by(c.by) : ""]
      .filter(Boolean)
      .join(" ");
  if (c.kind === "failure") return words.failedTool(c.label);
  return c.label;
}

/** What a chip's link points at, for its mark. */
function kindOf(c: Chip, origin?: Reference): ReferenceKind {
  if (c.kind === "origin" && origin)
    return origin.kind === "slack" || origin.kind === "github" || origin.kind === "schedule"
      ? origin.kind
      : "url";
  const host = c.url ? (/^https?:\/\/([^/]+)/.exec(c.url)?.[1] ?? "") : "";
  if (host.endsWith("slack.com")) return "slack";
  if (host === "github.com") return "github";
  return c.kind === "source" ? "file" : "url";
}

/**
 * Follow a link to another console page without reloading: a plain click
 * moves the address and tells the router, which reads it as Back would.
 */
export function inApp(event: ReactMouseEvent<HTMLAnchorElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
    return;
  event.preventDefault();
  window.history.pushState(null, "", event.currentTarget.getAttribute("href") ?? "/");
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** Slack's or GitHub's mark, or a calendar for a schedule. */
export function OriginMark({ kind, size = 16 }: { kind: Reference["kind"]; size?: number }) {
  if (kind === "slack") return <SlackIcon size={size} aria-hidden />;
  if (kind === "github") return <GitHubIcon size={size} aria-hidden />;
  if (kind === "schedule") return <CalendarIcon size={size} aria-hidden />;
  return null;
}

/**
 * A message's references as quiet chips, each the shared Reference: its
 * origin, what it pulled in and the sources it cited, linked. An approval and
 * who decided it, or a tool that failed, is a state, so it is a Pill.
 */
export function Chips({ chips, origin }: { chips: Chip[]; origin?: Reference }) {
  if (!chips.length) return null;
  return (
    <ul className="flex flex-wrap gap-1" aria-label={words.references}>
      {chips.map((c) => {
        const text = chipText(c);
        const hint = c.detail ? `${text}: ${c.detail}` : text;
        return (
          <li
            key={`${c.kind}:${c.url ?? c.label}`}
            className="min-w-0"
            data-chip={c.kind}
            title={hint}
          >
            {c.kind === "approval" || c.kind === "failure" ? (
              <Pill
                tone={
                  c.kind === "failure" || c.decision === "deny"
                    ? "bad"
                    : c.decision === "ask"
                      ? "warn"
                      : "ok"
                }
              >
                {text}
              </Pill>
            ) : (
              <SharedReference kind={kindOf(c, origin)} label={text} href={c.url} title={hint} />
            )}
          </li>
        );
      })}
    </ul>
  );
}

/** A run's time, what it cost when metered, and its model: "5.5s · $0.01 · claude-sonnet". */
function timeAndCost(n: TraceNode): string {
  return [n.ended_at ? took(n.ms) : "", n.cost ? usd(n.cost) : "", n.model ?? ""]
    .filter(Boolean)
    .join(" · ");
}

/**
 * Every run behind a response as one flat list in time order, whatever the
 * nesting: the pick and its reason first, then each agent with what it was
 * asked, what it answered, its references, its time and cost, and a link that
 * opens it at its own address.
 */
export function ActivityList({ node, skipRoot = false }: { node: TraceNode; skipRoot?: boolean }) {
  const rows = flatten(node).filter((n) => !skipRoot || n.run !== node.run);
  return (
    <div className="flex flex-col gap-4" data-testid="activity-list">
      {node.pick && !skipRoot && (
        <p className="text-aui-muted text-aui-xs" data-testid="activity-pick">
          {words.picked(node.pick)}
        </p>
      )}
      <ol className="flex flex-col gap-4">
        {rows.map((n) => (
          <li
            key={n.run}
            data-testid="activity-row"
            data-run={n.run}
            className="flex flex-col gap-1.5 border-[var(--humagen-rule)] border-t pt-3 first:border-t-0 first:pt-0"
          >
            <p className="flex min-w-0 items-center gap-2 text-aui-sm">
              <AgentMark name={n.agent} size={18} />
              <span className="min-w-0 truncate font-medium text-aui-fg">{n.agent}</span>
              {n.status !== "completed" && (
                <span className="text-aui-muted text-aui-xs">{n.status}</span>
              )}
              <span className="ml-auto shrink-0 text-aui-muted text-aui-xs tabular-nums">
                {timeAndCost(n)}
              </span>
              <a
                href={href({ page: "message", id: n.run })}
                onClick={inApp}
                aria-label={words.focus(n.agent)}
                title={words.focus(n.agent)}
                className="inline-flex size-7 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <MaximizeIcon size={14} />
              </a>
            </p>
            {n.ask && (
              <p className="line-clamp-3 whitespace-pre-wrap text-aui-muted text-aui-xs">
                <span className="sr-only">{words.asked}: </span>
                {n.ask}
              </p>
            )}
            {n.answer ? (
              <p className="whitespace-pre-wrap break-words text-aui-fg text-aui-sm">{n.answer}</p>
            ) : (
              n.error && <p className="text-aui-bad text-aui-sm">{n.error}</p>
            )}
            <Chips chips={chipsOf(n)} />
          </li>
        ))}
      </ol>
    </div>
  );
}

/**
 * Under an answer that more than one agent worked on: one quiet control, the
 * agents' marks and how many, that opens the response's activity in Details.
 */
function ViewActivity({ node, onOpen }: { node: TraceNode; onOpen: () => void }) {
  const agents = agentsOf(node);
  return (
    <button
      type="button"
      onClick={onOpen}
      data-testid="view-activity"
      className="inline-flex min-h-7 items-center gap-2 self-start rounded-aui px-1.5 text-aui-muted text-aui-xs hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      <span aria-hidden className="flex -space-x-1.5">
        {agents.slice(0, 3).map((a) => (
          <span key={a} className="inline-flex rounded-full ring-2 ring-[var(--aui-bg)]">
            <AgentMark name={a} size={16} />
          </span>
        ))}
      </span>
      <span>{words.viewActivity}</span>
      <span>· {words.agents(agents.length)}</span>
    </button>
  );
}

/**
 * Under an answer: its references as quiet chips and, when more than one
 * agent worked on it, one control that opens its activity in Details. Time,
 * cost and model are in Details, not here.
 */
export function AnswerRefs({
  run,
  eager,
  loader,
  onActivity,
}: {
  run: Run;
  eager?: boolean;
  loader?: TraceLoader;
  /** Open this response's activity in Details. Without it, no control is offered. */
  onActivity?: (run: string) => void;
}) {
  const { at, node } = useTrace(run, { eager, loader });
  const fallback = (run.citations ?? []).map(
    (c): Chip => ({ kind: "source", label: c.title, url: c.url }),
  );
  const chips = node ? chipsOf(node) : fallback;
  return (
    <div ref={at} className="flex flex-col gap-1" data-testid="answer-refs">
      <Chips chips={chips} />
      {node && onActivity && agentsOf(node).length > 1 && (
        <ViewActivity node={node} onOpen={() => onActivity(run.id)} />
      )}
    </div>
  );
}

/** Under the root message: where it came from, linked. */
export function OriginRef({ origin }: { origin?: Reference }) {
  if (
    !origin ||
    (origin.kind !== "slack" && origin.kind !== "github" && origin.kind !== "schedule")
  )
    return null;
  return (
    <Chips chips={[{ kind: "origin", label: origin.label, url: origin.url }]} origin={origin} />
  );
}

/** One answer in Details: who, when, how long, what it cost, on which model, and its references. */
function ReferenceRow({
  run,
  origin,
  loader,
}: {
  run: Run;
  origin?: Reference;
  loader?: TraceLoader;
}) {
  const { at, node } = useTrace(run, { eager: true, loader });
  const meta = node
    ? [
        node.ended_at ? took(node.ms) : undefined,
        node.cost ? usd(node.cost) : undefined,
        node.model,
      ]
        .filter(Boolean)
        .join(" · ")
    : "";
  const all = node
    ? [
        ...chipsOf(node, origin),
        ...node.children.flatMap(function down(n: TraceNode): Chip[] {
          return [...chipsOf(n), ...n.children.flatMap(down)];
        }),
      ]
    : [];
  return (
    <li ref={at as never} className="flex flex-col gap-1" data-testid="reference-row">
      <p className="flex flex-wrap items-center gap-x-2 text-aui-xs">
        <AgentMark name={run.agent} size={16} />
        <a
          href={`#turn-${run.id}`}
          className="inline-flex min-h-6 items-center rounded-aui text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
        >
          {run.agent}
        </a>
        {meta && <span className="text-aui-muted">{meta}</span>}
      </p>
      {node && node.children.length > 0 && (
        <p className="pl-6 text-aui-2xs text-aui-muted">
          {words.delegatedTo(node.children.map((c) => c.agent).join(", "))}
        </p>
      )}
      <div className="pl-6">
        <Chips chips={all} origin={origin} />
      </div>
    </li>
  );
}

/**
 * Every message's references for the whole session, in Details: per answer,
 * its time, cost and model, what it delegated, and what it read and touched.
 */
export function SessionReferences({
  runs,
  origin,
  loader,
}: {
  runs: Run[];
  origin?: Reference;
  loader?: TraceLoader;
}) {
  // Answers people see: each turn's own run, not the runs it called.
  const answers = runs.filter((r) => !r.parent);
  if (!answers.length) return null;
  return (
    <section aria-labelledby="session-references" className="flex flex-col gap-2">
      <h3 id="session-references" className="font-medium text-aui-fg text-aui-sm">
        {words.references}
      </h3>
      <ul className="flex flex-col gap-3">
        {answers.map((r, i) => (
          <ReferenceRow key={r.id} run={r} origin={i === 0 ? origin : undefined} loader={loader} />
        ))}
      </ul>
    </section>
  );
}

/**
 * One response's activity in the Details panel: every run behind it, flat and
 * in time order, read from the response's own run.
 */
export function ResponseActivity({
  run,
  leaving,
  onClose,
  loader,
}: {
  run: Run;
  leaving?: boolean;
  onClose: () => void;
  loader?: TraceLoader;
}) {
  const { at, node } = useTrace(run, { eager: true, loader });
  return (
    <Inspector open={!leaving} title={words.activity} wide onClose={onClose}>
      <div ref={at} data-testid="response-activity">
        {node ? <ActivityList node={node} /> : <SkeletonRows count={3} />}
      </div>
    </Inspector>
  );
}
