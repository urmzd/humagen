import { defaultCopy } from "@humagen/components";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { BuilderButton } from "./AgentBuilder.tsx";
import { AgentDefaultSkills } from "./AgentDefaultSkills.tsx";
import { AgentHealth } from "./AgentHealth.tsx";
import {
  type AgentSpec,
  type Alert,
  type AlertRule,
  ApiError,
  api,
  callGovernance,
  type Group,
  type ModelPolicy,
  type ModelRef,
  type Note,
  type ProjectView,
  type Revision,
  type Source,
  type SuiteView,
} from "./api.ts";
import { Evals } from "./Evals.tsx";
import { ApprovalQueue, CallPolicyEditor, useReach } from "./Governance.tsx";
import { chainProblem, chainWords, defaultModel, ModelChain, POLICY } from "./ModelChain.tsx";
import { ASK_PERSON, approvable, withApproval, withAsking, withNames } from "./people.ts";
import { accessLabel, accessOf, homeName } from "./projects.ts";
import type { AgentTab } from "./route.ts";
import {
  AgentMark,
  ago,
  button,
  Chip,
  card,
  field,
  Label,
  Menu,
  type Option,
  Page,
  PickMany,
  Problem,
  primary,
  Section,
  Switch,
  Tabs,
  textLink,
  took,
} from "./ui.tsx";

export type Tab = AgentTab;

/** Approvals on calls to single agents: each holds the agent tool's calls to the agent it names. */
const callApprovals = (spec: AgentSpec) =>
  (spec.approvals ?? []).filter((t) => t.startsWith("tag_"));

/** People and roles, typed freely: the draft keeps what is typed, the spec keeps the names in it. */
function NamesField({
  names,
  disabled,
  onChange,
}: {
  names?: string[];
  disabled: boolean;
  onChange: (text: string) => void;
}) {
  const [text, setText] = useState((names ?? []).join("\n"));
  return (
    <textarea
      className={`${field} min-h-16 font-aui-mono text-aui-sm`}
      value={text}
      disabled={disabled}
      spellCheck={false}
      onChange={(e) => {
        setText(e.target.value);
        onChange(e.target.value);
      }}
    />
  );
}

/** A rough size. Models count tokens differently, so this is a guide, not a bill. */
function approxTokens(text: string): number {
  return Math.ceil(text.length / 4);
}

/** Lines only in one side, in order: enough to see what a revision changed. */
function diff(before: string, after: string): { sign: "+" | "-" | " "; line: string }[] {
  const a = before.split("\n");
  const b = after.split("\n");
  // Longest common subsequence, by line.
  const lcs: number[][] = Array.from({ length: a.length + 1 }, () =>
    new Array<number>(b.length + 1).fill(0),
  );
  for (let i = a.length - 1; i >= 0; i--) {
    for (let j = b.length - 1; j >= 0; j--) {
      const row = lcs[i] as number[];
      row[j] =
        a[i] === b[j]
          ? ((lcs[i + 1] as number[])[j + 1] ?? 0) + 1
          : Math.max((lcs[i + 1] as number[])[j] ?? 0, row[j + 1] ?? 0);
    }
  }
  const out: { sign: "+" | "-" | " "; line: string }[] = [];
  let i = 0;
  let j = 0;
  while (i < a.length && j < b.length) {
    if (a[i] === b[j]) {
      out.push({ sign: " ", line: a[i] as string });
      i++;
      j++;
    } else if (((lcs[i + 1] as number[])[j] ?? 0) >= ((lcs[i] as number[])[j + 1] ?? 0)) {
      out.push({ sign: "-", line: a[i++] as string });
    } else {
      out.push({ sign: "+", line: b[j++] as string });
    }
  }
  while (i < a.length) out.push({ sign: "-", line: a[i++] as string });
  while (j < b.length) out.push({ sign: "+", line: b[j++] as string });
  return out;
}

function Diff({ before, after }: { before: string; after: string }) {
  const lines = diff(before, after);
  if (lines.every((l) => l.sign === " "))
    return <p className="text-aui-xs text-aui-muted">The prompt did not change.</p>;
  return (
    <pre className="aui-scroll max-h-72 overflow-auto rounded-aui border border-aui-border p-2 font-aui-mono text-aui-xs">
      {lines.map((l, n) => (
        <div
          key={n}
          className={
            l.sign === "+"
              ? "bg-aui-ok/15 text-aui-fg"
              : l.sign === "-"
                ? "bg-aui-bad/15 text-aui-fg"
                : "text-aui-muted"
          }
        >
          <span className="inline-block w-4 select-none">{l.sign}</span>
          {l.line || " "}
        </div>
      ))}
    </pre>
  );
}

const presets: { label: string; rule: (agent: string) => AlertRule }[] = [
  {
    label: "A run fails",
    rule: (a) => ({ name: `${a}-fails`, kind: "run_failed", agent: a, severity: "critical" }),
  },
  {
    label: "Half its runs fail in 15 minutes",
    rule: (a) => ({
      name: `${a}-failure-rate`,
      kind: "failure_rate",
      agent: a,
      severity: "critical",
      threshold: 50,
      window_seconds: 900,
      min_runs: 5,
    }),
  },
  {
    label: "A run takes over a minute",
    rule: (a) => ({
      name: `${a}-slow`,
      kind: "slow_run",
      agent: a,
      severity: "warning",
      threshold: 60,
    }),
  },
  {
    label: "An eval passes under 80%",
    rule: (a) => ({
      name: `${a}-quality`,
      kind: "eval_below",
      agent: a,
      severity: "warning",
      threshold: 80,
    }),
  },
];

