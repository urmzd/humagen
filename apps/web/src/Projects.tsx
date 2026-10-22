import { defaultCopy } from "@warp/components";
import { FolderIcon, FolderPlusIcon } from "@warp/render-web";
import { type FormEvent, type MouseEvent, type ReactNode, useCallback, useEffect, useState } from "react";
import {
  ApiError,
  api,
  type Note,
  type ProjectAction,
  type ProjectAccount,
  type ProjectDetail,
  type ProjectKind,
  type ProjectKindSpec,
  type ProjectView,
  type Revision,
  type ScheduleView,
  type ThreadSummary,
} from "./api.ts";
import { ScopeLimits } from "./Billing.tsx";
import { inWords } from "./cron.ts";
import { ProjectAgentsTab } from "./ProjectAgents.tsx";
import type { Source } from "./mail.ts";
import { ProjectHistory, ProjectInbox, ProjectLimits, ProjectPeople } from "./ProjectTabs.tsx";
import {
  above,
  accountOf,
  groupWord,
  INDENT_PX,
  kindsOf,
  kindsUnder,
  kindWord,
  likeOf,
  MAX_DEPTH,
  nameFrom,
  narrowAt,
  type Place,
  titleOf,
  treeOf,
  updateOf,
  WORKSPACE,
} from "./projects.ts";
import { deliverOptions, recapAgent, recapName, weeklyRecap } from "./recap.ts";
import { ChannelsTab } from "./Room.tsx";
import { href, PROJECT_TABS, type ProjectTab } from "./route.ts";
import {
  AgentMark,
  agentOptions,
  ago,
  button,
  FlashNote,
  field,
  Label,
  Menu,
  Page,
  Problem,
  primary,
  rows,
  SearchField,
  Segmented,
  SkeletonRows,
  Switch,
  Tabs,
  useFlash,
} from "./ui.tsx";

const words = defaultCopy.projects;
const scheduleWords = defaultCopy.schedules;
// A rule over each section parts it from the one before. The first in its column has none: what is above it draws its own.
const section = "flex flex-col gap-2 border-aui-border border-t pt-5 first:border-t-0 first:pt-0";
const heading = "text-aui-base font-medium text-aui-fg";
const fail = (set: (m: string) => void, fallback: string) => (e: unknown) => set(e instanceof ApiError ? e.message : fallback);

/** A plain link: a plain click goes there in place, a modified one opens a new tab. */
function Go({ to, onGo, className, children }: { to: string; onGo: () => void; className?: string; children: ReactNode }) {
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onGo();
  };
  return (
    <a href={to} onClick={go} className={className}>
      {children}
    </a>
  );
}

const projectHref = (name: string) => href({ page: "project", name, tab: "inbox" });

/** Whether the viewport is a phone's, where the tree stops nesting. Follows the window as it resizes. */
export function useNarrow(): boolean {
  const read = () => typeof window !== "undefined" && narrowAt(window.innerWidth);
  const [narrow, setNarrow] = useState(read);
  useEffect(() => {
    const on = () => setNarrow(typeof window !== "undefined" && narrowAt(window.innerWidth));
    window.addEventListener("resize", on);
    return () => window.removeEventListener("resize", on);
  }, []);
  return narrow;
}

/** What is happening in a project, what waits, and how much it holds, most pressing first. */
export function factsOf(p: ProjectView): string {
  return [
    p.active > 0 && words.count.running(p.active),
    p.proposed > 0 && words.count.review(p.proposed),
    words.count.tasks(p.conversations),
    p.active_at && words.count.active(ago(p.active_at)),
  ]
    .filter(Boolean)
    .join(" · ");
}

/** A project in words under its title: its kind, its customer, and where it sits when the tree does not show it. */
function aboutOf(p: ProjectView, path: string[], kinds: ProjectKindSpec[]): string {
  return [kindWord(p.kind, kinds), p.account?.customer, path.length > 0 ? words.within(path) : null].filter(Boolean).join(" · ");
}

/** The workspace's own kinds, as a project page reads them: from the workspace, first among its ancestors. */
function kindsIn(detail: ProjectDetail): ProjectKindSpec[] {
  const workspace = detail.project.name === "" ? detail.project : detail.ancestors.find((a) => a.name === "");
  return workspace?.kinds ?? [];
}

function ProjectRow({
  to,
  title,
  about,
  facts,
  indent,
  onOpen,
}: {
  to: string;
  title: string;
  about: string;
  facts: string;
  indent: number;
  onOpen: () => void;
}) {
  return (
    <li data-indent={indent} style={indent > 0 ? { paddingLeft: indent * INDENT_PX } : undefined}>
      <Go
        to={to}
        onGo={onOpen}
        className="flex min-h-11 w-full flex-wrap items-center gap-x-3 gap-y-0.5 rounded-aui px-2 py-2 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        <FolderIcon size={18} className="shrink-0 text-aui-muted" />
        <span className="min-w-0 flex-1">
          <span className="block truncate text-aui-sm font-medium text-aui-fg">{title}</span>
          <span className="block truncate text-aui-xs text-aui-muted">{about}</span>
        </span>
        <span className="shrink-0 text-aui-2xs text-aui-muted tabular-nums max-sm:w-full max-sm:pl-[30px]">{facts}</span>
      </Go>
    </li>
  );
}

/**
 * Every project, as a tree grouped by kind: Company-wide, then Teams, then
 * Accounts, then the workspace's own kinds by title. The workspace itself
 * heads Company-wide. A project is opened from
 * here, and made from here with an inline form.
 */
