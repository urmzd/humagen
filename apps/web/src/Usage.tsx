import { defaultCopy } from "@humagen/components";
import { useEffect, useRef, useState } from "react";
import { api, type Bucket, type Group, type Summary } from "./api.ts";
import { Burn } from "./Billing.tsx";
import { money } from "./usage.ts";
import { AgentMark, button, card, figures, Menu, Page, SkeletonRows, Stat, textLink, took } from "./ui.tsx";

const words = defaultCopy.usagePage;
const DAY = 24 * 60 * 60 * 1000;
export type UsageView = { days: 7 | 14 | 30; day?: string; display: "chart" | "table" };
export const defaultUsageView: UsageView = { days: 14, display: "chart" };

/** Calendar days use UTC, including today's partial day. Server bounds stay under 30 days. */
export function usageInterval(days: UsageView["days"], now = new Date()): { since: string; until: string; bucket: string } {
  const today = Date.UTC(now.getUTCFullYear(), now.getUTCMonth(), now.getUTCDate());
  return { since: new Date(today - (days - 1) * DAY).toISOString(), until: now.toISOString(), bucket: "24h" };
}
const dayName = (at: string) => new Date(at).toLocaleDateString(undefined, { timeZone: "UTC", month: "short", day: "numeric", year: "numeric" });
const totalOf = (b: Bucket) => b.completed + b.failed + b.stopped + b.active;
function values(b: Bucket) {
  const alone = b.completed_alone ?? 0;
  const needed = b.needed_person ?? 0;
  return { alone, needed, other: Math.max(totalOf(b) - alone - needed, 0) };
}

function DayChart({ buckets, day, onDay }: { buckets: Bucket[]; day?: string; onDay: (day: string) => void }) {
  const buttons = useRef<(HTMLButtonElement | null)[]>([]);
  const maximum = Math.max(1, ...buckets.map(totalOf));
  return <div className="flex min-w-0 flex-col gap-3">
    <div className="aui-scroll overflow-x-auto pb-1">
      <div role="group" aria-label={words.chartLabel} className="flex h-48 items-stretch gap-1" style={{ minWidth: buckets.length * 28 }}>
        {buckets.map((b, i) => {
          const v = values(b);
          const key = b.start.slice(0, 10);
          return <button key={b.start} ref={(node) => { buttons.current[i] = node; }} type="button"
            aria-label={words.dayLabel(dayName(b.start), totalOf(b), v.alone, v.needed)} aria-pressed={day === key}
            onClick={() => onDay(key)}
            onKeyDown={(event) => {
              const next = event.key === "ArrowRight" ? Math.min(i + 1, buckets.length - 1) : event.key === "ArrowLeft" ? Math.max(i - 1, 0) : event.key === "Home" ? 0 : event.key === "End" ? buckets.length - 1 : -1;
              if (next < 0) return;
              event.preventDefault(); buttons.current[next]?.focus();
              const at = buckets[next]; if (at) onDay(at.start.slice(0, 10));
            }}
            className="flex min-w-6 flex-1 flex-col items-center justify-end gap-1 rounded-aui px-1 py-1 text-aui-2xs tabular-nums text-aui-muted hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover aria-pressed:text-aui-fg">
            <span aria-hidden className="flex h-36 w-full max-w-7 flex-col justify-end gap-px">
              {v.other > 0 && <span className="block rounded-t-sm bg-aui-border" style={{ height: `${v.other / maximum * 100}%` }} />}
              {v.needed > 0 && <span className="block bg-aui-warn" style={{ height: `${v.needed / maximum * 100}%` }} />}
              {v.alone > 0 && <span className="block bg-aui-accent" style={{ height: `${v.alone / maximum * 100}%` }} />}
            </span>
            <span>{new Date(b.start).getUTCDate()}</span>
          </button>;
        })}
      </div>
    </div>
    <ul className="flex flex-wrap gap-x-4 gap-y-1 text-aui-xs text-aui-muted">
      {[{ label: words.alone, color: "bg-aui-accent" }, { label: words.needed, color: "bg-aui-warn" }, { label: words.other, color: "bg-aui-border" }].map((s) => <li key={s.label} className="flex items-center gap-1.5"><span aria-hidden className={`size-2 rounded-sm ${s.color}`} />{s.label}</li>)}
    </ul>
  </div>;
}

