import { hubTabs, workspaceHub } from "./workspace-navigation.ts";
import {
  BotIcon,
  BellIcon,
  CheckIcon,
  BrainIcon,
  ClipboardListIcon,
  DatabaseIcon,
  FileIcon,
  GaugeIcon,
  InboxIcon,
  LayersIcon,
  LogOutIcon,
  NetworkIcon,
  PinIcon,
  PlugIcon,
  ScrollTextIcon,
  SearchIcon,
  ShieldIcon,
  SlidersIcon,
  UsersIcon,
  WrenchIcon,
  ClockIcon,
  PlusIcon,
  ProjectIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SettingsIcon,
  MonitorIcon,
  SunIcon,
  MoonIcon,
  UserIcon,
  Tooltip,
} from "@humagen/render-web";
import { defaultCopy } from "@humagen/components";
import { Separator } from "@humagen/render-web/ui/separator";
import { HumagenMark } from "@humagen/web";
import { type MouseEvent, type ReactNode, useEffect, useRef, useState } from "react";
import type { Me, Preferences, ProjectView, ThreadSummary } from "./api.ts";
import { type Page as RoutePage, type Route, href } from "./route.ts";
import { Avatar } from "./Settings.tsx";
import { type Standing, useRecents } from "./recents.ts";
import { AgentMark, type Flash, FlashNote, Popover, SkeletonRows, useFlash } from "./ui.tsx";

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
      aria-label={count ? badgeLabel ?? `${label}, ${count} need you` : open ? undefined : label}
      className={`relative flex h-9 pointer-coarse:h-11 items-center gap-2.5 [&>svg]:size-[18px] rounded-aui text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:font-medium aria-[current=page]:text-aui-fg ${
        open ? "px-2.5" : "w-9 justify-center"
      }`}
    >
      {icon}
      {open && <span className="flex-1 truncate text-left">{label}</span>}
      {count > 0 &&
        (open ? (
          <span aria-hidden className="rounded-full bg-aui-accent-soft px-1.5 text-aui-2xs font-medium text-aui-fg">
            {count}
          </span>
        ) : (
          <span aria-hidden className="absolute top-1 right-1 size-1.5 rounded-full bg-aui-accent" />
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
function PinButton({ pinned, label, onClick, project = false, thread }: { pinned: boolean; label: string; onClick: (button: HTMLButtonElement) => void; project?: boolean; thread?: string }) {
  return <button
    type="button"
    onClick={event => onClick(event.currentTarget)}
    data-pin-thread={thread}
    aria-label={label}
    aria-pressed={pinned}
    title={label}
    className={`absolute top-1 right-1 inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent [@media(pointer:coarse)]:top-0.5 [@media(pointer:coarse)]:size-11 [@media(pointer:coarse)]:opacity-100 ${project ? "group-hover/project:opacity-100 group-focus-within/project:opacity-100" : "group-hover/task:opacity-100 group-focus-within/task:opacity-100"} ${pinned ? "opacity-100" : "opacity-0"}`}
  ><PinIcon size={14} fill={pinned ? "currentColor" : "none"} /></button>;
}

/** Agent identity and the people taking part stay separate at sidebar widths. */
function Participants({ task, me, prefs }: { task: ThreadSummary; me: Me; prefs: Preferences | null }) {
  const words = defaultCopy.sidebar;
  const agents = [...new Set(task.agents)];
  const people = [...new Set(task.people)];
  const isYou = (person: string) => person === me.principal.subject || person === me.handle;
  const personName = (person: string) => isYou(person) ? prefs?.display_name || me.principal.name || person : person;
  if (agents.length === 0 && people.length === 0) return null;
  return <span className="flex min-w-0 items-center gap-2.5 text-aui-2xs text-aui-muted">
    {agents.length > 0 && <span role="group" aria-label={words.agents(agents)} title={words.agents(agents)} className="flex min-w-0 flex-1 items-center gap-1.5">
      <BotIcon size={13} className="shrink-0" />
      <span aria-hidden className="flex shrink-0 items-center gap-0.5">{agents.slice(0, 2).map(agent => <AgentMark key={agent} name={agent} size={13} />)}</span>
      <span className="truncate">{agents[0]}</span>
      {agents.length > 1 && <span className="shrink-0">{words.moreParticipants(agents.length - 1)}</span>}
    </span>}
    {people.length > 0 && <span role="group" aria-label={words.people(people.map(personName))} title={words.people(people.map(personName))} className="flex max-w-[55%] shrink-0 items-center gap-1.5">
      <span aria-hidden className="flex shrink-0 -space-x-1">{people.slice(0, 3).map(person => <span key={person} className="rounded-full ring-2 ring-[var(--humagen-plane)]"><Avatar name={personName(person)} prefs={isYou(person) ? prefs : null} size={18} /></span>)}</span>
      {people.length === 1 ? <span className="truncate">{isYou(people[0]!) ? words.you : personName(people[0]!)}</span> : <>
        <span className="sr-only">{words.peopleCount(people.length)}</span>
        {people.length > 3 && <span>{words.moreParticipants(people.length - 3)}</span>}
      </>}
    </span>}
  </span>;
}

/** One conversation with its participants and an independent pin control. */
function TaskRow({
  task,
  projectTitle,
  current,
  pinned,
  onSelect,
  onPin,
  flash = null,
  me,
  prefs,
}: {
  task: ThreadSummary;
  projectTitle: string;
  current: boolean;
  pinned: boolean;
  onSelect: () => void;
  onPin?: (button: HTMLButtonElement) => void;
  flash?: Flash | null;
  me: Me;
  prefs: Preferences | null;
}) {
  const title = task.title || "Untitled conversation";
  const state = task.active > 0 ? "In progress" : task.failed > 0 ? "Needs attention" : "Finished";
  return (
    // A row that is saying something sits over its neighbours and over the
    // section's label, which sticks above the first row, so the note shows.
    <li className={`group/task relative humagen-enter ${flash ? "z-20" : ""}`}>
      <button
        type="button"
        onClick={onSelect}
        title={`${title}\n${state}\n${projectTitle}`}
        aria-current={current ? "page" : undefined}
        className="flex min-h-12 w-full items-center gap-2 rounded-aui py-2 pr-10 pl-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:text-aui-fg [@media(pointer:coarse)]:pr-14"
      >
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate">{title}</span>
          <Participants task={task} me={me} prefs={prefs} />
        </span>
        {task.active > 0 && <span className="sr-only">, running</span>}
        {task.active === 0 && task.failed > 0 && <span className="sr-only">, a reply failed</span>}
      </button>
      {onPin && <PinButton pinned={pinned} thread={task.id} label={pinned ? `Unpin ${title}` : `Pin ${title}`} onClick={onPin} />}
      {onPin && <FlashNote flash={flash} align="right" />}
    </li>
  );
}

/** A small section label, as Claude's sidebar draws Pinned and Recents. */
function Label({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="sticky top-0 z-10 flex h-8 items-center justify-between gap-1 bg-[var(--humagen-plane)] pr-2 pl-2">
      <p className="px-2 text-aui-xs text-aui-muted">{children}</p>
      {action}
    </div>
  );
}

/**
 * The console's navigation, laid out like Claude's: one flat list of pages,
 * then pinned tasks, then recent ones. Closed, it is a rail of icons. The open state is the caller's to persist.
 */
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
  onSearch?:()=>void;
  notificationCount?: number;
}) {
  const projectTitle = (project?:string) => me.personal_project&&project===me.personal_project ? defaultCopy.projects.personalTitle : !project||project==="~" ? workspaceName||defaultCopy.projects.companyName : projects.find(item=>item.name===project)?.title||project;
  const who = me.principal;
  const dev = who.subject === "dev";
  const name = prefs?.display_name || who.name || who.email || who.subject;
  const subtitle = dev ? "Dev mode, no sign-in" : (who.roles ?? []).join(", ") || who.tenant;
  // Recents is what people asked for. Scheduled and eval runs live on their own
  // pages, so a timer cannot push a person's work off the list.
  const [standing, setStanding] = useState<Standing>("");
  const [agent, setAgent] = useState("");
  const answering = [...new Set(threads.filter((t) => t.origin === "api").flatMap((t) => t.agents))].sort();
  // The list pages itself. The caller's threads change when a run starts or
  // ends, which is the signal to fetch the first page again.
  const { threads: recents, hasMore, loading, failed, stalled, retry, more } = useRecents(agent, standing, threads);
  const filters = (agent ? 1 : 0) + (standing ? 1 : 0);
  // A pin that did not take is said beside the conversation it was for, in whichever
  // list the conversation is in by then.
  const { flash, say, clear: unsay } = useFlash();
  const [flashed, setFlashed] = useState<string | null>(null);
  const sidebar = useRef<HTMLElement>(null);
  const pinFocus = useRef<{ id: string; pinned: boolean } | null>(null);
  useEffect(() => {
    const target = pinFocus.current;
    if (!target || pinned.includes(target.id) !== target.pinned) return;
    const control = Array.from(sidebar.current?.querySelectorAll<HTMLButtonElement>("[data-pin-thread]") ?? [])
      .find((button) => button.dataset.pinThread === target.id);
    // An older unselected row may leave the loaded page after unpinning.
    // Keep keyboard focus at the conversation list's stable filter control.
    const fallback = sidebar.current?.querySelector<HTMLButtonElement>('section[aria-label="Conversations"] button');
    (control ?? fallback)?.focus();
    pinFocus.current = null;
  }, [pinned]);
  const togglePin = (id: string, on: boolean, button: HTMLButtonElement) => {
    unsay();
    if (document.activeElement === button) pinFocus.current = { id, pinned: on };
    onPin(id, on).catch(() => {
      if (pinFocus.current?.id === id) pinFocus.current = null;
      setFlashed(id);
      say(on ? defaultCopy.pins.pinFailed : defaultCopy.pins.unpinFailed, true);
    });
  };
  const toggleProjectPin = (project: string, on: boolean) => {
    unsay();
    onPinProject?.(project, on).catch(() => {
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
  const nav=defaultCopy.navigation;
  const hub = workspaceHub(route);
  const notificationRoute: Route = { page: "notifications" };
  const libraryTabs = hubTabs("library", route, can);
  const agentTabs = hubTabs("agents", route, can);
  const operationTabs = hubTabs("operations", route, can);
  const entries: {to:Route;label:string;icon:ReactNode;show:boolean;active:boolean;badge?:number;group:"projects"|"library"|"operations"}[]=[
    {to:{page:"projects"},label:nav.projects,icon:<ProjectIcon/>,show:can("projects:read"),active:here("projects","project","channel"),group:"projects"},
    {to:{page:"directory",tab:"people"},label:nav.people,icon:<UsersIcon/>,show:can("people:read"),active:here("directory"),group:"projects"},
    {to:libraryTabs[0]?.route ?? {page:"library",section:"sources"},label:nav.library,icon:<LayersIcon/>,show:libraryTabs.length > 0,active:hub === "library",group:"library"},
    {to:agentTabs[0]?.route ?? {page:"agents"},label:nav.agents,icon:<BotIcon/>,show:agentTabs.length > 0,active:hub === "agents",group:"operations"},
    {to:operationTabs[0]?.route ?? {page:"operations",section:"context"},label:nav.operations,icon:<SlidersIcon/>,show:operationTabs.length > 0,active:hub === "operations",group:"operations"},
  ];

  // Pinned conversations come from whatever is loaded. One that cannot be found
  // (deleted, or no longer readable) is not shown.
  const known = new Map([...threads, ...recents].map((t) => [t.id, t]));
  const pinnedRows = pinned.map((id) => known.get(id)).filter((t): t is ThreadSummary => t !== undefined);
  const pinnedSet = new Set(pinned);
  const unpinned = recents.filter((t) => !pinnedSet.has(t.id));
  const currentThread = selected ? known.get(selected) : undefined;
  if (currentThread && !pinnedSet.has(currentThread.id) && !unpinned.some((thread) => thread.id === currentThread.id)) {
    unpinned.push(currentThread);
  }
  const noConversations = recents.length === 0 && unpinned.length === 0;
  const isCurrent = (t: ThreadSummary) => t.id === selected;
  const canPin = can("runs:read");
  const pinnedProjects = new Set(prefs?.pinned_projects ?? []);
  const readableProjectPins = [...pinnedProjects].filter(project => can("projects:read") && (project === "~" || projects.some(item => item.name === project)));
  const projectGroups = [...new Set([...recents, ...unpinned].map(thread => thread.project || "~"))];

  const clear = () => {
    setStanding("");
    setAgent("");
  };

  return (
    <>
    {open && <button type="button" tabIndex={-1} aria-label="Dismiss navigation" onClick={onToggle} className="fixed inset-0 z-40 bg-black/40 md:hidden" />}
    <aside
      ref={sidebar}
      aria-label="Navigation"
      className={`humagen-glass-bar flex h-full min-h-0 shrink-0 flex-col border-[var(--humagen-rule)] border-r transition-[width] duration-200 motion-reduce:transition-none ${
        open ? "w-64 max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-50 max-md:w-[min(20rem,85vw)]" : "w-12 max-md:hidden"
      }`}
    >
      <div className={`flex h-12 shrink-0 items-center ${open ? "justify-between pr-2 pl-3.5" : "justify-center"}`}>
        {open && <span className="humagen-display flex min-w-0 items-center gap-2 truncate text-aui-base font-semibold text-aui-fg"><HumagenMark size={23} />humagen</span>}
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
      <nav aria-label="Sections" className={`flex shrink-0 flex-col gap-1 ${open ? "px-2" : "items-center"}`}>
        {can("runs:write") && <Item icon={<PlusIcon />} label={nav.newConversation} href={href({page:"new"})} active={here("new")} open={open} onClick={()=>go({page:"new"})} />}
        {onSearch&&<button type="button" onClick={onSearch} aria-label={nav.search} className={`flex h-9 pointer-coarse:h-11 items-center gap-2.5 rounded-aui text-aui-sm text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent ${open?"px-2.5":"w-9 justify-center"}`}><SearchIcon size={18}/>{open&&<><span className="flex-1 text-left">{nav.search}</span><kbd className="text-aui-2xs">⌘ K</kbd></>}</button>}

        {(can("runs:read") || can("alerts:read")) && <Item icon={<BellIcon />} label={nav.notifications} href={href(can("runs:read") ? notificationRoute : {page:"monitor",tab:"rules"})} open={open} active={hub === "notifications"} badge={inboxCount} onClick={()=>go(can("runs:read") ? notificationRoute : {page:"monitor",tab:"rules"})} />}
        {(["projects","library","operations"] as const).map(group=>{
          const shown=entries.filter(entry=>entry.group===group&&entry.show);
          return shown.length>0&&<section key={group} aria-label={nav[group]} className={group === "library" ? "" : "mt-2"}>{group !== "library" && <Separator className="mb-3" />}<ul className={`flex flex-col gap-0.5 ${open?"":"items-center"}`}>{shown.map(entry=><li key={entry.label}><Item icon={entry.icon} label={entry.label} href={href(entry.to)} open={open} active={entry.active} badge={entry.badge} onClick={()=>go(entry.to)}/></li>)}</ul></section>;
        })}

      </nav>

      {open && (pinnedRows.length > 0 || readableProjectPins.length > 0) && (
        <section aria-label="Pinned" className="flex shrink-0 flex-col pt-4">
          <Label>{defaultCopy.sidebar.pinned}</Label>
          <ul className="px-2">
            {readableProjectPins.map(project => <li key={`project:${project}`} className={`group/project relative ${flashed === `project:${project}` && flash ? "z-20" : ""}`}>
              <div className="py-1 pr-10 [@media(pointer:coarse)]:pr-12"><Item icon={<ProjectIcon />} label={projectTitle(project)} href={href({page:"project",name:project === "~" ? "" : project,tab:"inbox"})} active={route.page === "project" && route.name === (project === "~" ? "" : project)} open onClick={() => go({page:"project",name:project === "~" ? "" : project,tab:"inbox"})} /></div>
              {onPinProject && <PinButton pinned project label={defaultCopy.sidebar.pinProject(projectTitle(project), true)} onClick={() => toggleProjectPin(project, false)} />}
              <FlashNote flash={flashed === `project:${project}` ? flash : null} align="right" />
            </li>)}
            {pinnedRows.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                me={me}
                prefs={prefs}
                projectTitle={projectTitle(t.project)}
                current={isCurrent(t)}
                pinned
                onSelect={() => go({ page: "conversation", id: t.id })}
                onPin={(button) => togglePin(t.id, false, button)}
                flash={flashed === t.id ? flash : null}
              />
            ))}
          </ul>
        </section>
      )}

      {open && can("runs:read") && (
        <section aria-label="Conversations" className="flex shrink-0 flex-col pt-4">
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
                    {filters > 0 && <span aria-hidden className="absolute top-0.5 right-0.5 size-1.5 rounded-full bg-aui-accent" />}
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
                          ...answering.map((a) => ({ value: a, label: a, mark: <AgentMark name={a} size={12} /> })),
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
                {filters > 0 ? "No conversation matches these filters." : "Your conversations appear here. Start with New."}
              </li>
            )}
            {projectGroups.map(project => <li key={project} className={`group/project relative min-w-0 pt-2 ${flashed === `project:${project}` && flash ? "z-20" : ""}`}>
              <details open><summary className="flex min-h-10 cursor-pointer items-center gap-1.5 rounded-aui pr-12 pl-2.5 text-aui-xs font-medium text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"><ProjectIcon size={14} /><span className="min-w-0 flex-1 truncate">{projectTitle(project)}</span></summary><ul className="ml-2 border-aui-border border-l pl-1">{unpinned.filter(thread => (thread.project || "~") === project).map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                me={me}
                prefs={prefs}
                projectTitle={projectTitle(t.project)}
                current={isCurrent(t)}
                pinned={false}
                onSelect={() => go({ page: "conversation", id: t.id })}
                onPin={canPin ? (button) => togglePin(t.id, true, button) : undefined}
                flash={flashed === t.id ? flash : null}
              />
            ))}</ul></details>
              {onPinProject && can("projects:read") && <div className="absolute top-2 right-0 h-10 w-10"><PinButton project pinned={pinnedProjects.has(project)} label={defaultCopy.sidebar.pinProject(projectTitle(project), pinnedProjects.has(project))} onClick={() => toggleProjectPin(project, !pinnedProjects.has(project))} /></div>}
              <FlashNote flash={flashed === `project:${project}` ? flash : null} align="right" />
            </li>)}
            {/* Older conversations arrive a page at a time, when asked for. */}
            {(hasMore || loading === "more") && (
              <li className="flex justify-center pt-2">
                <button
                  type="button"
                  onClick={more}
                  disabled={loading === "more"}
                  className="inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-default"
                >
                  {loading === "more" ? <span className="aui-shimmer">Loading</span> : failed ? "Could not load. Try again" : "See more"}
                </button>
              </li>
            )}
          </ul>
        </section>
      )}
      </div>

      {!live && open && <p className="px-4 pb-2 text-aui-2xs text-aui-muted">Reconnecting. What you see may be out of date.</p>}
      <div className={`flex shrink-0 items-center gap-1 border-aui-border border-t p-1.5 ${open ? "" : "flex-col"}`}>
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
                  <span className="min-w-0 flex-1">
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
                  { label: "Profile", icon: <UserIcon size={14} />, to: { page: "you", section: "profile" } as Route },
                  { label: "Settings", icon: <SettingsIcon size={14} />, to: { page: "settings", section: "appearance" } as Route },
                ].map((item) => (
                  <button
                    key={item.label}
                    type="button"
                    onClick={() => { close(); go(item.to); }}
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
                      onClick={() => { close(); onSignOut(); }}
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
export function DrawerButton({ open, inboxCount, onOpen }: { open: boolean; inboxCount: number; onOpen: () => void }) {
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
      {inboxCount > 0 && <span aria-hidden className="absolute top-1.5 right-1.5 size-2 rounded-full bg-aui-accent" />}
    </button>
  );
}
