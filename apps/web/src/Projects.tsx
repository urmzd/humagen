import { defaultCopy } from "@humagen/components";
import {
  CalendarIcon,
  FolderPlusIcon,
  GitHubIcon,
  LayersIcon,
  MonitorIcon,
  PinIcon,
  SlackIcon,
  UserIcon,
  UsersIcon,
} from "@humagen/render-web";
import { Sheet, SheetContent, SheetHeader, SheetTitle } from "@humagen/render-web/ui/sheet";
import {
  type FormEvent,
  type MouseEvent,
  type ReactNode,
  useEffect,
  useId,
  useRef,
  useState,
} from "react";
import {
  ApiError,
  api,
  type Channel,
  type Client,
  type CorpusBrief,
  coworking,
  type Note,
  type ProjectAccount,
  type ProjectAction,
  type ProjectCorpora,
  type ProjectDetail,
  type ProjectKind,
  type ProjectKindSpec,
  type ProjectView,
  type ProjectVisibility,
  type Revision,
  type ScheduleView,
} from "./api.ts";
import { FilterSelect, Pill, StatusPill } from "./cards.tsx";
import { inWords, presets } from "./cron.ts";
import { ProjectPeople } from "./ProjectTabs.tsx";
import {
  cardsOf,
  groupWord,
  isPrivate,
  kindsOf,
  kindsUnder,
  kindWord,
  lastActive,
  nameFrom,
  titleOf,
  updateOf,
  validName,
  visibilityOf,
  WORKSPACE,
} from "./projects.ts";
import { href, type ProjectSort } from "./route.ts";
import { type AgentSessionSummary, isRootSession, sessionCenter } from "./sessionCenter.ts";
import {
  AgentMark,
  ago,
  button,
  Confirm,
  FlashNote,
  field,
  Label,
  MoreMenu,
  Page,
  type PageAction,
  Problem,
  pageTitle,
  primary,
  rows,
  SearchField,
  SkeletonRows,
  textAction,
  useFlash,
} from "./ui.tsx";

const words = defaultCopy.projects;
const here = words.page;
const fail = (set: (m: string) => void, fallback: string) => (e: unknown) =>
  set(e instanceof ApiError ? e.message : fallback);

