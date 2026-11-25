import { defaultCopy } from "@humagen/components";
import {
  ArchiveIcon,
  BellIcon,
  BotIcon,
  CalendarIcon,
  CheckIcon,
  ChevronDownIcon,
  ChevronRightIcon,
  ClockIcon,
  EllipsisIcon,
  GitHubIcon,
  HistoryIcon,
  HourglassIcon,
  LayersIcon,
  ListChecksIcon,
  LogOutIcon,
  MessageSquareIcon,
  MonitorIcon,
  PanelLeftCloseIcon,
  PanelLeftOpenIcon,
  PencilIcon,
  PinIcon,
  PlusIcon,
  ProjectIcon,
  SearchIcon,
  SettingsIcon,
  ShieldIcon,
  SlackIcon,
  SlidersIcon,
  Tooltip,
  TrashIcon,
  UserIcon,
  XCircleIcon,
  ZapIcon,
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
import { agentShown, type Branding, servedBranding } from "./branding.ts";
import { Pill } from "./cards.tsx";
import {
  age,
  GROUP_ROWS,
  isUnread,
  type ListView,
  partition,
  projectOrder,
  type RowSource,
  type RowState,
  rowSource,
  rowState,
} from "./conversationList.ts";
import { daysAgo } from "./format.ts";
import { useMarks } from "./marks.ts";
import { ConversationPeek, PeekCard, usePeek } from "./RowPreview.tsx";
import { type Standing, useRecents, useRecentsRow } from "./recents.ts";
import { href, type Route, type Page as RoutePage } from "./route.ts";
import { type Alert, holdsAnAlertOnly, openAlert, orderAlerts } from "./sessionCenter.ts";
import { Avatar, workspaceSettingsRoute } from "./settingsLinks.tsx";
import {
  applySidebarWidth,
  clampSidebarWidth,
  SIDEBAR_WIDTH,
  sidebarCap,
  storedSidebarWidth,
} from "./sidebarState.ts";
import { AgentMark, type Flash, FlashNote, Popover, SkeletonRows, useFlash } from "./ui.tsx";
import { workspaceHub } from "./workspace-navigation.ts";

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
      // Open, the link is named by what it shows: its label, then its count.
      // An aria-label here would have to repeat those words exactly.
      aria-label={
        open ? undefined : count ? (badgeLabel ?? defaultCopy.sidebar.needYou(label, count)) : label
      }
      className={`relative flex h-9 pointer-coarse:h-11 items-center gap-2.5 [&>svg]:size-[18px] [&>svg]:[stroke-width:1.75] rounded-aui text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover/60 hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 aria-[current=page]:bg-aui-hover aria-[current=page]:font-medium aria-[current=page]:text-aui-fg ${
        open ? "px-2.5" : "w-9 justify-center"
      }`}
    >
      {icon}
      {open && <span className="humagen-sidebar-reveal flex-1 truncate text-left">{label}</span>}
      {count > 0 &&
        (open ? (
          <>
            {" "}
            <span className="humagen-sidebar-reveal rounded-full bg-aui-hover px-1.5 text-aui-2xs font-medium text-aui-fg">
              {count}
            </span>
            <span className="sr-only"> {badgeLabel ?? defaultCopy.sidebar.needYouSuffix}</span>
          </>
        ) : (
          <span aria-hidden className="absolute top-1 right-1 size-1.5 rounded-full bg-aui-muted" />
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
  // One line per filter, its current value on the right; it opens in place.
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const current = options.find((o) => o.value === value)?.label ?? "";
  return (
    <div className="flex flex-col">
      <button
        ref={trigger}
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((o) => !o)}
        className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
      >
        <span className="min-w-0 flex-1 truncate">{title}</span>
        <span className="truncate text-aui-muted">{current}</span>
        <ChevronRightIcon
          size={14}
          aria-hidden
          className={`shrink-0 text-aui-muted transition-transform ${open ? "rotate-90" : ""}`}
        />
      </button>
      {open && (
        <div role="group" aria-label={title} className="flex flex-col pb-1 pl-2">
          {options.map((o) => (
            <button
              key={o.value}
              type="button"
              aria-pressed={o.value === value}
              onClick={() => {
                onChange(o.value);
                setOpen(false);
                trigger.current?.focus();
              }}
              className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
            >
              {o.mark}
              <span className="min-w-0 flex-1 truncate">{o.label}</span>
              {o.value === value && <CheckIcon size={14} />}
            </button>
          ))}
        </div>
      )}
    </div>
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
      className={`absolute top-1 right-1 inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-opacity hover:bg-aui-hover hover:text-aui-fg focus-visible:opacity-100 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 [@media(pointer:coarse)]:top-0.5 [@media(pointer:coarse)]:size-11 [@media(pointer:coarse)]:opacity-100 ${project ? "group-hover/project:opacity-100 group-focus-within/project:opacity-100" : "group-hover/task:opacity-100 group-focus-within/task:opacity-100"} ${pinned ? "opacity-100" : "opacity-0"}`}
    >
      <PinIcon size={14} fill={pinned ? "currentColor" : "none"} />
    </button>
  );
}

/**
 * A project's conversations under a header that folds them. The header is
 * set in the foreground's weight so it reads above its rows, which carry the
 * icons. The chevron says which way it is. A group opens itself when the
 * open conversation is in it, and when rows arrive in one that started empty
 * unless the reader folded it; while folded over the open conversation the
 * header carries the selection, so where you are never disappears.
 */
function ProjectGroup({
  title,
  holdsCurrent,
  startsOpen = true,
  children,
}: {
  title: string;
  holdsCurrent: boolean;
  /** A project with nothing loaded in it starts folded. */
  startsOpen?: boolean;
  children: ReactNode;
}) {
  // What the reader chose by folding or unfolding, null until they do.
  const [chosen, setChosen] = useState<boolean | null>(null);
  useEffect(() => {
    if (holdsCurrent) setChosen(true);
  }, [holdsCurrent]);
  const open = chosen ?? startsOpen;
  return (
    <details
      open={open}
      onToggle={(e) => {
        if (e.currentTarget.open !== open) setChosen(e.currentTarget.open);
      }}
    >
      <summary
        aria-current={!open && holdsCurrent ? "true" : undefined}
        className="flex min-h-8 pointer-coarse:min-h-11 cursor-pointer list-none items-center gap-1.5 rounded-aui pr-12 pl-2.5 text-aui-xs font-semibold text-aui-fg transition-colors hover:bg-aui-hover/60 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 aria-[current]:bg-aui-hover [&::-webkit-details-marker]:hidden"
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
          className={`shrink-0 text-aui-muted transition-transform duration-150 motion-reduce:transition-none ${open ? "rotate-90" : ""}`}
        >
          <path d="M9 6l6 6-6 6" />
        </svg>
        <span className="min-w-0 flex-1 truncate">{title}</span>
      </summary>
      {children}
    </details>
  );
}

/** The one-time tip that teaches search; dismissed once per person (pkg/marks). */

/**
 * A conversation row's link: its title on the first line, when it moved and
 * where it started on the second, with room for a status badge. Hover
 * tints it; the open row is filled, set in medium weight and marked at its
 * left edge, so the two never read as one. Keyboard focus is a neutral ring
 * drawn inside the row.
 */
const rowLink =
  "relative flex w-full min-w-0 flex-col gap-0.5 rounded-aui py-1.5 pr-1.5 pl-2.5 pointer-coarse:py-2 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover/60 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 aria-[current=page]:bg-aui-hover before:absolute before:inset-y-2 before:left-0 before:w-0.5 before:rounded-full aria-[current=page]:before:bg-aui-fg";

/** A quiet icon button in a row's hover actions. */
const rowAction =
  "inline-flex size-8 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50";

/** One glyph per place a session can start, drawn in the row's muted ink. */
const SOURCE_ICONS: Record<RowSource, ReactNode> = {
  console: <MonitorIcon size={14} />,
  slack: <SlackIcon size={13} />,
  slack_dm: <SlackIcon size={13} />,
  github: <GitHubIcon size={14} />,
  scheduled: <CalendarIcon size={14} />,
  assignment: <ListChecksIcon size={14} />,
};

/**
 * Where a session started, on its row's second line beside its time: the
 * place's glyph and its short name, so it reads without a tooltip.
 */
function SourceLabel({ source }: { source: RowSource }) {
  return (
    <span
      data-source={source}
      title={defaultCopy.sidebar.sources[source]}
      className="inline-flex min-w-0 items-center gap-1"
    >
      <span aria-hidden className="inline-flex size-3.5 shrink-0 items-center justify-center">
        {SOURCE_ICONS[source]}
      </span>
      {/* Said in full after the title for a screen reader, so not twice. */}
      <span aria-hidden className="truncate">
        {defaultCopy.sidebar.sourceShort[source]}
      </span>
    </span>
  );
}

/**
 * A row's status as one small dot where the unread dot sits: amber when it
 * needs you, red when it failed, a soft pulse while it works, grey otherwise.
 * The reason is the dot's title and is said in words to assistive technology.
 */
const STATE_DOT: Record<RowState["tone"], string> = {
  warn: "bg-aui-warn",
  bad: "bg-aui-bad",
  neutral: "bg-aui-muted/70",
};

function RowStatusBadge({ state }: { state: RowState }) {
  return (
    <span
      aria-hidden
      data-state={state.kind}
      title={state.text}
      className={`size-2 shrink-0 rounded-full ${STATE_DOT[state.tone]} ${state.kind === "working" ? "motion-safe:animate-pulse" : ""}`}
    />
  );
}

/** Today, Yesterday or Earlier, by the reader's own calendar day. */
export function recentDay(iso: string, now: Date): "today" | "yesterday" | "earlier" {
  const days = daysAgo(iso, now.getTime());
  return days <= 0 ? "today" : days === 1 ? "yesterday" : "earlier";
}

const DAY_ORDER = ["today", "yesterday", "earlier"] as const;
const dayLabel = {
  today: defaultCopy.history.today,
  yesterday: defaultCopy.history.yesterday,
  earlier: defaultCopy.sidebar.earlier,
};

/**
 * Recents as one list: each session once, under Today, Yesterday and
 * Earlier in the reader's local time, in that order, newest first in each.
 */
export function recentGroups(
  rows: ThreadSummary[],
  now: Date,
): { day: (typeof DAY_ORDER)[number]; label: string; items: ThreadSummary[] }[] {
  const seen = new Set<string>();
  const buckets = { today: [], yesterday: [], earlier: [] } as Record<
    (typeof DAY_ORDER)[number],
    ThreadSummary[]
  >;
  for (const t of rows) {
    if (seen.has(t.id)) continue;
    seen.add(t.id);
    buckets[recentDay(t.updated_at, now)].push(t);
  }
  return DAY_ORDER.filter((d) => buckets[d].length).map((d) => ({
    day: d,
    label: dayLabel[d],
    items: buckets[d].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at)),
  }));
}

