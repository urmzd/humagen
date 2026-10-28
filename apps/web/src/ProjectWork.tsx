import { defaultCopy } from "@humagen/components";
import { CheckIcon, ChevronRightIcon, ClockIcon, MessageIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { type ReactNode, useState } from "react";
import type { ThreadSummary } from "./api.ts";
import { AgentMark, ago, button, Count, SkeletonRows } from "./ui.tsx";

type WorkFilter = "all" | "running" | "attention" | "finished";
const c = defaultCopy.projectWork;
const filters: WorkFilter[] = ["all", "running", "attention", "finished"];

/** Counts describe loaded conversations, never approvals or an unqueried total. */
export const workState = (thread: ThreadSummary): Exclude<WorkFilter, "all"> =>
  thread.active > 0 ? "running" : thread.failed > 0 ? "attention" : "finished";

/** Pure console presentation. The project host owns reading, paging and retry. */
export function ProjectWork({
  threads,
  project,
  controls,
  failed,
  onRetry,
  onOpenThread,
  hasMore,
  loadingMore,
  onMore,
  moreFailed,
}: {
  threads: ThreadSummary[] | null;
  project: string;
  controls?: ReactNode;
  failed?: boolean;
  onRetry?: () => void;
  onOpenThread: (id: string) => void;
  hasMore?: boolean;
  loadingMore?: boolean;
  onMore?: () => void;
  moreFailed?: boolean;
}) {
  const [filter, setFilter] = useState<WorkFilter>("all");
  const shown = threads?.filter((t) => filter === "all" || workState(t) === filter) ?? [];
  return (
    <section
      aria-label={defaultCopy.projects.tasks}
      className="humagen-work flex min-w-0 flex-col gap-4"
    >
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="flex items-center gap-2 text-aui-base font-semibold text-aui-fg">
            {defaultCopy.projects.tasks}
            {threads && <Count n={threads.length} />}
          </h2>
          <p className="mt-1 max-w-[56ch] text-aui-xs text-aui-muted">{c.hint}</p>
        </div>
        {controls}
      </div>
      {failed ? (
        <div role="alert" className="humagen-work-empty">
          <p className="font-medium text-aui-fg">{c.failed}</p>
          <p className="mt-1 text-aui-sm text-aui-muted">{c.failedHint}</p>
          {onRetry && (
            <button type="button" onClick={onRetry} className={`${button} mt-4`}>
              {c.retry}
            </button>
          )}
        </div>
      ) : threads === null ? (
        <SkeletonRows count={4} height="3.5rem" />
      ) : threads.length === 0 ? (
        <div className="humagen-work-empty">
          <MessageIcon size={22} className="mb-3 text-aui-muted" />
          <p className="font-medium text-aui-fg">{c.empty}</p>
          <p className="mt-1 max-w-[48ch] text-aui-sm text-aui-muted">
            {defaultCopy.projects.noTasks}
          </p>
        </div>
      ) : (
        <>
          <div role="group" aria-label={c.filterLabel} className="flex flex-wrap gap-1">
            {filters.map((key) => {
              const count =
                key === "all" ? threads.length : threads.filter((t) => workState(t) === key).length;
              return (
                <Button
                  key={key}
                  variant="ghost"
                  size="sm"
                  aria-pressed={filter === key}
                  onClick={() => setFilter(key)}
                  className="humagen-work-filter"
                >
                  {c.filter[key]}
                  <span className="tabular-nums opacity-65">{count}</span>
                </Button>
              );
            })}
          </div>
          {hasMore && <p className="text-aui-xs text-aui-muted">{c.loadedOnly}</p>}
          {shown.length === 0 ? (
            <p role="status" className="humagen-work-empty text-aui-sm text-aui-muted">
              {c.noMatch}
            </p>
          ) : (
            <ul className="humagen-work-list">
              {shown.map((thread) => {
                const state = workState(thread);
                const agent = thread.agents[thread.agents.length - 1];
                return (
                  <li key={thread.id}>
                    <button
                      type="button"
                      onClick={() => onOpenThread(thread.id)}
                      className="humagen-work-row"
                    >
                      <span className="humagen-work-mark">
                        {agent ? (
                          <AgentMark
                            name={agent}
                            size={20}
                            state={
                              state === "running"
                                ? "thinking"
                                : state === "attention"
                                  ? "error"
                                  : "idle"
                            }
                          />
                        ) : (
                          <MessageIcon size={18} />
                        )}
                      </span>
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-aui-sm font-medium text-aui-fg">
                          {thread.title}
                        </span>
                        <span className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-aui-xs text-aui-muted">
                          {agent && <span>{agent}</span>}
                          {(thread.project ?? "") !== project && (
                            <span>
                              {defaultCopy.projects.within([
                                thread.project || defaultCopy.projects.workspace,
                              ])}
                            </span>
                          )}
                          {(thread.origin === "cron" || thread.origin === "manual") && (
                            <span>{c.scheduled}</span>
                          )}
                          <time dateTime={thread.updated_at}>{ago(thread.updated_at)}</time>
                        </span>
                      </span>
                      <span className="humagen-work-status" data-state={state}>
                        {state === "finished" ? (
                          <CheckIcon size={13} />
                        ) : state === "running" ? (
                          <ClockIcon size={13} />
                        ) : (
                          <span aria-hidden className="size-1.5 rounded-full bg-current" />
                        )}
                        {c.status[state]}
                      </span>
                      <ChevronRightIcon size={15} className="humagen-work-chevron" />
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </>
      )}
      {moreFailed && (
        <p role="alert" className="text-aui-sm text-aui-bad">
          {c.moreFailed}
        </p>
      )}
      {hasMore && onMore && (
        <button
          type="button"
          className={`${button} self-start`}
          disabled={loadingMore}
          onClick={onMore}
        >
          {loadingMore ? c.loadingMore : defaultCopy.projects.moreTasks}
        </button>
      )}
    </section>
  );
}
