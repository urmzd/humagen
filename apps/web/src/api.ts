import { type AnswerRequest, answerPath } from "@warp/adapter/question";
import type {
  AgentHome,
  Person,
  ProjectAgentsInput,
  ProjectAgentsView,
  ProjectChange,
  ProjectDetail,
  ProjectEntry,
  ProjectInput,
  ProjectKnowledge,
  ProjectLimitInput,
  ProjectMembers,
  ProjectUpdate,
  ProjectView,
} from "../../../packages/client/src/generated/types.ts";
import type { BusEvent, Run } from "./adapter.ts";

// The project tree's shapes are the generated client's own, from
// api/openapi.yaml, so this file cannot drift from the contract on them.
export type {
  AgentHome,
  AgentOffer,
  AgentPlace,
  DefaultResolved,
  Person,
  Project,
  ProjectAccount,
  ProjectAgentsInput,
  ProjectAgentsView,
  ProjectChange,
  ProjectAccess,
  ProjectDetail,
  ProjectEntry,
  ProjectInput,
  ProjectKind,
  ProjectKindSpec,
  ProjectKnowledge,
  ProjectKnowledgeSource,
  ProjectLimit,
  ProjectLimitInput,
  ProjectMember,
  ProjectMembers,
  ProjectRole,
  ProjectStep,
  ProjectUpdate,
  ProjectView,
} from "../../../packages/client/src/generated/types.ts";

/** What the caller may do on a project: their permission and their role together. */
export type ProjectAction = ProjectView["actions"][number];

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

export interface Principal {
  subject: string;
  tenant: string;
  kind: "user" | "machine";
  name?: string;
  email?: string;
  roles?: string[];
}

export interface Me {
  principal: Principal;
  permissions: string[];
  /** The caller is an admin in a workspace nobody has set up yet. */
  first_admin?: boolean;
}

/** Why the caller may or may not do one thing. Only ever about the caller. */
export interface AccessDecision {
  permission: string;
  allowed: boolean;
  /** "scope" when a machine's token decided rather than the policy. */
  outcome: "allowed" | "denied" | "no match" | "scope";
  role?: string;
  path?: string[];
  rule?: string;
  /** The grant that gave `role`, when the sign-in kept it. */
  grant?: string;
  held: { role: string; grant?: string; group?: string }[];
  unknown?: string[];
}

export interface ModelRef {
  provider: string;
  model: string;
}

/** A workspace's named, ordered list of models and when a call moves down it. */
export interface ModelPolicy {
  name: string;
  description?: string;
  models: ModelRef[];
  fallback_on?: string[];
}

export interface AgentSpec {
  name: string;
  description?: string;
  model: string;
  provider: string;
  /** Tried in order when the primary model fails with an error another model could get past. */
  fallbacks?: ModelRef[];
  /** Which errors move a call to the next fallback. Absent means any recoverable error. */
  fallback_on?: string[];
  system_prompt: string;
  tools?: string[];
  sources?: { name: string; uri: string }[];
  access?: { spawn?: string[]; workspace_areas?: string[] };
  /** Tools whose every call waits for a person to approve it. */
  approvals?: string[];
  /** When set, the only people who may approve: subjects, or roles as `role:<name>`. */
  approvers?: string[];
  /** Lets the person who asked approve their own call. */
  self_approval?: boolean;
  /** When set, the only people who may answer a question the agent asks. */
  answerers?: string[];
}

export interface Revision {
  spec: AgentSpec;
  rev: number;
  hash: string;
  created_at: string;
  created_by: string;
  /** What whoever saved it is called. Shown in place of `created_by`, the subject. */
  created_by_name?: string;
}

export interface Schedule {
  name: string;
  expression: string;
  agent: string;
  text: string;
  /** Files every run the schedule raises under a project. */
  project?: string;
  /** What the schedule is for, at most 500 characters. The agent is not shown it. */
  description?: string;
  /**
   * Where the answer of each run goes: the creator's inbox, or posted in the
   * project (held for its owners where agent posts are reviewed). Absent, it
   * stays on the run.
   */
  deliver?: "inbox" | "project";
}

/**
 * The body of PUT /api/schedules/{name}: everything but the name, which
 * cannot change. Fields left out are cleared, not kept.
 */
export type ScheduleEdit = Omit<Schedule, "name">;

/** A schedule plus what makes it trustworthy: state, next fires, recent runs. */
export interface ScheduleView extends Schedule {
  paused: boolean;
  /** "file" while the platform file declares it: run and pause it here, change it in the file. */
  managed?: "file";
  created_by?: string;
  /** What the person who made it is called. Shown in place of `created_by`, the subject. */
  created_by_name?: string;
  /** Who last edited the schedule, and when. Absent until it is edited. */
  updated_by?: string;
  updated_at?: string;
  next: string[];
  recent: Run[];
}

export interface Group {
  name: string;
  runs: number;
  completed: number;
  failed: number;
  /** Runs stopped on request. Neither completed, failed nor in flight. */
  stopped: number;
  active: number;
  p50_ms: number;
  p95_ms: number;
  /** What the runs' own model calls used. Each run counts only its own calls. */
  usage?: { model_calls: number; tokens: number; spend: number; unpriced_calls: number; other_currency_runs: number };
}

export interface Bucket {
  start: string;
  completed: number;
  failed: number;
  stopped: number;
  active: number;
}

export interface Summary {
  since: string;
  until: string;
  bucket_ns: number;
  total: Group;
  buckets: Bucket[];
  by_agent: Group[];
  by_origin: Group[];
  slowest: Run[] | null;
  delegated: number;
}

export interface Entry {
  seq: number;
  run_id: string;
  agent: string;
  rev: number;
  origin: string;
  kind: string;
  at: string;
  detail?: string;
}

export interface Span {
  run_id: string;
  agent: string;
  kind: "run" | "attempt" | "tool" | "output" | "parked";
  name: string;
  start: string;
  end?: string;
  error?: string;
}