function TaskRow({
  task,
  current,
  pinned,
  archived = false,
  state,
  unread = false,
  ageText = "",
  source = "console",
  to,
  onSelect,
  onPin,
  onRename,
  onArchive,
  onDelete,
  flash = null,
}: {
  task: ThreadSummary;
  current: boolean;
  pinned: boolean;
  archived?: boolean;
  /** The most pressing status, with its full explanation. */
  state?: RowState;
  /** An agent said something the reader has not read: the title is heavier. */
  unread?: boolean;
  /** How long ago it moved, as the second line says it ("5m ago"). */
  ageText?: string;
  /** Where it started, on the second line. */
  source?: RowSource;
  /** Its address, so it opens in a new tab as a link does. */
  to: string;
  onSelect: () => void;
  onPin?: (button: HTMLButtonElement) => void;
  onRename?: (title: string) => void;
  /** Archives it for the reader alone, or brings it back. */
  onArchive?: (on: boolean) => void;
  /** Asks an admin to delete it. */
  onDelete?: () => void;
  flash?: Flash | null;
}) {
  const title = task.title || "Untitled conversation";
  const words = defaultCopy.sidebar;
  const [renaming, setRenaming] = useState(false);
  const actions = useRef<HTMLDivElement>(null);
  const trigger = () =>
    actions.current?.querySelector<HTMLButtonElement>("[data-row-menu] button") ?? null;
  const hasMenu = onPin || onRename || onArchive || onDelete;
  const save = (next: string) => {
    setRenaming(false);
    const t = next.trim();
    if (t && t !== task.title) onRename?.(t);
    requestAnimationFrame(() => trigger()?.focus());
  };
  // Resting the pointer or keyboard focus on the row, or holding a finger on
  // it, previews it: the preview says the status the badge shows as a glyph.
  const peek = usePeek();
  const peekId = `peek-${task.id}`;
  const statusId = `row-status-${task.id}`;
  const statusText = [state?.text, unread && words.state.unread].filter(Boolean).join(", ");
  const description = [statusText && statusId, peek.at && peekId].filter(Boolean).join(" ");
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
          className="h-9 w-full rounded-aui border border-aui-fg/40 bg-transparent px-2.5 text-aui-sm text-aui-fg outline-none"
        />
      ) : (
        <a
          href={to}
          onClick={(e) => {
            // A modified click opens it in a new tab, as any link does.
            if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
            e.preventDefault();
            onSelect();
          }}
          {...peek.bind}
          aria-describedby={description || undefined}
          data-thread-row={task.id}
          aria-current={current ? "page" : undefined}
          // The first line keeps the width the menu button takes at its end, so a title
          // is cut at the same place at rest and under the pointer.
          className={rowLink}
        >
          <span
            data-standing={unread ? "unread" : undefined}
            className={`min-w-0 truncate pr-7 pointer-coarse:pr-10 ${unread ? "font-semibold" : ""} ${current ? "font-medium" : ""}`}
          >
            {title}
          </span>
          <span
            data-row-age
            className="flex h-4 min-w-0 items-center gap-2 pr-7 text-aui-2xs text-aui-muted tabular-nums pointer-coarse:pr-10"
          >
            {ageText && <span className="shrink-0 whitespace-nowrap">{ageText}</span>}
            <SourceLabel source={source} />
          </span>
          {(state || unread) && (
            // The status sits in the menu button's column, centered under it, on the second line.
            <span
              data-row-status
              // Centered under the ⋯ button, which is 28px on a mouse so the two never meet.
              className="absolute right-0.5 bottom-1.5 flex h-4 w-8 items-center justify-center pointer-coarse:bottom-2 pointer-coarse:w-11 [@media(pointer:fine)]:w-7"
            >
              {state ? (
                <RowStatusBadge state={state} />
              ) : (
                <span
                  aria-hidden
                  data-dot="unread"
                  className="size-1.5 shrink-0 rounded-full bg-[var(--humagen-unread)]"
                />
              )}
            </span>
          )}
          <span className="sr-only">, {words.sources[source]}</span>
        </a>
      )}
      {statusText && (
        <span id={statusId} className="sr-only">
          {statusText}
        </span>
      )}
      {hasMenu && !renaming && (
        <div
          ref={actions}
          data-pin-thread={task.id}
          className="absolute top-0.5 right-0.5 flex items-center pointer-coarse:top-1"
        >
          <div data-row-menu onPointerDown={peek.close} onFocus={peek.close}>
            <Popover
              narrow
              align="right"
              label={`Manage ${title}`}
              popup="menu"
              className={`${rowAction} opacity-0 group-hover/task:opacity-100 group-focus-within/task:opacity-100 focus-visible:opacity-100 aria-expanded:opacity-100 aria-expanded:bg-aui-hover aria-expanded:text-aui-fg [@media(pointer:fine)]:size-7 [@media(pointer:coarse)]:size-11 ${current || peek.at ? "[@media(pointer:coarse)]:opacity-100" : "[@media(pointer:coarse)]:pointer-events-none [@media(pointer:coarse)]:group-focus-within/task:pointer-events-auto"}`}
              button={<EllipsisIcon size={16} />}
            >
              {(close) => (
                <RowMenu
                  pinned={pinned}
                  archived={archived}
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
                    (() => {
                      close();
                      onArchive(!archived);
                    })
                  }
                  onDelete={
                    onDelete &&
                    (() => {
                      close();
                      onDelete();
                    })
                  }
                />
              )}
            </Popover>
          </div>
        </div>
      )}
      {hasMenu && <FlashNote flash={flash} align="right" />}
      {peek.at && !renaming && (
        <PeekCard at={peek.at} id={peekId} onClose={peek.close}>
          <ConversationPeek task={task} status={state?.text} />
        </PeekCard>
      )}
    </li>
  );
}