/** A plain link: a plain click goes there in place, a modified one opens a new tab. */
function Go({
  to,
  onGo,
  className,
  children,
}: {
  to: string;
  onGo: () => void;
  className?: string;
  children: ReactNode;
}) {
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

const projectHref = (name: string) => href({ page: "project", name });

/**
 * Every project the reader has, as a grid of cards: sub-projects too, none
 * nested, and not the workspace itself. Pinned ones come first. A search, a
 * sort and, only when the workspace defines kinds of its own, a kind filter
 * narrow it. A project is opened from here, and made from here in a sheet.
 */
export function ProjectsPage({
  projects,
  root,
  q = "",
  kind,
  sort,
  onFilter,
  canCreate,
  canGovern,
  me,
  pinned = [],
  onPin,
  onOpen,
  onCreated,
}: {
  /** Null while they load. */
  projects: ProjectView[] | null;
  /** The workspace's own view, when it has been read: its kinds decide whether the kind filter shows. */
  root: ProjectView | null;
  q?: string;
  kind?: ProjectKind;
  /** Last activity when absent. */
  sort?: ProjectSort;
  onFilter: (next: { q: string; kind?: ProjectKind; sort?: ProjectSort }) => void;
  canCreate: boolean;
  /** May make company-wide projects, offered only among the workspace's kinds. */
  canGovern: boolean;
  /** The signed-in subject: the owner of a private project made here. */
  me: string;
  /** The names of the projects the reader pinned. */
  pinned?: readonly string[];
  onPin?: (project: string, pinned: boolean) => Promise<void>;
  onOpen: (name: string) => void;
  onCreated: (project: ProjectView) => void;
}) {
  const [creating, setCreating] = useState(false);
  const opener = useRef<HTMLElement | null>(null);
  const kinds = root?.kinds ?? [];
  // Kinds are the workspace's to name: without its own, there is nothing to filter by.
  const narrowed = kinds.length > 0 ? kind : undefined;
  const listed = projects?.filter((p) => p.name !== "") ?? null;
  const cards = listed
    ? cardsOf(listed, { q, kind: narrowed, sort: sort ?? "activity", pinned })
    : null;
  const empty = listed?.length === 0;
  const set = (next: { q?: string; kind?: ProjectKind; sort?: ProjectSort }) =>
    onFilter({ q, kind: narrowed, sort, ...next });
  const shownKinds = kindsOf(kinds, listed ?? []).filter(
    (k) => k === narrowed || listed?.some((p) => p.kind === k),
  );
  const create = canCreate && (
    <button
      type="button"
      className={primary}
      onClick={(e) => {
        opener.current = e.currentTarget;
        setCreating(true);
      }}
    >
      <FolderPlusIcon size={16} /> {words.newProject}
    </button>
  );

  return (
    <Page title={words.title} actions={empty ? undefined : create || undefined}>
      {!empty && (
        <div className="flex flex-wrap items-center gap-2">
          <SearchField
            label={words.search}
            value={q}
            onChange={(next) => set({ q: next })}
            className="w-full min-w-0 sm:w-auto sm:max-w-sm sm:flex-1"
          />
          <div className="flex min-w-0 flex-wrap items-center gap-2 sm:ml-auto">
            {kinds.length > 0 && (
              <FilterSelect
                label={words.kindFilter}
                value={narrowed ?? ""}
                options={[
                  { value: "", label: words.anyKind },
                  ...shownKinds.map((k) => ({ value: k, label: groupWord(k, kinds) })),
                ]}
                onChange={(next) => set({ kind: next || undefined })}
              />
            )}
            <span aria-hidden className="text-aui-xs text-aui-muted">
              {words.sort.label}
            </span>
            <FilterSelect<ProjectSort>
              label={words.sort.label}
              value={sort ?? ""}
              options={[
                { value: "", label: words.sort.activity },
                { value: "name", label: words.sort.name },
                { value: "created", label: words.sort.created },
              ]}
              onChange={(next) => set({ sort: next || undefined })}
            />
          </div>
        </div>
      )}
      {cards === null ? (
        <output data-testid="skeleton" className={`aui-skeleton-delay ${grid}`}>
          <span className="sr-only">{words.loading}</span>
          {LOADING_CARDS.map((k) => (
            <span key={k} aria-hidden className="aui-skeleton block h-32 rounded-aui-lg" />
          ))}
        </output>
      ) : empty ? (
        <div className="flex flex-col items-start gap-4 py-10">
          <p className="humagen-display text-aui-lg font-medium text-aui-fg">{words.empty}</p>
          {create}
        </div>
      ) : cards.length === 0 ? (
        <output className="block text-aui-sm text-aui-muted">{words.noMatch}</output>
      ) : (
        <ul aria-label={words.title} className={grid}>
          {cards.map((p) => (
            <li key={p.name} className="min-w-0">
              <ProjectCard
                project={p}
                pinned={pinned.includes(p.name)}
                onPin={onPin}
                onOpen={() => onOpen(p.name)}
              />
            </li>
          ))}
        </ul>
      )}
      <Sheet open={creating} onOpenChange={(next) => !next && setCreating(false)}>
        <SheetContent
          aria-describedby={undefined}
          onCloseAutoFocus={(event) => {
            if (opener.current?.isConnected) {
              event.preventDefault();
              opener.current.focus();
            }
          }}
        >
          <SheetHeader>
            <SheetTitle className="text-aui-base">{words.form.heading}</SheetTitle>
          </SheetHeader>
          <CreateProject
            me={me}
            kinds={kinds}
            canGovern={canGovern}
            onCancel={() => setCreating(false)}
            onCreated={(p) => {
              setCreating(false);
              onCreated(p);
            }}
          />
        </SheetContent>
      </Sheet>
    </Page>
  );
}

/** The cards' grid: as many 20rem columns as fit, one on a phone. */
const grid = "grid min-w-0 grid-cols-[repeat(auto-fill,minmax(min(100%,20rem),1fr))] gap-3";
/** The cards that stand in for the grid while it loads. */
const LOADING_CARDS = ["a", "b", "c", "d", "e", "f"];

/**
 * One project as a card: its title, which opens it, when something last
 * happened in it, and a pin. The whole card is the link; the pin sits above
 * it, shown on hover or focus and always once pinned.
 */
function ProjectCard({
  project,
  pinned,
  onPin,
  onOpen,
}: {
  project: ProjectView;
  pinned: boolean;
  onPin?: (project: string, pinned: boolean) => Promise<void>;
  onOpen: () => void;
}) {
  const title = titleOf(project);
  const when = lastActive(project);
  const { flash, say, clear } = useFlash();
  const label = defaultCopy.sidebar.pinProject(title, pinned);
  return (
    <article className="group/card relative flex h-full min-h-32 min-w-0 flex-col gap-3 rounded-aui-lg border border-[var(--humagen-rule)] bg-[var(--humagen-panel)] p-4 shadow-[var(--humagen-bevel)] transition-colors hover:border-aui-ring/50 has-[a:focus-visible]:outline-2 has-[a:focus-visible]:outline-aui-accent motion-reduce:transition-none">
      <div className="flex min-w-0 items-start gap-2">
        <Go
          to={projectHref(project.name)}
          onGo={onOpen}
          className="line-clamp-2 min-w-0 flex-1 break-words font-medium text-aui-base text-aui-fg outline-none after:absolute after:inset-0 after:rounded-aui-lg after:content-['']"
        >
          {title}
        </Go>
        {onPin && (
          <div className="relative -mt-1 -mr-1.5 shrink-0">
            <button
              type="button"
              aria-pressed={pinned}
              aria-label={label}
              title={label}
              onClick={() => {
                clear();
                onPin(project.name, !pinned).catch(() =>
                  say(pinned ? defaultCopy.pins.unpinFailed : defaultCopy.pins.pinFailed, true),
                );
              }}
              className={`relative z-10 inline-flex size-8 items-center justify-center rounded-aui hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent group-hover/card:opacity-100 group-focus-within/card:opacity-100 pointer-coarse:size-11 pointer-coarse:opacity-100 ${pinned ? "text-aui-fg opacity-100" : "text-aui-muted opacity-0"}`}
            >
              <PinIcon size={14} fill={pinned ? "currentColor" : "none"} />
            </button>
            <FlashNote flash={flash} align="right" />
          </div>
        )}
      </div>
      <div className="mt-auto flex min-w-0 flex-wrap items-center gap-2 text-aui-xs text-aui-muted">
        <time dateTime={when}>{words.updated(ago(when))}</time>
        {/* A personal project is always its owner's alone, so it does not say so. */}
        {isPrivate(project) && !project.personal_owner && <Pill>{words.private}</Pill>}
      </div>
    </article>
  );
}

/**
 * The New Project sheet: a name, a description and who reads it, at the top
 * level. Private names the person making it its one owner; Workspace leaves
 * the members to the platform's default. A kind is asked for only when the
 * workspace defines kinds of its own.
 */
function CreateProject({
  me,
  kinds,
  canGovern,
  onCreated,
  onCancel,
}: {
  me: string;
  kinds: ProjectKindSpec[];
  canGovern: boolean;
  onCreated: (project: ProjectView) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [access, setAccess] = useState<"private" | "workspace">(me ? "private" : "workspace");
  const offered = kinds.length > 0 ? kindsUnder(WORKSPACE, canGovern, kinds) : [];
  const [kind, setKind] = useState<ProjectKind>("team");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const shown = title.trim();
    const name = nameFrom(shown);
    if (!validName(name)) {
      setProblem(words.form.nameNeeded);
      return;
    }
    setSaving(true);
    setProblem(null);
    api
      .createProject({
        name,
        ...(shown !== name ? { title: shown } : {}),
        ...(description.trim() ? { description: description.trim() } : {}),
        ...(offered.includes(kind) && kind !== "team" ? { kind } : {}),
        ...(access === "private" && me ? { members: [{ who: me, role: "owner" as const }] } : {}),
      })
      .then(onCreated)
      .catch(fail(setProblem, words.failed.create))
      .finally(() => setSaving(false));
  };

  const choices = [
    ...(me
      ? [
          {
            id: "private" as const,
            label: words.form.accessPrivate,
            hint: words.form.accessPrivateHint,
          },
        ]
      : []),
    {
      id: "workspace" as const,
      label: words.form.accessWorkspace,
      hint: words.form.accessWorkspaceHint,
    },
  ];

  return (
    <form onSubmit={submit} aria-label={words.form.heading} className="flex min-w-0 flex-col gap-4">
      <Label text={words.form.name}>
        <input
          // biome-ignore lint/a11y/noAutofocus: the sheet opens because the person asked to make a project
          autoFocus
          className={field}
          value={title}
          required
          maxLength={80}
          disabled={saving}
          onChange={(e) => setTitle(e.target.value)}
        />
      </Label>
      <Label text={words.form.description}>
        <textarea
          className={`${field} h-auto min-h-20 py-2`}
          value={description}
          maxLength={500}
          disabled={saving}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Label>
      {offered.length > 0 && (
        <Label text={words.form.kind}>
          <select
            className={field}
            value={kind}
            disabled={saving}
            onChange={(e) => setKind(e.target.value)}
          >
            {offered.map((k) => (
              <option key={k} value={k}>
                {kindWord(k, kinds)}
              </option>
            ))}
          </select>
        </Label>
      )}
      <fieldset disabled={saving} className="flex min-w-0 flex-col gap-2">
        <legend className="pb-1 text-aui-xs font-medium text-aui-muted">{words.form.access}</legend>
        {choices.map((c) => (
          <label
            key={c.id}
            className="flex min-h-11 cursor-pointer items-start gap-3 rounded-aui border border-aui-border px-3 py-2.5 has-[:checked]:border-aui-accent has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-aui-accent"
          >
            <input
              type="radio"
              name="access"
              value={c.id}
              checked={access === c.id}
              onChange={() => setAccess(c.id)}
              className="mt-0.5 size-4 shrink-0 accent-[var(--aui-accent)] outline-none"
            />
            <span className="min-w-0">
              <span className="block text-aui-sm text-aui-fg">{c.label}</span>
              <span className="block text-aui-xs text-aui-muted">{c.hint}</span>
            </span>
          </label>
        ))}
      </fieldset>
      <Problem message={problem} />
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={button} disabled={saving} onClick={onCancel}>
          {words.cancel}
        </button>
        <button type="submit" className={primary} disabled={saving || !title.trim()}>
          {saving ? words.creating : words.createProject}
        </button>
      </div>
    </form>
  );
}

/** A project's title, description and, for an account, its customer, changed as a whole. */
function EditProject({
  project,
  onSaved,
  onCancel,
}: {
  project: ProjectView;
  onSaved: () => void;
  onCancel: () => void;
}) {
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
      .updateProject(
        project.name,
        updateOf(project, {
          title,
          description,
          ...(project.kind === "account" ? { account } : {}),
        }),
      )
      .then(onSaved)
      .catch((err: unknown) => {
        fail(setProblem, words.failed.save)(err);
        if (err instanceof ApiError && err.status === 409) onSaved();
      })
      .finally(() => setSaving(false));
  };
  return (
    <form onSubmit={save} aria-label={words.editProject} className="flex min-w-0 flex-col gap-4">
      <div className="flex min-w-0 flex-col gap-4">
        <Label text={words.form.title} hint={words.form.titleHint}>
          <input
            className={field}
            value={title}
            maxLength={80}
            disabled={saving}
            onChange={(e) => setTitle(e.target.value)}
          />
        </Label>
        <Label text={words.form.name} hint={words.form.nameHint}>
          <input className={field} value={project.name} disabled readOnly />
        </Label>
      </div>
      <Label text={words.form.description}>
        <input
          className={field}
          value={description}
          maxLength={500}
          disabled={saving}
          onChange={(e) => setDescription(e.target.value)}
        />
      </Label>
      {project.kind === "account" && (
        <div className="flex min-w-0 flex-col gap-4">
          <Label text={words.form.customer}>
            <input
              className={field}
              value={account.customer ?? ""}
              maxLength={120}
              disabled={saving}
              onChange={(e) => setAccount({ ...account, customer: e.target.value })}
            />
          </Label>
          <Label text={words.form.externalId}>
            <input
              className={field}
              value={account.external_id ?? ""}
              maxLength={120}
              disabled={saving}
              onChange={(e) => setAccount({ ...account, external_id: e.target.value })}
            />
          </Label>
          <Label text={words.form.url}>
            <input
              className={field}
              type="url"
              pattern="https://.*"
              value={account.url ?? ""}
              maxLength={500}
              disabled={saving}
              onChange={(e) => setAccount({ ...account, url: e.target.value })}
            />
          </Label>
        </div>
      )}
      <Problem message={problem} />
      <div className="flex flex-wrap justify-end gap-2">
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

/** A card in the project page's side column: a panel on a hairline, like the Projects grid's. */
const sideCard =
  "flex min-w-0 flex-col gap-2 rounded-aui-lg border border-[var(--humagen-rule)] bg-[var(--humagen-panel)] p-4";
/** A card's one action: quiet text, no underline, a full hit area on touch. */
const quietAction =
  "inline-flex min-h-8 pointer-coarse:min-h-11 items-center rounded-aui px-2 -mr-2 text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none";
/** A card's summary: a few short lines, muted. */
const summary = "min-w-0 text-aui-sm text-aui-muted";

/** What a side card's one action says, what a screen reader hears, and the heading of the sheet it opens. */
interface CardAction {
  text: string;
  label: string;
  heading: string;
}

/**
 * One part of a project in its side column: a heading, one action at its
 * right, and a short summary under it. The action opens a sheet beside the
 * page; closing the sheet puts focus back on the action. `plain` draws it as
 * a line with no box, its heading for assistive technology only.
 */
function SideCard({
  title,
  action,
  sheet,
  plain = false,
  children,
}: {
  title: string;
  action?: CardAction;
  /** What the sheet holds, given a way to close it. Drawn only while it is open. */
  sheet?: (close: () => void) => ReactNode;
  plain?: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const opener = useRef<HTMLButtonElement>(null);
  const id = useId();
  const control = action && sheet && (
    <button
      ref={opener}
      type="button"
      aria-label={action.label}
      className={`${quietAction} shrink-0`}
      onClick={() => setOpen(true)}
    >
      {action.text}
    </button>
  );
  return (
    <section
      aria-labelledby={id}
      className={plain ? "flex min-w-0 items-center justify-between gap-3 px-1" : sideCard}
    >
      {plain ? (
        <>
          <h2 id={id} className="sr-only">
            {title}
          </h2>
          {children}
          {control}
        </>
      ) : (
        <>
          <div className="flex min-w-0 items-center justify-between gap-3">
            <h2 id={id} className="text-aui-sm font-medium text-aui-fg">
              {title}
            </h2>
            {control}
          </div>
          {children}
        </>
      )}
      {action && sheet && (
        <Sheet open={open} onOpenChange={(next) => !next && setOpen(false)}>
          <SheetContent
            aria-describedby={undefined}
            onCloseAutoFocus={(event) => {
              if (opener.current?.isConnected) {
                event.preventDefault();
                opener.current.focus();
              }
            }}
          >
            <SheetHeader>
              <SheetTitle className="text-aui-base">{action.heading}</SheetTitle>
            </SheetHeader>
            {open && sheet(() => setOpen(false))}
          </SheetContent>
        </Sheet>
      )}
    </section>
  );
}

/** A sheet's Cancel and its one primary action, at its foot. */
function SheetButtons({
  busy,
  label,
  disabled = false,
  onCancel,
}: {
  busy: boolean;
  label: string;
  disabled?: boolean;
  onCancel: () => void;
}) {
  return (
    <div className="flex flex-wrap justify-end gap-2">
      <button type="button" className={button} disabled={busy} onClick={onCancel}>
        {here.cancel}
      </button>
      <button type="submit" className={primary} disabled={busy || disabled}>
        {busy ? here.saving : label}
      </button>
    </div>
  );
}

/** How many sessions Recents reads at a time. */
const RECENT_PAGE = 20;

/** Where a schedule's runs are filed: sessions under schedule://<name>. */
const SCHEDULE_PARENT = "schedule://";

/**
 * Where a session started, in words: a schedule's run by the schedule's name,
 * a Slack or GitHub conversation by where it came from, else the console. The
 * list carries no link back to the Slack thread or the pull request.
 */
function originOf(s: AgentSessionSummary): string {
  const t = here.trace;
  if (s.parent.startsWith(SCHEDULE_PARENT))
    return t.schedule(s.parent.slice(SCHEDULE_PARENT.length));
  const origin = s.conversation?.origin;
  if (origin === "slack") return t.slack;
  if (origin === "github") return t.github;
  if (origin === "cron" || origin === "manual") return t.scheduled;
  return t.console;
}

/** How many agents, then people, a row names before it counts the rest. */
const SHOWN = 3;

/** The mark beside where a session started. */
function OriginMark({ session: s }: { session: AgentSessionSummary }) {
  const origin = s.conversation?.origin;
  if (s.parent.startsWith(SCHEDULE_PARENT) || origin === "cron" || origin === "manual")
    return <CalendarIcon size={13} />;
  if (origin === "slack") return <SlackIcon size={13} />;
  if (origin === "github") return <GitHubIcon size={13} />;
  return <MonitorIcon size={13} />;
}

/**
 * One session in Recents: its title, then one quiet line: where it started,
 * the agents that answered and the people besides the reader, its state in
 * words, and when it last changed. It opens the session.
 */
function SessionRow({
  session: s,
  self,
  onOpen,
}: {
  session: AgentSessionSummary;
  self?: string;
  onOpen: () => void;
}) {
  const agents = s.conversation?.agents?.length ? s.conversation.agents : [s.agent];
  const people = (s.conversation?.people ?? []).filter((name) => name !== self);
  const who = [
    ...agents.map((name) => ({ kind: "agent" as const, name })),
    ...people.map((name) => ({ kind: "person" as const, name })),
  ];
  return (
    <li className="min-w-0">
      <Go
        to={href({ page: "conversation", id: s.id })}
        onGo={onOpen}
        className="flex min-h-11 min-w-0 flex-col gap-1 rounded-aui px-2 py-2 transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
      >
        <span className="min-w-0 truncate text-aui-sm font-medium text-aui-fg">
          {s.title || here.untitled}
        </span>
        <span className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-aui-xs text-aui-muted">
          <span className="inline-flex min-w-0 items-center gap-1">
            <OriginMark session={s} />
            <span className="truncate">{originOf(s)}</span>
          </span>
          {who.slice(0, SHOWN).map((w) =>
            w.kind === "agent" ? (
              <span key={`a:${w.name}`} className="inline-flex min-w-0 items-center gap-1">
                <AgentMark name={w.name} size={12} />
                <span className="truncate">{w.name}</span>
              </span>
            ) : (
              <span key={`p:${w.name}`} className="truncate">
                @{w.name}
              </span>
            ),
          )}
          {who.length > SHOWN && <span>{here.trace.more(who.length - SHOWN)}</span>}
          <span className="inline-flex items-center gap-1.5" data-status={s.status}>
            <span
              aria-hidden
              className={`size-1.5 rounded-full ${statusDot[s.status] ?? "bg-aui-muted"}`}
            />
            {statusWords(s.status)}
          </span>
          <time dateTime={s.updated_at}>{ago(s.updated_at)}</time>
        </span>
      </Go>
    </li>
  );
}

/** A session's state as a small dot, not a pill: only trouble and waiting take a color. */
const statusDot: Record<string, string> = {
  working: "bg-aui-accent",
  waiting: "bg-aui-warn",
  failed: "bg-aui-bad",
  done: "bg-aui-muted",
  stopped: "bg-aui-muted",
};

/** A session's state in words, sentence case. */
function statusWords(status: string): string {
  return status ? `${status[0]?.toUpperCase()}${status.slice(1)}` : "";
}

/** The project's sessions, newest first, each opening where it lives. */
function Recents({
  project,
  self,
  tick,
  onOpen,
}: {
  project: string;
  /** The reader's handle: they are not named among who answered. */
  self?: string;
  tick?: number;
  onOpen: (session: string) => void;
}) {
  const [list, setList] = useState<{ sessions: AgentSessionSummary[]; next: string } | null>(null);
  const [failed, setFailed] = useState(false);
  const [more, setMore] = useState(false);
  const [retry, setRetry] = useState(0);
  const generation = useRef(0);
  const id = useId();
  const filters = { project: project === "" ? "~" : project, roots: "true" };

  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and retry are signals to read again
  useEffect(() => {
    const current = ++generation.current;
    setFailed(false);
    sessionCenter.sessions({ ...filters, limit: String(RECENT_PAGE) }).then(
      (r) =>
        current === generation.current &&
        setList({ sessions: r.sessions.filter(isRootSession), next: r.next }),
      () => current === generation.current && setFailed(true),
    );
  }, [project, tick, retry]);

  const loadMore = () => {
    if (!list?.next || more) return;
    const current = generation.current;
    setMore(true);
    sessionCenter
      .sessions({ ...filters, limit: String(RECENT_PAGE), after: list.next })
      .then(
        (r) =>
          current === generation.current &&
          setList({
            sessions: [
              ...list.sessions,
              ...r.sessions
                .filter(isRootSession)
                .filter((s) => !list.sessions.some((t) => t.id === s.id)),
            ],
            next: r.next,
          }),
      )
      .catch(() => current === generation.current && setFailed(true))
      .finally(() => current === generation.current && setMore(false));
  };

  return (
    <section aria-labelledby={id} className="flex min-w-0 flex-col gap-2">
      <h2 id={id} className="text-aui-sm font-medium text-aui-muted">
        {here.recents}
      </h2>
      {failed ? (
        <div className="flex flex-wrap items-center gap-3">
          <Problem message={here.sessionsFailed} />
          <button type="button" className={textAction} onClick={() => setRetry((n) => n + 1)}>
            {here.retry}
          </button>
        </div>
      ) : null}
      {list === null ? (
        !failed && <SkeletonRows count={3} />
      ) : list.sessions.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{here.noSessions}</p>
      ) : (
        <ul className={rows}>
          {list.sessions.map((s) => (
            <SessionRow key={s.id} session={s} self={self} onOpen={() => onOpen(s.id)} />
          ))}
        </ul>
      )}
      {list?.next && (
        <button type="button" className={`${button} self-start`} disabled={more} onClick={loadMore}>
          {more ? here.loadingMore : here.showMore}
        </button>
      )}
    </section>
  );
}

/** The instructions every session in the project is sent, as a whole, from the version read. */
function InstructionsForm({
  project,
  onSaved,
  onCancel,
}: {
  project: ProjectView;
  onSaved: () => void;
  onCancel: () => void;
}) {
  const [draft, setDraft] = useState(project.instructions ?? "");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setProblem(null);
    api
      .updateProject(project.name, updateOf(project, { instructions: draft }))
      .then(onSaved)
      .catch((err: unknown) => {
        fail(setProblem, here.instructions.failed)(err);
        setSaving(false);
      });
  };
  return (
    <form onSubmit={save} className="flex min-w-0 flex-col gap-3">
      <textarea
        aria-label={here.instructions.title}
        // biome-ignore lint/a11y/noAutofocus: the sheet opens because the person asked to write them
        autoFocus
        className={`${field} h-auto min-h-56 py-2`}
        value={draft}
        maxLength={4000}
        disabled={saving}
        onChange={(e) => setDraft(e.target.value)}
      />
      <span className="self-end text-aui-2xs text-aui-muted tabular-nums">
        {here.instructions.count(draft.length)}
      </span>
      <Problem message={problem} />
      <SheetButtons
        busy={saving}
        label={here.save}
        disabled={draft === (project.instructions ?? "")}
        onCancel={onCancel}
      />
    </form>
  );
}

/** The corpora a project attaches, chosen from those the reader may read, saved as a whole. */
function ContextForm({
  project,
  attached,
  corpora,
  onSaved,
  onStale,
  onCancel,
}: {
  project: string;
  attached: ProjectCorpora;
  corpora: CorpusBrief[];
  onSaved: (next: ProjectCorpora) => void;
  onStale: () => void;
  onCancel: () => void;
}) {
  const [picked, setPicked] = useState(() => attached.corpora.map((c) => c.name));
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  // One the project attaches that the list leaves out stays offered, so it is not dropped unseen.
  const offered = [
    ...corpora,
    ...attached.corpora.filter((a) => !corpora.some((c) => c.name === a.name)),
  ];
  const same =
    picked.length === attached.corpora.length &&
    picked.every((name) => attached.corpora.some((c) => c.name === name));
  const save = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setProblem(null);
    api
      .setProjectCorpora(project, attached.version, picked)
      .then(onSaved)
      .catch((err: unknown) => {
        setSaving(false);
        if (err instanceof ApiError && err.status === 409) {
          setProblem(here.context.stale);
          onStale();
          return;
        }
        fail(setProblem, here.context.failed)(err);
      });
  };
  if (offered.length === 0)
    return <p className="text-aui-sm text-aui-muted">{here.context.noCorpora}</p>;
  return (
    <form onSubmit={save} className="flex min-w-0 flex-col gap-4">
      <fieldset disabled={saving} className="flex min-w-0 flex-col gap-1">
        <legend className="sr-only">{here.context.title}</legend>
        {offered.map((c) => (
          <label
            key={c.name}
            className="flex min-h-11 cursor-pointer items-center gap-3 rounded-aui px-2 text-aui-sm text-aui-fg hover:bg-aui-hover has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-aui-accent"
          >
            <input
              type="checkbox"
              className="size-4 shrink-0 accent-[var(--aui-accent)] outline-none"
              checked={picked.includes(c.name)}
              onChange={(e) =>
                setPicked((now) =>
                  e.target.checked ? [...now, c.name] : now.filter((name) => name !== c.name),
                )
              }
            />
            <span className="min-w-0 break-words">{c.title || c.name}</span>
          </label>
        ))}
      </fieldset>
      <Problem message={problem} />
      <SheetButtons busy={saving} label={here.save} disabled={same} onCancel={onCancel} />
    </form>
  );
}

