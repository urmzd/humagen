import {
  BotIcon,
  CheckIcon,
  ClockIcon,
  ComposeIcon,
  FolderIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  SettingsIcon,
  Tooltip,
} from "@humagen/render-web";
import { defaultCopy } from "@humagen/components";
import { HumagenMark } from "@humagen/web";
import { type MouseEvent, type ReactNode, type SVGProps, useState } from "react";
import type { Me, Preferences, ProjectView, ThreadSummary } from "./api.ts";
import { type Page as RoutePage, type Route, href } from "./route.ts";
import { Avatar } from "./Settings.tsx";
import { type Standing, useRecents } from "./recents.ts";
import { AgentMark, type Flash, FlashNote, Popover, SkeletonRows, useFlash } from "./ui.tsx";

// Glyphs outside the shared icon set. Same grid and stroke.
function glyph(d: ReactNode) {
  return function Glyph({ size = 18, ...rest }: SVGProps<SVGSVGElement> & { size?: number }) {
    return (
      <svg
        aria-hidden="true"
        focusable="false"
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        className="shrink-0"
        {...rest}
      >
        {d}
      </svg>
    );
  };
}
const InboxIcon = glyph(
  <>
    <path d="M22 12h-6l-2 3h-4l-2-3H2" />
    <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
  </>,
);
/** Context: layers of what agents are grounded in. */
const LayersIcon = glyph(
  <>
    <path d="m12 2 10 5-10 5L2 7z" />
    <path d="m2 17 10 5 10-5M2 12l10 5 10-5" />
  </>,
);
/** Directory: the people and agents in the workspace. */
const PeopleIcon = glyph(
  <>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2" />
    <circle cx="9" cy="7" r="4" />
    <path d="M22 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
  </>,
);
/** Monitor: a pulse, for how runs are going. */
const PulseIcon = glyph(<path d="M22 12h-4l-3 9L9 3l-3 9H2" />);
const BoxIcon = glyph(
  <>
    <path d="M21 8v13H3V8M1 3h22v5H1z" />
    <path d="M10 12h4" />
  </>,
);
const FilterIcon = glyph(<path d="M3 5h18M7 12h10M10 19h4" />);
const PinIcon = glyph(
  <>
    <path d="M12 17v5" />
    <path d="M9 10.76V6h6v4.76a2 2 0 0 0 1.11 1.79l1.78.9A2 2 0 0 1 19 15.24V17H5v-1.76a2 2 0 0 1 1.11-1.79l1.78-.9A2 2 0 0 0 9 10.76zM8 2h8" />
  </>,
);
const LogOutIcon = glyph(
  <>
    <path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4" />
    <path d="m16 17 5-5-5-5M21 12H9" />
  </>,
);