/**
 * Which models an agent runs on, in order, and when it moves to the next. Each
 * agent owns its chain. An agent saved while following an old workspace policy
 * shows that policy's models here, and saving makes them its own.
 */
function ModelTab({
  spec,
  editable,
  policies,
  others,
  onChange,
}: {
  spec: AgentSpec;
  editable: boolean;
  policies: ModelPolicy[];
  others: Revision[];
  onChange: (patch: Partial<AgentSpec>) => void;
  onManage?: () => void;
}) {
  const onPolicy = spec.provider === POLICY;
  const followed = onPolicy ? policies.find((p) => p.name === spec.model) : undefined;
  const chain: ModelRef[] = onPolicy
    ? (followed?.models ?? [{ provider: "anthropic", model: defaultModel.anthropic ?? "" }])
    : [{ provider: spec.provider, model: spec.model }, ...(spec.fallbacks ?? [])];
  const fallbackOn = onPolicy ? (followed?.fallback_on ?? []) : (spec.fallback_on ?? []);
  const writeChain = (models: ModelRef[], when: string[]) => {
    const [primary, ...rest] = models;
    if (!primary) return;
    onChange({
      provider: primary.provider,
      model: primary.model,
      fallbacks: rest.length > 0 ? rest : undefined,
      fallback_on: rest.length > 0 && when.length > 0 ? when : undefined,
    });
  };
  const copyable = others.filter((a) => a.spec.provider !== POLICY);

  return (
    <div className="flex flex-col gap-6">
      <Section
        title="Models"
        hint="Which models answer, in order, and which errors move a call to the next. A run keeps answering when one provider does not."
        actions={
          editable && copyable.length > 0 ? (
            <Menu
              label="Copy from an agent"
              className="w-56"
              value=""
              options={[
                { value: "", label: "Copy from an agent" },
                ...copyable.map((a) => ({
                  value: a.spec.name,
                  label: a.spec.name,
                  hint: chainWords([a.spec, ...(a.spec.fallbacks ?? [])], a.spec.fallback_on),
                })),
              ]}
              onChange={(name) => {
                const from = others.find((a) => a.spec.name === name)?.spec;
                if (from) writeChain([from, ...(from.fallbacks ?? [])], from.fallback_on ?? []);
              }}
            />
          ) : undefined
        }
      >
        {onPolicy && (
          <p className="text-aui-xs text-aui-muted">
            {followed
              ? `These models come from the shared chain "${spec.model}" this agent used to follow. Saving keeps them as its own.`
              : `This agent followed a shared chain named "${spec.model}" that no longer exists. Pick its models below.`}
          </p>
        )}
        <ModelChain
          models={chain}
          fallbackOn={fallbackOn}
          editable={editable}
          onChange={writeChain}
        />
        <Label
          text="Reasoning effort"
          hint="How hard the first model thinks. Sent only when that model takes it; a fallback answers at its default."
        >
          <select
            className={`${field} sm:w-56`}
            aria-label="Reasoning effort"
            value={spec.effort ?? ""}
            disabled={!editable}
            onChange={(e) => onChange({ effort: e.target.value || undefined })}
          >
            <option value="">Model's default</option>
            {[...new Set([...EFFORTS, ...(spec.effort ? [spec.effort] : [])])].map((e) => (
              <option key={e} value={e}>
                {e.charAt(0).toUpperCase() + e.slice(1)}
              </option>
            ))}
          </select>
        </Label>
      </Section>
      <p className="text-aui-xs text-aui-muted">
        Each call is billed at the price of the model that answered it. Temperature, token limits
        and step limits are not settings yet: the agent spec this platform runs on has no fields for
        them.
      </p>
    </div>
  );
}

/** The reasoning efforts an agent's editor offers; a saved one outside them stays listed. */
const EFFORTS = ["low", "medium", "high"];

/** What is wrong with an agent's models, before the server says so. */
export function policyProblem(spec: AgentSpec, policies: ModelPolicy[] = []): string | null {
  if (spec.provider === POLICY)
    return policies.some((p) => p.name === spec.model)
      ? null
      : `The workspace has no policy named ${spec.model}.`;
  return chainProblem(
    [{ provider: spec.provider, model: spec.model }, ...(spec.fallbacks ?? [])],
    spec.fallback_on ?? [],
  );
}

/** Why a built-in agent's editor is read only. */
export const BUILTIN_LOCKED =
  "This agent is built in. The platform provides it, so its prompt and setup cannot be changed. Create a new agent to make your own.";