/**
 * What every session in the project is sent: its notes, and those waiting to
 * become notes. Whoever decides the project's notes keeps or discards what
 * waits and forgets what is kept; whoever may propose adds one.
 */
function MemorySheet({
  project,
  canDecide,
  canPropose,
  onChanged,
}: {
  project: string;
  canDecide: boolean;
  canPropose: boolean;
  onChanged: () => void;
}) {
  const [notes, setNotes] = useState<Note[] | null>(null);
  const [text, setText] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const read = () =>
    api
      .projectMemory(project)
      .then(setNotes)
      .catch(() => setNotes([]));
  // biome-ignore lint/correctness/useExhaustiveDependencies: read once per project
  useEffect(() => {
    void read();
  }, [project]);
  const act = (work: Promise<unknown>) => {
    setBusy(true);
    setProblem(null);
    work
      .then(() => {
        onChanged();
        return read();
      })
      .catch(fail(setProblem, here.memory.failed))
      .finally(() => setBusy(false));
  };
  if (notes === null) return <SkeletonRows count={3} />;
  const kept = notes.filter((n) => n.status === "approved");
  const waiting = notes.filter((n) => n.status === "pending");
  const noteRow = (n: Note, controls: ReactNode) => (
    <li key={n.id} className="flex min-w-0 items-start justify-between gap-3 py-2.5">
      <span className="min-w-0 break-words text-aui-sm text-aui-fg">{n.text}</span>
      {controls && <span className="flex shrink-0 items-center gap-3">{controls}</span>}
    </li>
  );
  return (
    <div className="flex min-w-0 flex-col gap-5">
      <Problem message={problem} />
      {waiting.length > 0 && (
        <section aria-label={here.memory.waiting} className="flex min-w-0 flex-col gap-1">
          <h3 className="text-aui-xs font-medium text-aui-muted">{here.memory.waiting}</h3>
          <ul className={rows}>
            {waiting.map((n) =>
              noteRow(
                n,
                canDecide && (
                  <>
                    <button
                      type="button"
                      className={textAction}
                      disabled={busy}
                      aria-label={here.memory.keepLabel(n.text)}
                      onClick={() => act(api.approveProjectMemory(project, [n.id]))}
                    >
                      {here.memory.keep}
                    </button>
                    <button
                      type="button"
                      className={textAction}
                      disabled={busy}
                      aria-label={here.memory.discardLabel(n.text)}
                      onClick={() => act(api.removeProjectMemory(project, n.id))}
                    >
                      {here.memory.discard}
                    </button>
                  </>
                ),
              ),
            )}
          </ul>
        </section>
      )}
      <section aria-label={here.memory.kept} className="flex min-w-0 flex-col gap-1">
        {waiting.length > 0 && (
          <h3 className="text-aui-xs font-medium text-aui-muted">{here.memory.kept}</h3>
        )}
        {kept.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{here.memory.none}</p>
        ) : (
          <ul className={rows}>
            {kept.map((n) =>
              noteRow(
                n,
                canDecide && (
                  <button
                    type="button"
                    className={textAction}
                    disabled={busy}
                    aria-label={here.memory.forgetLabel(n.text)}
                    onClick={() => act(api.removeProjectMemory(project, n.id))}
                  >
                    {here.memory.forget}
                  </button>
                ),
              ),
            )}
          </ul>
        )}
      </section>
      {(canDecide || canPropose) && (
        <form
          className="flex min-w-0 flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            if (!text.trim()) return;
            act(api.addProjectMemory(project, text.trim()).then(() => setText("")));
          }}
        >
          <Label text={here.memory.note}>
            <textarea
              className={`${field} h-auto min-h-20 py-2`}
              value={text}
              maxLength={300}
              disabled={busy}
              onChange={(e) => setText(e.target.value)}
            />
          </Label>
          <button type="submit" className={`${button} self-end`} disabled={busy || !text.trim()}>
            {here.memory.add}
          </button>
        </form>
      )}
    </div>
  );
}