function DaysTable({ buckets, day, onDay }: { buckets: Bucket[]; day?: string; onDay: (day: string) => void }) {
  return <table className="w-full table-fixed text-aui-xs tabular-nums">
    <caption className="sr-only">{words.chartTitle}</caption>
    <thead><tr className="text-left align-bottom text-aui-muted">{[words.day, words.runs, words.alone, words.needed, words.other].map((label) => <th key={label} scope="col" className="break-words px-1 pb-2 font-medium">{label}</th>)}</tr></thead>
    <tbody>{buckets.map((b) => {
      const key = b.start.slice(0, 10), v = values(b);
      return <tr key={key} className={`border-t border-aui-border ${day === key ? "bg-aui-hover" : ""}`}>
        <th scope="row" className="px-1 py-1 text-left font-normal"><button type="button" onClick={() => onDay(key)} aria-pressed={day === key}
          aria-label={words.dayLabel(dayName(b.start), totalOf(b), v.alone, v.needed)}
          className="min-h-6 max-w-full break-words rounded-aui py-1 text-left text-aui-fg underline-offset-2 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent">
          {dayName(b.start)}
        </button></th>
        {[totalOf(b), v.alone, v.needed, v.other].map((count, index) => <td key={index} className="px-1 py-2">{count}</td>)}
      </tr>;
    })}</tbody>
  </table>;
}

function AgentBreakdown({ groups, onOpenAgent, onOpenRun }: { groups: Group[]; onOpenAgent: (name: string) => void; onOpenRun: (id: string) => void }) {
  return <section className={card}><h2 className="mb-3 font-medium text-aui-sm">{words.agents}</h2>
    {groups.length === 0 ? <p className="text-aui-sm text-aui-muted">{words.noData}</p> : <ul className="divide-y divide-aui-border">{groups.map((g) => <li key={g.name} className="py-3 first:pt-0 last:pb-0">
      <button type="button" className={`${textLink} inline-flex min-h-6 max-w-full items-center gap-2`} onClick={() => onOpenAgent(g.name)}><AgentMark name={g.name} size={16} /><span className="truncate">{g.name}</span></button>
      <dl className="mt-2 grid grid-cols-2 gap-x-4 gap-y-2 text-aui-xs sm:grid-cols-3 lg:grid-cols-6">
        {[{ label: words.runs, value: g.runs }, { label: words.alone, value: g.completed_alone ?? 0 }, { label: words.needed, value: g.needed_person ?? 0 }, { label: words.avg, value: g.completed + g.failed + g.stopped > 0 && g.avg_ms !== undefined ? took(g.avg_ms) : "–" }, { label: words.handoffs, value: g.handoffs_out ?? 0 }].map((m) => <div key={m.label}><dt className="text-aui-muted">{m.label}</dt><dd className="mt-0.5 tabular-nums text-aui-fg">{m.value}</dd></div>)}
        <div><dt className="text-aui-muted">{words.lastFailure}</dt><dd className="mt-0.5">{g.last_failure_run ? <button type="button" className={textLink} aria-label={`${words.openFailure}: ${g.name}`} onClick={() => onOpenRun(g.last_failure_run ?? "")}>{g.last_failure_at ? dayName(g.last_failure_at) : words.failed}</button> : words.noFailure}</dd></div>
      </dl>
      {(g.human_help_unknown ?? 0) > 0 && <p className="mt-2 text-aui-xs text-aui-muted">{words.unknown}: {g.human_help_unknown}</p>}
    </li>)}</ul>}
  </section>;
}

