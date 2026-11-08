import { defaultCopy } from "@humagen/components";
import { InfoIcon } from "@humagen/render-web";
import { type MouseEvent, useEffect, useState } from "react";
import type { Run } from "./adapter.ts";
import { api, coworking, type Thread } from "./api.ts";
import { took, usd } from "./format.ts";
import { ActivityList, Chips, loadTrace, type TraceLoader } from "./MessageRefs.tsx";
import { href, type Route } from "./route.ts";
import { OriginButton, usePlace } from "./SessionPlace.tsx";
import { chipsOf, type TraceNode } from "./trace.ts";
import { AgentMark, Page, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.trace;

/** What the focused page reads: the run, the runs of its call tree, its own subtree, and its session's record. */
export type FocusLoader = TraceLoader & {
  tree: (id: string) => Promise<Run[]>;
  thread: (id: string) => Promise<Thread | undefined>;
};

const live: FocusLoader = {
  trace: api.trace,
  events: api.runEvents,
  tree: api.tree,
  thread: (id) =>
    coworking
      .thread(id)
      .then((p) => p.thread)
      .catch(() => undefined),
};

/** From the call tree's top down to `id`: the breadcrumb's middle. */
function ancestors(runs: Run[], id: string): Run[] {
  const byId = new Map(runs.map((r) => [r.id, r]));
  const path: Run[] = [];
  const guard = new Set<string>();
  let at = byId.get(id);
  while (at?.parent && !guard.has(at.id)) {
    guard.add(at.id);
    at = byId.get(at.parent);
    if (at) path.unshift(at);
  }
  return path;
}

const crumb =
  "rounded-aui hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * One message focused on its own (`/messages/<run>`): the message, what it
 * was asked and answered, its references, and its direct children in time
 * order, each opening one level at a time. A breadcrumb walks back up to the
 * session's root; the address always names the message on view.
 */
export function MessageFocus({
  id,
  go,
  loader = live,
}: {
  id: string;
  go: (route: Route) => void;
  loader?: FocusLoader;
}) {
  const [node, setNode] = useState<TraceNode>();
  const [path, setPath] = useState<Run[]>([]);
  const [self, setSelf] = useState<Run>();
  const [thread, setThread] = useState<Thread>();
  const [problem, setProblem] = useState<string>();
  useEffect(() => {
    let on = true;
    setNode(undefined);
    setProblem(undefined);
    loader
      .tree(id)
      .then(async (runs) => {
        const run = runs.find((r) => r.id === id);
        if (!run) throw new Error(words.notFound);
        if (!on) return;
        setSelf(run);
        setPath(ancestors(runs, id));
        const [n, t] = await Promise.all([loadTrace(run, loader), loader.thread(run.thread)]);
        if (!on) return;
        setNode(n);
        setThread(t);
      })
      .catch((e) => on && setProblem(e instanceof Error ? e.message : words.notFound));
    return () => {
      on = false;
    };
  }, [id, loader]);
  const place = usePlace(thread);
  const open = (e: MouseEvent<HTMLAnchorElement>, to: Route) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    go(to);
  };
  const meta = node
    ? [
        node.status === "completed" ? "" : node.status,
        node.ended_at ? took(node.ms) : "",
        node.cost ? usd(node.cost) : "",
        node.model ?? "",
      ]
        .filter(Boolean)
        .join(" · ")
    : "";
  // The root's title can be a whole first message; the breadcrumb keeps its start.
  const rootLabel = (() => {
    const t = thread?.title || words.root;
    return t.length > 40 ? `${t.slice(0, 40).trimEnd()}…` : t;
  })();
  return (
    <Page
      layout="conversation"
      title={self ? words.messageBy(self.agent) : words.message}
      actions={
        <>
          <OriginButton place={place} />
          {meta && (
            <span
              title={meta}
              role="img"
              aria-label={meta}
              className="inline-flex size-9 items-center justify-center text-aui-muted"
            >
              <InfoIcon size={18} />
            </span>
          )}
        </>
      }
    >
      <div className="aui-scroll min-h-0 flex-1 overflow-y-auto">
        <Problem message={problem ?? null} />
        {!node && !problem ? (
          <div className="mx-auto w-full max-w-4xl px-4 py-6">
            <SkeletonRows count={3} />
          </div>
        ) : node && self ? (
          <article
            className="mx-auto flex w-full max-w-4xl flex-col gap-3 px-4 py-6"
            aria-label={words.messageBy(self.agent)}
          >
            <nav
              aria-label={words.breadcrumb}
              className="flex min-w-0 flex-wrap items-center gap-x-1.5 text-aui-muted text-aui-xs"
            >
              <a
                href={href({ page: "conversation", id: self.thread })}
                onClick={(e) => open(e, { page: "conversation", id: self.thread })}
                title={thread?.title}
                className={crumb}
              >
                {rootLabel}
              </a>
              {path.map((r) => (
                <span key={r.id} className="inline-flex items-center gap-x-1.5">
                  <span aria-hidden>›</span>
                  <a
                    href={href({ page: "message", id: r.id })}
                    onClick={(e) => open(e, { page: "message", id: r.id })}
                    className={crumb}
                  >
                    {r.agent}
                  </a>
                </span>
              ))}
              <span aria-hidden>›</span>
              <span aria-current="page" className="text-aui-fg">
                {self.agent}
              </span>
            </nav>
            {self.text && (
              <p className="humagen-glass max-w-[80%] self-end whitespace-pre-wrap rounded-aui-lg px-3.5 py-2 text-aui-fg text-aui-sm">
                {self.text}
              </p>
            )}
            <p className="flex items-center gap-2 font-medium text-aui-fg text-aui-sm">
              <AgentMark name={self.agent} size={24} />
              {self.agent}
            </p>
            {node.pick && <p className="text-aui-2xs text-aui-muted">{words.picked(node.pick)}</p>}
            {node.answer ? (
              <p className="whitespace-pre-wrap break-words text-aui-fg text-aui-sm">
                {node.answer}
              </p>
            ) : (
              node.error && <p className="text-aui-bad text-aui-sm">{node.error}</p>
            )}
            <Chips chips={chipsOf(node)} />
            {node.children.length > 0 && (
              <section aria-labelledby="focus-activity" className="mt-4 flex flex-col gap-3">
                <h2 id="focus-activity" className="font-medium text-aui-fg text-aui-sm">
                  {words.activity}
                </h2>
                <ActivityList node={node} skipRoot />
              </section>
            )}
          </article>
        ) : null}
      </div>
    </Page>
  );
}
