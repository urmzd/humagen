import { defaultCopy } from "@humagen/components";
import {
  BrainIcon,
  ChevronRightIcon,
  CircleIcon,
  ColumnsIcon,
  DiffIcon,
  FileIcon,
  FolderIcon,
  ForkIcon,
  LayersIcon,
  MaximizeIcon,
  MinimizeIcon,
  PlusIcon,
  StickyNoteIcon,
  XIcon,
} from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { Inspector } from "@humagen/web";
import { type ComponentProps, type ReactNode, useCallback, useEffect, useState } from "react";
import { Deliverables } from "./Deliverables.tsx";
import { Memories } from "./Memories.tsx";
import {
  activeKey,
  closeTab,
  openTab,
  type PaneKind,
  type PaneTab,
  paneTabs,
  parseTab,
  replaceTab,
  tabKey,
} from "./pane.ts";
import type { ConversationView } from "./route.ts";
import { SaveAsArtifact } from "./SaveAsArtifact.tsx";
import { Scratchpads, usePadDrafts } from "./Scratchpads.tsx";
import { AgentMark, ago, Popover, SkeletonRows } from "./ui.tsx";
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
  onSelect: (run?: string, label?: string) => void;
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
                <button type="button" className={row} onClick={() => onSelect(r.run, r.agent)}>
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

const pane = defaultCopy.workspace.pane;

const icons: Record<PaneKind, ReactNode> = {
  sandboxes: <FolderIcon size={14} />,
  sandbox: <FolderIcon size={14} />,
  files: <FileIcon size={14} />,
  file: <FileIcon size={14} />,
  scratchpads: <StickyNoteIcon size={14} />,
  scratchpad: <StickyNoteIcon size={14} />,
  memories: <BrainIcon size={14} />,
  memory: <BrainIcon size={14} />,
  artifacts: <LayersIcon size={14} />,
  artifact: <LayersIcon size={14} />,
  issue: <CircleIcon size={14} />,
  pull: <ForkIcon size={14} />,
  review: <DiffIcon size={14} />,
};

