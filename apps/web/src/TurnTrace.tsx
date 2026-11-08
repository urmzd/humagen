import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon } from "@humagen/render-web";
import { useCallback, useState } from "react";
import type { Run, RunEvent } from "./adapter.ts";
import { api, type Span } from "./api.ts";
import { took, usd } from "./format.ts";
import {
  buildTrace,
  referenceOf,
  TRACE_RUN_CAP,
  type TraceContext,
  type TraceNode,
  type TraceTool,
  totals,
} from "./trace.ts";
import { AgentMark } from "./ui.tsx";

const words = defaultCopy.trace;

/** Where the trace's data comes from. Tests and the gallery pass their own. */
export type TraceLoader = {
  trace: (id: string) => Promise<{ runs: Run[]; spans: Span[] }>;
  events: (id: string) => Promise<{ events: RunEvent[] }>;
};

const live: TraceLoader = { trace: api.trace, events: api.runEvents };

type State =
  | { kind: "closed" }
  | { kind: "loading" }
  | { kind: "failed" }
  | { kind: "open"; node: TraceNode; unread: number };

/** The runs under `root`, parents first, at most `cap`: the ones whose events are read. */
function readable(root: string, runs: Run[], cap: number): string[] {
  const out = [root];
  for (let i = 0; i < out.length && out.length < cap; i++)
    for (const r of runs)
      if (r.parent === out[i] && !out.includes(r.id) && out.length < cap) out.push(r.id);
  return out;
}

/** Load one answer's trace: its call tree and spans, then the events of the runs in it. */
export async function loadTrace(
  run: Run,
  loader: TraceLoader = live,
): Promise<{ node: TraceNode; unread: number } | undefined> {
  const { runs, spans } = await loader.trace(run.id);
  const tree = runs.some((r) => r.id === run.id) ? runs : [run, ...runs];
  const ids = readable(run.id, tree, TRACE_RUN_CAP);
  const read = await Promise.all(
    ids.map((id) =>
      loader
        .events(id)
        .then((r) => [id, r.events] as const)
        // A run whose events cannot be read still shows, with its spans.
        .catch(() => undefined),
    ),
  );
  const events: Record<string, RunEvent[]> = {};
  for (const r of read) if (r) events[r[0]] = r[1];
  const node = buildTrace(run.id, tree, spans, events);
  if (!node) return undefined;
  const shown = new Set<string>();
  const count = (n: TraceNode) => {
    shown.add(n.run);
    n.children.forEach(count);
  };
  count(node);
  return { node, unread: [...shown].filter((id) => !events[id]).length };
}

/** "14s · $0.0120": a run's time and, when it was metered, what it cost. */
function timeAndCost(n: Pick<TraceNode, "ms" | "cost" | "ended_at">): string {
  return [n.ended_at ? took(n.ms) : undefined, n.cost ? usd(n.cost) : undefined]
    .filter(Boolean)
    .join(" · ");
}

const link =
  "rounded-aui underline decoration-aui-border underline-offset-2 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

function Json({ value }: { value: unknown }) {
  return (
    <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-aui bg-aui-hover p-2 font-mono text-aui-2xs text-aui-fg">
      {typeof value === "string" ? value : JSON.stringify(value, null, 2)}
    </pre>
  );
}

function ToolRow({ tool }: { tool: TraceTool }) {
  const meta = [
    tool.ms !== undefined ? took(tool.ms) : undefined,
    tool.error ? "failed" : undefined,
  ]
    .filter(Boolean)
    .join(" · ");
  return (
    <li>
      <details className="group/tool">
        <summary className="flex min-h-6 cursor-pointer list-none items-center gap-1.5 rounded-aui hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
          <ChevronRightIcon
            size={12}
            aria-hidden
            className="shrink-0 transition-transform group-open/tool:rotate-90"
          />
          <span className="font-mono text-aui-fg">{tool.name}</span>
          {meta && <span>{meta}</span>}
          {tool.withheld && <span>{words.hiddenHere}</span>}
        </summary>
        <div className="mt-1 flex flex-col gap-1.5 pl-4">
          {tool.input !== undefined && (
            <div>
              <p className="mb-0.5">{words.input}</p>
              <Json value={tool.input} />
            </div>
          )}
          {tool.error ? (
            <p className="text-aui-bad">{tool.error}</p>
          ) : tool.output !== undefined ? (
            <div>
              <p className="mb-0.5">{words.output}</p>
              <Json value={tool.output} />
            </div>
          ) : (
            !tool.withheld && <p>{words.noOutput}</p>
          )}
        </div>
      </details>
    </li>
  );
}

function ContextRow({ read, origin }: { read: TraceContext; origin?: string }) {
  const ref = referenceOf(read.reference || origin || "");
  const where = read.reference || origin ? ref : undefined;
  return (
    <li className="flex flex-col gap-0.5" data-testid="trace-context">
      <p>
        <span className="text-aui-fg">{words.context}</span>
        {read.request && <> “{read.request}”</>} {words.contextFrom}{" "}
        {where?.url ? (
          <a href={where.url} target="_blank" rel="noreferrer" className={link}>
            {where.label}
          </a>
        ) : (
          <span>{where?.label ?? words.contextOrigin}</span>
        )}
      </p>
      {read.error ? (
        <p className="text-aui-bad">{read.error}</p>
      ) : (
        read.summary && <p className="text-aui-fg">{read.summary}</p>
      )}
      {read.sources.length > 0 && (
        <p className="flex flex-wrap gap-x-3">
          {read.sources.map((s) =>
            s.url ? (
              <a key={s.url} href={s.url} target="_blank" rel="noreferrer" className={link}>
                {s.title}
              </a>
            ) : (
              <span key={s.title}>{s.title}</span>
            ),
          )}
        </p>
      )}
      {read.truncated && <p>{words.truncated}</p>}
    </li>
  );
}