/** "Custom" in the When menu: a cron expression typed out. */
const CUSTOM = "custom";

/** A new schedule in this project: a name, when, the agent it asks and what it asks. */
function ScheduleForm({
  project,
  agents,
  onCreated,
  onCancel,
}: {
  project: string;
  agents: Revision[];
  onCreated: () => void;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [when, setWhen] = useState(presets[3]?.expression ?? "0 9 * * 1-5");
  const [expression, setExpression] = useState("0 9 * * 1-5");
  const [agent, setAgent] = useState(agents[0]?.spec.name ?? "");
  const [text, setText] = useState("");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const words = here.scheduled;
  const create = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    setSaving(true);
    setProblem(null);
    api
      .createSchedule({
        name: name.trim(),
        expression: when === CUSTOM ? expression.trim() : when,
        agent,
        text: text.trim(),
        project: project === "" ? "~" : project,
      })
      .then(onCreated)
      .catch((err: unknown) => {
        fail(setProblem, words.failed)(err);
        setSaving(false);
      });
  };
  return (
    <form onSubmit={create} className="flex min-w-0 flex-col gap-4">
      <Label text={words.name}>
        <input
          // biome-ignore lint/a11y/noAutofocus: the sheet opens because the person asked to add one
          autoFocus
          className={field}
          value={name}
          required
          maxLength={63}
          disabled={saving}
          onChange={(e) => setName(e.target.value)}
        />
      </Label>
      <Label text={words.when}>
        <select
          className={field}
          value={when}
          disabled={saving}
          onChange={(e) => setWhen(e.target.value)}
        >
          {presets.map((p) => (
            <option key={p.expression} value={p.expression}>
              {p.label}
            </option>
          ))}
          <option value={CUSTOM}>{words.custom}</option>
        </select>
      </Label>
      {when === CUSTOM && (
        <Label text={words.expression} hint={words.expressionHint}>
          <input
            className={`${field} font-aui-mono`}
            value={expression}
            required
            disabled={saving}
            onChange={(e) => setExpression(e.target.value)}
          />
        </Label>
      )}
      <Label text={words.agent}>
        <select
          className={field}
          value={agent}
          required
          disabled={saving}
          onChange={(e) => setAgent(e.target.value)}
        >
          {agents.map((a) => (
            <option key={a.spec.name} value={a.spec.name}>
              {a.spec.name}
            </option>
          ))}
        </select>
      </Label>
      <Label text={words.text}>
        <textarea
          className={`${field} h-auto min-h-24 py-2`}
          value={text}
          required
          disabled={saving}
          onChange={(e) => setText(e.target.value)}
        />
      </Label>
      <Problem message={problem} />
      <SheetButtons
        busy={saving}
        label={words.create}
        disabled={!name.trim() || !agent || !text.trim()}
        onCancel={onCancel}
      />
    </form>
  );
}

