import { defaultCopy } from "@humagen/components";
import { fileEdits, reduce, type Task } from "@humagen/core";
import { FolderIcon } from "@humagen/render-web";
import { type ReactNode, useEffect, useState } from "react";
import type { GitHubPull } from "../../../packages/client/src/generated/types.ts";
import { adapter, type Run, turnOf } from "./adapter.ts";
import { api, type Thread } from "./api.ts";
import { type GitHubRef, githubRef } from "./github-ref.ts";
import { openPane, paneTabs } from "./pane.ts";
import { type ConversationView, href } from "./route.ts";
import { AgentMark, Problem, SkeletonRows, UsageMeter } from "./ui.tsx";
import { contextFill, contextWords } from "./usage.ts";
import { type ConversationSandboxes, workspaceFiles } from "./workspace-files.ts";

const w = defaultCopy.conversation.overviewSections;
const empty = "py-2 text-aui-xs text-aui-muted";
const row = "flex min-w-0 items-center gap-2 py-1.5 text-aui-xs";

function Section({
  title,
  summary,
  children,
}: {
  title: string;
  summary?: ReactNode;
  children: ReactNode;
}) {
  return (
    <details open className="border-b border-aui-border py-3">
      <summary className="flex min-h-9 cursor-pointer items-center gap-2 text-aui-xs font-medium list-none [&::-webkit-details-marker]:hidden group">
        <span aria-hidden className="group-open:rotate-90">
          ▸
        </span>
        <h3>{title}</h3>
        <span className="ml-auto flex items-center gap-2 font-normal text-aui-muted">
          {summary}
        </span>
      </summary>
      <div className="min-w-0 pt-2">{children}</div>
    </details>
  );
}

function references(runs: Run[], view: ConversationView, tasks: Task[]): GitHubRef[] {
  const found = new Map<string, GitHubRef>();
  const add = (text: string) => {
    const ref = githubRef(text);
    if (ref) found.set(`${ref.kind}:${ref.repo}#${ref.number}`, ref);
  };
  for (const tab of paneTabs(view))
    if (["pull", "issue", "review"].includes(tab.kind)) {
      const ref = githubRef(tab.ref ?? "");
      if (ref)
        found.set(`${tab.kind === "issue" ? "issue" : "pull"}:${ref.repo}#${ref.number}`, {
          ...ref,
          kind: tab.kind === "issue" ? "issue" : "pull",
        });
    }
  for (const run of runs) {
    // Only explicit GitHub addresses, including structured review evidence.
    const text = `${run.text}\n${run.answer ?? ""}\n${JSON.stringify(run.input ?? {})}\n${JSON.stringify(run.citations ?? [])}`;
    for (const match of text.matchAll(
      /https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/(?:pull|issues)\/[1-9]\d*|[\w.-]+\/[\w.-]+#[1-9]\d*/g,
    ))
      add(match[0]);
  }
  for (const task of tasks) {
    for (const match of `${task.input ?? ""}\n${task.output ?? ""}`.matchAll(
      /https:\/\/github\.com\/[\w.-]+\/[\w.-]+\/(?:pull|issues)\/[1-9]\d*|[\w.-]+\/[\w.-]+#[1-9]\d*/g,
    ))
      add(match[0]);
  }
  return [...found.values()].slice(0, 50);
}

interface OverviewData {
  sandboxes: ConversationSandboxes;
  runs: Run[];
  tasks: Task[];
  refs: GitHubRef[];
  repos: string[];
  partial: boolean;
  /** A pull request whose details could not be read (no GitHub connection) has no value. */
  pulls: { ref: GitHubRef; value?: GitHubPull }[];
}

