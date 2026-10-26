import { agentHue, requestAgentHue, subscribeHues } from "./agentHues.ts";
import { BanIcon, CheckIcon, ChevronDownIcon, ShieldIcon, Tooltip, XIcon } from "@humagen/render-web";
import { defaultCopy } from "@humagen/components";
import { AgentOrb, type OrbState } from "@humagen/web";
import { type CSSProperties, type ReactNode, type RefObject, useEffect, useId, useLayoutEffect, useRef, useState, useSyncExternalStore } from "react";
import type { Run } from "./adapter.ts";
import { cause, dollars, elapsed, took } from "./format.ts";
import { moreOf, nudgeInto, revealAt } from "./layout.ts";

export const field =
  "w-full rounded-aui border border-aui-border bg-aui-bg px-2.5 py-1.5 text-aui-sm text-aui-fg outline-none placeholder:text-aui-muted focus-visible:border-aui-accent focus-visible:ring-2 focus-visible:ring-aui-ring";

// A button's label stays on one line: the button is one line tall.
export const button =
  "inline-flex h-8 pointer-coarse:h-10 items-center justify-center gap-1.5 whitespace-nowrap humagen-press rounded-aui border border-aui-border bg-aui-surface px-3 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-not-allowed disabled:opacity-50";

export const primary =
  "inline-flex h-8 pointer-coarse:h-10 items-center justify-center gap-1.5 whitespace-nowrap humagen-press rounded-aui bg-aui-primary px-3 text-aui-sm font-medium text-aui-primary-fg hover:opacity-90 focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent disabled:cursor-not-allowed disabled:opacity-50";

export function Label({ text, hint, children }: { text: string; hint?: string; children: ReactNode }) {
  return (
    <label className="flex flex-col gap-1">
      <span className="text-aui-xs font-medium text-aui-muted">{text}</span>
      {children}
      {hint && <span className="text-aui-2xs text-aui-muted">{hint}</span>}
    </label>
  );
}

export function Problem({ message }: { message: string | null }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-aui border border-aui-bad/40 bg-aui-bad/10 px-3 py-2 text-aui-sm text-aui-fg">
      {message}
    </p>
  );
}

export function Empty({ title, children }: { title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col gap-1 py-10">
      <p className="humagen-display text-aui-lg font-medium text-aui-fg">{title}</p>
      {children && <p className="max-w-md text-aui-sm text-aui-muted">{children}</p>}
    </div>
  );
}

const statusTone: Record<Run["status"], string> = {
  accepted: "bg-aui-neutral",
  running: "bg-aui-info",
  parked: "bg-aui-warn",
  completed: "bg-[var(--humagen-ink)]",
  failed: "bg-aui-bad",
  // Stopped from outside is not a failure, so it is not red.
  stopped: "bg-[var(--humagen-ink-faint)]",
};

const statusWord: Record<Run["status"], string> = {
  accepted: "Queued",
  running: "Running",
  parked: "Waiting",
  completed: "Done",
  failed: "Failed",
  stopped: "Stopped",
};

/** Status as a dot plus a word, so it never depends on color alone. */
export function Status({ status }: { status: Run["status"] }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-aui-xs text-aui-muted">
      <span aria-hidden className={`size-1.5 rounded-full ${statusTone[status]}`} />
      {statusWord[status]}
    </span>
  );
}

export function Chip({ children }: { children: ReactNode }) {
  return (
    <span className="inline-flex h-5 items-center rounded-full border border-aui-border px-2 text-aui-2xs text-aui-muted">
      {children}
    </span>
  );
}

const units: [Intl.RelativeTimeFormatUnit, number][] = [
  ["day", 86_400],
  ["hour", 3_600],
  ["minute", 60],
];
const relative = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

export function ago(iso: string): string {
  const seconds = Math.round((Date.parse(iso) - Date.now()) / 1000);
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) return relative.format(Math.round(seconds / size), unit);
  }
  return "just now";
}

// Formatting with no React in it lives in format.ts, so plain modules and their tests can use it too.
export { cause, elapsed, isEnded, took, usd } from "./format.ts";

/**
 * Every page sits in the same left-aligned column, so the title and the first
 * row of content start at the same place wherever a person goes.
 */
/** One choice offered by PickMany. */
export interface Pickable {
  value: string;
  label: string;
  hint?: string;
  mark?: ReactNode;
}

/**
 * A set picked from a list that may be long: what is picked shows as chips, and
 * more are added from a searchable list. It stays usable at five options or five
 * hundred, where a switch per option does not.
 */
