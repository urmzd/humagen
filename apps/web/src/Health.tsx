import { defaultCopy } from "@humagen/components/copy";
import { useCallback, useEffect, useState } from "react";
import type {
  HealthComponent,
  Health as HealthReport,
  HealthStatus,
} from "../../../packages/client/src/generated/types.ts";
import { ApiError, api } from "./api.ts";
import { Problem, rows, Section } from "./ui.tsx";

const copy = defaultCopy.health;

/** How long the page waits before asking again. The server reuses a report for 30 seconds. */
const REFRESH_MS = 30_000;

/** The color a status is drawn in. Operational is quiet; only trouble asks for a look. */
const tone: Record<HealthStatus, { dot: string; bar: string; text: string; banner: string }> = {
  operational: {
    dot: "bg-aui-ok",
    bar: "bg-aui-ok/70",
    text: "text-aui-fg-muted",
    banner: "border-aui-ok/40 bg-aui-ok/10",
  },
  degraded: {
    dot: "bg-aui-warn",
    bar: "bg-aui-warn",
    text: "text-aui-warn",
    banner: "border-aui-warn/40 bg-aui-warn/10",
  },
  down: {
    dot: "bg-aui-bad",
    bar: "bg-aui-bad",
    text: "text-aui-bad",
    banner: "border-aui-bad/40 bg-aui-bad/10",
  },
};

/** Components by group, in the order the server lists them. */
export function grouped(components: readonly HealthComponent[]): [string, HealthComponent[]][] {
  const out = new Map<string, HealthComponent[]>();
  for (const c of components) out.set(c.group, [...(out.get(c.group) ?? []), c]);
  return [...out];
}

/** The share of checks that found a component working, as a whole percent. */
export function uptime(c: Pick<HealthComponent, "history">): number | null {
  if (c.history.length === 0) return null;
  const ok = c.history.filter((s) => s.status !== "down").length;
  return Math.floor((ok / c.history.length) * 100);
}

/** One bar per check, oldest first, padded on the left to a fixed width. */
function History({ c, slots = 90 }: { c: HealthComponent; slots?: number }) {
  const pad = Math.max(0, slots - c.history.length);
  return (
    <div className="flex h-6 items-stretch gap-px" aria-hidden>
      {Array.from({ length: pad }, (_, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: padding has no identity
        <span key={`pad-${i}`} className="w-full min-w-0.5 rounded-[1px] bg-aui-fg/8" />
      ))}
      {c.history.map((s) => (
        <span
          key={s.at}
          title={`${new Date(s.at).toLocaleString()}: ${copy.status[s.status]}`}
          className={`w-full min-w-0.5 rounded-[1px] ${tone[s.status].bar}`}
        />
      ))}
    </div>
  );
}

function Component({ c }: { c: HealthComponent }) {
  const up = uptime(c);
  return (
    <li className="flex flex-col gap-2 py-4">
      <div className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1">
        <span className="flex items-center gap-2 font-medium text-aui-fg">
          <span className={`size-2 rounded-full ${tone[c.status].dot}`} aria-hidden />
          {c.name}
        </span>
        <span className={`text-aui-sm ${tone[c.status].text}`}>
          {copy.status[c.status]}
          {c.latency_ms > 0 && <span className="text-aui-fg-muted"> · {c.latency_ms} ms</span>}
        </span>
      </div>
      <History c={c} />
      <div className="flex flex-wrap justify-between gap-x-4 text-aui-fg-muted text-aui-xs">
        <span>{c.detail}</span>
        {up !== null && <span>{copy.uptime(up, c.history.length)}</span>}
      </div>
    </li>
  );
}

/** The deployment's health as a status page: a banner, then each component with its recent checks. */
export function HealthView({ report }: { report: HealthReport }) {
  return (
    <div className="flex flex-col gap-8">
      <div
        role="status"
        className={`flex flex-wrap items-center justify-between gap-2 rounded-aui border px-4 py-3 ${tone[report.status].banner}`}
      >
        <span className="flex items-center gap-2 font-medium text-aui-fg">
          <span className={`size-2.5 rounded-full ${tone[report.status].dot}`} aria-hidden />
          {copy.banner[report.status]}
        </span>
        <span className="text-aui-fg-muted text-aui-xs">
          {copy.checked(new Date(report.checked_at).toLocaleTimeString())}
        </span>
      </div>
      {grouped(report.components).map(([group, list]) => (
        <Section key={group} title={group}>
          <ul className={rows} aria-label={group}>
            {list.map((c) => (
              <Component key={c.id} c={c} />
            ))}
          </ul>
        </Section>
      ))}
    </div>
  );
}

/** Loads the report and asks again every 30 seconds while the page is open. */
export function Health() {
  const [report, setReport] = useState<HealthReport | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const load = useCallback(() => {
    api
      .health()
      .then((r) => {
        setReport(r);
        setProblem(null);
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : copy.failed));
  }, []);
  useEffect(() => {
    load();
    const t = setInterval(load, REFRESH_MS);
    return () => clearInterval(t);
  }, [load]);
  return (
    <>
      <Problem message={problem} />
      {report ? (
        <HealthView report={report} />
      ) : (
        !problem && <p className="text-aui-fg-muted text-aui-sm">{copy.loading}</p>
      )}
    </>
  );
}
