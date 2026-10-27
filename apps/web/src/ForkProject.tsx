import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { useEffect, useState } from "react";
import { api, type Note, type ProjectForkOperation, type ProjectView } from "./api.ts";
import { button, field, Label, primary, Problem } from "./ui.tsx";

const words = defaultCopy.projectFork;

/** The host opens the ready project only after the durable publication gate. */
export function ForkProject({ source, projects, onClose, onReady, open = true }: {
  open?: boolean; source: string; projects: ProjectView[]; onClose: () => void; onReady: (name: string) => void;
}) {
  const [name, setName] = useState(`${source}-copy`);
  const [title, setTitle] = useState("");
  const [parent, setParent] = useState("");
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [selectedMemory, setSelectedMemory] = useState<string[]>([]);
  const [sources, setSources] = useState<string[]>([]);
  const [selectedSources, setSelectedSources] = useState<string[]>([]);
  const [archive, setArchive] = useState(false);
  const [operation, setOperation] = useState<ProjectForkOperation | null>(null);
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    api.projectMemory(source).then((items) => { if (active) setNotes(items.filter((note) => note.status === "approved" && (!note.expires_at || new Date(note.expires_at).getTime() > Date.now()))); }).catch(() => { if (active) setNotes([]); });
    api.sources().then((items) => { if (active) setSources(items.map((item) => item.name)); }).catch(() => {});
    return () => { active = false; };
  }, [source]);
  const submit = () => {
    setSaving(true); setProblem(null);
    api.forkProject(source, { name, title, parent, memory: selectedMemory, sources: selectedSources, ...(archive ? { archive_conversations: true } : {}) }).then((result) => {
      setOperation(result);
      if (result.state === "ready") onReady(result.destination);
    }).catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.failed)).finally(() => setSaving(false));
  };
  return <Inspector open={open} title={words.title} onClose={onClose}>
    <form className="flex min-w-0 flex-col gap-5" onSubmit={(event) => { event.preventDefault(); submit(); }}>
      <p className="text-aui-sm text-aui-muted">{words.hint}</p><p className="text-aui-xs text-aui-muted">{words.hidden}</p>
      <Label text={words.name}><input className={field} value={name} maxLength={63} required disabled={saving || operation !== null} onChange={(event) => setName(event.target.value)} /></Label>
      <Label text={words.titleField}><input className={field} value={title} maxLength={80} disabled={saving || operation !== null} onChange={(event) => setTitle(event.target.value)} /></Label>
      <Label text={words.parent}><select aria-label={words.parent} className={field} value={parent} disabled={saving || operation !== null} onChange={(event) => setParent(event.target.value)}><option value="">{words.company}</option>{projects.filter((project) => !project.archived && (project.access.role === "owner" || project.access.role === "member")).map((project) => <option key={project.name} value={project.name}>{project.title || project.name}</option>)}</select></Label>
      <fieldset className="flex flex-col gap-2" disabled={saving || operation !== null}><legend className="mb-2 text-aui-sm font-medium">{words.memory}</legend>{notes === null ? <p className="text-aui-xs text-aui-muted">{words.loading}</p> : notes.length === 0 ? <p className="text-aui-xs text-aui-muted">{words.noMemory}</p> : notes.map((note) => <label key={note.id} className="flex min-h-10 items-start gap-2 text-aui-sm"><input className="mt-1" type="checkbox" checked={selectedMemory.includes(note.id)} onChange={(event) => setSelectedMemory(event.target.checked ? [...selectedMemory, note.id] : selectedMemory.filter((id) => id !== note.id))} /><span className="break-words">{note.text}</span></label>)}</fieldset>
      {sources.length > 0 && <fieldset className="flex flex-col gap-2" disabled={saving || operation !== null}><legend className="mb-2 text-aui-sm font-medium">{words.sources}</legend>{sources.map((sourceName) => <label key={sourceName} className="flex min-h-10 items-center gap-2 text-aui-sm"><input type="checkbox" checked={selectedSources.includes(sourceName)} disabled={!selectedSources.includes(sourceName) && selectedSources.length >= 16} onChange={(event) => setSelectedSources(event.target.checked ? [...selectedSources, sourceName] : selectedSources.filter((name) => name !== sourceName))} />{sourceName}</label>)}</fieldset>}
      <label className="flex min-h-10 items-start gap-2 text-aui-sm"><input aria-label={words.archive} type="checkbox" className="mt-1" checked={archive} disabled={saving || operation !== null} onChange={(event) => setArchive(event.target.checked)} /><span>{words.archive}<span className="mt-1 block text-aui-xs text-aui-muted">{words.archiveHint}</span></span></label>
      {operation && <section role="status" className="flex flex-col gap-2"><h3 className="text-aui-sm font-medium">{words.status}: {operation.state === "ready" ? words.ready : operation.state === "failed" ? words.stopped : words.preparing}</h3><p className="text-aui-xs text-aui-muted">{words.result(operation.projects, operation.agents, operation.memory)}</p><Problem message={operation.error ?? null} /></section>}
      <Problem message={problem} />
      <div className="flex flex-wrap justify-end gap-2"><button type="button" className={button} onClick={onClose}>{words.cancel}</button><button type="submit" className={primary} disabled={saving || !name.trim() || notes === null}>{operation ? words.retry : words.create}</button></div>
    </form>
  </Inspector>;
}
