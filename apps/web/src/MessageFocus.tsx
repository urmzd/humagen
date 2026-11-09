import { defaultCopy } from "@humagen/components";
import { answerOf } from "@humagen/core";
import { InfoIcon } from "@humagen/render-web";
import { type MouseEvent, useEffect, useMemo, useState } from "react";
import type { Run } from "./adapter.ts";
import { api, coworking, type Thread } from "./api.ts";
import { ActivityLanes, responseChips, summaryOf, useResponse } from "./ResponseActivity.tsx";
import { Chips } from "./refChips.tsx";
import { type TraceLoader, traceSource } from "./responseSource.ts";
import { href, type Route } from "./route.ts";
import { OriginButton, usePlace } from "./SessionPlace.tsx";
import { AgentMark, Page, Problem, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.trace;

/** What the focused page reads: the runs of its call tree, for the breadcrumb, its trace, and its session's record. */
export type FocusLoader = TraceLoader & {
  tree: (id: string) => Promise<Run[]>;
  thread: (id: string) => Promise<Thread | undefined>;
};

const live: FocusLoader = {
  trace: api.messageTrace,
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
  const [path, setPath] = useState<Run[]>([]);
  const [self, setSelf] = useState<Run>();
  const [thread, setThread] = useState<Thread>();
  const [problem, setProblem] = useState<string>();
  useEffect(() => {
    let on = true;
    setSelf(undefined);
    setProblem(undefined);
    loader
      .tree(id)
      .then(async (runs) => {
        const run = runs.find((r) => r.id === id);
        if (!run) throw new Error(words.notFound);
        if (!on) return;
        setSelf(run);
        setPath(ancestors(runs, id));
        const t = await loader.thread(run.thread);
        if (on) setThread(t);
      })
      .catch((e) => on && setProblem(e instanceof Error ? e.message : words.notFound));
    return () => {
      on = false;
    };
  }, [id, loader]);
  const place = usePlace(thread);
  // The focused message's own response: what it did, folded as any response is.
  const source = useMemo(
    () => (self ? traceSource(self, loader === live ? undefined : loader) : undefined),
    [self, loader],
  );
  const { state, ended } = useResponse(source);
  const node = ended ? state : undefined;
  const open = (e: MouseEvent<HTMLAnchorElement>, to: Route) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    go(to);
  };
  const meta = node ? summaryOf(node) : "";
  const answer = node ? answerOf(node) : undefined;
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
            {answer?.text ? (
              <p className="whitespace-pre-wrap break-words text-aui-fg text-aui-sm">
                {answer.text}
              </p>
            ) : (
              node.error && <p className="text-aui-bad text-aui-sm">{node.error}</p>
            )}
            <Chips chips={responseChips(node)} />
            {node.content.some((b) => b.type === "tool") && (
              <section aria-labelledby="focus-activity" className="mt-4 flex flex-col gap-3">
                <h2 id="focus-activity" className="font-medium text-aui-fg text-aui-sm">
                  {words.activity}
                </h2>
                <p className="text-aui-muted text-aui-xs">{meta}</p>
                <ActivityLanes state={node} />
              </section>
            )}
          </article>
        ) : null}
      </div>
    </Page>
  );
}
