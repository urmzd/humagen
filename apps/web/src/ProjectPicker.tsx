import { defaultCopy } from "@warp/components";
import { CheckIcon, ChevronDownIcon, FolderIcon, FolderOpenIcon, FolderPlusIcon, MessageIcon } from "@warp/render-web";
import { useEffect, useRef, useState } from "react";
import { ApiError, api, type ProjectKindSpec, type ProjectView } from "./api.ts";
import { above, kindWord, matches, titleOf } from "./projects.ts";
import { button, field, Label, Popover, primary, Problem, SearchField } from "./ui.tsx";

const words = defaultCopy.projects;
const choice = "flex min-h-11 w-full items-center gap-3 rounded-aui px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover";

/** Where a project sits, in words under its title: "Account · in Field › Globex". */
export function placeOf(p: ProjectView, kinds: ProjectKindSpec[]): string {
  const path = above(p);
  return [kindWord(p.kind, kinds), p.account?.customer, path.length > 0 ? words.within(path) : null].filter(Boolean).join(" · ");
}

export function ProjectChoices({ projects, kinds = [], value, canCreate = true, onChange, onCreated, close }: {
  /** The tree as GET /api/projects lists it, or null while it loads. */
  projects: ProjectView[] | null;
  /** The workspace's own kinds, so a project of one is called by its title. */
  kinds?: ProjectKindSpec[];
  value: string | null;
  /** May make a project here. */
  canCreate?: boolean;
  onChange: (name: string) => void;
  onCreated: (project: ProjectView) => void;
  close: () => void;
}) {
  const [query, setQuery] = useState("");
  const [creating, setCreating] = useState(false);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  useEffect(() => { input.current?.focus(); }, [creating]);

  const select = (name: string) => { onChange(name); close(); };
  const create = async () => {
    if (saving || !name.trim()) return;
    setSaving(true);
    setProblem(null);
    try {
      const made = await api.createProject({ name: name.trim(), ...(description.trim() ? { description: description.trim() } : {}) });
      onCreated(made);
      select(made.name);
    } catch (err) {
      setProblem(err instanceof ApiError && err.status === 409
        ? `A project named "${name.trim()}" already exists. Choose another name.`
        : err instanceof ApiError ? err.message : words.failed.create);
    } finally {
      setSaving(false);
    }
  };

  if (creating) return (
    // This panel lives inside the composer's form. Enter creates the project
    // without submitting the message, and there is no nested form.
    <div className="flex flex-col gap-3 p-2" onKeyDown={(e) => {
      if (e.key === "Enter" && e.target instanceof HTMLInputElement && !e.nativeEvent.isComposing) {
        e.preventDefault();
        if (input.current?.reportValidity()) void create();
      }
    }}>
      <h2 className="text-aui-sm font-medium">{words.form.heading}</h2>
      <p className="text-aui-xs text-aui-muted">Keep related conversations and shared memory together. It starts at the top level, as a team.</p>
      <Label text={words.form.name} hint={words.form.nameHint}>
        <input ref={input} className={field} value={name} onChange={(e) => setName(e.target.value)} required maxLength={63}
          pattern="[A-Za-z0-9][A-Za-z0-9 _\-]*" title={words.form.nameHint} disabled={saving} />
      </Label>
      <Label text={words.form.description}>
        <input className={field} value={description} maxLength={500} onChange={(e) => setDescription(e.target.value)} disabled={saving} />
      </Label>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} disabled={saving} onClick={() => { setCreating(false); setProblem(null); }}>Back</button>
        <button type="button" className={primary} disabled={saving || !name.trim()} onClick={() => { if (input.current?.reportValidity()) void create(); }}>
          {saving ? words.creating : words.createProject}
        </button>
      </div>
    </div>
  );

  const shown = projects?.filter((p) => matches(p, query));
  return (
    <div className="flex flex-col gap-1" onKeyDown={(e) => {
      if (e.key === "Enter" && e.target instanceof HTMLInputElement) e.preventDefault();
    }}>
      <div className="p-1">
        <SearchField label={words.search} value={query} onChange={setQuery} inputRef={input} />
      </div>
      <button type="button" className={choice} aria-pressed={value === ""} onClick={() => select("")}>
        <MessageIcon size={18} className="text-aui-muted" /><span className="min-w-0 flex-1"><span className="block">Projectless</span><span className="block text-aui-xs text-aui-muted">Start without a project</span></span>
        {value === "" && <CheckIcon size={14} />}
      </button>
      <div className="my-1 h-px bg-aui-border" />
      <ul className="aui-scroll max-h-56 overflow-y-auto" aria-label="Projects">
        {shown?.map((p) => <li key={p.name}>
          <button type="button" className={choice} aria-pressed={p.name === value} onClick={() => select(p.name)}>
            {p.name === value ? <FolderOpenIcon size={18} /> : <FolderIcon size={18} className="text-aui-muted" />}
            <span className="min-w-0 flex-1">
              <span className="block truncate">{titleOf(p)}</span>
              <span className="block truncate text-aui-xs text-aui-muted">{placeOf(p, kinds)}</span>
            </span>
            {p.name === value && <CheckIcon size={14} />}
          </button>
        </li>)}
      </ul>
      {(!projects || shown?.length === 0) && <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
        {!projects ? "Loading projects…" : projects.length === 0 ? (canCreate ? "No projects yet. Create your first one below." : words.empty) : words.noMatch}
      </p>}
      {canCreate && <>
        <div className="my-1 h-px bg-aui-border" />
        <button type="button" className={choice} onClick={() => { setName(query.trim()); setCreating(true); }}><FolderPlusIcon size={18} />{words.createProject}</button>
      </>}
    </div>
  );
}

/** Choose where new work belongs without leaving or clearing the message. */
export function ProjectPicker({ projects, kinds, value, canCreate = true, onChange, onCreated }: {
  projects: ProjectView[] | null;
  /** The workspace's own kinds, so a project of one is called by its title. */
  kinds?: ProjectKindSpec[];
  /** null is untouched; an empty string explicitly chooses projectless work. */
  value: string | null;
  canCreate?: boolean;
  onChange: (name: string) => void;
  onCreated: (project: ProjectView) => void;
}) {
  const chosen = value ? projects?.find((p) => p.name === value) : undefined;
  const label = value === null ? "Select project" : value === "" ? "Projectless" : chosen ? titleOf(chosen) : value;
  return <div className="warp-project-picker min-w-0"><Popover label={value === null ? "Select project" : `Select project: ${label}`} up
    className="inline-flex h-8 max-w-32 items-center gap-1 rounded-aui px-1.5 text-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent sm:max-w-40"
    button={<>{value === "" ? <MessageIcon size={16} /> : value ? <FolderOpenIcon size={16} /> : <FolderIcon size={16} />}<span className="truncate">{label}</span><ChevronDownIcon size={14} className="hidden shrink-0 sm:block" /></>}>
    {(close) => <ProjectChoices projects={projects} kinds={kinds} value={value} canCreate={canCreate} onChange={onChange} onCreated={onCreated} close={close} />}
  </Popover></div>;
}
