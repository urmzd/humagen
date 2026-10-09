import { InboxView } from "@agent-ui/components";
import { type ActionHandler, type InboxItem, openCount } from "@agent-ui/core";
import { CheckCircleIcon, WebSurface } from "@agent-ui/render-web";
import { type KeyboardEvent, useEffect, useRef, useState } from "react";

export type InboxFilter = "all" | "needs" | "alerts" | "resolved";

const TABS: { id: InboxFilter; label: string }[] = [
  { id: "all", label: "All" },
  { id: "needs", label: "Needs you" },
  { id: "alerts", label: "Alerts" },
  { id: "resolved", label: "Resolved" },
];

function matches(item: InboxItem, filter: InboxFilter) {
  switch (filter) {
    case "all":
      return true;
    case "needs":
      return item.state === "open" && item.kind !== "alert";
    case "alerts":
      return item.kind === "alert";
    case "resolved":
      return item.state !== "open";
  }
}

/** How long a just-resolved item keeps its record in place before moving to Resolved. */
const LINGER_MS = 2500;

/**
 * Items that just left "open" stay put for a moment, showing their record
 * ("Approved by You · just now"), then move to Resolved.
 */
function useLinger(items: InboxItem[]): InboxItem[] {
  const prev = useRef(new Map<string, InboxItem["state"]>());
  const [lingering, setLingering] = useState<Set<string>>(new Set());
  useEffect(() => {
    const changed: string[] = [];
    for (const i of items) {
      if (prev.current.get(i.id) === "open" && i.state !== "open") changed.push(i.id);
      prev.current.set(i.id, i.state);
    }
    if (changed.length === 0) return;
    setLingering((s) => new Set([...s, ...changed]));
    const t = setTimeout(
      () => setLingering((s) => new Set([...s].filter((id) => !changed.includes(id)))),
      LINGER_MS,
    );
    return () => clearTimeout(t);
  }, [items]);
  return items.map((i) => (lingering.has(i.id) ? { ...i, state: "open" } : i));
}

/** Top-nav button with the open count. */
export function InboxButton({
  count,
  expanded,
  onClick,
}: {
  count: number;
  expanded?: boolean;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-expanded={expanded}
      aria-label={count > 0 ? `Inbox, ${count} need you` : "Inbox"}
      className="relative inline-flex h-8 items-center gap-1.5 rounded-aui px-2.5 text-aui-muted text-aui-sm transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover aria-expanded:text-aui-fg"
    >
      <svg
        aria-hidden="true"
        width="15"
        height="15"
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        strokeLinejoin="round"
      >
        <path d="M22 12h-6l-2 3h-4l-2-3H2" />
        <path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" />
      </svg>
      Inbox
      {count > 0 && (
        <span className="inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-aui-accent px-1 font-semibold text-[0.625rem] text-aui-accent-fg tabular-nums">
          {count > 99 ? "99+" : count}
        </span>
      )}
    </button>
  );
}

/**
 * The inbox as a panel or page: open count, filter tabs, and the shared
 * InboxView. j/k move between entries; decisions resolve in place.
 */
export function Inbox({
  items,
  onAction,
  viewer,
  defaultFilter = "all",
  className,
}: {
  items: InboxItem[];
  onAction?: ActionHandler;
  viewer?: string;
  defaultFilter?: InboxFilter;
  className?: string;
}) {
  const [filter, setFilter] = useState<InboxFilter>(defaultFilter);
  const shown = useLinger(items);
  const visible = shown.filter((i) => matches(i, filter));
  const body = useRef<HTMLDivElement>(null);
  const count = openCount(items);

  // Entries end with their own actions row; j/k focus the first control of each entry.
  const onKeyDown = (e: KeyboardEvent) => {
    if (e.key !== "j" && e.key !== "k") return;
    const t = e.target as HTMLElement;
    if (t.closest("textarea, input")) return;
    const root = body.current;
    if (!root) return;
    const focusables = Array.from(
      root.querySelectorAll<HTMLElement>("button:not([disabled]), a[href]"),
    );
    const blockOf = (el: HTMLElement) => el.closest('[data-aui-block="inbox"]');
    const starts = focusables.filter((el, i) => {
      const prev = focusables[i - 1];
      return !prev || (blockOf(prev) !== null && blockOf(prev) !== blockOf(el));
    });
    let current = -1;
    starts.forEach((s, i) => {
      if (s === t || s.compareDocumentPosition(t) & Node.DOCUMENT_POSITION_FOLLOWING) current = i;
    });
    const to = e.key === "j" ? current + 1 : Math.max(0, current - 1);
    const target = starts[Math.min(starts.length - 1, to)];
    if (target) {
      e.preventDefault();
      target.focus();
      target.scrollIntoView({ block: "nearest" });
    }
  };

  return (
    <section
      aria-label="Inbox"
      onKeyDown={onKeyDown}
      className={`flex min-h-0 flex-col ${className ?? ""}`}
    >
      <header className="flex items-center gap-2 border-aui-border border-b pb-1.5">
        <h2 className="font-semibold text-aui-base">Inbox</h2>
        {count > 0 && (
          <span className="rounded-full bg-aui-accent-soft px-1.5 font-medium text-aui-2xs text-aui-fg tabular-nums">
            {count} open
          </span>
        )}
        <div role="tablist" aria-label="Filter" className="ml-auto flex gap-0.5">
          {TABS.map((tab) => {
            const n = items.filter((i) => matches(i, tab.id)).length;
            return (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={filter === tab.id}
                onClick={() => setFilter(tab.id)}
                className="inline-flex h-6 items-center gap-1 rounded-aui-sm px-2 text-aui-muted text-aui-xs transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-selected:bg-aui-subtle aria-selected:font-medium aria-selected:text-aui-fg"
              >
                {tab.label}
                <span className="opacity-60 tabular-nums">{n}</span>
              </button>
            );
          })}
        </div>
      </header>
      <div ref={body} role="tabpanel" className="aui-scroll min-h-0 flex-1 overflow-y-auto py-2">
        {visible.length === 0 ? (
          <div className="flex flex-col items-center gap-1.5 py-10 text-center">
            <CheckCircleIcon size={20} className="text-aui-ok" />
            <p className="font-medium text-aui-sm">
              {filter === "resolved" ? "Nothing resolved yet" : "Nothing waiting on you"}
            </p>
            <p className="text-aui-muted text-aui-xs">
              Approvals, questions, and alerts show up here.
            </p>
          </div>
        ) : (
          <WebSurface onAction={onAction} className="[&>[data-aui=header]:first-child]:sr-only">
            <InboxView items={visible} viewer={viewer} />
          </WebSurface>
        )}
      </div>
      <p className="hidden pt-1 text-aui-2xs text-aui-muted sm:block">
        <kbd className="font-medium">j</kbd>/<kbd className="font-medium">k</kbd> to move between
        items
      </p>
    </section>
  );
}
