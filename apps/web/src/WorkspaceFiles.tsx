import { defaultCopy } from "@humagen/components";
import { Markdown } from "@humagen/primitives";
import {
  CodeBlock,
  CodeIcon,
  DiffIcon,
  DownloadIcon,
  FileIcon,
  MarkdownHeadingContext,
  PlusIcon,
  RefreshIcon,
  SaveIcon,
  StickyNoteIcon,
  TrashIcon,
  UploadIcon,
  WebSurface,
  XIcon,
} from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { ChangeReview, CodeEditor, FileBrowser, Inspector } from "@humagen/web";
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { ApiError, api } from "./api.ts";
import type { ConversationView } from "./route.ts";
import {
  MAX_WORKSPACE_FILE_BYTES,
  staticWorkspaceHTML,
  validWorkspacePath,
  type WorkspaceFileContent,
  type WorkspaceFileList,
  workspaceFiles,
} from "./workspace-files.ts";
import { workspaceReviewPrompt } from "./workspace-review.ts";

const words = defaultCopy.workspaceFiles;
type Draft = WorkspaceFileContent & {
  base: string;
  conflict?: boolean;
  latest?: WorkspaceFileContent;
};
const changed = (draft: Draft) => draft.content !== draft.base || !draft.revision;
const asDraft = (file: WorkspaceFileContent): Draft => ({ ...file, base: file.content });

function Preview({ path, content }: { path: string; content: string }) {
  if (content.length > 512 * 1024)
    return <p className="p-3 text-aui-xs text-aui-muted">{words.previewLarge}</p>;
  if (/\.html?$/i.test(path))
    return (
      <div className="flex min-w-0 flex-col gap-2 p-3">
        <p className="text-aui-xs text-aui-muted">{words.staticHTML}</p>
        <iframe
          title={`${words.preview}: ${path}`}
          sandbox=""
          referrerPolicy="no-referrer"
          srcDoc={staticWorkspaceHTML(content)}
          className="min-h-96 w-full rounded-aui border border-aui-border bg-white"
        />
      </div>
    );
  return (
    <WebSurface className="min-w-0 overflow-hidden p-3">
      {/\.md(?:own|arkdown)?$/i.test(path) ? (
        <MarkdownHeadingContext.Provider value={2}>
          <Markdown text={content} />
        </MarkdownHeadingContext.Provider>
      ) : (
        <CodeBlock text={content} title={path} />
      )}
    </WebSurface>
  );
}