const menuItem =
  "flex h-8 w-full items-center gap-2.5 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50";

/**
 * What a conversation's ⋯ menu offers. Each item has a letter that works while
 * the menu is open. Delete asks once more, since an admin then removes it.
 */
function RowMenu({
  pinned,
  archived,
  onPin,
  onRename,
  onArchive,
  onDelete,
}: {
  pinned: boolean;
  archived: boolean;
  onPin?: () => void;
  onRename?: () => void;
  onArchive?: () => void;
  onDelete?: () => void;
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
      label: archived ? "Unarchive" : "Archive",
      icon: <ArchiveIcon size={15} />,
      run: onArchive,
      separated: true,
    },
    onDelete && {
      key: "d",
      label: "Delete",
      icon: <TrashIcon size={15} />,
      run: () => setConfirming(true),
      danger: true,
    },
  ].filter((i): i is Exclude<typeof i, undefined | null | false> => Boolean(i));
  if (confirming && onDelete) {
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
            className="h-7 rounded-aui px-2.5 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
          >
            Cancel
          </button>
          <button
            type="button"
            onClick={onDelete}
            className="h-7 rounded-aui bg-aui-bad px-2.5 text-aui-sm text-white hover:opacity-90 focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
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
            aria-keyshortcuts={i.key}
            onClick={i.run}
            className={`${menuItem} ${i.danger ? "text-aui-bad" : ""}`}
          >
            {i.icon}
            <span className="min-w-0 flex-1 truncate">{i.label}</span>
            <kbd aria-hidden="true" className="text-aui-2xs text-aui-muted">
              {i.key.toUpperCase()}
            </kbd>
          </button>
        </div>
      ))}
    </div>
  );
}

/** A small section label, as Claude's sidebar draws Pinned and Recents. */
function Label({
  children,
  action,
  fold,
  count,
}: {
  children: ReactNode;
  action?: ReactNode;
  /** How many the section holds, after its name ("Pinned 2"). */
  count?: string | number;
  /** Makes the heading a button that folds its section away and brings it back. */
  fold?: { open: boolean; onToggle: () => void };
}) {
  return (
    // Solid on a desktop, in the sidebar's own color, so rows pass under it without showing through.
    <div className="humagen-sidebar-label sticky top-0 z-10 flex h-8 items-center justify-between gap-1 pr-2 pl-2">
      {fold ? (
        <button
          type="button"
          data-fold
          aria-expanded={fold.open}
          onClick={fold.onToggle}
          className="inline-flex min-h-7 min-w-0 items-center gap-1.5 rounded-aui px-2.5 text-aui-xs font-medium text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 pointer-coarse:min-h-11"
        >
          <span className="min-w-0 truncate">{children}</span>
          {count !== undefined && (
            <span data-count className="font-normal tabular-nums">
              {count}
            </span>
          )}
          {fold.open ? <ChevronDownIcon size={12} /> : <ChevronRightIcon size={12} />}
        </button>
      ) : (
        <p className="px-2.5 text-aui-xs text-aui-muted">{children}</p>
      )}
      {action}
    </div>
  );
}

/**
 * The open sidebar's edge on a wide screen, dragged to make the sidebar wider
 * or narrower between its bounds. With the keyboard it is a separator whose
 * arrow keys move it 16px, Home and End to its bounds; a double click puts it
 * back. The width is kept in this browser (sidebarState.ts).
 */
