import { AlertTriangleIcon, CheckCircleIcon, SparkleIcon } from "@warp/render-web";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { type Alert, type AlertRule, ApiError, alertRuleEdit, api, type Revision } from "./api.ts";
import { Analytics } from "./Analytics.tsx";
import { type ProjectRef, projectOptions, titleIn } from "./projects.ts";
import type { MonitorTab } from "./route.ts";
import { agentOptions, ago, button, card, Chip, Empty, field, Label, Menu, Page, Problem, primary, rows, Tabs, useStrip } from "./ui.tsx";

const kindLabel: Record<AlertRule["kind"], string> = {
  run_failed: "A run fails",
  failure_rate: "Failure rate passes a threshold",
  slow_run: "A run takes too long",
  eval_below: "An eval's pass rate falls short",
  spend: "Spend passes an amount",
};

const usd = (micros = 0) => `$${(micros / 1_000_000).toLocaleString(undefined, { maximumFractionDigits: 2 })}`;
const span = (seconds = 0) =>
  seconds >= 86400 && seconds % 86400 === 0
    ? `${seconds / 86400} ${seconds === 86400 ? "day" : "days"}`
    : seconds >= 3600 && seconds % 3600 === 0
      ? `${seconds / 3600} ${seconds === 3600 ? "hour" : "hours"}`
      : `${Math.round(seconds / 60)} minutes`;

/** Who hears about a rule, in words. */
export function audience(r: Pick<AlertRule, "notify">): string {
  const n = r.notify ?? [];
  return n.length === 0 ? "everyone who can see alerts" : n.map((x) => (x.startsWith("role:") ? `the ${x.slice(5)} role` : x)).join(", ");
}

/** What a rule watches, in words. A project is named by its title among projects, else as the rule names it. */
export function describe(r: AlertRule, projects: readonly ProjectRef[] = []): string {
  const scope = `${r.agent ? r.agent : "any agent"}${r.project ? ` in ${titleIn(projects, r.project)}` : ""}`;
  switch (r.kind) {
    case "spend":
      return `When runs of ${scope} cost ${usd(r.threshold_micros)} or more over ${span(r.window_seconds)}.`;
    case "run_failed":
      return `When a run of ${scope} fails.`;
    case "slow_run":
      return `When a run of ${scope} takes longer than ${r.threshold}s.`;
    case "eval_below":
      return `When an eval of ${scope} passes fewer than ${r.threshold}% of its cases.`;
    case "failure_rate":
      return `When ${r.threshold}% or more of ${scope}'s runs fail over ${span(r.window_seconds)}, with at least ${r.min_runs} runs.`;
  }
}

const linkish =
  "rounded-aui-sm text-aui-fg underline underline-offset-2 hover:no-underline focus-visible:outline-2 focus-visible:outline-aui-accent";

/** Severity as a word and a mark, so it never depends on color alone. */
function Severity({ level }: { level: AlertRule["severity"] }) {
  return (
    <span className="inline-flex items-center gap-1.5 text-aui-xs text-aui-muted">
      <span aria-hidden className={`size-1.5 rounded-full ${level === "critical" ? "bg-aui-bad" : "bg-aui-warn"}`} />
      {level === "critical" ? "Critical" : "Warning"}
    </span>
  );
}

/** Questions the analyst is good at, offered as one tap each. */
export const ANALYST_STARTERS = [
  "How did my agents do today?",
  "Which agent failed most this week, and why?",
  "What did we spend in the last 7 days, by agent?",
];

/**
 * What fired, at a glance. Fired alerts are resolved in the inbox, so this
 * only counts them and sends the reader there.
 */