/** A link to a view. A plain click opens it in place; a modified one, in a new tab. */
function Item({
  icon,
  label,
  href,
  active,
  open,
  badge,
  onClick,
}: {
  icon: ReactNode;
  label: string;
  href: string;
  active: boolean;
  open: boolean;
  badge?: number;
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
      aria-label={count ? `${label}, ${count} need you` : open ? undefined : label}
      className={`relative flex h-9 items-center gap-2.5 [&>svg]:size-[18px] rounded-aui text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:font-medium aria-[current=page]:text-aui-fg ${
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

/**
 * One conversation in the Pinned or Conversations list. The pin sits beside it, shown
 * on hover or focus, and a pin that did not take says so above it.
 */
function TaskRow({
  task,
  current,
  pinned,
  onSelect,
  onPin,
  flash = null,
}: {
  task: ThreadSummary;
  current: boolean;
  pinned: boolean;
  onSelect: () => void;
  onPin?: () => void;
  flash?: Flash | null;
}) {
  const title = task.title || "Untitled conversation";
  const state = task.active > 0 ? "In progress" : task.failed > 0 ? "Needs attention" : "Finished";
  return (
    // A row that is saying something sits over its neighbours and over the
    // section's label, which sticks above the first row, so the note shows.
    <li className={`group relative humagen-enter ${flash ? "z-20" : ""}`}>
      <button
        type="button"
        onClick={onSelect}
        title={`${title}\n${state}${task.project ? ` · ${task.project}` : ""}`}
        aria-current={current ? "page" : undefined}
        className="flex min-h-10 w-full items-center gap-2 rounded-aui py-1.5 pr-8 pl-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:text-aui-fg"
      >
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="truncate">{title}</span>
          {task.agents.length > 0 && (
            <span className="flex min-w-0 items-center gap-1 text-aui-2xs text-aui-muted" aria-label={`Agents that took part: ${task.agents.join(", ")}`}>
              {[...new Set(task.agents)].slice(0, 4).map((agent) => <AgentMark key={agent} name={agent} size={9} />)}
              <span className="truncate">{[...new Set(task.agents)].join(", ")}</span>
            </span>
          )}
        </span>
        {task.active > 0 && <span className="sr-only">, running</span>}
        {task.active === 0 && task.failed > 0 && <span className="sr-only">, a reply failed</span>}
      </button>
      {onPin && (
        <button
          type="button"
          onClick={onPin}
          aria-label={pinned ? `Unpin ${title}` : `Pin ${title}`}
          title={pinned ? "Unpin" : "Pin"}
          className="absolute top-1 right-1 inline-flex size-6 items-center justify-center rounded-aui text-aui-muted opacity-0 transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:outline-aui-accent group-hover:opacity-100 max-md:opacity-100"
        >
          <PinIcon size={14} fill={pinned ? "currentColor" : "none"} />
        </button>
      )}
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
  inboxCount,
  live,
  onSignOut,
  onSearch,
  notifications,
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
  inboxCount: number;
  /** Updates are arriving. When false, the sidebar says so. */
  live: boolean;
  onSignOut: () => void;
  onSearch?:()=>void;
  notifications?:ReactNode;
}) {
  const who = me.principal;
  const dev = who.subject === "dev";
  const name = prefs?.display_name || who.name || who.email || who.subject;
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
  const togglePin = (id: string, on: boolean) => {
    unsay();
    onPin(id, on).catch(() => {
      setFlashed(id);
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
  const entries: {to:Route;label:string;icon:ReactNode;show:boolean;active:boolean;badge?:number;group:"projects"|"library"|"operations"}[]=[
    {to:{page:"new"},label:nav.newConversation,icon:<ComposeIcon/>,show:can("runs:write"),active:here("new"),group:"projects"},
    {to:{page:"projects"},label:nav.projects,icon:<FolderIcon/>,show:can("projects:read"),active:here("projects","project","channel"),group:"projects"},
    {to:{page:"directory",tab:"people"},label:nav.people,icon:<PeopleIcon/>,show:can("people:read"),active:here("directory"),group:"projects"},
    {to:{page:"library",section:"sources"},label:nav.sources,icon:<LayersIcon/>,show:can("knowledge:read"),active:here("context")||(route.page==="library"&&route.section==="sources"),group:"library"},
    {to:{page:"library",section:"skills"},label:nav.skills,icon:<LayersIcon/>,show:can("projects:read"),active:route.page==="library"&&route.section==="skills",group:"library"},
    {to:{page:"library",section:"memory"},label:nav.memory,icon:<LayersIcon/>,show:can("projects:read"),active:route.page==="library"&&route.section==="memory",group:"library"},
    {to:{page:"artifacts"},label:defaultCopy.conversation.deliverables,icon:<BoxIcon/>,show:can("artifacts:read"),active:here("artifacts"),group:"library"},
    {to:{page:"inbox"},label:nav.needsYou,icon:<InboxIcon/>,show:can("runs:read"),active:here("inbox"),badge:inboxCount,group:"operations"},
    {to:{page:"agents"},label:nav.agents,icon:<BotIcon/>,show:can("agents:read"),active:here("agents","agent","agent-setup"),group:"operations"},
    {to:{page:"agent-network"},label:nav.network,icon:<PeopleIcon/>,show:can("runs:read"),active:here("agent-network"),group:"operations"},
    {to:{page:"assignments"},label:nav.assignments,icon:<CheckIcon/>,show:can("runs:read"),active:here("assignments"),group:"operations"},
    {to:{page:"operations",section:"context"},label:nav.context,icon:<LayersIcon/>,show:can("knowledge:read"),active:route.page==="operations"&&route.section==="context",group:"operations"},
    {to:{page:"operations",section:"integrations"},label:nav.integrations,icon:<BoxIcon/>,show:can("clients:read"),active:route.page==="operations"&&route.section==="integrations",group:"operations"},
    {to:{page:"action-policy"},label:nav.policies,icon:<CheckIcon/>,show:can("agents:read"),active:here("action-policy"),group:"operations"},
    {to:{page:"agent-control"},label:nav.controls,icon:<BotIcon/>,show:can("agents:read"),active:here("agent-control"),group:"operations"},
    {to:{page:"schedules"},label:nav.schedules,icon:<ClockIcon/>,show:can("schedules:read"),active:here("schedules"),group:"operations"},
    {to:{page:"usage"},label:nav.usage,icon:<PulseIcon/>,show:can("runs:read"),active:here("usage","monitor"),group:"operations"},
    {to:{page:"audit"},label:nav.audit,icon:<CheckIcon/>,show:can("projects:govern"),active:here("audit"),group:"operations"},
  ];

  // Pinned conversations come from whatever is loaded. One that cannot be found
  // (deleted, or no longer readable) is not shown.
  const known = new Map([...threads, ...recents].map((t) => [t.id, t]));
  const pinnedRows = pinned.map((id) => known.get(id)).filter((t): t is ThreadSummary => t !== undefined);
  const pinnedSet = new Set(pinned);
  const unpinned = recents.filter((t) => !pinnedSet.has(t.id));
  const isCurrent = (t: ThreadSummary) => t.id === selected;
  const canPin = can("runs:read");

  const clear = () => {
    setStanding("");
    setAgent("");
  };

  return (
    <>
    {open && <button type="button" tabIndex={-1} aria-label="Dismiss navigation" onClick={onToggle} className="fixed inset-0 z-40 bg-black/40 md:hidden" />}
    <aside
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
      <nav aria-label="Sections" className={`flex shrink-0 flex-col ${open ? "px-2" : "items-center"}`}>
        {onSearch&&<button type="button" onClick={onSearch} aria-label={nav.search} className={`flex min-h-10 items-center gap-2 rounded-aui text-aui-sm text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent ${open?"px-2.5":"w-9 justify-center"}`}><FilterIcon size={18}/>{open&&<><span className="flex-1 text-left">{nav.search}</span><kbd className="text-aui-2xs">⌘ K</kbd></>}</button>}
        {(["projects","library","operations"] as const).map(group=>{
          const shown=entries.filter(entry=>entry.group===group&&entry.show);
          return shown.length>0&&<section key={group} aria-label={nav[group]} className="pt-2">{open&&<Label>{nav[group]}</Label>}<ul className={`flex flex-col gap-0.5 ${open?"":"items-center"}`}>{shown.map(entry=><li key={entry.label}><Item icon={entry.icon} label={entry.label} href={href(entry.to)} open={open} active={entry.active} badge={entry.badge} onClick={()=>go(entry.to)}/></li>)}</ul></section>;
        })}
        {open&&notifications&&<div className="min-w-0 pt-3">{notifications}</div>}
      </nav>

      {open && pinnedRows.length > 0 && (
        <section aria-label="Pinned" className="flex shrink-0 flex-col pt-4">
          <Label>Pinned</Label>
          <ul className="px-2">
            {pinnedRows.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                current={isCurrent(t)}
                pinned
                onSelect={() => go({ page: "conversation", id: t.id })}
                onPin={() => togglePin(t.id, false)}
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
                    <FilterIcon size={14} />
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
            {recents.length === 0 && loading === "first" && (
              <li className="px-2.5">
                <SkeletonRows count={5} height="1.25rem" />
              </li>
            )}
            {recents.length === 0 && loading !== "first" && stalled && (
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
            {recents.length === 0 && loading !== "first" && !stalled && (
              <li className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                {filters > 0 ? "No conversation matches these filters." : "Your conversations appear here. Start with New."}
              </li>
            )}
            {[...new Set(unpinned.map((thread) => thread.project ?? ""))].map((project) => <li key={project} className="min-w-0 pt-2"><details open><summary className="flex min-h-7 cursor-pointer items-center gap-1.5 px-2.5 text-aui-xs font-medium text-aui-muted"><FolderIcon size={13} /><span className="min-w-0 flex-1 truncate">{projects.find((item) => item.name === project)?.title || project || workspaceName || defaultCopy.projects.workspace}</span></summary><ul className="ml-2 border-aui-border border-l pl-1">{unpinned.filter((thread) => (thread.project ?? "") === project).map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                current={isCurrent(t)}
                pinned={false}
                onSelect={() => go({ page: "conversation", id: t.id })}
                onPin={canPin ? () => togglePin(t.id, true) : undefined}
                flash={flashed === t.id ? flash : null}
              />
            ))}</ul></details></li>)}
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
        <button
          type="button"
          onClick={() => go({ page: "you", section: "profile" })}
          aria-current={route.page === "you" ? "page" : undefined}
          aria-label={nav.account(name)}
          title={open ? undefined : nav.you}
          className={`flex min-w-0 items-center gap-2 rounded-aui p-1 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover ${
            open ? "flex-1" : "size-9 justify-center"
          }`}
        >
          {open ? <Avatar name={name} prefs={prefs} size={26} /> : <SettingsIcon size={18} />}
          {open && (
            <span className="min-w-0 flex-1">
              <span className="block truncate text-aui-sm text-aui-fg">{name}</span>
              <span className="block truncate text-aui-2xs text-aui-muted">
                {dev ? "Dev mode, no sign-in" : (who.roles ?? []).join(", ") || who.tenant}
              </span>
            </span>
          )}
        </button>
        {!dev && (
          <button
            type="button"
            onClick={onSignOut}
            aria-label="Sign out"
            title="Sign out"
            className="inline-flex size-8 shrink-0 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <LogOutIcon />
          </button>
        )}
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
