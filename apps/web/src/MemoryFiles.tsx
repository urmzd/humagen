import { defaultCopy } from "@humagen/components";
import { CodeEditor, FileBrowser } from "@humagen/web";
import { useEffect, useState } from "react";
import type {
  DriveVersion,
  MemoryFile,
  MemoryFiles as Tree,
} from "../../../packages/client/src/generated/types.ts";
import { ApiError, api, type MemoryTreeRef } from "./api.ts";
import { ago, bytes } from "./format.ts";
import {
  button,
  Confirm,
  Empty,
  LoadMore,
  Problem,
  Section,
  SkeletonRows,
  textAction,
} from "./ui.tsx";

/** A file larger than this is named and sized, not fetched and drawn. */
const LARGEST_SHOWN = 512 * 1024;
/** The most text one save may carry, which is the server's own limit. */
const LARGEST_SAVED = 1024 * 1024;
/** How many versions one read of the history asks for. */
const HISTORY_PAGE = 20;

/**
 * One memory tree as files: the tree at a version, the open file's text, the
 * proposals waiting on it and its versions. Someone who may write the tree
 * can edit a text file at the newest version, restore an earlier version and
 * decide proposals. A reader sees none of those controls, and a refusal that
 * arrives anyway says so and leaves the view as it was.
 */
/** A file being edited: the person's text and the tree version it was read at. */
interface Draft {
  path: string;
  text: string;
  base: number;
  /** The file changed on the server after it was read, so the save was refused. */
  conflict?: boolean;
  /** Why the last save or reload did not happen, in words. */
  error?: string;
}

