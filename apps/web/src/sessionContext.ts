/**
 * What the session panel shows beyond Details' own rows, worked out from what
 * the page already reads: how full the context is, the pull requests the
 * session made or read and their checks, the repositories and branches they
 * name, the files it wrote, and where its sandbox stands. Pure; no React.
 */
import type { ResponseState } from "@humagen/core";
import type { GitHubPull } from "../../../packages/client/src/generated/types.ts";
import type { Run, Usage } from "./adapter.ts";
import type { SessionSummary } from "./sessionSummary.ts";
import { fileDiff, sessionWrites } from "./workbenchChanges.ts";
import type { ConversationSandboxes } from "./workspace-files.ts";

/** How full the context is, with what it took to get there. */
export interface ContextUse {
  /** Tokens in the latest model call's prompt. */
  used: number;
  /** The model's window, in tokens. 0 when the catalog does not know it. */
  window: number;
  /** 0 to 100, rounded. Null without a window. */
  percent: number | null;
  model?: string;
  /** What every answer of the session used, added up. */
  total: Usage;
}

const add = (a: number | undefined, b: number | undefined) => (a ?? 0) + (b ?? 0);

/**
 * The session's context: the fill of its latest answer that reported one,
 * and the usage of all its answers. Undefined when no answer has reported.
 */
export function contextUse(runs: readonly Run[]): ContextUse | undefined {
  const answers = runs.filter((r) => !r.parent);
  const latest = [...answers].reverse().find((r) => r.usage?.context?.tokens);
  if (!latest?.usage?.context) return undefined;
  const c = latest.usage.context;
  const window = c.window && c.window > 0 ? c.window : 0;
  const used = c.tokens ?? 0;
  const total: Usage = {};
  let micros = 0;
  let currency: string | undefined;
  for (const r of answers) {
    const u = r.usage?.total;
    if (!u) continue;
    total.model_calls = add(total.model_calls, u.model_calls);
    total.prompt_tokens = add(total.prompt_tokens, u.prompt_tokens);
    total.cached_prompt_tokens = add(total.cached_prompt_tokens, u.cached_prompt_tokens);
    total.completion_tokens = add(total.completion_tokens, u.completion_tokens);
    if (u.cost && (!currency || u.cost.currency === currency)) {
      micros += u.cost.micros;
      currency = u.cost.currency;
    }
    if (u.unpriced_calls) total.unpriced_calls = add(total.unpriced_calls, u.unpriced_calls);
  }
  if (currency) total.cost = { micros, currency };
  return {
    used,
    window,
    percent: window ? Math.min(100, Math.round((100 * used) / window)) : null,
    ...(c.model ? { model: c.model } : {}),
    total,
  };
}

/** A pull request the session made or read, by where it lives. */
export interface PullRef {
  repo: string;
  number: number;
  url: string;
}

/** The pull requests among the session's outputs, in the order it reached them. */
export function pullsOf(summary: SessionSummary | undefined): PullRef[] {
  const out: PullRef[] = [];
  for (const o of summary?.outputs ?? []) {
    if (o.kind !== "pr" || !o.url) continue;
    const m = /github\.com\/([^/\s]+\/[^/\s]+)\/pull\/(\d+)/.exec(o.url);
    if (!m?.[1] || !m[2]) continue;
    const ref = { repo: m[1], number: Number(m[2]), url: o.url };
    if (!out.some((p) => p.repo === ref.repo && p.number === ref.number)) out.push(ref);
  }
  return out;
}

export type CheckState = "failing" | "running" | "passing" | "none";

/** What the panel says of one pull request. */
export interface PullFacts {
  title: string;
  branch?: string;
  state: string;
  additions: number;
  deletions: number;
  /** More files changed than were read, so the counts are a floor. */
  partial: boolean;
  checks: { state: CheckState; failing: number; total: number };
}

/** The workflow conclusions that mean a check did not pass. */
const FAILED = new Set(["failure", "timed_out", "cancelled", "action_required", "startup_failure"]);

