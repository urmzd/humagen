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
} from "@warp/render-web";
import { type MouseEvent, type ReactNode, type SVGProps, useState } from "react";
import type { Me, Preferences, ThreadSummary } from "./api.ts";
import { type Page as RoutePage, type Route, href } from "./route.ts";
import { Avatar } from "./Settings.tsx";
import { type Standing, useRecents } from "./recents.ts";
import { AgentMark, Popover, SkeletonRows } from "./ui.tsx";

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
/** Monitor: a pulse, for how runs are going. */
const PulseIcon = glyph(<path d="M22 12h-4l-3 9L9 3l-3 9H2" />);
const MenuIcon = glyph(<path d="M4 6h16M4 12h16M4 18h16" />);
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

/** One conversation in the Pinned or Tasks list. The pin sits beside it, shown on hover or focus. */
function TaskRow({
  task,
  current,
  pinned,
  onSelect,
  onPin,
}: {
  task: ThreadSummary;
  current: boolean;
  pinned: boolean;
  onSelect: () => void;
  onPin?: () => void;
}) {
  const title = task.title || "Untitled task";
  const state = task.active > 0 ? "In progress" : task.failed > 0 ? "Needs attention" : "Finished";
  return (
    <li className="group relative warp-enter">
      <button
        type="button"
        onClick={onSelect}
        title={`${title}\n${state}${task.project ? ` · ${task.project}` : ""}`}
        aria-current={current ? "page" : undefined}
        className="flex h-8 w-full items-center gap-2 rounded-aui pr-8 pl-2.5 text-left text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=page]:bg-aui-hover aria-[current=page]:text-aui-fg"
      >
        {/* The orb of the agent that last answered: moving while it works, red when a reply failed. */}
        <AgentMark
          name={task.agents[task.agents.length - 1] ?? "?"}
          size={10}
          state={task.active > 0 ? "thinking" : task.failed > 0 ? "error" : "idle"}
        />
        <span className="min-w-0 flex-1 truncate">{title}</span>
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
    </li>
  );
}

