import { defaultCopy } from "@humagen/components";
import { type ReactNode, useCallback, useContext, useEffect, useId, useRef, useState } from "react";
import { ApiError, api, type ScopeUsage, type ThreadUsage } from "./api.ts";
import { NoticeSlotContext } from "./Composer.tsx";
import { usd } from "./format.ts";
import { PaneMenu } from "./PaneMenu.tsx";
import { href } from "./route.ts";
import { AgentMark, composerIconControl } from "./ui.tsx";

/*
 * The quiet row under a conversation's composer: the agent answering (and
 * its model), the approval mode, and a usage ring that opens what the session
 * has used and the limits that hold it. The ring shows the most constraining
 * number, the context's fill or a limit's, and takes a color only when that
 * number is near its end.
 */

const words = defaultCopy.dock.usage;
/** How full a context gets before the ring says so. */
const CONTEXT_NEAR = 0.8;

export interface LimitRow {
  key: string;
  /** "Workspace", "Project Launch", "Agent lead": whom it holds. */
  scope: string;
  /** "Monthly tokens". */
  label: string;
  fraction: number;
  used: string;
  resets: string;
  near: boolean;
  over: boolean;
  action: ScopeUsage["action"];
}

const MEASURES = ["tokens", "spend", "runs", "concurrent"] as const;

function count(n: number): string {
  return new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 }).format(
    n,
  );
}

/** "Tue 5:00 AM", in the reader's own time. */
export function resetTime(iso: string): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  return new Intl.DateTimeFormat(undefined, {
    weekday: "short",
    hour: "numeric",
    minute: "2-digit",
  }).format(at);
}

function scopeName(s: ScopeUsage["scope"]): string {
  const kind = words.scope[s.kind];
  return s.name ? `${kind} ${s.name}` : kind;
}

/**
 * The limits that hold this session: the workspace's, its project's and the
 * projects above it, and those of the agents in it. Each measure with a limit
 * is a row, the fullest first.
 */
export function limitsFor(
  scopes: ScopeUsage[],
  where: { project?: string; agents: string[] },
): LimitRow[] {
  const project = where.project ?? "";
  const holds = (s: ScopeUsage["scope"]) =>
    s.kind === "tenant" ||
    (s.kind === "project" &&
      !!s.name &&
      (project === s.name || project.startsWith(`${s.name}/`))) ||
    (s.kind === "agent" && !!s.name && where.agents.includes(s.name));
  const rows: LimitRow[] = [];
  for (const s of scopes) {
    if (!holds(s.scope)) continue;
    const warn = (s.warn_at || 80) / 100;
    for (const m of MEASURES) {
      const measure = s[m];
      if (!measure || !(measure.limit > 0)) continue;
      const fraction = measure.used / measure.limit;
      rows.push({
        key: `${s.scope.kind}:${s.scope.name ?? ""}:${m}`,
        scope: scopeName(s.scope),
        label: `${words.period[s.period]} ${words.measure[m].toLowerCase()}`,
        fraction,
        used:
          m === "spend"
            ? `${usd(measure.used)} of ${usd(measure.limit)}`
            : `${count(measure.used)} of ${count(measure.limit)}`,
        resets: resetTime(s.period_end),
        near: fraction >= warn,
        over: fraction >= 1,
        action: s.action,
      });
    }
  }
  return rows.sort((a, b) => b.fraction - a.fraction);
}

/** The ring's one number: the fullest of the context and every limit, and what it is. */
export function ringOf(
  usage: ThreadUsage | null,
  limits: LimitRow[],
): { what: string; fraction: number; near: boolean; over: boolean } | null {
  const c = usage?.context;
  const context =
    c?.window && c.tokens !== undefined
      ? {
          what: words.context,
          fraction: c.tokens / c.window,
          near: c.tokens / c.window >= CONTEXT_NEAR,
          over: c.tokens >= c.window,
        }
      : null;
  const top = limits[0]
    ? {
        what: `${limits[0].scope} ${limits[0].label}`,
        fraction: limits[0].fraction,
        near: limits[0].near,
        over: limits[0].over,
      }
    : null;
  // A limit near its end says so before the context does; otherwise the fuller one.
  if (top && (top.near || !context || top.fraction > context.fraction)) return top;
  return context;
}

type Read<T> = { state: "loading" } | { state: "failed" } | { state: "ok"; value: T };