export function Usage({ agents, tick, onOpenRun, onOpenAgent, view: controlled, onViewChange }: {
  agents: string[]; tick: number; onOpenRun: (id: string) => void; onOpenAgent: (name: string) => void;
  view?: UsageView; onViewChange?: (view: UsageView) => void;
}) {
  const [local, setLocal] = useState(defaultUsageView);
  const view = controlled ?? local;
  const change = (next: UsageView) => { setLocal(next); onViewChange?.(next); };
  const [summary, setSummary] = useState<Summary>();
  const [daily, setDaily] = useState<Summary>();
  const [error, setError] = useState(false);
  const [dailyError, setDailyError] = useState(false);
  const [retry, setRetry] = useState(0);
  const previousRange = useRef(view.days);
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick and retry explicitly refresh the bounded query
  useEffect(() => {
    let current = true;
    if (previousRange.current !== view.days) { setSummary(undefined); previousRange.current = view.days; }
    setError(false);
    api.analytics(`${view.days * 24}h`, usageInterval(view.days)).then((next) => { if (current) setSummary(next); }).catch(() => { if (current) setError(true); });
    return () => { current = false; };
  }, [view.days, tick, retry]);
  const selected = summary?.buckets.find((b) => b.start.slice(0, 10) === view.day);
  const start = selected?.start;
  const until = summary?.until;
  // biome-ignore lint/correctness/useExhaustiveDependencies: retry refreshes a failed selected-day query
  useEffect(() => {
    let current = true; setDaily(undefined); setDailyError(false);
    if (start && until) {
      const end = new Date(Math.min(Date.parse(start) + DAY, Date.parse(until))).toISOString();
      api.analytics("24h", { since: start, until: end, bucket: "24h" }).then((next) => { if (current) setDaily(next); }).catch(() => { if (current) setDailyError(true); });
    }
    return () => { current = false; };
  }, [start, until, retry]);
  const shown = view.day ? daily : summary;
  const total = shown?.total;
  const choose = (day: string) => change({ ...view, day });
  const clear = () => change({ ...view, day: undefined });
  return <Page title={words.title} lede={words.lede} actions={<fieldset className="flex flex-wrap gap-1"><legend className="sr-only">{words.range}</legend>{([7, 14, 30] as const).map((days) => <button key={days} type="button" aria-pressed={view.days === days} className={`${button} aria-pressed:bg-aui-fg aria-pressed:text-aui-bg`} onClick={() => change({ days, display: view.display })}>{words.days(days)}</button>)}</fieldset>}>
    {error && <p role="alert" className="flex flex-wrap items-center gap-2 text-aui-sm text-aui-bad">{words.loadFailed}<button type="button" className={button} onClick={() => setRetry((n) => n + 1)}>{words.retry}</button></p>}
    {!summary ? !error && <SkeletonRows count={4} height="4rem" /> : <>
      <section className={card}><div className="mb-3 flex flex-wrap items-center justify-between gap-2"><h2 className="font-medium text-aui-sm">{words.chartTitle}</h2><button type="button" className={button} aria-pressed={view.display === "table"} onClick={() => change({ ...view, display: view.display === "chart" ? "table" : "chart" })}>{view.display === "chart" ? words.table : words.chart}</button></div>
        {view.display === "chart" ? <DayChart buckets={summary.buckets} day={view.day} onDay={choose} /> : <DaysTable buckets={summary.buckets} day={view.day} onDay={choose} />}
        <p className="mt-3 text-aui-xs text-aui-muted">{words.timezone} {words.otherNote}</p>
        <div className="mt-3 flex flex-wrap items-center gap-3" aria-live="polite"><span className="text-aui-sm font-medium">{selected ? `${words.details}: ${dayName(selected.start)}` : `${dayName(summary.since)} – ${dayName(summary.until)}`}</span>{view.day && <button type="button" className={textLink} onClick={clear}>{words.clearDay}</button>}</div>
      </section>
      {view.day && !selected && <p role="alert" className="text-aui-sm text-aui-muted">{words.noDataBody}<button type="button" className={button} onClick={clear}>{words.clearDay}</button></p>}
      {dailyError && <p role="alert" className="flex flex-wrap items-center gap-2 text-aui-sm text-aui-bad">{words.dayFailed}<button type="button" className={button} onClick={() => setRetry((n) => n + 1)}>{words.retry}</button></p>}
      {!total ? selected && !dailyError && <SkeletonRows count={3} height="3rem" /> : <>
        <div className={figures}><Stat label={words.runs} value={String(total.runs)} /><Stat label={words.alone} value={String(total.completed_alone ?? 0)} /><Stat label={words.needed} value={String(total.needed_person ?? 0)} /><Stat label={words.failed} value={String(total.failed)} /><Stat label={words.cost} value={`${money({ micros: total.usage?.spend ?? 0, currency: "USD" })}${total.usage?.unpriced_calls || total.usage?.other_currency_runs ? "+" : ""}`} note={total.usage?.unpriced_calls || total.usage?.other_currency_runs ? words.partialCost : words.priced} /><Stat label={words.tokens} value={String(total.usage?.tokens ?? 0)} /></div>
        {(total.human_help_unknown ?? 0) > 0 && <p className="text-aui-sm text-aui-muted">{words.unknown}: {total.human_help_unknown}. {words.unknownNote}</p>}
        {total.runs === 0 && <p className="text-aui-sm text-aui-muted">{words.noData}</p>}
        <AgentBreakdown groups={shown?.by_agent ?? []} onOpenAgent={onOpenAgent} onOpenRun={onOpenRun} />
        {agents.length > 0 && <div className="max-w-sm"><Menu label={words.agents} value="" options={[{ value: "", label: words.agents }, ...agents.map((name) => ({ value: name, label: name }))]} onChange={(name) => { if (name) onOpenAgent(name); }} /></div>}
      </>}
      <Burn tick={tick} />
    </>}
  </Page>;
}