/** A Slack channel's id as Slack writes it: C for a public channel, G for a private one. */
const SLACK_CHANNEL = /^[CG][A-Z0-9]{2,}$/;

/**
 * Links a Slack channel to the project: a project channel bound to it, named
 * for it, so a session started there is filed here.
 */
function SlackForm({
  project,
  clients,
  onLinked,
  onCancel,
}: {
  project: string;
  clients: Client[];
  onLinked: () => void;
  onCancel: () => void;
}) {
  const [client, setClient] = useState(clients[0]?.id ?? "");
  const [channel, setChannel] = useState("");
  const [saving, setSaving] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const words = here.slack;
  if (clients.length === 0) return <p className="text-aui-sm text-aui-muted">{words.noClients}</p>;
  const link = (e: FormEvent) => {
    e.preventDefault();
    if (saving) return;
    const id = channel.trim().toUpperCase();
    if (!SLACK_CHANNEL.test(id)) {
      setProblem(words.invalid);
      return;
    }
    setSaving(true);
    setProblem(null);
    coworking
      .createChannel(project, {
        name: `slack-${id.toLowerCase()}`,
        slack: { client, channel: id },
      })
      .then(onLinked)
      .catch((err: unknown) => {
        fail(setProblem, words.failed)(err);
        setSaving(false);
      });
  };
  return (
    <form onSubmit={link} className="flex min-w-0 flex-col gap-4">
      <Label text={words.client}>
        <select
          className={field}
          value={client}
          disabled={saving}
          onChange={(e) => setClient(e.target.value)}
        >
          {clients.map((c) => (
            <option key={c.id} value={c.id}>
              {c.name || c.id}
            </option>
          ))}
        </select>
      </Label>
      <Label text={words.channel} hint={words.channelHint}>
        <input
          // biome-ignore lint/a11y/noAutofocus: the sheet opens because the person asked to link one
          autoFocus
          className={`${field} font-aui-mono`}
          value={channel}
          required
          maxLength={32}
          autoComplete="off"
          spellCheck={false}
          disabled={saving}
          onChange={(e) => setChannel(e.target.value)}
        />
      </Label>
      <Problem message={problem} />
      <SheetButtons
        busy={saving}
        label={words.link}
        disabled={!client || !channel.trim()}
        onCancel={onCancel}
      />
    </form>
  );
}

