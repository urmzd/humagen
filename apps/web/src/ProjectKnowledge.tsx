import { defaultCopy } from "@warp/components";
import { useEffect, useRef, useState } from "react";
import { ApiError, api, coworking, type ProjectKnowledge, type ProjectView, type Source } from "./api.ts";
import { titleOf } from "./projects.ts";
import { type Flash, FlashNote, PickMany, Problem, primary, SkeletonRows, useFlash } from "./ui.tsx";

const words = defaultCopy.projects.knowledge;
const heading = "text-aui-base font-medium text-aui-fg";

/** The most knowledge sources a project names. */
export const MAX_KNOWLEDGE = 8;

const same = (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i]);

/**
 * Where a source a run is grounded in comes from, in words: this project, a
 * project above it by its title, or the workspace.
 */
export function givenFrom(from: string, project: string, titles: Record<string, string>): string {
  if (from === project) return words.fromHere;
  if (from === "") return words.fromWorkspace;
  return words.from(titles[from] ?? from);
}

/**
 * A project's knowledge sources, as read: what it names, editable when
 * `editable`, and, read only, what a run filed here is grounded in. Draws
 * nothing of its own state, so it renders the same on a server.
 */
export function KnowledgeLists({
  project,
  view,
  titles,
  sources,
  draft,
  editable,
  fileOwned,
  saving = false,
  problem = null,
  flash = null,
  onChange = () => {},
  onSave = () => {},
}: {
  /** "" is the workspace. */
  project: string;
  view: ProjectKnowledge;
  /** The titles of the projects above, by name, for where a source comes from. */
  titles: Record<string, string>;
  /** The workspace's sources, to choose from; null while they are read. */
  sources: Source[] | null;
  /** What the editor holds. */
  draft: string[];
  editable: boolean;
  /** The platform file sets the list. */
  fileOwned: boolean;
  saving?: boolean;
  problem?: string | null;
  flash?: Flash | null;
  onChange?: (next: string[]) => void;
  onSave?: () => void;
}) {
  const full = draft.length >= MAX_KNOWLEDGE;
  const all = (sources ?? []).map((s) => ({ value: s.name, label: s.name, hint: s.description }));
  // At the cap, only what is named is offered, so nothing more can be added.
  const options = full ? all.filter((o) => draft.includes(o.value)) : all;
  const dirty = !same(draft, view.knowledge);
  return (
    <div className="flex flex-col gap-5">
      <section className="flex flex-col gap-2" aria-label={words.heading}>
        <h2 className={heading}>{words.heading}</h2>
        <p className="text-aui-xs text-aui-muted">{words.hint}</p>
        {fileOwned && <p className="text-aui-xs text-aui-muted">{words.managed}</p>}
        {editable && !fileOwned ? (
          <>
            <PickMany label={words.heading} add={words.add} disabled={saving} empty={words.none} value={draft} options={options} onChange={onChange} />
            {full && <p className="text-aui-xs text-aui-muted">{words.full}</p>}
            <div className="flex items-center gap-3">
              <button type="button" className={primary} disabled={saving || !dirty} onClick={onSave}>
                {saving ? defaultCopy.projects.saving : words.save}
              </button>
              <FlashNote flash={flash} align="left" />
            </div>
          </>
        ) : view.knowledge.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{words.none}</p>
        ) : (
          <ul aria-label={words.heading} className="flex flex-col">
            {view.knowledge.map((k) => (
              <li key={k} className="border-aui-border border-t py-2 text-aui-sm text-aui-fg first:border-t-0">
                {k}
              </li>
            ))}
          </ul>
        )}
        <Problem message={problem} />
      </section>
      <section className="flex flex-col gap-2" aria-label={words.given}>
        <h3 className="text-aui-sm font-medium text-aui-fg">{words.given}</h3>
        <p className="text-aui-xs text-aui-muted">{words.givenHint}</p>
        {view.given.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{words.noneGiven}</p>
        ) : (
          <ol aria-label={words.given} className="flex flex-col">
            {view.given.map((g) => (
              <li key={g.name} className="flex items-baseline justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
                <span className="min-w-0 truncate text-aui-sm text-aui-fg">{g.name}</span>
                <span className="shrink-0 text-aui-xs text-aui-muted">{givenFrom(g.from, project, titles)}</span>
              </li>
            ))}
          </ol>
        )}
      </section>
    </div>
  );
}

