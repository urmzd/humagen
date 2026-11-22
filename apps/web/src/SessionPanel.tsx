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
import {
  defaultTraceLoader,
  eventsOfTrace,
  followTraces,
  type TraceLoader,
} from "./responseSource.ts";
import {
  type PanelOpen,
  Row,
  Section,
  type SessionChoice,
  SessionSections,
  ShowMoreList,
} from "./SessionSections.tsx";
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
import { AgentMark, SkeletonRows } from "./ui.tsx";
import type { WorkbenchTabProps } from "./workbench.ts";
import { workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.details;

/** The most answers whose activity the panel reads, newest kept. */
export const PANEL_RESPONSES = 20;

/** Every answer of a session, folded from its trace, oldest first, kept current while any runs. */
export function useSessionResponses(runs: Run[], loader?: TraceLoader) {
  const answers = runs.filter((r) => !r.parent).slice(-PANEL_RESPONSES);
  const key = answers.map((r) => `${r.id}:${r.status}`).join("|");
  const [states, setStates] = useState<ResponseState[]>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: keyed by each answer's id and status.
  useEffect(() => {
    let on = true;
    const read = () =>
      Promise.all(
        answers.map((r) =>
          eventsOfTrace(r, loader)
            .then((events) => foldAll(events))
            .catch(() => undefined),
        ),
      ).then((all) => on && setStates(all.filter((s): s is ResponseState => !!s)));
    void read();
    // While an answer runs, its session's work is read again as it happens.
    const thread = answers.find((r) => r.thread)?.thread;
    const follow = (loader ?? defaultTraceLoader).follow;
    const stop =
      thread && follow && answers.some((r) => !ENDED_RUN.has(r.status))
        ? followTraces(thread, () => true, read, follow)
        : () => {};
    return () => {
      on = false;
      stop();
    };
  }, [key, loader]);
  return states;
}

const ENDED_RUN = new Set(["completed", "failed", "stopped"]);

type View =
  | { kind: "summary" }
  | { kind: "progress" }
  | { kind: "agent"; name: string }
  | { kind: "used"; key: UsedKey | "origin" };

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
  return defaultCopy.trace.worked(p.agents, p.tools);
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
  return (
    <Inspector open={!leaving} title={titleOf(view)} onClose={onClose}>
      <SessionDetailsBody
        view={view}
        onView={setView}
        responses={responses}
        summary={summary}
        origin={origin}
        onOpenFile={onOpenFile}
        messageId={messageId}
      />
    </Inspector>
  );
}

/** What a view of Details is headed by: Details, or what a row opened. */
function titleOf(view: View): string {
  return view.kind === "summary"
    ? words.title
    : view.kind === "progress"
      ? words.progress
      : view.kind === "agent"
        ? view.name
        : view.key === "origin"
          ? words.used.origin
          : words.used[view.key];
}

/**
 * Details as the workbench's first tab: the same rows, with what a row
 * opened named above its Back, since the pane's header names the tab.
 */
export function DetailsTab({
  responses,
  summary,
  origin,
  openFile,
  openTab,
  openWorkspace,
  runs,
  thread,
  chosen,
}: WorkbenchTabProps) {
  const [view, setView] = useState<View>({ kind: "summary" });
  return (
    <SessionDetailsBody
      view={view}
      onView={setView}
      heading={view.kind === "summary" ? undefined : titleOf(view)}
      responses={responses}
      summary={summary}
      origin={origin}
      onOpenFile={openFile}
      panel={{ thread, runs, chosen, open: { tab: openTab, workspace: openWorkspace } }}
    />
  );
}