/** Every kind a rule can have. `budget` alerts come from billing policies, never from a rule. */
export type AlertKind = "run_failed" | "failure_rate" | "slow_run" | "eval_below" | "spend";

/** A rule: what it watches, how serious it is, and who hears about it. */
export interface AlertRule {
  name: string;
  kind: AlertKind;
  /** What the rule is for, at most 500 characters. */
  description?: string;
  /** Only this agent's runs. Absent means every agent. */
  agent?: string;
  /** Only runs filed under this project. Narrows together with agent. Not for eval_below. */
  project?: string;
  severity: "warning" | "critical";
  /** A percentage for failure_rate and eval_below, seconds for slow_run. Not for spend. */
  threshold?: number;
  /** For spend only, and required by it: the cost in micro-USD at which it fires. */
  threshold_micros?: number;
  /** How far back failure_rate and spend look, up to 30 days. */
  window_seconds?: number;
  min_runs?: number;
  /** Quiet for this long after raising, acknowledged or not, up to 30 days. */
  cooldown_seconds?: number;
  /** A paused rule is not evaluated. Absent means enabled. */
  paused?: boolean;
  /**
   * Who the inbox message is for: subjects, and roles as "role:<name>".
   * Absent or empty means everyone who may read alerts.
   */
  notify?: string[];
  created_by?: string;
  /** What the person who made it is called. Shown in place of `created_by`, the subject. */
  created_by_name?: string;
  created_at?: string;
  /** Who last replaced the rule, and when. Absent until it is replaced. */
  updated_by?: string;
  updated_at?: string;
}

/** Kept for callers: a rule of any kind is an AlertRule. */
export type AlertRuleConfig = AlertRule;

/**
 * The body of PUT /api/alert-rules/{name}: the whole rule but its name, which
 * cannot change. Fields left out are cleared, not kept. The server sets who
 * created and updated it.
 */
export type AlertRuleEdit = Omit<AlertRuleConfig, "name" | "created_by" | "created_by_name" | "created_at" | "updated_by" | "updated_at">;

export interface Alert {
  id: string;
  rule: string;
  kind: AlertRule["kind"];
  severity: AlertRule["severity"];
  agent?: string;
  /** The project of the run that raised the alert, if any. */
  project?: string;
  run_id?: string;
  suite?: string;
  title: string;
  body?: string;
  at: string;
  acked_by?: string;
  /** What whoever acknowledged it is called. Shown in place of `acked_by`, the subject. */
  acked_by_name?: string;
  acked_at?: string;
}

export interface Source {
  name: string;
  description?: string;
  documents: number;
  passages: number;
  /** Documents held but left out of search. */
  excluded: number;
  variants: { name: string; excluded: string[] }[];
  updated_at: string;
}

export interface Doc {
  id: string;
  title: string;
  uri?: string;
  bytes: number;
  passages: number;
  excluded: boolean;
  added_by?: string;
  added_at: string;
}

export interface Hit {
  source: string;
  document_id: string;
  title: string;
  uri?: string;
  index: number;
  text: string;
  score: number;
}

export interface ThreadSummary {
  id: string;
  title: string;
  /** The title is a name, not just the first message. */
  named: boolean;
  origin: string;
  project?: string;
  /** The project's channel it is in, `general` when its runs name none. */
  channel: string;
  agents: string[];
  /** The people who asked or posted in it, by subject. */
  people: string[];
  visibility: "shared" | "private";
  /** How many messages people have posted in it. */
  messages: number;
  turns: number;
  branches: number;
  active: number;
  failed: number;
  created_at: string;
  updated_at: string;
}

export interface Choice {
  agent: string;
  reason: string;
  matched?: string[];
}

/**
 * Workspace settings. Who answers a message that tags no agent is not one of
 * them: it is the workspace's default, on the root's agents.
 */
export interface Settings {
  history: "recent" | "none";
  history_limit: number;
  title_agent: string;
  memory_agent: string;
  memory_auto_approve: boolean;
  workspace_name?: string;
  /** Named model lists agents run on with provider "policy". */
  model_policies?: ModelPolicy[];
  /** Set once by the server when an admin first saves settings. */
  setup_at?: string;
  setup_by?: string;
}

/** A proposed memory, for an agent or for a project. */
export interface Proposal {
  agent?: string;
  project?: string;
  note: Note;
}

export interface PolicyScope {
  kind: "tenant" | "project" | "agent" | "client";
  name?: string;
}

/** The limits for one scope. A limit of 0 or absent means the scope sets none. */
export interface Policy {
  scope: PolicyScope;
  period: "day" | "month";
  max_runs?: number;
  max_concurrent?: number;
  /** Micro-USD a period, enforced. Only money priced in US dollars counts. */
  spend_limit?: number;
  max_lease?: number;
  /** Model tokens a period, enforced, priced or not. */
  max_tokens?: number;
  warn_at?: number;
  action: "block" | "approve";
  /** Who set it. Absent is an admin; "owner" is a tighten-only limit a project's owner set. */
  authority?: "admin" | "owner";
  version: number;
  updated_by?: string;
  /** What whoever last changed it is called. Shown in place of `updated_by`, the subject. */
  updated_by_name?: string;
  updated_at: string;
}

/** Where one of a client's secrets is. The secret itself is never sent. */
export interface SecretRef {
  set: boolean;
  /** "env:NAME" when the secret lives in the server's environment. */
  ref?: string;
}

/** A Slack app or GitHub installation the workspace answers through. */
export interface Client {
  id: string;
  name: string;
  surface: "slack" | "github";
  identity: { app_id?: string; team?: string; installation?: string; owners?: string[] };
  credentials: { token: SecretRef; signing_secret: SecretRef };
  default_agent?: string;
  /** The agents it may route to besides its default. "*" alone means any. */
  agents?: string[];
  bots?: Record<string, string>;
  slack?: { sessions?: boolean; commands?: { name: string; description: string; argument_hint?: string }[] };
  github?: {
    auto_labels?: Record<string, Record<string, string>>;
    checks?: boolean;
    guides?: boolean;
    mutations?: boolean;
    read_repositories?: string[];
    /** The app's own login. Once warp's GitHub connector reads it, a comment that mentions it and names no agent is answered by the default; until then it changes nothing. */
    login?: string;
  };
  /** Where the client files threads it starts outside a bound channel. Absent, the workspace's general. */
  home?: { project: string; channel?: string };
  enabled: boolean;
  /** Set on a client the server's configuration owns and the API cannot change. */
  managed?: string;
  version: number;
  /** The client's own policy, set through the policies API with a client scope. */
  limits?: Policy;
}

