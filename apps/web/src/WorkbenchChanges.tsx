import { defaultCopy } from "@humagen/components";
import {
  ChevronDownIcon,
  ChevronRightIcon,
  PencilIcon,
  RefreshIcon,
  TrashIcon,
} from "@humagen/render-web";
import { useCallback, useEffect, useId, useMemo, useRef, useState } from "react";
import { ApiError } from "./api.ts";
import { follow } from "./cards.tsx";
import { ago, bytes } from "./format.ts";
import { href } from "./route.ts";
import { button, LoadMore, SkeletonRows } from "./ui.tsx";
import type { WorkbenchTabProps } from "./workbench.ts";
import {
  type ChangedFile,
  type Compare,
  comparison,
  DIFF_PAGE,
  type DiffRow,
  diffText,
  type FileDiff,
  fileDiff,
  LARGE_FILE_BYTES,
  previousText,
  revisionOf,
  sessionChanges,
  sessionWrites,
} from "./workbenchChanges.ts";
import {
  type ConversationSandboxes,
  type WorkspaceFileList,
  workspaceFiles,
} from "./workspace-files.ts";

const words = defaultCopy.workbenchChanges;

/*
 * The Changes tab of a session's workbench: the files the session's runs
 * wrote or removed, each with its lines added and removed against the text
 * before. The reader can put a file back to its earlier text or delete it,
 * and each of those is recorded for their next message. Editing is the
 * Workspace pane's: the tab links to it and draws no editor of its own.
 * What is known and what is not is in workbenchChanges.ts.
 */

// Text a person reads in the tab is 14px; secondary text is the muted ink.
const quiet = "text-aui-base text-aui-muted";
const act = `${button} h-9 gap-1.5 px-3 text-aui-base`;
// The label keeps the ink; the mark alone is red, since red text is under 7:1 on a light pane.
const danger = `${act} [&_svg]:text-aui-bad`;

const mark: Record<ChangedFile["state"], { sign: string; tone: string }> = {
  added: { sign: "+", tone: "text-aui-ok" },
  changed: { sign: "~", tone: "text-aui-fg" },
  removed: { sign: "−", tone: "text-aui-bad" },
};

/** A file's state as a sign and a word, so it never depends on color alone. */
function State({ state }: { state: ChangedFile["state"] }) {
  const m = mark[state];
  return (
    <span
      data-state={state}
      className="inline-flex shrink-0 items-center gap-1 font-medium text-aui-base text-aui-fg"
    >
      <span aria-hidden className={`font-aui-mono font-semibold ${m.tone}`}>
        {m.sign}
      </span>
      {words.state[state]}
    </span>
  );
}

type Read =
  | { kind: "loading" }
  | { kind: "text"; text: string; revision: string }
  | { kind: "binary" }
  | { kind: "large" }
  | { kind: "gone" }
  | { kind: "failed"; message: string };