function ResizeHandle() {
  const [width, setWidth] = useState(storedSidebarWidth);
  // The sidebar is never drawn wider than a third of the window, so the edge
  // says and moves from the width that is drawn, not the one kept.
  const [cap, setCap] = useState(() => sidebarCap());
  useEffect(() => {
    const resized = () => setCap(sidebarCap());
    window.addEventListener("resize", resized);
    return () => window.removeEventListener("resize", resized);
  }, []);
  const drawn = Math.min(width, cap);
  const drag = useRef<{ x: number; from: number; kept: number } | null>(null);
  // Moves the edge to a width, and with `keep` remembers it. Only a width
  // the person actually changed is remembered: on a window too narrow for
  // the remembered width, a press that moves nothing, or a key toward a
  // bound the cap already holds it at, asks for the width already drawn, and
  // remembering that would overwrite what they chose on a wider screen with
  // this window's cap. `from` is the drawn width the gesture started at.
  const set = (next: number, keep: boolean, from = drawn) => {
    const to = clampSidebarWidth(Math.min(next, cap));
    if (keep && to === from) return false;
    setWidth(applySidebarWidth(to, keep));
    return true;
  };
  const step = 16;
  return (
    // biome-ignore lint/a11y/useSemanticElements: a vertical separator that moves is a focusable splitter, which <hr> is not
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={defaultCopy.sidebar.resize}
      aria-valuemin={SIDEBAR_WIDTH.min}
      aria-valuemax={Math.max(SIDEBAR_WIDTH.min, Math.min(SIDEBAR_WIDTH.max, cap))}
      aria-valuenow={drawn}
      tabIndex={0}
      data-sidebar-resize
      onPointerDown={(e) => {
        if (e.button !== 0) return;
        e.preventDefault();
        // Kept while the pointer moves off the thin edge; a browser that cannot is still dragged.
        e.currentTarget.setPointerCapture?.(e.pointerId);
        drag.current = { x: e.clientX, from: drawn, kept: width };
      }}
      onPointerMove={(e) => {
        const d = drag.current;
        if (d) set(d.from + e.clientX - d.x, false);
      }}
      onPointerUp={(e) => {
        const d = drag.current;
        drag.current = null;
        // Let go where it started: the width remembered before stays.
        if (d && !set(d.from + e.clientX - d.x, true, d.from)) setWidth(applySidebarWidth(d.kept));
      }}
      onPointerCancel={() => {
        const d = drag.current;
        drag.current = null;
        if (d) setWidth(applySidebarWidth(d.kept));
      }}
      // The default itself is remembered, not this window's cap on it.
      onDoubleClick={() => setWidth(applySidebarWidth(SIDEBAR_WIDTH.initial, true))}
      onKeyDown={(e) => {
        const next =
          e.key === "ArrowLeft"
            ? drawn - step
            : e.key === "ArrowRight"
              ? drawn + step
              : e.key === "Home"
                ? SIDEBAR_WIDTH.min
                : e.key === "End"
                  ? SIDEBAR_WIDTH.max
                  : null;
        if (next === null) return;
        e.preventDefault();
        set(next, true);
      }}
      className="group/resize absolute inset-y-0 right-0 z-30 flex w-1.5 cursor-col-resize touch-none justify-center outline-none max-md:hidden"
    >
      <span className="h-full w-px bg-transparent transition-colors group-hover/resize:bg-aui-fg/25 group-focus-visible/resize:w-0.5 group-focus-visible/resize:bg-aui-fg/50 group-active/resize:bg-aui-fg/40" />
    </div>
  );
}

