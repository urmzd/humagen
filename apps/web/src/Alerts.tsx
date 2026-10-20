import { type FormEvent, useEffect, useRef, useState } from "react";
import { type Alert, type AlertRule, ApiError, alertRuleEdit, api, type Revision } from "./api.ts";
import { Analytics } from "./Analytics.tsx";
import { alertNamed } from "./links.ts";
import type { AlertsTab } from "./route.ts";
import { agentOptions, ago, button, card, Chip, Empty, field, Label, Menu, Page, Problem, primary, rows, Tabs } from "./ui.tsx";

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

export function describe(r: AlertRule): string {
  const scope = `${r.agent ? r.agent : "any agent"}${r.project ? ` in ${r.project}` : ""}`;
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

/**
 * What to watch for. A rule looks at runs as they end, and when it holds it
 * raises a message in the inbox. This page is where rules are made and where
 * each one's recent firings are listed. Resolving what a rule raised happens
 * in the inbox, so there is one place for it.
 */
export function Alerts({
  tab: wanted = "rules",
  onTab,
  projects = [],
  alerts,
  loaded = true,
  selected = null,
  onSelect,
  rules,
  agents,
  canWrite,
  canAnalytics = false,
  tick = 0,
  onChanged,
  onOpenInbox,
  onOpenRun,
}: {
  /** Open: what fired. Rules: what is watched. Overview: how runs are going. */
  tab?: AlertsTab;
  onTab?: (next: AlertsTab) => void;
  /** Projects a rule can be scoped to. */
  projects?: string[];
  /** May read run statistics, for the Overview tab. */
  canAnalytics?: boolean;
  /** Changes when runs do, so the overview reads again. */
  tick?: number;
  alerts: Alert[];
  /** False until the first list is in, so a linked alert is not called missing too early. */
  loaded?: boolean;
  /** The alert a link opened, by id: `?view=alerts&alert=<id>`. */
  selected?: string | null;
  /** Open another alert, or close it with null. */
  onSelect?: (id: string | null) => void;
  rules: AlertRule[];
  agents: Revision[];
  canWrite: boolean;
  onChanged: () => void;
  /** Open the inbox, at the message an alert raised when one is given. */
  onOpenInbox?: (alert?: Alert) => void;
  /** Open the task a run belongs to, from the overview. */
  onOpenRun?: (id: string) => void;
}) {
  const [own, setOwn] = useState<AlertsTab>(wanted);
  const tab = onTab ? wanted : own;
  const setTab = (next: AlertsTab) => (onTab ? onTab(next) : setOwn(next));
  const [adding, setAdding] = useState(false);
  const [editing, setEditing] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const open = alerts.filter((a) => !a.acked_by).length;
  const newest = [...alerts].sort((x, y) => y.at.localeCompare(x.at));
  const shown = alertNamed(alerts, selected);

  const act = (p: Promise<unknown>, fallback: string) => {
    setProblem(null);
    p.then(onChanged).catch((e) => setProblem(e instanceof ApiError ? e.message : fallback));
  };

  return (
    <Page
      title="Alerts"
      lede="Rules watch runs, evals and spend. When one holds, it raises a message in the inbox for the people it names, and that is where it gets resolved."
      actions={
        canWrite && !adding && tab === "rules" ? (
          <button type="button" className={primary} onClick={() => setAdding(true)}>
            New Rule
          </button>
        ) : undefined
      }
      tabs={
        <Tabs
          label="Alerts sections"
          value={tab}
          onChange={setTab}
          tabs={[
            { id: "open", label: "Fired", count: open },
            { id: "rules", label: "Rules", count: rules.length },
            ...(canAnalytics ? [{ id: "overview" as const, label: "Overview" }] : []),
          ]}
        />
      }
    >
      <Problem message={problem} />
      {tab === "overview" && canAnalytics && <Analytics embedded agents={agents} openAlerts={open} tick={tick} onOpenRun={onOpenRun ?? (() => {})} />}
      {tab === "open" && (
        <>
      {selected && loaded && !shown && (
        <Problem message="That alert is not here. It may have aged out, or you may not have access to it." />
      )}
      {shown && <AlertCard alert={shown} onClose={() => onSelect?.(null)} onOpenInbox={onOpenInbox} />}

      <p data-testid="alerts-open" className="text-aui-sm text-aui-muted">
        {open === 0 ? "Nothing a rule raised is open." : `${open} ${open === 1 ? "message a rule raised is" : "messages rules raised are"} open.`}{" "}
        {onOpenInbox && (
          <button type="button" className={linkish} onClick={() => onOpenInbox()}>
            Open the inbox
          </button>
        )}
      </p>
      {newest.length === 0 ? (
        <Empty title="Nothing has fired">When a rule holds, what it raised is listed here, newest first.</Empty>
      ) : (
        <ul className={rows} aria-label="Fired alerts">
          {newest.slice(0, 100).map((a) => (
            <li key={a.id} className="flex flex-wrap items-baseline gap-x-4 gap-y-1 py-3">
              <Severity level={a.severity} />
              <button
                type="button"
                className={`${linkish} min-w-0 flex-1 truncate text-left`}
                aria-current={a.id === selected ? "true" : undefined}
                onClick={() => onSelect?.(a.id)}
              >
                {a.title}
              </button>
              <span className="shrink-0 text-aui-xs text-aui-muted">
                {a.rule || "Limits"}
                {a.agent ? ` on ${a.agent}` : ""}, {ago(a.at)}, {a.acked_by ? `acknowledged by ${a.acked_by}` : "open"}
              </span>
            </li>
          ))}
        </ul>
      )}
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
                  <p className="pt-0.5 text-aui-sm text-aui-muted">{describe(r)}</p>
                  {r.description && <p className="pt-0.5 text-aui-sm text-aui-fg">{r.description}</p>}
                  <p className="pt-1 text-aui-xs text-aui-muted">
                    Watches {r.agent ? r.agent : "every agent"}{r.project ? ` in ${r.project}` : ""}. Raises a {r.severity === "critical" ? "critical" : "warning"} message for {audience(r)}
                    {r.cooldown_seconds ? `, then stays quiet for ${span(r.cooldown_seconds)}` : ""}
                    {r.created_by ? `. Made by ${r.created_by}.` : "."}
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
                        <li
                          key={a.id}
                          aria-current={a.id === selected ? "true" : undefined}
                          className={`flex items-baseline gap-3 py-0.5 text-aui-sm ${a.id === selected ? "font-medium" : ""}`}
                        >
                          {onOpenInbox ? (
                            <button type="button" className={`${linkish} min-w-0 truncate text-left`} onClick={() => onOpenInbox(a)}>
                              {a.title}
                            </button>
                          ) : (
                            <span className="min-w-0 truncate text-aui-fg">{a.title}</span>
                          )}
                          <span className="shrink-0 text-aui-xs text-aui-muted">
                            {ago(a.at)}, {a.acked_by ? `acknowledged by ${a.acked_by}` : "open"}
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

/** The alert a link opened: what fired, how serious, when, and where to resolve it. */
function AlertCard({ alert, onClose, onOpenInbox }: { alert: Alert; onClose: () => void; onOpenInbox?: (alert?: Alert) => void }) {
  const heading = useRef<HTMLHeadingElement>(null);
  // Reached from a link, so the alert is where the reader starts.
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per alert
  useEffect(() => {
    heading.current?.focus();
  }, [alert.id]);
  return (
    <section data-testid="alert-open" aria-labelledby={`alert-${alert.id}`} className={`${card} flex flex-col gap-2`}>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
        <h2 id={`alert-${alert.id}`} ref={heading} tabIndex={-1} className="warp-display text-aui-lg font-semibold text-aui-fg outline-none">
          {alert.title}
        </h2>
        <Severity level={alert.severity} />
      </div>
      {alert.body && <p className="text-aui-sm text-aui-muted">{alert.body}</p>}
      <p className="text-aui-xs text-aui-muted">
        Raised by the rule {alert.rule || "for limits"}
        {alert.agent ? ` on ${alert.agent}` : ""}, {ago(alert.at)}. {alert.acked_by ? `Acknowledged by ${alert.acked_by}.` : "Open."}
      </p>
      <div className="flex flex-wrap gap-2">
        {onOpenInbox && (
          <button type="button" className={primary} onClick={() => onOpenInbox(alert)}>
            Open Its Message
          </button>
        )}
        <button type="button" className={button} onClick={onClose}>
          Close
        </button>
      </div>
    </section>
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
  projects: string[];
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
              options={[{ value: "", label: "Any project, or none" }, ...projects.map((p) => ({ value: p, label: p }))]}
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