/** The rows of a comparison, as a table: old number, new number, sign, text. */
function DiffTable({ path, diff }: { path: string; diff: FileDiff }) {
  const [shown, setShown] = useState(DIFF_PAGE);
  const rows = diff.rows.slice(0, shown);
  const row = (line: DiffRow, index: number) => {
    if (line.kind === "fold")
      return (
        <tr key={`fold:${index}`} className="bg-aui-hover text-aui-muted">
          <td colSpan={4} className="px-3 py-0.5 font-aui-sans">
            {words.fold(line.count)}
          </td>
        </tr>
      );
    const tone =
      line.kind === "add" ? "bg-aui-ok/15" : line.kind === "remove" ? "bg-aui-bad/15" : "";
    return (
      <tr key={`${index}:${line.oldLine}:${line.newLine}`} data-line={line.kind} className={tone}>
        <td className="w-12 select-none px-2 text-right align-top text-aui-muted">
          {line.oldLine ?? ""}
        </td>
        <td className="w-12 select-none border-aui-border border-r px-2 text-right align-top text-aui-muted">
          {line.newLine ?? ""}
        </td>
        <td className="w-7 select-none px-2 align-top font-semibold text-aui-fg">
          <span aria-hidden>{line.kind === "add" ? "+" : line.kind === "remove" ? "−" : ""}</span>
          <span className="sr-only">
            {line.kind === "add"
              ? words.lineAdded
              : line.kind === "remove"
                ? words.lineRemoved
                : ""}
          </span>
        </td>
        <td className="whitespace-pre pr-4 text-aui-fg">{line.text || " "}</td>
      </tr>
    );
  };
  return (
    <div className="flex min-w-0 flex-col gap-2">
      <section
        // biome-ignore lint/a11y/noNoninteractiveTabindex: a keyboard reader must be able to scroll the comparison
        tabIndex={0}
        aria-label={words.diffFor(path)}
        data-testid="changes-diff"
        className="aui-scroll max-h-[60dvh] min-w-0 overflow-auto rounded-aui border border-aui-border bg-aui-code outline-none focus-visible:ring-2 focus-visible:ring-aui-accent focus-visible:ring-inset"
      >
        <table className="w-full border-collapse font-aui-mono text-aui-base leading-6">
          <thead className="sr-only">
            <tr>
              <th>{words.oldLine}</th>
              <th>{words.newLine}</th>
              <th>{words.change}</th>
              <th>{words.text}</th>
            </tr>
          </thead>
          <tbody>{rows.map(row)}</tbody>
        </table>
      </section>
      {diff.rows.length > shown && (
        <LoadMore
          busy={false}
          label={words.more}
          className="self-start"
          onMore={() => setShown((n) => n + DIFF_PAGE)}
        />
      )}
      {diff.truncated && diff.rows.length <= shown && <p className={quiet}>{words.truncated}</p>}
    </div>
  );
}

/**
 * The second step of putting a file back or deleting it, in place: the
 * question, the action and Keep It, which has focus. Escape keeps things as
 * they are. The console's Confirm (ui.tsx) is this at 13px.
 */
function Sure({
  question,
  action,
  busy,
  onConfirm,
  onKeep,
}: {
  question: string;
  action: string;
  busy: boolean;
  onConfirm: () => void;
  onKeep: () => void;
}) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>("[data-keep]")?.focus();
  }, []);
  return (
    // biome-ignore lint/a11y/useSemanticElements: a fieldset would restyle the row; the group names the question
    <div
      ref={root}
      role="group"
      aria-label={question}
      className="flex flex-wrap items-center gap-3"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onKeep();
        }
      }}
    >
      <span className="min-w-0 break-words text-aui-base text-aui-fg">{question}</span>
      <button type="button" className={danger} disabled={busy} onClick={onConfirm}>
        {action}
      </button>
      <button type="button" data-keep className={act} disabled={busy} onClick={onKeep}>
        {words.keep}
      </button>
    </div>
  );
}