/** Reads the session's usage and the limits the reader may see, again when `version` changes. */
export function useSessionUsage(
  thread: string,
  version: unknown,
): {
  usage: Read<ThreadUsage>;
  limits: Read<ScopeUsage[]> | { state: "hidden" };
  reload: () => void;
} {
  const [usage, setUsage] = useState<Read<ThreadUsage>>({ state: "loading" });
  const [limits, setLimits] = useState<Read<ScopeUsage[]> | { state: "hidden" }>({
    state: "loading",
  });
  const [attempt, setAttempt] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again as the session moves on
  useEffect(() => {
    let live = true;
    api
      .threadUsage(thread)
      .then((value) => live && setUsage({ state: "ok", value }))
      .catch(() => live && setUsage({ state: "failed" }));
    api
      .usage()
      .then((value) => live && setLimits({ state: "ok", value }))
      .catch(
        (e) =>
          live &&
          setLimits(
            e instanceof ApiError && e.status === 403 ? { state: "hidden" } : { state: "failed" },
          ),
      );
    return () => {
      live = false;
    };
  }, [thread, version, attempt]);
  const reload = useCallback(() => {
    setUsage({ state: "loading" });
    setLimits({ state: "loading" });
    setAttempt((n) => n + 1);
  }, []);
  return { usage, limits, reload };
}

function Ring({ fraction, tone }: { fraction: number; tone: "plain" | "near" | "over" }) {
  const r = 6.5;
  const around = 2 * Math.PI * r;
  const filled = Math.max(0, Math.min(1, fraction)) * around;
  return (
    <svg aria-hidden width="18" height="18" viewBox="0 0 16 16" className="shrink-0 -rotate-90">
      <circle
        cx="8"
        cy="8"
        r={r}
        fill="none"
        stroke="currentColor"
        strokeOpacity="0.25"
        strokeWidth="2"
      />
      <circle
        cx="8"
        cy="8"
        r={r}
        fill="none"
        strokeWidth="2"
        strokeLinecap="round"
        strokeDasharray={`${filled} ${around}`}
        className={
          tone === "over"
            ? "stroke-aui-bad"
            : tone === "near"
              ? "stroke-aui-warn"
              : "stroke-current"
        }
      />
    </svg>
  );
}

function Bar({ fraction, tone }: { fraction: number; tone: "plain" | "near" | "over" }) {
  return (
    <span aria-hidden className="block h-1 w-full overflow-hidden rounded-full bg-aui-hover">
      <span
        className={`block h-full rounded-full ${tone === "over" ? "bg-aui-bad" : tone === "near" ? "bg-aui-warn" : "bg-aui-fg/50"}`}
        style={{ width: `${Math.max(2, Math.min(100, fraction * 100))}%` }}
      />
    </span>
  );
}

const toneOf = (x: { near: boolean; over: boolean }) =>
  x.over ? "over" : x.near ? "near" : "plain";
const pct = (f: number) => Math.round(f * 100);

/** What the session used: context, spend or tokens, and the agents that used most. */
function SessionSection({ usage }: { usage: Read<ThreadUsage> }) {
  if (usage.state === "loading") return <Skeleton rows={2} />;
  if (usage.state === "failed") return null;
  const u = usage.value;
  const tokens = (u.usage.prompt_tokens ?? 0) + (u.usage.completion_tokens ?? 0);
  const cost = u.usage.cost?.currency === "USD" ? u.usage.cost.micros : 0;
  const c = u.context;
  const top = u.agents.slice(0, 2);
  if (u.runs === 0 || (!tokens && !c))
    return <p className="text-aui-xs text-aui-muted">{words.nothingYet}</p>;
  return (
    <div className="flex flex-col gap-2.5">
      {c?.window && c.tokens !== undefined && (
        <div className="flex flex-col gap-1">
          <p className="flex items-baseline justify-between gap-3 text-aui-xs">
            <span className="text-aui-fg">
              {words.context}
              {c.model && <span className="pl-2 text-aui-muted">{c.model}</span>}
            </span>
            <span className="flex gap-3 tabular-nums text-aui-muted">
              <span>{words.contextUsed(count(c.tokens), count(c.window))}</span>
              <span className="text-aui-fg">{pct(c.tokens / c.window)}%</span>
            </span>
          </p>
          <Bar
            fraction={c.tokens / c.window}
            tone={toneOf({ near: c.tokens / c.window >= CONTEXT_NEAR, over: c.tokens >= c.window })}
          />
        </div>
      )}
      <p className="flex items-baseline justify-between gap-3 text-aui-xs">
        <span className="text-aui-fg">{cost ? words.spent : words.tokens}</span>
        <span className="flex gap-3 tabular-nums text-aui-muted">
          <span>{cost ? usd(cost) : words.tokenCount(count(tokens))}</span>
          <span>{words.runs(u.runs)}</span>
        </span>
      </p>
      {top.length > 0 && (
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-aui-xs text-aui-muted">
          <span>{words.topAgents}</span>
          {top.map((a) => (
            <span key={a.agent} className="inline-flex items-center gap-1 text-aui-fg">
              <AgentMark name={a.agent} size={14} />
              {a.agent}
              <span className="tabular-nums text-aui-muted">
                {a.usage.cost?.currency === "USD" && a.usage.cost.micros
                  ? usd(a.usage.cost.micros)
                  : count((a.usage.prompt_tokens ?? 0) + (a.usage.completion_tokens ?? 0))}
              </span>
            </span>
          ))}
        </p>
      )}
      {u.truncated && <p className="text-aui-2xs text-aui-muted">{words.atLeast}</p>}
    </div>
  );
}

