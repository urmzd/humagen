import { defaultCopy } from "@humagen/components";
import { Markdown } from "@humagen/primitives";
import {
  ChevronRightIcon,
  CodeIcon,
  MarkdownHeadingContext,
  PencilIcon,
  PlusIcon,
  SaveIcon,
  StickyNoteIcon,
  TrashIcon,
  WebSurface,
} from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { CodeEditor } from "@humagen/web";
import { type ReactNode, useCallback, useEffect, useId, useRef, useState } from "react";
import { ApiError } from "./api.ts";
import { ago, SkeletonRows } from "./ui.tsx";
import { type Scratchpad, type ScratchpadList, scratchpadApi } from "./workspace-files.ts";

const words = defaultCopy.workspace.scratchpads;
export const MAX_SCRATCHPAD_BYTES = 256 * 1024;

export interface PadDraft {
  saved: Scratchpad;
  content: string;
  conflict?: boolean;
}
export type PadDrafts = Record<string, PadDraft>;
const dirty = (d?: PadDraft) => !!d && d.content !== d.saved.content;

/**
 * A conversation's scratchpad drafts, held by the Workspace so they survive
 * switching sections and closing the panel. Leaving the conversation with an
 * unsaved draft asks first.
 */
export function usePadDrafts() {
  const [drafts, setDrafts] = useState<PadDrafts>({});
  const hasDrafts = Object.values(drafts).some(dirty);
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
  return [drafts, setDrafts] as const;
}

const row =
  "flex min-h-11 w-full min-w-0 items-center gap-2 rounded-aui px-2 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent";

function Alert({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="flex flex-wrap items-center gap-2 text-aui-xs text-aui-bad">
      {children}
    </div>
  );
}

