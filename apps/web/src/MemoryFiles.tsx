import { defaultCopy } from "@humagen/components";
import { Input } from "@humagen/render-web/ui/input";
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
/** The longest path a tree takes, in bytes, which is the server's own limit. */
const LONGEST_PATH = 512;

/**
 * Whether a typed path names a place inside a tree, by the rule the server
 * enforces: no leading slash, no backslash, and no empty, "." or ".." part.
 * The server checks it again.
 */
function insideTree(path: string): boolean {
  if (!path || path.startsWith("/") || /[\\\0]/.test(path)) return false;
  if (new TextEncoder().encode(path).length > LONGEST_PATH) return false;
  return path.split("/").every((part) => part !== "" && part !== "." && part !== "..");
}

/**
 * One memory tree as files: the tree at a version, the open file's text, the
 * proposals waiting on it and its versions. Someone who may write the tree
 * can add a text file, edit or delete one at the newest version, restore an
 * earlier version and decide proposals. A reader sees none of those controls,
 * and a refusal that arrives anyway says so and leaves the view as it was.
 * Beside an open file are the versions in which that file changed. Leaving
 * with a changed draft asks first.
 */
/** A file being edited: the person's text and the tree version it was read at. */
interface Draft {
  path: string;
  text: string;
  /** The file's text as it was read, which tells whether the draft changed it. */
  read: string;
  base: number;
  /** The tree does not hold the file yet. Saving creates it. */
  created?: boolean;
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
  onDraft,
}: MemoryTreeRef & {
  /** The open file's path, from the address. */
  file?: string;
  onFile?: (path: string) => void;
  /** Told whether a draft holds changes that leaving would lose. */
  onDraft?: (unsaved: boolean) => void;
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
  // The path being typed for a new file, or null while none is asked for.
  const [naming, setNaming] = useState<string | null>(null);
  const [nameError, setNameError] = useState<string | null>(null);
  const [removing, setRemoving] = useState(false);
  // The versions in which the open file changed.
  const [fileHistory, setFileHistory] = useState<DriveVersion[] | null>(null);
  const [fileHistoryError, setFileHistoryError] = useState(false);
  const [fileHistoryMore, setFileHistoryMore] = useState(false);
  const [fileHistoryBusy, setFileHistoryBusy] = useState(false);

  useEffect(() => setPicked(file ?? ""), [file]);

  // A draft belongs to one tree, so another tree starts without one.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the tree's name is the trigger.
  useEffect(() => {
    setDraft(null);
    setNaming(null);
    setNameError(null);
  }, [scope, project, agent]);

  // Leaving the page or the screen with a changed draft asks first, as the
  // workspace file editor does. Whoever shows this view asks through onDraft
  // before it switches away within the screen.
  const unsaved = draft !== null && draft.text !== draft.read;
  useEffect(() => {
    onDraft?.(unsaved);
    return () => onDraft?.(false);
  }, [unsaved, onDraft]);
  useEffect(() => {
    if (!unsaved) return;
    const preventLoss = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    const beforeNavigate = (event: Event) => {
      if (!window.confirm(defaultCopy.memoryFiles.leaveDraft)) event.preventDefault();
    };
    window.addEventListener("beforeunload", preventLoss);
    window.addEventListener("humagen:before-navigate", beforeNavigate);
    return () => {
      window.removeEventListener("beforeunload", preventLoss);
      window.removeEventListener("humagen:before-navigate", beforeNavigate);
    };
  }, [unsaved]);

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

  // The confirm to delete is about the open file, so it closes with it.
  // biome-ignore lint/correctness/useExhaustiveDependencies: the open file is the trigger.
  useEffect(() => setRemoving(false), [path, version]);

  // The versions in which the open file changed, read again after any change.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reload asks for a fresh read.
  useEffect(() => {
    let live = true;
    setFileHistory(null);
    setFileHistoryError(false);
    setFileHistoryMore(false);
    if (!path) return;
    api
      .memoryHistory({ scope, project, agent }, 0, HISTORY_PAGE, path)
      .then((rows) => {
        if (!live) return;
        setFileHistory(rows);
        setFileHistoryMore(rows.length === HISTORY_PAGE);
      })
      .catch(() => {
        if (live) setFileHistoryError(true);
      });
    return () => {
      live = false;
    };
  }, [scope, project, agent, path, reload]);

  /**
   * Runs a change and shows the tree it answers with, or says why it did not
   * happen. A conflict with its own words reads the tree again, so the newest
   * version is the one on view.
   */
  const change = async (run: () => Promise<Tree>, done?: string, conflict?: string) => {
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
      const status = err instanceof ApiError ? err.status : 0;
      if (status === 409 && conflict) setReload((n) => n + 1);
      setProblem(
        status === 403
          ? defaultCopy.noPermission
          : status === 409 && conflict
            ? conflict
            : copy.actionFailed,
      );
    } finally {
      setBusy(false);
    }
  };
  /** Deletes the open file against the version on view, once the person confirmed. */
  const remove = (gone: string, base: number) => {
    setRemoving(false);
    void change(
      () => api.deleteMemoryFile({ scope, project, agent }, gone, base),
      copy.deleted(gone),
      copy.deleteConflict,
    );
  };
  /** A draft of another file is replaced only once the person agrees to lose it. */
  const mayDropDraft = () => !unsaved || window.confirm(copy.leaveDraft);
  /** Opens an empty draft at the typed path, or says why the path will not do. */
  const startFile = () => {
    const next = (naming ?? "").trim();
    if (!insideTree(next)) return setNameError(copy.pathInvalid);
    if (entries.some((e) => e.path === next)) return setNameError(copy.pathTaken);
    if (!mayDropDraft()) return;
    setNaming(null);
    setNameError(null);
    setDraft({ path: next, text: "", read: "", base: tree?.version ?? 0, created: true });
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
      if (draft.created) {
        setPicked(draft.path);
        onFile?.(draft.path);
      }
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
      setDraft({
        path: draft.path,
        text: latest.content,
        read: latest.content,
        base: next.version,
      });
      // A file someone else created meanwhile is now one of the tree's own.
      if (draft.created) {
        setPicked(draft.path);
        onFile?.(draft.path);
      }
    } catch {
      setDraft({ ...draft, error: copy.latestFailed });
    } finally {
      setSaving(false);
    }
  };
  const olderOfFile = async () => {
    const last = fileHistory?.at(-1);
    if (!last || !path) return;
    setFileHistoryBusy(true);
    try {
      const rows = await api.memoryHistory(
        { scope, project, agent },
        last.version,
        HISTORY_PAGE,
        path,
      );
      setFileHistory((have) => [...(have ?? []), ...rows]);
      setFileHistoryMore(rows.length === HISTORY_PAGE);
    } catch {
      setFileHistoryError(true);
    } finally {
      setFileHistoryBusy(false);
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
  const writable = version === 0 && tree.can_write;
  const creating = writable && draft?.created ? draft : null;
  const editing = writable && draft?.path === path && !draft.created ? draft : null;
  /** The editor over a draft: of the open file, or of a file not yet created. */
  const editor = (d: Draft) => (
    <div className="flex min-w-0 flex-col gap-3">
      {d.created && (
        <p className="break-all text-sm text-muted-foreground">{copy.newDraft(d.path)}</p>
      )}
      {d.conflict && (
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
      <Problem message={d.error ?? null} />
      <CodeEditor
        path={d.path}
        value={d.text}
        readOnly={saving}
        saving={saving}
        maxBytes={LARGEST_SAVED}
        onChange={(text) => setDraft({ ...d, text })}
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
  );
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
      <Section
        title={copy.files}
        count={entries.length}
        actions={
          writable &&
          !creating &&
          naming === null && (
            <button type="button" className={button} disabled={busy} onClick={() => setNaming("")}>
              {copy.newFile}
            </button>
          )
        }
      >
        {writable && naming !== null && (
          <form
            className="flex flex-col gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              startFile();
            }}
          >
            <div className="flex flex-wrap items-center gap-2">
              <Input
                autoFocus
                className="min-w-0 flex-1"
                aria-label={copy.newPath}
                placeholder="notes/plan.md"
                value={naming}
                onChange={(event) => {
                  setNaming(event.target.value);
                  setNameError(null);
                }}
              />
              <button type="submit" className={button}>
                {copy.create}
              </button>
              <button
                type="button"
                className={button}
                onClick={() => {
                  setNaming(null);
                  setNameError(null);
                }}
              >
                {copy.cancel}
              </button>
            </div>
            <Problem message={nameError} />
          </form>
        )}
        {creating && editor(creating)}
        {entries.length === 0 ? (
          !creating && <Empty title={copy.empty} />
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
              {writable &&
                !editing &&
                (removing ? (
                  <Confirm
                    question={copy.deleteAsk(path)}
                    action={copy.delete}
                    onConfirm={() => remove(path, tree.version)}
                    onKeep={() => setRemoving(false)}
                  />
                ) : (
                  <button
                    type="button"
                    className={`${textAction} self-start`}
                    aria-label={`${copy.delete} ${path}`}
                    disabled={busy}
                    onClick={() => setRemoving(true)}
                  >
                    {copy.delete}
                  </button>
                ))}
              {editing ? (
                editor(editing)
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
                  {writable && (
                    <button
                      type="button"
                      className={`${button} self-start`}
                      disabled={busy}
                      onClick={() =>
                        mayDropDraft() &&
                        setDraft({
                          path,
                          text: content.content,
                          read: content.content,
                          base: tree.version,
                        })
                      }
                    >
                      {copy.edit}
                    </button>
                  )}
                  <pre className="whitespace-pre-wrap break-words font-mono text-sm leading-6">
                    {content.content}
                  </pre>
                </>
              )}
              <div className="flex min-w-0 flex-col gap-2 border-t border-border pt-4">
                <h3 className="text-sm font-medium">{copy.fileHistory}</h3>
                {fileHistoryError && <Problem message={copy.fileHistoryFailed} />}
                {fileHistory === null ? (
                  !fileHistoryError && <SkeletonRows count={2} />
                ) : (
                  <ul className="divide-y divide-border">
                    {fileHistory.map((v) => {
                      // The version on view: the newest when none was chosen.
                      const shown = v.version === (version || tree.version);
                      const held = (v.entries?.length ?? 0) > 0;
                      return (
                        <li
                          key={v.version}
                          className="flex flex-wrap items-center gap-x-4 gap-y-1 py-2 text-sm"
                        >
                          <span aria-current={shown ? "true" : undefined}>
                            {copy.version(v.version)}
                          </span>
                          <span className="flex min-w-0 flex-1 flex-wrap gap-x-3 text-xs text-muted-foreground">
                            {!held && <span>{copy.removed}</span>}
                            {v.by && <span>{v.by}</span>}
                            {v.at && <time dateTime={v.at}>{ago(v.at)}</time>}
                          </span>
                          {held && !shown && (
                            <button
                              type="button"
                              className={textAction}
                              aria-label={`${copy.view} ${copy.fileVersion(path, v.version)}`}
                              onClick={() => setVersion(v.version === newest ? 0 : v.version)}
                            >
                              {copy.view}
                            </button>
                          )}
                        </li>
                      );
                    })}
                  </ul>
                )}
                {fileHistoryMore && (
                  <LoadMore
                    busy={fileHistoryBusy}
                    onMore={() => void olderOfFile()}
                    label={copy.more}
                  />
                )}
              </div>
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
