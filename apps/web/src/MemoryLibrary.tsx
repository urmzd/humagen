import { defaultCopy } from "@humagen/components";
import type {
  MemoryFolder,
  Note,
  NoteInput,
} from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { FileBrowser, Inspector, ResourceBrowser } from "@humagen/web";
import {
  FileIcon,
  FolderIcon,
  ProjectIcon,
  ChevronDownIcon,
  SearchIcon,
  MessageIcon,
} from "@humagen/render-web";
import { Button, buttonVariants } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { Textarea } from "@humagen/render-web/ui/textarea";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Badge } from "@humagen/render-web/ui/badge";
import { api } from "./api.ts";
import { href } from "./route.ts";
import { AgentMark, Page, Problem, SkeletonRows } from "./ui.tsx";

type Scope = "personal" | "workspace" | "project" | "agent";
const localDate = (value: string) => {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
};
export interface MemoryLibraryLocation {
  scope: Scope;
  owner?: string;
  file?: string;
}
export function MemoryLibrary({
  onLocationChange,
  file,
}: {
  onLocationChange?: (location: MemoryLibraryLocation) => void;
  file?: string;
} = {}) {
  const copy = defaultCopy.memoryLibrary;
  const [scope, setScope] = useState<Scope>(() => {
    const value = new URLSearchParams(window.location.search).get("scope");
    return value === "workspace" || value === "project" || value === "agent" ? value : "personal";
  });
  const [owner, setOwner] = useState(
    () => new URLSearchParams(window.location.search).get("owner") ?? "",
  );
  const [selectedFile, setSelectedFile] = useState(file ?? "");
  useEffect(() => setSelectedFile(file ?? ""), [file]);
  const [destinationsCatalog, setDestinationsCatalog] = useState<MemoryFolder[]>([]);
  const [folders, setFolders] = useState<MemoryFolder[] | null>(null);
  const [folderQuery, setFolderQuery] = useState("");
  const [folderError, setFolderError] = useState(false);
  const [catalogRefresh, setCatalogRefresh] = useState(0);
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [query, setQuery] = useState("");
  const [text, setText] = useState("");
  const [kind, setKind] = useState<NoteInput["kind"]>("fact");
  const [expires, setExpires] = useState("");
  const [editing, setEditing] = useState<Note | null>(null);
  const [editorOpen, setEditorOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [loadError, setLoadError] = useState(false);
  const [mutationError, setMutationError] = useState(false);
  const [destination, setDestination] = useState("~");
  const [promotionSaved, setPromotionSaved] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const id = scope === "personal" || scope === "workspace" ? scope : `${scope}:${owner}`;
  const folder = folders?.find((value) => value.id === id);
  const canWrite = folder?.can_write ?? false;
  const canAdd = folder?.can_propose ?? false;
  const canSubmit = editing ? canWrite : canAdd;
  const destinations = destinationsCatalog.filter(
    (value) =>
      (value.scope === "project" || value.scope === "workspace") &&
      value.can_propose &&
      value.id !== id,
  );
  useEffect(() => {
    let live = true;
    const timer = setTimeout(
      () => {
        api
          .memoryFolders(folderQuery, id)
          .then((rows) => {
            if (live) {
              setFolders(rows);
              if (!folderQuery) setDestinationsCatalog(rows);
              setFolderError(false);
              if (
                !new URLSearchParams(window.location.search).has("scope") &&
                !rows.some((row) => row.id === id) &&
                rows[0]
              ) {
                setScope(rows[0].scope);
                setOwner(rows[0].owner ?? "");
              }
            }
          })
          .catch(() => {
            if (live) setFolderError(true);
          });
      },
      folderQuery ? 180 : 0,
    );
    return () => {
      live = false;
      clearTimeout(timer);
    };
  }, [folderQuery, catalogRefresh, id]);
  const read = () =>
    scope === "personal"
      ? api.personalMemory()
      : scope === "agent"
        ? api.memory(owner)
        : api.projectMemory(scope === "workspace" ? "" : owner);
  const folderAvailable = !!folder;
  useEffect(() => {
    let live = true;
    setNotes(null);
    setLoadError(false);
    setMutationError(false);
    setEditing(null);
    setEditorOpen(false);
    setText("");
    setExpires("");
    setPromotionSaved(false);
    if (folderAvailable)
      read()
        .then((rows) => {
          if (live) setNotes(rows);
        })
        .catch(() => {
          if (live) setLoadError(true);
        });
    return () => {
      live = false;
    };
  }, [scope, owner, folderAvailable, refresh]);
  const changed = (rows: Note[]) => {
    setNotes(rows);
    setCatalogRefresh((n) => n + 1);
  };
  const save = async () => {
    setBusy(true);
    setMutationError(false);
    try {
      const input: NoteInput = {
        text,
        kind,
        expires_at: expires ? new Date(expires).toISOString() : null,
      };
      if (editing) {
        if (scope === "personal") await api.editPersonalMemory(editing.id, input);
        else if (scope === "agent") await api.editAgentMemory(owner, editing.id, input);
        else await api.editProjectMemory(scope === "workspace" ? "" : owner, editing.id, input);
      } else if (scope === "personal") await api.rememberPersonal(input);
      else if (scope === "agent") await api.addMemory(owner, text, input);
      else await api.addProjectMemory(scope === "workspace" ? "" : owner, text, input);
      changed(await read());
      setText("");
      setExpires("");
      setEditing(null);
      setEditorOpen(false);
    } catch {
      setMutationError(true);
    } finally {
      setBusy(false);
    }
  };
  const forget = async (note: Note) => {
    setBusy(true);
    setMutationError(false);
    try {
      if (scope === "personal") await api.forgetPersonal(note.id);
      else if (scope === "agent") await api.removeMemory(owner, note.id);
      else await api.removeProjectMemory(scope === "workspace" ? "" : owner, note.id);
      changed(await read());
      if (selectedFile === `${note.id}.md`) {
        setSelectedFile("");
        onLocationChange?.({ scope, owner });
      }
    } catch {
      setMutationError(true);
    } finally {
      setBusy(false);
    }
  };
  const approve = async (note: Note) => {
    setBusy(true);
    setMutationError(false);
    try {
      if (scope === "personal") await api.approvePersonal([note.id]);
      else if (scope === "agent") await api.approveMemory(owner, [note.id]);
      else await api.approveProjectMemory(scope === "workspace" ? "" : owner, [note.id]);
      changed(await read());
    } catch {
      setMutationError(true);
    } finally {
      setBusy(false);
    }
  };
  const promote = async (note: Note) => {
    setBusy(true);
    setMutationError(false);
    setPromotionSaved(false);
    try {
      await api.promoteMemory(
        scope,
        scope === "personal" || scope === "workspace" ? "" : owner,
        note.id,
        destinations.some((folder) => (folder.owner || "~") === destination)
          ? destination
          : destinations[0]?.owner || "~",
      );
      setPromotionSaved(true);
    } catch {
      setMutationError(true);
    } finally {
      setBusy(false);
    }
  };
  const select = (note?: Note) => {
    const next = note ? `${note.id}.md` : "";
    setSelectedFile(next);
    onLocationChange?.({ scope, owner, ...(next ? { file: next } : {}) });
  };
  const status = (note: Note) =>
    note.expires_at && new Date(note.expires_at) <= new Date()
      ? copy.expired
      : note.status === "pending"
        ? copy.suggested
        : copy.confirmed;
  const filePath = (note: Note) => `${status(note)}/${note.id}.md`;
  const visible = (notes ?? []).filter((note) =>
    `${note.text} ${note.source_name ?? ""} ${note.by_name ?? ""} ${note.kind ?? "fact"}`
      .toLowerCase()
      .includes(query.trim().toLowerCase()),
  );
  const selected = notes?.find((note) => `${note.id}.md` === selectedFile);
  const edit = (note: Note) => {
    setEditing(note);
    setText(note.text);
    setKind(note.kind ?? "fact");
    setExpires(note.expires_at ? localDate(note.expires_at) : "");
    setEditorOpen(true);
  };
  const actions = (note: Note) =>
    canWrite && (
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" size="sm" disabled={busy} onClick={() => edit(note)}>
          {copy.edit}
        </Button>
        {note.status === "pending" && (
          <Button variant="outline" size="sm" disabled={busy} onClick={() => void approve(note)}>
            {copy.approve}
          </Button>
        )}
        <Button variant="ghost" size="sm" disabled={busy} onClick={() => void forget(note)}>
          {note.status === "pending" ? copy.discard : copy.forget}
        </Button>
      </div>
    );
  const management = folder ? copy[folder.management] : "";
  return (
    <Page
      title={copy.title}
      lede={copy.description}
      wide
      actions={
        folder && (
          <>
            <Button
              variant="outline"
              disabled={!canAdd || busy}
              onClick={() => {
                setEditing(null);
                setText("");
                setExpires("");
                setKind("fact");
                setEditorOpen(true);
              }}
            >
              {copy.add}
            </Button>
            {folder.can_chat && (
              <a
                className={buttonVariants()}
                href={href({
                  page: "new",
                  project: folder.chat_project || "~",
                  agent: folder.chat_agent,
                  ask: copy.chatQuestion,
                })}
              >
                <MessageIcon />
                {copy.chat}
              </a>
            )}
          </>
        )
      }
    >
      {folderError && (
        <div className="flex flex-col items-start gap-3">
          <Problem message={copy.catalogFailed} />
          <Button variant="outline" onClick={() => setCatalogRefresh((n) => n + 1)}>
            {copy.retry}
          </Button>
        </div>
      )}
      {mutationError && !editorOpen && <Problem message={copy.mutationFailed} />}
      {promotionSaved && (
        <p role="status" className="text-sm text-muted-foreground">
          {copy.promotionSaved}
        </p>
      )}
      {folders === null ? (
        !folderError && <SkeletonRows count={4} />
      ) : (
        <ResourceBrowser
          selectedLabel={
            folder?.scope === "personal"
              ? copy.personal
              : folder?.scope === "workspace"
                ? copy.workspace
                : folder?.title
          }
          disabled={busy || editorOpen}
          scopes={folders
            .filter((folder) => !folder.selected_only)
            .map((folder) => ({
              id: folder.id,
              label:
                folder.scope === "personal"
                  ? copy.personal
                  : folder.scope === "workspace"
                    ? copy.workspace
                    : folder.title,
              group:
                folder.scope === "project"
                  ? copy.projects
                  : folder.scope === "agent"
                    ? copy.agents
                    : undefined,
              icon:
                folder.scope === "agent" ? (
                  <AgentMark name={folder.owner ?? folder.title} />
                ) : folder.scope === "project" ? (
                  <ProjectIcon />
                ) : (
                  <FolderIcon />
                ),
              count: folder.notes + folder.pending,
            }))}
          selectedScope={id}
          scopeLabel={copy.chains}
          scopeSearch={
            <Input
              type="search"
              aria-label={copy.searchChains}
              placeholder={copy.searchChains}
              value={folderQuery}
              onChange={(event) => setFolderQuery(event.target.value)}
            />
          }
          onScopeChange={(next) => {
            const found = folders.find((folder) => folder.id === next);
            if (!found) return;
            setScope(found.scope);
            setOwner(found.owner ?? "");
            setSelectedFile("");
            onLocationChange?.({ scope: found.scope, owner: found.owner });
          }}
        >
          {!folder ? (
            <p className="py-8 text-sm text-muted-foreground">{copy.chainMissing}</p>
          ) : (
            <div className="flex min-w-0 flex-col gap-5">
              <header className="flex flex-col gap-3">
                <h2 className="text-lg font-semibold">
                  {folder.scope === "personal"
                    ? copy.personal
                    : folder.scope === "workspace"
                      ? copy.workspace
                      : folder.title}
                </h2>
                <details className="text-sm">
                  <summary className="group flex min-h-9 w-fit max-w-full cursor-pointer items-center gap-2 rounded-md pr-2 focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-11">
                    <Badge variant="secondary" className="max-w-full whitespace-normal text-left">{management}</Badge>
                    <ChevronDownIcon
                      size={14}
                      className="text-muted-foreground group-open:rotate-180"
                    />
                  </summary>
                  <div className="mt-3 flex max-w-[70ch] flex-col items-start gap-3 leading-6 text-muted-foreground">
                    <p>
                      {folder.management === "manual"
                        ? copy.manualManagementHint
                        : folder.scope === "personal"
                          ? copy.personalManagementHint
                          : copy.managementHint}
                    </p>
                    {folder.scope === "personal" && (
                      <a className="underline underline-offset-4" href="/you/privacy">
                        {copy.preferences}
                      </a>
                    )}
                    <p>{folder.can_chat ? copy.chatHint : copy.chatUnavailable}</p>
                  </div>
                </details>
              </header>
              <div className="relative">
                <SearchIcon className="absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <Input
                  type="search"
                  className="pl-9"
                  aria-label={copy.search}
                  placeholder={copy.search}
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                />
              </div>
              {loadError ? (
                <div className="space-y-3">
                  <Problem message={copy.loadFailed} />
                  <Button variant="outline" onClick={() => setRefresh((n) => n + 1)}>
                    {copy.retry}
                  </Button>
                </div>
              ) : notes === null ? (
                <SkeletonRows count={3} />
              ) : selectedFile ? (
                <>
                  <Button variant="ghost" className="self-start" onClick={() => select()}>
                    {copy.back}
                  </Button>
                  {!selected ? (
                    <p className="text-sm text-muted-foreground">{copy.fileMissing}</p>
                  ) : (
                    <FileBrowser
                      label={copy.entries}
                      paths={visible.map(filePath)}
                      labels={Object.fromEntries(
                        visible.map((note) => [filePath(note), note.text]),
                      )}
                      selected={filePath(selected)}
                      onSelect={(path) => select(notes.find((note) => filePath(note) === path))}
                    >
                      <div className="flex min-w-0 flex-col gap-5 p-5">
                        <div className="flex flex-wrap items-center gap-2">
                          <FileIcon />
                          <span className="min-w-0 truncate text-xs text-muted-foreground">
                            {selected.id}.md
                          </span>
                          <Badge variant="outline">{status(selected)}</Badge>
                          <Badge variant="secondary">{copy[selected.kind ?? "fact"]}</Badge>
                        </div>
                        <p className="whitespace-pre-wrap break-words text-base leading-7">
                          {selected.text}
                        </p>
                        {actions(selected)}
                        <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 border-t border-border pt-4 text-sm">
                          <dt className="text-muted-foreground">{copy.source}</dt>
                          <dd className="min-w-0 break-words">
                            {selected.source_url ? (
                              <a
                                className="underline underline-offset-4"
                                href={selected.source_url}
                              >
                                {selected.source_name ?? copy.openSource}
                              </a>
                            ) : (
                              (selected.source_name ?? selected.by_name ?? selected.source)
                            )}
                          </dd>
                          <dt className="text-muted-foreground">{copy.uses}</dt>
                          <dd>{selected.uses ?? 0}</dd>
                          <dt className="text-muted-foreground">{copy.created}</dt>
                          <dd>
                            <time dateTime={selected.at}>
                              {new Date(selected.at).toLocaleString()}
                            </time>
                          </dd>
                          {selected.expires_at && (
                            <>
                              <dt className="text-muted-foreground">{copy.expires}</dt>
                              <dd>{new Date(selected.expires_at).toLocaleString()}</dd>
                            </>
                          )}
                          {selected.edited_at && (
                            <>
                              <dt className="text-muted-foreground">{copy.corrected}</dt>
                              <dd>{new Date(selected.edited_at).toLocaleString()}</dd>
                            </>
                          )}
                        </dl>
                        {folder.can_promote &&
                          destinations.length > 0 &&
                          selected.status === "approved" &&
                          status(selected) !== copy.expired && (
                            <details className="border-t border-border pt-3 text-sm">
                              <summary className="cursor-pointer py-2 font-medium">
                                {copy.promote}
                              </summary>
                              <div className="flex flex-col gap-3 pt-2">
                                <p className="text-muted-foreground">{copy.promotionReview}</p>
                                <label className="flex flex-col gap-2">
                                  {copy.destination}
                                  <NativeSelect
                                    aria-label={copy.destination}
                                    value={
                                      destinations.some(
                                        (folder) => (folder.owner || "~") === destination,
                                      )
                                        ? destination
                                        : destinations[0]?.owner || "~"
                                    }
                                    onChange={(event) => setDestination(event.target.value)}
                                  >
                                    {destinations.map((folder) => (
                                      <option key={folder.id} value={folder.owner || "~"}>
                                        {folder.scope === "workspace"
                                          ? copy.workspace
                                          : folder.title}
                                      </option>
                                    ))}
                                  </NativeSelect>
                                </label>
                                <Button
                                  variant="outline"
                                  className="self-start"
                                  disabled={busy}
                                  onClick={() => void promote(selected)}
                                >
                                  {copy.promote}
                                </Button>
                              </div>
                            </details>
                          )}
                      </div>
                    </FileBrowser>
                  )}
                </>
              ) : visible.length === 0 ? (
                <p className="py-10 text-sm text-muted-foreground">
                  {query ? copy.noMatches : copy.empty}
                </p>
              ) : (
                <ul className="divide-y divide-border">
                  {visible.map((note) => (
                    <li key={note.id} className="flex flex-wrap items-center gap-x-3 px-2 py-3">
                      <button
                        type="button"
                        onClick={() => select(note)}
                        className="flex min-h-12 min-w-0 flex-1 items-start gap-3 rounded-md py-2 text-left hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring"
                      >
                        <FileIcon className="mt-1" />
                        <span className="min-w-0 flex-1">
                          <span className="line-clamp-2 text-sm leading-6">{note.text}</span>
                          <span className="mt-1 flex flex-wrap gap-x-3 gap-y-1 text-xs text-muted-foreground">
                            <span>{status(note)}</span>
                            <span>{copy[note.kind ?? "fact"]}</span>
                            <time dateTime={note.at}>{new Date(note.at).toLocaleDateString()}</time>
                          </span>
                        </span>
                      </button>
                      {actions(note)}
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </ResourceBrowser>
      )}
      <Inspector
        open={editorOpen}
        title={editing ? copy.saveEdit : copy.add}
        onClose={() => {
          if (!busy) setEditorOpen(false);
        }}
      >
        {editorOpen && (<form
          className="space-y-5"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          {mutationError && <Problem message={copy.mutationFailed} />}
          <label className="flex flex-col gap-2 text-sm">
            {copy.text}
            <Textarea
              required
              maxLength={300}
              className="min-h-32"
              value={text}
              disabled={busy}
              onChange={(event) => setText(event.target.value)}
            />
          </label>
          <label className="flex flex-col gap-2 text-sm">
            {copy.kind}
            <NativeSelect
              value={kind ?? "fact"}
              disabled={busy}
              onChange={(event) => setKind(event.target.value as NoteInput["kind"])}
            >
              {(["fact", "decision", "preference", "person", "risk"] as const).map((value) => (
                <option key={value} value={value}>
                  {copy[value]}
                </option>
              ))}
            </NativeSelect>
          </label>
          <label className="flex flex-col gap-2 text-sm">
            {copy.expires}
            <Input
              type="datetime-local"
              value={expires}
              disabled={busy}
              onChange={(event) => setExpires(event.target.value)}
            />
          </label>
          <div className="flex flex-wrap gap-2">
            <Button type="submit" disabled={busy || !canSubmit || !text.trim()}>
              {editing ? copy.saveEdit : copy.save}
            </Button>
            <Button variant="outline" disabled={busy} onClick={() => setEditorOpen(false)}>
              {copy.cancel}
            </Button>
          </div>
        </form>)}
      </Inspector>
    </Page>
  );
}