export function ProjectsPage({
  projects,
  root,
  workspaceName,
  q = "",
  kind,
  onFilter,
  canCreate,
  canGovern,
  narrow: forced,
  onOpen,
  onCreated,
}: {
  /** Null while they load. */
  projects: ProjectView[] | null;
  /** The workspace's own view, when it has been read. */
  root: ProjectView | null;
  workspaceName: string;
  q?: string;
  kind?: ProjectKind;
  onFilter: (q: string, kind?: ProjectKind) => void;
  canCreate: boolean;
  /** May make company-wide projects. */
  canGovern: boolean;
  /** Lays the tree out for a phone. Measured from the window when left out. */
  narrow?: boolean;
  onOpen: (name: string) => void;
  onCreated: (project: ProjectView) => void;
}) {
  const measured = useNarrow();
  const narrow = forced ?? measured;
  const [adding, setAdding] = useState(false);
  const kinds = root?.kinds ?? [];
  const groups = projects ? treeOf(projects, { narrow, q, kind, kinds }) : [];
  const showRoot = root !== null && !q.trim() && (!kind || kind === "company");
  const shown = showRoot && !groups.some((g) => g.kind === "company") ? [{ kind: "company" as const, title: words.group.company ?? "", rows: [] }, ...groups] : groups;

  return (
    <Page
      title={words.title}
      lede={words.lede}
      actions={
        canCreate && !adding ? (
          <button type="button" className={primary} onClick={() => setAdding(true)}>
            <FolderPlusIcon size={16} /> {words.newProject}
          </button>
        ) : undefined
      }
    >
      {adding && (
        <NewProject
          places={projects ?? []}
          kinds={kinds}
          canGovern={canGovern}
          onCancel={() => setAdding(false)}
          onCreated={(p) => {
            setAdding(false);
            onCreated(p);
          }}
        />
      )}
      <div className="flex flex-wrap items-center gap-2">
        <SearchField label={words.search} value={q} onChange={(next) => onFilter(next, kind)} className="w-full min-w-0 sm:w-auto sm:max-w-xs sm:flex-1" />
        <Segmented
          label={words.kindFilter}
          value={kind ?? "all"}
          options={[{ id: "all", label: words.anyKind }, ...kindsOf(kinds, projects ?? []).map((k) => ({ id: k, label: groupWord(k, kinds) }))]}
          onChange={(next) => onFilter(q, next === "all" ? undefined : (next as ProjectKind))}
        />
      </div>
      {projects === null ? (
        <SkeletonRows count={5} height="2.75rem" />
      ) : shown.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{projects.length > 0 ? words.noMatch : canCreate ? words.emptyCreate : words.empty}</p>
      ) : (
        shown.map((g) => (
          <section key={g.kind} aria-labelledby={`projects-${g.kind}`} className="flex flex-col gap-1.5">
            <h2 id={`projects-${g.kind}`} className="text-aui-xs font-medium text-aui-muted">
              {g.title}
            </h2>
            <ul className={rows}>
              {g.kind === "company" && showRoot && root && (
                <ProjectRow
                  to={projectHref("")}
                  title={workspaceName}
                  about={`${words.workspace} · ${words.workspaceHint}`}
                  facts={factsOf(root)}
                  indent={0}
                  onOpen={() => onOpen("")}
                />
              )}
              {g.rows.map((r) => (
                <ProjectRow
                  key={r.project.name}
                  to={projectHref(r.project.name)}
                  title={titleOf(r.project)}
                  about={aboutOf(r.project, r.path, kinds)}
                  facts={factsOf(r.project)}
                  indent={r.indent}
                  onOpen={() => onOpen(r.project.name)}
                />
              ))}
            </ul>
          </section>
        ))
      )}
    </Page>
  );
}

/**
 * The form that makes a project: a title, a name that never changes, its kind
 * and where it sits. Under a fixed parent it makes a sub-project.
 */
export function NewProject({
  places,
  kinds: own = [],
  parent: fixed,
  canGovern,
  onCreated,
  onCancel,
}: {
  /** The projects it could sit under. */
  places: ProjectView[];
  /** The workspace's own kinds, offered after warp's where they may sit. */
  kinds?: ProjectKindSpec[];
  /** Where it goes, when that is decided: a sub-project's parent. */
  parent?: Place & { title?: string };
  canGovern: boolean;
  onCreated: (project: ProjectView) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState("");
  const [name, setName] = useState("");
  const [named, setNamed] = useState(false);
  const [parentName, setParentName] = useState(fixed?.name ?? "");
  const [want, setWant] = useState<ProjectKind>("team");
  const [description, setDescription] = useState("");
  const [customer, setCustomer] = useState("");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const parent: Place = fixed ?? places.find((p) => p.name === parentName) ?? WORKSPACE;
  const kinds = kindsUnder(parent, canGovern, own);
  const kind = kinds.includes(want) ? want : (kinds[0] ?? "team");
  const shownName = named ? name : nameFrom(title);
  const roomy = places.filter((p) => p.depth < MAX_DEPTH);
  // Someone who may not make company-wide projects is told why it is not offered where it could be.
  const governedHere = !canGovern && parent.kind === "company";

  const create = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const chosen = shownName.trim();
    const shownTitle = title.trim();
    const account = likeOf(kind, own) === "account" ? accountOf({ customer }) : undefined;
    setSaving(true);
    setProblem(null);
    api
      .createProject({
        name: chosen,
        ...(shownTitle && shownTitle !== chosen ? { title: shownTitle } : {}),
        ...(description.trim() ? { description: description.trim() } : {}),
        kind,
        ...(parent.name ? { parent: parent.name } : {}),
        ...(account ? { account } : {}),
      })
      .then(onCreated)
      .catch(fail(setProblem, words.failed.create))
      .finally(() => setSaving(false));
  };

  return (
    <form onSubmit={create} aria-label={words.form.heading} className="flex flex-col gap-3 rounded-aui-lg border border-aui-border p-3">
      <h2 className="text-aui-sm font-medium text-aui-fg">
        {fixed ? `${words.form.heading} ${words.within([titleOf(fixed)])}` : words.form.heading}
      </h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text={words.form.title} hint={words.form.titleHint}>
          <input
            // biome-ignore lint/a11y/noAutofocus: the form appears because the person asked to make a project
            autoFocus
            className={field}
            value={title}
            maxLength={80}
            disabled={saving}
            onChange={(e) => setTitle(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Escape") onCancel();
            }}
          />
        </Label>
        <Label text={words.form.name} hint={words.form.nameHint}>
          <input
            className={field}
            value={shownName}
            required
            maxLength={63}
            pattern="[A-Za-z0-9][A-Za-z0-9 _\-]*"
            title={words.form.nameHint}
            disabled={saving}
            onChange={(e) => {
              setName(e.target.value);
              setNamed(true);
            }}
          />
        </Label>
      </div>
      <div className="flex flex-wrap items-end gap-x-4 gap-y-3">
        {!fixed && (
          <Label text={words.form.parent}>
            <Menu
              label={words.form.parent}
              value={parentName}
              disabled={saving}
              options={[
                { value: "", label: words.form.topLevel },
                ...roomy.map((p) => ({
                  value: p.name,
                  label: titleOf(p),
                  hint: [kindWord(p.kind, own), above(p).length > 0 ? words.within(above(p)) : null].filter(Boolean).join(" · "),
                })),
              ]}
              onChange={setParentName}
            />
          </Label>
        )}
        <div className="flex flex-col gap-1">
          <span className="text-aui-xs font-medium text-aui-muted">{words.form.kind}</span>
          <Segmented
            label={words.form.kind}
            value={kind}
            options={kinds.map((k) => ({ id: k, label: kindWord(k, own) }))}
            onChange={setWant}
          />
        </div>
      </div>
      {governedHere && <p className="text-aui-2xs text-aui-muted">{words.form.governed}</p>}
      {kind === "account" && <p className="text-aui-2xs text-aui-muted">{words.form.accountMembersOnly}</p>}
      <Label text={words.form.description}>
        <input className={field} value={description} maxLength={500} disabled={saving} onChange={(e) => setDescription(e.target.value)} />
      </Label>
      {likeOf(kind, own) === "account" && (
        <Label text={words.form.customer}>
          <input className={field} value={customer} maxLength={120} disabled={saving} onChange={(e) => setCustomer(e.target.value)} />
        </Label>
      )}
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} disabled={saving} onClick={onCancel}>
          {words.cancel}
        </button>
        <button type="submit" className={primary} disabled={saving || !shownName.trim()}>
          {saving ? words.creating : words.createProject}
        </button>
      </div>
    </form>
  );
}