/** One file's changes: its comparison, and what the reader may do to it. */
function FileChanges({
  thread,
  file,
  writable,
  record,
  onChanged,
}: {
  thread: string;
  file: ChangedFile;
  writable: boolean;
  record: WorkbenchTabProps["record"];
  onChanged: () => void;
}) {
  const [read, setRead] = useState<Read>({ kind: "loading" });
  const [forced, setForced] = useState(false);
  const [compare, setCompare] = useState<Compare>("last");
  const [asking, setAsking] = useState<"restore" | "remove" | null>(null);
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState<{ text: string; bad: boolean } | null>(null);
  const [exact, setExact] = useState(false);
  const gone = file.state === "removed";
  const large = (file.size ?? 0) > LARGE_FILE_BYTES;
  // The file is read again whenever its content hash changes.
  const revision = file.revision;
  useEffect(() => {
    void revision;
    if (gone) {
      setRead({ kind: "gone" });
      return;
    }
    if (large && !forced) {
      setRead({ kind: "large" });
      return;
    }
    let live = true;
    setRead({ kind: "loading" });
    workspaceFiles.read(thread, file.path).then(
      (content) => {
        if (live) setRead({ kind: "text", text: content.content, revision: content.revision });
      },
      (error: unknown) => {
        if (!live) return;
        if (error instanceof ApiError && error.status === 404) setRead({ kind: "gone" });
        else if (error instanceof ApiError && /binary/i.test(error.message))
          setRead({ kind: "binary" });
        else
          setRead({
            kind: "failed",
            message: error instanceof Error ? error.message : words.unreadable,
          });
      },
    );
    return () => {
      live = false;
    };
  }, [thread, file.path, revision, gone, large, forced]);

  const current = read.kind === "text" ? read.text : undefined;
  const settled = read.kind === "text" || read.kind === "gone";
  const hasLast = settled && !gone && !!comparison(file, current, "last");
  const mode: Compare = hasLast ? compare : "all";
  const sides = settled ? comparison(file, current, mode) : undefined;
  const before = sides?.before;
  const after = sides?.after;
  const diff = useMemo(
    () =>
      before === undefined || after === undefined ? undefined : fileDiff(before, after, file.path),
    [before, after, file.path],
  );
  const previous = current === undefined ? undefined : previousText(file, current);
  // An earlier text is put back only when its hash is one a run recorded
  // leaving at this path, so what is written is the file exactly as it was.
  useEffect(() => {
    setExact(false);
    if (!previous) return;
    let live = true;
    revisionOf(previous.text).then(
      (hash) => {
        if (live) setExact(file.recorded.includes(hash));
      },
      () => {},
    );
    return () => {
      live = false;
    };
  }, [previous, file.recorded]);

  const failed = (error: unknown) => {
    if (error instanceof ApiError && error.status === 409) {
      setNote({ text: error.message || words.stale, bad: true });
      onChanged();
    } else
      setNote({ text: error instanceof Error ? error.message : words.actionFailed, bad: true });
  };
  const restore = async () => {
    if (read.kind !== "text" || !previous || busy) return;
    setBusy(true);
    setNote(null);
    try {
      await workspaceFiles.write(thread, {
        path: file.path,
        content: previous.text,
        revision: read.revision,
      });
      const undone = fileDiff(read.text, previous.text, file.path);
      record({
        source: "changes",
        summary: words.recordRestored(file.path, undone.added, undone.removed),
        detail: undone.error ?? diffText(undone),
        key: `discard:${file.path}`,
      });
      setNote({ text: words.restored, bad: false });
      setAsking(null);
      onChanged();
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };
  const remove = async () => {
    const at = read.kind === "text" ? read.revision : file.revision;
    if (!at || busy) return;
    setBusy(true);
    setNote(null);
    try {
      await workspaceFiles.remove(thread, file.path, at);
      const undone = read.kind === "text" ? fileDiff(read.text, "", file.path) : undefined;
      record({
        source: "changes",
        summary: words.recordRemoved(file.path, undone?.removed),
        detail: undone ? (undone.error ?? diffText(undone)) : undefined,
        key: `discard:${file.path}`,
      });
      setNote({ text: words.removed, bad: false });
      setAsking(null);
      onChanged();
    } catch (error) {
      failed(error);
    } finally {
      setBusy(false);
    }
  };

  const editor = href({
    page: "conversation",
    id: thread,
    panel: "workspace",
    tabs: [`file:${file.path}`],
  });
  const choice = (id: Compare, label: string) => (
    <button
      key={id}
      type="button"
      aria-pressed={mode === id}
      onClick={() => setCompare(id)}
      className="inline-flex h-9 items-center rounded-aui-sm px-3 text-aui-base text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover aria-pressed:font-medium aria-pressed:text-aui-fg pointer-coarse:h-11"
    >
      {label}
    </button>
  );
  return (
    <div data-testid="changes-file" className="flex min-w-0 flex-col gap-3 pt-1 pb-4">
      {file.conflict && <p className="text-aui-base text-aui-fg">{words.conflict}</p>}
      {hasLast && (
        <fieldset className="inline-flex self-start rounded-aui border border-aui-border p-0.5">
          <legend className="sr-only">{words.compare}</legend>
          {choice("last", words.compareLast)}
          {choice("all", words.compareAll)}
        </fieldset>
      )}
      {read.kind === "loading" && <SkeletonRows count={3} />}
      {read.kind === "binary" && <p className={quiet}>{words.binary(bytes(file.size ?? 0))}</p>}
      {read.kind === "failed" && (
        <p role="alert" className="text-aui-base text-aui-fg">
          {read.message}
        </p>
      )}
      {read.kind === "large" && (
        <div className="flex flex-wrap items-center gap-3">
          <p className={quiet}>{words.large(bytes(file.size ?? 0))}</p>
          <button type="button" className={act} onClick={() => setForced(true)}>
            {words.compareAnyway}
          </button>
        </div>
      )}
      {settled && !diff && <p className={quiet}>{words.gone}</p>}
      {diff?.error && (
        <p role="alert" className="text-aui-base text-aui-fg">
          {diff.error}
        </p>
      )}
      {diff && !diff.error && diff.rows.length === 0 && <p className={quiet}>{words.same}</p>}
      {diff && !diff.error && diff.rows.length > 0 && (
        <>
          <p className="flex flex-wrap items-center gap-x-3 text-aui-base">
            <span className="font-medium text-aui-fg">
              <span aria-hidden>
                <span className="text-aui-ok">+</span>
                {diff.added}
              </span>
              <span className="sr-only">{words.linesAdded(diff.added)}</span>
            </span>
            <span className="font-medium text-aui-fg">
              <span aria-hidden>
                <span className="text-aui-bad">−</span>
                {diff.removed}
              </span>
              <span className="sr-only">{words.linesRemoved(diff.removed)}</span>
            </span>
            {mode === "all" && !gone && <span className="text-aui-muted">{words.noEarlier}</span>}
          </p>
          <DiffTable key={`${mode}:${revision}`} path={file.path} diff={diff} />
        </>
      )}
      {!gone && (
        <div className="flex min-w-0 flex-col gap-2">
          {asking === "restore" ? (
            <Sure
              question={words.confirmRestore(file.path)}
              action={words.restore}
              busy={busy}
              onConfirm={restore}
              onKeep={() => setAsking(null)}
            />
          ) : asking === "remove" ? (
            <Sure
              question={words.confirmRemove(file.path)}
              action={words.remove}
              busy={busy}
              onConfirm={remove}
              onKeep={() => setAsking(null)}
            />
          ) : (
            <div className="flex flex-wrap items-center gap-2">
              {read.kind === "text" && (
                <a href={editor} onClick={(e) => follow(e)} className={act}>
                  <PencilIcon size={16} />
                  {words.edit}
                </a>
              )}
              {writable && previous && exact && previous.text !== current && (
                <button type="button" className={act} onClick={() => setAsking("restore")}>
                  {words.restore}
                </button>
              )}
              {writable && (read.kind === "text" || file.revision) && (
                <button type="button" className={danger} onClick={() => setAsking("remove")}>
                  <TrashIcon size={16} />
                  {words.remove}
                </button>
              )}
            </div>
          )}
        </div>
      )}
      {note && (
        <p role={note.bad ? "alert" : "status"} className="text-aui-base text-aui-fg">
          {note.text}
        </p>
      )}
    </div>
  );
}

type Loaded = { sandboxes: ConversationSandboxes; listing: WorkspaceFileList };

export function WorkbenchChanges({
  thread,
  runs,
  responses,
  selection,
  onSelection,
  record,
}: WorkbenchTabProps) {
  const [data, setData] = useState<Loaded | null>(null);
  const [problem, setProblem] = useState("");
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const list = useId();
  // Read again when a run starts or ends: that is when the files change.
  const settled = runs.map((run) => `${run.id}:${run.status}`).join(",");
  const reload = useCallback(() => setTick((n) => n + 1), []);
  useEffect(() => {
    void settled;
    void tick;
    let live = true;
    setLoading(true);
    Promise.all([workspaceFiles.sandboxes(thread), workspaceFiles.list(thread)]).then(
      ([sandboxes, listing]) => {
        if (!live) return;
        setData({ sandboxes, listing });
        setProblem("");
        setLoading(false);
      },
      (error: unknown) => {
        if (!live) return;
        setProblem(error instanceof Error && error.message ? error.message : words.failed);
        setLoading(false);
      },
    );
    return () => {
      live = false;
    };
  }, [thread, settled, tick]);
  const writes = useMemo(() => sessionWrites(responses), [responses]);
  const changes = useMemo(
    () => (data ? sessionChanges(data.sandboxes, data.listing, writes) : undefined),
    [data, writes],
  );
  const files = href({ page: "conversation", id: thread, panel: "workspace", tabs: ["files"] });

  if (!changes)
    return problem ? (
      <div className="flex flex-col items-start gap-3 py-6">
        <p role="alert" className="text-aui-base text-aui-fg">
          {problem}
        </p>
        <button type="button" className={act} onClick={reload}>
          {words.retry}
        </button>
      </div>
    ) : (
      <SkeletonRows count={4} />
    );
  if (!changes.files.length)
    return (
      <div data-testid="changes-empty" className="flex flex-col items-start gap-3 py-6">
        <p className="humagen-display font-medium text-aui-fg text-aui-lg">{words.empty}</p>
        <a href={files} onClick={(e) => follow(e)} className={act}>
          {words.openFiles}
        </a>
      </div>
    );
  return (
    <div className="flex min-w-0 flex-col gap-2" data-testid="changes">
      <div className="flex min-w-0 items-center justify-between gap-3">
        <h3 id={list} className={`min-w-0 ${quiet}`}>
          {words.list}
        </h3>
        <button
          type="button"
          aria-label={words.refresh}
          title={words.refresh}
          disabled={loading}
          aria-busy={loading || undefined}
          onClick={reload}
          className="inline-flex size-9 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-60 pointer-coarse:size-11"
        >
          <RefreshIcon size={16} />
        </button>
      </div>
      {problem && (
        <p role="alert" className="text-aui-base text-aui-fg">
          {problem}
        </p>
      )}
      {!changes.writable && changes.reason && (
        <p role="status" className={quiet}>
          {changes.reason}
        </p>
      )}
      <ul
        aria-labelledby={list}
        className="flex min-w-0 flex-col divide-y divide-aui-border border-aui-border border-y"
      >
        {changes.files.map((file) => {
          const open = selection === file.path;
          return (
            <li key={file.path} data-path={file.path} className="min-w-0">
              <button
                type="button"
                aria-expanded={open}
                onClick={() => onSelection(open ? undefined : file.path)}
                className="flex min-h-11 w-full min-w-0 flex-wrap items-center gap-x-3 gap-y-0.5 rounded-aui px-1 py-2 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
              >
                <span className="flex min-w-0 flex-1 basis-56 items-center gap-2">
                  <span aria-hidden className="shrink-0 text-aui-muted">
                    {open ? <ChevronDownIcon size={16} /> : <ChevronRightIcon size={16} />}
                  </span>
                  <span className="min-w-0 break-all font-aui-mono text-aui-base text-aui-fg">
                    {file.path}
                  </span>
                </span>
                <span className="flex shrink-0 flex-wrap items-center gap-x-3 pl-6">
                  <State state={file.state} />
                  {file.agent && (
                    <span className={quiet}>
                      {[words.by(file.agent), file.at && ago(file.at)].filter(Boolean).join(", ")}
                    </span>
                  )}
                </span>
              </button>
              {open && (
                <FileChanges
                  thread={thread}
                  file={file}
                  writable={changes.writable}
                  record={record}
                  onChanged={reload}
                />
              )}
            </li>
          );
        })}
      </ul>
      {changes.partial && <p className={quiet}>{words.partial}</p>}
    </div>
  );
}