/** A small section label, as Claude's sidebar draws Pinned and Recents. */
function Label({ children, action }: { children: ReactNode; action?: ReactNode }) {
  return (
    <div className="sticky top-0 z-10 flex h-8 items-center justify-between gap-1 bg-[var(--warp-plane)] pr-2 pl-2">
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
  pinned,
  onPin,
  inboxCount,
  live,
  onSignOut,
}: {
  open: boolean;
  onToggle: () => void;
  route: Route;
  go: (next: Route) => void;
  me: Me;
  prefs: Preferences | null;
  can: (permission: string) => boolean;
  threads: ThreadSummary[];
  /** Pinned task ids, most recently pinned first. */
  pinned: string[];
  onPin: (id: string, pin: boolean) => void;
  inboxCount: number;
  /** Updates are arriving. When false, the sidebar says so. */
  live: boolean;
  onSignOut: () => void;
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

  // Every page is hidden from someone who may not read it, so nothing in the
  // menu answers with a refusal. Work comes first, then what watches it, then
  // what it is built from. A task keeps its own runs, evals live on what they
  // test, and trends live with alerts, so none of those needs a page here.
  const selected = route.page === "task" ? route.id : null;
  const here = (...pages: RoutePage[]) => pages.includes(route.page);
  const entries: { to: Route; label: string; icon: ReactNode; show: boolean; active: boolean; badge?: number }[] = [
    { to: { page: "new" }, label: "New", icon: <ComposeIcon />, show: can("runs:write"), active: here("new") },
    // What fired, what waits for a person and what agents published all arrive here, so it comes first.
    { to: { page: "inbox" }, label: "Inbox", icon: <InboxIcon />, show: can("runs:read"), active: here("inbox"), badge: inboxCount },
    { to: { page: "projects" }, label: "Projects", icon: <FolderIcon />, show: can("runs:read"), active: here("projects", "project") },
    { to: { page: "schedules" }, label: "Schedules", icon: <ClockIcon />, show: can("schedules:read"), active: here("schedules") },
    {
      to: { page: "monitor", tab: "overview" },
      label: "Monitor",
      icon: <PulseIcon />,
      show: can("alerts:read") || can("runs:read"),
      active: here("monitor"),
    },
    { to: { page: "context" }, label: "Context", icon: <LayersIcon />, show: can("knowledge:read"), active: here("context") },
    { to: { page: "agents" }, label: "Agents", icon: <BotIcon />, show: can("agents:read"), active: here("agents", "agent") },
    { to: { page: "artifacts" }, label: "Artifacts", icon: <BoxIcon />, show: can("artifacts:read"), active: here("artifacts") },
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
      className={`warp-glass-bar flex shrink-0 flex-col border-[var(--warp-rule)] border-r transition-[width] duration-200 motion-reduce:transition-none ${
        open ? "w-64 max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-50 max-md:w-[min(20rem,85vw)]" : "w-12 max-md:hidden"
      }`}
    >
      <div className={`flex h-12 items-center ${open ? "justify-between pr-2 pl-3.5" : "justify-center"}`}>
        {open && <span className="warp-display truncate text-aui-base font-semibold text-aui-fg">Warp</span>}
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
      <div className="aui-scroll flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto">
      <nav aria-label="Sections" className={`flex shrink-0 flex-col ${open ? "px-2" : "items-center"}`}>
        <ul className={`flex flex-col gap-0.5 ${open ? "" : "items-center"}`}>
          {entries
            .filter((e) => e.show)
            .map((e) => (
              <li key={e.label}>
                <Item icon={e.icon} label={e.label} href={href(e.to)} open={open} active={e.active} badge={e.badge} onClick={() => go(e.to)} />
              </li>
            ))}
        </ul>
      </nav>

      {open && pinnedRows.length > 0 && (
        <section aria-label="Pinned" className="flex shrink-0 flex-col pt-4">
          <Label>Pinned</Label>
          <ul className="px-2">
            {pinnedRows.map((t) => (
              <TaskRow key={t.id} task={t} current={isCurrent(t)} pinned onSelect={() => go({ page: "task", id: t.id })} onPin={() => onPin(t.id, false)} />
            ))}
          </ul>
        </section>
      )}

      {open && can("runs:read") && (
        <section aria-label="Tasks" className="flex shrink-0 flex-col pt-4">
          {/* The label stays in reach while the list scrolls under it. */}
          <Label
            action={
              <Popover
                narrow
                align="right"
                label={filters > 0 ? `Filter tasks, ${filters} on` : "Filter tasks"}
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
                        { value: "", label: "Every task" },
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
                        className="mt-1 rounded-aui border-[var(--warp-rule)] border-t px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                      >
                        Clear filters
                      </button>
                    )}
                  </div>
                )}
              </Popover>
            }
          >
            Tasks
          </Label>
          <ul className="px-2 pb-3" aria-label="Tasks" aria-busy={loading !== null}>
            {recents.length === 0 && loading === "first" && (
              <li className="px-2.5">
                <SkeletonRows count={5} height="1.25rem" />
              </li>
            )}
            {recents.length === 0 && loading !== "first" && stalled && (
              <li className="flex flex-col items-start gap-1.5 px-2.5 py-1.5 text-aui-xs text-aui-muted">
                Your tasks could not be loaded.
                <button
                  type="button"
                  onClick={retry}
                  className="warp-press inline-flex h-6 items-center rounded-full border border-[var(--warp-rule)] px-2.5 text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  Try Again
                </button>
              </li>
            )}
            {recents.length === 0 && loading !== "first" && !stalled && (
              <li className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                {filters > 0 ? "No task matches these filters." : "Your tasks appear here. Start with New."}
              </li>
            )}
            {unpinned.map((t) => (
              <TaskRow
                key={t.id}
                task={t}
                current={isCurrent(t)}
                pinned={false}
                onSelect={() => go({ page: "task", id: t.id })}
                onPin={canPin ? () => onPin(t.id, true) : undefined}
              />
            ))}
            {/* Older conversations arrive a page at a time, when asked for. */}
            {(hasMore || loading === "more") && (
              <li className="flex justify-center pt-2">
                <button
                  type="button"
                  onClick={more}
                  disabled={loading === "more"}
                  className="inline-flex h-6 items-center rounded-full border border-[var(--warp-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-default"
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
      <div className={`flex items-center gap-1 border-aui-border border-t p-1.5 ${open ? "" : "flex-col"}`}>
        <button
          type="button"
          onClick={() => go({ page: "settings", section: "profile" })}
          aria-current={route.page === "settings" ? "page" : undefined}
          aria-label={`Settings for ${name}`}
          title={open ? undefined : "Settings"}
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
 * Navigation on a phone: the few places a person reaches for between other
 * things, one thumb away at the bottom of the screen. Inbox is first because
 * that is where what needs them lands. Menu opens the full sidebar, with
 * tasks and every other page.
 */
export function MobileNav({
  route,
  go,
  can,
  inboxCount,
  onMenu,
}: {
  route: Route;
  go: (next: Route) => void;
  can: (permission: string) => boolean;
  inboxCount: number;
  onMenu: () => void;
}) {
  const items: { to: Route; label: string; icon: ReactNode; show: boolean; active: boolean; badge?: number }[] = [
    { to: { page: "inbox" }, label: "Inbox", icon: <InboxIcon />, show: can("runs:read"), active: route.page === "inbox", badge: inboxCount },
    { to: { page: "new" }, label: "New", icon: <ComposeIcon />, show: can("runs:write"), active: route.page === "new" || route.page === "task" },
    {
      to: { page: "monitor", tab: "overview" },
      label: "Monitor",
      icon: <PulseIcon />,
      show: can("alerts:read") || can("runs:read"),
      active: route.page === "monitor",
    },
    { to: { page: "agents" }, label: "Agents", icon: <BotIcon />, show: can("agents:read"), active: route.page === "agents" || route.page === "agent" },
  ];
  const cell =
    "relative flex h-14 min-w-0 flex-1 flex-col items-center justify-center gap-0.5 text-aui-2xs text-aui-muted [&>svg]:size-5 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent aria-[current=page]:text-aui-fg";
  return (
    <nav
      aria-label="Quick navigation"
      data-testid="mobile-nav"
      className="warp-glass-bar fixed inset-x-0 bottom-0 z-30 flex border-[var(--warp-rule)] border-t pb-[env(safe-area-inset-bottom)] md:hidden"
    >
      {items
        .filter((i) => i.show)
        .map((i) => (
          <a
            key={i.label}
            href={href(i.to)}
            aria-current={i.active ? "page" : undefined}
            aria-label={i.badge ? `${i.label}, ${i.badge} need you` : undefined}
            onClick={(e) => {
              if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
              e.preventDefault();
              go(i.to);
            }}
            className={cell}
          >
            {i.icon}
            <span className="truncate">{i.label}</span>
            {i.badge ? (
              <span
                aria-hidden
                className="absolute top-1.5 left-1/2 ml-1.5 min-w-4 rounded-full bg-aui-accent px-1 text-center font-semibold text-[0.625rem] text-aui-accent-fg tabular-nums leading-4"
              >
                {i.badge > 99 ? "99+" : i.badge}
              </span>
            ) : null}
          </a>
        ))}
      <button type="button" onClick={onMenu} className={cell}>
        <MenuIcon />
        <span>Menu</span>
      </button>
    </nav>
  );
}