/**
 * What every agent in the project is sent, and what is waiting to become that.
 * An owner decides what is kept; a member's note is a proposal for them.
 */
function SharedMemory({
  project,
  canWrite,
  canPropose = false,
  onChanged,
}: {
  project: string;
  canWrite: boolean;
  canPropose?: boolean;
  onChanged: () => void;
}) {
  const [notes, setNotes] = useState<Note[]>([]);
  const [text, setText] = useState("");
  const [picked, setPicked] = useState<Set<string>>(() => new Set());
  const [problem, setProblem] = useState<string | null>(null);

  const load = () => {
    api
      .projectMemory(project)
      .then(setNotes)
      .catch(() => setNotes([]));
    onChanged();
  };
  // biome-ignore lint/correctness/useExhaustiveDependencies: reload when the project changes
  useEffect(() => {
    api
      .projectMemory(project)
      .then(setNotes)
      .catch(() => setNotes([]));
  }, [project]);

  const kept = notes.filter((n) => n.status === "approved");
  const proposed = notes.filter((n) => n.status === "pending");
  const failed = (fallback: string) => fail(setProblem, fallback);

  return (
    <section className={section}>
      <h2 className={heading}>Shared memory</h2>
      <p className="max-w-xl text-aui-sm text-aui-muted">
        Every agent answering in this project and below it is sent these, in every conversation. Keep them few and lasting.
      </p>
      <Problem message={problem} />
      {proposed.length > 0 && (
        <div className="flex flex-col gap-2 rounded-aui-lg border border-aui-border p-3">
          <h3 className="text-aui-sm font-medium text-aui-fg">Proposed ({proposed.length})</h3>
          <p className="text-aui-xs text-aui-muted">Not in use yet. Tick what the project should keep.</p>
          <ul>
            {proposed.map((n) => (
              <li key={n.id} className="border-aui-border border-t py-2 first:border-t-0">
                <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
                  <input
                    type="checkbox"
                    className="mt-1"
                    disabled={!canWrite}
                    checked={picked.has(n.id)}
                    onChange={(e) =>
                      setPicked((p) => {
                        const next = new Set(p);
                        if (e.target.checked) next.add(n.id);
                        else next.delete(n.id);
                        return next;
                      })
                    }
                  />
                  <span>{n.text}</span>
                </label>
              </li>
            ))}
          </ul>
          {canWrite && (
            <div className="flex flex-wrap justify-end gap-2">
              <button
                type="button"
                className={button}
                onClick={() =>
                  Promise.all(
                    proposed.filter((n) => picked.size === 0 || picked.has(n.id)).map((n) => api.removeProjectMemory(project, n.id)),
                  )
                    .then(() => {
                      setPicked(new Set());
                      load();
                    })
                    .catch(failed("Those could not be discarded."))
                }
              >
                {picked.size > 0 ? `Discard ${picked.size} Selected` : "Discard All"}
              </button>
              <button
                type="button"
                className={primary}
                disabled={picked.size === 0}
                onClick={() =>
                  api
                    .approveProjectMemory(project, [...picked])
                    .then(() => {
                      setPicked(new Set());
                      load();
                    })
                    .catch(failed("Those could not be kept."))
                }
              >
                Keep {picked.size > 0 ? `${picked.size} Selected` : "Selected"}
              </button>
            </div>
          )}
        </div>
      )}
      {kept.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">Nothing shared yet.</p>
      ) : (
        <ul>
          {kept.map((n) => (
            <li key={n.id} className="warp-enter flex items-start justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
              <span className="min-w-0">
                <span className="block text-aui-sm text-aui-fg">{n.text}</span>
                <span className="block text-aui-xs text-aui-muted">
                  {n.source === "person" ? `Written by ${n.by}` : "Proposed by the memory agent and approved"}, {ago(n.at)}
                </span>
              </span>
              {canWrite && (
                <button type="button" className={button} onClick={() => api.removeProjectMemory(project, n.id).then(load).catch(failed("That could not be removed."))}>
                  Forget
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      {canPropose && !canWrite && <p className="text-aui-xs text-aui-muted">{defaultCopy.project.proposeHint}</p>}
      {(canWrite || canPropose) && (
        <form
          className="flex gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setProblem(null);
            api
              .addProjectMemory(project, text)
              .then(() => {
                setText("");
                load();
              })
              .catch(failed("The note could not be saved."));
          }}
        >
          <input
            aria-label="Something everyone in this project should know"
            className={field}
            value={text}
            maxLength={300}
            required
            placeholder="A lasting fact, such as: The launch date is the first Tuesday of November."
            onChange={(e) => setText(e.target.value)}
          />
          <button type="submit" className={button}>
            Share
          </button>
        </form>
      )}
    </section>
  );
}

const TASK_PAGE = 20;

/**
 * The project's tasks, newest first, paged on the server. With its
 * sub-projects included, a task filed below says where.
 */
function ProjectTasks({
  name,
  hasChildren,
  tick,
  onOpenThread,
}: {
  name: string;
  hasChildren: boolean;
  tick?: number;
  onOpenThread: (id: string) => void;
}) {
  const [below, setBelow] = useState(false);
  const [page, setPage] = useState<{ threads: ThreadSummary[]; next: string | null } | null>(null);
  const [more, setMore] = useState(false);
  const address = name === "" ? "~" : name;
  const scope = useCallback((): { project?: string; within?: string } => (below ? { within: address } : { project: address }), [below, address]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the signal to read again
  useEffect(() => {
    let live = true;
    api
      .threadsPage({ limit: TASK_PAGE, ...scope() })
      .then((r) => live && setPage({ threads: r.threads, next: r.next ?? null }))
      .catch(() => live && setPage({ threads: [], next: null }));
    return () => {
      live = false;
    };
  }, [scope, tick]);

  const loadMore = () => {
    if (!page?.next || more) return;
    setMore(true);
    api
      .threadsPage({ limit: TASK_PAGE, before: page.next, ...scope() })
      .then((r) => setPage({ threads: [...page.threads, ...r.threads.filter((t) => !page.threads.some((s) => s.id === t.id))], next: r.next ?? null }))
      .catch(() => {})
      .finally(() => setMore(false));
  };

  return (
    <section className="flex flex-col gap-2" aria-label={words.tasks}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className={heading}>{words.tasks}</h2>
        {hasChildren && (
          <div className="min-w-0 max-w-xs">
            <Switch label={words.includeBelow} checked={below} onChange={setBelow} />
          </div>
        )}
      </div>
      {page === null ? (
        <SkeletonRows count={3} />
      ) : page.threads.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.noTasks}</p>
      ) : (
        <ul>
          {page.threads.map((t) => (
            <li key={t.id} className="warp-enter border-aui-border border-t first:border-t-0">
              <button
                type="button"
                onClick={() => onOpenThread(t.id)}
                className="flex w-full items-center gap-3 py-2 text-left text-aui-sm hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <AgentMark name={t.agents[t.agents.length - 1] ?? "?"} size={14} state={t.active > 0 ? "thinking" : t.failed > 0 ? "error" : "idle"} />
                <span className="min-w-0 flex-1 truncate text-aui-fg">{t.title}</span>
                <span className="shrink-0 text-aui-xs text-aui-muted">
                  {(t.project ?? "") !== name ? `${words.within([t.project || words.workspace])} · ` : ""}
                  {t.origin === "cron" || t.origin === "manual" ? "Scheduled, " : ""}
                  {t.active > 0 ? "In progress" : t.failed > 0 ? "Needs attention" : "Finished"} · {ago(t.updated_at)}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
      {page?.next && (
        <button type="button" className={`${button} self-start`} disabled={more} onClick={loadMore}>
          {words.moreTasks}
        </button>
      )}
    </section>
  );
}

/** What sits directly below a project, and the way to put something there. */
function SubProjects({
  detail,
  places,
  canCreate,
  canGovern,
  onOpen,
  onCreated,
}: {
  detail: ProjectDetail;
  places: ProjectView[];
  canCreate: boolean;
  canGovern: boolean;
  onOpen: (name: string) => void;
  onCreated: (project: ProjectView) => void;
}) {
  const [adding, setAdding] = useState(false);
  const here = detail.project;
  const kinds = kindsIn(detail);
  const room = here.depth < MAX_DEPTH && kindsUnder(here, canGovern, kinds).length > 0;
  return (
    <section className={section} aria-label={words.subProjects}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className={heading}>
          {words.subProjects} ({detail.children.length})
        </h2>
        {canCreate && room && !adding && (
          <button type="button" className={button} onClick={() => setAdding(true)}>
            <FolderPlusIcon size={16} /> {words.newSubProject}
          </button>
        )}
      </div>
      {adding && (
        <NewProject
          places={places}
          kinds={kinds}
          parent={here}
          canGovern={canGovern}
          onCancel={() => setAdding(false)}
          onCreated={(p) => {
            setAdding(false);
            onCreated(p);
          }}
        />
      )}
      {detail.children.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{here.depth >= MAX_DEPTH ? words.deepest : words.noSubProjects}</p>
      ) : (
        <ul className={rows}>
          {detail.children.map((c) => (
            <ProjectRow
              key={c.name}
              to={projectHref(c.name)}
              title={titleOf(c)}
              about={[aboutOf(c, [], kinds), c.children > 0 ? words.count.below(c.children) : null].filter(Boolean).join(" · ")}
              facts={factsOf(c)}
              indent={0}
              onOpen={() => onOpen(c.name)}
            />
          ))}
        </ul>
      )}
    </section>
  );
}

/** What runs in a project on a timer, and a way to add one. */
function Scheduled({
  name,
  title,
  schedules,
  agents,
  canSchedule,
  recap = false,
  onOpenThread,
  onSchedulesChanged,
  onChanged,
}: {
  name: string;
  /** The project's title, for the recap's question. */
  title: string;
  schedules: ScheduleView[];
  agents: Revision[];
  canSchedule: boolean;
  /** Offer "Add Weekly Recap" and where answers go: the Schedules tab. */
  recap?: boolean;
  onOpenThread: (id: string) => void;
  onSchedulesChanged: () => void;
  onChanged: () => void;
}) {
  const timed = schedules.filter((s) => (s.project ?? "") === name);
  const [draft, setDraft] = useState<{ name: string; expression: string; agent: string; text: string; deliver?: "inbox" | "project" }>({
    name: "",
    expression: "0 9 * * 1-5",
    agent: "",
    text: "",
  });
  const [adding, setAdding] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const agent = draft.agent || agents[0]?.spec.name || "";
  const failed = (fallback: string) => fail(setProblem, fallback);
  const recapper = recapAgent(agents.map((a) => a.spec.name));
  const hasRecap = timed.some((s) => s.name === recapName(name));
  const addRecap = () => {
    if (!recapper) return;
    setProblem(null);
    api
      .createSchedule(weeklyRecap(name, recapper, scheduleWords.recapText(title), scheduleWords.recapDescription))
      .then(() => {
        onSchedulesChanged();
        onChanged();
      })
      .catch(failed("The recap could not be added."));
  };

  return (
    <section className={section}>
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className={heading}>Scheduled ({timed.length})</h2>
        <span className="flex flex-wrap gap-2">
          {recap && canSchedule && recapper && !hasRecap && (
            <button type="button" className={button} onClick={addRecap}>
              {scheduleWords.addWeeklyRecap}
            </button>
          )}
          {canSchedule && agents.length > 0 && !adding && (
            <button type="button" className={button} onClick={() => setAdding(true)}>
              New Schedule
            </button>
          )}
        </span>
      </div>
      <p className="max-w-xl text-aui-sm text-aui-muted">Each time one fires, its run is filed in this project and is sent what the project knows.</p>
      {recap && recapper && (
        <p className="max-w-xl text-aui-xs text-aui-muted">{hasRecap ? scheduleWords.hasRecap : scheduleWords.recapHint(recapper)}</p>
      )}
      <Problem message={problem} />
      {adding && (
        <form
          className="flex flex-col gap-3 rounded-aui-lg border border-aui-border p-3"
          onSubmit={(e) => {
            e.preventDefault();
            setProblem(null);
            api
              .createSchedule({ ...draft, agent, project: name === "" ? "~" : name })
              .then(() => {
                setDraft({ ...draft, name: "", text: "" });
                setAdding(false);
                onSchedulesChanged();
                onChanged();
              })
              .catch(failed("The schedule could not be saved."));
          }}
        >
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <Label text="Name">
              <input className={field} value={draft.name} required onChange={(e) => setDraft({ ...draft, name: e.target.value })} />
            </Label>
            <Label text="Send to agent">
              <Menu label="Send to agent" value={agent} options={agentOptions(agents.map((a) => a.spec.name))} onChange={(v) => setDraft({ ...draft, agent: v })} />
            </Label>
            <Label text="How often" hint="minute hour day month weekday, in UTC">
              <input className={`${field} font-aui-mono`} value={draft.expression} required onChange={(e) => setDraft({ ...draft, expression: e.target.value })} />
            </Label>
          </div>
          <Label text="What to ask">
            <textarea className={`${field} min-h-14`} value={draft.text} required onChange={(e) => setDraft({ ...draft, text: e.target.value })} />
          </Label>
          {recap && (
            <Label text={scheduleWords.deliver} hint={scheduleWords.deliverHint}>
              <Menu
                label={scheduleWords.deliver}
                value={draft.deliver ?? ""}
                options={deliverOptions(name, scheduleWords, true)}
                onChange={(v) => setDraft({ ...draft, deliver: v === "inbox" || v === "project" ? v : undefined })}
              />
            </Label>
          )}
          <div className="flex justify-end gap-2">
            <button type="button" className={button} onClick={() => setAdding(false)}>
              Cancel
            </button>
            <button type="submit" className={primary}>
              Create Schedule
            </button>
          </div>
        </form>
      )}
      {timed.length === 0 && !adding ? (
        <p className="text-aui-sm text-aui-muted">Nothing runs on a timer here.</p>
      ) : (
        <ul>
          {timed.map((s) => (
            <li key={s.name} className="flex items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
              <span className="min-w-0">
                <span className="block text-aui-sm text-aui-fg">
                  {s.name}
                  <span className="pl-2 text-aui-xs text-aui-muted">
                    <span title={s.expression}>{inWords(s.expression)}</span> to {s.agent}
                    {s.paused ? ", paused" : ""}
                    {s.deliver === "project" ? `, ${scheduleWords.deliverProject.toLowerCase()}` : ""}
                  </span>
                </span>
                <span className="block truncate text-aui-xs text-aui-muted">{s.text}</span>
                {s.managed && <span className="block text-aui-xs text-aui-muted">{scheduleWords.managed}</span>}
              </span>
              {canSchedule && (
                <button
                  type="button"
                  className={button}
                  onClick={() =>
                    api
                      .runSchedule(s.name)
                      .then((run) => {
                        onSchedulesChanged();
                        onOpenThread(run.thread);
                      })
                      .catch(failed("The schedule could not be run."))
                  }
                >
                  Run Now
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** A project's title, description and, for an account, its customer, changed as a whole. */
function EditProject({ project, onSaved, onCancel }: { project: ProjectView; onSaved: () => void; onCancel: () => void }) {
  const [title, setTitle] = useState(project.title ?? "");
  const [description, setDescription] = useState(project.description ?? "");
  const [account, setAccount] = useState<ProjectAccount>(project.account ?? {});
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setProblem(null);
    api
      .updateProject(project.name, updateOf(project, { title, description, ...(project.kind === "account" ? { account } : {}) }))
      .then(onSaved)
      .catch((err: unknown) => {
        fail(setProblem, words.failed.save)(err);
        if (err instanceof ApiError && err.status === 409) onSaved();
      })
      .finally(() => setSaving(false));
  };
  return (
    <form onSubmit={save} aria-label={words.editProject} className="flex flex-col gap-3 rounded-aui-lg border border-aui-border p-3">
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text={words.form.title} hint={words.form.titleHint}>
          <input className={field} value={title} maxLength={80} disabled={saving} onChange={(e) => setTitle(e.target.value)} />
        </Label>
        <Label text={words.form.name} hint={words.form.nameHint}>
          <input className={field} value={project.name} disabled readOnly />
        </Label>
      </div>
      <Label text={words.form.description}>
        <input className={field} value={description} maxLength={500} disabled={saving} onChange={(e) => setDescription(e.target.value)} />
      </Label>
      {project.kind === "account" && (
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
          <Label text={words.form.customer}>
            <input className={field} value={account.customer ?? ""} maxLength={120} disabled={saving} onChange={(e) => setAccount({ ...account, customer: e.target.value })} />
          </Label>
          <Label text={words.form.externalId}>
            <input className={field} value={account.external_id ?? ""} maxLength={120} disabled={saving} onChange={(e) => setAccount({ ...account, external_id: e.target.value })} />
          </Label>
          <Label text={words.form.url}>
            <input className={field} type="url" pattern="https://.*" value={account.url ?? ""} maxLength={500} disabled={saving} onChange={(e) => setAccount({ ...account, url: e.target.value })} />
          </Label>
        </div>
      )}
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} disabled={saving} onClick={onCancel}>
          {words.cancel}
        </button>
        <button type="submit" className={primary} disabled={saving}>
          {saving ? words.saving : words.saveChanges}
        </button>
      </div>
    </form>
  );
}

/**
 * What a turn here is given from the project tree: this project's
 * instructions, which those who may change the project edit, and, read only,
 * what each project above it sends, the workspace first.
 */
export function ContextTab({
  detail,
  workspaceName,
  canWrite,
  onSaved,
}: {
  detail: ProjectDetail;
  workspaceName: string;
  canWrite: boolean;
  onSaved: () => void;
}) {
  const p = detail.project;
  const [draft, setDraft] = useState(p.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [notes, setNotes] = useState<Record<string, Note[]>>({});
  const { flash, say } = useFlash();
  const chain = detail.ancestors.map((a) => a.name).join("\n");

  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when the projects above change
  useEffect(() => {
    let live = true;
    Promise.all(
      detail.ancestors.map((a) =>
        api
          .projectMemory(a.name)
          .then((n): [string, Note[]] => [a.name, n.filter((x) => x.status === "approved")])
          .catch((): [string, Note[]] => [a.name, []]),
      ),
    ).then((pairs) => live && setNotes(Object.fromEntries(pairs)));
    return () => {
      live = false;
    };
  }, [chain]);

  const save = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setProblem(null);
    api
      .updateProject(p.name, updateOf(p, { instructions: draft }))
      .then(() => {
        say(words.context.saved);
        onSaved();
      })
      .catch((err: unknown) => {
        fail(setProblem, words.failed.save)(err);
        // A stale version: read the latest, keeping what was typed.
        if (err instanceof ApiError && err.status === 409) onSaved();
      })
      .finally(() => setSaving(false));
  };

  return (
    <div className="flex max-w-3xl flex-col gap-6">
      <p className="text-aui-sm text-aui-muted">{words.context.order}</p>
      <section className="flex flex-col gap-2" aria-label={words.context.instructions}>
        <h2 className={heading}>{words.context.instructions}</h2>
        <p className="text-aui-xs text-aui-muted">{words.context.instructionsHint}</p>
        {canWrite ? (
          <form onSubmit={save} className="flex flex-col gap-2">
            <textarea
              aria-label={words.context.instructions}
              className={`${field} min-h-32`}
              value={draft}
              maxLength={4000}
              disabled={saving}
              onChange={(e) => setDraft(e.target.value)}
            />
            <div className="flex items-center justify-between gap-3">
              <span className="text-aui-2xs text-aui-muted tabular-nums">{draft.length} / 4,000</span>
              <span className="flex items-center gap-2">
                <FlashNote flash={flash} align="right" />
                <button type="submit" className={primary} disabled={saving || draft === (p.instructions ?? "")}>
                  {saving ? words.saving : words.context.save}
                </button>
              </span>
            </div>
            <Problem message={problem} />
          </form>
        ) : (
          <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{p.instructions || <span className="text-aui-muted">{words.context.none}</span>}</p>
        )}
      </section>
      {detail.ancestors.length > 0 && (
        <section className="flex flex-col gap-3" aria-label={words.context.inherited}>
          <div>
            <h2 className={heading}>{words.context.inherited}</h2>
            <p className="text-aui-xs text-aui-muted">{words.context.inheritedHint}</p>
          </div>
          {detail.ancestors.map((a) => {
            const kept = notes[a.name] ?? [];
            return (
              <section key={a.name} aria-label={words.context.from(titleOf(a, workspaceName))} className="flex flex-col gap-1.5 border-aui-border border-t pt-3">
                <h3 className="text-aui-sm font-medium text-aui-fg">{words.context.from(titleOf(a, workspaceName))}</h3>
                <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{a.instructions || <span className="text-aui-muted">{words.context.none}</span>}</p>
                {kept.length > 0 && (
                  <ul className="flex flex-col gap-1">
                    {kept.map((n) => (
                      <li key={n.id} className="text-aui-sm text-aui-muted">
                        {n.text}
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            );
          })}
        </section>
      )}
    </div>
  );
}

/** Where a project sits: plain links from the workspace down, then the project itself. */
function Breadcrumb({
  detail,
  workspaceName,
  onOpen,
}: {
  detail: ProjectDetail;
  workspaceName: string;
  onOpen: (name: string) => void;
}) {
  return (
    <nav aria-label={words.breadcrumb}>
      <ol className="flex flex-wrap items-center gap-x-1 text-aui-xs text-aui-muted">
        {detail.ancestors.map((a) => (
          <li key={a.name} className="flex items-center gap-1">
            <Go to={projectHref(a.name)} onGo={() => onOpen(a.name)} className="rounded-aui hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent">
              {titleOf(a, workspaceName)}
            </Go>
            <span aria-hidden>›</span>
          </li>
        ))}
        <li aria-current="page" className="text-aui-fg">
          {titleOf(detail.project, workspaceName)}
        </li>
      </ol>
    </nav>
  );
}

/**
 * One project, the workspace included: where it sits, what it holds, and what
 * every turn in it is sent. Its tabs are addresses.
 */
export function ProjectPage({
  name,
  tab,
  onTab,
  message,
  onMessage = () => {},
  me = "",
  inboxTick,
  muted = [],
  onMute,
  onOpenSource = () => {},
  workspaceName,
  projects,
  schedules,
  agents,
  homes = {},
  enabledIn = {},
  can,
  tick,
  onOpen,
  onOpenAll,
  onNewChat,
  onOpenThread,
  onChanged,
  onSchedulesChanged,
  onAgentsChanged,
  onDeleted,
  onOpenChannel,
}: {
  /** "" is the workspace. */
  name: string;
  tab: ProjectTab;
  /** Open one of the project's channels. */
  onOpenChannel?: (channel: string) => void;
  onTab: (tab: ProjectTab) => void;
  /** The message open in its inbox, and how another is opened. */
  message?: string;
  onMessage?: (id: string | null) => void;
  /** The reader's subject, to tell their own announcements apart. */
  me?: string;
  /** Changes whenever the inbox may have. */
  inboxTick?: unknown;
  /** The projects whose announcements stay out of the reader's own inbox. */
  muted?: string[];
  onMute?: (project: string, muted: boolean) => void;
  /** Open what an inbox message is about. */
  onOpenSource?: (source: Source) => void;
  workspaceName: string;
  /** Every project, for the places a sub-project could go and the title shown while this one loads. */
  projects: ProjectView[];
  schedules: ScheduleView[];
  agents: Revision[];
  /** Where each agent that lives in a project lives, and which projects enable each. */
  homes?: Record<string, string>;
  enabledIn?: Record<string, string[]>;
  can: (permission: string) => boolean;
  /** Changes when runs do, so what the project holds is read again. */
  tick?: number;
  onOpen: (name: string) => void;
  onOpenAll: () => void;
  onNewChat: () => void;
  onOpenThread: (id: string) => void;
  onChanged: () => void;
  onSchedulesChanged: () => void;
  /** Where an agent lives, or what a project enables, changed. */
  onAgentsChanged?: () => void;
  onDeleted: () => void;
}) {
  const [detail, setDetail] = useState<ProjectDetail | null>(null);
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [moving, setMoving] = useState<string | null>(null);
  const [reads, setReads] = useState(0);

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and reads are signals to read again
  useEffect(() => {
    let live = true;
    api
      .project(name)
      .then((d) => {
        if (!live) return;
        setDetail(d);
        setMissing(false);
      })
      .catch((e) => {
        if (live && e instanceof ApiError && e.status === 404) setMissing(true);
      });
    return () => {
      live = false;
    };
  }, [name, tick, reads]);

  const changed = () => {
    setReads((n) => n + 1);
    onChanged();
  };

  if (missing) {
    return (
      <Page title={words.missing(name === "" ? "~" : name)} lede={words.missingHint} back={{ label: words.allProjects, onClick: onOpenAll }}>
        {null}
      </Page>
    );
  }
  if (!detail) {
    const listed = projects.find((p) => p.name === name);
    return (
      <Page title={listed ? titleOf(listed) : name === "" ? workspaceName : name} back={{ label: words.allProjects, onClick: onOpenAll }} icon={<FolderIcon size={28} />}>
        <SkeletonRows count={4} />
      </Page>
    );
  }

  const p = detail.project;
  const root = p.name === "";
  // The workspace and company-wide projects are changed only by those who govern projects as well.
  const governed = root || p.kind === "company";
  // What the caller may do here is their permission and their role together, as the server says.
  const may = (action: ProjectAction) => p.actions.includes(action);
  const canWrite = may("edit");
  const isMuted = muted.includes(p.name);
  const parent = detail.ancestors[detail.ancestors.length - 1];
  const back = parent && parent.name !== "" ? { label: titleOf(parent), onClick: () => onOpen(parent.name) } : { label: words.allProjects, onClick: onOpenAll };
  const meta = root ? words.workspace : [kindWord(p.kind, kindsIn(detail)), p.account?.customer, p.depth > 1 ? words.within(above(p)) : null].filter(Boolean).join(" · ");
  const ownScope = root ? ({ kind: "tenant" } as const) : ({ kind: "project", name: p.name } as const);
  const chain = detail.limits
    .filter((l) => l.from !== p.name)
    .map((l) => {
      const from = detail.ancestors.find((a) => a.name === l.from);
      return { scope: l.scope, words: l.from === "" ? words.workspaceLimit : words.limitAbove(from ? titleOf(from) : l.from) };
    });

  return (
    <Page
      back={back}
      icon={<FolderIcon size={28} />}
      title={titleOf(p, workspaceName)}
      meta={<span>{meta}</span>}
      crumbs={root ? undefined : <Breadcrumb detail={detail} workspaceName={workspaceName} onOpen={onOpen} />}
      lede={root ? words.workspaceHint : p.description}
      actions={
        <>
          {!root && onMute && (
            <button type="button" className={button} onClick={() => onMute(p.name, !isMuted)}>
              {isMuted ? defaultCopy.announcements.unmute : defaultCopy.announcements.mute}
            </button>
          )}
          {canWrite && !root && !editing && (
            <button type="button" className={button} onClick={() => setEditing(true)}>
              {words.editProject}
            </button>
          )}
          {may("move") && moving === null && (
            <button type="button" className={button} onClick={() => setMoving(p.parent ?? "")}>
              {defaultCopy.roles.moveProject}
            </button>
          )}
          {may("archive") && (
            <button
              type="button"
              className={button}
              onClick={() => api.updateProject(p.name, { ...updateOf(p), archived: !p.archived }).then(changed).catch(fail(setProblem, words.failed.save))}
            >
              {p.archived ? defaultCopy.roles.unarchive : defaultCopy.roles.archive}
            </button>
          )}
          {may("delete") && (
            <button type="button" className={button} onClick={() => api.deleteProject(p.name).then(onDeleted).catch(fail(setProblem, words.failed.delete))}>
              {words.deleteProject}
            </button>
          )}
          {can("runs:write") && (
            <button type="button" className={primary} onClick={onNewChat}>
              {words.newTask}
            </button>
          )}
        </>
      }
      tabs={
        <Tabs
          label={titleOf(p, workspaceName)}
          value={tab}
          tabs={PROJECT_TABS.map((id) => ({ id, label: words.tab[id] ?? defaultCopy.project.tabs[id as keyof typeof defaultCopy.project.tabs] ?? id }))}
          onChange={onTab}
        />
      }
    >
      <Problem message={problem} />
      {isMuted && <p className="text-aui-xs text-aui-muted">{defaultCopy.announcements.muted}</p>}
      {p.archived && <p className="text-aui-xs text-aui-muted">{defaultCopy.roles.archived}</p>}
      {moving !== null && (
        <form
          className="flex flex-wrap items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            api
              .moveProject(p.name, p.version, moving)
              .then(() => {
                setMoving(null);
                changed();
              })
              .catch(fail(setProblem, words.failed.save));
          }}
        >
          <Menu
            label={defaultCopy.project.moveUnder}
            value={moving}
            options={[
              { value: "", label: defaultCopy.project.topLevel },
              ...projects
                // Not under itself or anything below it, and only where the caller works.
                .filter((q) => q.name !== p.name && !q.path.some((step) => step.name === p.name) && (q.access.role === "member" || q.access.role === "owner"))
                .map((q) => ({ value: q.name, label: titleOf(q, workspaceName), hint: q.depth > 1 ? words.within(above(q)) : undefined })),
            ]}
            onChange={setMoving}
          />
          <button type="submit" className={primary} disabled={moving === (p.parent ?? "")}>
            {defaultCopy.project.move}
          </button>
          <button type="button" className={button} onClick={() => setMoving(null)}>
            {words.cancel}
          </button>
        </form>
      )}
      {p.managed && <p className="text-aui-xs text-aui-muted">{words.managed}</p>}
      {can("projects:write") && governed && !can("projects:govern") && <p className="text-aui-xs text-aui-muted">{words.governed}</p>}
      {editing && (
        <EditProject
          project={p}
          onCancel={() => setEditing(false)}
          onSaved={() => {
            setEditing(false);
            changed();
          }}
        />
      )}

      {tab === "inbox" ? (
        <ProjectInbox key={p.name} project={p} me={me} signal={inboxTick} message={message} onMessage={onMessage} onOpen={onOpenSource} onChanged={onChanged} />
      ) : tab === "people" ? (
        <ProjectPeople key={`${p.name}:${p.version}`} project={p} onChanged={changed} />
      ) : tab === "limits" ? (
        <ProjectLimits project={p} scope={ownScope} chain={chain} canBilling={can("billing:write")} tick={tick} />
      ) : tab === "history" ? (
        <ProjectHistory project={p} tick={tick} />
      ) : tab === "agents" ? (
        <ProjectAgentsTab
          key={p.name}
          name={p.name}
          workspaceName={workspaceName}
          agents={agents}
          homes={homes}
          enabledIn={enabledIn}
          projects={projects}
          canWrite={canWrite}
          canHome={can("agents:write") && (!governed || can("projects:govern"))}
          onChanged={() => {
            changed();
            onAgentsChanged?.();
          }}
        />
      ) : tab === "context" ? (
        <ContextTab key={p.name} detail={detail} workspaceName={workspaceName} canWrite={canWrite} onSaved={changed} />
      ) : tab === "schedules" ? (
        <Scheduled
          key={p.name}
          name={p.name}
          title={titleOf(p, workspaceName)}
          schedules={schedules}
          agents={agents}
          canSchedule={can("schedules:write")}
          recap
          onOpenThread={onOpenThread}
          onSchedulesChanged={onSchedulesChanged}
          onChanged={changed}
        />
      ) : tab === "channels" ? (
        <ChannelsTab key={p.name} project={p.name} canWrite={canWrite} onOpenChannel={(c) => onOpenChannel?.(c)} />
      ) : (
        // Tasks, what sits below and what holds the project take the wide column; what every task is sent and what runs on a timer sit beside them.
        <div className="grid grid-cols-1 items-start gap-8 @4xl:grid-cols-[minmax(0,1fr)_20rem]">
          <div className="flex flex-col gap-8">
            <ProjectTasks key={p.name} name={p.name} hasChildren={p.children > 0} tick={tick} onOpenThread={onOpenThread} />
            <SubProjects
              detail={detail}
              places={projects}
              canCreate={may("create")}
              canGovern={can("projects:govern")}
              onOpen={onOpen}
              onCreated={(created) => {
                changed();
                onOpen(created.name);
              }}
            />
          </div>
          <aside className="flex flex-col gap-8">
            <SharedMemory project={p.name} canWrite={may("notes")} canPropose={may("propose")} onChanged={changed} />
            <Scheduled
              name={p.name}
              title={titleOf(p, workspaceName)}
              schedules={schedules}
              agents={agents}
              canSchedule={can("schedules:write")}
              onOpenThread={onOpenThread}
              onSchedulesChanged={onSchedulesChanged}
              onChanged={changed}
            />
          </aside>
        </div>
      )}
    </Page>
  );
}