/** Conversation-scoped evidence, read through the same APIs as Workspace and Activity. */
export function ConversationOverview({
  thread,
  project,
  runs,
  view = {},
}: {
  thread: Thread;
  project: string;
  runs: Run[];
  view?: ConversationView;
}) {
  const [data, setData] = useState<OverviewData>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const stamp = JSON.stringify(runs.map((r) => [r.id, r.status, r.calls, r.usage, r.answer]));
  const tabs = JSON.stringify(view.tabs);
  // Runs and tabs are compared by value because hosts construct arrays on every render.
  // biome-ignore lint/correctness/useExhaustiveDependencies: stamp and tabs represent the run evidence and references.
  useEffect(() => {
    void stamp;
    void tabs;
    void attempt;
    let live = true;
    setData(undefined);
    setFailed(false);
    const load = async () => {
      const [sandboxes, traces] = await Promise.all([
        workspaceFiles.sandboxes(thread.id),
        Promise.all(
          runs
            .filter((r) => !r.parent)
            .slice(-50)
            .map((r) => api.trace(r.id)),
        ),
      ]);
      const all = [
        ...new Map([...traces.flatMap((t) => t.runs), ...runs].map((r) => [r.id, r])).values(),
      ];
      const tasks = (
        await Promise.all(
          all.slice(-200).map(async (run) => {
            const { events } = await api.runEvents(run.id);
            const map = adapter();
            return events.flatMap(map).reduce(reduce, turnOf(run)).tasks;
          }),
        )
      ).flat();
      const refs = references(all, view, tasks);
      const repos = new Set(refs.map((ref) => ref.repo));
      for (const text of [
        ...all.map((run) => JSON.stringify([run.text, run.answer, run.input, run.citations])),
        ...tasks.map((task) => `${task.input ?? ""}\n${task.output ?? ""}`),
      ]) {
        for (const match of text.matchAll(/https:\/\/github\.com\/([\w.-]+\/[\w.-]+)/g)) {
          repos.add(match[1]!.replace(/\.git$/, ""));
        }
      }
      const pulls = await Promise.all(
        refs
          .filter((r) => r.kind === "pull")
          .map(async (ref) => ({
            ref,
            // One unreadable pull request leaves its row plain; it never fails the panel.
            value: await api.githubPull(ref.repo, ref.number).catch(() => undefined),
          })),
      );
      if (live)
        setData({
          sandboxes,
          runs: all,
          tasks,
          refs,
          repos: [...repos].slice(0, 50),
          pulls,
          partial:
            sandboxes.more ||
            sandboxes.runs.some((r) => (r.more ?? 0) > 0) ||
            all.length > 200 ||
            runs.filter((r) => !r.parent).length > 50 ||
            repos.size > 50,
        });
    };
    load().catch(() => {
      if (live) setFailed(true);
    });
    return () => {
      live = false;
    };
  }, [thread.id, stamp, tabs, attempt]);
  if (failed)
    return (
      <div className="flex flex-col gap-2">
        <Problem message={w.failed} />
        <button
          type="button"
          className="min-h-9 text-aui-xs underline"
          onClick={() => setAttempt((n) => n + 1)}
        >
          {w.retry}
        </button>
      </div>
    );
  if (!data) return <SkeletonRows count={4} />;
  const fill = contextFill(runs.at(-1)?.usage);
  const repos = data.repos;
  const agents = new Map(
    data.runs.map(
      (r) =>
        [
          r.agent,
          r.status === "parked"
            ? "waiting"
            : r.status === "failed" || r.status === "stopped"
              ? "failed"
              : r.status === "completed"
                ? "done"
                : "working",
        ] as const,
    ),
  );
  for (const task of data.tasks)
    if (task.subagent && !agents.has(task.subagent.name))
      agents.set(
        task.subagent.name,
        task.status === "error" || task.status === "denied"
          ? "failed"
          : task.status === "waiting"
            ? "waiting"
            : task.status === "complete"
              ? "done"
              : "working",
      );
  const changes = new Map<string, { added: number; removed: number } | undefined>();
  for (const sandbox of data.sandboxes.runs)
    for (const file of sandbox.files) changes.set(file.path, undefined);
  for (const edit of fileEdits(data.tasks.filter((task) => task.status === "complete")).values()) {
    if (edit.change === "wrote") {
      if (!changes.has(edit.path)) changes.set(edit.path, undefined);
      continue;
    }
    const before = changes.get(edit.path);
    changes.set(edit.path, {
      added: (before?.added ?? 0) + edit.added,
      removed: (before?.removed ?? 0) + edit.removed,
    });
  }
  const counts = [...changes.values()].filter((c) => c !== undefined);
  const total = counts.reduce(
    (a, c) => ({ added: a.added + c.added, removed: a.removed + c.removed }),
    { added: 0, removed: 0 },
  );
  return (
    <div className="min-w-0">
      <Section
        title={w.environment}
        summary={repos.length || data.sandboxes.runs.length ? w.connected : w.disconnected}
      >
        <a
          className={`${row} underline underline-offset-4`}
          href={href({ page: "project", name: thread.project, tab: "overview" })}
        >
          {project}
        </a>
        {repos.length ? (
          repos.map((repo) => (
            <div key={repo} className={row}>
              <svg
                aria-hidden="true"
                role="presentation"
                width="14"
                height="14"
                viewBox="0 0 16 16"
                fill="none"
                stroke="currentColor"
              >
                <path d="M3 2h10v12H3zM5 2v12M8 5h3M8 8h3" />
              </svg>
              <a href={`https://github.com/${repo}`} className="break-all underline">
                {repo}
              </a>
            </div>
          ))
        ) : (
          <p className={empty}>{w.noRepos}</p>
        )}
        {data.sandboxes.runs.map((r) => (
          <a
            key={r.run}
            className={row}
            href={href({
              page: "conversation",
              id: thread.id,
              panel: "workspace",
              workspaceSection: "sandboxes",
              sandbox: r.run,
            })}
          >
            <FolderIcon size={14} />
            <span className="truncate">
              {w.sandbox}: {r.agent}
            </span>
            <span className="ml-auto max-w-28 truncate text-aui-muted" title={r.run}>
              {r.run}
            </span>
          </a>
        ))}
        {data.pulls
          .filter((p) => p.value?.subject.head?.ref)
          .map(({ ref, value }) => (
            <p key={`${ref.repo}#${ref.number}`} className={empty}>
              {ref.repo}: {value?.subject.head?.ref}
            </p>
          ))}
      </Section>
      <Section
        title={w.context}
        summary={
          fill ? (
            <>
              <span>{Math.round((100 * fill.used) / fill.limit)}%</span>
              <UsageMeter size="mini" label={w.context} spent={fill.used} limit={fill.limit} />
            </>
          ) : undefined
        }
      >
        <p className={empty}>{fill ? contextWords(runs.at(-1)?.usage) : w.noContext}</p>
      </Section>
      <Section title={w.agents} summary={agents.size || undefined}>
        {agents.size ? (
          [...agents].map(([name, status]) => (
            <div key={name} className={row}>
              <AgentMark name={name} size={18} />
              <span className="truncate">{name}</span>
              <span
                className={`ml-auto ${status === "failed" ? "text-aui-bad" : "text-aui-muted"}`}
              >
                {w.status[status]}
              </span>
            </div>
          ))
        ) : (
          <p className={empty}>{w.noAgents}</p>
        )}
      </Section>
      <Section
        title={w.changes}
        summary={counts.length ? `+${total.added} -${total.removed}` : undefined}
      >
        {data.partial && <p className={empty}>{w.partial}</p>}
        {counts.length > 0 && changes.size > counts.length && (
          <p className={empty}>{w.partialCounts}</p>
        )}
        {changes.size ? (
          [...changes].map(([path, count]) => (
            <div key={path} className={row}>
              <a
                className="min-w-0 flex-1 truncate underline"
                href={href({
                  page: "conversation",
                  id: thread.id,
                  panel: "workspace",
                  file: path,
                  workspaceTab: "changes",
                })}
              >
                {path}
              </a>
              <span className="shrink-0 text-aui-muted">
                {count ? `+${count.added} -${count.removed}` : w.unknownCounts}
              </span>
            </div>
          ))
        ) : (
          <p className={empty}>{w.noChanges}</p>
        )}
        {data.pulls.length ? (
          data.pulls.map(({ ref, value }) => {
            const status = !value
              ? null
              : value.subject.state === "merged"
                ? "merged"
                : value.subject.state === "closed"
                  ? "closed"
                  : "open";
            return (
              <a
                key={`${ref.repo}#${ref.number}`}
                className={`${row} flex-wrap`}
                href={href({
                  page: "conversation",
                  id: thread.id,
                  ...openPane(view, { kind: "pull", ref: `${ref.repo}#${ref.number}` }),
                })}
              >
                {status && (
                  <span
                    className={`rounded-aui px-1.5 py-0.5 ${status === "open" ? "bg-aui-ok/10 text-aui-ok" : status === "merged" ? "bg-aui-info/10 text-aui-info" : "bg-aui-bad/10 text-aui-bad"}`}
                  >
                    {w.pullStatus[status]}
                  </span>
                )}
                <span>
                  {ref.repo} #{ref.number}
                </span>
                {value && <span className="w-full truncate">{value.subject.title}</span>}
              </a>
            );
          })
        ) : (
          <p className={empty}>{w.noPulls}</p>
        )}
      </Section>
    </div>
  );
}
