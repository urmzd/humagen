import { defaultCopy } from "@humagen/components";
import {
  ArchiveIcon,
  BellIcon,
  BotIcon,
  CheckIcon,
  EllipsisIcon,
  LayersIcon,
  LogOutIcon,
  MonitorIcon,
  MoonIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  PencilIcon,
  PinIcon,
  PlusIcon,
  ProjectIcon,
  SearchIcon,
  SettingsIcon,
  SlidersIcon,
  SunIcon,
  Tooltip,
  TrashIcon,
  UserIcon,
  UsersIcon,
} from "@humagen/render-web";
import { Separator } from "@humagen/render-web/ui/separator";
import { HumagenMark } from "@humagen/web";
import { type MouseEvent, type ReactNode, useEffect, useRef, useState } from "react";
import {
  coworking,
  type Me,
  type Preferences,
  type ProjectView,
  type ThreadSummary,
} from "./api.ts";
import { type Branding, servedBranding } from "./branding.ts";
import { type Standing, useRecents } from "./recents.ts";
import { href, type Route, type Page as RoutePage } from "./route.ts";
import { Avatar } from "./Settings.tsx";
import { AgentMark, type Flash, FlashNote, Popover, SkeletonRows, useFlash } from "./ui.tsx";
import { hubTabs, workspaceHub } from "./workspace-navigation.ts";

/** A link to a view. A plain click opens it in place; a modified one, in a new tab. */
export function SidebarItem({
  icon,
  label,
  href,
  active,
  open,
  badge,
  badgeLabel,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  href: string;
  active: boolean;
  open: boolean;
  badge?: number;
  badgeLabel?: string;
  onClick: () => void;
}) {
  const count = badge && badge > 0 ? badge : 0;
  const go = (e: MouseEvent<HTMLAnchorElement>) => {
    if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
    e.preventDefault();
    onClick();
  };
  const link = (
    <a
      href={href}
      onClick={go}
      aria-current={active ? "page" : undefined}
      aria-label={count ? (badgeLabel ?? `${label}, ${count} need you`) : open ? undefined : label}
      className={`relative flex h-9 pointer-coarse:h-11 items-center gap-2.5 [&>svg]:size-[18px] rounded-aui text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:font-medium aria-[current=page]:text-aui-fg ${
        open ? "px-2.5" : "w-9 justify-center"
      }`}
    >
      {icon}
      {open && <span className="humagen-sidebar-reveal flex-1 truncate text-left">{label}</span>}
      {count > 0 &&
        (open ? (
          <span
            aria-hidden
            className="humagen-sidebar-reveal rounded-full bg-aui-accent-soft px-1.5 text-aui-2xs font-medium text-aui-fg"
          >
            {count}
          </span>
        ) : (
          <span
            aria-hidden
            className="absolute top-1 right-1 size-1.5 rounded-full bg-aui-accent"
          />
        ))}
    </a>
  );
  return open ? link : <Tooltip label={label}>{link}</Tooltip>;
}

const Item = SidebarItem;

/** One group in the filter panel: a title, then choices where exactly one is on. */
function Choices<T extends string>({
  title,
  value,
  options,
  onChange,
}: {
  title: string;
  value: T;
  options: { value: T; label: string; mark?: ReactNode }[];
  onChange: (next: T) => void;
}) {
  return (
    <fieldset className="flex flex-col pb-1">
      <legend className="px-2.5 pt-2 pb-1 text-aui-xs text-aui-muted">{title}</legend>
      {options.map((o) => (
        <button
          key={o.value}
          type="button"
          aria-pressed={o.value === value}
          onClick={() => onChange(o.value)}
          className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          {o.mark}
          <span className="min-w-0 flex-1 truncate">{o.label}</span>
          {o.value === value && <CheckIcon size={14} />}
        </button>
      ))}
    </fieldset>
  );
}

/** Pin controls stay visible when pinned, focused or used with a touch pointer. */
function PinButton({
  pinned,
  label,
  onClick,
  project = false,
  thread,
  projectId,
}: {
  pinned: boolean;
  label: string;
  onClick: (button: HTMLButtonElement) => void;
  project?: boolean;
  thread?: string;
  projectId?: string;
}) {
  return (
    <button
      type="button"
      onClick={(event) => onClick(event.currentTarget)}
      data-pin-thread={thread}
      data-pin-project={projectId}
      aria-label={label}
      aria-pressed={pinned}
      title={label}
      className={`absolute top-1 right-1 inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent [@media(pointer:coarse)]:top-0.5 [@media(pointer:coarse)]:size-11 [@media(pointer:coarse)]:opacity-100 ${project ? "group-hover/project:opacity-100 group-focus-within/project:opacity-100" : "group-hover/task:opacity-100 group-focus-within/task:opacity-100"} ${pinned ? "opacity-100" : "opacity-0"}`}
    >
      <PinIcon size={14} fill={pinned ? "currentColor" : "none"} />
    </button>
  );
}

/**
 * A project's conversations under a header that folds them. The chevron says
 * which way it is; the group opens itself when the open conversation is in
 * it, and while folded over the open conversation the header carries the
 * selection, so where you are never disappears.
 */
