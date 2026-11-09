import { defaultCopy } from "@humagen/components";
import { allTools, foldAll, type ResponseState } from "@humagen/core";
import {
  ArchiveIcon,
  BrainIcon,
  ChevronRightIcon,
  CodeIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  FileIcon,
  GitHubIcon,
  MessageSquareIcon,
  MonitorIcon,
  PaperclipIcon,
  PlugIcon,
  SlackIcon,
  TerminalIcon,
} from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { type ReactNode, useEffect, useState } from "react";
import type { Run } from "./adapter.ts";
import { Reference } from "./cards.tsx";
import { took } from "./format.ts";
import { ActivityLanes, AgentCallLane, outcomeOf } from "./ResponseActivity.tsx";
import { OriginMark } from "./refChips.tsx";
import { eventsOfRun, type RunLoader } from "./responseSource.ts";
import {
  type Output,
  type SavedFile,
  type SessionSummary,
  shortList,
  summarize,
  type UsedItem,
  type UsedKey,
} from "./sessionSummary.ts";
import type { Reference as Ref } from "./trace.ts";
import { AgentMark, menuRow, SkeletonRows } from "./ui.tsx";
import { workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.details;

/** The most answers whose activity the panel reads, newest kept. */
export const PANEL_RESPONSES = 20;

/** Every answer of a session, folded from its stream, oldest first. */
export function useSessionResponses(runs: Run[], loader?: RunLoader) {
  const answers = runs.filter((r) => !r.parent).slice(-PANEL_RESPONSES);
  const key = answers.map((r) => `${r.id}:${r.status}`).join("|");
  const [states, setStates] = useState<ResponseState[]>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed by each answer's id and status.
  useEffect(() => {
    let on = true;
    Promise.all(
      answers.map((r) =>
        eventsOfRun(r, loader)
          .then((events) => foldAll(events))
          .catch(() => undefined),
      ),
    ).then((all) => on && setStates(all.filter((s): s is ResponseState => !!s)));
    return () => {
      on = false;
    };
  }, [key, loader]);
  return states;
}

type View =
  | { kind: "summary" }
  | { kind: "progress" }
  | { kind: "agent"; name: string }
  | { kind: "used"; key: UsedKey | "origin" };

const row = `${menuRow()} justify-between`;

/** One row of the panel: a mark, what it is, a short value, and where it leads. */
function Row({
  mark,
  label,
  value,
  onOpen,
  href,
  testId,
}: {
  mark: ReactNode;
  label: ReactNode;
  value?: ReactNode;
  onOpen?: () => void;
  href?: string;
  testId?: string;
}) {
  const body = (
    <>
      <span className="flex min-w-0 items-center gap-2">
        <span className="flex size-5 shrink-0 items-center justify-center text-aui-muted">
          {mark}
        </span>
        <span className="min-w-0 truncate">{label}</span>
      </span>
      <span className="flex min-w-0 shrink items-center gap-1 text-aui-muted text-aui-xs">
        {value !== undefined && <span className="min-w-0 truncate">{value}</span>}
        {(onOpen || href) && <ChevronRightIcon size={12} aria-hidden className="shrink-0" />}
      </span>
    </>
  );
  if (href)
    return (
      <li>
        <a href={href} target="_blank" rel="noreferrer" className={row} data-testid={testId}>
          {body}
        </a>
      </li>
    );
  return (
    <li>
      <button
        type="button"
        onClick={onOpen}
        disabled={!onOpen}
        className={row}
        data-testid={testId}
      >
        {body}
      </button>
    </li>
  );
}

function Section({ label, children }: { label: string; children: ReactNode }) {
  return (
    <section aria-label={label} className="flex flex-col gap-0.5" data-testid="details-section">
      <h3 className="px-2.5 pb-1 text-aui-muted text-aui-xs">{label}</h3>
      <ul className="flex flex-col">{children}</ul>
    </section>
  );
}

const outputMark: Record<Output["kind"], ReactNode> = {
  spreadsheet: <FileIcon size={16} />,
  pdf: <FileIcon size={16} />,
  document: <FileIcon size={16} />,
  presentation: <FileIcon size={16} />,
  image: <FileIcon size={16} />,
  code: <CodeIcon size={16} />,
  html: <CodeIcon size={16} />,
  data: <DatabaseIcon size={16} />,
  archive: <ArchiveIcon size={16} />,
  pr: <GitHubIcon size={16} />,
  issue: <GitHubIcon size={16} />,
  slack: <SlackIcon size={16} />,
};

const usedMark: Record<UsedKey, ReactNode> = {
  uploads: <PaperclipIcon size={16} />,
  connections: <PlugIcon size={16} />,
  knowledge: <DatabaseIcon size={16} />,
  context: <MessageSquareIcon size={16} />,
  web: <ExternalLinkIcon size={16} />,
  memory: <BrainIcon size={16} />,
};

/** "Read · notes.md, plan.md": memory says whether it was read or written. */
function memoryValue(items: UsedItem[]): string {
  const wrote = items.filter((i) => i.detail === "Wrote");
  const read = items.filter((i) => i.detail !== "Wrote");
  return [
    read.length ? `${words.read} · ${shortList(read)}` : "",
    wrote.length ? `${words.wrote} · ${shortList(wrote)}` : "",
  ]
    .filter(Boolean)
    .join("; ");
}

/** The progress line: what the current response is doing, or how it ended. */
export function progressWords(summary: SessionSummary, current?: ResponseState): string {
  const p = summary.progress;
  if (!p || !current) return "";
  if (p.status === "running") return p.waiting ? words.waiting : words.working(p.agents, p.tools);
  // A stopped or failed response says how it ended; a finished session says what it took.
  if (p.status !== "done") return outcomeOf(current);
  return defaultCopy.trace.worked(took(p.ms), p.agents, p.tools);
}

/**
 * The session's Details: its progress, the agents it brought in, what it
 * produced, where it ran, and what it used. Each row opens what it names:
 * the activity, an agent's calls, a file or link, or the list behind a value.
 * Empty sections are left out.
 */
export function SessionPanel({
  responses,
  summary,
  origin,
  leaving,
  onClose,
  onOpenFile,
  messageId,
}: {
  responses: ResponseState[] | undefined;
  summary: SessionSummary | undefined;
  origin?: Ref;
  leaving?: boolean;
  onClose: () => void;
  onOpenFile?: (artifactId: string) => void;
  messageId?: string;
}) {
  const [view, setView] = useState<View>({ kind: "summary" });
  const current = responses?.[responses.length - 1];
  const back = () => setView({ kind: "summary" });
  const title =
    view.kind === "summary"
      ? words.title
      : view.kind === "progress"
        ? words.progress
        : view.kind === "agent"
          ? view.name
          : view.key === "origin"
            ? words.used.origin
            : words.used[view.key];
  return (
    <Inspector open={!leaving} title={title} wide onClose={onClose}>
      <div className="flex flex-col gap-4" data-testid="session-panel" data-view={view.kind}>
        {view.kind !== "summary" && (
          <button
            type="button"
            onClick={back}
            className="inline-flex min-h-7 items-center gap-1 self-start rounded-aui px-1.5 text-aui-muted text-aui-xs hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <ChevronRightIcon size={12} aria-hidden className="rotate-180" />
            {words.back}
          </button>
        )}
        {!summary || !responses ? (
          <SkeletonRows count={4} />
        ) : view.kind === "progress" && current ? (
          <ActivityLanes state={current} messageId={messageId} />
        ) : view.kind === "agent" ? (
          <div className="flex flex-col gap-4">
            {responses.flatMap((r) =>
              allTools(r)
                .filter((t) => t.kind === "agent" && (t.agent ?? t.name) === view.name)
                .map((t) => <AgentCallLane key={t.id} tool={t} messageId={messageId} />),
            )}
          </div>
        ) : view.kind === "used" ? (
          <ul className="flex flex-col gap-1.5" data-testid="used-list">
            {view.key === "origin" && origin ? (
              <li>
                <Reference
                  kind={
                    origin.kind === "slack" ||
                    origin.kind === "github" ||
                    origin.kind === "schedule"
                      ? origin.kind
                      : "url"
                  }
                  label={origin.label}
                  href={origin.url}
                />
              </li>
            ) : view.key !== "origin" ? (
              summary.used[view.key].map((item) => (
                <li key={item.label}>
                  <Reference
                    kind={
                      view.key === "uploads" ? "file" : view.key === "knowledge" ? "corpus" : "url"
                    }
                    label={
                      item.detail && view.key === "memory"
                        ? `${item.detail} · ${item.label}`
                        : item.label
                    }
                    href={item.url}
                  />
                </li>
              ))
            ) : null}
          </ul>
        ) : (
          <Summary
            summary={summary}
            current={current}
            origin={origin}
            onView={setView}
            onOpenFile={onOpenFile}
          />
        )}
      </div>
    </Inspector>
  );
}

function Summary({
  summary,
  current,
  origin,
  onView,
  onOpenFile,
}: {
  summary: SessionSummary;
  current?: ResponseState;
  origin?: Ref;
  onView: (v: View) => void;
  onOpenFile?: (artifactId: string) => void;
}) {
  const progress = progressWords(summary, current);
  const used = (Object.keys(summary.used) as UsedKey[]).filter((k) => summary.used[k].length > 0);
  const tone =
    summary.progress?.status === "running"
      ? summary.progress.waiting
        ? "bg-aui-warn"
        : "bg-aui-accent"
      : summary.progress?.status === "done"
        ? "bg-aui-ok"
        : "bg-aui-bad";
  return (
    <>
      {progress && (
        <Section label={words.progress}>
          <Row
            testId="details-progress"
            mark={<span aria-hidden className={`size-2 rounded-full ${tone}`} />}
            label={progress}
            onOpen={() => onView({ kind: "progress" })}
          />
        </Section>
      )}
      {summary.agents.length > 0 && (
        <Section label={words.agents}>
          {summary.agents.map((a) => (
            <Row
              key={a.name}
              testId="details-agent"
              mark={<AgentMark name={a.name} size={18} />}
              label={a.name}
              value={words.calls(a.calls)}
              onOpen={() => onView({ kind: "agent", name: a.name })}
            />
          ))}
        </Section>
      )}
      {summary.outputs.length > 0 && (
        <Section label={words.outputs}>
          {summary.outputs.map((o) => (
            <Row
              key={o.key}
              testId="details-output"
              mark={outputMark[o.kind]}
              label={o.title}
              value={o.type}
              href={o.url}
              onOpen={
                o.artifactId && onOpenFile ? () => onOpenFile(o.artifactId as string) : undefined
              }
            />
          ))}
        </Section>
      )}
      {summary.environment.length > 0 && (
        <Section label={words.environment}>
          {summary.environment.map((e) => (
            <Row
              key={e.kind}
              testId="details-environment"
              mark={e.kind === "sandbox" ? <TerminalIcon size={16} /> : <MonitorIcon size={16} />}
              label={e.detail ? `${e.label} · ${e.detail}` : e.label}
            />
          ))}
        </Section>
      )}
      {(origin || used.length > 0) && (
        <Section label={words.usedTitle}>
          {origin && (
            <Row
              testId="details-used"
              mark={<OriginMark kind={origin.kind} size={16} />}
              label={words.used.origin}
              value={origin.label}
              href={origin.url}
            />
          )}
          {used.map((k) => (
            <Row
              key={k}
              testId="details-used"
              mark={usedMark[k]}
              label={words.used[k]}
              value={k === "memory" ? memoryValue(summary.used[k]) : shortList(summary.used[k])}
              onOpen={() => onView({ kind: "used", key: k })}
            />
          ))}
        </Section>
      )}
    </>
  );
}

/** The session's Details, loading what it needs once open: its answers' activity and its saved files. */
export function SessionDetailsPanel({
  runs,
  thread,
  uploads,
  origin,
  leaving,
  onClose,
  onOpenFile,
  loader,
  files: loadFiles,
}: {
  runs: Run[];
  thread?: string;
  uploads?: string[];
  origin?: Ref;
  leaving?: boolean;
  onClose: () => void;
  onOpenFile?: (artifactId: string) => void;
  loader?: RunLoader;
  files?: (thread: string) => Promise<{ files: SavedFile[]; sandbox?: string }>;
}) {
  const responses = useSessionResponses(runs, loader);
  const [extra, setExtra] = useState<{ files: SavedFile[]; sandbox?: string }>();
  useEffect(() => {
    let on = true;
    if (!thread || !loadFiles) {
      setExtra({ files: [] });
      return;
    }
    loadFiles(thread)
      .then((r) => on && setExtra(r))
      .catch(() => on && setExtra({ files: [] }));
    return () => {
      on = false;
    };
  }, [thread, loadFiles]);
  const summary = responses && extra ? summarize(responses, { ...extra, uploads }) : undefined;
  return (
    <SessionPanel
      responses={responses}
      summary={summary}
      origin={origin}
      leaving={leaving}
      onClose={onClose}
      onOpenFile={onOpenFile}
    />
  );
}

/**
 * The header's count, from what the page already holds: answers still
 * running or waiting on someone. Working is any that runs.
 */
export function sessionProgress(runs: Run[]): { open: number; working: boolean } {
  const live = runs.filter(
    (r) => r.status === "running" || r.status === "accepted" || r.status === "parked",
  );
  return { open: live.length, working: live.some((r) => r.status !== "parked") };
}

/** A session's saved files and whether it ran in a sandbox, for its Details. */
export async function sessionFiles(
  thread: string,
): Promise<{ files: SavedFile[]; sandbox?: string }> {
  const [saved, boxes] = await Promise.all([
    workspaceFiles.artifacts(thread).catch(() => ({ artifacts: [] })),
    workspaceFiles.sandboxes(thread).catch(() => undefined),
  ]);
  const isolated = boxes && (boxes.session.isolated || boxes.runs.some((r) => r.isolated));
  return {
    files: saved.artifacts.map((a) => ({ id: a.id, title: a.title, kind: a.kind, mime: a.mime })),
    sandbox: isolated ? words.files(boxes?.session.files ?? 0) : undefined,
  };
}