/** What a tab is called: a list by its section, an item by its name when known. */
function tabLabel(tab: PaneTab, labels: Record<string, string>): string {
  const known = labels[tabKey(tab)];
  if (known) return known;
  if (!tab.ref) return pane.kind[tab.kind];
  if (tab.kind === "file") return tab.ref.split("/").at(-1) || tab.ref;
  if (tab.kind === "issue" || tab.kind === "pull") return tab.ref.replace(/^.*\//, "");
  return pane.kind[tab.kind];
}

/** The item tabs a "+" opens: each list, from which an item opens its own tab. */
const PICKS: PaneKind[] = ["files", "scratchpads", "artifacts", "sandboxes", "memories"];

/**
 * The conversation's Workspace: a pane beside it with a tab for each open
 * item, a file, a scratchpad, an artifact, a run's sandbox or memory. On a
 * desktop it is the third pane, which can fill the page or show two tabs
 * side by side; on a phone it is a full-screen sheet with the same tabs. The
 * open tabs are the pane's address. Files and scratchpads keep their drafts
 * while another tab is shown or the pane is closed.
 */
export function Workspace({
  thread,
  open,
  view,
  onView,
  onClose,
  onRequest,
  onRun,
  extraTab,
  extraPicks,
  onRevise,
  promotion,
}: {
  thread: string;
  open: boolean;
  view: ConversationView;
  onView: (view: ConversationView) => void;
  onClose: () => void;
  onRequest?: (text: string) => void;
  /** Opens a run in Activity. */
  onRun?: (run: string) => void;
  /** Renders tabs of kinds this pane does not own, such as a GitHub issue or a review. */
  extraTab?: (tab: PaneTab, show: (view: ConversationView) => void) => ReactNode | undefined;
  /** More "+" choices, such as opening a GitHub issue. */
  extraPicks?: (close: () => void) => ReactNode;
  /** An artifact tab's Request Changes and promotion, as Deliverables takes them. */
  onRevise?: ComponentProps<typeof Deliverables>["onRevise"];
  promotion?: ComponentProps<typeof Deliverables>["promotion"];
}) {
  const [drafts, setDrafts] = usePadDrafts();
  const [labels, setLabels] = useState<Record<string, string>>({});
  const tabs = paneTabs(view);
  const active = activeKey(view);
  const current = parseTab(active) ?? (tabs.at(-1) as PaneTab);
  const keys = tabs.map(tabKey);
  const before = keys[keys.indexOf(active) - 1] ?? keys[keys.indexOf(active) + 1];
  const second = view.split && before ? parseTab(before) : undefined;
  const fileKind = (t?: PaneTab) => t?.kind === "file" || t?.kind === "files";
  const name = (tab: PaneTab, label?: string) => {
    if (label) setLabels((l) => ({ ...l, [tabKey(tab)]: label }));
    return tab;
  };
  /** Opens an item from a tab: a list keeps its tab, an item tab is replaced. */
  const show = (
    from: PaneTab,
    tab: PaneTab,
    label?: string,
    extra: Partial<ConversationView> = {},
  ) =>
    onView({
      ...(from.ref
        ? replaceTab(view, tabKey(from), name(tab, label))
        : openTab(view, name(tab, label))),
      ...extra,
    });
  const nameTab = useCallback(
    (id: string, label: string) =>
      setLabels((l) =>
        l[`scratchpad:${id}`] === label ? l : { ...l, [`scratchpad:${id}`]: label },
      ),
    [],
  );
  const nameArtifact = useCallback(
    (id: string, label: string) =>
      setLabels((l) => (l[`artifact:${id}`] === label ? l : { ...l, [`artifact:${id}`]: label })),
    [],
  );
  const close = (key: string) => {
    const next = closeTab(view, key);
    if (next) onView(next);
    else onClose();
  };
  const toggle = (field: "expanded" | "split") =>
    onView({ ...view, [field]: !view[field] || undefined });

  const render = (tab: PaneTab, files: ReactNode): ReactNode => {
    switch (tab.kind) {
      case "files":
      case "file":
        return files;
      case "sandboxes":
      case "sandbox":
        return (
          <Sandboxes
            thread={thread}
            open={open}
            selected={tab.ref}
            onSelect={(run, label) =>
              show(tab, run ? { kind: "sandbox", ref: run } : { kind: "sandboxes" }, label)
            }
            onFile={(file) =>
              onView(openTab(view, file ? { kind: "file", ref: file } : { kind: "files" }))
            }
            onActivity={onRun}
          />
        );
      case "scratchpads":
      case "scratchpad":
        return (
          <Scratchpads
            thread={thread}
            open={open}
            selected={tab.ref}
            onSelect={(id, label) =>
              show(tab, id ? { kind: "scratchpad", ref: id } : { kind: "scratchpads" }, label)
            }
            drafts={drafts}
            setDrafts={setDrafts}
            onName={nameTab}
            actions={(pad, dirty) => (
              <SaveAsArtifact
                thread={thread}
                what={{ source: "scratchpad", id: pad.id }}
                label
                disabled={dirty}
                className="inline-flex min-h-8 items-center gap-1.5 rounded-aui px-2.5 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:min-h-11"
                onSaved={(artifact) =>
                  onView(openTab(view, { kind: "artifact", ref: artifact.id }))
                }
              />
            )}
          />
        );
      case "memories":
      case "memory":
        return (
          <Memories
            thread={thread}
            open={open}
            selected={tab.ref}
            onSelect={(run, label) =>
              show(tab, run ? { kind: "memory", ref: run } : { kind: "memories" }, label)
            }
          />
        );
      case "artifacts":
      case "artifact":
        return (
          <Deliverables
            open={open}
            thread={thread}
            view={{ ...view, artifact: tab.ref }}
            onView={(next) =>
              show(
                tab,
                next.artifact ? { kind: "artifact", ref: next.artifact } : { kind: "artifacts" },
                undefined,
                {
                  artifactTab: next.artifactTab,
                  slide: next.slide,
                  compare: next.compare,
                },
              )
            }
            onEditCopy={(file) => onView(openTab(view, { kind: "file", ref: file }))}
            onRevise={onRevise}
            promotion={promotion}
            onClose={onClose}
            frame={(body) => body}
            onTitle={nameArtifact}
          />
        );
      default:
        return (
          extraTab?.(tab, onView) ?? (
            <p className="text-aui-xs text-aui-muted">{pane.unavailable}</p>
          )
        );
    }
  };

  const strip = (
    <div className="flex min-w-0 items-center gap-1">
      <div
        role="tablist"
        aria-label={pane.tabs}
        className="aui-scroll flex min-w-0 flex-1 gap-1 overflow-x-auto"
      >
        {tabs.map((tab) => {
          const key = tabKey(tab);
          const label = tabLabel(tab, labels);
          return (
            <span
              key={key}
              className={`inline-flex min-w-0 shrink-0 items-center rounded-aui ${key === active ? "bg-aui-hover text-aui-fg" : "text-aui-muted"}`}
            >
              <button
                type="button"
                role="tab"
                aria-selected={key === active}
                title={tab.ref ?? label}
                onClick={() => onView(openTab(view, tab))}
                className="inline-flex min-h-9 max-w-44 items-center gap-1.5 px-2 text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
              >
                {icons[tab.kind]}
                <span className="truncate">{label}</span>
              </button>
              <button
                type="button"
                aria-label={pane.close(label)}
                onClick={() => close(key)}
                className="inline-flex size-7 items-center justify-center rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
              >
                <XIcon size={12} />
              </button>
            </span>
          );
        })}
      </div>
      <Popover
        label={pane.open}
        button={<PlusIcon size={16} />}
        className="inline-flex size-9 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
        align="right"
        narrow
      >
        {(closeMenu) => (
          <div className="flex flex-col p-1">
            {PICKS.map((kind) => (
              <button
                key={kind}
                type="button"
                className="inline-flex min-h-9 items-center gap-2 rounded-aui px-2.5 text-left text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
                onClick={() => {
                  closeMenu();
                  onView(openTab(view, { kind }));
                }}
              >
                {icons[kind]}
                {pane.pick[kind]}
              </button>
            ))}
            {extraPicks?.(closeMenu)}
          </div>
        )}
      </Popover>
    </div>
  );
  const actions = (
    <>
      <button
        type="button"
        aria-label={pane.split}
        title={pane.split}
        aria-pressed={!!view.split}
        disabled={tabs.length < 2}
        onClick={() => toggle("split")}
        className="inline-flex size-9 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40 aria-pressed:text-aui-fg max-lg:hidden"
      >
        <ColumnsIcon size={16} />
      </button>
      <button
        type="button"
        aria-label={view.expanded ? pane.collapse : pane.expand}
        title={view.expanded ? pane.collapse : pane.expand}
        aria-pressed={!!view.expanded}
        onClick={() => toggle("expanded")}
        className="inline-flex size-9 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-fg max-lg:hidden"
      >
        {view.expanded ? <MinimizeIcon size={16} /> : <MaximizeIcon size={16} />}
      </button>
    </>
  );
  const fileView: ConversationView = {
    ...view,
    file: current.kind === "file" ? current.ref : second?.kind === "file" ? second.ref : undefined,
  };
  return (
    <WorkspaceFiles
      thread={thread}
      open={open && (fileKind(current) || fileKind(second))}
      view={fileView}
      onView={(next) =>
        onView({
          ...openTab(view, next.file ? { kind: "file", ref: next.file } : { kind: "files" }),
          workspaceTab: next.workspaceTab,
        })
      }
      onClose={onClose}
      onRequest={onRequest}
      onPublished={(id) => onView(openTab(view, { kind: "artifact", ref: id }))}
      frame={(files) => (
        <Inspector
          open={open}
          title={defaultCopy.workspace.title}
          onClose={onClose}
          wide
          expanded={!!view.expanded}
          lead={strip}
          actions={actions}
        >
          {second ? (
            <div className="grid min-w-0 gap-4 lg:grid-cols-2" data-slot="workspace-split">
              <section aria-label={tabLabel(current, labels)} className="min-w-0">
                {render(current, files)}
              </section>
              <section
                aria-label={tabLabel(second, labels)}
                className="min-w-0 max-lg:hidden lg:border-aui-border lg:border-l lg:pl-4"
              >
                {fileKind(second) && fileKind(current) ? (
                  <p className="text-aui-xs text-aui-muted">{pane.splitFiles}</p>
                ) : (
                  render(second, files)
                )}
              </section>
            </div>
          ) : (
            <div className="min-w-0" data-slot={`workspace-tab-${current.kind}`}>
              {render(current, files)}
            </div>
          )}
        </Inspector>
      )}
    />
  );
}