function ProjectGroup({
  title,
  holdsCurrent,
  children,
}: {
  title: string;
  holdsCurrent: boolean;
  children: ReactNode;
}) {
  const [open, setOpen] = useState(true);
  useEffect(() => {
    if (holdsCurrent) setOpen(true);
  }, [holdsCurrent]);
  return (
    <details open={open} onToggle={(e) => setOpen(e.currentTarget.open)}>
      <summary
        aria-current={!open && holdsCurrent ? "true" : undefined}
        className="flex min-h-10 cursor-pointer list-none items-center gap-1.5 rounded-aui pr-12 pl-1.5 text-aui-xs font-medium text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current]:bg-aui-subtle aria-[current]:text-aui-fg aria-[current]:shadow-[inset_2px_0_0_var(--aui-accent)] [&::-webkit-details-marker]:hidden"
      >
        <svg
          aria-hidden="true"
          focusable="false"
          width={12}
          height={12}
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth={2.25}
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`shrink-0 transition-transform duration-150 motion-reduce:transition-none ${open ? "rotate-90" : ""}`}
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
        <ProjectIcon size={14} />
        <span className="min-w-0 flex-1 truncate">{title}</span>
      </summary>
      {children}
    </details>
  );
}

/** One conversation by its title, with a ⋯ menu to pin, rename, archive or delete it. */
function TaskRow({
  task,
  projectTitle,
  current,
  pinned,
  onSelect,
  onPin,
  onRename,
  onArchive,
  flash = null,
}: {
  task: ThreadSummary;
  projectTitle: string;
  current: boolean;
  pinned: boolean;
  onSelect: () => void;
  onPin?: (button: HTMLButtonElement) => void;
  onRename?: (title: string) => void;
  /** Archives it; deleteNow asks an admin to delete it at once. */
  onArchive?: (deleteNow: boolean) => void;
  flash?: Flash | null;
}) {
  const title = task.title || "Untitled conversation";
  const state = task.active > 0 ? "In progress" : task.failed > 0 ? "Needs attention" : "Finished";
  const [renaming, setRenaming] = useState(false);
  const menu = useRef<HTMLDivElement>(null);
  const trigger = () => menu.current?.querySelector<HTMLButtonElement>("button") ?? null;
  const hasMenu = onPin || onRename || onArchive;
  const save = (next: string) => {
    setRenaming(false);
    const t = next.trim();
    if (t && t !== task.title) onRename?.(t);
    requestAnimationFrame(() => trigger()?.focus());
  };
  return (
    // A row that is saying something sits over its neighbours and over the
    // section's label, which sticks above the first row, so the note shows.
    <li className={`group/task relative humagen-enter ${flash ? "z-20" : ""}`}>
      {renaming ? (
        <input
          // biome-ignore lint/a11y/noAutofocus: rename is asked for from the menu
          autoFocus
          aria-label={`Rename ${title}`}
          defaultValue={task.title}
          maxLength={200}
          onFocus={(e) => e.currentTarget.select()}
          onBlur={(e) => save(e.currentTarget.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") save(e.currentTarget.value);
            if (e.key === "Escape") {
              e.stopPropagation();
              setRenaming(false);
            }
          }}
          className="h-12 w-full rounded-aui border border-aui-accent bg-transparent px-2.5 text-aui-sm text-aui-fg outline-none"
        />
      ) : (
        <button
          type="button"
          onClick={onSelect}
          title={`${title}\n${state}\n${projectTitle}`}
          aria-current={current ? "page" : undefined}
          className="flex min-h-10 w-full items-center gap-2 rounded-aui py-2 pr-10 pl-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-subtle aria-[current=page]:font-medium aria-[current=page]:text-aui-fg aria-[current=page]:shadow-[inset_2px_0_0_var(--aui-accent)] [@media(pointer:coarse)]:pr-14"
        >
          <span className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="flex min-w-0 items-center gap-1.5">
              <span className="truncate">{title}</span>
              {pinned && (
                <PinIcon
                  size={11}
                  fill="currentColor"
                  className="shrink-0 text-aui-muted"
                  aria-hidden
                />
              )}
            </span>
          </span>
          {task.active > 0 && <span className="sr-only">, running</span>}
          {task.active === 0 && task.failed > 0 && (
            <span className="sr-only">, a reply failed</span>
          )}
        </button>
      )}
      {hasMenu && !renaming && (
        <div
          ref={menu}
          data-pin-thread={task.id}
          className="absolute top-1 right-1 [@media(pointer:coarse)]:top-0.5"
        >
          <Popover
            narrow
            align="right"
            label={`Manage ${title}`}
            className="inline-flex size-8 items-center justify-center rounded-aui text-aui-muted opacity-0 transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent group-hover/task:opacity-100 group-focus-within/task:opacity-100 aria-expanded:opacity-100 aria-expanded:bg-aui-hover aria-expanded:text-aui-fg [@media(pointer:coarse)]:size-11 [@media(pointer:coarse)]:opacity-100"
            button={<EllipsisIcon size={16} />}
          >
            {(close) => (
              <RowMenu
                pinned={pinned}
                onPin={
                  onPin &&
                  (() => {
                    close();
                    const b = trigger();
                    if (b) onPin(b);
                  })
                }
                onRename={
                  onRename &&
                  (() => {
                    close();
                    setRenaming(true);
                  })
                }
                onArchive={
                  onArchive &&
                  ((now) => {
                    close();
                    onArchive(now);
                  })
                }
              />
            )}
          </Popover>
        </div>
      )}
      {hasMenu && <FlashNote flash={flash} align="right" />}
    </li>
  );
}

