import { defaultCopy } from "@humagen/components";
import { ArtifactIcon, ChevronRightIcon, FileIcon, FolderIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { Inspector } from "@humagen/web";
import { useCallback, useEffect, useState } from "react";
import type { ConversationView, WorkspaceSection } from "./route.ts";
import { Scratchpads, usePadDrafts } from "./Scratchpads.tsx";
import type { Stored } from "./storage.ts";
import { AgentMark, ago, SkeletonRows, Tabs } from "./ui.tsx";
import { WorkspaceFiles } from "./WorkspaceFiles.tsx";
import { type ConversationSandboxes, type RunSandbox, workspaceFiles } from "./workspace-files.ts";

const words = defaultCopy.workspace;

/** Reads once per open, with an explicit retry. */
function useRead<T>(open: boolean, read: () => Promise<T>) {
  const [value, setValue] = useState<T>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    void attempt;
    if (!open) return;
    let live = true;
    setFailed(false);
    read().then(
      (next) => {
        if (live) setValue(next);
      },
      () => {
        if (live) setFailed(true);
      },
    );
    return () => {
      live = false;
    };
  }, [open, read, attempt]);
  return { value, failed, retry: () => setAttempt((n) => n + 1) };
}

function Failed({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div role="alert" className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-bad">
      {message}
      <Button variant="outline" size="sm" onClick={onRetry}>
        {words.retry}
      </Button>
    </div>
  );
}

const row =
  "flex min-h-11 w-full min-w-0 items-center gap-2 rounded-aui px-2 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent";

/** The session sandbox, and a nested sandbox for each agent run in it. */
export function Sandboxes({
  thread,
  open,
  selected,
  onSelect,
  onFile,
  onActivity,
}: {
  thread: string;
  open: boolean;
  selected?: string;
  onSelect: (run?: string) => void;
  onFile: (path: string) => void;
  onActivity?: (run: string) => void;
}) {
  const read = useCallback(() => workspaceFiles.sandboxes(thread), [thread]);
  const { value, failed, retry } = useRead<ConversationSandboxes>(open, read);
  if (failed) return <Failed message={words.failed} onRetry={retry} />;
  if (!value) return <SkeletonRows count={3} />;
  const run = selected ? value.runs.find((r) => r.run === selected) : undefined;
  if (run)
    return (
      <RunDetail run={run} onBack={() => onSelect()} onFile={onFile} onActivity={onActivity} />
    );
  const { session } = value;
  return (
    <div className="flex min-w-0 flex-col gap-4" data-slot="workspace-sandboxes">
      <section
        aria-label={words.sessionSandbox}
        className="flex flex-col gap-2 rounded-aui border border-aui-border p-3"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="inline-flex items-center gap-2 text-aui-sm font-medium">
            <FolderIcon size={16} />
            {words.sessionSandbox}
          </h3>
          <span className="text-aui-xs text-aui-muted">
            {session.isolated ? words.isolated : words.unisolated}
          </span>
        </div>
        <p className="text-aui-xs text-aui-muted">{words.sessionHint(session.isolated)}</p>
        <div className="flex flex-wrap items-center justify-between gap-2">
          <span className="text-aui-xs tabular-nums">
            {words.sessionStats(session.files, words.size(session.bytes))}
          </span>
          <Button variant="outline" size="sm" onClick={() => onFile("")}>
            {words.openFiles}
          </Button>
        </div>
        {!session.writable && session.reason && (
          <p className="text-aui-xs text-aui-muted">{session.reason}</p>
        )}
      </section>
      <section aria-label={words.runs} className="flex min-w-0 flex-col gap-1">
        <h3 className="px-2 text-aui-xs font-medium text-aui-muted">{words.runs}</h3>
        {value.runs.length === 0 ? (
          <p className="px-2 py-3 text-aui-xs text-aui-muted">{words.noRuns}</p>
        ) : (
          <ul className="flex min-w-0 flex-col">
            {value.runs.map((r) => (
              <li key={r.run} style={{ paddingInlineStart: `${Math.min(r.depth, 4) * 16}px` }}>
                <button type="button" className={row} onClick={() => onSelect(r.run)}>
                  <AgentMark name={r.agent} size={14} />
                  <span className="min-w-0 flex-1 truncate">{r.agent}</span>
                  <span className="shrink-0 text-aui-xs text-aui-muted tabular-nums">
                    {words.runFiles(r.files.length + (r.more ?? 0))}
                  </span>
                  <span className="shrink-0 text-aui-xs text-aui-muted">{ago(r.created_at)}</span>
                  <ChevronRightIcon size={14} />
                </button>
              </li>
            ))}
          </ul>
        )}
        {value.more && <p className="px-2 text-aui-xs text-aui-muted">{words.moreRuns}</p>}
      </section>
    </div>
  );
}