/** One agent: its prompt, its setup, how it is measured, and its history. */
export function AgentDetail({
  operations,
  onBuilderSession,
  revision,
  tab: wanted = "prompt",
  onTab,
  agents,
  sources,
  suites,
  rules,
  alerts,
  stats,
  workspaceDefault = "",
  tree = [],
  home,
  personalProject,
  canWrite,
  canGovern = false,
  builtin = false,
  canEval,
  canAlert,
  canKnowledge = false,
  onBack,
  onChanged,
  onRulesChanged,
  onSuitesChanged,
  onSourcesChanged,
  onOpenRun,
  onOpenSource,
  onOpenAlerts,
  onOpenTask,
  onHomed,
  policies = [],
  onManagePolicies,
  seed,
  viewer,
}: {
  operations?: ReactNode;
  onBuilderSession?: (id: string) => void;
  /** null creates a new agent. */
  revision: Revision | null;
  /** The tab on screen. The address holds it, so a link can open any tab. */
  tab?: Tab;
  onTab?: (next: Tab) => void;
  agents: Revision[];
  sources: Source[];
  suites: SuiteView[];
  rules: AlertRule[];
  alerts: Alert[];
  stats: Group | undefined;
  /** Who answers a message that tags no agent in the workspace itself. */
  workspaceDefault?: string;
  /** The project tree, for the title of the project whose members see the agent. */
  tree?: ProjectView[];
  /** The project a saved agent is homed in, which says who sees it. None, or "~", is the workspace. */
  home?: string;
  /** The signed-in person's Personal project: an agent homed there is private to them. */
  personalProject?: string;
  canWrite: boolean;
  /** Holds projects:govern, which homing out of a company project needs. */
  canGovern?: boolean;
  /** Provided by the platform. It can be read and run, and its memory kept, but its spec is not saved here. */
  builtin?: boolean;
  canEval: boolean;
  canAlert: boolean;
  /** May create knowledge sources, to attach a new one from here. */
  canKnowledge?: boolean;
  /** Kept for callers; projects scope alert rules once rules take one. */
  projects?: string[];
  onBack: () => void;
  onChanged: (name: string) => void;
  onRulesChanged: () => void;
  onSuitesChanged: () => void;
  onSourcesChanged?: () => void;
  onOpenRun: (id: string) => void;
  onOpenSource?: (name: string) => void;
  onOpenAlerts?: () => void;
  /** Open a task, from an approval waiting in it. */
  onOpenTask?: (thread: string) => void;
  /** Who sees the agent changed. */
  onHomed?: () => void;
  /** The workspace's model policies an agent can follow. */
  policies?: ModelPolicy[];
  onManagePolicies?: () => void;
  /** What to show before the revisions and memory are fetched, for the gallery and tests. */
  seed?: { history?: Revision[]; notes?: Note[] };
  /** The signed-in person's subject, whom the approval queue offers a note only where a run takes one from them. */
  viewer?: string;
}) {
  const saved: AgentSpec = revision?.spec ?? {
    name: "",
    description: "",
    // No model until one is picked: an agent runs only on a provider someone set up.
    provider: "",
    model: "",
    system_prompt: "",
    access: { spawn: [] },
  };
  const [spec, setSpec] = useState<AgentSpec>(saved);
  // A tab that needs a saved agent falls back to the prompt until there is one.
  const [own, setOwn] = useState<Tab>(wanted);
  const asked = onTab ? wanted : own;
  const tab: Tab =
    revision || ["prompt", "model", "access", "context"].includes(asked) ? asked : "prompt";
  const setTab = (next: Tab) => (onTab ? onTab(next) : setOwn(next));
  const [newSource, setNewSource] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [history, setHistory] = useState<Revision[]>(seed?.history ?? []);
  const [notes, setNotes] = useState<Note[]>(seed?.notes ?? []);
  const [note, setNote] = useState("");
  const [picked, setPicked] = useState<Set<string>>(() => new Set());
  const set = (patch: Partial<AgentSpec>) => setSpec((s) => ({ ...s, ...patch }));
  const name = revision?.spec.name ?? "";
  // The server refuses a built-in's revision, so the editor does not offer one.
  const editable = canWrite && !(builtin && revision);

  // A new revision of the same agent replaces the draft with what was saved.
  // biome-ignore lint/correctness/useExhaustiveDependencies: reset when the saved revision changes
  useEffect(() => {
    setSpec(saved);
    setProblem(null);
  }, [revision?.spec.name, revision?.rev]);

  // biome-ignore lint/correctness/useExhaustiveDependencies: reload when a revision lands
  useEffect(() => {
    if (!name) return setHistory([]);
    let live = true;
    api
      .history(name)
      .then((h) => live && setHistory(h))
      .catch(() => live && setHistory([]));
    return () => {
      live = false;
    };
  }, [name, revision?.rev]);

  const loadNotes = () => {
    if (name)
      api
        .memory(name)
        .then(setNotes)
        .catch(() => setNotes([]));
  };
  // biome-ignore lint/correctness/useExhaustiveDependencies: reload when the agent changes or its tab is opened
  useEffect(loadNotes, [name, tab]);

  const kept = notes.filter((n) => n.status === "approved");
  const proposed = notes.filter((n) => n.status === "pending");

  const dirty = useMemo(() => JSON.stringify(spec) !== JSON.stringify(saved), [spec, saved]);
  const myRules = rules.filter((r) => r.agent === name);
  const myOpen = alerts.filter((a) => a.agent === name && !a.acked_by).length;
  const mySuites = suites.filter((s) => s.target === "agent" && s.subject === name);
  const spawn = spec.access?.spawn ?? [];
  const reads = spec.sources ?? [];
  // A caller is only offered the agents whose own policy would take its call.
  const reach = useReach(revision ? name : null, revision?.rev);
  const reachOf = new Map((reach ?? []).map((r) => [r.agent, r]));
  const callable = agents.filter(
    (a) =>
      a.spec.name !== spec.name && (reach === null || reachOf.get(a.spec.name)?.admits !== false),
  );
  const refusing =
    reach === null
      ? []
      : agents.filter(
          (a) => a.spec.name !== spec.name && reachOf.get(a.spec.name)?.admits === false,
        );
  const [waiting, setWaiting] = useState(0);
  // The queue's count shows on its tab before the tab is opened.
  useEffect(() => {
    if (!name) return;
    let live = true;
    callGovernance
      .approvalQueue(name, "open")
      .then((r) => live && setWaiting(r.messages.length))
      .catch(() => {});
    return () => {
      live = false;
    };
  }, [name]);
  const heldCalls = callApprovals(spec);

  const save = async () => {
    setSaving(true);
    setProblem(null);
    try {
      // Message choices are the workspace's now (Settings, Model providers); an agent's
      // own, from before, are kept as they are for one release.
      const draft = spec;
      const next = revision
        ? await api.reviseAgent(draft, revision.rev)
        : await api.createAgent(draft);
      onChanged(next.spec.name);
    } catch (e) {
      setProblem(e instanceof ApiError ? e.message : "The agent could not be saved.");
    } finally {
      setSaving(false);
    }
  };

  const restore = async (old: Revision) => {
    if (!revision) return;
    setProblem(null);
    try {
      const next = await api.reviseAgent(old.spec, revision.rev);
      onChanged(next.spec.name);
    } catch (e) {
      setProblem(e instanceof ApiError ? e.message : "The revision could not be restored.");
    }
  };

  const addRule = (rule: AlertRule) => {
    setProblem(null);
    api
      .createAlertRule(rule)
      .then(onRulesChanged)
      .catch((e) =>
        setProblem(e instanceof ApiError ? e.message : "The rule could not be created."),
      );
  };

  const ended = stats ? stats.completed + stats.failed : 0;

  const tabs: { id: Tab; label: string; count?: number }[] = [
    { id: "prompt", label: "Prompt" },
    { id: "model", label: "Model" },
    { id: "access", label: "Access", count: spawn.length },
    ...(revision ? [{ id: "queue" as const, label: "Queue", count: waiting }] : []),
    { id: "context", label: "Context", count: reads.length },
    ...(revision
      ? ([
          { id: "memory", label: "Memory", count: proposed.length },
          { id: "evals", label: "Evals", count: mySuites.length },
          { id: "alerts", label: "Rules", count: myRules.length },
          { id: "revisions", label: "Revisions", count: history.length },
        ] as const)
      : []),
  ];

  return (
    <Page
      back={{ label: "All agents", onClick: onBack }}
      icon={<AgentMark name={spec.name || "?"} size={32} />}
      title={revision ? name : spec.name || "New agent"}
      meta={
        revision ? (
          <>
            <span>Revision {revision.rev}</span>
            <span>
              {revision.spec.provider} / {revision.spec.model}
            </span>
            <span>
              Saved {ago(revision.created_at)} by {revision.created_by_name || revision.created_by}
            </span>
            {workspaceDefault === name && <Chip>{defaultCopy.workspaceDefault}</Chip>}
          </>
        ) : (
          <span>Not saved yet</span>
        )
      }
      actions={
        editable ? (
          <>
            {revision && onBuilderSession && (
              <BuilderButton name={name} onOpen={onBuilderSession} />
            )}
            {dirty && <span className="text-aui-xs text-aui-muted">Unsaved changes</span>}
            {dirty && revision && (
              <button type="button" className={button} onClick={() => setSpec(saved)}>
                Discard
              </button>
            )}
            <button
              type="button"
              className={primary}
              disabled={!dirty || saving || policyProblem(spec, policies) !== null}
              onClick={save}
            >
              {revision ? `Save as Revision ${revision.rev + 1}` : "Create Agent"}
            </button>
          </>
        ) : undefined
      }
      tabs={<Tabs label="Agent sections" value={tab} onChange={setTab} tabs={tabs} />}
    >
      {operations}
      {revision && (tab === "prompt" || tab === "evals") && <AgentHealth name={name} />}
      {revision && tab === "prompt" && (
        <dl className="grid grid-cols-2 gap-3 sm:grid-cols-5">
          {[
            ["Runs, 24h", String(stats?.runs ?? 0)],
            [
              "Success",
              ended ? `${Math.round((100 * (stats?.completed ?? 0)) / ended)}%` : "No data",
            ],
            ["Median", stats?.p50_ms ? took(stats.p50_ms) : "–"],
            ["Open alerts", String(myOpen)],
            ["Evals", String(mySuites.length)],
          ].map(([label, value]) => (
            <div key={label} className={`${card} py-3`}>
              <dt className="text-aui-xs text-aui-muted">{label}</dt>
              <dd className="text-aui-lg font-semibold tabular-nums text-aui-fg">{value}</dd>
            </div>
          ))}
        </dl>
      )}

      {builtin && revision && (
        <p data-testid="agent-locked" className={`${card} text-aui-sm text-aui-muted`}>
          {BUILTIN_LOCKED}
        </p>
      )}

      {heldCalls.length > 0 && (
        <div
          data-testid="agent-held-calls"
          className={`${card} flex flex-wrap items-center justify-between gap-3`}
        >
          <p className="min-w-0 flex-1 text-aui-sm text-aui-fg">
            This agent asks for approval before calling{" "}
            {heldCalls.map((t) => t.slice(4)).join(", ")}. Each of those calls waits until a person
            approves it.
          </p>
          {editable && (
            <button
              type="button"
              className={button}
              onClick={() =>
                set({ approvals: (spec.approvals ?? []).filter((t) => !t.startsWith("tag_")) })
              }
            >
              Remove It
            </button>
          )}
        </div>
      )}

      <Problem message={problem} />

      {tab === "prompt" && (
        <section className="flex flex-col gap-3">
          {!revision && (
            <Label text="Name" hint="Letters, digits and dashes. It cannot change later.">
              <input
                className={field}
                value={spec.name}
                required
                onChange={(e) => set({ name: e.target.value })}
              />
            </Label>
          )}
          <Label
            text="Description"
            hint="One or two lines on what this agent is for. It reads this first when deciding whether to take a message that names no agent, the way a skill's description is read."
          >
            <textarea
              className={`${field} min-h-14`}
              value={spec.description ?? ""}
              disabled={!editable}
              maxLength={400}
              onChange={(e) => set({ description: e.target.value })}
            />
          </Label>
          <Switch
            label="Only when tagged"
            hint="On, it answers only a message that names it, and is never asked about one that names no agent."
            checked={spec.tagged_only === true}
            disabled={!editable}
            onChange={(on) => set({ tagged_only: on || undefined })}
          />
          <div className="flex flex-col gap-1">
            <div className="flex items-baseline justify-between">
              <label htmlFor="agent-prompt" className="text-aui-xs font-medium text-aui-muted">
                System prompt
              </label>
              <span className="text-aui-2xs tabular-nums text-aui-muted">
                {spec.system_prompt.split("\n").length}{" "}
                {spec.system_prompt.includes("\n") ? "lines" : "line"},{" "}
                {spec.system_prompt.length.toLocaleString()} characters, about{" "}
                {approxTokens(spec.system_prompt).toLocaleString()} tokens
              </span>
            </div>
            <textarea
              id="agent-prompt"
              className={`${field} humagen-glass min-h-[22rem] resize-y font-aui-mono text-aui-sm leading-relaxed`}
              value={spec.system_prompt}
              disabled={!editable}
              spellCheck={false}
              placeholder="Who the agent is, what it is for, how it should answer, and what it must never do."
              onChange={(e) => set({ system_prompt: e.target.value })}
            />
          </div>
          {revision && dirty && spec.system_prompt !== saved.system_prompt && (
            <details className={card} open>
              <summary className="cursor-pointer text-aui-sm text-aui-fg">
                Changes since revision {revision.rev}
              </summary>
              <div className="pt-2">
                <Diff before={saved.system_prompt} after={spec.system_prompt} />
              </div>
            </details>
          )}
          {revision && (
            <p className="text-aui-xs text-aui-muted">
              Runs and evals use the saved revision. Save your changes, then run the evals to
              compare this revision with the last.
            </p>
          )}
        </section>
      )}

      {tab === "model" && (
        <ModelTab
          spec={spec}
          editable={editable}
          policies={policies}
          others={agents.filter((a) => a.spec.name !== spec.name)}
          onChange={(patch) => set(patch)}
          onManage={onManagePolicies}
        />
      )}

      {tab === "access" && (
        <div className="flex flex-col gap-8">
          <p className="text-aui-xs text-aui-muted">
            Agent calls and approval settings below are saved with{" "}
            {revision ? `Save as Revision ${revision.rev + 1}` : "Create Agent"}. Who sees it and
            Who may call it have their own save controls and take effect immediately.
          </p>
          {revision && (
            <WhoSeesIt
              name={name}
              home={home}
              personalProject={personalProject}
              tree={tree}
              builtin={builtin}
              canChange={canWrite && !builtin}
              canGovern={canGovern}
              onChanged={() => onHomed?.()}
            />
          )}
          <Section
            title="Agents it may call"
            hint="The agents this one may hand work to. Only agents whose own policy takes its calls are offered. Depth, fan-out and budget limits still apply to every call."
          >
            <PickMany
              label="Agents it may call"
              add="Add Agent"
              disabled={!editable}
              empty={
                callable.length === 0
                  ? "No other agents exist yet."
                  : "It calls no one. It does every task itself."
              }
              value={spawn}
              options={callable.map((a) => ({
                value: a.spec.name,
                label: a.spec.name,
                hint:
                  reachOf.get(a.spec.name)?.approval === "required"
                    ? `Each call waits for approval. ${a.spec.description ?? ""}`.trim()
                    : a.spec.description || undefined,
                mark: <AgentMark name={a.spec.name} size={14} />,
              }))}
              onChange={(next) => set({ access: { ...spec.access, spawn: next } })}
            />
            {refusing.length > 0 && (
              <p className="text-aui-xs text-aui-muted">
                Not open to {name}: {refusing.map((a) => a.spec.name).join(", ")}. Their own call
                policy decides.
              </p>
            )}
          </Section>

          {revision && (
            <Section
              title="Who may call it"
              hint="This agent decides who may hand it work and when a call waits for a person. It applies from the moment it is saved, not with the next revision."
            >
              <CallPolicyEditor agent={name} agents={agents} canWrite={canWrite} />
            </Section>
          )}

          <Section
            title="Personal connections"
            hint="Use only the tools that the person starting the work has connected and approved. This does not share their credentials or give this agent access to another person's connections."
          >
            <Switch
              label="Use the requester's approved connection tools"
              hint="Saved with this agent's next revision. Workspace policy and the person's current tool approval still apply to every call."
              checked={(spec.tools ?? []).some(
                (tool) => tool === "person_tools" || tool === "mcp:person_tools:write",
              )}
              disabled={!editable}
              onChange={(on) =>
                setSpec((current) => ({
                  ...current,
                  tools: [
                    ...(current.tools ?? []).filter(
                      (tool) => tool !== "person_tools" && tool !== "mcp:person_tools:write",
                    ),
                    ...(on ? ["person_tools"] : []),
                  ],
                  ...(!on
                    ? {
                        approvals: current.approvals?.filter(
                          (tool) => tool !== "person_tools" && tool !== "mcp:person_tools:write",
                        ),
                      }
                    : {}),
                }))
              }
            />
            <a href="/connections" className={textLink}>
              Review Connections
            </a>
          </Section>

          <Section
            title="People in the loop"
            hint="A question or an approval reaches the inboxes of the people who may answer it. Runs started before you save keep the rules they started with."
          >
            <div className={`${card} flex flex-col gap-3`}>
              <Switch
                label="May ask a person"
                hint="The agent can ask someone to choose or reply, and waits for the answer."
                checked={(spec.tools ?? []).includes(ASK_PERSON)}
                disabled={!editable}
                onChange={(on) => setSpec((s) => withAsking(s, on))}
              />
              {approvable(spec)
                .filter(
                  (t) =>
                    !t.startsWith("tag_") && t !== "person_tools" && t !== "mcp:person_tools:write",
                )
                .map((tool) => (
                  <Switch
                    key={tool}
                    label={`Approve ${tool} first`}
                    hint="Each use waits until a person approves it. Nobody answering in time denies it."
                    checked={(spec.approvals ?? []).includes(tool)}
                    disabled={!editable}
                    onChange={(on) => setSpec((s) => withApproval(s, tool, on))}
                  />
                ))}
              <Label
                text="Who may approve"
                hint="People and roles, one per line, such as role:operator. Empty means anyone but the person who asked."
              >
                <NamesField
                  key={`approvers-${revision?.rev ?? 0}`}
                  names={spec.approvers}
                  disabled={!editable}
                  onChange={(text) => setSpec((s) => withNames(s, "approvers", text))}
                />
              </Label>
              <Switch
                label="The person who asked may approve"
                hint="Off, an approval is always someone else's."
                checked={spec.self_approval === true}
                disabled={!editable}
                onChange={(on) => set({ self_approval: on || undefined })}
              />
              <Label
                text="Who may answer its questions"
                hint="People and roles, one per line. Empty means anyone in the workspace."
              >
                <NamesField
                  key={`answerers-${revision?.rev ?? 0}`}
                  names={spec.answerers}
                  disabled={!editable}
                  onChange={(text) => setSpec((s) => withNames(s, "answerers", text))}
                />
              </Label>
            </div>
          </Section>
        </div>
      )}

      {tab === "queue" && revision && (
        <ApprovalQueue agent={name} viewer={viewer} onOpenTask={onOpenTask} onCount={setWaiting} />
      )}

      {tab === "context" && revision && (
        <AgentDefaultSkills
          agent={revision.spec.name}
          home={home ?? ""}
          agentRevision={revision.rev}
          canWrite={canWrite}
        />
      )}
      {tab === "context" && (
        <Section
          title="Context"
          hint="The sources this agent reads from. A source is shared: attaching it here does not copy it."
        >
          <PickMany
            label="Sources it reads"
            add="Attach Source"
            disabled={!editable}
            empty={sources.length === 0 ? "No sources exist yet." : "It reads no sources."}
            value={reads.map((r) => r.name)}
            options={sources.map((k) => ({
              value: k.name,
              label: k.name,
              hint:
                k.description || `${k.documents} ${k.documents === 1 ? "document" : "documents"}`,
            }))}
            onChange={(next) =>
              set({
                sources: next.map(
                  (n) => reads.find((r) => r.name === n) ?? { name: n, uri: `knowledge://${n}` },
                ),
              })
            }
          />
          {onOpenSource && reads.length > 0 && (
            <p className="flex flex-wrap gap-x-3 text-aui-xs text-aui-muted">
              Open:
              {reads.map((r) => (
                <button
                  key={r.name}
                  type="button"
                  className="underline hover:text-aui-fg"
                  onClick={() => onOpenSource(r.name)}
                >
                  {r.name}
                </button>
              ))}
            </p>
          )}
          {editable && canKnowledge && (
            <form
              className="flex flex-wrap gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                const chosen = newSource.trim();
                setProblem(null);
                api
                  .createSource(chosen, "")
                  .then(() => {
                    setNewSource("");
                    onSourcesChanged?.();
                    set({ sources: [...reads, { name: chosen, uri: `knowledge://${chosen}` }] });
                  })
                  .catch((err) =>
                    setProblem(
                      err instanceof ApiError ? err.message : "The source could not be created.",
                    ),
                  );
              }}
            >
              <input
                aria-label="New source name"
                className={`${field} h-8 max-w-xs flex-1`}
                placeholder="Or name a new source"
                value={newSource}
                onChange={(e) => setNewSource(e.target.value)}
              />
              <button type="submit" className={button} disabled={!newSource.trim()}>
                Create and Attach
              </button>
            </form>
          )}
          <p className="text-aui-xs text-aui-muted">
            Attaching a source records the intent and its evals run against it. Retrieval inside a
            run is not wired yet. Save the agent to keep a change.
          </p>
        </Section>
      )}

      {tab === "memory" && revision && (
        <section className="flex flex-col gap-3">
          <p className="max-w-xl text-aui-sm text-aui-muted">
            What {name} carries from one conversation to the next. Every note is sent with each
            turn, so keep them few and lasting.
          </p>
          {canWrite && (
            <form
              className="flex gap-2"
              onSubmit={(e) => {
                e.preventDefault();
                setProblem(null);
                api
                  .addMemory(name, note)
                  .then(() => {
                    setNote("");
                    loadNotes();
                  })
                  .catch((err) =>
                    setProblem(
                      err instanceof ApiError ? err.message : "The note could not be saved.",
                    ),
                  );
              }}
            >
              <input
                aria-label="Something to remember"
                className={field}
                value={note}
                maxLength={300}
                required
                placeholder="A lasting fact, such as: Releases go out on Tuesdays."
                onChange={(e) => setNote(e.target.value)}
              />
              <button type="submit" className={primary}>
                Remember
              </button>
            </form>
          )}
          {proposed.length > 0 && (
            <div className={`${card} flex flex-col gap-2`}>
              <h3 className="text-aui-sm font-medium text-aui-fg">
                Proposed by the memory agent ({proposed.length})
              </h3>
              <p className="text-aui-xs text-aui-muted">
                Not in use yet. Tick what {name} should remember, then keep it. Discard the rest.
              </p>
              <ul>
                {proposed.map((n) => (
                  <li key={n.id} className="border-aui-border border-t py-2 first:border-t-0">
                    <label className="flex items-start gap-2.5 text-aui-sm text-aui-fg">
                      <input
                        type="checkbox"
                        className="mt-1"
                        disabled={!canWrite}
                        checked={picked.has(n.id)}
                        onChange={(e) =>
                          setPicked((p) => {
                            const next = new Set(p);
                            if (e.target.checked) next.add(n.id);
                            else next.delete(n.id);
                            return next;
                          })
                        }
                      />
                      <span className="min-w-0">
                        <span className="block">{n.text}</span>
                        <span className="block text-aui-xs text-aui-muted">
                          Proposed {ago(n.at)}
                        </span>
                      </span>
                    </label>
                  </li>
                ))}
              </ul>
              {canWrite && (
                <div className="flex flex-wrap justify-end gap-2">
                  <button
                    type="button"
                    className={button}
                    onClick={() =>
                      Promise.all(
                        proposed
                          .filter((n) => picked.size === 0 || picked.has(n.id))
                          .map((n) => api.removeMemory(name, n.id)),
                      )
                        .then(() => {
                          setPicked(new Set());
                          loadNotes();
                        })
                        .catch(() => {})
                    }
                  >
                    {picked.size > 0 ? `Discard ${picked.size} Selected` : "Discard All"}
                  </button>
                  <button
                    type="button"
                    className={primary}
                    disabled={picked.size === 0}
                    onClick={() =>
                      api
                        .approveMemory(name, [...picked])
                        .then(() => {
                          setPicked(new Set());
                          loadNotes();
                        })
                        .catch((err) =>
                          setProblem(
                            err instanceof ApiError ? err.message : "Those could not be kept.",
                          ),
                        )
                    }
                  >
                    Keep {picked.size > 0 ? `${picked.size} Selected` : "Selected"}
                  </button>
                </div>
              )}
            </div>
          )}
          {kept.length === 0 ? (
            <p className={`${card} text-aui-sm text-aui-muted`}>
              {name} remembers nothing yet. Write a note here, or choose a memory agent under
              Settings to have notes proposed after each turn.
            </p>
          ) : (
            <ul className="humagen-glass overflow-hidden rounded-aui-lg">
              {kept.map((n) => (
                <li
                  key={n.id}
                  className="flex items-start justify-between gap-3 border-aui-border border-b px-4 py-2.5 last:border-b-0"
                >
                  <span className="min-w-0">
                    <span className="block text-aui-sm text-aui-fg">{n.text}</span>
                    <span className="block text-aui-xs text-aui-muted">
                      {n.source === "person"
                        ? `Written by ${n.by_name || n.by}`
                        : "Proposed by the memory agent and approved"}
                      , {ago(n.at)}
                    </span>
                  </span>
                  {canWrite && (
                    <button
                      type="button"
                      className={button}
                      onClick={() =>
                        api
                          .removeMemory(name, n.id)
                          .then(loadNotes)
                          .catch(() => {})
                      }
                    >
                      Forget
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {tab === "evals" && revision && (
        <Evals
          target="agent"
          subject={name}
          suites={suites}
          canWrite={canEval}
          onChanged={onSuitesChanged}
          onOpenRun={onOpenRun}
        />
      )}

      {tab === "alerts" && revision && (
        <section className="flex flex-col gap-3">
          <p className="max-w-xl text-aui-sm text-aui-muted">
            Rules that watch this agent. When one holds, an alert lands in the inbox and stays until
            someone acknowledges it.
          </p>
          {canAlert && (
            <div className={`${card} flex flex-col gap-2`}>
              <h3 className="text-aui-sm font-medium text-aui-fg">Alert me when</h3>
              <div className="flex flex-wrap gap-2">
                {presets.map((p) => {
                  const rule = p.rule(name);
                  const have = myRules.some((r) => r.name === rule.name);
                  return (
                    <button
                      key={rule.name}
                      type="button"
                      className={button}
                      disabled={have}
                      onClick={() => addRule(rule)}
                    >
                      {have ? `On: ${p.label}` : p.label}
                    </button>
                  );
                })}
              </div>
              <p className="text-aui-xs text-aui-muted">
                For other conditions, people to notify and cooldowns,{" "}
                {onOpenAlerts ? (
                  <button
                    type="button"
                    className="underline hover:text-aui-fg"
                    onClick={onOpenAlerts}
                  >
                    add a rule under Alerts
                  </button>
                ) : (
                  "add a rule under Alerts"
                )}
                .
              </p>
            </div>
          )}
          {myRules.length === 0 ? (
            <p className={`${card} text-aui-sm text-aui-muted`}>
              No rules watch this agent. Rules for any agent still apply.
            </p>
          ) : (
            <ul className="humagen-glass overflow-hidden rounded-aui-lg">
              {myRules.map((r) => (
                <li
                  key={r.name}
                  className="flex items-center justify-between gap-3 border-aui-border border-b px-4 py-2.5 last:border-b-0"
                >
                  <span className="min-w-0">
                    <span className="block text-aui-sm text-aui-fg">{r.name}</span>
                    <span className="block text-aui-xs text-aui-muted">
                      {r.kind.replace("_", " ")}
                      {r.threshold ? `, threshold ${r.threshold}` : ""}, {r.severity}
                    </span>
                  </span>
                  {canAlert && (
                    <button
                      type="button"
                      className={button}
                      onClick={() =>
                        api
                          .deleteAlertRule(r.name)
                          .then(onRulesChanged)
                          .catch(() => {})
                      }
                    >
                      Remove
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {tab === "revisions" && revision && (
        <ol className="flex flex-col gap-2">
          {history.map((h, i) => {
            const older = history[i + 1];
            return (
              <li key={h.rev} className={`${card} flex flex-col gap-2`}>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
                    <span className="text-aui-sm font-medium text-aui-fg">Revision {h.rev}</span>
                    {h.rev === revision.rev && <Chip>Current</Chip>}
                    <span className="text-aui-xs text-aui-muted">
                      {ago(h.created_at)} by {h.created_by_name || h.created_by}
                    </span>
                    <span className="text-aui-xs text-aui-muted">
                      {h.spec.provider} / {h.spec.model}
                    </span>
                  </div>
                  {editable && h.rev !== revision.rev && (
                    <button type="button" className={button} onClick={() => restore(h)}>
                      Restore as Revision {revision.rev + 1}
                    </button>
                  )}
                </div>
                {older ? (
                  <details>
                    <summary className="cursor-pointer text-aui-xs text-aui-muted">
                      What changed from revision {older.rev}
                    </summary>
                    <div className="pt-2">
                      <Diff before={older.spec.system_prompt} after={h.spec.system_prompt} />
                    </div>
                  </details>
                ) : (
                  <p className="text-aui-xs text-aui-muted">The first revision.</p>
                )}
              </li>
            );
          })}
        </ol>
      )}
    </Page>
  );
}

const accessWords = defaultCopy.agentAccess;

/**
 * Who sees an agent: Private (homed in the person's Personal project), the
 * workspace, or the members of the project it is homed in. Where it answers is
 * the allow lists' business, so a change here strands nothing.
 */
function WhoSeesIt({
  name,
  home,
  personalProject,
  tree,
  builtin,
  canChange,
  canGovern,
  onChanged,
}: {
  name: string;
  home?: string;
  personalProject?: string;
  tree: ProjectView[];
  builtin: boolean;
  canChange: boolean;
  canGovern: boolean;
  onChanged: () => void;
}) {
  const [problem, setProblem] = useState<string | null>(null);
  // A pick shows at once, until the agent list says where the agent is now.
  const [pending, setPending] = useState<{ from: string; to: string } | null>(null);
  // The platform's own agents are the workspace's.
  const access = accessOf(builtin ? undefined : home, personalProject, tree);
  const current = access.kind === "workspace" ? "" : access.project;
  const mine = homeName(personalProject);
  const options: Option[] = [];
  if (mine && !builtin) {
    options.push({ value: mine, label: accessWords.private, hint: accessWords.privateHint });
  }
  options.push({ value: "~", label: accessWords.workspace, hint: accessWords.workspaceHint });
  // Another project's members, or someone else's Personal project, is kept as it is.
  if (access.kind !== "workspace" && access.project !== mine) {
    options.push({
      value: access.project,
      label: accessLabel(access),
      hint:
        access.kind === "members" ? accessWords.membersHint(access.title) : accessWords.privateHint,
    });
  }
  const value = pending?.from === current ? pending.to : current || "~";
  const change = (to: string) => {
    if (to === value) return;
    setProblem(null);
    setPending({ from: current, to });
    api
      .setAgentHome(name, to)
      .then(onChanged)
      .catch((e) => {
        setPending(null);
        setProblem(e instanceof ApiError ? e.message : accessWords.failed);
      });
  };
  return (
    <Section title={accessWords.title}>
      <Problem message={problem} />
      <div className="max-w-sm">
        <Label text={accessWords.label}>
          <Menu
            label={accessWords.label}
            value={value}
            // Taking an agent out of a company project needs projects:govern.
            disabled={
              !canChange || (!canGovern && tree.find((p) => p.name === current)?.kind === "company")
            }
            options={options}
            onChange={change}
          />
        </Label>
      </div>
    </Section>
  );
}