/** The body of Details, in a view the caller holds: the summary, or what one of its rows opened. */
function SessionDetailsBody({
  view,
  onView: setView,
  heading,
  responses,
  summary,
  origin,
  onOpenFile,
  messageId,
  panel,
}: {
  view: View;
  onView: (view: View) => void;
  /** The workbench's own: the sections that need the session's runs and its other views. */
  panel?: { thread: string; runs: Run[]; open: PanelOpen; chosen?: SessionChoice };
  heading?: string;
  responses: ResponseState[] | undefined;
  summary: SessionSummary | undefined;
  origin?: Ref;
  onOpenFile?: (artifactId: string) => void;
  messageId?: string;
}) {
  const current = responses?.[responses.length - 1];
  const back = () => setView({ kind: "summary" });
  return (
    <div className="flex flex-col gap-4" data-testid="session-panel" data-view={view.kind}>
      {heading && <h3 className="font-medium text-aui-base text-aui-fg">{heading}</h3>}
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
              .map((t) => <AgentCallLane key={t.id} tool={t} messageId={messageId ?? r.id} />),
          )}
        </div>
      ) : view.kind === "used" ? (
        <ul className="flex flex-col gap-1.5" data-testid="used-list">
          {view.key === "origin" && origin ? (
            <li>
              <Reference
                kind={
                  origin.kind === "slack" || origin.kind === "github" || origin.kind === "schedule"
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
          responses={responses}
          current={current}
          origin={origin}
          onView={setView}
          onOpenFile={onOpenFile}
          panel={panel}
        />
      )}
    </div>
  );
}

function Summary({
  summary,
  responses,
  current,
  origin,
  onView,
  onOpenFile,
  panel,
}: {
  summary: SessionSummary;
  responses: ResponseState[];
  panel?: { thread: string; runs: Run[]; open: PanelOpen; chosen?: SessionChoice };
  current?: ResponseState;
  origin?: Ref;
  onView: (v: View) => void;
  onOpenFile?: (artifactId: string) => void;
}) {
  const progress = progressWords(summary, current);
  // In the workbench, connections have a section of their own, and pull requests are changes.
  const used = (Object.keys(summary.used) as UsedKey[]).filter(
    (k) => summary.used[k].length > 0 && !(panel && k === "connections"),
  );
  const outputs = panel ? summary.outputs.filter((o) => o.kind !== "pr") : summary.outputs;
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
      {panel && (
        <SessionSections
          thread={panel.thread}
          runs={panel.runs}
          responses={responses}
          summary={summary}
          open={panel.open}
          chosen={panel.chosen}
          origin={origin}
          outputs={
            outputs.length > 0 ? (
              <Section label={words.outputs} testId="outputs">
                <ShowMoreList items={outputs}>
                  {(o) => (
                    <Row
                      key={o.key}
                      testId="details-output"
                      mark={outputMark[o.kind]}
                      label={o.title}
                      value={o.type}
                      href={o.url}
                      onOpen={
                        o.artifactId && onOpenFile
                          ? () => onOpenFile(o.artifactId as string)
                          : undefined
                      }
                    />
                  )}
                </ShowMoreList>
              </Section>
            ) : null
          }
        />
      )}
      {!panel && outputs.length > 0 && (
        <Section label={words.outputs}>
          {outputs.map((o) => (
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
      {!panel && summary.environment.length > 0 && (
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
  loader?: TraceLoader;
  files?: (thread: string) => Promise<{ files: SavedFile[]; sandbox?: string }>;
}) {
  const { responses, summary } = useSessionSummary({
    runs,
    thread,
    uploads,
    loader,
    files: loadFiles,
  });
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

/** What Details and the workbench read once open: each answer's activity, and the session's summary. */
export function useSessionSummary({
  runs,
  thread,
  uploads,
  loader,
  files: loadFiles,
}: {
  runs: Run[];
  thread?: string;
  uploads?: string[];
  loader?: TraceLoader;
  files?: (thread: string) => Promise<{ files: SavedFile[]; sandbox?: string }>;
}): { responses: ResponseState[] | undefined; summary: SessionSummary | undefined } {
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
  return { responses, summary };
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