/** The host owns saved bytes and identity; the editor and review only emit actions. */
export function WorkspaceFiles({
  thread,
  open,
  onClose,
  view,
  onView,
  onRequest,
  onPublished,
}: {
  thread: string;
  open: boolean;
  onClose: () => void;
  view: ConversationView;
  onView: (view: ConversationView) => void;
  onRequest?: (text: string) => void;
  onPublished?: (id: string) => void;
}) {
  const [listing, setListing] = useState<WorkspaceFileList | null>(null);
  const [tabs, setTabs] = useState<Record<string, Draft>>({});
  const [selected, setSelected] = useState(view.file ?? "");
  const [filter, setFilter] = useState("");
  const [newPath, setNewPath] = useState("");
  const [creating, setCreating] = useState(false);
  const [loading, setLoading] = useState(false);
  const [loadingPath, setLoadingPath] = useState("");
  const [busy, setBusy] = useState("");
  const [problem, setProblem] = useState("");
  const [note, setNote] = useState("");
  const [canPublish, setCanPublish] = useState(false);
  const [closing, setClosing] = useState("");
  const [deleting, setDeleting] = useState(false);
  const [published, setPublished] = useState("");
  const upload = useRef<HTMLInputElement>(null);
  const pathId = useId();
  const refreshId = useRef(0);
  const [readAttempt, setReadAttempt] = useState(0);
  const live = useRef(true);
  const drafts = useRef(tabs);
  drafts.current = tabs;
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const path = view.file ?? selected;
  const draft = tabs[path];
  const mode = view.workspaceTab ?? "edit";
  const writable = listing?.writable === true;
  const dirty = draft ? changed(draft) : false;
  const failed = useCallback((error: unknown) => {
    if (live.current) setProblem(error instanceof Error ? error.message : words.failed);
  }, []);
  const choose = (next: string) => {
    setSelected(next);
    setDeleting(false);
    setClosing("");
    setPublished("");
    setProblem("");
    setNote("");
    onView({ ...view, panel: "workspace", file: next || undefined });
  };
  const changeMode = (next: "edit" | "preview" | "changes") =>
    onView({ ...view, panel: "workspace", file: path || undefined, workspaceTab: next });
  const refresh = useCallback(async () => {
    const attempt = ++refreshId.current;
    setLoading(true);
    setProblem("");
    try {
      const value = await workspaceFiles.list(thread);
      if (!live.current || attempt !== refreshId.current) return;
      setListing(value);
      setTabs((previous) => {
        const next = { ...previous };
        for (const [name, file] of Object.entries(previous)) {
          const current = value.files.find((item) => item.path === name);
          if (file.revision && current?.revision !== file.revision)
            next[name] = { ...file, conflict: true, latest: undefined };
        }
        return next;
      });
    } catch (error) {
      failed(error);
    } finally {
      if (live.current && attempt === refreshId.current) setLoading(false);
    }
  }, [thread, failed]);
  useEffect(() => {
    if (!open) return;
    void refresh();
    api
      .me()
      .then((me) => {
        if (live.current) setCanPublish(me.permissions.includes("artifacts:write"));
      })
      .catch(() => {
        if (live.current) setCanPublish(false);
      });
  }, [open, refresh]);
  useEffect(() => {
    // An explicit retry changes readAttempt without replacing an existing draft.
    void readAttempt;
    if (!open || !path || drafts.current[path]) return;
    let current = true;
    setLoadingPath(path);
    setProblem("");
    workspaceFiles
      .read(thread, path)
      .then((file) => {
        if (current)
          setTabs((previous) =>
            previous[path] ? previous : { ...previous, [path]: asDraft(file) },
          );
      })
      .catch((error) => {
        if (current) failed(error);
      })
      .finally(() => {
        if (current) setLoadingPath("");
      });
    return () => {
      current = false;
    };
  }, [open, thread, path, readAttempt, failed]);
  const hasDrafts = Object.values(tabs).some(changed);
  useEffect(() => {
    if (!hasDrafts) return;
    const preventLoss = (event: BeforeUnloadEvent) => {
      event.preventDefault();
      event.returnValue = "";
    };
    const beforeNavigate = (event: Event) => {
      if (!window.confirm(words.leaveDrafts)) event.preventDefault();
    };
    window.addEventListener("beforeunload", preventLoss);
    window.addEventListener("humagen:before-navigate", beforeNavigate);
    return () => {
      window.removeEventListener("beforeunload", preventLoss);
      window.removeEventListener("humagen:before-navigate", beforeNavigate);
    };
  }, [hasDrafts]);
  const updateDraft = (name: string, change: (file: Draft) => Draft) =>
    setTabs((previous) =>
      previous[name] ? { ...previous, [name]: change(previous[name]) } : previous,
    );
  const save = async () => {
    if (!draft || !writable || busy || draft.conflict) return;
    const snapshot = draft;
    const name = path;
    setBusy("save");
    setProblem("");
    setNote("");
    try {
      const file = await workspaceFiles.write(thread, {
        path: name,
        content: snapshot.content,
        revision: snapshot.revision,
      });
      if (!live.current) return;
      updateDraft(name, (current) => ({
        ...asDraft(file),
        content: current.content === snapshot.content ? file.content : current.content,
      }));
      setNote(words.saved);
      await refresh();
    } catch (error) {
      if (error instanceof ApiError && error.status === 409) {
        // An agent can start after the initial listing. A busy refusal does
        // not imply that the file's revision changed.
        try {
          const current = await workspaceFiles.list(thread);
          if (!live.current) return;
          setListing(current);
          if (!current.writable) {
            setProblem(error.message);
            return;
          }
        } catch {
          // Keep the draft and the conservative conflict recovery below.
        }
        updateDraft(name, (current) => ({ ...current, conflict: true }));
        setProblem(words.conflict);
      } else failed(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const latest = async (discard: boolean) => {
    if (!draft || busy) return;
    const name = path;
    setBusy("latest");
    setProblem("");
    try {
      const file = await workspaceFiles.read(thread, name);
      if (!live.current) return;
      if (discard) updateDraft(name, () => asDraft(file));
      else {
        updateDraft(name, (current) => ({ ...current, latest: file, conflict: true }));
        changeMode("changes");
      }
    } catch (error) {
      if (error instanceof ApiError && error.status === 404) setProblem(words.latestMissing);
      else failed(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const createDraft = (name: string, content = "") => {
    if (!validWorkspacePath(name)) {
      setProblem(words.invalidPath);
      return;
    }
    if (tabs[name] || listing?.files.some((file) => file.path === name)) {
      setProblem(words.duplicate);
      return;
    }
    setTabs((previous) => ({
      ...previous,
      [name]: { path: name, content, base: "", revision: "" },
    }));
    setCreating(false);
    setNewPath("");
    choose(name);
  };
  const scratchpad = () => {
    const paths = new Set([
      ...Object.keys(tabs),
      ...(listing?.files.map((file) => file.path) ?? []),
    ]);
    let name = "scratchpads/notes.md";
    for (let i = 2; paths.has(name); i++) name = `scratchpads/notes-${i}.md`;
    createDraft(name, "# Scratchpad\n\n");
  };
  const importFile = async (file: File | undefined) => {
    if (!file) return;
    setProblem("");
    try {
      if (file.size > MAX_WORKSPACE_FILE_BYTES) throw new Error(words.tooLarge);
      const content = new TextDecoder("utf-8", { fatal: true }).decode(await file.arrayBuffer());
      if (content.includes("\0")) throw new Error(words.tooLarge);
      const parts = file.name.match(/^(.*?)(\.[^.]*)?$/);
      const base = parts?.[1] || "imported";
      const extension = parts?.[2] ?? "";
      let name = file.name;
      for (let i = 2; tabs[name] || listing?.files.some((item) => item.path === name); i++)
        name = `${base}-${i}${extension}`;
      createDraft(name, content);
    } catch {
      setProblem(words.tooLarge);
    }
    if (upload.current) upload.current.value = "";
  };
  const closeTab = (name: string, discard = false) => {
    const value = tabs[name];
    if (value && changed(value) && !discard) {
      setClosing(name);
      return;
    }
    const remaining = Object.keys(tabs).filter((item) => item !== name);
    setTabs((previous) => {
      const next = { ...previous };
      delete next[name];
      return next;
    });
    setClosing("");
    if (path === name) choose(remaining.at(-1) ?? "");
  };
  const download = () => {
    if (!draft) return;
    const url = URL.createObjectURL(
      new Blob([draft.content], { type: "text/plain;charset=utf-8" }),
    );
    const link = document.createElement("a");
    link.href = url;
    link.download = path.split("/").at(-1) || "file.txt";
    link.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  };
  const publish = async () => {
    if (!draft || dirty || busy || draft.conflict || !canPublish || !writable) return;
    setBusy("publish");
    setProblem("");
    try {
      const artifact = await workspaceFiles.publish(thread, path, draft.revision);
      if (!live.current) return;
      setPublished(artifact.id);
      setNote(words.published(artifact.version));
    } catch (error) {
      failed(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const remove = async () => {
    if (!draft || busy || !writable) return;
    setBusy("delete");
    setProblem("");
    try {
      await workspaceFiles.remove(thread, path, draft.revision);
      if (!live.current) return;
      closeTab(path, true);
      setNote(words.deleted);
      setDeleting(false);
      await refresh();
    } catch (error) {
      failed(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const paths = [
    ...new Set([...(listing?.files.map((file) => file.path) ?? []), ...Object.keys(tabs)]),
  ]
    .filter((name) => name.toLowerCase().includes(filter.toLowerCase()))
    .sort();
  const labels = Object.fromEntries(
    Object.entries(tabs)
      .filter(([, file]) => changed(file))
      .map(([name]) => [name, `${name.split("/").at(-1)} (${words.dirty})`]),
  );
  return (
    <Inspector open={open} title={words.title} onClose={onClose} wide>
      <div className="flex min-w-0 flex-col gap-3" data-slot="workspace-files">
        <div className="flex flex-wrap gap-1.5">
          <Button
            variant="outline"
            size="sm"
            disabled={!writable || !!busy}
            onClick={() => setCreating((value) => !value)}
          >
            <PlusIcon size={15} />
            {words.newFile}
          </Button>
          <Button variant="ghost" size="sm" disabled={!writable || !!busy} onClick={scratchpad}>
            <StickyNoteIcon size={15} />
            {words.scratchpad}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={!writable || !!busy}
            onClick={() => upload.current?.click()}
          >
            <UploadIcon size={15} />
            {words.import}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            disabled={loading || !!busy}
            onClick={() => void refresh()}
          >
            <RefreshIcon size={15} />
            {words.refresh}
          </Button>
          <input
            ref={upload}
            type="file"
            className="sr-only"
            aria-label={words.import}
            accept="text/*,.diff,.patch,.ts,.tsx,.js,.jsx,.json,.yaml,.yml,.go,.py,.rs,.css,.html,.md,.toml,.sql,.sh"
            onChange={(event) => void importFile(event.currentTarget.files?.[0])}
          />
        </div>
        {creating && (
          <form
            className="flex flex-wrap items-end gap-2 rounded-aui border border-aui-border p-3"
            onSubmit={(event) => {
              event.preventDefault();
              createDraft(newPath.trim());
            }}
          >
            <label htmlFor={pathId} className="min-w-0 flex-1 text-aui-xs">
              {words.path}
              <Input
                id={pathId}
                value={newPath}
                onChange={(event) => setNewPath(event.target.value)}
                placeholder={words.pathHint}
                className="mt-1"
              />
            </label>
            <Button type="submit" size="sm" disabled={!writable || !newPath.trim()}>
              {words.create}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setCreating(false)}>
              {words.cancel}
            </Button>
          </form>
        )}
        <p className="text-aui-xs leading-relaxed text-aui-muted">
          {words.retention(listing?.retention_days ?? 7)}
        </p>
        {listing && !writable && (
          <output className="rounded-aui border border-aui-border p-3 text-aui-xs">
            {listing.reason || words.readOnly}
          </output>
        )}
        {problem && (
          <div role="alert" className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-bad">
            {problem}
            {
              <Button
                variant="outline"
                size="sm"
                onClick={() => {
                  void refresh();
                  setReadAttempt((value) => value + 1);
                }}
              >
                {words.retry}
              </Button>
            }
          </div>
        )}
        {note && (
          <output className="text-aui-xs text-aui-muted">
            {note}
            {published && onPublished && (
              <Button variant="link" size="sm" onClick={() => onPublished(published)}>
                {words.openPublished}
              </Button>
            )}
          </output>
        )}
        {loading && !listing && (
          <output className="text-aui-xs text-aui-muted">{words.loading}</output>
        )}
        {Object.keys(tabs).length > 0 && (
          <nav aria-label={words.openFiles} className="flex max-w-full flex-wrap gap-1">
            {Object.entries(tabs).map(([name, file]) => (
              <span
                key={name}
                className={`inline-flex min-w-0 max-w-full items-center rounded-aui border ${path === name ? "border-aui-accent bg-aui-hover" : "border-aui-border"}`}
              >
                <button
                  type="button"
                  className="inline-flex min-h-9 min-w-0 items-center gap-1.5 px-2 text-aui-xs focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11"
                  aria-current={path === name ? "page" : undefined}
                  title={name}
                  onClick={() => choose(name)}
                >
                  <FileIcon size={13} />
                  <span className="max-w-40 truncate">{name.split("/").at(-1)}</span>
                  {changed(file) && <span className="text-aui-muted">{words.dirty}</span>}
                </button>
                <Button
                  variant="ghost"
                  size="icon-sm"
                  aria-label={words.closeFile(name)}
                  disabled={!!busy}
                  onClick={() => closeTab(name)}
                >
                  <XIcon size={13} />
                </Button>
              </span>
            ))}
          </nav>
        )}
        {closing && (
          <div
            role="alert"
            className="flex flex-wrap items-center gap-2 rounded-aui border border-aui-border p-3 text-aui-xs"
          >
            <p>{words.closeDirty}</p>
            <Button variant="outline" size="sm" onClick={() => closeTab(closing, true)}>
              {words.discardClose}
            </Button>
            <Button variant="ghost" size="sm" onClick={() => setClosing("")}>
              {words.cancel}
            </Button>
          </div>
        )}
        <Input
          aria-label={words.filter}
          placeholder={words.filter}
          value={filter}
          onChange={(event) => setFilter(event.target.value)}
        />
        {listing && paths.length === 0 && (
          <p className="py-3 text-aui-xs text-aui-muted">
            {filter ? words.noMatches : words.empty}
          </p>
        )}
        {(paths.length > 0 || draft) && (
          <FileBrowser
            paths={paths}
            selected={path}
            labels={labels}
            onSelect={choose}
            label={words.files}
          >
            {loadingPath && loadingPath === path ? (
              <output className="p-3 text-aui-xs text-aui-muted">{words.loading}</output>
            ) : draft ? (
              <div className="flex min-w-0 flex-col">
                <div className="flex flex-wrap items-center justify-between gap-1 border-b border-aui-border p-2">
                  <fieldset aria-label={words.modes} className="flex flex-wrap gap-1">
                    {(["edit", "preview", "changes"] as const).map((item) => (
                      <Button
                        key={item}
                        variant={mode === item ? "secondary" : "ghost"}
                        size="sm"
                        aria-pressed={mode === item}
                        onClick={() => changeMode(item)}
                      >
                        {item === "changes" ? (
                          <DiffIcon size={14} />
                        ) : item === "edit" ? (
                          <CodeIcon size={14} />
                        ) : null}
                        {words[item]}
                      </Button>
                    ))}
                  </fieldset>
                  <Button
                    size="sm"
                    disabled={!dirty || !writable || !!busy || draft.conflict}
                    onClick={() => void save()}
                  >
                    <SaveIcon size={14} />
                    {busy === "save" ? words.saving : words.save}
                    <kbd className="hidden text-[10px] opacity-60 sm:inline">⌘/Ctrl S</kbd>
                  </Button>
                </div>
                {draft.conflict && (
                  <div className="flex flex-wrap items-center gap-2 border-b border-aui-border p-3 text-aui-xs">
                    <p>{words.conflict}</p>
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!!busy}
                      onClick={() => void latest(false)}
                    >
                      {words.compareLatest}
                    </Button>
                    {draft.latest && (
                      <Button
                        variant="outline"
                        size="sm"
                        disabled={!!busy}
                        onClick={() => {
                          updateDraft(path, (current) =>
                            current.latest
                              ? {
                                  ...current,
                                  base: current.latest.content,
                                  revision: current.latest.revision,
                                  latest: undefined,
                                  conflict: false,
                                }
                              : current,
                          );
                          setNote(words.baseUpdated);
                          setProblem("");
                        }}
                      >
                        {words.useLatestBase}
                      </Button>
                    )}
                  </div>
                )}
                {mode === "edit" ? (
                  <CodeEditor
                    path={path}
                    value={draft.content}
                    readOnly={!writable || !!busy}
                    saving={busy === "save"}
                    onChange={(content) =>
                      updateDraft(path, (current) => ({ ...current, content }))
                    }
                    onSave={() => void save()}
                    onAskSelection={
                      onRequest
                        ? ({ text, startLine, endLine }) =>
                            onRequest(words.selectionRequest(path, startLine, endLine, text))
                        : undefined
                    }
                  />
                ) : mode === "preview" ? (
                  <Preview path={path} content={draft.content} />
                ) : (
                  <div className="min-w-0 p-2">
                    <ChangeReview
                      key={path}
                      {...(/\.(diff|patch)$/i.test(path)
                        ? { patch: draft.content }
                        : {
                            before: draft.latest?.content ?? draft.base,
                            after: draft.content,
                            path,
                          })}
                      onComment={
                        onRequest
                          ? (comment) =>
                              onRequest(
                                workspaceReviewPrompt(
                                  { ...draft, base: draft.latest?.content ?? draft.base },
                                  comment,
                                ),
                              )
                          : undefined
                      }
                    />
                    {onRequest && (
                      <p className="mt-3 text-aui-xs text-aui-muted">{words.reviewHint}</p>
                    )}
                  </div>
                )}
                <div className="flex flex-wrap gap-1 border-t border-aui-border p-2">
                  <Button variant="ghost" size="sm" onClick={download}>
                    <DownloadIcon size={14} />
                    {words.download}
                  </Button>
                  {onRequest && (
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={!draft.revision || dirty || draft.conflict}
                      onClick={() => onRequest(words.request(path))}
                    >
                      {words.ask}
                    </Button>
                  )}
                  <Button
                    variant="ghost"
                    size="sm"
                    disabled={!dirty || !!busy}
                    onClick={() => (draft.revision ? void latest(true) : closeTab(path, true))}
                  >
                    {words.discard}
                  </Button>
                  {canPublish && (
                    <Button
                      variant="outline"
                      size="sm"
                      disabled={!writable || dirty || !!busy || draft.conflict}
                      title={words.publishHint}
                      onClick={() => void publish()}
                    >
                      {words.publish}
                    </Button>
                  )}
                  {draft.revision && writable && (
                    <Button
                      variant="ghost"
                      size="sm"
                      disabled={!!busy}
                      onClick={() => setDeleting(true)}
                    >
                      <TrashIcon size={14} />
                      {words.delete}
                    </Button>
                  )}
                </div>
                {deleting && (
                  <div
                    role="alert"
                    className="flex flex-wrap items-center gap-2 border-t border-aui-border p-3 text-aui-xs"
                  >
                    <p>{words.deleteConfirm(path)}</p>
                    <Button
                      variant="destructive"
                      size="sm"
                      disabled={!!busy}
                      onClick={() => void remove()}
                    >
                      {words.delete}
                    </Button>
                    <Button variant="ghost" size="sm" onClick={() => setDeleting(false)}>
                      {words.cancel}
                    </Button>
                  </div>
                )}
              </div>
            ) : (
              <p className="p-3 text-aui-xs text-aui-muted">{words.select}</p>
            )}
          </FileBrowser>
        )}
        {hasDrafts && <p className="text-aui-xs text-aui-muted">{words.draftHint}</p>}
      </div>
    </Inspector>
  );
}