const menuItem =
  "flex h-8 w-full items-center gap-2.5 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * What a conversation's ⋯ menu offers. Each item has a letter that works while
 * the menu is open. Delete asks once more, since an admin then removes it.
 */
function RowMenu({
  pinned,
  onPin,
  onRename,
  onArchive,
}: {
  pinned: boolean;
  onPin?: () => void;
  onRename?: () => void;
  onArchive?: (deleteNow: boolean) => void;
}) {
  const [confirming, setConfirming] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>("button")?.focus();
  }, [confirming]);
  const items = [
    onPin && { key: "p", label: pinned ? "Unpin" : "Pin", icon: <PinIcon size={15} />, run: onPin },
    onRename && { key: "r", label: "Rename", icon: <PencilIcon size={15} />, run: onRename },
    onArchive && {
      key: "a",
      label: "Archive",
      icon: <ArchiveIcon size={15} />,
      run: () => onArchive(false),
      separated: true,
    },
    onArchive && {
      key: "d",
      label: "Delete",
      icon: <TrashIcon size={15} />,
      run: () => setConfirming(true),
      danger: true,
    },
  ].filter((i): i is Exclude<typeof i, undefined | null | false> => Boolean(i));
  if (confirming && onArchive) {
    return (
      <div ref={root} className="flex flex-col gap-2 p-1.5">
        <p className="text-aui-sm text-aui-fg">Delete this conversation?</p>
        <p className="text-aui-xs text-aui-muted">
          It is archived now and removed once an admin approves.
        </p>
        <div className="flex justify-end gap-1.5">
          <button
            type="button"
            onClick={() => setConfirming(false)}
            className="h-7 rounded-aui px-2.5 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={() => onArchive(true)}
            className="h-7 rounded-aui bg-aui-bad px-2.5 text-aui-sm text-white hover:opacity-90 focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            Delete
          </button>
        </div>
      </div>
    );
  }
  return (
    <div
      ref={root}
      role="menu"
      className="flex flex-col"
      onKeyDown={(e) => {
        if (e.metaKey || e.ctrlKey || e.altKey) return;
        const hit = items.find((i) => i.key === e.key.toLowerCase());
        if (hit) {
          e.preventDefault();
          hit.run();
        }
      }}
    >
      {items.map((i) => (
        <div key={i.key}>
          {i.separated && <Separator className="my-1" />}
          <button
            type="button"
            role="menuitem"
            onClick={i.run}
            className={`${menuItem} ${i.danger ? "text-aui-bad" : ""}`}
          >
            {i.icon}
            <span className="min-w-0 flex-1 truncate">{i.label}</span>
            <kbd className="text-aui-2xs text-aui-muted">{i.key.toUpperCase()}</kbd>
          </button>
        </div>
      ))}
    </div>
  );
}

/** A small section label, as Claude's sidebar draws Pinned and Recents. */
function Label({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="aui-chrome sticky top-0 z-10 flex h-8 items-center justify-between gap-1 pr-2 pl-2 shadow-none">
      <p className="px-2 text-aui-xs text-aui-muted">{children}</p>
      {action}
    </div>
  );
}

/**
 * The console's navigation, laid out like Claude's: one flat list of pages,
 * then pinned tasks, then recent ones. Closed, it is a rail of icons. The open state is the caller's to persist.
 */
/**
 * The workspace's logo, in the theme's variant when it has one, or
 * Humagen's mark when it sets none. Until the
 * branding is read the place is kept empty, so one mark never flashes in
 * for another.
 */
function BrandMark({ branding }: { branding: Branding | null }) {
  if (!branding) return <span aria-hidden className="inline-block size-[23px] shrink-0" />;
  if (!branding.logo) return <HumagenMark size={23} />;
  const alt = branding.wordmark ? "" : branding.logo_alt;
  const img = "h-[23px] w-auto max-w-[8rem] shrink-0 object-contain";
  if (!branding.logo_dark) return <img src={branding.logo} alt={alt} className={img} />;
  // One per theme, so a logo in dark ink is not lost on a dark sidebar.
  return (
    <>
      <img src={branding.logo} alt={alt} className={`${img} dark:hidden`} />
      <img src={branding.logo_dark} alt={alt} className={`${img} hidden dark:block`} />
    </>
  );
}