export function PickMany({
  label,
  add = "Add",
  options,
  value,
  disabled,
  empty,
  onChange,
}: {
  label: string;
  /** The add button's words. */
  add?: string;
  options: Pickable[];
  value: string[];
  disabled?: boolean;
  /** What to say when nothing is picked. */
  empty?: string;
  onChange: (next: string[]) => void;
}) {
  const [query, setQuery] = useState("");
  const byValue = new Map(options.map((o) => [o.value, o]));
  const q = query.trim().toLowerCase();
  const rest = options.filter((o) => !value.includes(o.value) && (!q || `${o.label} ${o.hint ?? ""}`.toLowerCase().includes(q)));
  return (
    <div className="flex flex-col gap-2">
      {value.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{empty ?? "None yet."}</p>
      ) : (
        <ul aria-label={label} className="flex flex-col">
          {value.map((v) => {
            const o = byValue.get(v);
            return (
              <li key={v} className="flex items-center gap-3 border-aui-border border-t py-2 first:border-t-0">
                {o?.mark}
                <span className="min-w-0 flex-1">
                  <span className="block truncate text-aui-sm text-aui-fg">{o?.label ?? v}</span>
                  {o?.hint && <span className="block truncate text-aui-xs text-aui-muted">{o.hint}</span>}
                  {!o && <span className="block text-aui-xs text-aui-muted">No longer exists.</span>}
                </span>
                {!disabled && (
                  <button type="button" className={button} aria-label={`Remove ${o?.label ?? v}`} onClick={() => onChange(value.filter((x) => x !== v))}>
                    Remove
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
      {!disabled && options.some((o) => !value.includes(o.value)) && (
        <Popover
          label={`${add}: ${label}`}
          className={`${button} self-start`}
          button={<>{add}</>}
        >
          {(close) => (
            <div className="flex max-h-[min(24rem,60vh)] flex-col gap-1">
              <input
                // biome-ignore lint/a11y/noAutofocus: the list opened because the person asked to pick
                autoFocus
                type="search"
                aria-label={`Find: ${label}`}
                placeholder="Find"
                className={`${field} h-8`}
                value={query}
                onChange={(e) => setQuery(e.target.value)}
              />
              <ul className="aui-scroll flex flex-col overflow-y-auto">
                {rest.length === 0 && <li className="px-2.5 py-2 text-aui-xs text-aui-muted">Nothing matches.</li>}
                {rest.map((o) => (
                  <li key={o.value}>
                    <button
                      type="button"
                      onClick={() => {
                        onChange([...value, o.value]);
                        setQuery("");
                        close();
                      }}
                      className="flex w-full items-center gap-2 rounded-aui px-2.5 py-1.5 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                    >
                      {o.mark}
                      <span className="min-w-0 flex-1">
                        <span className="block truncate text-aui-sm text-aui-fg">{o.label}</span>
                        {o.hint && <span className="block truncate text-aui-xs text-aui-muted">{o.hint}</span>}
                      </span>
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </Popover>
      )}
    </div>
  );
}

/** The one style for a page's title and the line under it, wherever a page draws its own header. */
export const pageTitle = "humagen-display truncate text-[1.875rem] leading-[1.15] font-semibold text-aui-fg";
export const pageLede = "max-w-[62ch] pt-1.5 text-aui-sm text-aui-muted";

export function Page({
  title,
  lede,
  icon,
  meta,
  crumbs,
  back,
  actions,
  tabs,
  layout,
  start,
  narrow,
  children,
}: {
  title: ReactNode;
  /** One or two lines on what the page is for. */
  lede?: ReactNode;
  /** Drawn before the title: an agent's orb, a folder. */
  icon?: ReactNode;
  /** A short line under the title: a revision, a status, who made it. */
  meta?: ReactNode;
  /** Where the page sits in a tree, as plain links from the top down, under the meta line. */
  crumbs?: ReactNode;
  /** Where the page sits: a link back to the list it came from. */
  back?: { label: string; onClick: () => void };
  /** The page's own actions. The primary one goes last. */
  actions?: ReactNode;
  /** Tabs under the header, sticking to the top while the page scrolls. What the page shows under them is a column of its own. */
  tabs?: ReactNode;
  /** Conversation controls surround its one centered page title. */
  layout?: "conversation";
  /** The project control, at the leading edge of a conversation. */
  start?: ReactNode;
  /** A reading width, for forms and settings. */
  narrow?: boolean;
  /** Kept for callers. Pages share one width now. */
  wide?: boolean;
  children: ReactNode;
}) {
  if (layout === "conversation") {
    return (
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <header className="humagen-conversation-header relative z-10 grid shrink-0 items-center gap-x-3 gap-y-2 border-aui-border border-b px-4 py-3">
          <div className="humagen-conversation-start flex min-w-0 items-center gap-2">{start}</div>
          <div className="humagen-conversation-title min-w-0 text-center">
            <h1 className="truncate text-aui-sm font-semibold text-aui-fg" title={typeof title === "string" ? title : undefined}>{title}</h1>
            {meta && <div className="truncate pt-1 text-aui-2xs text-aui-muted">{meta}</div>}
          </div>
          <div className="humagen-conversation-actions flex min-w-0 items-center justify-end gap-1">{actions}</div>
        </header>
        {children}
      </div>
    );
  }
  const width = narrow ? "max-w-3xl" : "max-w-[68rem]";
  return (
    <div className="aui-scroll min-h-0 flex-1 overflow-y-auto">
      {/* Every page is one centered column, so a wide window never strands the content against the sidebar. */}
      <div className={`@container humagen-fade mx-auto flex w-full ${width} flex-col gap-6 px-5 pt-8 pb-12 md:px-10`}>
        <header className="flex flex-col gap-3">
          {back && (
            <button
              type="button"
              onClick={back.onClick}
              className="inline-flex min-h-6 items-center self-start rounded-aui text-aui-xs text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              ‹ {back.label}
            </button>
          )}
          <div className="flex flex-wrap items-start justify-between gap-x-6 gap-y-3">
            <div className="flex min-w-0 items-center gap-3">
              {icon}
              <div className="min-w-0">
                <h1 className={pageTitle}>{title}</h1>
                {meta && <div className="flex flex-wrap items-center gap-x-3 pt-1 text-aui-xs text-aui-muted">{meta}</div>}
                {crumbs && <div className="pt-1.5">{crumbs}</div>}
                {lede && <p className={pageLede}>{lede}</p>}
              </div>
            </div>
            {/* The actions may shrink to the column, so on a narrow screen they wrap onto more lines instead of running off it. */}
            {actions && <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2">{actions}</div>}
          </div>
        </header>
        {tabs && <div className="sticky top-0 z-10 -mx-1 bg-[var(--aui-bg)] px-1">{tabs}</div>}
        {/*
          Under tabs, what a tab shows is one column of its own. The tab strip draws a rule,
          so a section that opens the column, and draws its rule only after something, draws none.
        */}
        {tabs ? <div className="flex min-w-0 flex-col gap-6">{children}</div> : children}
      </div>
    </div>
  );
}

/**
 * A count beside a label: a tab's, a section's heading. Read with its label,
 * never on its own, and never a status color.
 */
export function Count({ n }: { n: number }) {
  // Read as "Needs you, 3": a pause, so the number is heard as the heading's count.
  return (
    <>
      <span className="sr-only">, </span>
      <CountBadge n={n} />
    </>
  );
}

function CountBadge({ n }: { n: number }) {
  return <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-aui-hover px-1.5 text-aui-2xs font-normal tabular-nums text-aui-muted">{n}</span>;
}

/**
 * A control drawn as text: a section's way to see the rest ("See All"), a link
 * back. Its label is Title Case, its hit area at least 24px tall, and it is
 * underlined, so it reads as something to press, not as a line of text, on a
 * phone too, where nothing hovers.
 */
export const textAction =
  "inline-flex min-h-6 items-center gap-1 rounded-aui text-aui-sm font-medium text-aui-fg underline underline-offset-4 hover:no-underline focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * A control inside a line of text, a row's details or a table cell: "Open the
 * run", an agent's name, Download. Underlined, so it never reads as plain text,
 * and a line 24px tall, so it is never too small to press.
 */
export const textLink =
  "inline-block min-h-6 rounded-aui-sm leading-6 text-aui-fg underline underline-offset-2 hover:no-underline focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * A titled part of a page: a heading, its count, an optional line on it, and
 * its own actions. `box` draws it as a panel: a group that stands on its own,
 * such as Home's Needs you, rather than one part of a longer page.
 */
export function Section({
  title,
  count,
  hint,
  actions,
  box,
  children,
}: {
  title: ReactNode;
  /** How many there are in all, beside the title. Hidden at zero. */
  count?: number;
  hint?: ReactNode;
  actions?: ReactNode;
  box?: boolean;
  children: ReactNode;
}) {
  const id = useId();
  return (
    <section aria-labelledby={id} className={`flex min-w-0 flex-col gap-3 ${box ? card : ""}`}>
      <div className="flex flex-wrap items-end justify-between gap-x-4 gap-y-2">
        <div className="min-w-0">
          <h2 id={id} className="flex items-center gap-2 text-aui-base font-medium text-aui-fg">
            {title}
            {count !== undefined && count > 0 && <Count n={count} />}
          </h2>
          {hint && <p className="max-w-[62ch] text-aui-xs text-aui-muted">{hint}</p>}
        </div>
        {actions && <div className="flex min-w-0 max-w-full flex-wrap items-center gap-2">{actions}</div>}
      </div>
      {children}
    </section>
  );
}

/**
 * One row of a list that opens something: what it is on the first line, and
 * a quieter line under it (who, where, when). The whole row is the control.
 * `lead` is drawn before the text in a column of its own, so marked and
 * unmarked rows line up.
 */
export function RowButton({
  title,
  detail,
  lead,
  label,
  onClick,
}: {
  title: ReactNode;
  detail?: ReactNode;
  lead?: ReactNode;
  /** What a screen reader says for the row, when the visible text is not enough. */
  label?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-label={label}
      className="flex min-h-11 w-full min-w-0 items-start gap-2 rounded-aui px-2 py-2 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent"
    >
      {lead !== undefined && <span className="flex w-2 shrink-0 justify-center self-stretch pt-[0.45rem]">{lead}</span>}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="min-w-0 truncate text-aui-sm text-aui-fg">{title}</span>
        {detail && <span className="min-w-0 truncate text-aui-xs text-aui-muted">{detail}</span>}
      </span>
    </button>
  );
}

export const card = "humagen-glass rounded-aui-lg p-4";

/** A list of rows on hairlines, with no box around it. */
export const rows = "divide-y divide-[var(--humagen-rule)] border-[var(--humagen-rule)] border-y";

/** Figures side by side on one rule. Wrap a row of `Stat`s in it. */
export const figures =
  "grid grid-cols-2 gap-x-8 gap-y-5 border-[var(--humagen-rule)] border-y py-5 sm:grid-cols-3 lg:grid-cols-5";

/** A figure with its label. The value is ink, never a status color. */
export function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="flex min-w-0 flex-col">
      <span className="text-aui-xs text-aui-muted">{label}</span>
      <span className="humagen-display pt-0.5 text-[1.75rem] leading-9 font-semibold text-aui-fg">{value}</span>
      {note && <span className="text-aui-2xs text-aui-muted">{note}</span>}
    </div>
  );
}

/** The longest run in a set, as the scale a tape is drawn against. */
export function longest(runs: Run[]): number {
  return runs.reduce((max, r) => Math.max(max, elapsed(r)), 0);
}

/** What a tape says, for anyone who cannot see it. */
function tally(runs: Run[]): string {
  if (runs.length === 0) return "No runs yet";
  const count = (s: Run["status"]) => runs.filter((r) => r.status === s).length;
  const parts = [`${runs.length} ${runs.length === 1 ? "run" : "runs"}`];
  for (const [status, word] of [
    ["failed", "failed"],
    ["running", "running"],
    ["parked", "waiting"],
  ] as const) {
    if (count(status) > 0) parts.push(`${count(status)} ${word}`);
  }
  return parts.join(", ");
}

/**
 * The run tape: the most recent runs, oldest on the left, one tick each. A
 * tick's height is how long the run took, on a log scale so a slow run stands
 * out without flattening the rest. Pass `onOpen` to make each tick open its run.
 */
export function Tape({
  runs,
  slots = 40,
  scale,
  tall,
  onOpen,
  className = "",
}: {
  /** Any order. The newest `slots` are drawn. */
  runs: Run[];
  slots?: number;
  /** The duration, in ms, that fills the tape. Share one across tapes to compare them. */
  scale?: number;
  tall?: boolean;
  onOpen?: (id: string) => void;
  className?: string;
}) {
  const shown = [...runs].sort((a, b) => a.created_at.localeCompare(b.created_at)).slice(-slots);
  const top = Math.log1p(Math.max(scale ?? longest(shown), 1));
  const blanks = Array.from({ length: Math.max(slots - shown.length, 0) }, (_, i) => i);
  const style = tall ? ({ "--humagen-tape-h": "2.75rem" } as React.CSSProperties) : undefined;
  return (
    <div
      role={onOpen ? "group" : "img"}
      aria-label={`Recent runs. ${tally(shown)}.`}
      className={`humagen-tape ${className}`}
      style={style}
    >
      {blanks.map((i) => (
        <span key={i} aria-hidden className="humagen-tick" data-status="none" />
      ))}
      {shown.map((r) => {
        const t = { "--t": Math.min(Math.log1p(elapsed(r)) / top, 1).toFixed(3) } as React.CSSProperties;
        const label = `${r.agent}, ${statusWord[r.status].toLowerCase()}, ${took(elapsed(r))}, ${ago(r.created_at)}`;
        return onOpen ? (
          <button
            key={r.id}
            type="button"
            className="humagen-tick"
            data-status={r.status}
            style={t}
            title={label}
            aria-label={`Open run: ${label}`}
            onClick={() => onOpen(r.id)}
          />
        ) : (
          <span key={r.id} aria-hidden className="humagen-tick" data-status={r.status} style={t} title={label} />
        );
      })}
    </div>
  );
}

/** The original palette, retained as the fallback before the agent list arrives. */
export const AGENT_HUES = 12;
export { setAgentHues } from "./agentHues.ts";

function useAgentHue(name: string): number | undefined {
  const hue = useSyncExternalStore(subscribeHues, () => agentHue(name), () => agentHue(name));
  useEffect(() => { requestAgentHue(name); }, [name]);
  return hue;
}

/** A server-reserved hue, with the original mapping while loading. */
export function hueOf(name: string): number {
  const assigned = agentHue(name);
  if (assigned !== undefined) return assigned;
  let h = 186;
  for (const c of name) h = (h * 31 + c.charCodeAt(0)) % 1000003;
  return (h % AGENT_HUES) * (360 / AGENT_HUES);
}

/**
 * Gives everything inside it an agent's color. humagen draws its accent from
 * one hue, so setting that hue here recolors the orb, and any accent, for this
 * agent alone.
 */
export function AgentTint({ name, className, children }: { name: string; className?: string; children: ReactNode }) {
  useAgentHue(name);
  return (
    <div data-accent="agent" className={className} style={{ "--aui-accent-h": hueOf(name) } as CSSProperties}>
      {children}
    </div>
  );
}

/**
 * An agent's mark is its orb, in its own color. The same orb sits beside its
 * answers, where it moves with what the agent is doing, so the mark in a list
 * and the presence in a conversation are one thing. At rest it is still.
 */
export function AgentMark({ name, size = 18, state = "idle" }: { name: string; size?: number; state?: OrbState }) {
  const hue = useAgentHue(name);
  if (hue === undefined) return <span aria-hidden className="inline-block shrink-0" style={{ width: size, height: size }} />;
  return (
    <span data-accent="agent" className="inline-flex shrink-0 items-center align-middle" style={{ "--aui-accent-h": hueOf(name) } as CSSProperties}>
      <AgentOrb state={state} size={size} label="" />
    </span>
  );
}

/** A labelled on/off choice. */
export function Switch({
  label,
  hint,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  hint?: string;
  checked: boolean;
  disabled?: boolean;
  onChange: (next: boolean) => void;
}) {
  return (
    <label className="flex min-h-6 items-start justify-between gap-4">
      <span className="min-w-0">
        <span className="block text-aui-sm text-aui-fg">{label}</span>
        {hint && <span className="block text-aui-xs text-aui-muted">{hint}</span>}
      </span>
      <input
        type="checkbox"
        className="humagen-switch mt-0.5"
        checked={checked}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
    </label>
  );
}

/** One of a few choices, shown together. */
export function Segmented<T extends string>({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: T;
  options: { id: T; label: string }[];
  onChange: (next: T) => void;
}) {
  const strip = useStrip<HTMLDivElement>(value);
  // A fieldset is as wide as its content unless told otherwise, so min-w-0 lets it
  // fit the column; its choices then scroll inside the box, each on one line.
  return (
    <fieldset className="inline-flex min-w-0 max-w-full rounded-aui border border-aui-border p-0.5">
      <legend className="sr-only">{label}</legend>
      <div ref={strip} className="humagen-strip flex min-w-0 overflow-x-auto [scrollbar-width:none]">
        {options.map((o) => (
          <button
            key={o.id}
            type="button"
            aria-pressed={value === o.id}
            onClick={() => onChange(o.id)}
            className="h-7 shrink-0 whitespace-nowrap rounded-aui-sm px-2.5 text-aui-xs text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover aria-pressed:text-aui-fg"
          >
            {o.label}
          </button>
        ))}
      </div>
    </fieldset>
  );
}

/**
 * A row that scrolls sideways inside itself when it does not fit: tabs, chips,
 * a segmented choice. Whenever `chosen` changes, whenever items are added to
 * or taken from the row, and whenever the row or what is in it changes size,
 * it brings the chosen item (aria-selected, aria-pressed or aria-current) into
 * view. It marks which ends have more as data-more, which humagen-strip fades, so
 * an item cut off at the edge reads as more to scroll to.
 */
export function useStrip<E extends HTMLElement>(chosen: unknown): RefObject<E | null> {
  const ref = useRef<E>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: a new choice is the signal to bring it into view
  useLayoutEffect(() => {
    if (ref.current) fit(ref.current);
  }, [chosen]);
  useEffect(() => {
    const strip = ref.current;
    if (!strip) return;
    const marked = () => mark(strip);
    strip.addEventListener("scroll", marked, { passive: true });
    // Widths settle late, when a face loads or the window turns, so the place is found again.
    const sized = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(() => fit(strip));
    sized?.observe(strip);
    for (const item of strip.children) sized?.observe(item);
    // Items come and go after the strip shows, as an agent's tabs do once it loads. The new
    // ones are watched too, and the place is found again: what fitted may no longer.
    const changed =
      typeof MutationObserver === "undefined"
        ? null
        : new MutationObserver((records) => {
            for (const r of records) {
              for (const node of r.addedNodes) if (node instanceof Element) sized?.observe(node);
              for (const node of r.removedNodes) if (node instanceof Element) sized?.unobserve(node);
            }
            fit(strip);
          });
    changed?.observe(strip, { childList: true });
    return () => {
      strip.removeEventListener("scroll", marked);
      sized?.disconnect();
      changed?.disconnect();
    };
  }, []);
  return ref;
}

/** The strip's own measurements: how far it is scrolled, how wide it is, and how wide its content is. */
const viewOf = (strip: HTMLElement) => ({ scroll: strip.scrollLeft, width: strip.clientWidth, content: strip.scrollWidth });

function fit(strip: HTMLElement) {
  const item = strip.querySelector<HTMLElement>('[aria-selected="true"], [aria-pressed="true"], [aria-current="page"]');
  if (item) {
    const box = strip.getBoundingClientRect();
    const r = item.getBoundingClientRect();
    // Where the item sits in the strip's content, which starts inside its border.
    const start = r.left - box.left - strip.clientLeft + strip.scrollLeft;
    const at = revealAt({ start, end: start + r.width }, viewOf(strip));
    if (at !== strip.scrollLeft) strip.scrollLeft = at;
  }
  mark(strip);
}

function mark(strip: HTMLElement) {
  const more = moreOf(viewOf(strip));
  if (more === "none") delete strip.dataset.more;
  else strip.dataset.more = more;
}

/** Tabs for the sections of one thing. */
export function Tabs<T extends string>({
  label,
  value,
  tabs,
  onChange,
}: {
  label: string;
  value: T;
  tabs: { id: T; label: string; count?: number }[];
  onChange: (next: T) => void;
}) {
  const strip = useStrip<HTMLDivElement>(value);
  // The rule under the tabs is drawn beside the strip rather than on it, so it runs the
  // full width while the strip's ends fade, and the chosen tab's line sits on it.
  return (
    <div className="relative">
      <span aria-hidden className="pointer-events-none absolute inset-x-0 bottom-0 border-aui-border border-b" />
      <div ref={strip} role="tablist" aria-label={label} className="humagen-strip aui-scroll relative flex gap-1 overflow-x-auto">
        {tabs.map((t) => (
          <button
            key={t.id}
            type="button"
            role="tab"
            aria-selected={value === t.id}
            onClick={() => onChange(t.id)}
            className="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap border-transparent border-b-2 px-3 text-aui-sm text-aui-muted transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:-outline-offset-2 focus-visible:outline-aui-accent aria-selected:border-aui-accent aria-selected:text-aui-fg"
          >
            {t.label}
            {t.count !== undefined && t.count > 0 && <Count n={t.count} />}
          </button>
        ))}
      </div>
    </div>
  );
}

/**
 * How far to move an open panel sideways so it stays inside the screen's
 * gutter, when the control it hangs from sits near an edge, as the composer's
 * agent menu does on a phone. It is measured from the panel's laid-out box,
 * which its opening scale does not change, and again when the window resizes.
 */
export function useInsideGutter(panel: RefObject<HTMLElement | null>, root: RefObject<HTMLElement | null>, open: boolean): number {
  const [shift, setShift] = useState(0);
  useLayoutEffect(() => {
    if (!open) return;
    const place = () => {
      const p = panel.current;
      const r = root.current;
      if (!p || !r) return;
      const left = r.getBoundingClientRect().left + r.clientLeft + p.offsetLeft;
      setShift(nudgeInto({ left, width: p.offsetWidth }, document.documentElement.clientWidth || window.innerWidth));
    };
    place();
    window.addEventListener("resize", place);
    return () => window.removeEventListener("resize", place);
  }, [open, panel, root]);
  return shift;
}

/**
 * The second step of an action that cannot be undone, in place on the page:
 * what will happen in words, the action itself, and Keep It. It takes focus,
 * on Keep It, and Escape or moving focus away from it keeps things as they
 * are. An action that can be undone (Archive, Mute) acts at once instead.
 */
export function Confirm({ question, action, onConfirm, onKeep }: { question: string; action: string; onConfirm: () => void; onKeep: () => void }) {
  const root = useRef<HTMLDivElement>(null);
  useEffect(() => {
    root.current?.querySelector<HTMLButtonElement>("[data-keep]")?.focus();
  }, []);
  return (
    <div
      ref={root}
      role="group"
      aria-label={question}
      className="flex flex-wrap items-center gap-3"
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.stopPropagation();
          onKeep();
        }
      }}
      onBlur={(e) => {
        if (!root.current?.contains(e.relatedTarget as Node | null)) onKeep();
      }}
    >
      <span className="text-aui-sm text-aui-fg">{question}</span>
      <button type="button" className={button} onClick={onConfirm}>
        {action}
      </button>
      <button type="button" data-keep className={button} onClick={onKeep}>
        {defaultCopy.confirm.keep}
      </button>
    </div>
  );
}

/** One of a page's own actions, for `PageActions`. */
export interface PageAction {
  label: string;
  onSelect: () => void;
  /** Archive, Delete: last in the menu, after a rule. What it does is the same in the menu as on a button. */
  destructive?: boolean;
}

/**
 * A page's actions in its header: at most one primary action, as a button,
 * and the rest beside it. Two in all are drawn as buttons. With more, the
 * rest are one More menu, a text control, with the destructive ones last
 * after a rule, so the header on a phone is its title, its meta and two
 * controls on one line. `note` sits beside them, such as a flash that says
 * an action did not take.
 */
export function PageActions({ primary: main, actions, note }: { primary?: PageAction; actions: PageAction[]; note?: ReactNode }) {
  const few = actions.length + (main ? 1 : 0) <= 2;
  return (
    <span className="relative inline-flex flex-wrap items-center gap-2">
      {few
        ? actions.map((a) => (
            <button key={a.label} type="button" className={button} onClick={a.onSelect}>
              {a.label}
            </button>
          ))
        : actions.length > 0 && <MoreMenu items={actions} />}
      {main && (
        <button type="button" className={primary} onClick={main.onSelect}>
          {main.label}
        </button>
      )}
      {note}
    </span>
  );
}

/**
 * The More menu: a text control that opens a list of actions. Arrow keys,
 * Home and End move through it; Escape, Tab or a click outside close it and
 * put focus back on the control. Destructive actions come last, after a rule.
 */
export function MoreMenu({ items, label = defaultCopy.more }: { items: PageAction[]; label?: string }) {
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLSpanElement>(null);
  const list = useRef<HTMLDivElement>(null);
  const id = useId();
  const shift = useInsideGutter(list, root, open);
  const everyday = items.filter((i) => !i.destructive);
  const destructive = items.filter((i) => i.destructive);

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);

  const entries = () => Array.from(list.current?.querySelectorAll<HTMLButtonElement>("[role=menuitem]") ?? []);
  const focusAt = (i: number) => {
    const all = entries();
    if (all.length) all[(i + all.length) % all.length]?.focus();
  };
  const show = (at = 0) => {
    setOpen(true);
    requestAnimationFrame(() => focusAt(at));
  };
  const close = () => {
    setOpen(false);
    root.current?.querySelector<HTMLButtonElement>("button")?.focus();
  };
  const entry = (item: PageAction) => (
    <button
      key={item.label}
      type="button"
      role="menuitem"
      tabIndex={-1}
      onClick={() => {
        close();
        item.onSelect();
      }}
      className={`flex min-h-9 w-full items-center rounded-aui px-2.5 text-left text-aui-sm outline-none hover:bg-aui-hover focus-visible:bg-aui-hover ${item.destructive ? "text-aui-bad" : "text-aui-fg"}`}
    >
      {item.label}
    </button>
  );

  return (
    <span ref={root} className="relative inline-flex">
      <button
        type="button"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        className={textAction}
        onClick={() => (open ? close() : show())}
        onKeyDown={(e) => {
          if (e.key === "ArrowDown") {
            e.preventDefault();
            show();
          } else if (e.key === "ArrowUp") {
            e.preventDefault();
            show(-1);
          }
        }}
      >
        {label}
        <ChevronDownIcon size={14} />
      </button>
      {open && (
        <div
          ref={list}
          id={id}
          role="menu"
          aria-label={label}
          tabIndex={-1}
          style={shift ? { translate: `${shift}px 0` } : undefined}
          onKeyDown={(e) => {
            const i = entries().indexOf(document.activeElement as HTMLButtonElement);
            if (e.key === "ArrowDown") {
              e.preventDefault();
              focusAt(i + 1);
            } else if (e.key === "ArrowUp") {
              e.preventDefault();
              focusAt(i - 1);
            } else if (e.key === "Home") {
              e.preventDefault();
              focusAt(0);
            } else if (e.key === "End") {
              e.preventDefault();
              focusAt(-1);
            } else if (e.key === "Escape" || e.key === "Tab") {
              e.preventDefault();
              e.stopPropagation();
              close();
            }
          }}
          className="humagen-glass-pop absolute top-full right-0 z-40 mt-2 flex w-56 max-w-[calc(100vw-2rem)] flex-col rounded-aui-lg p-1.5"
        >
          {everyday.map(entry)}
          {everyday.length > 0 && destructive.length > 0 && <div role="separator" className="my-1 border-[var(--humagen-rule)] border-t" />}
          {destructive.map(entry)}
        </div>
      )}
    </span>
  );
}

/**
 * A button that opens a small panel. It closes on Escape, on a click outside,
 * and when focus leaves it, and returns focus to the button.
 */
export function Popover({
  label,
  button: trigger,
  className,
  align = "left",
  up = false,
  narrow = false,
  side,
  tooltip,
  children,
}: {
  label: string;
  button: ReactNode;
  className?: string;
  align?: "left" | "right";
  up?: boolean;
  /** A slimmer panel, for a button in a tight column such as the sidebar. */
  narrow?: boolean;
  /** A rail flyout opens alongside its trigger instead of above or below. */
  side?: "right";
  tooltip?: string;
  children: (close: () => void) => ReactNode;
}) {
  const [open, setOpen] = useState(false);
  const present = usePresence(open);
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const id = useId();
  // A flyout is placed by hand beside its trigger; anything else hangs from it and is kept inside the gutter.
  const shift = useInsideGutter(panel, root, open && side !== "right");

  useEffect(() => {
    if (!open) return;
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open]);

  const [flyout, setFlyout] = useState<{ left: number; top: number }>();
  useEffect(() => {
    if (side === "right" && open && present.mounted) {
      root.current?.querySelector<HTMLElement>('[role="dialog"] button, [role="dialog"] input')?.focus();
    }
  }, [side, open, present.mounted]);

  const triggerButton = (
      <button
        type="button"
        aria-label={label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => {
          if (side === "right") {
            const rect = root.current?.getBoundingClientRect();
            if (rect) setFlyout({ left: rect.right + 8, top: Math.max(16, Math.min(rect.top, window.innerHeight - 320)) });
          }
          setOpen(!open);
        }}
        className={className}
      >
        {trigger}
      </button>
  );
  return (
    <div
      ref={root}
      className="relative"
      onKeyDown={(e) => {
        if (e.key === "Escape" && open) {
          e.stopPropagation();
          setOpen(false);
          root.current?.querySelector<HTMLButtonElement>("button")?.focus();
        }
      }}
    >
      {tooltip ? <Tooltip label={tooltip}>{triggerButton}</Tooltip> : triggerButton}
      {present.mounted && (
        <div
          ref={panel}
          id={id}
          role="dialog"
          aria-label={label}
          data-leaving={present.leaving}
          style={
            {
              ...(side === "right" ? flyout : {}),
              ...(shift ? { translate: `${shift}px 0` } : {}),
              "--humagen-pop-origin": side === "right" ? "top left" : `${up ? "bottom" : "top"} ${align}`,
            } as CSSProperties
          }
          className={`humagen-glass-pop humagen-pop ${side === "right" ? "fixed" : "absolute"} z-40 max-w-[calc(100vw-2rem)] rounded-aui-lg p-1.5 ${narrow ? "w-56" : "w-80"} ${
            side === "right" ? "" : align === "right" ? "right-0" : "left-0"
          } ${side === "right" ? "" : up ? "bottom-full mb-2" : "top-full mt-2"}`}
        >
          {children(() => {
            setOpen(false);
            root.current?.querySelector<HTMLButtonElement>("button")?.focus();
          })}
        </div>
      )}
    </div>
  );
}

function SearchIcon({ size = 14 }: { size?: number }) {
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
    >
      <circle cx="11" cy="11" r="7" />
      <path d="m20 20-3.5-3.5" />
    </svg>
  );
}

/** A search box that filters what is on the page as a person types. */
export function SearchField({
  label,
  value,
  onChange,
  placeholder,
  hint,
  inputRef,
  className = "",
}: {
  label: string;
  value: string;
  onChange: (next: string) => void;
  placeholder?: string;
  /** A shortcut shown while the box is empty, such as ⌘K. */
  hint?: string;
  inputRef?: React.Ref<HTMLInputElement>;
  className?: string;
}) {
  return (
    <div
      className={`flex h-8 items-center gap-2 rounded-aui border border-[var(--humagen-rule)] bg-aui-bg px-2.5 text-aui-muted transition-colors focus-within:border-aui-accent focus-within:text-aui-fg ${className}`}
    >
      <SearchIcon />
      <input
        ref={inputRef}
        type="search"
        aria-label={label}
        value={value}
        placeholder={placeholder ?? label}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && value) {
            e.stopPropagation();
            onChange("");
          }
        }}
        className="min-w-0 flex-1 self-stretch bg-transparent text-aui-sm text-aui-fg outline-none placeholder:text-aui-muted [&::-webkit-search-cancel-button]:hidden"
      />
      {value ? (
        <button
          type="button"
          aria-label="Clear search"
          onClick={() => onChange("")}
          className="-mr-1 inline-flex size-5 items-center justify-center rounded-aui-sm text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
        >
          <XIcon size={12} />
        </button>
      ) : (
        hint && <kbd className="font-aui-sans text-aui-2xs text-aui-muted">{hint}</kbd>
      )}
    </div>
  );
}

/**
 * Keeps something on screen for as long as its leaving takes. `mounted` says
 * whether to render it, and `leaving` is true for the moment between being
 * closed and being gone: set it as data-leaving so the motion classes play out.
 * The timer is cleared if the thing reopens or the component goes away.
 */
export function usePresence(open: boolean, ms = 110): { mounted: boolean; leaving: boolean } {
  const [held, setHeld] = useState(open);
  useEffect(() => {
    if (open) {
      setHeld(true);
      return;
    }
    const timer = window.setTimeout(() => setHeld(false), ms);
    return () => window.clearTimeout(timer);
  }, [open, ms]);
  return { mounted: open || held, leaving: !open && held };
}

/** Renders its children while `when` is true and for `ms` after, telling them when they are leaving. */
export function Presence({ when, ms, children }: { when: boolean; ms?: number; children: (leaving: boolean) => ReactNode }) {
  const { mounted, leaving } = usePresence(when, ms);
  return mounted ? children(leaving) : null;
}

export type Flash = { text: string; bad: boolean };

/**
 * A brief confirmation that clears itself. `say("Copied")` shows for `ms` and
 * goes. `say(text, true)` is a failure: it stays until the next `say` or
 * `clear`, because a person needs time to read what went wrong. The timer is
 * cleared when the component goes away.
 */
export function useFlash(ms = 1600): { flash: Flash | null; say: (text: string, bad?: boolean) => void; clear: () => void } {
  const [flash, setFlash] = useState<Flash | null>(null);
  useEffect(() => {
    if (!flash || flash.bad) return;
    const timer = window.setTimeout(() => setFlash(null), ms);
    return () => window.clearTimeout(timer);
  }, [flash, ms]);
  return { flash, say: (text, bad = false) => setFlash({ text, bad }), clear: () => setFlash(null) };
}

/**
 * The note a `useFlash` shows, floating just above the control it belongs to.
 * It takes no room, so nothing beside the control moves, and the control keeps
 * its own label. Put it inside a `relative` wrapper. It is a live region, so
 * the confirmation is announced as well as seen.
 */
export function FlashNote({ flash, align = "center" }: { flash: Flash | null; align?: "center" | "left" | "right" }) {
  const present = usePresence(flash !== null);
  // Keep the words while the note leaves.
  const last = useRef<Flash | null>(flash);
  if (flash) last.current = flash;
  const shown = flash ?? last.current;
  const place = align === "center" ? "left-1/2 -translate-x-1/2" : align === "left" ? "left-0" : "right-0";
  return (
    <span role="status" aria-live="polite" className={`pointer-events-none absolute bottom-full z-50 mb-1.5 ${place}`}>
      {present.mounted && shown && (
        <span
          data-testid="flash-note"
          data-bad={shown.bad}
          data-leaving={present.leaving}
          className="humagen-note humagen-glass-pop flex items-center gap-1.5 whitespace-nowrap rounded-aui px-2 py-1 text-aui-xs text-aui-fg"
        >
          {/* A mark as well as the words, so a small notice beside a bare icon still reads as a result. */}
          {shown.bad ? <XIcon size={12} className="text-aui-bad" /> : <CheckIcon size={12} className="text-aui-ok" />}
          {shown.text}
        </span>
      )}
    </span>
  );
}

/**
 * Rows that stand in for a list while it loads, at the height the real rows
 * will have, so nothing jumps when they arrive. Says "Loading" to a screen
 * reader. The shimmer stops when motion is reduced.
 */
export function SkeletonRows({ count = 4, height = "2rem", className = "" }: { count?: number; height?: string; className?: string }) {
  return (
    <div role="status" data-testid="skeleton" className={`flex flex-col gap-2 py-2 ${className}`}>
      <span className="sr-only">Loading</span>
      {Array.from({ length: count }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: placeholders have no identity
        <span key={i} aria-hidden className="aui-skeleton block" style={{ height, width: `${92 - ((i * 17) % 40)}%` }} />
      ))}
    </div>
  );
}

export type Option<T extends string = string> = {
  value: T;
  label: string;
  /** A second line that says what choosing this does. */
  hint?: string;
  /** Drawn before the label, such as an agent's mark. */
  mark?: ReactNode;
};

/**
 * A menu of choices, used wherever a person picks one thing from a list. It
 * opens toward the side with more room, moves with the arrow keys, jumps to a
 * choice as its name is typed, and grows a filter when the list is long.
 */
export function Menu<T extends string>({
  label,
  value,
  options,
  onChange,
  disabled,
  quiet,
  shows,
  header,
  align = "left",
  className = "",
}: {
  label: string;
  value: T;
  /** A line above the choices that says what they act on. */
  header?: ReactNode;
  options: Option<T>[];
  onChange: (next: T) => void;
  disabled?: boolean;
  /** Text only, for a menu that sits in a line of other controls. */
  quiet?: boolean;
  /** What the button says, when it is not the chosen option's label. */
  shows?: ReactNode;
  align?: "left" | "right";
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const present = usePresence(open);
  const [up, setUp] = useState(false);
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const root = useRef<HTMLDivElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const filter = useRef<HTMLInputElement>(null);
  const typed = useRef({ text: "", at: 0 });
  const id = useId();
  const shift = useInsideGutter(panel, root, open);

  const searchable = options.length > 8;
  const q = query.trim().toLowerCase();
  const shown = q ? options.filter((o) => `${o.label} ${o.hint ?? ""}`.toLowerCase().includes(q)) : options;
  const chosen = options.find((o) => o.value === value);

  const show = () => {
    const box = root.current?.getBoundingClientRect();
    if (box) {
      const below = window.innerHeight - box.bottom;
      setUp(below < 300 && box.top > below);
    }
    setQuery("");
    setActive(Math.max(options.findIndex((o) => o.value === value), 0));
    setOpen(true);
  };
  const hide = (refocus = true) => {
    setOpen(false);
    if (refocus) root.current?.querySelector<HTMLButtonElement>("button")?.focus();
  };
  const pick = (o: Option<T> | undefined) => {
    if (!o) return;
    hide();
    if (o.value !== value || shows !== undefined) onChange(o.value);
  };

  useEffect(() => {
    if (!open) return;
    (searchable ? filter.current : list.current)?.focus();
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [open, searchable]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: scroll follows the highlighted row
  useEffect(() => {
    if (open) list.current?.querySelector('[data-active="true"]')?.scrollIntoView({ block: "nearest" });
  }, [open, active]);

  const onKey = (e: React.KeyboardEvent) => {
    const last = shown.length - 1;
    if (e.key === "ArrowDown") setActive((a) => Math.min(a + 1, last));
    else if (e.key === "ArrowUp") setActive((a) => Math.max(a - 1, 0));
    else if (e.key === "Home") setActive(0);
    else if (e.key === "End") setActive(last);
    else if (e.key === "Enter" || (e.key === " " && !searchable)) pick(shown[active]);
    else if (e.key === "Escape") {
      e.stopPropagation();
      hide();
    } else if (e.key === "Tab") {
      hide(false);
      return;
    } else {
      // Typing a name jumps to it, as a native menu does.
      if (!searchable && e.key.length === 1 && !e.metaKey && !e.ctrlKey) {
        const now = Date.now();
        typed.current = { text: (now - typed.current.at > 600 ? "" : typed.current.text) + e.key.toLowerCase(), at: now };
        const hit = shown.findIndex((o) => o.label.toLowerCase().startsWith(typed.current.text));
        if (hit >= 0) setActive(hit);
      }
      return;
    }
    e.preventDefault();
  };

  return (
    <div ref={root} className={`relative ${quiet ? "inline-flex" : "flex"} ${className}`}>
      <button
        type="button"
        aria-label={`${label}: ${chosen?.label ?? "none"}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        disabled={disabled}
        onClick={() => (open ? hide() : show())}
        onKeyDown={(e) => {
          if (!open && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
            e.preventDefault();
            show();
          }
        }}
        className={
          quiet
            ? "inline-flex h-7 max-w-60 items-center gap-1 rounded-aui px-1.5 text-aui-sm text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:cursor-not-allowed disabled:opacity-50 aria-expanded:bg-aui-hover aria-expanded:text-aui-fg"
            : "flex h-8 w-full min-w-0 items-center gap-2 rounded-aui border border-aui-border bg-aui-bg px-2.5 text-left text-aui-sm text-aui-fg transition-colors hover:border-aui-ring focus-visible:border-aui-accent focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-aui-ring disabled:cursor-not-allowed disabled:opacity-50 aria-expanded:border-aui-accent"
        }
      >
        {shows === undefined && chosen?.mark}
        <span className="min-w-0 flex-1 truncate">{shows ?? chosen?.label ?? "Choose"}</span>
        <ChevronDownIcon size={14} className={`text-aui-muted transition-transform motion-reduce:transition-none ${open ? "rotate-180" : ""}`} />
      </button>
      {present.mounted && (
        // biome-ignore lint/a11y/noStaticElementInteractions: keys are handled here for the filter and the list together
        <div
          ref={panel}
          onKeyDown={onKey}
          data-leaving={present.leaving}
          style={{ ...(shift ? { translate: `${shift}px 0` } : {}), "--humagen-pop-origin": `${up ? "bottom" : "top"} ${align}` } as CSSProperties}
          className={`humagen-glass-pop humagen-pop absolute z-40 flex max-h-80 min-w-full max-w-[min(22rem,calc(100vw-2rem))] flex-col rounded-aui-lg p-1 ${
            quiet ? "w-max min-w-44" : ""
          } ${align === "right" ? "right-0" : "left-0"} ${up ? "bottom-full mb-1.5" : "top-full mt-1.5"}`}
        >
          {header && <div className="shrink-0 border-[var(--humagen-rule)] border-b px-2.5 pt-1.5 pb-2 text-aui-xs text-aui-muted">{header}</div>}
          {searchable && (
            <SearchField
              label={`Filter ${label.toLowerCase()}`}
              placeholder="Filter"
              value={query}
              inputRef={filter}
              onChange={(next) => {
                setQuery(next);
                setActive(0);
              }}
              className="mb-1 shrink-0"
            />
          )}
          <ul
            ref={list}
            id={id}
            role="listbox"
            aria-label={label}
            aria-activedescendant={shown[active] ? `${id}-${active}` : undefined}
            tabIndex={-1}
            className="aui-scroll min-h-0 overflow-y-auto outline-none"
          >
            {shown.length === 0 && <li className="px-2.5 py-2 text-aui-sm text-aui-muted">Nothing matches.</li>}
            {shown.map((o, i) => (
              // biome-ignore lint/a11y/useKeyWithClickEvents: the listbox handles keys for its options
              <li
                key={o.value}
                id={`${id}-${i}`}
                role="option"
                aria-selected={o.value === value}
                data-active={i === active}
                onMouseMove={() => setActive(i)}
                onClick={() => pick(o)}
                className="flex cursor-pointer items-center gap-2.5 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg data-[active=true]:bg-aui-hover"
              >
                {o.mark}
                <span className="min-w-0 flex-1">
                  <span className="block truncate">{o.label}</span>
                  {o.hint && <span className="block text-aui-xs text-aui-muted">{o.hint}</span>}
                </span>
                {o.value === value && shows === undefined && <CheckIcon size={14} className="text-aui-fg" />}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

/** The choices for a menu of agents, each with its mark. */
export function agentOptions(names: string[], none?: string): Option[] {
  const list: Option[] = names.map((n) => ({ value: n, label: n, mark: <AgentMark name={n} size={16} /> }));
  return none === undefined ? list : [{ value: "", label: none }, ...list];
}

export type StrokeState = "idle" | "working" | "waiting" | "failed" | "done";

/**
 * A surface known by its edge. With no `agent` the edge is the neutral rule,
 * which is right for anything a person or the system made. With an `agent` the
 * edge takes that agent's hue, and `state` says how the agent stands: working
 * breathes, waiting on a person is dashed and still, failed is red.
 */
export function Stroke({
  agent,
  state = "idle",
  edge = "start",
  className = "",
  testId = "stroke",
  children,
}: {
  /** The agent that made or owns what is inside. Leave out for people and the system. */
  agent?: string;
  state?: StrokeState;
  /** Where the line is drawn: the leading side, underneath, or all the way around. */
  edge?: "start" | "under" | "around";
  className?: string;
  testId?: string;
  children: ReactNode;
}) {
  return (
    <div
      data-testid={testId}
      data-agent={agent}
      data-state={agent ? state : undefined}
      data-edge={edge}
      className={`humagen-stroke ${className}`}
      style={agent ? ({ "--humagen-hue": hueOf(agent) } as CSSProperties) : undefined}
    >
      {children}
    </div>
  );
}

/**
 * Whether a run is at work: accepted or running. One waiting on a person is
 * not, so nothing about it animates; nor is one that has ended.
 */
export function isWorking(status: Run["status"]): boolean {
  return strokeOf(status) === "working";
}

/** Which stroke state a run's status is. */
export function strokeOf(status: Run["status"]): StrokeState {
  // A stopped run is at rest: it is neither working nor a failure.
  return status === "failed" ? "failed" : status === "parked" ? "waiting" : status === "completed" ? "done" : status === "stopped" ? "idle" : "working";
}

export { dollars };

export type Period = "day" | "month";
export type UsageState = "none" | "under" | "warning" | "over";

/** Where spending stands against a limit. Warning starts at `warnAt`, a share of the limit. */
export function usageOf(spent: number, limit: number | null | undefined, warnAt = 0.8): UsageState {
  if (!limit || limit <= 0) return "none";
  return spent > limit ? "over" : spent >= limit * warnAt ? "warning" : "under";
}

const usageWord: Record<UsageState, string> = {
  none: "No limit",
  under: "Under limit",
  warning: "Near limit",
  over: "Over limit",
};

/** A formatter for a meter that counts things: `counted("runs")(12)` is "12 runs". */
export function counted(unit: string): (amount: number) => string {
  return (amount) => `${amount.toLocaleString()} ${unit}`;
}

/**
 * Use against a limit. The state is always written out, and going over is
 * drawn past the limit line, so nothing depends on color. Amounts are
 * micro-USD unless `format` says otherwise. Presentational: it fetches nothing.
 */
export function UsageMeter({
  label,
  spent,
  limit,
  warnAt = 0.8,
  projected,
  period,
  size = "row",
  labelled = false,
  format: show = dollars,
  testId = "usage-meter",
}: {
  /** What is being measured, such as a project's name. Read out, and shown at the header size. */
  label: string;
  spent: number;
  /** Leave out, or pass null or 0, when nothing caps the spending. */
  limit?: number | null;
  /** The share of the limit where the warning starts. */
  warnAt?: number;
  /** What the period is expected to end at, drawn as a lighter extension. */
  projected?: number;
  period?: Period;
  /** `row` fits a table cell. `header` leads a page. `mini` is the track alone, to sit inside another control. */
  size?: "row" | "header" | "mini";
  /** Show the label above a row-size meter. The header size always shows it. */
  labelled?: boolean;
  /** How an amount is written. Dollars from micro-USD unless given. Use `counted("runs")` for a plain count. */
  format?: (amount: number) => string;
  testId?: string;
}) {
  const state = usageOf(spent, limit, warnAt);
  const cap = limit && limit > 0 ? limit : 0;
  const ahead = projected !== undefined && projected > spent ? projected : 0;
  const scale = Math.max(cap, spent, ahead, 1);
  const pct = (v: number) => `${Math.min((100 * v) / scale, 100)}%`;
  const share = cap ? Math.round((100 * spent) / cap) : null;
  // Exactly at the limit is still the warning state, but "near" would be wrong.
  const word = cap > 0 && spent === cap ? "At limit" : usageWord[state];
  const each = period ? ` a ${period}` : "";
  const text = cap
    ? `${show(spent)} of ${show(cap)}${each}, ${share}%. ${state === "over" ? `Over limit by ${show(spent - cap)}` : word}.${
        ahead ? ` Heading for ${show(ahead)}.` : ""
      }`
    : `${show(spent)} used. No limit.`;
  const note =
    state === "over" ? `Over by ${show(spent - cap)}` : ahead && cap && ahead > cap ? `Heading for ${show(ahead)}` : word;
  const header = size === "header";

  if (size === "mini") {
    return (
      <span
        data-testid={testId}
        data-usage={state}
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={cap || Math.max(spent, 1)}
        aria-valuenow={Math.min(spent, cap || spent)}
        aria-valuetext={text}
        title={text}
        className="humagen-meter-track inline-block w-10 shrink-0"
        style={{ "--humagen-meter-h": "4px" } as CSSProperties}
      >
        {cap > 0 && <span className="humagen-meter-fill" style={{ width: pct(spent) }} />}
      </span>
    );
  }

  return (
    <div data-testid={testId} data-usage={state} className={`flex min-w-0 flex-col ${header ? "gap-2" : "gap-1"}`}>
      {header && (
        <div className="flex flex-wrap items-baseline justify-between gap-x-4">
          <span className="text-aui-xs text-aui-muted">{label}</span>
          <span className="text-aui-xs text-aui-muted">
            {cap ? `of ${show(cap)}${each}` : "No limit"}
          </span>
        </div>
      )}
      {!header && labelled && <span className="truncate text-aui-xs text-aui-muted">{label}</span>}
      <div className="flex items-baseline justify-between gap-3">
        <span className={`humagen-display min-w-0 truncate font-semibold text-aui-fg ${header ? "text-[1.75rem] leading-9" : "text-aui-sm"}`}>
          {show(spent)}
          {!header && cap > 0 && <span className="font-normal text-aui-muted"> of {show(cap)}</span>}
        </span>
        <span className={`shrink-0 text-aui-xs ${state === "over" || state === "warning" ? "font-medium text-aui-fg" : "text-aui-muted"}`}>
          {header && !cap ? "" : note}
        </span>
      </div>
      <div
        role="meter"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={cap || Math.max(spent, 1)}
        aria-valuenow={Math.min(spent, cap || spent)}
        aria-valuetext={text}
        className="humagen-meter-track"
        style={header ? ({ "--humagen-meter-h": "10px" } as CSSProperties) : undefined}
      >
        {ahead > 0 && <span data-testid={`${testId}-projected`} className="humagen-meter-projected" style={{ width: pct(ahead) }} />}
        {/* With no limit there is nothing to fill toward, so the track stays empty. */}
        {cap > 0 && <span className="humagen-meter-fill" style={{ width: pct(spent) }} />}
        {cap > 0 && warnAt > 0 && warnAt < 1 && <span aria-hidden className="humagen-meter-warn" style={{ left: pct(cap * warnAt) }} />}
        {cap > 0 && scale > cap && <span aria-hidden className="humagen-meter-limit" style={{ left: pct(cap) }} />}
      </div>
    </div>
  );
}

const periods: Option<Period>[] = [
  { value: "month", label: "a month" },
  { value: "day", label: "a day" },
];

/** Dollars typed by a person, as micro-USD. Null when empty, undefined when it is not an amount. */
export function parseDollars(text: string): number | null | undefined {
  const t = text.trim().replace(/^\$/, "").replace(/,/g, "");
  if (t === "") return null;
  if (!/^\d+(\.\d{0,2})?$|^\.\d{1,2}$/.test(t)) return undefined;
  return Math.round(Number(t) * 1_000_000);
}

/**
 * A spending limit: dollars, and the period they cover. The value is micro-USD,
 * or null for no limit of its own. When it is null and `inherits` is given, the
 * field says which limit applies instead.
 */
export function LimitField({
  label,
  value,
  period,
  onChange,
  inherits,
  disabled,
  testId = "limit-field",
}: {
  label: string;
  /** Micro-USD, or null when this scope sets no limit. */
  value: number | null;
  period: Period;
  onChange: (value: number | null, period: Period) => void;
  /** The limit that applies when this one is empty, and where it comes from. */
  inherits?: { scope: string; value: number; period: Period };
  disabled?: boolean;
  testId?: string;
}) {
  const shown = value === null ? "" : (value / 1_000_000).toFixed(2).replace(/\.00$/, "");
  const [text, setText] = useState(shown);
  const [bad, setBad] = useState(false);
  const id = useId();

  // Follow the value when it changes from outside, but not while it is being typed.
  const last = useRef(value);
  useEffect(() => {
    if (last.current !== value) {
      last.current = value;
      setText(shown);
      setBad(false);
    }
  }, [value, shown]);

  const type = (next: string) => {
    setText(next);
    const parsed = parseDollars(next);
    setBad(parsed === undefined);
    if (parsed !== undefined) {
      last.current = parsed;
      onChange(parsed, period);
    }
  };

  const hint = bad
    ? "Enter an amount in dollars, such as 250 or 49.50."
    : value !== null
      ? undefined
      : inherits
        ? `Inherits ${dollars(inherits.value)} a ${inherits.period} from ${inherits.scope}.`
        : "No limit.";

  return (
    <div data-testid={testId} data-empty={value === null} className="flex flex-col gap-1">
      <label htmlFor={id} className="text-aui-xs font-medium text-aui-muted">
        {label}
      </label>
      <div className="flex items-center gap-2">
        <div
          className={`flex h-8 min-w-0 flex-1 items-center gap-1 rounded-aui border bg-aui-bg px-2.5 transition-colors focus-within:border-aui-accent focus-within:ring-2 focus-within:ring-aui-ring ${
            bad ? "border-aui-bad" : "border-aui-border"
          } ${disabled ? "opacity-50" : ""}`}
        >
          <span aria-hidden className="text-aui-sm text-aui-muted">
            $
          </span>
          <input
            id={id}
            inputMode="decimal"
            autoComplete="off"
            disabled={disabled}
            value={text}
            placeholder={inherits ? (inherits.value / 1_000_000).toFixed(2).replace(/\.00$/, "") : "No limit"}
            aria-invalid={bad}
            aria-describedby={hint ? `${id}-hint` : undefined}
            onChange={(e) => type(e.target.value)}
            className="min-w-0 flex-1 self-stretch bg-transparent text-aui-sm tabular-nums text-aui-fg outline-none placeholder:text-aui-muted disabled:cursor-not-allowed"
          />
        </div>
        <Menu label={`${label} period`} className="w-28 shrink-0" value={period} options={periods} disabled={disabled} onChange={(p) => onChange(value, p)} />
      </div>
      {hint && (
        <span id={`${id}-hint`} role={bad ? "alert" : undefined} className="text-aui-2xs text-aui-muted">
          {hint}
        </span>
      )}
    </div>
  );
}

export type PolicyAction = "block" | "approve";

const actionWord: Record<PolicyAction, string> = {
  block: "Blocks at the limit",
  approve: "Asks for approval",
};

/** What a policy covers and what it does when its limit is reached. */
export function PolicyBadge({
  scope,
  name,
  action,
  testId = "policy-badge",
}: {
  /** The kind of thing the policy covers, such as Workspace, Project or Agent. */
  scope: string;
  /** Which one, when the scope has many. */
  name?: string;
  action: PolicyAction;
  testId?: string;
}) {
  const Icon = action === "block" ? BanIcon : ShieldIcon;
  return (
    <span
      data-testid={testId}
      data-action={action}
      className="inline-flex h-6 max-w-full items-center gap-1.5 rounded-full border border-[var(--humagen-stroke)] pr-2.5 pl-2 text-aui-xs text-aui-muted"
    >
      <Icon size={12} />
      <span className="truncate">
        <span className="text-aui-fg">{name ? `${scope} ${name}` : scope}</span>, {actionWord[action].toLowerCase()}
      </span>
    </span>
  );
}