export interface ClientCaps {
  max: number;
  max_per_surface: number;
}

export interface Measure {
  used: number;
  limit: number;
  projected?: number;
}

/** One policy and where its scope stands against it. */
export interface ScopeUsage {
  scope: PolicyScope;
  period: "day" | "month";
  period_start: string;
  period_end: string;
  version: number;
  warn_at: number;
  action: "block" | "approve";
  runs: Measure;
  concurrent: Measure;
  /** Micro-USD priced in US dollars. A lower bound when calls had no price or another currency. */
  spend: Measure & { metered: boolean; unpriced_calls: number; other_costs?: { micros: number; currency: string }[] };
  tokens: Measure;
  max_lease: number;
  state: "ok" | "warn" | "over";
}

export interface PolicyChange {
  scope: PolicyScope;
  by: string;
  /** What whoever made the change is called. Shown in place of `by`, the subject. */
  by_name?: string;
  at: string;
  before?: Policy;
  after?: Policy;
}

export interface Note {
  id: string;
  text: string;
  source: "person" | "consolidated";
  /** Pending notes are proposals: not used until a person approves them. */
  status: "approved" | "pending";
  by?: string;
  /** What the person who wrote it is called. Shown in place of `by`, the subject. */
  by_name?: string;
  at: string;
}

/**
 * A tool that acts as you: an MCP provider reached with each person's own
 * credential. A value you stored is never sent back; a reference is shown,
 * since it names where the secret is kept and not the secret.
 */
export interface ToolCredential {
  provider: string;
  transport: "http" | "stdio";
  /** Where an http provider is reached, so you see where your credential goes. */
  endpoint?: string;
  /** How many of its tools the workspace accepted. */
  tools: number;
  connected: boolean;
  kind?: "reference" | "value";
  ref?: string;
  updated_at?: string;
}

/** The tools that act as you, and what this workspace lets people give for them. */
export interface ToolCredentials {
  providers: ToolCredential[];
  /** Whether you may store the credential itself with warp. */
  values: boolean;
  /** How the references you may give start. Empty means none can be given. */
  references: string[];
}

/** What you give for one tool: a reference or a value, exactly one. */
export type ToolCredentialInput = { ref: string } | { value: string };

export interface Preferences {
  display_name: string;
  theme: "system" | "light" | "dark";
  accent: string;
  has_photo: boolean;
  photo_version: number;
  /** When the person finished or skipped the first-run questions. Absent means not yet. */
  onboarded_at?: string;
  /** Conversations kept at the top of the sidebar, most recently pinned first. */
  pinned: string[];
  /** Projects whose announcements stay out of this person's own inbox. */
  muted: string[];
}

export interface Check {
  kind: "contains" | "not_contains" | "matches" | "equals" | "within_ms";
  value: string;
}

export interface EvalCase {
  id: string;
  input: string;
  checks?: Check[];
  expect?: string[];
}

export interface EvalResult {
  case_id: string;
  input: string;
  passed: boolean;
  output?: string;
  verdicts?: { check: string; passed: boolean; reason?: string }[];
  hits?: { title: string; score: number; expected: boolean }[];
  rank?: number;
  run_id?: string;
  duration_ms: number;
  error?: string;
}

export interface EvalRun {
  id: string;
  suite: string;
  target: "agent" | "knowledge";
  subject: string;
  rev?: number;
  status: "running" | "passed" | "failed" | "errored";
  passed: number;
  total: number;
  pass_rate: number;
  threshold: number;
  metrics?: Record<string, number>;
  results: EvalResult[];
  started_by?: string;
  started_at: string;
  ended_at?: string;
}

export interface Suite {
  name: string;
  description?: string;
  target: "agent" | "knowledge";
  subject: string;
  cases: EvalCase[];
  k?: number;
  pass_rate?: number;
}

export interface SuiteView extends Suite {
  runs: EvalRun[];
}

/** A project in a path: `~` is the workspace, which is the project named "". */
export function projectPath(name: string): string {
  return name === "" ? "~" : encodeURIComponent(name);
}

// Same-origin only. The session is an HttpOnly cookie the page cannot read,
// so there is no token here to leak.
async function call<T>(method: string, path: string, json?: unknown): Promise<T> {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: json === undefined ? undefined : { "Content-Type": "application/json" },
    body: json === undefined ? undefined : JSON.stringify(json),
  });
  if (res.status === 204) return undefined as T;
  const data = (await res.json().catch(() => ({}))) as { error?: string };
  if (!res.ok) throw new ApiError(res.status, data.error ?? `Request failed (${res.status})`);
  return data as T;
}

/** The editable part of a rule, for sending back with PUT /api/alert-rules/{name}. */
export function alertRuleEdit(rule: AlertRuleConfig): AlertRuleEdit {
  const edit: AlertRuleEdit & Partial<AlertRuleConfig> = { ...rule };
  delete edit.name;
  delete edit.created_by;
  delete edit.created_by_name;
  delete edit.created_at;
  delete edit.updated_by;
  delete edit.updated_at;
  return edit;
}