function Skeleton({ rows }: { rows: number }) {
  return (
    <div role="status" aria-label={words.loading} className="flex flex-col gap-2.5">
      {Array.from({ length: rows }, (_, i) => `row-${i}`).map((k) => (
        <span key={k} aria-hidden className="flex flex-col gap-1">
          <span className="aui-skeleton block h-3 w-2/3" />
          <span className="aui-skeleton block h-1 w-full" />
        </span>
      ))}
    </div>
  );
}

function LimitsSection({
  limits,
  rows,
}: {
  limits: Read<ScopeUsage[]> | { state: "hidden" };
  rows: LimitRow[];
}) {
  if (limits.state === "loading") return <Skeleton rows={1} />;
  if (limits.state === "hidden")
    return <p className="text-aui-xs text-aui-muted">{words.limitsHidden}</p>;
  if (limits.state === "failed") return null;
  if (rows.length === 0) return <p className="text-aui-xs text-aui-muted">{words.noLimits}</p>;
  return (
    <ul className="flex flex-col gap-3">
      {rows.map((row) => (
        <li
          key={row.key}
          className="flex flex-col gap-1"
          data-limit={row.over ? "over" : row.near ? "near" : "ok"}
        >
          <p className="flex items-baseline justify-between gap-3 text-aui-xs">
            <span className="min-w-0 truncate text-aui-fg">{row.label}</span>
            <span className="shrink-0 tabular-nums text-aui-fg">{pct(row.fraction)}%</span>
          </p>
          <Bar fraction={row.fraction} tone={toneOf(row)} />
          <p className="flex flex-wrap items-baseline justify-between gap-x-3 text-aui-2xs text-aui-muted">
            <span className="min-w-0 truncate">{row.scope}</span>
            <span className="shrink-0">{words.resets(row.resets)}</span>
          </p>
          {row.over && (
            <p role="alert" className="text-aui-xs text-aui-bad">
              {(row.action === "approve" ? words.overApprove : words.over)(
                `${row.scope} ${row.label.toLowerCase()}`,
                row.resets,
              )}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}

/**
 * The ring and what it opens. The popover sits over the composer (the
 * popover layer); on a phone it is a sheet along the bottom. Esc, a press
 * outside and Close put it away and give focus back to the ring.
 */
export function UsageRing({
  thread,
  project,
  agents,
  version,
}: {
  thread: string;
  project?: string;
  agents: string[];
  /** Changes as the session moves on, to read again. */
  version: unknown;
}) {
  const { usage, limits, reload } = useSessionUsage(thread, version);
  const [open, setOpen] = useState(false);
  const ring = useRef<HTMLButtonElement>(null);
  const id = useId();
  const rows = limits.state === "ok" ? limitsFor(limits.value, { project, agents }) : [];
  const shown = ringOf(usage.state === "ok" ? usage.value : null, rows);
  const failed = usage.state === "failed" && limits.state === "failed";
  const close = useCallback((refocus = true) => {
    setOpen(false);
    if (refocus) ring.current?.focus();
  }, []);
  const leave = useCallback(() => close(false), [close]);
  const label = shown ? words.ring(shown.what, pct(shown.fraction)) : words.ringIdle;
  return (
    <span className="relative inline-flex">
      <button
        ref={ring}
        type="button"
        data-layer="usage-ring"
        data-tone={shown ? toneOf(shown) : "plain"}
        aria-label={label}
        title={shown?.near ? words.near(shown.what) : label}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onClick={() => (open ? close() : setOpen(true))}
        className={`${composerIconControl} ${shown?.over ? "text-aui-bad" : shown?.near ? "text-aui-warn" : "text-aui-muted"}`}
      >
        <Ring fraction={shown?.fraction ?? 0} tone={shown ? toneOf(shown) : "plain"} />
      </button>
      {open && (
        <PaneMenu
          sheet
          anchor={ring}
          id={id}
          label={words.title}
          data-layer="usage-popover"
          onClose={close}
          onLeave={leave}
          className="p-4 sm:right-0 sm:bottom-full sm:mb-2 sm:w-80"
        >
          <div className="flex flex-col gap-4">
            <header className="flex items-center justify-between gap-3">
              <h2 className="text-aui-sm font-medium text-aui-fg">{words.title}</h2>
              <span className="flex items-center gap-1">
                <a
                  href={href({ page: "usage" })}
                  className="rounded-aui px-1.5 py-0.5 text-aui-xs text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  {words.seeAll} <span aria-hidden>→</span>
                </a>
                <button
                  type="button"
                  aria-label={words.close}
                  onClick={() => close()}
                  className="inline-flex size-7 pointer-coarse:size-11 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  <span aria-hidden>×</span>
                </button>
              </span>
            </header>
            {failed ? (
              <p className="flex items-center justify-between gap-3 text-aui-xs text-aui-muted">
                {words.failed}
                <button
                  type="button"
                  onClick={reload}
                  className="rounded-aui px-1.5 py-0.5 text-aui-fg underline hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
                >
                  {words.retry}
                </button>
              </p>
            ) : (
              <>
                <Section title={words.session}>
                  <SessionSection usage={usage} />
                </Section>
                <Section title={words.limits}>
                  <LimitsSection limits={limits} rows={rows} />
                </Section>
              </>
            )}
          </div>
        </PaneMenu>
      )}
    </span>
  );
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="flex flex-col gap-2">
      <h3 className="text-aui-2xs font-medium text-aui-muted uppercase tracking-wide">{title}</h3>
      {children}
    </section>
  );
}

/**
 * The row under the composer: on the left where the session runs and its
 * approval mode, in the middle the AI notice, on the right who answers (and
 * on which model) and the usage ring. One line; where the composer is too
 * narrow for all three, the notice is a line of its own under the row.
 */
export function ComposerFooter({
  start,
  agent,
  model,
  approval,
  usage,
}: {
  /**
   * The left of the row: where the session runs, such as an environment and a
   * repository picker. Empty, the rest stays at the right. On a phone each
   * item keeps its icon: mark its words with `data-label` and they are hidden.
   */
  start?: ReactNode;
  agent?: string;
  model?: string;
  approval?: ReactNode;
  usage?: ReactNode;
}) {
  const notice = useContext(NoticeSlotContext);
  return (
    <div
      data-layer="composer-footer"
      aria-label={defaultCopy.dock.footer}
      role="group"
      className="humagen-composer-footer min-h-7 min-w-0 items-center gap-x-3 px-2 text-aui-xs text-aui-muted"
    >
      <div
        data-layer="composer-footer-start"
        className="flex min-w-0 items-center gap-3 max-[30rem]:[&_[data-label]]:sr-only"
      >
        {start}
      </div>
      {/* The composer puts its AI notice here. */}
      {notice && <div ref={notice} data-layer="composer-footer-notice" className="min-w-0" />}
      <div data-layer="composer-footer-end" className="flex min-w-0 items-center gap-3">
        {agent && (
          <span
            className="inline-flex min-w-0 items-center gap-1.5"
            title={defaultCopy.dock.answeredBy(agent)}
          >
            <AgentMark name={agent} size={14} />
            <span className="truncate text-aui-fg">{agent}</span>
            {model && <span className="truncate max-[40rem]:hidden">{model}</span>}
          </span>
        )}
        {approval && <span className="inline-flex max-[30rem]:hidden">{approval}</span>}
        {approval && <MoreControls>{approval}</MoreControls>}
        {usage}
      </div>
    </div>
  );
}

/** On a phone, the approval mode waits under ⋯, so the row stays the agent and the ring. */
function MoreControls({ children }: { children: ReactNode }) {
  const [open, setOpen] = useState(false);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const down = (e: PointerEvent) => {
      const t = e.target as Node;
      if (!panel.current?.contains(t) && !button.current?.contains(t)) setOpen(false);
    };
    document.addEventListener("pointerdown", down);
    return () => document.removeEventListener("pointerdown", down);
  }, [open]);
  return (
    <span className="relative inline-flex min-[30rem]:hidden">
      <button
        ref={button}
        type="button"
        aria-label={defaultCopy.dock.more}
        aria-expanded={open}
        onClick={() => setOpen(!open)}
        className={`${composerIconControl} text-aui-muted hover:text-aui-fg`}
      >
        <span aria-hidden>⋯</span>
      </button>
      {open && (
        // biome-ignore lint/a11y/noStaticElementInteractions: Escape closes what it opened
        <div
          ref={panel}
          data-layer="composer-more"
          onKeyDown={(e) => {
            if (e.key === "Escape") {
              setOpen(false);
              button.current?.focus();
            }
          }}
          className="humagen-glass-pop aui-pop-in absolute right-0 bottom-full z-(--aui-z-popover) mb-2 flex items-center rounded-aui-lg p-1.5"
        >
          {children}
        </div>
      )}
    </span>
  );
}