export function FiredStrip({ alerts, onOpen }: { alerts: Alert[]; onOpen?: () => void }) {
  const open = alerts.filter((a) => !a.acked_by);
  const latest = [...open].sort((x, y) => y.at.localeCompare(x.at))[0];
  const critical = open.some((a) => a.severity === "critical");
  return (
    <section data-testid="alerts-open" aria-label="Fired alerts" className={`${card} flex flex-wrap items-center gap-x-4 gap-y-3`}>
      <span
        aria-hidden
        className={`inline-flex size-9 shrink-0 items-center justify-center rounded-full ${
          open.length === 0 ? "bg-aui-ok/12 text-aui-ok" : critical ? "bg-aui-bad/12 text-aui-bad" : "bg-aui-warn/12 text-aui-warn"
        }`}
      >
        {open.length === 0 ? <CheckCircleIcon size={16} /> : <AlertTriangleIcon size={16} />}
      </span>
      <div className="min-w-[12rem] flex-1">
        <p className="text-aui-sm font-medium text-aui-fg">
          {open.length === 0 ? "Nothing that fired is still open" : `${open.length} fired ${open.length === 1 ? "alert is" : "alerts are"} open`}
        </p>
        <p className="truncate text-aui-xs text-aui-muted">
          {latest ? `Latest: ${latest.title}, ${ago(latest.at)}` : "When a rule fires, it lands in your inbox, where you resolve it."}
        </p>
      </div>
      {onOpen && (
        <button type="button" className={`${open.length > 0 ? primary : button} max-sm:w-full`} onClick={onOpen}>
          Review in Inbox
        </button>
      )}
    </section>
  );
}

/** A way into the built-in analyst: one tap asks it a question in a new conversation. */
export function AskAnalyst({ onAsk }: { onAsk: (question: string) => void }) {
  // Nothing in the row is chosen: the strip only fades the end that has more.
  const strip = useStrip<HTMLDivElement>(null);
  return (
    <section aria-label="Ask the analyst" className="flex flex-col gap-2">
      <h2 className="text-aui-xs font-medium text-aui-muted">Ask the analyst for a deep dive</h2>
      <div ref={strip} className="warp-strip aui-scroll -mx-1 flex gap-2 overflow-x-auto px-1 pb-1">
        {ANALYST_STARTERS.map((q) => (
          <button
            key={q}
            type="button"
            onClick={() => onAsk(q)}
            className="inline-flex h-9 shrink-0 items-center gap-1.5 whitespace-nowrap rounded-full border border-aui-border px-3.5 text-aui-sm text-aui-fg transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
          >
            <SparkleIcon size={14} className="text-aui-accent" />
            {q}
          </button>
        ))}
      </div>
    </section>
  );
}

/**
 * Monitor: how the workspace's runs are going, and the rules that watch them.
 * It is where alerting is configured and where trends are read. When a rule
 * holds it raises a message in the inbox, and that is where it is resolved,
 * so there is one place for what fired.
 */