export const api = {
  me: () => call<Me>("GET", "/api/me"),
  explain: (permission: string) =>
    call<AccessDecision>("GET", `/api/access/explain?${new URLSearchParams({ permission })}`),
  agents: () => call<{ agents: Revision[] }>("GET", "/api/agents").then((r) => r.agents),
  /** Every agent, and the names of those the platform provides, which cannot be edited. */
  /**
   * Every agent, the platform's own by name, where each that lives in a
   * project lives, and which projects enable each ("~" is the workspace).
   */
  agentList: () =>
    call<{ agents: Revision[]; builtin?: string[]; homes?: Record<string, string>; enabled_in?: Record<string, string[]> }>("GET", "/api/agents"),
  /** Make an agent live in a project, "" for none: available there and below, and nowhere else. */
  setAgentHome: (agent: string, project: string) =>
    call<AgentHome>("PUT", `/api/agents/${encodeURIComponent(agent)}/home`, { project }),
  createAgent: (spec: AgentSpec) => call<Revision>("POST", "/api/agents", spec),
  reviseAgent: (spec: AgentSpec, expectedRev: number) =>
    call<Revision>("PUT", `/api/agents/${encodeURIComponent(spec.name)}`, {
      spec,
      expected_rev: expectedRev,
    }),
  runs: () => call<{ runs: Run[] }>("GET", "/api/runs").then((r) => r.runs),
  run: (id: string) => call<Run>("GET", `/api/runs/${encodeURIComponent(id)}`),
  tree: (id: string) =>
    call<{ runs: Run[] }>("GET", `/api/runs/${encodeURIComponent(id)}/tree`).then((r) => r.runs),
  /** `agent` "" leaves it out, so the default the conversation's place inherits answers. */
  createRun: (agent: string, text: string, opts: { after?: string; project?: string } = {}) =>
    call<Run>("POST", "/api/runs", { ...(agent ? { agent } : {}), text, ...opts }),
  threads: () => call<{ threads: ThreadSummary[] }>("GET", "/api/threads").then((r) => r.threads),
  /**
   * One page of conversations, newest first. `next` is the cursor for the page after it, null at the end, and absent from a server that does not page.
   * `project` keeps one project's own (`~` for the workspace's), and `within` a project and everything below it.
   */
  threadsPage: (q: {
    limit: number;
    before?: string;
    origin?: string;
    agent?: string;
    standing?: string;
    project?: string;
    within?: string;
  }) => {
    const params = new URLSearchParams({ limit: String(q.limit) });
    for (const k of ["before", "origin", "agent", "standing", "project", "within"] as const) if (q[k]) params.set(k, q[k]);
    return call<{ threads: ThreadSummary[]; next?: string | null }>("GET", `/api/threads?${params}`);
  },
  /** The agents' turns of a conversation, its newest page. `coworking.thread` reads people's messages too. */
  thread: (id: string) =>
    call<ThreadPage>("GET", `/api/threads/${encodeURIComponent(id)}`).then((r) =>
      r.turns.flatMap((t) => (t.run ? [t.run] : [])),
    ),
  /** Who @auto would pick for a message in a project, "" for the workspace. */
  pick: (q: string, project = "") =>
    call<Choice>("GET", `/api/pick?${new URLSearchParams(project ? { q, project } : { q })}`),
  settings: () => call<Settings>("GET", "/api/settings"),
  saveSettings: (s: Settings) => call<Settings>("PUT", "/api/settings", s),
  history: (name: string) =>
    call<{ revisions: Revision[] }>("GET", `/api/agents/${encodeURIComponent(name)}/history`).then((r) => r.revisions),

  memory: (agent: string) =>
    call<{ notes: Note[] }>("GET", `/api/agents/${encodeURIComponent(agent)}/memory`).then((r) => r.notes),
  addMemory: (agent: string, text: string) =>
    call<Note>("POST", `/api/agents/${encodeURIComponent(agent)}/memory`, { text }),
  approveMemory: (agent: string, ids: string[]) =>
    call<{ approved: number }>("POST", `/api/agents/${encodeURIComponent(agent)}/memory/approve`, { ids }),
  pendingMemory: () => call<{ proposals: Proposal[] }>("GET", "/api/memory/pending").then((r) => r.proposals),
  /** Stop a run and everything attached to it. Runs it delegated keep going. */
  stopRun: (id: string) => call<Run>("POST", `/api/runs/${encodeURIComponent(id)}/stop`),
  /** Answer the question a run is waiting on. Returns the question's inbox message, settled. */
  answer: (r: AnswerRequest) => call<unknown>("POST", answerPath(r), r.body),
  /** A run's events as they were published, for a run that is no longer streaming. */
  runEvents: (id: string) =>
    call<{ events: BusEvent[]; complete: boolean }>("GET", `/api/runs/${encodeURIComponent(id)}/events`),
  /** Everything recorded about a run and the runs under it, as one document. */
  manifest: (id: string) => call<unknown>("GET", `/api/runs/${encodeURIComponent(id)}/manifest`),
  /** Open and unread count what may need the reader; updates are announcements they have not read, which never grow the badge. */
  inboxCount: () => call<{ open: number; unread: number; updates: number }>("GET", "/api/inbox/count"),
  /** The id of the message an alert raised, or null when it raised none. */
  inboxFor: (alert: string) =>
    call<{ messages: { id: string }[] }>("GET", `/api/inbox?alert=${encodeURIComponent(alert)}`).then((r) => r.messages[0]?.id ?? null),
  clients: () => call<{ clients: Client[]; caps: ClientCaps }>("GET", "/api/clients"),
  /** Replace a client's record. Secrets are not sent, so the server keeps the ones it has. */
  saveClient: (c: Client, enabled: boolean) =>
    call<Client>("PUT", `/api/clients/${encodeURIComponent(c.id)}`, {
      name: c.name,
      surface: c.surface,
      identity: c.identity,
      default_agent: c.default_agent ?? "",
      agents: c.agents ?? [],
      bots: c.bots ?? {},
      slack: c.slack ?? null,
      github: c.github ?? null,
      home: c.home ?? null,
      enabled,
    }),
  deleteClient: (id: string) => call<void>("DELETE", `/api/clients/${encodeURIComponent(id)}`),
  /** Your own credentials for the tools that act as you. Nobody else's can be named. */
  toolCredentials: () => call<ToolCredentials>("GET", "/api/me/credentials"),
  connectTool: (provider: string, input: ToolCredentialInput) =>
    call<ToolCredential>("PUT", `/api/me/credentials/${encodeURIComponent(provider)}`, input),
  disconnectTool: (provider: string) => call<void>("DELETE", `/api/me/credentials/${encodeURIComponent(provider)}`),
  policies: () => call<{ policies: Policy[] }>("GET", "/api/policies").then((r) => r.policies),
  savePolicy: (p: Omit<Policy, "version" | "updated_at" | "updated_by" | "action"> & { action?: Policy["action"] }) =>
    call<Policy>("PUT", "/api/policies", p),
  deletePolicy: (scope: PolicyScope) =>
    call<void>("DELETE", `/api/policies?${new URLSearchParams({ kind: scope.kind, name: scope.name ?? "" })}`),
  policyHistory: () => call<{ changes: PolicyChange[] }>("GET", "/api/policies/history").then((r) => r.changes),
  usage: () => call<{ scopes: ScopeUsage[] }>("GET", "/api/usage").then((r) => r.scopes),
  /** The workspace's projects in tree order, each before its sub-projects. The workspace itself is not one of them. */
  projects: async () => {
    // Read a page at a time, at most the 500 a workspace holds.
    const all: ProjectView[] = [];
    let before: string | null = null;
    for (let page = 0; page < 5; page++) {
      const q = new URLSearchParams({ limit: "100" });
      if (before) q.set("before", before);
      const r: { projects: ProjectView[]; next: string | null } = await call("GET", `/api/projects?${q}`);
      all.push(...r.projects);
      before = r.next;
      if (!before) break;
    }
    return all;
  },
  /** One project, "" for the workspace: where it sits, its sub-projects and the limits that hold it. */
  project: (name: string) => call<ProjectDetail>("GET", `/api/projects/${projectPath(name)}`),
  createProject: (p: ProjectInput) => call<ProjectView>("POST", "/api/projects", p),
  /** Replace a project's title, description, account and instructions, as a whole, from the version read. */
  updateProject: (name: string, u: ProjectUpdate) => call<ProjectView>("PUT", `/api/projects/${projectPath(name)}`, u),
  /** What a project turns on, what is available there, and who answers untagged messages. "" is the workspace. */
  projectAgents: (name: string) => call<ProjectAgentsView>("GET", `/api/projects/${projectPath(name)}/agents`),
  setProjectAgents: (name: string, input: ProjectAgentsInput) =>
    call<ProjectAgentsView>("PUT", `/api/projects/${projectPath(name)}/agents`, input),
  /** One page of the directory, by handle. `next` is the cursor for the page after, null at the end. */
  people: (q: { q?: string; before?: string; limit?: number } = {}) => {
    const params = new URLSearchParams();
    if (q.q) params.set("q", q.q);
    if (q.before) params.set("before", q.before);
    if (q.limit) params.set("limit", String(q.limit));
    const query = params.toString();
    return call<{ people: Person[]; next: string | null }>("GET", `/api/people${query ? `?${query}` : ""}`);
  },
  deleteProject: (name: string) => call<void>("DELETE", `/api/projects/${projectPath(name)}`),
  /** Who holds a role in a project, and who owns it from above. "" is the workspace. */
  projectMembers: (name: string) => call<ProjectMembers>("GET", `/api/projects/${projectPath(name)}/members`),
  setProjectMembers: (name: string, version: number, members: ProjectEntry[]) =>
    call<ProjectMembers>("PUT", `/api/projects/${projectPath(name)}/members`, { version, members }),
  /** Put a project under another, "" for the top level. The parent is a name in the body, never encoded as a path is. */
  moveProject: (name: string, version: number, parent: string) =>
    call<ProjectView>("POST", `/api/projects/${projectPath(name)}/move`, { version, parent: parent === "" ? "~" : parent }),
  projectHistory: (name: string, before?: string) =>
    call<{ changes: ProjectChange[]; next: string | null }>(
      "GET",
      `/api/projects/${projectPath(name)}/history?${new URLSearchParams(before ? { limit: "50", before } : { limit: "50" })}`,
    ),
  /** A tighten-only limit, as an owner of a project above. */
  setProjectLimit: (name: string, limits: ProjectLimitInput) => call<Policy>("PUT", `/api/projects/${projectPath(name)}/limits`, limits),
  deleteProjectLimit: (name: string) => call<void>("DELETE", `/api/projects/${projectPath(name)}/limits`),
  postAnnouncement: (name: string, a: { subject: string; text: string; severity?: "info" | "warning" }) =>
    call<unknown>("POST", `/api/projects/${projectPath(name)}/announcements`, a),
  withdrawAnnouncement: (name: string, id: string) =>
    call<void>("DELETE", `/api/projects/${projectPath(name)}/announcements/${encodeURIComponent(id)}`),
  projectMemory: (project: string) =>
    call<{ notes: Note[] }>("GET", `/api/projects/${projectPath(project)}/memory`).then((r) => r.notes),
  addProjectMemory: (project: string, text: string) =>
    call<Note>("POST", `/api/projects/${projectPath(project)}/memory`, { text }),
  approveProjectMemory: (project: string, ids: string[]) =>
    call<{ approved: number }>("POST", `/api/projects/${projectPath(project)}/memory/approve`, { ids }),
  removeProjectMemory: (project: string, id: string) =>
    call<void>("DELETE", `/api/projects/${projectPath(project)}/memory/${encodeURIComponent(id)}`),
  removeMemory: (agent: string, id: string) =>
    call<void>("DELETE", `/api/agents/${encodeURIComponent(agent)}/memory/${encodeURIComponent(id)}`),

  preferences: () => call<Preferences>("GET", "/api/preferences"),
  savePreferences: (p: Pick<Preferences, "display_name" | "theme" | "accent"> & { onboarded?: boolean; pinned?: string[]; muted?: string[] }) =>
    call<Preferences>("PUT", "/api/preferences", p),
  removePhoto: () => call<Preferences>("DELETE", "/api/preferences/photo"),
  // The picture is sent as its own bytes. The server decides what it is.
  uploadPhoto: async (file: Blob) => {
    const res = await fetch("/api/preferences/photo", { method: "PUT", credentials: "same-origin", body: file });
    const data = (await res.json().catch(() => ({}))) as Preferences & { error?: string };
    if (!res.ok) throw new ApiError(res.status, data.error ?? "The picture could not be uploaded.");
    return data as Preferences;
  },

  suites: () => call<{ suites: SuiteView[] }>("GET", "/api/evals").then((r) => r.suites),
  createSuite: (s: Suite) => call<Suite>("POST", "/api/evals", s),
  saveSuite: (s: Suite) => call<Suite>("PUT", `/api/evals/${encodeURIComponent(s.name)}`, s),
  deleteSuite: (name: string) => call<void>("DELETE", `/api/evals/${encodeURIComponent(name)}`),
  runSuite: (name: string) => call<EvalRun>("POST", `/api/evals/${encodeURIComponent(name)}/run`),
  schedules: () => call<{ schedules: ScheduleView[] }>("GET", "/api/schedules").then((r) => r.schedules),
  createSchedule: (s: Schedule) => call<ScheduleView>("POST", "/api/schedules", s),
  /** Change a schedule. Its pause state, creator and recent runs are kept. */
  updateSchedule: (name: string, s: ScheduleEdit) =>
    call<ScheduleView>("PUT", `/api/schedules/${encodeURIComponent(name)}`, s),
  deleteSchedule: (name: string) => call<void>("DELETE", `/api/schedules/${encodeURIComponent(name)}`),
  pauseSchedule: (name: string, paused: boolean) =>
    call<ScheduleView>("POST", `/api/schedules/${encodeURIComponent(name)}/${paused ? "pause" : "resume"}`),
  runSchedule: (name: string) => call<Run>("POST", `/api/schedules/${encodeURIComponent(name)}/run`),

  analytics: (window: string) => call<Summary>("GET", `/api/analytics?window=${window}`),
  journal: (params: Record<string, string>) =>
    call<{ entries: Entry[] }>("GET", `/api/journal?${new URLSearchParams(params)}`).then((r) => r.entries),
  trace: (id: string) => call<{ runs: Run[]; spans: Span[] }>("GET", `/api/runs/${encodeURIComponent(id)}/trace`),

  alerts: () => call<{ alerts: Alert[] }>("GET", "/api/alerts").then((r) => r.alerts),
  ackAlert: (id: string) => call<Alert>("POST", `/api/alerts/${encodeURIComponent(id)}/ack`),
  alertRules: () => call<{ rules: AlertRule[] }>("GET", "/api/alert-rules").then((r) => r.rules),
  createAlertRule: (r: AlertRule) => call<AlertRule>("POST", "/api/alert-rules", r),
  /** Every rule, of every kind, spend included. */
  alertRuleConfigs: () => call<{ rules: AlertRuleConfig[] }>("GET", "/api/alert-rules").then((r) => r.rules),
  /** Create a rule of any kind, spend included. */
  createAlertRuleConfig: (r: AlertRuleConfig) => call<AlertRuleConfig>("POST", "/api/alert-rules", r),
  /** Replace a rule. Its creator, its alerts and its cooldown are kept. */
  updateAlertRule: (name: string, r: AlertRuleEdit) =>
    call<AlertRuleConfig>("PUT", `/api/alert-rules/${encodeURIComponent(name)}`, r),
  /** Pause or resume a rule, keeping everything else about it. */
  setAlertRulePaused: (rule: AlertRuleConfig, paused: boolean) =>
    call<AlertRuleConfig>("PUT", `/api/alert-rules/${encodeURIComponent(rule.name)}`, {
      ...alertRuleEdit(rule),
      paused,
    }),
  deleteAlertRule: (name: string) => call<void>("DELETE", `/api/alert-rules/${encodeURIComponent(name)}`),

  sources: () => call<{ sources: Source[] }>("GET", "/api/knowledge").then((r) => r.sources),
  createSource: (name: string, description: string) => call<Source>("POST", "/api/knowledge", { name, description }),
  deleteSource: (name: string) => call<void>("DELETE", `/api/knowledge/${encodeURIComponent(name)}`),
  documents: (name: string) =>
    call<{ documents: Doc[] }>("GET", `/api/knowledge/${encodeURIComponent(name)}/documents`).then((r) => r.documents),
  addDocument: (name: string, d: { title: string; uri: string; text: string }) =>
    call<Doc>("POST", `/api/knowledge/${encodeURIComponent(name)}/documents`, d),
  removeDocument: (name: string, id: string) =>
    call<void>("DELETE", `/api/knowledge/${encodeURIComponent(name)}/documents/${encodeURIComponent(id)}`),
  excludeDocument: (name: string, id: string, excluded: boolean) =>
    call<Doc>("PUT", `/api/knowledge/${encodeURIComponent(name)}/documents/${encodeURIComponent(id)}`, { excluded }),
  saveVariant: (name: string, variant: string) =>
    call<{ name: string }>("POST", `/api/knowledge/${encodeURIComponent(name)}/variants`, { name: variant }),
  deleteVariant: (name: string, variant: string) =>
    call<void>("DELETE", `/api/knowledge/${encodeURIComponent(name)}/variants/${encodeURIComponent(variant)}`),
  searchKnowledge: (q: string, source: string, variant = "") =>
    call<{ hits: Hit[] }>(
      "GET",
      `/api/knowledge/search?${new URLSearchParams(variant ? { q, source, variant } : { q, source })}`,
    ).then((r) => r.hits),
  logout: () => call<void>("POST", "/auth/logout"),
};

