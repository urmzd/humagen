import type { AgentActivity, TurnStatus } from "./types";

/**
 * Conversation-level state that outlives a single turn: context window,
 * compaction, budget, model, and approval mode. The web shows it as a status
 * line under the composer (segments like an editor's statusline); Slack has no
 * persistent bar, so the same data surfaces as notices when it matters.
 */

export interface ContextWindow {
  usedTokens: number;
  limitTokens: number;
  /** Compaction starts at this fraction of the limit (e.g. 0.8). */
  compactAt?: number;
}

export type CompactionState =
  | { kind: "idle" }
  | { kind: "compacting"; startedAt: string }
  | { kind: "compacted"; at: string; savedTokens: number; summary?: string }
  | { kind: "failed"; at: string; reason: string };

export interface Budget {
  spentUsd: number;
  limitUsd: number;
  /** "day", "month", "session". */
  period: string;
  /** Estimated cost of the next turn, to warn before it would cross the cap. */
  nextTurnUsd?: number;
}

/** "ask": gate side effects for approval. "auto": act without asking. */
export type ApprovalMode = "ask" | "auto";

export interface SessionState {
  model?: string;
  approvalMode?: ApprovalMode;
  context?: ContextWindow;
  compaction?: CompactionState;
  budget?: Budget;
  /** Messages waiting behind the current turn (see queue.ts). */
  queued?: number;
  status?: TurnStatus | "idle";
  activity?: AgentActivity;
}

export type Pressure = "ok" | "warn" | "critical";

/** Context pressure: warn as compaction nears, critical once past the limit's last 5%. */
export function contextPressure(c: ContextWindow): { ratio: number; pressure: Pressure } {
  const ratio = c.limitTokens > 0 ? c.usedTokens / c.limitTokens : 0;
  const warnAt = c.compactAt ?? 0.8;
  return { ratio, pressure: ratio >= 0.95 ? "critical" : ratio >= warnAt ? "warn" : "ok" };
}

/** Budget pressure: critical when spent or when the next turn would cross the cap. */
export function budgetPressure(b: Budget): { ratio: number; pressure: Pressure; blocked: boolean } {
  const ratio = b.limitUsd > 0 ? b.spentUsd / b.limitUsd : 0;
  const blocked = b.spentUsd >= b.limitUsd || b.spentUsd + (b.nextTurnUsd ?? 0) > b.limitUsd;
  return {
    ratio,
    blocked,
    pressure: blocked || ratio >= 0.95 ? "critical" : ratio >= 0.8 ? "warn" : "ok",
  };
}

/** The more urgent of context and budget, for a single headroom indicator. */
export function headroom(s: SessionState): {
  source: "context" | "budget" | null;
  pressure: Pressure;
} {
  const c = s.context ? contextPressure(s.context).pressure : "ok";
  const b = s.budget ? budgetPressure(s.budget).pressure : "ok";
  const rank: Record<Pressure, number> = { ok: 0, warn: 1, critical: 2 };
  if (rank[c] === 0 && rank[b] === 0) return { source: null, pressure: "ok" };
  return rank[b] >= rank[c]
    ? { source: "budget", pressure: b }
    : { source: "context", pressure: c };
}

export function formatTokens(n: number): string {
  return n >= 1_000_000
    ? `${(n / 1_000_000).toFixed(1)}M`
    : n >= 1_000
      ? `${Math.round(n / 1_000)}k`
      : String(n);
}

export function formatUsd(n: number): string {
  return n < 0.01 && n > 0 ? "<$0.01" : `$${n.toFixed(2)}`;
}