/**
 * A pull request as the API read it: its title and branch, the lines its
 * patches add and remove, and its checks on the head commit. A check counts
 * once, by its latest attempt.
 */
export function pullFacts(pull: GitHubPull): PullFacts {
  let additions = 0;
  let deletions = 0;
  for (const f of pull.files) {
    for (const line of (f.patch ?? "").split("\n")) {
      if (line.startsWith("+++") || line.startsWith("---")) continue;
      if (line.startsWith("+")) additions++;
      else if (line.startsWith("-")) deletions++;
    }
  }
  const latest = new Map<string, (typeof pull.workflows)[number]>();
  for (const w of pull.workflows) {
    const seen = latest.get(w.name);
    if (!seen || w.run_attempt >= seen.run_attempt) latest.set(w.name, w);
  }
  const runs = [...latest.values()];
  const failing = runs.filter((w) => FAILED.has(w.conclusion)).length;
  const running = runs.some((w) => w.status !== "completed");
  const state: CheckState =
    runs.length === 0 ? "none" : failing > 0 ? "failing" : running ? "running" : "passing";
  return {
    title: pull.subject.title,
    ...(pull.subject.head?.ref ? { branch: pull.subject.head.ref } : {}),
    state: pull.subject.state,
    additions,
    deletions,
    partial: !!pull.next_page,
    checks: { state, failing, total: runs.length },
  };
}

/** A repository the session worked in, with the branches its pull requests are on. */
export interface RepoUse {
  repo: string;
  branches: string[];
}

/** The repositories the session's pull requests are in, with their branches, first seen first. */
export function reposOf(
  pulls: readonly PullRef[],
  facts: Readonly<Record<string, PullFacts | undefined>>,
): RepoUse[] {
  const out: RepoUse[] = [];
  for (const p of pulls) {
    let repo = out.find((r) => r.repo === p.repo);
    if (!repo) {
      repo = { repo: p.repo, branches: [] };
      out.push(repo);
    }
    const branch = facts[pullKey(p)]?.branch;
    if (branch && !repo.branches.includes(branch)) repo.branches.push(branch);
  }
  return out;
}

export const pullKey = (p: Pick<PullRef, "repo" | "number">) => `${p.repo}#${p.number}`;

/** The files the session's calls wrote, and the lines they add and remove all told. */
export function localChanges(responses: readonly ResponseState[] | undefined): {
  files: number;
  additions: number;
  deletions: number;
} {
  const byPath = new Map<string, string[]>();
  for (const w of sessionWrites(responses)) {
    const texts = byPath.get(w.path) ?? [];
    texts.push(w.text);
    byPath.set(w.path, texts);
  }
  let additions = 0;
  let deletions = 0;
  for (const [path, texts] of byPath) {
    const last = texts[texts.length - 1] ?? "";
    // A file written once is new here; one written again is compared with its first text.
    const first = texts.length > 1 ? (texts[0] ?? "") : "";
    const diff = fileDiff(first, last, path);
    additions += diff.added;
    deletions += diff.removed;
  }
  return { files: byPath.size, additions, deletions };
}

/**
 * Where the session's sandbox stands. `isolated`: its runs lease an instance
 * of their own. `host`: the deployment has no sandbox and tools work in the
 * session's folder on the server. `none`: isolated, but no run has started
 * one yet. `unreachable`: it could not be read.
 */
export type SandboxState = "isolated" | "host" | "none" | "unreachable" | "loading";

export function sandboxState(
  boxes: ConversationSandboxes | undefined,
  failed: boolean,
): SandboxState {
  if (failed) return "unreachable";
  if (!boxes) return "loading";
  const isolated = boxes.session.isolated || boxes.runs.some((r) => r.isolated);
  if (!isolated) return "host";
  return boxes.runs.length > 0 || boxes.session.files > 0 ? "isolated" : "none";
}
