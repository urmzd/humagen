import { defaultCopy } from "@humagen/components";
import type { Note, NoteInput } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api, type ProjectView, type Revision } from "./api.ts";
import { card, field, Page, Problem, SkeletonRows } from "./ui.tsx";

type Scope = "personal" | "workspace" | "project" | "agent";
const localDate = (value: string) => {
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
};
export function MemoryLibrary() {
  const copy = defaultCopy.memoryLibrary;
  const [scope, setScope] = useState<Scope>(() => {
    const value = new URLSearchParams(window.location.search).get("scope");
    return value === "workspace" || value === "project" || value === "agent" ? value : "personal";
  });
  const [owner, setOwner] = useState(
    () => new URLSearchParams(window.location.search).get("owner") ?? "",
  );
  const [projects, setProjects] = useState<ProjectView[]>([]);
  const [agents, setAgents] = useState<Revision[]>([]);
  const [homes, setHomes] = useState<Record<string, string>>({});
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [query, setQuery] = useState("");
  const [text, setText] = useState("");
  const [kind, setKind] = useState<NoteInput["kind"]>("fact");
  const [expires, setExpires] = useState("");
  const [editing, setEditing] = useState<Note | null>(null);
  const [canWrite, setCanWrite] = useState(false);
  const [canAdd, setCanAdd] = useState(false);
  const [permissions, setPermissions] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  const [destination, setDestination] = useState("~");
  const [promotionSaved, setPromotionSaved] = useState(false);
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    let live = true;
    Promise.all([api.me(), api.projects(), api.agentList()])
      .then(([me, places, workers]) => {
        if (live) {
          setPermissions(me.permissions);
          setProjects(places);
          setAgents(workers.agents);
          setHomes(workers.homes ?? {});
        }
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    return () => {
      live = false;
    };
  }, []);
  const read = () =>
    scope === "personal"
      ? api.personalMemory()
      : scope === "agent"
        ? api.memory(owner)
        : api.projectMemory(scope === "workspace" ? "" : owner);
  useEffect(() => {
    let live = true;
    setNotes(null);
    setFailed(false);
    setEditing(null);
    setText("");
    setExpires("");
    setCanWrite(false);
    setCanAdd(false);
    if ((scope === "project" || scope === "agent") && !owner) {
      setNotes([]);
      return () => {
        live = false;
      };
    }
    read()
      .then((rows) => {
        if (live) setNotes(rows);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    if (scope === "personal") {
      setCanWrite(permissions.includes("runs:write"));
      setCanAdd(permissions.includes("runs:write"));
    } else if (scope === "agent") {
      const allowed = permissions.includes("agents:write");
      const home = homes[owner]?.replace(/^~/, "") ?? "";
      if (home) {
        api
          .project(home)
          .then((detail) => {
            if (live) {
              const owns = allowed && detail.project.access.role === "owner";
              setCanWrite(owns);
              setCanAdd(owns);
            }
          })
          .catch(() => {});
      } else {
        setCanWrite(allowed);
        setCanAdd(allowed);
      }
    } else {
      api
        .project(scope === "workspace" ? "" : owner)
        .then((detail) => {
          if (live) {
            setCanWrite(detail.project.actions.includes("notes"));
            setCanAdd(
              detail.project.actions.includes("propose") ||
                detail.project.actions.includes("notes"),
            );
          }
        })
        .catch(() => {});
    }
    return () => {
      live = false;
    };
  }, [scope, owner, refresh, permissions, homes]);
  const save = async () => {
    setBusy(true);
    setFailed(false);
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
      setNotes(await read());
      setText("");
      setExpires("");
      setEditing(null);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const promote = async (note: Note) => {
    setBusy(true);
    setFailed(false);
    setPromotionSaved(false);
    try {
      await api.promoteMemory(
        scope,
        scope === "personal" || scope === "workspace" ? "" : owner,
        note.id,
        destination,
      );
      setPromotionSaved(true);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const forget = async (id: string) => {
    setBusy(true);
    setFailed(false);
    try {
      if (scope === "personal") await api.forgetPersonal(id);
      else if (scope === "agent") await api.removeMemory(owner, id);
      else await api.removeProjectMemory(scope === "workspace" ? "" : owner, id);
      setNotes((current) => current?.filter((note) => note.id !== id) ?? null);
      if (editing?.id === id) {
        setEditing(null);
        setText("");
      }
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const approve = async (id: string) => {
    setBusy(true);
    setFailed(false);
    try {
      if (scope === "personal") await api.approvePersonal([id]);
      else if (scope === "agent") await api.approveMemory(owner, [id]);
      else await api.approveProjectMemory(scope === "workspace" ? "" : owner, [id]);
      setNotes(await read());
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  const visible = (notes ?? []).filter((note) =>
    `${note.text} ${note.source_name ?? ""} ${note.by_name ?? ""} ${note.kind ?? "fact"}`
      .toLowerCase()
      .includes(query.trim().toLowerCase()),
  );
  return (
    <Page title={copy.title} lede={copy.description}>
      <div className="flex flex-wrap items-end gap-4">
        <label>
          {copy.scope}
          <select
            className={`${field} mt-2`}
            value={scope}
            onChange={(event) => {
              setScope(event.target.value as Scope);
              setOwner("");
            }}
          >
            {(["personal", "workspace", "project", "agent"] as const).map((value) => (
              <option key={value} value={value}>
                {copy[value]}
              </option>
            ))}
          </select>
        </label>
        {(scope === "project" || scope === "agent") && (
          <label>
            {copy.owner}
            <select
              className={`${field} mt-2 max-w-full`}
              value={owner}
              onChange={(event) => setOwner(event.target.value)}
            >
              <option value="">{copy.emptyScope}</option>
              {scope === "project"
                ? projects.map((project) => (
                    <option key={project.name} value={project.name}>
                      {project.title || project.name}
                    </option>
                  ))
                : agents.map((agent) => (
                    <option key={agent.spec.name} value={agent.spec.name}>
                      {agent.spec.name}
                    </option>
                  ))}
            </select>
          </label>
        )}
        <label className="min-w-0 flex-1">
          {copy.search}
          <input
            type="search"
            className={`${field} mt-2 w-full`}
            value={query}
            onChange={(event) => setQuery(event.target.value)}
          />
        </label>
      </div>
      {failed && <Problem message={copy.failed} />}
      {canAdd && (
        <form
          className={`${card} space-y-4 p-4`}
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <label className="block">
            {copy.text}
            <textarea
              required
              maxLength={300}
              className={`${field} mt-2 min-h-24 w-full`}
              value={text}
              onChange={(event) => setText(event.target.value)}
            />
          </label>
          <div className="flex flex-wrap gap-4">
            <label>
              {copy.kind}
              <select
                className={`${field} mt-2 block max-w-full`}
                value={kind}
                onChange={(event) => setKind(event.target.value as NoteInput["kind"])}
              >
                {(["fact", "decision", "preference", "person", "risk"] as const).map((value) => (
                  <option key={value} value={value}>
                    {copy[value]}
                  </option>
                ))}
              </select>
            </label>
            <label>
              {copy.expires}
              <input
                type="datetime-local"
                className={`${field} mt-2 block max-w-full`}
                value={expires}
                onChange={(event) => setExpires(event.target.value)}
              />
            </label>
          </div>
          <div className="flex flex-wrap gap-4">
            <button
              type="submit"
              disabled={busy || !text.trim()}
              className="min-h-11 text-aui-accent disabled:opacity-50"
            >
              {editing ? copy.saveEdit : copy.save}
            </button>
            {editing && (
              <button
                type="button"
                className="min-h-11 text-aui-muted"
                onClick={() => {
                  setEditing(null);
                  setText("");
                  setExpires("");
                }}
              >
                {copy.cancel}
              </button>
            )}
          </div>
        </form>
      )}
      {!canWrite && notes !== null && notes.length > 0 && (
        <p className="text-aui-muted">{copy.readOnly}</p>
      )}
      {promotionSaved && (
        <p role="status" className="text-aui-muted">
          {copy.promotionSaved}
        </p>
      )}
      {notes === null ? (
        <SkeletonRows count={3} />
      ) : visible.length === 0 ? (
        <p className="text-aui-muted">
          {query
            ? copy.noMatches
            : (scope === "project" || scope === "agent") && !owner
              ? copy.emptyScope
              : copy.empty}
        </p>
      ) : (
        <ol className="space-y-3">
          {visible.map((note) => (
            <li key={note.id} className={`${card} min-w-0 break-words p-4`}>
              <div className="flex flex-wrap items-baseline justify-between gap-3">
                <span className="text-aui-sm text-aui-muted">
                  {copy[scope]}: {copy[note.kind ?? "fact"]},{" "}
                  {note.expires_at && new Date(note.expires_at) <= new Date()
                    ? copy.expired
                    : note.status === "approved"
                      ? copy.confirmed
                      : copy.suggested}
                </span>
                <div className="flex flex-wrap gap-4">
                  {note.source_url && (
                    <a
                      className="inline-flex min-h-11 items-center text-aui-accent"
                      href={note.source_url}
                    >
                      {copy.openSource}
                    </a>
                  )}
                  {permissions.includes("projects:write") &&
                    note.status === "approved" &&
                    (!note.expires_at || new Date(note.expires_at) > new Date()) && (
                      <details className="min-w-0 max-w-full">
                        <summary className="cursor-pointer py-3 text-aui-accent">
                          {copy.promote}
                        </summary>
                        <div className="max-w-md space-y-3 py-2">
                          <p className="text-aui-sm text-aui-muted">{copy.promotionReview}</p>
                          <label className="block">
                            {copy.destination}
                            <select
                              className={`${field} mt-2 block max-w-full`}
                              value={destination}
                              onChange={(event) => setDestination(event.target.value)}
                            >
                              <option value="~">{copy.workspace}</option>
                              {projects
                                .filter(
                                  (project) =>
                                    project.name !== owner &&
                                    (project.actions.includes("propose") ||
                                      project.actions.includes("notes")),
                                )
                                .map((project) => (
                                  <option key={project.name} value={project.name}>
                                    {project.title || project.name}
                                  </option>
                                ))}
                            </select>
                          </label>
                          <button
                            type="button"
                            className="min-h-11 text-aui-accent"
                            disabled={busy}
                            onClick={() => void promote(note)}
                          >
                            {copy.promote}
                          </button>
                        </div>
                      </details>
                    )}
                  {canWrite && (
                    <>
                      <button
                        type="button"
                        disabled={busy}
                        className="min-h-11 text-aui-accent disabled:opacity-50"
                        onClick={() => {
                          setEditing(note);
                          setText(note.text);
                          setKind(note.kind ?? "fact");
                          setExpires(note.expires_at ? localDate(note.expires_at) : "");
                        }}
                      >
                        {copy.edit}
                      </button>
                      {note.status === "pending" && (
                        <button
                          type="button"
                          disabled={busy}
                          className="min-h-11 text-aui-accent disabled:opacity-50"
                          onClick={() => void approve(note.id)}
                        >
                          {copy.approve}
                        </button>
                      )}
                      <button
                        type="button"
                        disabled={busy}
                        className="min-h-11 text-aui-accent disabled:opacity-50"
                        onClick={() => void forget(note.id)}
                      >
                        {note.status === "pending" ? copy.discard : copy.forget}
                      </button>
                    </>
                  )}
                </div>
              </div>
              <p className="whitespace-pre-wrap">{note.text}</p>
              <p className="mt-3 text-aui-sm text-aui-muted">
                {copy.source}: {note.source_name ?? note.by_name ?? note.source}, {copy.uses}:{" "}
                {note.uses ?? 0}
              </p>
              <time dateTime={note.at} className="text-aui-sm text-aui-muted">
                {new Date(note.at).toLocaleString()}
              </time>
              {note.expires_at && (
                <p className="text-aui-sm text-aui-muted">
                  {copy.expires}: {new Date(note.expires_at).toLocaleString()}
                </p>
              )}
            </li>
          ))}
        </ol>
      )}
    </Page>
  );
}
