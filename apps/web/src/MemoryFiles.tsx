import { defaultCopy } from "@humagen/components";
import { FileBrowser } from "@humagen/web";
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
/** How many versions one read of the history asks for. */
const HISTORY_PAGE = 20;

/**
 * One memory tree as files: the tree at a version, the open file's text, the
 * proposals waiting on it and its versions. Nothing here edits a file. The
 * server does not say who may restore or decide, so those controls are shown
 * to every reader, and a refusal says so and leaves the view as it was.
 */
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
              {tooLarge ? (
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
                <pre className="whitespace-pre-wrap break-words font-mono text-sm leading-6">
                  {content.content}
                </pre>
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
                      <button
                        type="button"
                        className={textAction}
                        aria-label={`${copy.restore} ${copy.version(v.version)}`}
                        disabled={busy}
                        onClick={() => setAsking(v.version)}
                      >
                        {copy.restore}
                      </button>
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