/** Whether a section of the sidebar is folded, as this browser left it. It must never throw. */
function useFold(key: string): { open: boolean; onToggle: () => void } {
  const [open, setOpen] = useState(() => {
    try {
      return window.localStorage.getItem(key) !== "0";
    } catch {
      return true;
    }
  });
  return {
    open,
    onToggle: () =>
      setOpen((was) => {
        try {
          window.localStorage.setItem(key, was ? "0" : "1");
        } catch {
          // The fold still works for this visit.
        }
        return !was;
      }),
  };
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
  onSearch,
  sessionMode = false,
  sessionAlerts = [],
  drawer = false,
}: {
  open: boolean;
  /**
   * Open as a drawer over the page, on a phone: it takes focus, Escape or a
   * swipe toward the edge puts it away, and the page under it is inert.
   */
  drawer?: boolean;
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
  sessionMode?: boolean;
  sessionAlerts?: Alert[];
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
  // The account row is the name alone; the menu adds roles for a signed-in person.
  const subtitle = dev ? "" : (who.roles ?? []).join(", ") || who.tenant;
  // Settings open from the account row: yours, and the workspace's when you may see any of it.
  const workspaceSettings = workspaceSettingsRoute(can, who.roles ?? []);
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
  } = useRecents(agent, standing, threads, sessionMode);
  const filters = (agent ? 1 : 0) + (standing ? 1 : 0);
  // The row the open session is shown as: its own, or its root's when it is an agent's session under one.
  const openRow = useRecentsRow(
    route.page === "conversation" ? route.id : null,
    route.page === "conversation" && recents.some((t) => t.id === route.id),
  );
  // A pin that did not take is said beside the conversation it was for, in whichever
  // list the conversation is in by then.
  const { flash, say, clear: unsay } = useFlash();
  const [flashed, setFlashed] = useState<string | null>(null);
  const sidebar = useRef<HTMLElement>(null);
  const closer = useRef<HTMLButtonElement>(null);
  const toggleRef = useRef(onToggle);
  toggleRef.current = onToggle;
  // The drawer is modal: focus goes into it and comes back to the button that
  // opened it, and Escape closes it unless something inside used the key.
  useEffect(() => {
    if (!drawer) return;
    const aside = sidebar.current;
    closer.current?.focus({ preventScroll: true });
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape" || e.defaultPrevented) return;
      e.preventDefault();
      toggleRef.current();
    };
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("keydown", onKey);
      const inside = !document.activeElement || aside?.contains(document.activeElement);
      if (!inside) return;
      requestAnimationFrame(() => {
        const now = document.activeElement;
        if (now && now !== document.body && !aside?.contains(now)) return;
        document.querySelector<HTMLElement>("[data-testid='drawer-button']")?.focus();
      });
    };
  }, [drawer]);
  const swipe = useRef<{ x: number; y: number } | null>(null);
  const pinFocus = useRef<{
    kind: "conversation" | "project";
    id: string;
    pinned: boolean;
    /** The row's control it was pinned from: its ⋮ menu or its Pin button. */
    from?: string;
  } | null>(null);
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
          ?.querySelector<HTMLButtonElement>(target.from ?? "[data-row-menu] button");
    // An unpinned row may leave the current group or loaded page.
    // Keep focus at its remaining control or a stable navigation target.
    const fallback = projects
      ? sidebar.current?.querySelector<HTMLElement>(
          'nav[aria-label="Sections"] a[href="/projects"]',
        )
      : sidebar.current?.querySelector<HTMLButtonElement>(
          'section[aria-label="Sessions"] button:not([data-fold])',
        );
    (control ?? fallback)?.focus();
    pinFocus.current = null;
  }, [pinned, prefs?.pinned_projects]);
  const togglePin = (id: string, on: boolean, button: HTMLButtonElement) => {
    unsay();
    if (document.activeElement === button)
      pinFocus.current = {
        kind: "conversation",
        id,
        pinned: on,
        from: button.closest("[data-row-menu]") ? "[data-row-menu] button" : "[data-row-pin]",
      };
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
  const participatedFold = useFold("humagen.sidebar.participated");
  const pinnedFold = useFold("humagen.sidebar.pinned");
  const recentsFold = useFold("humagen.sidebar.recents");
  const selected = route.page === "conversation" ? route.id : null;
  const here = (...pages: RoutePage[]) => pages.includes(route.page);
  const nav = defaultCopy.navigation;
  const hub = workspaceHub(route);
  const entries = [
    {
      to: { page: "sessions" } as Route,
      label: "Sessions",
      icon: <HistoryIcon />,
      show: can("runs:read"),
      active: here("sessions", "history", "conversation"),
    },
    {
      to: { page: "alerts" } as Route,
      label: "Alerts",
      icon: <BellIcon />,
      show: can("alerts:read"),
      active: here("alerts", "inbox", "notifications"),
      badge: inboxCount,
    },
    {
      to: { page: "projects" } as Route,
      label: "Projects",
      icon: <ProjectIcon />,
      show: can("projects:read"),
      active: here("projects", "project"),
    },
    {
      to: { page: "agents" } as Route,
      label: "Agents",
      icon: <BotIcon />,
      show: can("agents:read"),
      active: hub === "agents" || hub === "customize",
    },
    {
      to: { page: "automations" } as Route,
      label: "Automations",
      icon: <ZapIcon />,
      show: can("schedules:read"),
      active: here("automations", "schedules"),
    },
    {
      to: { page: "artifacts" } as Route,
      label: nav.artifacts,
      icon: <LayersIcon />,
      show: can("artifacts:read"),
      active:
        route.page === "artifacts" ||
        (route.page === "library" && route.section === "deliverables"),
    },
  ];

  // Archived here is the reader's own: the conversation leaves their lists
  // and stays for everyone else. Delete still asks an admin.
  const { marks, archive: setArchivedMark, read: markRead } = useMarks(can("runs:read"));
  // Recent lists sessions newest first; By project, By day and Archived are
  // groupings one choice away in the filter.
  const [view, setView] = useState<ListView>("flat");
  const [note, setNote] = useState<{ id: string; archived: boolean } | null>(null);
  const [gone, setGone] = useState<Set<string>>(() => new Set());
  const [titles, setTitles] = useState<Record<string, string>>({});
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const [now, setNow] = useState(() => Date.now());
  // Ages move on their own; once a minute is enough for "5m".
  useEffect(() => {
    const t = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(t);
  }, []);
  const shown = (t: ThreadSummary) =>
    titles[t.id] ? { ...t, title: titles[t.id]!, named: true } : t;
  const toggleArchive = (t: ThreadSummary, on: boolean) => {
    unsay();
    setNote({ id: t.id, archived: on });
    if (on && (selected === t.id || openRow === t.id)) go({ page: "new" });
    setArchivedMark(t.id, on).catch(() => {
      setNote(null);
      setFlashed(t.id);
      say(on ? defaultCopy.sidebar.archiveFailed : defaultCopy.sidebar.unarchiveFailed, true);
    });
  };
  const undoArchive = () => {
    if (!note) return;
    const { id, archived } = note;
    setNote(null);
    setArchivedMark(id, !archived).catch(() => {});
  };
  const remove = (t: ThreadSummary) => {
    unsay();
    setGone((g) => new Set(g).add(t.id));
    if (pinned.includes(t.id)) onPin(t.id, false).catch(() => {});
    if (selected === t.id || openRow === t.id) go({ page: "new" });
    coworking.archive(t.id, true).catch(() => {
      setGone((g) => {
        const n = new Set(g);
        n.delete(t.id);
        return n;
      });
      setFlashed(t.id);
      say("Could not delete. Try again.", true);
    });
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
  // Every session loaded, once each: the caller's live ones and the
  // pages of Recents. Pinned and Archived come from these, so one that cannot
  // be found (deleted, or no longer readable) is not shown.
  const known = new Map(
    [...threads, ...recents].filter((t) => !gone.has(t.id)).map((t) => [t.id, shown(t)]),
  );
  const lists = partition(
    [...known.values()].sort((a, b) => b.updated_at.localeCompare(a.updated_at)),
    pinned,
    marks,
  );
  const pinnedRows = lists.pinned;
  const pinnedSet = new Set(pinned);
  // Recents is the paged list, so a filter narrows it; the open conversation
  // stays in it even when it is older than what is loaded.
  const archivedSet = new Set(marks.archived);
  // The most pressing open alert on each session, which its row says in words.
  const alertBySession = new Map<
    string,
    { priority: "urgent" | "normal" | "low"; title: string }
  >();
  for (const alert of orderAlerts(sessionAlerts.filter(openAlert)))
    if (alert.session && !alertBySession.has(alert.session))
      alertBySession.set(alert.session, {
        priority: alert.priority ?? "normal",
        title: alert.title,
      });
  // Recents is newest first. An alert marks its session's row; it does not move
  // it, and a session that only holds an alert is in Alerts, not here.
  const unpinned = [
    ...new Map(
      recents
        .filter(
          (t) =>
            !pinnedSet.has(t.id) &&
            !gone.has(t.id) &&
            !archivedSet.has(t.id) &&
            (t.id === openRow || !holdsAnAlertOnly(t)),
        )
        .map((t) => [t.id, shown(t)] as const),
    ).values(),
  ].sort((a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at));
  const currentThread = openRow ? known.get(openRow) : undefined;
  if (
    currentThread &&
    !pinnedSet.has(currentThread.id) &&
    !archivedSet.has(currentThread.id) &&
    !unpinned.some((thread) => thread.id === currentThread.id)
  ) {
    unpinned.push(currentThread);
  }
  // Sessions someone else started that the reader joined have their own
  // section above Pinned; Recent is the rest.
  const participatedRows = unpinned.filter((t) => t.joined);
  const recentRows = unpinned.filter((t) => !t.joined);
  // Opening a conversation reads it, and so does staying in it while it moves.
  const selectedUpdated = (selected ? known.get(selected) : undefined)?.updated_at;
  // biome-ignore lint/correctness/useExhaustiveDependencies: a newer update is the reason to mark it read again
  useEffect(() => {
    if (selected && can("runs:read")) markRead(selected);
  }, [selected, selectedUpdated, markRead, can]);
  const noConversations = recents.length === 0 && unpinned.length === 0;
  // Nothing to list under Recents: it says so once, in words that fit why, and
  // draws no project folders that would contradict it.
  const listEmpty = recentRows.length === 0;
  const isCurrent = (t: ThreadSummary) => t.id === openRow;
  const canPin = can("runs:read");
  const pinnedProjects = new Set(prefs?.pinned_projects ?? []);
  const readableProjectPins = [...pinnedProjects].filter(
    (project) =>
      can("projects:read") && (project === "~" || projects.some((item) => item.name === project)),
  );
  // The Projects view: projects with a conversation loaded first, then the
  // reader's others, which open to "No conversations". Bounded, as the
  // sidebar is not the project list.
  const projectGroups = projectOrder(
    recentRows,
    projects.slice(0, 12).map((p) => p.name),
  );
  const row = (t: ThreadSummary, opts: { pinned?: boolean } = {}) => (
    <TaskRow
      key={t.id}
      task={t}
      source={rowSource(t)}
      current={isCurrent(t)}
      pinned={opts.pinned ?? false}
      archived={archivedSet.has(t.id)}
      state={rowState(t, defaultCopy.sidebar.state, {
        session: sessionMode,
        alert: alertBySession.get(t.id),
      })}
      unread={isUnread(t, marks)}
      ageText={defaultCopy.sidebar.movedAgo(age(t.updated_at, now))}
      to={href({ page: "conversation", id: t.id })}
      onSelect={() => go({ page: "conversation", id: t.id })}
      onPin={
        canPin && !archivedSet.has(t.id)
          ? (button) => togglePin(t.id, !opts.pinned, button)
          : undefined
      }
      onRename={can("runs:write") ? (title) => rename(t, title) : undefined}
      onArchive={canPin ? (on) => toggleArchive(t, on) : undefined}
      onDelete={can("runs:write") ? () => remove(t) : undefined}
      flash={flashed === t.id ? flash : null}
    />
  );
  const viewTitle =
    view === "projects"
      ? defaultCopy.sidebar.projects
      : view === "archived"
        ? defaultCopy.sidebar.archived
        : defaultCopy.sidebar.recents;

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
          aria-label={defaultCopy.sidebar.dismiss}
          onClick={onToggle}
          className="humagen-fade fixed inset-0 z-40 touch-none bg-black/40 md:hidden"
        />
      )}
      <aside
        ref={sidebar}
        aria-label="Navigation"
        // A swipe toward the edge it came from closes the drawer; the close
        // button and Escape do the same for anyone who cannot swipe.
        onTouchStart={
          drawer
            ? (e) => {
                const t = e.touches[0];
                swipe.current = t && e.touches.length === 1 ? { x: t.clientX, y: t.clientY } : null;
              }
            : undefined
        }
        onTouchEnd={
          drawer
            ? (e) => {
                const from = swipe.current;
                const t = e.changedTouches[0];
                swipe.current = null;
                if (from && t && drawerSwiped(from, { x: t.clientX, y: t.clientY })) onToggle();
              }
            : undefined
        }
        className={`aui-chrome humagen-glass-bar humagen-sidebar flex h-full min-h-0 shrink-0 flex-col max-md:border-aui-border max-md:border-r ${
          open
            ? "relative w-[min(var(--humagen-sidebar-w),33.333vw)] max-md:fixed max-md:inset-y-0 max-md:left-0 max-md:z-50 max-md:w-[min(20rem,85vw)]"
            : "w-12 max-md:hidden"
        }`}
      >
        {open && !drawer && <ResizeHandle />}
        <div
          className={`flex h-12 shrink-0 items-center ${open ? "justify-between pr-2 pl-4.5" : "justify-center"}`}
        >
          {open && (
            <span className="humagen-display humagen-sidebar-reveal flex min-w-0 items-center gap-2 truncate text-aui-base font-semibold text-aui-fg">
              <BrandMark branding={brand} />
              {brand?.wordmark}
            </span>
          )}
          <button
            type="button"
            ref={closer}
            onClick={onToggle}
            aria-expanded={open}
            aria-label={open ? "Close sidebar" : "Open sidebar"}
            className="inline-flex size-8 pointer-coarse:size-11 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
          >
            {open ? (
              <PanelLeftCloseIcon size={18} strokeWidth={1.75} />
            ) : (
              <PanelLeftOpenIcon size={18} strokeWidth={1.75} />
            )}
          </button>
        </div>

        <nav
          aria-label="Sections"
          className="aui-scroll flex min-h-0 flex-1 flex-col [@media(max-height:600px)]:overflow-x-hidden [@media(max-height:600px)]:overflow-y-auto [@media(max-height:600px)]:overscroll-contain"
        >
          {/* Starting work, finding work and the sections stay fixed while the session list scrolls.
              Below 600px of height that block would leave the list no room, so the whole nav scrolls. */}
          <div
            className={`flex shrink-0 flex-col gap-1 ${open ? "px-2" : "items-center"}`}
            data-sidebar-start
          >
            {can("runs:write") && (
              <Item
                icon={<PlusIcon />}
                label="New"
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
                className={`group/search flex h-9 pointer-coarse:h-11 items-center gap-2.5 [&>svg]:[stroke-width:1.75] rounded-aui text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover/60 hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 ${open ? "px-2.5" : "w-9 justify-center"}`}
              >
                <SearchIcon size={18} />
                {open && (
                  <>
                    <span className="humagen-sidebar-reveal flex-1 text-left">{nav.search}</span>
                    <kbd className="humagen-sidebar-reveal text-aui-2xs opacity-0 transition-opacity group-hover/search:opacity-100 group-focus-visible/search:opacity-100 motion-reduce:transition-none">
                      ⌘ K
                    </kbd>
                  </>
                )}
              </button>
            )}
            {entries
              .filter((entry) => entry.show)
              .map((entry) => (
                <Item
                  key={entry.label}
                  icon={entry.icon}
                  label={entry.label}
                  href={href(entry.to)}
                  open={open}
                  active={entry.active}
                  badge={entry.badge}
                  onClick={() => go(entry.to)}
                />
              ))}
          </div>
          <div
            data-sidebar-scroll
            className="aui-scroll flex min-h-0 flex-1 flex-col overflow-x-hidden overflow-y-auto overscroll-contain [@media(max-height:600px)]:flex-none [@media(max-height:600px)]:overflow-visible"
          >
            {open && view !== "archived" && participatedRows.length > 0 && (
              <section
                aria-label={defaultCopy.sidebar.participated}
                className="humagen-sidebar-reveal flex shrink-0 flex-col pt-6"
              >
                <Label fold={participatedFold} count={participatedRows.length}>
                  {defaultCopy.sidebar.participated}
                </Label>
                <ul className="px-2" hidden={!participatedFold.open}>
                  {participatedRows.map((t) => row(t))}
                </ul>
              </section>
            )}

            {open && (pinnedRows.length > 0 || readableProjectPins.length > 0) && (
              <section
                aria-label="Pinned"
                className="humagen-sidebar-reveal flex shrink-0 flex-col pt-6"
              >
                <Label fold={pinnedFold} count={pinnedRows.length + readableProjectPins.length}>
                  {defaultCopy.sidebar.pinned}
                </Label>
                <ul className="px-2" hidden={!pinnedFold.open}>
                  {readableProjectPins.map((project) => (
                    <li
                      key={`project:${project}`}
                      className={`group/project relative ${flashed === `project:${project}` && flash ? "z-20" : ""}`}
                    >
                      <div className="py-1 pr-10 [@media(pointer:coarse)]:pr-12">
                        <Item
                          icon={<ProjectIcon />}
                          label={projectTitle(project)}
                          href={href({ page: "project", name: project === "~" ? "" : project })}
                          active={
                            route.page === "project" &&
                            route.name === (project === "~" ? "" : project)
                          }
                          open
                          onClick={() =>
                            go({ page: "project", name: project === "~" ? "" : project })
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
                  {pinnedRows.map((t) => row(t, { pinned: true }))}
                </ul>
              </section>
            )}

            {open && can("runs:read") && (
              <section
                aria-label="Sessions"
                data-view={view}
                className="humagen-sidebar-reveal flex shrink-0 flex-col pt-6"
              >
                {/* The label stays in reach while the list scrolls under it. */}
                <Label
                  fold={recentsFold}
                  action={
                    <Popover
                      narrow
                      align="right"
                      label={defaultCopy.chat.filter(filters)}
                      className="relative inline-flex size-8 pointer-coarse:size-11 items-center justify-center rounded-aui text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 aria-expanded:bg-aui-hover aria-expanded:text-aui-fg"
                      button={
                        <>
                          <SlidersIcon size={14} />
                          {(filters > 0 || view !== "flat") && (
                            <span
                              aria-hidden
                              className="absolute top-1.5 right-1.5 size-1.5 rounded-full bg-aui-fg"
                            />
                          )}
                        </>
                      }
                    >
                      {() => (
                        <div className="aui-scroll flex max-h-[60vh] flex-col overflow-y-auto">
                          <Choices<ListView>
                            title="View"
                            value={view}
                            options={[
                              { value: "flat", label: defaultCopy.sidebar.views.flat },
                              { value: "projects", label: defaultCopy.sidebar.views.projects },
                              { value: "recents", label: defaultCopy.sidebar.views.recents },
                              {
                                value: "archived",
                                label: `${defaultCopy.sidebar.archived}${marks.archived.length ? ` (${marks.archived.length})` : ""}`,
                              },
                            ]}
                            onChange={setView}
                          />
                          <Choices
                            title="Status"
                            value={standing}
                            options={[
                              { value: "", label: "All" },
                              { value: "running", label: "Running" },
                              { value: "failed", label: "Failed" },
                            ]}
                            onChange={setStanding}
                          />
                          {answering.length > 1 && (
                            <Choices
                              title="Agent"
                              value={agent}
                              options={[
                                { value: "", label: "Any" },
                                ...answering.map((a) => ({
                                  value: a,
                                  label: agentShown(a),
                                  mark: <AgentMark name={a} size={12} />,
                                })),
                              ]}
                              onChange={setAgent}
                            />
                          )}
                          {(filters > 0 || view !== "flat") && (
                            <button
                              type="button"
                              onClick={() => {
                                clear();
                                setView("flat");
                              }}
                              className="mt-1 rounded-aui border-[var(--humagen-rule)] border-t px-2.5 py-2 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                            >
                              Reset to defaults
                            </button>
                          )}
                        </div>
                      )}
                    </Popover>
                  }
                >
                  {view === "archived" ? defaultCopy.sidebar.archived : defaultCopy.sidebar.recent}
                </Label>
                {note && (
                  <p
                    role="status"
                    className="mx-3 mb-1 flex items-center gap-2 rounded-aui bg-aui-hover px-2.5 py-1.5 text-aui-xs text-aui-muted"
                  >
                    <span className="min-w-0 flex-1">
                      {note.archived
                        ? defaultCopy.sidebar.archivedNote
                        : defaultCopy.sidebar.unarchivedNote}
                    </span>
                    <button
                      type="button"
                      onClick={undoArchive}
                      className="shrink-0 rounded-aui px-1.5 font-medium text-aui-fg hover:bg-aui-bg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                    >
                      {defaultCopy.sidebar.undo}
                    </button>
                    <button
                      type="button"
                      onClick={() => setNote(null)}
                      aria-label="Dismiss"
                      className="shrink-0 rounded-aui px-1 text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                    >
                      ×
                    </button>
                  </p>
                )}
                <ul
                  className="px-2 pb-3"
                  hidden={!recentsFold.open}
                  aria-label={view === "recents" ? defaultCopy.sidebar.recents : viewTitle}
                  aria-busy={loading !== null}
                >
                  {noConversations && loading === "first" && (
                    <li className="px-2.5">
                      <SkeletonRows count={5} height="1.25rem" />
                    </li>
                  )}
                  {noConversations && loading !== "first" && stalled && (
                    <li className="flex flex-col items-start gap-1.5 px-2.5 py-1.5 text-aui-xs text-aui-muted">
                      Your sessions could not be loaded.
                      <button
                        type="button"
                        onClick={retry}
                        className="humagen-press inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                      >
                        Try Again
                      </button>
                    </li>
                  )}
                  {view !== "archived" && listEmpty && loading !== "first" && !stalled && (
                    <li data-empty className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                      {/* Said of every section: a session listed above is not "no session". */}
                      {filters > 0
                        ? participatedRows.length > 0
                          ? "No other session matches these filters."
                          : "No session matches these filters."
                        : pinnedRows.length > 0 || participatedRows.length > 0
                          ? "No other sessions yet."
                          : "Your sessions appear here. Start with New."}
                    </li>
                  )}
                  {view === "flat" && recentRows.map((t) => row(t))}
                  {view === "recents" &&
                    recentGroups(recentRows, new Date(now)).map((g, i) => (
                      <li
                        key={g.day}
                        className={i === 0 ? "" : "pt-3"}
                        data-testid="recents-group"
                        data-day={g.day}
                      >
                        {/* Recents already says these are the newest, so today's sessions take no heading. */}
                        {g.day !== "today" && (
                          <p className="px-2.5 pb-1 text-aui-xs text-aui-muted">{g.label}</p>
                        )}
                        <ul>{g.items.map((t) => row(t))}</ul>
                      </li>
                    ))}
                  {view === "archived" &&
                    (lists.archived.length === 0 && loading !== "first" ? (
                      <li className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                        {defaultCopy.sidebar.noArchived}
                      </li>
                    ) : (
                      lists.archived.map((t) => row(t))
                    ))}
                  {view === "projects" &&
                    !listEmpty &&
                    projectGroups.map((project) => {
                      const inGroup = recentRows.filter((t) => (t.project || "~") === project);
                      const all = expanded.has(project);
                      const rows = all ? inGroup : inGroup.slice(0, GROUP_ROWS);
                      return (
                        <li
                          key={project}
                          className={`group/project relative min-w-0 pt-3 ${flashed === `project:${project}` && flash ? "z-20" : ""}`}
                        >
                          <ProjectGroup
                            title={projectTitle(project)}
                            startsOpen={inGroup.length > 0}
                            holdsCurrent={inGroup.some(isCurrent)}
                          >
                            <ul className="mt-0.5">
                              {inGroup.length === 0 && (
                                <li className="px-2.5 py-1.5 text-aui-xs text-aui-muted">
                                  {defaultCopy.sidebar.noConversations}
                                </li>
                              )}
                              {rows.map((t) => row(t))}
                              {inGroup.length > GROUP_ROWS && (
                                <li>
                                  <button
                                    type="button"
                                    aria-expanded={all}
                                    onClick={() =>
                                      setExpanded((e) => {
                                        const n = new Set(e);
                                        if (all) n.delete(project);
                                        else n.add(project);
                                        return n;
                                      })
                                    }
                                    className="flex h-8 w-full items-center rounded-aui px-2.5 text-left text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                                  >
                                    {all
                                      ? defaultCopy.sidebar.showFewer
                                      : defaultCopy.sidebar.seeAll(inGroup.length)}
                                  </button>
                                </li>
                              )}
                            </ul>
                          </ProjectGroup>
                          {onPinProject && can("projects:read") && (
                            <div className="absolute top-2 right-0 h-9 w-10">
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
                      );
                    })}
                  {/* Older conversations arrive a page at a time, when asked for. */}
                  {(hasMore || loading === "more") && (
                    <li className="flex justify-center pt-2">
                      <button
                        type="button"
                        onClick={more}
                        disabled={loading === "more"}
                        className="inline-flex h-6 items-center rounded-full border border-[var(--humagen-rule)] px-2.5 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 disabled:cursor-default"
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
        </nav>

        {!live && open && (
          <p className="px-4 pb-2 text-aui-2xs text-aui-muted">
            Reconnecting. What you see may be out of date.
          </p>
        )}
        <div
          className={`flex shrink-0 items-center gap-1 border-aui-border border-t py-1.5 ${open ? "pr-1.5 pl-3.5" : "flex-col px-1.5"}`}
        >
          <div className={open ? "min-w-0 flex-1" : ""}>
            <Popover
              narrow
              up
              side={open ? undefined : "right"}
              // Open, the button is named by the name it shows, then what it opens.
              label={open ? undefined : defaultCopy.preferences.openFor(name)}
              tooltip={open ? undefined : name}
              className={`flex w-full min-w-0 items-center gap-2 rounded-aui p-1 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 aria-expanded:bg-aui-hover ${
                open ? "" : "size-9 justify-center"
              }`}
              button={
                <>
                  <Avatar name={name} prefs={prefs} size={26} />
                  {open && (
                    <span className="humagen-sidebar-reveal flex min-w-0 flex-1 items-baseline truncate">
                      <span className="max-w-full truncate text-aui-sm text-aui-fg">{name}</span>
                      <span className="sr-only">, {defaultCopy.preferences.open}</span>
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
                      {subtitle && (
                        <span className="block truncate text-aui-2xs text-aui-muted">
                          {subtitle}
                        </span>
                      )}
                    </span>
                  </div>
                  <Separator className="my-1" />
                  {[
                    {
                      label: "Your settings",
                      icon: <UserIcon size={14} />,
                      to: { page: "settings", section: "profile" } as Route,
                    },
                    ...(workspaceSettings
                      ? [
                          {
                            label: "Workspace settings",
                            icon: <SettingsIcon size={14} />,
                            to: workspaceSettings,
                          },
                        ]
                      : []),
                  ].map((item) => (
                    <button
                      key={item.label}
                      type="button"
                      onClick={() => {
                        close();
                        go(item.to);
                      }}
                      className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
                    >
                      {item.icon}
                      <span className="min-w-0 flex-1 truncate">{item.label}</span>
                    </button>
                  ))}
                  {!dev && (
                    <>
                      <Separator className="my-1" />
                      <button
                        type="button"
                        onClick={() => {
                          close();
                          onSignOut();
                        }}
                        className="flex h-8 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50"
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
 * on it says something in the inbox is unread.
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
      aria-label={inboxCount > 0 ? `Open sidebar, ${inboxCount} unread` : "Open sidebar"}
      className="humagen-drawer-button fixed top-[calc(0.5rem+env(safe-area-inset-top))] left-[max(1rem,env(safe-area-inset-left))] z-30 inline-flex size-11 items-center justify-center rounded-full border border-aui-border text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-fg/50 md:hidden"
    >
      <PanelLeftOpenIcon size={20} />
      {inboxCount > 0 && (
        <span
          aria-hidden
          className="absolute top-2 right-2 size-2 rounded-full bg-aui-fg ring-2 ring-aui-bg"
        />
      )}
    </button>
  );
}

/**
 * Whether a touch that went from `from` to `to` swiped the drawer shut: at
 * least 56px toward the left edge, and more sideways than up or down, so
 * scrolling its lists never closes it.
 */
/**
 * Whether a touch that went from `from` to `to` swiped the drawer open from
 * the screen's left edge: started within EDGE_PX of it, went at least 56px
 * right, and more sideways than up or down.
 */
export function edgeSwiped(from: { x: number; y: number }, to: { x: number; y: number }): boolean {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  return from.x <= EDGE_PX && dx >= 56 && Math.abs(dx) > Math.abs(dy) * 1.5;
}

/** How near the screen's left edge a swipe must start to open the drawer. */
export const EDGE_PX = 24;

/**
 * On a phone, with the drawer shut, a swipe from the screen's left edge
 * toward the middle opens it, as the drawer button does.
 */
export function useEdgeSwipe(enabled: boolean, onOpen: () => void): void {
  useEffect(() => {
    if (!enabled) return;
    let from: { x: number; y: number } | null = null;
    const start = (e: TouchEvent) => {
      const t = e.touches[0];
      from =
        e.touches.length === 1 && t && t.clientX <= EDGE_PX && window.innerWidth < 768
          ? { x: t.clientX, y: t.clientY }
          : null;
    };
    const end = (e: TouchEvent) => {
      const t = e.changedTouches[0];
      if (from && t && edgeSwiped(from, { x: t.clientX, y: t.clientY })) onOpen();
      from = null;
    };
    window.addEventListener("touchstart", start, { passive: true });
    window.addEventListener("touchend", end, { passive: true });
    return () => {
      window.removeEventListener("touchstart", start);
      window.removeEventListener("touchend", end);
    };
  }, [enabled, onOpen]);
}

export function drawerSwiped(
  from: { x: number; y: number },
  to: { x: number; y: number },
): boolean {
  const dx = to.x - from.x;
  const dy = to.y - from.y;
  return dx <= -56 && Math.abs(dx) > Math.abs(dy) * 1.5;
}