/**
 * Subscribe to the tenant's events. `onOpen` fires on every (re)connect, and
 * the stream then replays what the bus still holds, so a caller that folds
 * events into state resets that state in `onOpen`.
 */
export function subscribe(
  topic: string,
  handlers: {
    onOpen?: () => void;
    onEvent: (e: BusEvent) => void;
    onState?: (live: boolean) => void;
    /** The inbox changed. It arrives as a named event, apart from run events. */
    onInbox?: () => void;
    /** Something happened in a shared thread: a message was posted, or its record changed. */
    onThread?: () => void;
  },
): () => void {
  const source = new EventSource(`/api/events?topic=${encodeURIComponent(topic)}`);
  source.onopen = () => {
    handlers.onOpen?.();
    handlers.onState?.(true);
  };
  source.onerror = () => handlers.onState?.(false);
  source.onmessage = (m) => {
    try {
      handlers.onEvent(JSON.parse(m.data as string) as BusEvent);
    } catch {
      // A frame that is not an event is ignored rather than fatal.
    }
  };
  source.addEventListener("inbox", () => handlers.onInbox?.());
  source.addEventListener("thread", () => handlers.onThread?.());
  return () => source.close();
}

// ---------------------------------------------------------------------------
// Call governance: which agents may call an agent, and its approval queue.
// The callee decides. These mirror CallPolicy, CallPolicyInput and Reach in
// api/openapi.yaml.
// ---------------------------------------------------------------------------