/**
 * The Knowledge section of a project's Context tab. A reader without
 * `knowledge:read` is shown nothing, and nothing is asked for them. An owner
 * who reads knowledge chooses from the workspace's sources and saves the list
 * as a whole from the version read. The record does not say which fields the
 * platform file sets, so a list the file sets is known from a refusal that is
 * not about a change made since.
 */
export function ProjectKnowledgeSection({
  project,
  ancestors,
  workspaceName,
  version,
  canRead,
  canWrite,
  onSaved = () => {},
}: {
  /** "" is the workspace. */
  project: string;
  /** The projects above, for their titles. */
  ancestors: ProjectView[];
  workspaceName: string;
  /** The project's version as last read: the list is read again when it changes. */
  version: number;
  /** Holds `knowledge:read`. */
  canRead: boolean;
  /** May change the project: its owner, governing it where that is needed. */
  canWrite: boolean;
  /** The list was saved, so the project's version moved. */
  onSaved?: () => void;
}) {
  const [view, setView] = useState<ProjectKnowledge | null>(null);
  const [draft, setDraft] = useState<string[]>([]);
  const [sources, setSources] = useState<Source[] | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [fileOwned, setFileOwned] = useState(false);
  const { flash, say } = useFlash();
  const editable = canRead && canWrite;
  // What was last read, so a read caused by a change to another of the
  // project's fields keeps an edit in progress. A read that finds the list
  // itself changed shows the latest and says so, so a save never writes over
  // a change it did not see.
  const read = useRef<string[] | null>(null);
  const editing = useRef<string[]>([]);
  editing.current = draft;
  const show = (v: ProjectKnowledge, keepEdit: boolean) => {
    const before = read.current;
    read.current = v.knowledge;
    setView(v);
    const edited = keepEdit && before !== null && !same(editing.current, before);
    if (edited && same(v.knowledge, before)) return;
    if (edited) setProblem(words.stale);
    setDraft(v.knowledge);
  };

  // biome-ignore lint/correctness/useExhaustiveDependencies: version is the signal to read again
  useEffect(() => {
    if (!canRead) return;
    let live = true;
    coworking
      .knowledge(project)
      .then((v) => live && show(v, true))
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.readFailed));
    return () => {
      live = false;
    };
  }, [project, canRead, version]);

  useEffect(() => {
    let live = true;
    if (editable) {
      api
        .sources()
        .then((s) => live && setSources(s))
        .catch(() => live && setSources([]));
    }
    return () => {
      live = false;
    };
  }, [editable]);

  if (!canRead) return null;
  if (!view) return problem ? <Problem message={problem} /> : <SkeletonRows count={2} />;

  const titles: Record<string, string> = Object.fromEntries(ancestors.map((a) => [a.name, titleOf(a, workspaceName)]));

  const save = () => {
    if (saving) return;
    const sent = view.version;
    setSaving(true);
    setProblem(null);
    coworking
      .setKnowledge(project, sent, draft)
      .then((v) => {
        show(v, false);
        say(words.saved);
        onSaved();
      })
      .catch((err: unknown) => {
        if (!(err instanceof ApiError) || err.status !== 409) {
          setProblem(err instanceof ApiError ? err.message : words.failed);
          return;
        }
        // Read the latest. Still at the version sent, the refusal was the platform file's.
        coworking
          .knowledge(project)
          .then((v) => {
            show(v, false);
            if (v.version === sent) setFileOwned(true);
            else setProblem(words.stale);
          })
          .catch(() => setProblem(err.message));
      })
      .finally(() => setSaving(false));
  };

  return (
    <KnowledgeLists
      project={project}
      view={view}
      titles={titles}
      sources={sources}
      draft={draft}
      editable={editable}
      fileOwned={fileOwned}
      saving={saving}
      problem={problem}
      flash={flash}
      onChange={setDraft}
      onSave={save}
    />
  );
}