/** Create, name, rename, delete and edit a conversation's scratchpads. */
export function Scratchpads({
  thread,
  open,
  selected,
  onSelect,
  drafts,
  setDrafts,
  actions,
  onName,
}: {
  thread: string;
  open: boolean;
  selected?: string;
  onSelect: (id?: string, label?: string) => void;
  drafts: PadDrafts;
  setDrafts: (change: (previous: PadDrafts) => PadDrafts) => void;
  /** More actions for the open scratchpad, such as Save as Artifact. */
  actions?: (pad: Scratchpad, dirty: boolean) => ReactNode;
  /** Says the open scratchpad's name, for its tab. */
  onName?: (id: string, name: string) => void;
}) {
  const [list, setList] = useState<ScratchpadList>();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [problem, setProblem] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState("");
  const [mode, setMode] = useState<"edit" | "preview">("edit");
  const [renaming, setRenaming] = useState<string>();
  const [deleting, setDeleting] = useState(false);
  const nameId = useId();
  const renameId = useId();
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  const refresh = useCallback(async () => {
    try {
      const next = await scratchpadApi.list(thread);
      if (live.current) {
        setList(next);
        setFailed(false);
      }
    } catch {
      if (live.current) setFailed(true);
    }
  }, [thread]);
  useEffect(() => {
    void attempt;
    if (open) void refresh();
  }, [open, refresh, attempt]);
  const draft = selected ? drafts[selected] : undefined;
  const shownName = draft?.saved.name;
  useEffect(() => {
    if (selected && shownName) onName?.(selected, shownName);
  }, [selected, shownName, onName]);
  useEffect(() => {
    if (!open || !selected || drafts[selected]) return;
    let current = true;
    scratchpadApi.read(thread, selected).then(
      (pad) => {
        if (current)
          setDrafts((previous) =>
            previous[selected]
              ? previous
              : { ...previous, [selected]: { saved: pad, content: pad.content } },
          );
      },
      (error) => {
        if (current) setProblem(error instanceof Error ? error.message : words.failed);
      },
    );
    return () => {
      current = false;
    };
  }, [open, thread, selected, drafts, setDrafts]);
  const fail = (error: unknown) => {
    if (!live.current) return;
    if (error instanceof ApiError && error.status === 409) setProblem(words.conflict);
    else setProblem(error instanceof Error ? error.message : words.failed);
  };
  const writable = list?.writable === true;
  const patch = (id: string, change: (current: PadDraft) => PadDraft) =>
    setDrafts((previous) => {
      const current = previous[id];
      return current ? { ...previous, [id]: change(current) } : previous;
    });
  const create = async () => {
    const value = name.trim();
    if (!value || busy) return;
    setBusy("create");
    setProblem("");
    try {
      const pad = await scratchpadApi.create(thread, value, `# ${value}\n\n`);
      if (!live.current) return;
      setDrafts((previous) => ({ ...previous, [pad.id]: { saved: pad, content: pad.content } }));
      setCreating(false);
      setName("");
      await refresh();
      onSelect(pad.id, pad.name);
    } catch (error) {
      fail(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const write = async (pad: Scratchpad, next: { name: string; content: string }, label: string) => {
    setBusy(label);
    setProblem("");
    setNote("");
    try {
      const saved = await scratchpadApi.update(thread, pad.id, { ...next, version: pad.version });
      if (!live.current) return;
      setDrafts((previous) => {
        const current = previous[pad.id];
        return {
          ...previous,
          [pad.id]: {
            saved,
            // Keep typing done while the save was in flight.
            content: current && current.content !== next.content ? current.content : saved.content,
          },
        };
      });
      setNote(words.saved);
      setRenaming(undefined);
      await refresh();
    } catch (error) {
      if (error instanceof ApiError && error.status === 409)
        patch(pad.id, (current) => ({ ...current, conflict: true }));
      fail(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const reload = async (id: string) => {
    setBusy("reload");
    setProblem("");
    try {
      const pad = await scratchpadApi.read(thread, id);
      if (live.current)
        setDrafts((previous) => ({ ...previous, [id]: { saved: pad, content: pad.content } }));
    } catch (error) {
      fail(error);
    } finally {
      if (live.current) setBusy("");
    }
  };
  const remove = async (pad: Scratchpad) => {
    setBusy("delete");
    setProblem("");
    try {
      await scratchpadApi.remove(thread, pad.id, pad.version);
      if (!live.current) return;
      setDrafts((previous) => {
        const next = { ...previous };
        delete next[pad.id];
        return next;
      });
      setDeleting(false);
      setNote(words.deleted);
      await refresh();
      onSelect();
    } catch (error) {
      fail(error);
    } finally {
      if (live.current) setBusy("");
    }
  };

  if (failed)
    return (
      <Alert>
        {words.failed}
        <Button variant="outline" size="sm" onClick={() => setAttempt((n) => n + 1)}>
          {words.retry}
        </Button>
      </Alert>
    );
  if (!list) return <SkeletonRows count={3} />;

  if (selected) {
    if (!draft)
      return problem ? (
        <div className="flex flex-col gap-2">
          <Button variant="ghost" size="sm" className="self-start" onClick={() => onSelect()}>
            {words.back}
          </Button>
          <Alert>{problem}</Alert>
        </div>
      ) : (
        <SkeletonRows count={4} />
      );
    const pad = draft.saved;
    const changed = dirty(draft);
    return (
      <div className="flex min-w-0 flex-col gap-3" data-slot="workspace-scratchpad">
        <div className="flex flex-wrap items-center justify-between gap-1">
          <Button variant="ghost" size="sm" onClick={() => onSelect()}>
            {words.back}
          </Button>
          <span className="text-aui-xs text-aui-muted">{words.agentPath(pad.name)}</span>
        </div>
        {renaming !== undefined ? (
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(event) => {
              event.preventDefault();
              if (renaming.trim())
                void write(pad, { name: renaming.trim(), content: pad.content }, "rename");
            }}
          >
            <label htmlFor={renameId} className="min-w-0 flex-1 text-aui-xs">
              {words.renameTo}
              <Input
                id={renameId}
                value={renaming}
                maxLength={80}
                onChange={(event) => setRenaming(event.target.value)}
                className="mt-1"
              />
            </label>
            <Button type="submit" size="sm" disabled={!!busy || !renaming.trim()}>
              {words.rename}
            </Button>
            <Button type="button" variant="ghost" size="sm" onClick={() => setRenaming(undefined)}>
              {words.cancel}
            </Button>
          </form>
        ) : (
          <h3 className="inline-flex min-w-0 items-center gap-2 text-aui-sm font-medium">
            <StickyNoteIcon size={16} />
            <span className="truncate">{pad.name}</span>
            {changed && (
              <span className="text-aui-xs font-normal text-aui-muted">{words.unsaved}</span>
            )}
          </h3>
        )}
        {problem && (
          <Alert>
            {problem}
            {draft.conflict && (
              <Button
                variant="outline"
                size="sm"
                disabled={!!busy}
                onClick={() => void reload(pad.id)}
              >
                {words.reload}
              </Button>
            )}
          </Alert>
        )}
        {note && <output className="text-aui-xs text-aui-muted">{note}</output>}
        <div className="flex min-w-0 flex-col rounded-aui border border-aui-border">
          <div className="flex flex-wrap items-center justify-between gap-1 border-b border-aui-border p-2">
            <fieldset aria-label={words.edit} className="flex gap-1">
              {(["edit", "preview"] as const).map((item) => (
                <Button
                  key={item}
                  variant={mode === item ? "secondary" : "ghost"}
                  size="sm"
                  aria-pressed={mode === item}
                  onClick={() => setMode(item)}
                >
                  {item === "edit" && <CodeIcon size={14} />}
                  {words[item]}
                </Button>
              ))}
            </fieldset>
            <Button
              size="sm"
              disabled={!changed || !writable || !!busy || draft.conflict}
              onClick={() => void write(pad, { name: pad.name, content: draft.content }, "save")}
            >
              <SaveIcon size={14} />
              {busy === "save" ? words.saving : words.save}
            </Button>
          </div>
          {mode === "edit" ? (
            <CodeEditor
              path={`${pad.name}.md`}
              value={draft.content}
              readOnly={!writable || !!busy}
              saving={busy === "save"}
              maxBytes={MAX_SCRATCHPAD_BYTES}
              onChange={(content) => patch(pad.id, (current) => ({ ...current, content }))}
              onSave={() => {
                if (changed && writable && !busy && !draft.conflict)
                  void write(pad, { name: pad.name, content: draft.content }, "save");
              }}
            />
          ) : (
            <WebSurface className="min-w-0 overflow-hidden p-3">
              <MarkdownHeadingContext.Provider value={2}>
                <Markdown text={draft.content} />
              </MarkdownHeadingContext.Provider>
            </WebSurface>
          )}
          <div className="flex flex-wrap gap-1 border-t border-aui-border p-2">
            {writable && renaming === undefined && (
              <Button
                variant="ghost"
                size="sm"
                disabled={!!busy}
                onClick={() => setRenaming(pad.name)}
              >
                <PencilIcon size={14} />
                {words.rename}
              </Button>
            )}
            {actions?.(pad, changed)}
            {writable && (
              <Button variant="ghost" size="sm" disabled={!!busy} onClick={() => setDeleting(true)}>
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
              <p>{words.deleteConfirm(pad.name)}</p>
              <Button
                variant="destructive"
                size="sm"
                disabled={!!busy}
                onClick={() => void remove(pad)}
              >
                {words.delete}
              </Button>
              <Button variant="ghost" size="sm" onClick={() => setDeleting(false)}>
                {words.cancel}
              </Button>
            </div>
          )}
        </div>
      </div>
    );
  }

  return (
    <div className="flex min-w-0 flex-col gap-3" data-slot="workspace-scratchpads">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={!writable || list.scratchpads.length >= list.limit}
          onClick={() => setCreating((value) => !value)}
        >
          <PlusIcon size={15} />
          {words.new}
        </Button>
        <span className="text-aui-xs text-aui-muted tabular-nums">
          {words.limit(list.scratchpads.length, list.limit)}
        </span>
      </div>
      <p className="text-aui-xs text-aui-muted">{words.hint}</p>
      {!writable && (
        <output className="rounded-aui border border-aui-border p-3 text-aui-xs">
          {list.reason || words.readOnly}
        </output>
      )}
      {creating && (
        <form
          className="flex flex-wrap items-end gap-2 rounded-aui border border-aui-border p-3"
          onSubmit={(event) => {
            event.preventDefault();
            void create();
          }}
        >
          <label htmlFor={nameId} className="min-w-0 flex-1 text-aui-xs">
            {words.name}
            <Input
              id={nameId}
              value={name}
              maxLength={80}
              placeholder={words.nameHint}
              onChange={(event) => setName(event.target.value)}
              className="mt-1"
            />
          </label>
          <Button type="submit" size="sm" disabled={!name.trim() || !!busy}>
            {words.create}
          </Button>
          <Button type="button" variant="ghost" size="sm" onClick={() => setCreating(false)}>
            {words.cancel}
          </Button>
        </form>
      )}
      {problem && <Alert>{problem}</Alert>}
      {note && <output className="text-aui-xs text-aui-muted">{note}</output>}
      {list.scratchpads.length === 0 ? (
        <p className="py-3 text-aui-xs text-aui-muted">{words.empty}</p>
      ) : (
        <ul className="flex min-w-0 flex-col">
          {list.scratchpads.map((pad) => (
            <li key={pad.id}>
              <button type="button" className={row} onClick={() => onSelect(pad.id, pad.name)}>
                <StickyNoteIcon size={14} />
                <span className="min-w-0 flex-1 truncate">{pad.name}</span>
                {dirty(drafts[pad.id]) && (
                  <span className="text-aui-xs text-aui-muted">{words.unsaved}</span>
                )}
                <span className="shrink-0 text-aui-xs text-aui-muted">{ago(pad.updated_at)}</span>
                <ChevronRightIcon size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