/** Which agents may call an agent: anyone its spec allows, only those listed, or none. */
export type CallCallers = "anyone" | "listed" | "none";
/** Which admitted calls wait for a person. `unlisted` holds every caller not in `trusted`. */
export type CallApproval = "never" | "always" | "unlisted";

export interface CallPolicyInput {
  callers: CallCallers;
  /** The callers admitted when `callers` is `listed`. */
  allow?: string[];
  approval: CallApproval;
  /** Callers whose calls skip approval when `approval` is `unlisted`. */
  trusted?: string[];
  /** Subjects, and roles as "role:<name>". Empty means managers and admins. */
  approvers?: string[];
  self_approval?: boolean;
  /** How long a held call waits before it is denied. 0 means a day. */
  expires_seconds?: number;
}

export interface CallPolicy extends Required<CallPolicyInput> {
  agent: string;
  /** 0 is the default of an agent with no stored policy. */
  version: number;
  updated_by?: string;
  /** What whoever last saved it is called. Shown in place of `updated_by`, the subject. */
  updated_by_name?: string;
  updated_at?: string;
}

/** Whether one agent (the caller in the path) may call another. */
export interface Reach {
  agent: string;
  /** The callee's policy accepts calls from the caller. */
  admits: boolean;
  /** The caller's spec lists the callee in its spawn allowlist. */
  in_spawn: boolean;
  approval: "never" | "required";
  reason: string;
}