/** The Context card: the corpora the project attaches, each with how many sources it holds. */
function ContextCard({
  project,
  tick,
  canEdit,
  canList,
}: {
  project: ProjectView;
  tick: number;
  canEdit: boolean;
  /** Holds knowledge:read, which listing the workspace's corpora takes. */
  canList: boolean;
}) {
  const name = project.name;
  const [attached, setAttached] = useState<ProjectCorpora | null>(null);
  const [corpora, setCorpora] = useState<CorpusBrief[] | null>(null);
  const [reads, setReads] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and reads are signals to read again
  useEffect(() => {
    let live = true;
    api
      .projectCorpora(name)
      .then((v) => live && setAttached(v))
      .catch(() => live && setAttached(null));
    if (canEdit && canList)
      api
        .corpora()
        .then((list) => live && setCorpora(list))
        // A server without corpora answers 404: the card stays empty, with nothing to add.
        .catch(() => live && setCorpora(null));
    return () => {
      live = false;
    };
  }, [name, tick, reads, canEdit, canList]);
  const words = here.context;
  const some = (attached?.corpora.length ?? 0) > 0;
  return (
    <SideCard
      title={words.title}
      action={
        canEdit && attached && corpora
          ? {
              text: some ? words.edit : words.add,
              label: some ? words.editLabel : words.addLabel,
              heading: words.title,
            }
          : undefined
      }
      sheet={
        attached && corpora
          ? (close) => (
              <ContextForm
                project={name}
                attached={attached}
                corpora={corpora}
                onCancel={close}
                onStale={() => setReads((n) => n + 1)}
                onSaved={(next) => {
                  setAttached(next);
                  close();
                }}
              />
            )
          : undefined
      }
    >
      {some ? (
        <ul className={`${summary} flex flex-col gap-0.5`}>
          {attached?.corpora.map((c) => (
            <li key={c.name} className="break-words">
              {words.corpus(c.title || c.name, c.sources)}
            </li>
          ))}
        </ul>
      ) : (
        <p className={summary}>{words.none}</p>
      )}
    </SideCard>
  );
}

