import type { Turn } from "@agent-ui/core";
import type { FileProps } from "@agent-ui/primitives";
import { createContext, useContext, useEffect, useState } from "react";

/**
 * The turn a web frame is drawing, when there is one. Primitives only get
 * contract props; this lets the web add detail the contract leaves out
 * (a task's sub-agent, a failure's HTTP status) without changing it.
 */
export const TurnMetaContext = createContext<Turn | null>(null);

export function useTurnMeta() {
  return useContext(TurnMetaContext);
}

/** Clock for countdowns. Stories pin it; apps leave it to the real time. */
export const NowContext = createContext<number | null>(null);

/** Current time, re-read every `everyMs`, or the pinned time from NowContext. */
export function useNow(everyMs = 30_000): number {
  const pinned = useContext(NowContext);
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    if (pinned !== null) return;
    const t = setInterval(() => setNow(Date.now()), everyMs);
    return () => clearInterval(t);
  }, [pinned, everyMs]);
  return pinned ?? now;
}

/** Opens a file preview. The web package supplies a docked panel; without one, Open is a link. */
export const ArtifactOpenContext = createContext<((file: FileProps) => void) | null>(null);

/** "42m left", "3h left", "2d left"; null once past. */
export function timeLeft(iso: string, now: number): { text: string; soon: boolean } | null {
  const ms = new Date(iso).getTime() - now;
  if (Number.isNaN(ms) || ms <= 0) return null;
  const m = Math.ceil(ms / 60_000);
  const text =
    m < 60
      ? `${m}m left`
      : m < 48 * 60
        ? `${Math.round(m / 60)}h left`
        : `${Math.round(m / 1440)}d left`;
  return { text, soon: m <= 60 };
}

/**
 * Lines a surrounding web frame already shows (e.g. the agent name and status
 * that `AgentMessage` puts in its header). Matching Context items and Text are
 * skipped; `block:<kind>` entries skip an Actions row or Feedback of that kind.
 */
export const SuppressContext = createContext<ReadonlySet<string>>(new Set());