export function MemoryFiles({
  scope,
  project,
  agent,
  file,
  onFile,
}: MemoryTreeRef & {
  /** The open file's path, from the address. */
  file?: string;
  onFile?: (path: string) => void;
}) {
  const copy = defaultCopy.memoryFiles;
  const [tree, setTree] = useState<Tree | null>(null);
  const [loadError, setLoadError] = useState(false);
  // Zero is the newest version. Any other number is an earlier one on view.
  const [version, setVersion] = useState(0);
  const [reload, setReload] = useState(0);
  const [picked, setPicked] = useState(file ?? "");
  const [content, setContent] = useState<MemoryFile | null>(null);
  const [contentError, setContentError] = useState(false);
  const [history, setHistory] = useState<DriveVersion[] | null>(null);
  const [historyError, setHistoryError] = useState(false);
  const [historyMore, setHistoryMore] = useState(false);
  const [historyBusy, setHistoryBusy] = useState(false);
  const [asking, setAsking] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  // A draft stays with its file, so opening another file and coming back keeps the text.
  const [draft, setDraft] = useState<Draft | null>(null);
  const [saving, setSaving] = useState(false);

  useEffect(() => setPicked(file ?? ""), [file]);

  useEffect(() => {
    let live = true;
    setLoadError(false);
    api
      .memoryFiles({ scope, project, agent }, version)
      .then((next) => {
        if (live) setTree(next);
      })
      .catch(() => {
        if (live) setLoadError(true);
      });
    return () => {
      live = false;
    };
  }, [scope, project, agent, version, reload]);

  useEffect(() => {
    let live = true;
    setHistoryError(false);
    api
      .memoryHistory({ scope, project, agent }, 0, HISTORY_PAGE)
      .then((rows) => {
        if (!live) return;
        setHistory(rows);
        setHistoryMore(rows.length === HISTORY_PAGE);
      })
      .catch(() => {
        if (live) setHistoryError(true);
      });
    return () => {
      live = false;
    };
  }, [scope, project, agent, reload]);

  // The open file is the one in the address when the tree has it, or else the
  // tree's first, so an earlier version that lacks a file still shows one.
  const entries = tree?.entries ?? [];
  const entry = entries.find((e) => e.path === picked) ?? entries[0];
  const path = entry?.path ?? "";
  const hash = entry?.hash ?? "";
  const tooLarge = (entry?.size ?? 0) > LARGEST_SHOWN;

  // A file is read again when its content changes, which its hash tells.
  useEffect(() => {
    let live = true;
    setContent(null);
    setContentError(false);
    if (!path || !hash || tooLarge) return;
    api
      .memoryFile({ scope, project, agent }, path, version)
      .then((next) => {
        if (live) setContent(next);
      })
      .catch(() => {
        if (live) setContentError(true);
      });
    return () => {
      live = false;
    };
  }, [scope, project, agent, path, hash, version, tooLarge, reload]);

  /** Runs a change and shows the tree it answers with, or says why it did not happen. */
  const change = async (run: () => Promise<Tree>, done?: string) => {
    setBusy(true);
    setProblem(null);
    setNotice(null);
    try {
      const next = await run();
      setTree(next);
      setVersion(0);
      setReload((n) => n + 1);
      if (done) setNotice(done);
    } catch (err) {
      setProblem(
        err instanceof ApiError && err.status === 403
          ? defaultCopy.noPermission
          : copy.actionFailed,
      );
    } finally {
      setBusy(false);
    }
  };
  const restore = (n: number) => {
    setAsking(null);
    void change(() => api.restoreMemoryFiles({ scope, project, agent }, n), copy.restored(n));
  };
  const save = async () => {
    if (!draft) return;
    setSaving(true);
    setProblem(null);
    setNotice(null);
    try {
      const next = await api.writeMemoryFile(
        { scope, project, agent },
        draft.path,
        draft.text,
        draft.base,
      );
      setTree(next);
      setDraft(null);
      setReload((n) => n + 1);
      setNotice(copy.saved(next.version));
    } catch (err) {
      const status = err instanceof ApiError ? err.status : 0;
      // The text is kept whatever went wrong. Only the reason shown differs.
      setDraft({
        ...draft,
        conflict: status === 409,
        error:
          status === 409
            ? undefined
            : status === 403
              ? defaultCopy.noPermission
              : status === 400 && err instanceof Error
                ? err.message
                : copy.actionFailed,
      });
    } finally {
      setSaving(false);
    }
  };
  /** Replaces the draft with the file as it is now, once the person asks for that. */
  const loadLatest = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const at = { scope, project, agent };
      const [next, latest] = await Promise.all([
        api.memoryFiles(at),
        api.memoryFile(at, draft.path),
      ]);
      setTree(next);
      setDraft({ path: draft.path, text: latest.content, base: next.version });
    } catch {
      setDraft({ ...draft, error: copy.latestFailed });
    } finally {
      setSaving(false);
    }
  };
  const older = async () => {
    const last = history?.at(-1);
    if (!last) return;
    setHistoryBusy(true);
    try {
      const rows = await api.memoryHistory({ scope, project, agent }, last.version, HISTORY_PAGE);
      setHistory((have) => [...(have ?? []), ...rows]);
      setHistoryMore(rows.length === HISTORY_PAGE);
    } catch {
      setHistoryError(true);
    } finally {
      setHistoryBusy(false);
    }
  };

  if (loadError && !tree)
    return (
      <div className="flex flex-col items-start gap-3">
        <Problem message={copy.loadFailed} />
        <button type="button" className={button} onClick={() => setReload((n) => n + 1)}>
          {copy.retry}
        </button>
      </div>
    );
  if (!tree) return <SkeletonRows count={4} />;

  const newest = history?.[0]?.version ?? tree.version;
  const editing = version === 0 && tree.can_write && draft?.path === path ? draft : null;
  return (
    <div className="flex min-w-0 flex-col gap-6">
      <Problem message={problem} />
      {loadError && <Problem message={copy.loadFailed} />}
      {notice && (
        <p role="status" className="text-sm text-muted-foreground">
          {notice}
        </p>
      )}
      {version > 0 && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm">
          <p role="status">{copy.viewing(version)}</p>
          <button type="button" className={textAction} onClick={() => setVersion(0)}>
            {copy.backToNewest}
          </button>
        </div>
      )}
      <Section title={copy.files} count={entries.length}>
        {entries.length === 0 ? (
          <Empty title={copy.empty} />
        ) : (
          <FileBrowser
            label={copy.browse}
            paths={entries.map((e) => e.path)}
            selected={path}
            onSelect={(next) => {
              setPicked(next);
              onFile?.(next);
            }}
          >
            <div className="flex min-w-0 flex-col gap-4 p-5">
              <p className="flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                <span className="min-w-0 break-all">{`${tree.mount}/${path}`}</span>
                <span>{bytes(entry?.size ?? 0)}</span>
              </p>
              {editing ? (
                <div className="flex min-w-0 flex-col gap-3">
                  {editing.conflict && (
                    <div className="flex flex-col items-start gap-3">
                      <Problem message={copy.conflict} />
                      <button
                        type="button"
                        className={button}
                        disabled={saving}
                        onClick={() => void loadLatest()}
                      >
                        {copy.loadLatest}
                      </button>
                    </div>
                  )}
                  <Problem message={editing.error ?? null} />
                  <CodeEditor
                    path={path}
                    value={editing.text}
                    readOnly={saving}
                    saving={saving}
                    maxBytes={LARGEST_SAVED}
                    onChange={(text) => setDraft({ ...editing, text })}
                    onSave={() => void save()}
                  />
                  <button
                    type="button"
                    className={`${button} self-start`}
                    disabled={saving}
                    onClick={() => setDraft(null)}
                  >
                    {copy.cancel}
                  </button>
                </div>
              ) : tooLarge ? (
                <p className="text-sm text-muted-foreground">
                  {copy.tooLarge(bytes(entry?.size ?? 0))}
                </p>
              ) : contentError ? (
                <div className="flex flex-col items-start gap-3">
                  <Problem message={copy.fileFailed} />
                  <button type="button" className={button} onClick={() => setReload((n) => n + 1)}>
                    {copy.retry}
                  </button>
                </div>
              ) : content === null ? (
                <SkeletonRows count={3} />
              ) : content.binary ? (
                <p className="text-sm text-muted-foreground">{copy.binary}</p>
              ) : (
                <>
                  {tree.can_write && version === 0 && (
                    <button
                      type="button"
                      className={`${button} self-start`}
                      disabled={busy}
                      onClick={() => setDraft({ path, text: content.content, base: tree.version })}
                    >
                      {copy.edit}
                    </button>
                  )}
                  <pre className="whitespace-pre-wrap break-words font-mono text-sm leading-6">
                    {content.content}
                  </pre>
                </>
              )}
            </div>
          </FileBrowser>
        )}
      </Section>
      {tree.proposals.length > 0 && (
        <Section title={copy.proposals} count={tree.proposals.length}>
          <ul className="divide-y divide-border">
            {tree.proposals.map((p) => (
              <li key={p.id} className="flex flex-wrap items-start gap-x-4 gap-y-2 py-3">
                <div className="min-w-0 flex-1 text-sm">
                  {p.message && <p className="break-words">{p.message}</p>}
                  <ul className="mt-1 flex flex-col gap-0.5 text-muted-foreground">
                    {p.changes.map((c) => (
                      <li key={c.path} className="break-all">
                        {`${c.delete ? copy.removes : copy.writes} ${c.path}`}
                      </li>
                    ))}
                  </ul>
                  <p className="mt-1 flex flex-wrap gap-x-3 text-xs text-muted-foreground">
                    {p.by && <span>{copy.proposalBy(p.by)}</span>}
                    <span>{copy.proposalOn(p.base)}</span>
                    {p.at && <time dateTime={p.at}>{ago(p.at)}</time>}
                  </p>
                </div>
                {tree.can_write && (
                  <div className="flex flex-wrap gap-2">
                    <button
                      type="button"
                      className={button}
                      disabled={busy}
                      onClick={() =>
                        void change(() =>
                          api.decideMemoryProposal({ scope, project, agent }, p.id, "approve"),
                        )
                      }
                    >
                      {copy.approve}
                    </button>
                    <button
                      type="button"
                      className={button}
                      disabled={busy}
                      onClick={() =>
                        void change(() =>
                          api.decideMemoryProposal({ scope, project, agent }, p.id, "discard"),
                        )
                      }
                    >
                      {copy.discard}
                    </button>
                  </div>
                )}
              </li>
            ))}
          </ul>
        </Section>
      )}
      <Section title={copy.history}>
        {historyError && (
          <div className="flex flex-col items-start gap-3">
            <Problem message={copy.historyFailed} />
            <button type="button" className={button} onClick={() => setReload((n) => n + 1)}>
              {copy.retry}
            </button>
          </div>
        )}
        {history === null ? (
          !historyError && <SkeletonRows count={3} />
        ) : (
          <ul className="divide-y divide-border">
            {history.map((v) => (
              <li key={v.version} className="flex flex-col gap-2 py-3">
                <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm">
                  <span
                    className="font-medium"
                    aria-current={v.version === tree.version ? "true" : undefined}
                  >
                    {copy.version(v.version)}
                  </span>
                  <span className="flex min-w-0 flex-1 flex-wrap gap-x-3 text-xs text-muted-foreground">
                    {v.version === newest && <span>{copy.newest}</span>}
                    {v.restored ? <span>{copy.restoredFrom(v.restored)}</span> : null}
                    {v.message && <span className="break-words">{v.message}</span>}
                    {v.by && <span>{v.by}</span>}
                    {v.at && <time dateTime={v.at}>{ago(v.at)}</time>}
                  </span>
                  {v.version !== newest && asking !== v.version && (
                    <span className="flex flex-wrap gap-x-4">
                      <button
                        type="button"
                        className={textAction}
                        aria-label={`${copy.view} ${copy.version(v.version)}`}
                        onClick={() => setVersion(v.version)}
                      >
                        {copy.view}
                      </button>
                      {tree.can_write && (
                        <button
                          type="button"
                          className={textAction}
                          aria-label={`${copy.restore} ${copy.version(v.version)}`}
                          disabled={busy}
                          onClick={() => setAsking(v.version)}
                        >
                          {copy.restore}
                        </button>
                      )}
                    </span>
                  )}
                </div>
                {asking === v.version && (
                  <Confirm
                    question={copy.restoreAsk(v.version)}
                    action={copy.restore}
                    onConfirm={() => restore(v.version)}
                    onKeep={() => setAsking(null)}
                  />
                )}
              </li>
            ))}
          </ul>
        )}
        {historyMore && (
          <LoadMore busy={historyBusy} onMore={() => void older()} label={copy.more} />
        )}
      </Section>
    </div>
  );
}