/** The Scheduled card: what runs in the project on a timer. Each run is a session here. */
function ScheduledCard({
  project,
  tick,
  agents,
  canAdd,
  onChanged,
}: {
  project: string;
  tick: number;
  agents: Revision[];
  canAdd: boolean;
  onChanged: () => void;
}) {
  const [schedules, setSchedules] = useState<ScheduleView[] | null>(null);
  const [reads, setReads] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and reads are signals to read again
  useEffect(() => {
    let live = true;
    api
      .projectSchedules(project)
      .then((list) => live && setSchedules(list))
      .catch(() => live && setSchedules([]));
    return () => {
      live = false;
    };
  }, [project, tick, reads]);
  const words = here.scheduled;
  return (
    <SideCard
      title={words.title}
      action={
        canAdd && agents.length > 0
          ? { text: words.add, label: words.addLabel, heading: words.heading }
          : undefined
      }
      sheet={(close) => (
        <ScheduleForm
          project={project}
          agents={agents}
          onCancel={close}
          onCreated={() => {
            setReads((n) => n + 1);
            onChanged();
            close();
          }}
        />
      )}
    >
      {schedules === null ? null : schedules.length === 0 ? (
        <p className={summary}>{words.none}</p>
      ) : (
        <ul className="flex min-w-0 flex-col gap-1.5">
          {schedules.map((s) => (
            <li key={s.name} className="min-w-0">
              <span className="block break-words text-aui-sm text-aui-fg">
                {s.description || s.name}
              </span>
              <span className="block text-aui-xs text-aui-muted">
                <span title={s.expression}>{inWords(s.expression)}</span>
                {s.paused ? `, ${words.paused.toLowerCase()}` : ""}
              </span>
            </li>
          ))}
        </ul>
      )}
    </SideCard>
  );
}