export function Sidebar({
  open,
  onToggle,
  route,
  go,
  me,
  prefs,
  can,
  threads,
  projects = [],
  workspaceName,
  branding,
  pinned,
  onPin,
  onPinProject,
  inboxCount,
  live,
  onSignOut,
  onTheme,
  onSearch,
  notificationCount,
}: {
  open: boolean;
  onToggle: () => void;
  route: Route;
  go: (next: Route) => void;
  me: Me;
  prefs: Preferences | null;
  can: (permission: string) => boolean;
  threads: ThreadSummary[];
  projects?: ProjectView[];
  workspaceName?: string;
  /** The workspace's logo and wordmark, null until read. */
  branding?: Branding | null;
  /** Pinned conversation ids, most recently pinned first. */
  pinned: string[];
  /** Resolves once the pin is saved, and rejects when it could not be. */
  onPin: (id: string, pin: boolean) => Promise<void>;
  onPinProject?: (project: string, pin: boolean) => Promise<void>;
  inboxCount: number;
  /** Updates are arriving. When false, the sidebar says so. */
  live: boolean;
  onSignOut: () => void;
  /** Saves the theme picked from the account menu. */
  onTheme?: (theme: Preferences["theme"]) => void;
  onSearch?: () => void;
  notificationCount?: number;
}) {
  const projectTitle = (project?: string) =>
    me.personal_project && project === me.personal_project
      ? defaultCopy.projects.personalTitle
      : !project || project === "~"
        ? workspaceName || defaultCopy.projects.companyName
        : projects.find((item) => item.name === project)?.title || project;
  // Left out, as a story or a test does, the page's own name brands it.
  const brand = branding === undefined ? servedBranding() : branding;
  const who = me.principal;
  const dev = who.subject === "dev";
  const name = prefs?.display_name || who.name || who.email || who.subject;
  const subtitle = dev ? "Dev mode, no sign-in" : (who.roles ?? []).join(", ") || who.tenant;
  // Recents is what people asked for. Scheduled and eval runs live on their own
  // pages, so a timer cannot push a person's work off the list.
  const [standing, setStanding] = useState<Standing>("");
  const [agent, setAgent] = useState("");
  const answering = [
    ...new Set(threads.filter((t) => t.origin === "api").flatMap((t) => t.agents)),
  ].sort();
  // The list pages itself. The caller's threads change when a run starts or
  // ends, which is the signal to fetch the first page again.
  const {
    threads: recents,
    hasMore,
    loading,
    failed,
    stalled,
    retry,
    more,
  } = useRecents(agent, standing, threads);
  const filters = (agent ? 1 : 0) + (standing ? 1 : 0);
  // A pin that did not take is said beside the conversation it was for, in whichever
  // list the conversation is in by then.
  const { flash, say, clear: unsay } = useFlash();
  const [flashed, setFlashed] = useState<string | null>(null);
  const sidebar = useRef<HTMLElement>(null);
  const pinFocus = useRef<{ kind: "conversation" | "project"; id: string; pinned: boolean } | null>(
    null,
  );
  useEffect(() => {
    const target = pinFocus.current;
    if (!target) return;
    const projects = target.kind === "project";
    const list = projects ? (prefs?.pinned_projects ?? []) : pinned;
    if (list.includes(target.id) !== target.pinned) return;
    const control = projects
      ? Array.from(
          sidebar.current?.querySelectorAll<HTMLButtonElement>("[data-pin-project]") ?? [],
        ).find((button) => button.dataset.pinProject === target.id)
      : Array.from(sidebar.current?.querySelectorAll<HTMLElement>("[data-pin-thread]") ?? [])
          .find((menu) => menu.dataset.pinThread === target.id)
          ?.querySelector<HTMLButtonElement>("button");
    // An unpinned row may leave the current group or loaded page.
    // Keep focus at its remaining control or a stable navigation target.
    const fallback = projects
      ? sidebar.current?.querySelector<HTMLAnchorElement>(
          'nav[aria-label="Sections"] a[href="/projects"]',
        )
      : sidebar.current?.querySelector<HTMLButtonElement>(
          'section[aria-label="Conversations"] button',
        );
    (control ?? fallback)?.focus();
    pinFocus.current = null;
  }, [pinned, prefs?.pinned_projects]);
  const togglePin = (id: string, on: boolean, button: HTMLButtonElement) => {
    unsay();
    if (document.activeElement === button)
      pinFocus.current = { kind: "conversation", id, pinned: on };
    onPin(id, on).catch(() => {
      if (pinFocus.current?.kind === "conversation" && pinFocus.current.id === id)
        pinFocus.current = null;
      setFlashed(id);
      say(on ? defaultCopy.pins.pinFailed : defaultCopy.pins.unpinFailed, true);
    });
  };
  const toggleProjectPin = (project: string, on: boolean, button: HTMLButtonElement) => {
    unsay();
    if (document.activeElement === button)
      pinFocus.current = { kind: "project", id: project, pinned: on };
    onPinProject?.(project, on).catch(() => {
      if (pinFocus.current?.kind === "project" && pinFocus.current.id === project)
        pinFocus.current = null;
      setFlashed(`project:${project}`);
      say(on ? defaultCopy.pins.pinFailed : defaultCopy.pins.unpinFailed, true);
    });
  };

  // Every page is hidden from someone who may not read it, so nothing in the
  // menu answers with a refusal. Work comes first, then what watches it, then
  // what it is built from. A task keeps its own runs, evals live on what they
  // test, and trends live with alerts, so none of those needs a page here.
  const selected = route.page === "conversation" ? route.id : null;
  const here = (...pages: RoutePage[]) => pages.includes(route.page);
  const nav = defaultCopy.navigation;
  const hub = workspaceHub(route);
  const notificationRoute: Route = { page: "notifications" };
  const libraryTabs = hubTabs("library", route, can);
  const agentTabs = hubTabs("agents", route, can);
  const operationTabs = hubTabs("operations", route, can);
  const entries: {
    to: Route;
    label: string;
    icon: ReactNode;
    show: boolean;
    active: boolean;
    badge?: number;
    group: "projects" | "library" | "operations";
  }[] = [
    {
      to: { page: "projects" },
      label: nav.projects,
      icon: <ProjectIcon />,
      show: can("projects:read"),
      active: here("projects", "project", "channel"),
      group: "projects",
    },
    {
      to: { page: "directory", tab: "people" },
      label: nav.people,
      icon: <UsersIcon />,
      show: can("people:read"),
      active: here("directory"),
      group: "projects",
    },
    {
      to: libraryTabs[0]?.route ?? { page: "library", section: "sources" },
      label: nav.library,
      icon: <LayersIcon />,
      show: libraryTabs.length > 0,
      active: hub === "library",
      group: "library",
    },
    {
      to: agentTabs[0]?.route ?? { page: "agents" },
      label: nav.agents,
      icon: <BotIcon />,
      show: agentTabs.length > 0,
      active: hub === "agents",
      group: "operations",
    },
    {
      to: operationTabs[0]?.route ?? { page: "operations", section: "context" },
      label: nav.operations,
      icon: <SlidersIcon />,
      show: operationTabs.length > 0,
      active: hub === "operations",
      group: "operations",
    },
  ];

  // Pinned conversations come from whatever is loaded. One that cannot be found
  // (deleted, or no longer readable) is not shown.
  const [archived, setArchived] = useState<{
    id: string;
    title: string;
    deleteNow: boolean;
  } | null>(null);
  const [gone, setGone] = useState<Set<string>>(() => new Set());
  const [titles, setTitles] = useState<Record<string, string>>({});
  const shown = (t: ThreadSummary) =>
    titles[t.id] ? { ...t, title: titles[t.id]!, named: true } : t;
  const archive = (t: ThreadSummary, deleteNow: boolean) => {
    unsay();
    setGone((g) => new Set(g).add(t.id));
    setArchived({ id: t.id, title: t.title || "Untitled conversation", deleteNow });
    if (pinned.includes(t.id)) onPin(t.id, false).catch(() => {});
    if (selected === t.id) go({ page: "new" });
    coworking.archive(t.id, deleteNow).catch(() => {
      setGone((g) => {
        const n = new Set(g);
        n.delete(t.id);
        return n;
      });
      setArchived(null);
      setFlashed(t.id);
      say(deleteNow ? "Could not delete. Try again." : "Could not archive. Try again.", true);
    });
  };
  const undoArchive = () => {
    if (!archived) return;
    const { id } = archived;
    setArchived(null);
    coworking.unarchive(id).then(
      () =>
        setGone((g) => {
          const n = new Set(g);
          n.delete(id);
          return n;
        }),
      () => {},
    );
  };
  const rename = (t: ThreadSummary, title: string) => {
    unsay();
    const was = titles[t.id];
    setTitles((m) => ({ ...m, [t.id]: title }));
    coworking.rename(t.id, title).catch(() => {
      setTitles((m) => {
        const n = { ...m };
        if (was) n[t.id] = was;
        else delete n[t.id];
        return n;
      });
      setFlashed(t.id);
      say("Could not rename. Try again.", true);
    });
  };
  const known = new Map(
    [...threads, ...recents].filter((t) => !gone.has(t.id)).map((t) => [t.id, shown(t)]),
  );
  const pinnedRows = pinned
    .map((id) => known.get(id))
    .filter((t): t is ThreadSummary => t !== undefined);
  const pinnedSet = new Set(pinned);
  const unpinned = recents.filter((t) => !pinnedSet.has(t.id) && !gone.has(t.id)).map(shown);
  const currentThread = selected ? known.get(selected) : undefined;
  if (
    currentThread &&
    !pinnedSet.has(currentThread.id) &&
    !unpinned.some((thread) => thread.id === currentThread.id)
  ) {
    unpinned.push(currentThread);
  }
  const noConversations = recents.length === 0 && unpinned.length === 0;
  const isCurrent = (t: ThreadSummary) => t.id === selected;
  const canPin = can("runs:read");
  const pinnedProjects = new Set(prefs?.pinned_projects ?? []);
  const readableProjectPins = [...pinnedProjects].filter(
    (project) =>
      can("projects:read") && (project === "~" || projects.some((item) => item.name === project)),
  );
  const projectGroups = [
    ...new Set(
      [...recents.filter((t) => !gone.has(t.id)), ...unpinned].map(
        (thread) => thread.project || "~",
      ),
    ),
  ];

  const clear = () => {
    setStanding("");
    setAgent("");
  };

  return (
    <>
      {open && (
        <button
          type="button"
          tabIndex={-1}
          aria-label="Dismiss navigation"
          onClick={onToggle}
          className="humagen-fade fixed inset-0 z-40 bg-black/40 md:hidden"
        />
      )}
      <aside
        ref={sidebar}
        aria-label="Navigation"
        className={`aui-chrome humagen-glass-bar humagen-sidebar flex h-full min-h-0 shrink-0 flex-col max-md:border-aui-border max-md:border-r ${
          open
            ? "w-64 max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-50 max-md:w-[min(20rem,85vw)]"
            : "w-12 max-md:hidden"
        }`}
      >
        <div
          className={`flex h-12 shrink-0 items-center ${open ? "justify-between pr-2 pl-3.5" : "justify-center"}`}
        >
          {open && (
            <span className="humagen-display humagen-sidebar-reveal flex min-w-0 items-center gap-2 truncate text-aui-base font-semibold text-aui-fg">
              <BrandMark branding={brand} />
              {brand?.wordmark}
            </span>
          )}
          <button
            type="button"
            onClick={onToggle}
            aria-expanded={open}
            aria-label={open ? "Close sidebar" : "Open sidebar"}
            className="inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {open ? <PanelLeftCloseIcon size={18} /> : <PanelLeftOpenIcon size={18} />}
          </button>
        </div>

        {/* Everything between the top row and the account scrolls as one, so a short window never squeezes the lists into a sliver. */}
        <div className="aui-scroll flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto overscroll-contain">
          <nav
            aria-label="Sections"
            className={`flex shrink-0 flex-col gap-1 ${open ? "px-2" : "items-center"}`}
          >
            {can("runs:write") && (
              <Item
                icon={<PlusIcon />}
                label={nav.newConversation}
                href={href({ page: "new" })}
                active={here("new")}
                open={open}
                onClick={() => go({ page: "new" })}
              />
            )}
            {onSearch && (
              <button
                type="button"
                onClick={onSearch}
                aria-label={nav.search}
                className={`flex h-9 pointer-coarse:h-11 items-center gap-2.5 rounded-aui text-aui-sm text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent ${open ? "px-2.5" : "w-9 justify-center"}`}
              >
                <SearchIcon size={18} />
                {open && (
                  <>
                    <span className="humagen-sidebar-reveal flex-1 text-left">{nav.search}</span>
                    <kbd className="humagen-sidebar-reveal text-aui-2xs">⌘ K</kbd>
                  </>
                )}
              </button>
            )}

            {(can("runs:read") || can("alerts:read")) && (
              <Item
                icon={<BellIcon />}
                label={nav.notifications}
                href={href(
                  can("runs:read") ? notificationRoute : { page: "monitor", tab: "rules" },
                )}
                open={open}
                active={hub === "notifications"}
                badge={inboxCount}
                onClick={() =>
                  go(can("runs:read") ? notificationRoute : { page: "monitor", tab: "rules" })
                }
              />
            )}
            {(["projects", "library", "operations"] as const).map((group) => {
              const shown = entries.filter((entry) => entry.group === group && entry.show);
              return (
                shown.length > 0 && (
                  <section
                    key={group}
                    aria-label={nav[group]}
                    className={group === "library" ? "" : "mt-2"}
                  >
                    {group !== "library" && <Separator className="mb-3" />}
                    <ul className={`flex flex-col gap-0.5 ${open ? "" : "items-center"}`}>
                      {shown.map((entry) => (
                        <li key={entry.label}>
                          <Item
                            icon={entry.icon}
                            label={entry.label}
                            href={href(entry.to)}
                            open={open}
                            active={entry.active}
                            badge={entry.badge}
                            onClick={() => go(entry.to)}
                          />
                        </li>
                      ))}
                    </ul>
                  </section>
                )
              );
            })}
          </nav>

          {open && (pinnedRows.length > 0 || readableProjectPins.length > 0) && (
            <section
              aria-label="Pinned"
              className="humagen-sidebar-reveal flex shrink-0 flex-col pt-4"
            >
              <Label>{defaultCopy.sidebar.pinned}</Label>
              <ul className="px-2">
                {readableProjectPins.map((project) => (
                  <li
                    key={`project:${project}`}
                    className={`group/project relative ${flashed === `project:${project}` && flash ? "z-20" : ""}`}
                  >
                    <div className="py-1 pr-10 [@media(pointer:coarse)]:pr-12">
                      <Item
                        icon={<ProjectIcon />}
                        label={projectTitle(project)}
                        href={href({
                          page: "project",
                          name: project === "~" ? "" : project,
                          tab: "inbox",
                        })}
                        active={
                          route.page === "project" &&
                          route.name === (project === "~" ? "" : project)
                        }
                        open
                        onClick={() =>
                          go({
                            page: "project",
                            name: project === "~" ? "" : project,
                            tab: "inbox",
                          })
                        }
                      />
                    </div>
                    {onPinProject && (
                      <PinButton
                        pinned
                        project
                        projectId={project}
                        label={defaultCopy.sidebar.pinProject(projectTitle(project), true)}
                        onClick={(button) => toggleProjectPin(project, false, button)}
                      />
                    )}
                    <FlashNote
                      flash={flashed === `project:${project}` ? flash : null}
                      align="right"
                    />
                  </li>
                ))}
                {pinnedRows.map((t) => (
                  <TaskRow
                    key={t.id}
                    task={t}
                    projectTitle={projectTitle(t.project)}
                    current={isCurrent(t)}
                    pinned
                    onSelect={() => go({ page: "conversation", id: t.id })}
                    onPin={(button) => togglePin(t.id, false, button)}
                    onRename={(title) => rename(t, title)}
                    onArchive={(now) => archive(t, now)}
                    flash={flashed === t.id ? flash : null}
                  />
                ))}
              </ul>
            </section>
          )}

          {open && can("runs:read") && (
            <section
              aria-label="Conversations"
              className="humagen-sidebar-reveal flex shrink-0 flex-col pt-4"
            >
              {/* The label stays in reach while the list scrolls under it. */}
              <Label
                action={
                  <Popover
                    narrow
                    align="right"
                    label={defaultCopy.chat.filter(filters)}
                    className="relative inline-flex size-6 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover aria-expanded:text-aui-fg"
                    button={
                      <>
                        <SlidersIcon size={14} />
                        {filters > 0 && (
                          <span
                            aria-hidden
                            className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-aui-accent"
                          />
                        )}
                      </>
                    }
                  >
                    {() => (
                      <div className="aui-scroll flex max-h-[60vh] flex-col overflow-y-auto">
                        <Choices
                          title="Show"
                          value={standing}
                          options={[
                            { value: "", label: "Every conversation" },
                            { value: "running", label: "Running now" },
                            { value: "failed", label: "Needs attention" },
                          ]}
                          onChange={setStanding}
                        />
                        {answering.length > 1 && (
                          <Choices
                            title="Answered by"
                            value={agent}
                            options={[
                              { value: "", label: "Any agent" },
                              ...answering.map((a) => ({
                                value: a,
                                label: a,
                                mark: <AgentMark name={a} size={12} />,
                              })),
                            ]}
                            onChange={setAgent}
                          />
                        )}
                        {filters > 0 && (
                          <button
                            type="button"
                            onClick={clear}
                            className="mt-1 rounded-aui border-[var(--humagen-rule)] border-t px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                          >
                            Clear filters
                          </button>
                        )}
                      </div>
                    )}
                  </Popover>
                }
              >
                Conversations
              </Label>
              {archived && (
                <p
                  role="status"
                  className="mx-3 mb-1 flex items-center gap-2 rounded-aui bg-aui-hover px-2.5 py-1.5 text-aui-xs text-aui-muted"
                >
                  <span className="min-w-0 flex-1">
                    {archived.deleteNow
                      ? "Sent to an admin to delete."
                      : "Archived. An admin can delete it after 30 days."}
                  </span>
                  <button
                    type="button"
                    onClick={undoArchive}
                    className="shrink-0 rounded-aui px-1.5 font-medium text-aui-fg hover:bg-aui-bg focus-visible:outline-2 focus-visible:outline-aui-accent"
                  >
                    Undo
                  </button>
                  <button
                    type="button"
                    onClick={() => setArchived(null)}
                    aria-label="Dismiss"
                    className="shrink-0 rounded-aui px-1 text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                  >
                    ×
                  </button>
                </p>
              )}
              <ul className="px-2 pb-3" aria-label="Conversations" aria-busy={loading !== null}>
                {noConversations && loading === "first" && (
                  <li className="px-2.5">
                    <SkeletonRows count={5} height="1.25rem" />
                  </li>
                )}
                {noConversations && loading !== "first" && stalled && (
                  <li className="flex flex-col items-start gap-1.5 px-2.5 py-1.5 text-aui-xs text-aui-muted">
                    Your conversations could not be loaded.
                    <button
                      type="button"
                      onClick={retry}
                      className="humagen-press inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      Try Again
                    </button>
                  </li>
                )}
                {noConversations && loading !== "first" && !stalled && (
                  <li className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                    {filters > 0
                      ? "No conversation matches these filters."
                      : "Your conversations appear here. Start with New."}
                  </li>
                )}
                {projectGroups.map((project) => (
                  <li
                    key={project}
                    className={`group/project relative min-w-0 pt-2 ${flashed === `project:${project}` && flash ? "z-20" : ""}`}
                  >
                    <ProjectGroup
                      title={projectTitle(project)}
                      holdsCurrent={unpinned.some(
                        (t) => (t.project || "~") === project && isCurrent(t),
                      )}
                    >
                      <ul className="ml-2 border-aui-border border-l pl-1">
                        {unpinned
                          .filter((thread) => (thread.project || "~") === project)
                          .map((t) => (
                            <TaskRow
                              key={t.id}
                              task={t}
                              projectTitle={projectTitle(t.project)}
                              current={isCurrent(t)}
                              pinned={false}
                              onSelect={() => go({ page: "conversation", id: t.id })}
                              onPin={canPin ? (button) => togglePin(t.id, true, button) : undefined}
                              onRename={can("runs:write") ? (title) => rename(t, title) : undefined}
                              onArchive={can("runs:write") ? (now) => archive(t, now) : undefined}
                              flash={flashed === t.id ? flash : null}
                            />
                          ))}
                      </ul>
                    </ProjectGroup>
                    {onPinProject && can("projects:read") && (
                      <div className="absolute top-2 right-0 h-10 w-10">
                        <PinButton
                          project
                          projectId={project}
                          pinned={pinnedProjects.has(project)}
                          label={defaultCopy.sidebar.pinProject(
                            projectTitle(project),
                            pinnedProjects.has(project),
                          )}
                          onClick={(button) =>
                            toggleProjectPin(project, !pinnedProjects.has(project), button)
                          }
                        />
                      </div>
                    )}
                    <FlashNote
                      flash={flashed === `project:${project}` ? flash : null}
                      align="right"
                    />
                  </li>
                ))}
                {/* Older conversations arrive a page at a time, when asked for. */}
                {(hasMore || loading === "more") && (
                  <li className="flex justify-center pt-2">
                    <button
                      type="button"
                      onClick={more}
                      disabled={loading === "more"}
                      className="inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-default"
                    >
                      {loading === "more" ? (
                        <span className="aui-shimmer">Loading</span>
                      ) : failed ? (
                        "Could not load. Try again"
                      ) : (
                        "See more"
                      )}
                    </button>
                  </li>
                )}
              </ul>
            </section>
          )}
        </div>

        {!live && open && (
          <p className="px-4 pb-2 text-aui-2xs text-aui-muted">
            Reconnecting. What you see may be out of date.
          </p>
        )}
        <div
          className={`flex shrink-0 items-center gap-1 border-aui-border border-t p-1.5 ${open ? "" : "flex-col"}`}
        >
          <div className={open ? "min-w-0 flex-1" : ""}>
            <Popover
              narrow
              up
              side={open ? undefined : "right"}
              label={defaultCopy.preferences.openFor(name)}
              tooltip={open ? undefined : name}
              className={`flex w-full min-w-0 items-center gap-2 rounded-aui p-1 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover ${
                open ? "" : "size-9 justify-center"
              }`}
              button={
                <>
                  <Avatar name={name} prefs={prefs} size={26} />
                  {open && (
                    <span className="humagen-sidebar-reveal min-w-0 flex-1">
                      <span className="block truncate text-aui-sm text-aui-fg">{name}</span>
                      <span className="block truncate text-aui-2xs text-aui-muted">{subtitle}</span>
                    </span>
                  )}
                </>
              }
            >
              {(close) => (
                <div className="flex flex-col">
                  <div className="flex items-center gap-2 px-2.5 py-2">
                    <Avatar name={name} prefs={prefs} size={32} />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate text-aui-sm text-aui-fg">{name}</span>
                      <span className="block truncate text-aui-2xs text-aui-muted">{subtitle}</span>
                    </span>
                  </div>
                  <Separator className="my-1" />
                  {[
                    {
                      label: "Profile",
                      icon: <UserIcon size={14} />,
                      to: { page: "you", section: "profile" } as Route,
                    },
                    {
                      label: "Settings",
                      icon: <SettingsIcon size={14} />,
                      to: { page: "settings", section: "appearance" } as Route,
                    },
                  ].map((item) => (
                    <button
                      key={item.label}
                      type="button"
                      onClick={() => {
                        close();
                        go(item.to);
                      }}
                      className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      {item.icon}
                      <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    </button>
                  ))}
                  {onTheme && (
                    <Choices
                      title="Theme"
                      value={prefs?.theme ?? "system"}
                      options={[
                        { value: "system", label: "Auto", mark: <MonitorIcon size={14} /> },
                        { value: "light", label: "Light", mark: <SunIcon size={14} /> },
                        { value: "dark", label: "Dark", mark: <MoonIcon size={14} /> },
                      ]}
                      onChange={onTheme}
                    />
                  )}
                  {!dev && (
                    <>
                      <Separator className="my-1" />
                      <button
                        type="button"
                        onClick={() => {
                          close();
                          onSignOut();
                        }}
                        className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                      >
                        <LogOutIcon size={14} />
                        <span className="min-w-0 flex-1 truncate">Sign out</span>
                      </button>
                    </>
                  )}
                </div>
              )}
            </Popover>
          </div>
        </div>
      </aside>
    </>
  );
}

/**
 * On a phone the sidebar is a drawer, opened from one small button in the
 * corner, as Claude's own app does it. Nothing else sits over the page. A dot
 * on it says something in the inbox needs you.
 */
export function DrawerButton({
  open,
  inboxCount,
  onOpen,
}: {
  open: boolean;
  inboxCount: number;
  onOpen: () => void;
}) {
  if (open) return null;
  return (
    <button
      type="button"
      data-testid="drawer-button"
      onClick={onOpen}
      aria-label={inboxCount > 0 ? `Open sidebar, ${inboxCount} need you` : "Open sidebar"}
      className="fixed top-[calc(0.5rem+env(safe-area-inset-top))] left-4 z-30 inline-flex size-9 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent md:hidden"
    >
      <PanelLeftOpenIcon size={20} />
      {inboxCount > 0 && (
        <span
          aria-hidden
          className="absolute top-1.5 right-1.5 size-2 rounded-full bg-aui-accent"
        />
      )}
    </button>
  );
}