function NodeView({ node, origin, top }: { node: TraceNode; origin?: string; top?: boolean }) {
  const meta = [node.status === "completed" ? undefined : node.status, timeAndCost(node)]
    .filter(Boolean)
    .join(" · ");
  return (
    <li className="flex flex-col gap-1.5" data-testid="trace-node" data-run={node.run}>
      <p className="flex flex-wrap items-center gap-x-2">
        <AgentMark name={node.agent} size={16} />
        <span className="text-aui-fg">{node.agent}</span>
        {meta && <span>{meta}</span>}
      </p>
      <div className="flex flex-col gap-1.5 pl-6">
        {node.pick && !top && <p>{words.picked(node.pick)}</p>}
        {node.context.length > 0 && (
          <ul className="flex flex-col gap-1.5">
            {node.context.map((c) => (
              <ContextRow key={c.id} read={c} origin={origin} />
            ))}
          </ul>
        )}
        {node.tools.length > 0 && (
          <ul className="flex flex-col gap-0.5">
            {node.tools.map((t) => (
              <ToolRow key={t.id} tool={t} />
            ))}
          </ul>
        )}
        {node.sources.length > 0 && (
          <div>
            <p>{words.sourcesTitle}</p>
            <ul className="flex flex-col gap-0.5 pl-2">
              {node.sources.map((s) => (
                <li key={s.title}>
                  {s.url ? (
                    <a href={s.url} target="_blank" rel="noreferrer" className={link}>
                      {s.title}
                    </a>
                  ) : (
                    s.title
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
        {node.approvals.length > 0 && (
          <ul className="flex flex-col gap-0.5">
            {node.approvals.map((a) => (
              <li key={a.id}>
                <span className="font-mono text-aui-fg">{a.action}</span>{" "}
                {[words.decided[a.decision], a.by ? words.by(a.by) : undefined]
                  .filter(Boolean)
                  .join(" ")}
                {a.note && <>: {a.note}</>}
              </li>
            ))}
          </ul>
        )}
        {node.children.length > 0 && (
          <ul className="flex flex-col gap-2 border-l border-[var(--humagen-rule)] pl-3">
            {node.children.map((c) => (
              <NodeView key={c.run} node={c} origin={origin} />
            ))}
          </ul>
        )}
      </div>
    </li>
  );
}

/**
 * The trace under an answer. Closed, it is one quiet line, as in Slack
 * ("Trace · 14s · $0.01 · 2 sources"); open, it is the tree of what ran: why
 * this agent was picked, each agent and child session it called, the tools
 * each used, what each read of the session's origin, its sources, the
 * approvals and who decided them, and the time and cost of each. What the
 * reader may not see is left out by the platform, and said once.
 */
export function TurnTrace({
  run,
  origin,
  loader = live,
}: {
  run: Run;
  /** The session's origin, such as `slack://T/C/ts`: where `get_context` reads from when a call does not say. */
  origin?: string;
  loader?: TraceLoader;
}) {
  const [state, setState] = useState<State>({ kind: "closed" });
  const open = state.kind !== "closed";
  const load = useCallback(() => {
    setState({ kind: "loading" });
    loadTrace(run, loader)
      .then((r) => setState(r ? { kind: "open", ...r } : { kind: "failed" }))
      .catch(() => setState({ kind: "failed" }));
  }, [run, loader]);
  const toggle = () => (open ? setState({ kind: "closed" }) : load());
  const sum = state.kind === "open" ? totals(state.node) : undefined;
  const sources = sum?.sources ?? run.citations?.length ?? 0;
  const line = [
    words.open,
    sum && sum.agents > 1 ? words.agents(sum.agents) : undefined,
    sum?.context ? words.reads(sum.context) : undefined,
    sum?.tools ? words.tools(sum.tools) : undefined,
    sources ? words.sources(sources) : undefined,
    sum?.approvals ? words.approvals(sum.approvals) : undefined,
    timeAndCost({
      ms: Math.max(Date.parse(run.updated_at) - Date.parse(run.created_at), 0),
      cost: run.usage?.total?.cost?.micros || run.spent || undefined,
      ended_at:
        run.status === "completed" || run.status === "failed" || run.status === "stopped"
          ? run.updated_at
          : undefined,
    }),
  ]
    .filter(Boolean)
    .join(" · ");
  const id = `trace-${run.id}`;
  return (
    <section className="pl-11 text-aui-2xs text-aui-muted" aria-label={words.label(run.agent)}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        title={open ? words.hide : words.show}
        onClick={toggle}
        className="inline-flex min-h-6 items-center gap-1 rounded-aui hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <ChevronRightIcon
          size={12}
          aria-hidden
          className={`shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
        />
        {line}
      </button>
      {open && (
        <div id={id} className="mt-2 flex flex-col gap-2">
          {state.kind === "loading" && <p>{words.loading}</p>}
          {state.kind === "failed" && (
            <p className="flex items-center gap-2">
              {words.failed}
              <button type="button" onClick={load} className={link}>
                {words.retry}
              </button>
            </p>
          )}
          {state.kind === "open" && (
            <>
              {state.node.pick && <p>{words.picked(state.node.pick)}</p>}
              {sum?.withheld && <p role="note">{words.withheld}</p>}
              <ul>
                <NodeView node={state.node} origin={origin} top />
              </ul>
              {state.unread > 0 && <p>{words.unread(state.unread)}</p>}
            </>
          )}
        </div>
      )}
    </section>
  );
}