/** One message of an approval queue, with the fields the queue needs. */
export interface QueuedApproval {
  id: string;
  subject: string;
  state: string;
  source: { run?: string; agent?: string; target?: string; thread?: string; project?: string };
  parts: {
    id: string;
    question?: {
      run: string;
      id: string;
      prompt: string;
      approvers?: string[];
      expires_at?: string;
      tool?: { name: string; acts_for_person?: boolean };
      /** The person whose own credentials an answer may reach, by subject: only they add a note. */
      steered_for?: string;
      /** What that person is called. Shown in place of `steered_for`, the subject. */
      steered_for_name?: string;
    };
  }[];
}

const agentPath = (name: string) => `/api/agents/${encodeURIComponent(name)}`;

export const callGovernance = {
  /** An agent's call policy, or its default (version 0). Needs agents:read. */
  policy: (agent: string) => call<CallPolicy>("GET", `${agentPath(agent)}/call-policy`),
  /** Replace an agent's call policy. Needs agents:write. */
  setPolicy: (agent: string, policy: CallPolicyInput) =>
    call<CallPolicy>("PUT", `${agentPath(agent)}/call-policy`, policy),
  /** For an agent as a caller, every other agent and whether it may call it. Needs agents:read. */
  reachable: (agent: string) => call<{ agents: Reach[]; truncated: boolean }>("GET", `${agentPath(agent)}/reachable`),
  /**
   * An agent's queue of calls waiting for approval: approvals it raised, or
   * that would call it. Only what the caller may see and answer is listed.
   */
  approvalQueue: (agent: string, state: "open" | "resolved" | "all" = "open") =>
    call<{ messages: QueuedApproval[]; next: string | null }>(
      "GET",
      `/api/inbox?${new URLSearchParams({ kind: "approval", agent, state })}`,
    ),
};

/** Who answers an untagged message in a place, and where that is set: `thread`, `client`, `channel`, or `project:<name>`. An empty agent is nobody. */
export interface ThreadAnswers {
  agent: string;
  from: string;
  /** Why the default cannot answer, when it has stopped being available. */
  unavailable?: string;
}

export interface ThreadParticipant {
  kind: "person" | "agent";
  /** A person's subject or an agent's name. */
  name: string;
  owner?: boolean;
  added_by?: string;
  at: string;
}