/** The Slack card: the Slack channels linked to the project, and a way to link or unlink one. */
function SlackCard({
  project,
  tick,
  canLink,
  canReadClients,
}: {
  project: string;
  tick: number;
  canLink: boolean;
  canReadClients: boolean;
}) {
  const [channels, setChannels] = useState<Channel[] | null>(null);
  const [clients, setClients] = useState<Client[]>([]);
  const [reads, setReads] = useState(0);
  const [problem, setProblem] = useState<string | null>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and reads are signals to read again
  useEffect(() => {
    let live = true;
    coworking
      .channels(project)
      .then((list) => live && setChannels(list))
      .catch(() => live && setChannels([]));
    if (canReadClients)
      api
        .clients()
        .then((r) => live && setClients(r.clients.filter((c) => c.surface === "slack")))
        .catch(() => live && setClients([]));
    return () => {
      live = false;
    };
  }, [project, tick, reads, canReadClients]);
  const words = here.slack;
  const links = (channels ?? []).filter(
    (c): c is Channel & { slack: NonNullable<Channel["slack"]> } => !!c.slack && !c.archived,
  );
  const clientName = (id: string) => clients.find((c) => c.id === id)?.name || id;
  return (
    <SideCard
      title={words.title}
      action={
        canLink ? { text: words.add, label: words.addLabel, heading: words.heading } : undefined
      }
      sheet={(close) => (
        <SlackForm
          project={project}
          clients={clients}
          onCancel={close}
          onLinked={() => {
            setReads((n) => n + 1);
            close();
          }}
        />
      )}
    >
      {channels === null ? null : links.length === 0 ? (
        <p className={summary}>{words.none}</p>
      ) : (
        <ul className="flex min-w-0 flex-col gap-1">
          {links.map((c) => (
            <li key={c.name} className="flex min-w-0 items-center justify-between gap-3">
              <span className={`${summary} break-words`}>
                {words.via(c.slack.channel, clientName(c.slack.client))}
              </span>
              {canLink && (
                <button
                  type="button"
                  className={`${textAction} shrink-0`}
                  aria-label={words.removeLabel(c.slack.channel)}
                  onClick={() => {
                    setProblem(null);
                    coworking
                      .archiveChannel(project, c.name)
                      .then(() => setReads((n) => n + 1))
                      .catch(fail(setProblem, words.removeFailed));
                  }}
                >
                  {words.remove}
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Problem message={problem} />
    </SideCard>
  );
}

const VISIBILITY_ICON: Record<ProjectVisibility, ReactNode> = {
  private: <UserIcon size={16} />,
  shared: <UsersIcon size={16} />,
  workspace: <LayersIcon size={16} />,
};

/**
 * One project, the workspace included, as its owner looks after it: a
 * composer to try it and its recent sessions, and beside them what every
 * session in it is given (instructions, context, memory), what runs on a
 * timer, the Slack channels linked to it and who can see it. It has no tabs,
 * and no agents of its own: agents are independent of projects, so a session
 * names who answered it.
 */
export function ProjectPage({
  composer,
  self,
  name,
  pinned = false,
  onPin,
  workspaceName,
  projects,
  agents,
  can,
  tick = 0,
  onOpen,
  onOpenAll,
  onOpenThread,
  onChanged,
  onSchedulesChanged,
  onDeleted,
  onFork,
}: {
  /**
   * The composer Home uses, drawn with the words given: a session started
   * with it is filed in this project and opens once it exists.
   */
  composer?: (placeholder: string) => ReactNode;
  /** The reader's handle, which Recents does not name among who answered. */
  self?: string;
  /** "" is the workspace. */
  name: string;
  pinned?: boolean;
  onPin?: (project: string, pinned: boolean) => Promise<void>;
  workspaceName: string;
  /** Every project, for the title shown while this one loads. */
  projects: ProjectView[];
  /** The agents a new schedule may ask. */
  agents: Revision[];
  can: (permission: string) => boolean;
  /** Changes when runs do, so what the project holds is read again. */
  tick?: number;
  /** Open another project: the one this was forked from. */
  onOpen: (name: string) => void;
  onOpenAll: () => void;
  /** Open a session. */
  onOpenThread: (id: string) => void;
  onChanged: () => void;
  onSchedulesChanged: () => void;
  onDeleted: () => void;
  onFork?: () => void;
}) {
  const [detail, setDetail] = useState<ProjectDetail | null>(null);
  const [missing, setMissing] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [editing, setEditing] = useState(false);
  const [reads, setReads] = useState(0);
  // Deleting cannot be undone, so it is asked twice.
  const [deleting, setDeleting] = useState(false);
  // A pin that did not take says so beside the menu.
  const { flash, say, clear } = useFlash();

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
      <Page
        title={words.missing(name === "" ? words.workspace : name)}
        lede={words.missingHint}
        back={{ label: words.allProjects, onClick: onOpenAll }}
      >
        {null}
      </Page>
    );
  }
  if (!detail) {
    const listed = projects.find((p) => p.name === name);
    return (
      <Page
        back={{ label: words.title, onClick: onOpenAll }}
        title={listed ? titleOf(listed) : name === "" ? workspaceName : name}
      >
        <SkeletonRows count={4} />
      </Page>
    );
  }

  const p = detail.project;
  const root = p.name === "";
  const title = titleOf(p, workspaceName);
  // What the caller may do here is their permission and their role together, as the server says.
  const may = (action: ProjectAction) => p.actions.includes(action);
  const canEdit = may("edit") && p.managed !== "file";
  const visibility = visibilityOf(p);
  const menu: PageAction[] = [
    ...(onPin
      ? [
          {
            label: pinned ? defaultCopy.pins.unpinProject : defaultCopy.pins.pinProject,
            onSelect: () => {
              clear();
              onPin(p.name || "~", !pinned).catch(() =>
                say(pinned ? defaultCopy.pins.unpinFailed : defaultCopy.pins.pinFailed, true),
              );
            },
          },
        ]
      : []),
    ...(may("edit") && !root
      ? [{ label: words.editProject, onSelect: () => setEditing(true) }]
      : []),
    ...(can("projects:write") && !root && !p.personal_owner && onFork
      ? [{ label: defaultCopy.projectFork.title, onSelect: onFork }]
      : []),
    ...(may("archive")
      ? [
          {
            label: p.archived ? defaultCopy.roles.unarchive : defaultCopy.roles.archive,
            // Restoring is not destructive; archiving is.
            destructive: !p.archived,
            onSelect: () =>
              api
                .updateProject(p.name, { ...updateOf(p), archived: !p.archived })
                .then(changed)
                .catch(fail(setProblem, words.failed.save)),
          },
        ]
      : []),
    ...(may("delete")
      ? [{ label: words.deleteProject, destructive: true, onSelect: () => setDeleting(true) }]
      : []),
  ];

  return (
    <Page
      back={{ label: words.title, onClick: onOpenAll }}
      title={<span className="block whitespace-normal break-words">{title}</span>}
      meta={
        p.fork?.ready ? (
          <span>
            {p.fork.source ? defaultCopy.projectFork.origin : defaultCopy.projectFork.originHidden}
            {p.fork.source && (
              <>
                {" "}
                <Go
                  to={href({ page: "project", name: p.fork.source })}
                  onGo={() => onOpen(p.fork?.source || "")}
                  className="text-aui-accent underline"
                >
                  {p.fork.source}
                </Go>
              </>
            )}
          </span>
        ) : undefined
      }
      actions={
        menu.length > 0 ? (
          <span className="relative inline-flex items-center">
            <MoreMenu items={menu} />
            <FlashNote flash={flash} align="right" />
          </span>
        ) : undefined
      }
    >
      {(p.archived || p.managed || problem || deleting) && (
        <div className="flex min-w-0 flex-col gap-2">
          {p.archived && <p className="text-aui-xs text-aui-muted">{defaultCopy.roles.archived}</p>}
          {p.managed && <p className="text-aui-xs text-aui-muted">{words.managed}</p>}
          <Problem message={problem} />
          {deleting && (
            <Confirm
              question={defaultCopy.confirm.ask(title)}
              action={words.deleteProject}
              onConfirm={() => {
                setDeleting(false);
                api
                  .deleteProject(p.name)
                  .then(onDeleted)
                  .catch(fail(setProblem, words.failed.delete));
              }}
              onKeep={() => setDeleting(false)}
            />
          )}
        </div>
      )}
      <div className="grid min-w-0 grid-cols-1 gap-8 @3xl:grid-cols-[minmax(0,1fr)_20rem]">
        <div className="flex min-w-0 flex-col gap-8">
          {composer?.(here.composer(title))}
          {can("runs:read") && (
            <Recents project={p.name} self={self} tick={tick} onOpen={onOpenThread} />
          )}
        </div>
        <aside aria-label={here.side} className="flex min-w-0 flex-col gap-3">
          <SideCard
            title={here.instructions.title}
            action={
              canEdit
                ? p.instructions
                  ? {
                      text: here.instructions.edit,
                      label: here.instructions.editLabel,
                      heading: here.instructions.title,
                    }
                  : {
                      text: here.instructions.add,
                      label: here.instructions.addLabel,
                      heading: here.instructions.title,
                    }
                : undefined
            }
            sheet={(close) => (
              <InstructionsForm
                project={p}
                onCancel={close}
                onSaved={() => {
                  close();
                  changed();
                }}
              />
            )}
          >
            <p className={`${summary} line-clamp-3 whitespace-pre-line break-words`}>
              {p.instructions || here.instructions.none}
            </p>
          </SideCard>
          <ContextCard project={p} tick={tick} canEdit={canEdit} canList={can("knowledge:read")} />
          <SideCard
            title={here.memory.title}
            action={{
              text: here.memory.view,
              label: here.memory.viewLabel,
              heading: here.memory.title,
            }}
            sheet={() => (
              <MemorySheet
                project={p.name}
                canDecide={may("notes")}
                canPropose={may("propose")}
                onChanged={changed}
              />
            )}
          >
            <p className={summary}>
              {p.notes + p.proposed > 0
                ? here.memory.summary(p.notes, p.proposed)
                : here.memory.none}
            </p>
          </SideCard>
          {can("schedules:read") && (
            <ScheduledCard
              project={p.name}
              tick={tick}
              agents={agents}
              canAdd={canEdit && can("schedules:write")}
              onChanged={onSchedulesChanged}
            />
          )}
          <SlackCard
            project={p.name}
            tick={tick}
            canLink={canEdit && can("clients:write")}
            canReadClients={can("clients:read")}
          />
          <SideCard
            plain
            title={here.visibility.title}
            action={
              may("edit")
                ? {
                    text: here.visibility.change,
                    label: here.visibility.changeLabel,
                    heading: here.visibility.heading,
                  }
                : undefined
            }
            sheet={() => (
              <ProjectPeople key={`${p.name}:${p.version}`} project={p} onChanged={changed} />
            )}
          >
            <span className="inline-flex min-w-0 items-center gap-2 text-aui-sm text-aui-muted">
              {VISIBILITY_ICON[visibility]}
              {here.visibility[visibility]}
            </span>
          </SideCard>
        </aside>
      </div>
      <Sheet open={editing} onOpenChange={(next) => !next && setEditing(false)}>
        <SheetContent aria-describedby={undefined}>
          <SheetHeader>
            <SheetTitle className="text-aui-base">{here.editHeading}</SheetTitle>
          </SheetHeader>
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
        </SheetContent>
      </Sheet>
    </Page>
  );
}