export function Monitor({
  tab: wanted = "overview",
  onTab,
  projects = [],
  alerts,
  rules,
  agents,
  canWrite,
  canAnalytics = false,
  tick = 0,
  onChanged,
  onOpenFired,
  onOpenAlert,
  onAsk,
  onOpenRun,
}: {
  /** Overview: how runs are going. Rules: what is watched. */
  tab?: MonitorTab;
  onTab?: (next: MonitorTab) => void;
  /** Projects a rule can be scoped to, each named by its title. */
  projects?: ProjectRef[];
  /** May read run statistics, for the Overview tab. */
  canAnalytics?: boolean;
  /** Changes when runs do, so the overview reads again. */
  tick?: number;
  alerts: Alert[];
  rules: AlertRule[];
  agents: Revision[];
  canWrite: boolean;
  onChanged: () => void;
  /** Open the inbox at what rules fired. */
  onOpenFired?: () => void;
  /** Open the message one fired alert raised. */
  onOpenAlert?: (alert: Alert) => void;
  /** Ask the built-in analyst, when the workspace has it. */
  onAsk?: (question: string) => void;
  /** Open the task a run belongs to, from the overview. */
  onOpenRun?: (id: string) => void;
}) {
  const [own, setOwn] = useState<MonitorTab>(wanted);
  const asked = onTab ? wanted : own;
  // Someone who may not read run statistics has only the rules.
  const tab: MonitorTab = asked === "overview" && !canAnalytics ? "rules" : asked;
  const setTab = (next: MonitorTab) => (onTab ? onTab(next) : setOwn(next));
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const open = alerts.filter((a) => !a.acked_by).length;
  const newest = [...alerts].sort((x, y) => y.at.localeCompare(x.at));

  const act = (p: Promise<unknown>, fallback: string) => {
    setProblem(null);
    p.then(onChanged).catch((e) => setProblem(e instanceof ApiError ? e.message : fallback));
  };

  return (
    <Page
      title="Monitor"
      lede="How your agents are doing, and the rules that watch them. When a rule fires, it lands in your inbox, where it gets resolved."
      actions={
        canWrite && !adding && tab === "rules" ? (
          <button type="button" className={primary} onClick={() => setAdding(true)}>
            New Rule
          </button>
        ) : undefined
      }
      tabs={
        <Tabs
          label="Monitor sections"
          value={tab}
          onChange={setTab}
          tabs={[...(canAnalytics ? [{ id: "overview" as const, label: "Overview" }] : []), { id: "rules", label: "Rules", count: rules.length }]}
        />
      }
    >
      <Problem message={problem} />
      <FiredStrip alerts={alerts} onOpen={onOpenFired} />
      {tab === "overview" && (
        <>
          {onAsk && <AskAnalyst onAsk={onAsk} />}
          <Analytics embedded agents={agents} openAlerts={open} tick={tick} onOpenRun={onOpenRun ?? (() => {})} />
        </>
      )}
      {tab === "rules" && (
        <>

      {adding && (
        <RuleForm
          agents={agents}
          projects={projects}
          onClose={() => setAdding(false)}
          onSaved={() => {
            setAdding(false);
            onChanged();
          }}
        />
      )}

      {rules.length === 0 ? (
        <Empty title="No rules yet">
          {canWrite ? "Without a rule, nothing is watched and nothing reaches the inbox. Add one with New Rule." : "Without a rule, nothing is watched. Ask someone who can change alerts to add one."}
        </Empty>
      ) : (
        <ul className={rows} aria-label="Rules">
          {rules.map((r) => {
            const fired = newest.filter((a) => a.rule === r.name);
            const waiting = fired.filter((a) => !a.acked_by).length;
            if (editing === r.name) {
              return (
                <li key={r.name} className="py-4">
                  <RuleForm
                    editing={r}
                    agents={agents}
                    projects={projects}
                    onClose={() => setEditing(null)}
                    onSaved={() => {
                      setEditing(null);
                      onChanged();
                    }}
                  />
                </li>
              );
            }
            return (
              <li key={r.name} data-testid="alert-rule" className="grid grid-cols-1 gap-x-8 gap-y-3 py-4 @3xl:grid-cols-[minmax(0,5fr)_minmax(0,6fr)_auto]">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <h2 className="warp-display text-aui-lg font-semibold text-aui-fg">{r.name}</h2>
                    <Severity level={r.severity} />
                    {r.paused && <Chip>Paused</Chip>}
                  </div>
                  <p className="pt-0.5 text-aui-sm text-aui-muted">{describe(r, projects)}</p>
                  {r.description && <p className="pt-0.5 text-aui-sm text-aui-fg">{r.description}</p>}
                  <p className="pt-1 text-aui-xs text-aui-muted">
                    Watches {r.agent ? r.agent : "every agent"}{r.project ? ` in ${titleIn(projects, r.project)}` : ""}. Raises a {r.severity === "critical" ? "critical" : "warning"} message for {audience(r)}
                    {r.cooldown_seconds ? `, then stays quiet for ${span(r.cooldown_seconds)}` : ""}
                    {r.created_by ? `. Made by ${r.created_by_name || r.created_by}.` : "."}
                  </p>
                </div>

                <div className="min-w-0">
                  <p className="text-aui-xs text-aui-muted">
                    {fired.length === 0
                      ? "Has not fired."
                      : `Fired ${fired.length} ${fired.length === 1 ? "time" : "times"}${waiting > 0 ? `, ${waiting} still open` : ""}.`}
                  </p>
                  {fired.length > 0 && (
                    <ul className="pt-1">
                      {fired.slice(0, 3).map((a) => (
                        <li key={a.id} className="flex items-baseline gap-3 py-0.5 text-aui-sm">
                          {onOpenAlert ? (
                            <button type="button" className={`${linkish} min-w-0 truncate text-left`} onClick={() => onOpenAlert(a)}>
                              {a.title}
                            </button>
                          ) : (
                            <span className="min-w-0 truncate text-aui-fg">{a.title}</span>
                          )}
                          <span className="shrink-0 text-aui-xs text-aui-muted">
                            {ago(a.at)}, {a.acked_by ? `acknowledged by ${a.acked_by_name || a.acked_by}` : "open"}
                          </span>
                        </li>
                      ))}
                    </ul>
                  )}
                </div>

                {canWrite && (
                  <div className="flex flex-wrap gap-2 self-start">
                    <button type="button" className={button} aria-label={`Edit the rule ${r.name}`} onClick={() => setEditing(r.name)}>
                      Edit
                    </button>
                    <button
                      type="button"
                      className={button}
                      aria-label={`${r.paused ? "Resume" : "Pause"} the rule ${r.name}`}
                      onClick={() => act(api.setAlertRulePaused(r, !r.paused), "Could not change the rule.")}
                    >
                      {r.paused ? "Resume" : "Pause"}
                    </button>
                    <button
                      type="button"
                      className={button}
                      aria-label={`Delete the rule ${r.name}`}
                      onClick={() => act(api.deleteAlertRule(r.name), "Could not delete the rule.")}
                    >
                      Delete
                    </button>
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}
        </>
      )}
    </Page>
  );
}

/** Days, hours or minutes, whichever reads naturally, as a number and a unit. */
const units = [
  { value: "60", label: "minutes" },
  { value: "3600", label: "hours" },
  { value: "86400", label: "days" },
];
function split(seconds: number): [number, string] {
  if (seconds > 0 && seconds % 86400 === 0) return [seconds / 86400, "86400"];
  if (seconds > 0 && seconds % 3600 === 0) return [seconds / 3600, "3600"];
  return [Math.round(seconds / 60), "60"];
}
function Duration({ label, seconds, onChange }: { label: string; seconds: number; onChange: (seconds: number) => void }) {
  const [n, unit] = split(seconds);
  return (
    <span className="flex gap-2">
      <input
        aria-label={label}
        className={`${field} w-24`}
        type="number"
        min={0}
        value={n}
        onChange={(e) => onChange(Math.max(0, Number(e.target.value)) * Number(unit))}
      />
      <Menu label={`${label}, unit`} className="w-32" value={unit} options={units} onChange={(u) => onChange(n * Number(u))} />
    </span>
  );
}

const defaults: Record<AlertRule["kind"], Partial<AlertRule>> = {
  run_failed: {},
  failure_rate: { threshold: 50, window_seconds: 900, min_runs: 5 },
  slow_run: { threshold: 60 },
  eval_below: { threshold: 80 },
  spend: { threshold_micros: 10_000_000, window_seconds: 86400 },
};

/** Only the fields a kind uses, so a rule never carries a stale threshold from another kind. */
function bodyOf(r: AlertRule): AlertRule {
  const body: AlertRule = { name: r.name, kind: r.kind, severity: r.severity };
  if (r.description?.trim()) body.description = r.description.trim();
  if (r.agent) body.agent = r.agent;
  if (r.project && r.kind !== "eval_below") body.project = r.project;
  if (r.kind === "failure_rate" || r.kind === "slow_run" || r.kind === "eval_below") body.threshold = r.threshold;
  if (r.kind === "failure_rate") body.min_runs = r.min_runs;
  if (r.kind === "failure_rate" || r.kind === "spend") body.window_seconds = r.window_seconds;
  if (r.kind === "spend") body.threshold_micros = r.threshold_micros;
  if (r.cooldown_seconds) body.cooldown_seconds = r.cooldown_seconds;
  if (r.notify && r.notify.length > 0) body.notify = r.notify;
  if (r.paused) body.paused = true;
  return body;
}

/** Make a rule, or change one. What a kind needs appears with it. */
function RuleForm({
  editing,
  agents,
  projects,
  onClose,
  onSaved,
}: {
  editing?: AlertRule;
  agents: Revision[];
  projects: ProjectRef[];
  onClose: () => void;
  onSaved: () => void;
}) {
  const [rule, setRule] = useState<AlertRule>(editing ?? { name: "", kind: "run_failed", severity: "critical", agent: "" });
  const [notify, setNotify] = useState((editing?.notify ?? []).join("\n"));
  const [problem, setProblem] = useState<string | null>(null);
  const set = (patch: Partial<AlertRule>) => setRule((r) => ({ ...r, ...patch }));

  const save = async (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const names = [...new Set(notify.split(/[\n,]+/).map((x) => x.trim()).filter(Boolean))];
    const body = bodyOf({ ...rule, notify: names });
    try {
      if (editing) await api.updateAlertRule(editing.name, alertRuleEdit(body));
      else await api.createAlertRule(body);
      onSaved();
    } catch (err) {
      setProblem(err instanceof ApiError ? err.message : "The rule could not be saved.");
    }
  };

  return (
    <form onSubmit={save} className={`${card} flex flex-col gap-4`}>
      <h2 className="text-aui-base font-medium text-aui-fg">{editing ? `Change ${editing.name}` : "New rule"}</h2>
      <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
        <Label text="Name" hint={editing ? "A rule's name cannot change." : undefined}>
          <input className={field} value={rule.name} required disabled={!!editing} onChange={(e) => set({ name: e.target.value })} />
        </Label>
        <Label text="Raise an alert when">
          <Menu
            label="Raise an alert when"
            value={rule.kind}
            options={Object.entries(kindLabel).map(([k, label]) => ({ value: k as AlertRule["kind"], label }))}
            onChange={(kind) => set({ kind, ...defaults[kind] })}
          />
        </Label>
        <Label text="Agent">
          <Menu
            label="Agent"
            value={rule.agent ?? ""}
            options={agentOptions(agents.map((a) => a.spec.name), "Any agent")}
            onChange={(agent) => set({ agent })}
          />
        </Label>
        {rule.kind !== "eval_below" && (
          <Label text="Project">
            <Menu
              label="Project"
              value={rule.project ?? ""}
              options={[{ value: "", label: "Any project, or none" }, ...projectOptions(projects)]}
              onChange={(project) => set({ project: project || undefined })}
            />
          </Label>
        )}
        {rule.kind === "slow_run" && (
          <Label text="Longer than (seconds)">
            <input className={field} type="number" min={1} value={rule.threshold ?? 60} onChange={(e) => set({ threshold: Number(e.target.value) })} />
          </Label>
        )}
        {rule.kind === "eval_below" && (
          <Label text="Pass rate under (%)">
            <input className={field} type="number" min={1} max={100} value={rule.threshold ?? 80} onChange={(e) => set({ threshold: Number(e.target.value) })} />
          </Label>
        )}
        {rule.kind === "failure_rate" && (
          <>
            <Label text="Failure rate at or above (%)">
              <input className={field} type="number" min={1} max={100} value={rule.threshold ?? 50} onChange={(e) => set({ threshold: Number(e.target.value) })} />
            </Label>
            <Label text="With at least (runs)" hint="So one failure out of one run does not count as 100%.">
              <input className={field} type="number" min={1} value={rule.min_runs ?? 5} onChange={(e) => set({ min_runs: Number(e.target.value) })} />
            </Label>
          </>
        )}
        {rule.kind === "spend" && (
          <Label text="Costs at least (USD)">
            <input
              className={field}
              type="number"
              min={0.01}
              step={0.01}
              value={(rule.threshold_micros ?? 0) / 1_000_000}
              onChange={(e) => set({ threshold_micros: Math.round(Number(e.target.value) * 1_000_000) })}
            />
          </Label>
        )}
        {(rule.kind === "failure_rate" || rule.kind === "spend") && (
          <Label text="Over the last">
            <Duration label="Over the last" seconds={rule.window_seconds ?? 900} onChange={(window_seconds) => set({ window_seconds })} />
          </Label>
        )}
      </div>

      <div className="grid grid-cols-1 gap-3 border-aui-border border-t pt-4 sm:grid-cols-2">
        <Label text="Severity">
          <Menu
            label="Severity"
            value={rule.severity}
            options={[
              { value: "critical", label: "Critical" },
              { value: "warning", label: "Warning" },
            ]}
            onChange={(severity) => set({ severity })}
          />
        </Label>
        <Label text="Then stay quiet for" hint="After it raises, it waits this long before raising again. Zero waits only until the alert is acknowledged.">
          <Duration label="Then stay quiet for" seconds={rule.cooldown_seconds ?? 0} onChange={(cooldown_seconds) => set({ cooldown_seconds })} />
        </Label>
        <Label text="Notify" hint="People by sign-in subject, and roles as role:operator, one per line. Empty notifies everyone who can see alerts.">
          <textarea className={`${field} min-h-16 font-aui-mono text-aui-sm`} value={notify} spellCheck={false} onChange={(e) => setNotify(e.target.value)} />
        </Label>
        <Label text="Description" hint="What to do when it fires, for whoever reads it.">
          <textarea className={`${field} min-h-16`} maxLength={500} value={rule.description ?? ""} onChange={(e) => set({ description: e.target.value })} />
        </Label>
      </div>
      <Problem message={problem} />
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={onClose}>
          Cancel
        </button>
        <button type="submit" className={primary}>
          {editing ? "Save Rule" : "Create Rule"}
        </button>
      </div>
    </form>
  );
}