/** A shared thread's record. A conversation from before threads reads at version 0. */
export interface Thread {
  id: string;
  project: string;
  channel: string;
  title?: string;
  visibility: "shared" | "private";
  participants: ThreadParticipant[];
  default?: string;
  /** The inbox id of the announcement a reply to which, in Slack, started the thread. */
  announcement?: string;
  /** The Slack thread this thread is, when it was started from Slack. It never changes. */
  surface?: { client: string; channel: string; ts: string };
  version: number;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface ThreadMention {
  kind: "agent" | "person" | "sme";
  /** The agent (`auto` for @auto), the person's subject, or the experts' topic. */
  name: string;
  run?: string;
  refused?: string;
}

/** A person's turn in a thread. It starts no run of its own. */
export interface ThreadMessage {
  id: string;
  thread: string;
  prev?: string;
  by: string;
  name: string;
  text: string;
  mentions: ThreadMention[];
  answers?: { run: string; question: string };
  via: string;
  surface_id?: string;
  at: string;
}

/**
 * The announcement a thread was started from, as it stands now: who posted
 * it, a person by handle, an agent by name, or warp itself (`system`, by
 * `warp`) for a release's notes, and what it says. A withdrawn one says
 * nothing more.
 */
export interface ThreadAnnouncement {
  id: string;
  kind: "person" | "agent" | "system";
  by: string;
  name?: string;
  subject?: string;
  text?: string;
  withdrawn?: boolean;
  at: string;
}

/**
 * The question a parked run waits on, as the reader stands to it: whether
 * they may answer it, and whom it is for otherwise.
 */
export interface ThreadAsk {
  question: string;
  yours: boolean;
  for: "anyone" | "person" | "experts" | "answerers" | "nobody";
  /** The experts' topic, for `experts`. Absent is every expert. */
  topic?: string;
  /** The person it is for, by handle, for `person`. */
  person?: string;
}

export interface ThreadTurn {
  kind: "message" | "run" | "announcement";
  message?: ThreadMessage;
  run?: Run;
  announcement?: ThreadAnnouncement;
  /** On a run parked on a question a person answers. */
  ask?: ThreadAsk;
}

export interface ThreadPage {
  thread: Thread;
  turns: ThreadTurn[];
  next: string | null;
  answers: ThreadAnswers;
}

export type ThreadRule = "tagged" | "people" | "reply" | "post_only" | "default";

/** What posting a message did. */
export interface ThreadPosted {
  thread: Thread;
  message: ThreadMessage;
  runs: Run[];
  notified: string[];
  refused: ThreadMention[];
  rule: ThreadRule;
  reason?: string;
}

/** One place in a project where threads live, and who answers there. */
export interface Channel {
  project: string;
  name: string;
  title?: string;
  purpose?: string;
  default?: string;
  /** The Slack channel it is bound to: a Slack thread there is a shared thread here. */
  slack?: { client: string; channel: string };
  archived?: boolean;
  managed?: string;
  version: number;
  created_by?: string;
  created_at: string;
  updated_by?: string;
  updated_at: string;
  answers: ThreadAnswers;
}

export interface ProjectExpert {
  /** A person's subject, or `role:<name>`. */
  who: string;
  topics: string[];
}

/** One person in the directory, as the typeahead shows them. */
export interface DirectoryPerson {
  subject: string;
  name?: string;
  handle: string;
  picture: string | null;
}

/** Which agents may answer in a project. */
export interface ProjectAvailability {
  version: number;
  available: { all: boolean; names: string[] };
  homes: string[];
  default_resolved: { agent: string; from: string } | null;
}

/** A project's API path: `~` is the workspace itself. */
const projectAt = (name: string) => `/api/projects/${projectPath(name)}`;
const threadPath = (id: string) => `/api/threads/${encodeURIComponent(id)}`;

/** Channels, experts and shared threads: where people and agents work together. */
export const coworking = {
  channels: (project: string) => call<{ channels: Channel[] }>("GET", `${projectAt(project)}/channels`).then((r) => r.channels),
  createChannel: (project: string, c: { name: string; title?: string; purpose?: string; default?: string }) =>
    call<Channel>("POST", `${projectAt(project)}/channels`, c),
  updateChannel: (project: string, name: string, c: { version: number; title: string; purpose: string; default: string }) =>
    call<Channel>("PUT", `${projectAt(project)}/channels/${encodeURIComponent(name)}`, c),
  archiveChannel: (project: string, name: string) =>
    call<void>("DELETE", `${projectAt(project)}/channels/${encodeURIComponent(name)}`),
  experts: (project: string) => call<{ version: number; experts: ProjectExpert[] }>("GET", `${projectAt(project)}/experts`),
  setExperts: (project: string, version: number, experts: ProjectExpert[]) =>
    call<{ version: number; experts: ProjectExpert[] }>("PUT", `${projectAt(project)}/experts`, { version, experts }),
  /** The knowledge sources a project names, and what a run filed in it is grounded in. */
  knowledge: (project: string) => call<ProjectKnowledge>("GET", `${projectAt(project)}/knowledge`),
  /** Replaces the sources a project names, as a whole, from the version read. */
  setKnowledge: (project: string, version: number, knowledge: string[]) =>
    call<ProjectKnowledge>("PUT", `${projectAt(project)}/knowledge`, { version, knowledge }),
  /** One page of a channel's threads, most recently active first. */
  threads: (project: string, channel: string, before?: string) => {
    const q = new URLSearchParams({ project: project === "" ? "~" : project, channel, limit: "30" });
    if (before) q.set("before", before);
    return call<{ threads: ThreadSummary[]; next: string | null }>("GET", `/api/threads?${q}`);
  },
  /** A thread and one page of its turns, the newest first; `before` reads the page above. */
  thread: (id: string, before?: string) =>
    call<ThreadPage>("GET", before ? `${threadPath(id)}?${new URLSearchParams({ before })}` : threadPath(id)),
  /** Starts a thread: shared unless `visibility` says private, read then by the people in it alone. */
  start: (t: { project: string; channel: string; text: string; title?: string; post_only?: boolean; visibility?: "shared" | "private" }) =>
    call<ThreadPosted>("POST", "/api/threads", { visibility: "shared", ...t }),
  post: (id: string, m: { text: string; after?: string; post_only?: boolean; answers?: { run: string; question: string } }) =>
    call<ThreadPosted>("POST", `${threadPath(id)}/messages`, m),
  update: (id: string, t: { version: number; title: string; visibility: "shared" | "private"; default: string }) =>
    call<Thread>("PUT", threadPath(id), t),
  /** The workspace's people whose name or handle holds `q`, for the @ typeahead. */
  people: (q: string, limit = 8) =>
    call<{ people: DirectoryPerson[] }>("GET", `/api/people?${new URLSearchParams({ q, limit: String(limit) })}`).then((r) => r.people),
  /** Which agents may answer in a project, for the @ typeahead. */
  available: (project: string) => call<ProjectAvailability>("GET", `${projectAt(project)}/agents`),
};
