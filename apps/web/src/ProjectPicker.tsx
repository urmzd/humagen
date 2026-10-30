import { defaultCopy } from "@humagen/components";
import { CheckIcon, FolderPlusIcon, ProjectIcon } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import { ApiError, api, type ProjectKindSpec, type ProjectView } from "./api.ts";
import { above, kindWord, matches, titleOf } from "./projects.ts";
import { button, field, Label, Problem, primary, SearchField } from "./ui.tsx";

const words = defaultCopy.projects;
const choice =
  "flex min-h-11 w-full items-center gap-3 rounded-aui px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover";

/** Where a project sits, in words under its title: "Account in Field › Globex". */
export function placeOf(p: ProjectView, kinds: ProjectKindSpec[]): string {
  const path = above(p);
  return [kindWord(p.kind, kinds), p.account?.customer, path.length > 0 ? words.within(path) : null]
    .filter(Boolean)
    .join(" ");
}

export function ProjectChoices({
  projects,
  kinds = [],
  value,
  personalProject,
  canCreate = true,
  onChange,
  onCreated,
  close,
}: {
  /** The tree as GET /api/projects lists it, or null while it loads. */
  projects: ProjectView[] | null;
  /** The workspace's own kinds, so a project of one is called by its title. */
  kinds?: ProjectKindSpec[];
  value: string | null;
  personalProject?: string;
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
  useEffect(() => {
    input.current?.focus();
  }, [creating]);

  const select = (name: string) => {
    onChange(name);
    close();
  };
  const create = async () => {
    if (saving || !name.trim()) return;
    setSaving(true);
    setProblem(null);
    try {
      const made = await api.createProject({
        name: name.trim(),
        ...(description.trim() ? { description: description.trim() } : {}),
      });
      onCreated(made);
      select(made.name);
    } catch (err) {
      setProblem(
        err instanceof ApiError && err.status === 409
          ? `A project named "${name.trim()}" already exists. Choose another name.`
          : err instanceof ApiError
            ? err.message
            : words.failed.create,
      );
    } finally {
      setSaving(false);
    }
  };

  if (creating)
    return (
      // This panel lives inside the composer's form. Enter creates the project
      // without submitting the message, and there is no nested form.
      <div
        className="flex flex-col gap-3 p-2"
        onKeyDown={(e) => {
          if (e.key === "Escape") {
            e.preventDefault();
            e.stopPropagation();
            setCreating(false);
            setProblem(null);
            return;
          }
          if (
            e.key === "Enter" &&
            e.target instanceof HTMLInputElement &&
            !e.nativeEvent.isComposing
          ) {
            e.preventDefault();
            if (input.current?.reportValidity()) void create();
          }
        }}
      >
        <h2 className="text-aui-sm font-medium">{words.form.heading}</h2>
        <p className="text-aui-xs text-aui-muted">
          Keep related conversations and shared memory together. It starts at the top level, as a
          team.
        </p>
        <Label text={words.form.name} hint={words.form.nameHint}>
          <input
            ref={input}
            className={field}
            value={name}
            onChange={(e) => setName(e.target.value)}
            required
            maxLength={63}
            pattern="[A-Za-z0-9][A-Za-z0-9 _\-]*"
            title={words.form.nameHint}
            disabled={saving}
          />
        </Label>
        <Label text={words.form.description}>
          <input
            className={field}
            value={description}
            maxLength={500}
            onChange={(e) => setDescription(e.target.value)}
            disabled={saving}
          />
        </Label>
        <Problem message={problem} />
        <div className="flex justify-end gap-2">
          <button
            type="button"
            className={button}
            disabled={saving}
            onClick={() => {
              setCreating(false);
              setProblem(null);
            }}
          >
            Back
          </button>
          <button
            type="button"
            className={primary}
            disabled={saving || !name.trim()}
            onClick={() => {
              if (input.current?.reportValidity()) void create();
            }}
          >
            {saving ? words.creating : words.createProject}
          </button>
        </div>
      </div>
    );

  const shown = projects?.filter((p) => p.name !== personalProject && matches(p, query));
  return (
    <div
      className="flex flex-col gap-1"
      onKeyDown={(e) => {
        if (e.nativeEvent.isComposing) return;
        if (e.key === "Enter" && e.target instanceof HTMLInputElement) e.preventDefault();
        if (e.key === "ArrowDown" || e.key === "ArrowUp") {
          const buttons = [
            ...e.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"),
          ];
          if (!buttons.length) return;
          e.preventDefault();
          const at = buttons.indexOf(document.activeElement as HTMLButtonElement);
          if (at === 0 && e.key === "ArrowUp") input.current?.focus();
          else
            buttons[
              at < 0
                ? e.key === "ArrowDown"
                  ? 0
                  : buttons.length - 1
                : (at + (e.key === "ArrowDown" ? 1 : buttons.length - 1)) % buttons.length
            ]?.focus();
        }
      }}
    >
      <div className="p-1">
        <SearchField label={words.search} value={query} onChange={setQuery} inputRef={input} />
      </div>
      {personalProject && (
        <button
          type="button"
          className={choice}
          aria-pressed={value === personalProject}
          onClick={() => select(personalProject)}
        >
          <ProjectIcon size={18} />
          <span className="min-w-0 flex-1">{words.personalTitle}</span>
          {value === personalProject && <CheckIcon size={14} />}
        </button>
      )}
      <button
        type="button"
        className={choice}
        aria-pressed={value === ""}
        onClick={() => select("")}
      >
        <ProjectIcon size={18} className="text-aui-muted" />
        <span className="min-w-0 flex-1">
          <span className="block">{words.companyName}</span>
          <span className="block text-aui-xs text-aui-muted">{words.companyStart}</span>
        </span>
        {value === "" && <CheckIcon size={14} />}
      </button>
      <div className="my-1 h-px bg-aui-border" />
      <ul className="aui-scroll max-h-56 overflow-y-auto" aria-label="Projects">
        {shown?.map((p) => (
          <li key={p.name}>
            <button
              type="button"
              className={choice}
              aria-pressed={p.name === value}
              onClick={() => select(p.name)}
            >
              <ProjectIcon size={18} className="text-aui-muted" />
              <span className="min-w-0 flex-1">
                <span className="block truncate">{titleOf(p)}</span>
                <span className="block truncate text-aui-xs text-aui-muted">
                  {placeOf(p, kinds)}
                </span>
              </span>
              {p.name === value && <CheckIcon size={14} />}
            </button>
          </li>
        ))}
      </ul>
      {(!projects || shown?.length === 0) && (
        <p role="status" className="px-2.5 py-3 text-aui-xs text-aui-muted">
          {!projects
            ? "Loading projects…"
            : projects.length === 0
              ? canCreate
                ? "No projects yet. Create your first one below."
                : words.empty
              : words.noMatch}
        </p>
      )}
      {canCreate && (
        <>
          <div className="my-1 h-px bg-aui-border" />
          <button
            type="button"
            className={choice}
            onClick={() => {
              setName(query.trim());
              setCreating(true);
            }}
          >
            <FolderPlusIcon size={18} />
            {words.createProject}
          </button>
        </>
      )}
    </div>
  );
}

/** Choose where new work belongs without leaving or clearing the message. */
export function ProjectPicker({
  projects,
  kinds,
  value,
  personalProject,
  channel,
  canCreate = true,
  onChange,
  onCreated,
}: {
  projects: ProjectView[] | null;
  kinds?: ProjectKindSpec[];
  value: string | null;
  personalProject?: string;
  channel?: string;
  canCreate?: boolean;
  onChange: (name: string) => void;
  onCreated: (project: ProjectView) => void;
}) {
  const [open, setOpen] = useState(false);
  const chosen = value ? projects?.find((project) => project.name === value) : undefined;
  const project =
    value === null
      ? "Select project"
      : value === ""
        ? words.companyName
        : value === personalProject
          ? words.personalTitle
          : chosen
            ? titleOf(chosen)
            : value;
  const label = channel && channel !== "general" ? `${project} / #${channel}` : project;
  return (
    <div className="humagen-project-picker min-w-0">
      <button
        type="button"
        aria-label={value === null ? "Select project" : `Select project: ${label}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen(true)}
        className="inline-flex min-h-9 pointer-coarse:min-h-11 max-w-full items-center gap-2 rounded-aui px-1.5 text-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <ProjectIcon size={18} />
        <span className="truncate">{label}</span>
      </button>
      <Inspector open={open} title={defaultCopy.chat.chooseProject} onClose={() => setOpen(false)}>
        <ProjectChoices
          projects={projects}
          kinds={kinds}
          value={value}
          personalProject={personalProject}
          canCreate={canCreate}
          onChange={onChange}
          onCreated={onCreated}
          close={() => setOpen(false)}
        />
      </Inspector>
    </div>
  );
}