function RunDetail({
  run,
  onBack,
  onFile,
  onActivity,
}: {
  run: RunSandbox;
  onBack: () => void;
  onFile: (path: string) => void;
  onActivity?: (run: string) => void;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-4" data-slot="workspace-run-sandbox">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button variant="ghost" size="sm" onClick={onBack}>
          {words.back}
        </Button>
        {onActivity && (
          <Button variant="ghost" size="sm" onClick={() => onActivity(run.run)}>
            {words.openActivity}
          </Button>
        )}
      </div>
      <h3 className="inline-flex min-w-0 items-center gap-2 text-aui-sm font-medium">
        <AgentMark name={run.agent} size={16} />
        <span className="truncate">{run.agent}</span>
        <span className="text-aui-xs font-normal text-aui-muted">
          {run.isolated ? words.isolated : words.unisolated}
        </span>
      </h3>
      <section aria-label={words.mounts} className="flex flex-col gap-1">
        <h4 className="text-aui-xs font-medium text-aui-muted">{words.mounts}</h4>
        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-3 gap-y-1 text-aui-xs">
          {run.mounts.map((m) => (
            <div key={m.dir} className="contents">
              <dt className="font-mono">{m.dir}</dt>
              <dd className="break-words text-aui-muted">
                {words.mountKind[m.kind] ?? m.kind}
                {m.name ? `: ${m.name}` : ""}
                {m.read_only ? `, ${words.readOnly}` : ""}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      <section aria-label={words.filesLeft} className="flex min-w-0 flex-col gap-1">
        <h4 className="text-aui-xs font-medium text-aui-muted">{words.filesLeft}</h4>
        {run.files.length === 0 ? (
          <p className="text-aui-xs text-aui-muted">{words.noFiles}</p>
        ) : (
          <ul className="flex min-w-0 flex-col">
            {run.files.map((f) => (
              <li key={f.path}>
                {f.removed ? (
                  <span className={`${row} text-aui-muted line-through hover:bg-transparent`}>
                    <FileIcon size={14} />
                    <span className="min-w-0 flex-1 truncate">{f.path}</span>
                    <span className="text-aui-xs no-underline">{words.removed}</span>
                  </span>
                ) : (
                  <button
                    type="button"
                    className={row}
                    aria-label={words.openFile(f.path)}
                    onClick={() => onFile(f.path)}
                  >
                    <FileIcon size={14} />
                    <span className="min-w-0 flex-1 truncate">{f.path}</span>
                    <span className="text-aui-xs text-aui-muted tabular-nums">
                      {words.size(f.size)}
                    </span>
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
        {(run.more ?? 0) > 0 && (
          <p className="text-aui-xs text-aui-muted">{words.moreFiles(run.more ?? 0)}</p>
        )}
      </section>
      {(run.conflicts?.length ?? 0) > 0 && (
        <section aria-label={words.conflicts} className="flex flex-col gap-1">
          <h4 className="text-aui-xs font-medium text-aui-muted">{words.conflicts}</h4>
          <p className="text-aui-xs text-aui-muted">{words.conflictHint}</p>
          <ul className="list-disc ps-5 text-aui-xs">
            {run.conflicts?.map((c) => (
              <li key={c} className="break-words">
                {c}
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/** The conversation's saved artifacts; each opens in Deliverables. */
export function Artifacts({
  thread,
  open,
  onOpen,
}: {
  thread: string;
  open: boolean;
  onOpen: (artifact: string) => void;
}) {
  const read = useCallback(() => workspaceFiles.artifacts(thread), [thread]);
  const { value, failed, retry } = useRead(open, read);
  if (failed) return <Failed message={words.artifactsFailed} onRetry={retry} />;
  if (!value) return <SkeletonRows count={3} />;
  const items: Stored[] = value.artifacts ?? [];
  return (
    <div className="flex min-w-0 flex-col gap-2" data-slot="workspace-artifacts">
      <p className="text-aui-xs text-aui-muted">{words.artifactsHint}</p>
      {items.length === 0 ? (
        <p className="py-3 text-aui-xs text-aui-muted">{words.artifactsEmpty}</p>
      ) : (
        <ul className="flex min-w-0 flex-col">
          {items.map((a) => (
            <li key={a.id}>
              <button type="button" className={row} onClick={() => onOpen(a.id)}>
                <ArtifactIcon kind={a.kind} phase="ready" size={18} />
                <span className="min-w-0 flex-1 truncate">{a.title}</span>
                <span className="shrink-0 text-aui-xs text-aui-muted">
                  {words.artifactVersion(a.version)}
                </span>
                <ChevronRightIcon size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/**
 * One place for a conversation's items. The conversation is a session with a
 * sandbox of its own; each agent run in it works in a nested one. Files keeps
 * its drafts while another section is shown or the panel is closed.
 */
export function Workspace({
  thread,
  open,
  view,
  onView,
  onClose,
  onRequest,
  onArtifact,
  onRun,
}: {
  thread: string;
  open: boolean;
  view: ConversationView;
  onView: (view: ConversationView) => void;
  onClose: () => void;
  onRequest?: (text: string) => void;
  /** Opens a saved artifact in Deliverables. */
  onArtifact: (id: string) => void;
  /** Opens a run in Activity. */
  onRun?: (run: string) => void;
}) {
  const section: WorkspaceSection = view.workspaceSection ?? "files";
  const [drafts, setDrafts] = usePadDrafts();
  const go = (next: WorkspaceSection, extra: Partial<ConversationView> = {}) =>
    onView({ panel: "workspace", workspaceSection: next === "files" ? undefined : next, ...extra });
  const nav = (
    <Tabs
      label={words.sections}
      value={section}
      tabs={(["sandboxes", "files", "scratchpads", "artifacts"] as const).map((id) => ({
        id,
        label: words.section[id],
      }))}
      onChange={(next) => go(next)}
    />
  );
  return (
    <WorkspaceFiles
      thread={thread}
      open={open && section === "files"}
      view={view}
      onView={onView}
      onClose={onClose}
      onRequest={onRequest}
      onPublished={onArtifact}
      frame={(files) => (
        <Inspector open={open} title={words.title} onClose={onClose} wide lead={nav}>
          {section === "files" ? (
            files
          ) : section === "sandboxes" ? (
            <Sandboxes
              thread={thread}
              open={open}
              selected={view.sandbox}
              onSelect={(run) => go("sandboxes", { sandbox: run })}
              onFile={(file) => go("files", file ? { file } : {})}
              onActivity={onRun}
            />
          ) : section === "scratchpads" ? (
            <Scratchpads
              thread={thread}
              open={open}
              selected={view.scratchpad}
              onSelect={(id) => go("scratchpads", { scratchpad: id })}
              drafts={drafts}
              setDrafts={setDrafts}
            />
          ) : (
            <Artifacts thread={thread} open={open} onOpen={onArtifact} />
          )}
        </Inspector>
      )}
    />
  );
}
