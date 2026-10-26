import { defaultCopy } from "@humagen/components";
import type { Note, NoteInput } from "../../../packages/client/src/generated/types.ts";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { card, pageLede, pageTitle, Problem, SkeletonRows } from "./ui.tsx";

export function MemoryLibrary() {
  const copy = defaultCopy.memoryLibrary;
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [text, setText] = useState("");
  const [kind, setKind] = useState<NoteInput["kind"]>("fact");
  const [expires, setExpires] = useState("");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    let live = true;
    api
      .personalMemory()
      .then((value) => {
        if (live) setNotes(value);
      })
      .catch(() => {
        if (live) setFailed(true);
      });
    const approve=async(id:string)=>{setBusy(true);setFailed(false);try{await api.approvePersonal([id]);setNotes(await api.personalMemory())}catch{setFailed(true)}finally{setBusy(false)}};
  return () => {
      live = false;
    };
  }, []);
  const save = async () => {
    setBusy(true);
    setFailed(false);
    try {
      await api.rememberPersonal({
        text,
        kind,
        ...(expires ? { expires_at: new Date(expires).toISOString() } : {}),
      });
      setNotes(await api.personalMemory());
      setText("");
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
      await api.forgetPersonal(id);
      setNotes((current) => current?.filter((n) => n.id !== id) ?? null);
    } catch {
      setFailed(true);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section className="min-w-0 space-y-5">
      <header>
        <h1 className={pageTitle}>{copy.title}</h1>
        <p className={pageLede}>{copy.description}</p>
      </header>
      {failed && <Problem message={copy.failed} />}
      <form
        className={`${card} space-y-4 p-4`}
        onSubmit={(event) => {
          event.preventDefault();
          void save();
        }}
      >
        <h2 className="font-medium">{copy.personal}</h2>
        <label className="block">
          {copy.text}
          <textarea
            required
            maxLength={300}
            value={text}
            onChange={(e) => setText(e.target.value)}
            className="mt-2 min-h-24 w-full rounded border border-aui-border bg-aui-bg p-3"
          />
        </label>
        <div className="flex flex-wrap gap-4">
          <label>
            {copy.kind}
            <select
              className="ml-2 min-h-11 bg-aui-bg"
              value={kind}
              onChange={(e) => setKind(e.target.value as NoteInput["kind"])}
            >
              {(["fact", "decision", "preference", "person"] as const).map((value) => (
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
              className="ml-2 min-h-11 max-w-full bg-aui-bg"
              value={expires}
              onChange={(e) => setExpires(e.target.value)}
            />
          </label>
        </div>
        <button
          type="submit"
          disabled={busy || !text.trim()}
          className="min-h-11 text-aui-accent disabled:opacity-50"
        >
          {copy.save}
        </button>
      </form>
      {notes === null ? (
        <SkeletonRows count={3} />
      ) : notes.length === 0 ? (
        <p className="text-aui-muted">{copy.empty}</p>
      ) : (
        <ol className="space-y-3">
          {notes.map((note) => (
            <li key={note.id} className={`${card} min-w-0 break-words p-4`}>
              <div className="flex flex-wrap justify-between gap-3">
                <span className="text-aui-sm text-aui-muted">
                  {copy.personal} · {copy[note.kind ?? "fact"]} ·{" "}
                  {note.expires_at && new Date(note.expires_at) <= new Date()
                    ? copy.expired
                    : note.status === "approved"
                      ? copy.confirmed
                      : copy.suggested}
                </span>
                {note.status==="pending"&&<button type="button" disabled={busy} onClick={()=>void approve(note.id)} className="min-h-11 text-aui-accent disabled:opacity-50">{copy.approve}</button>}
                    <button
                  type="button"
                  disabled={busy}
                  onClick={() => void forget(note.id)}
                  className="min-h-11 text-aui-accent disabled:opacity-50"
                >
                  {copy.forget}
                </button>
              </div>
              <p className="whitespace-pre-wrap">{note.text}</p>
              <p className="mt-3 text-aui-sm text-aui-muted">
                {copy.source}: {note.source} · {copy.uses}: {note.uses ?? 0}
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
    </section>
  );
}
