/**
 * What one run used, as a surface shows it when a person asks: the model,
 * tokens, cost and how long it took. Never drawn on a turn itself: a surface
 * opens it for the one person who asked (see `RunDetailsView`).
 */
export interface RunDetails {
  agent?: string;
  /** The model of the run's latest model call. */
  model?: string;
  /** All input tokens, cached ones included. */
  inputTokens?: number;
  /** The part of the input that was read from cache. */
  cachedTokens?: number;
  outputTokens?: number;
  /**
   * Cost in integer millionths of `currency`. `atLeast` when some calls had
   * no price, so the figure is a lower bound.
   */
  cost?: { micros: number; currency: string; atLeast?: boolean };
  /** From the run's start to its last change. */
  elapsedMs?: number;
}

/**
 * An amount held in integer millionths, written without float arithmetic:
 * two decimals, or four for the small sums one run costs. US dollars read
 * `$0.0421`; any other currency reads `0.0421 EUR`.
 */
export function formatMicros(micros: number, currency: string): string {
  const sign = micros < 0 ? "-" : "";
  const abs = Math.abs(Math.trunc(micros));
  let whole = Math.floor(abs / 1_000_000);
  // Round the six decimals half up to four, in integers.
  let frac = Math.floor((abs % 1_000_000) / 100) + (abs % 100 >= 50 ? 1 : 0);
  if (frac >= 10_000) {
    whole += 1;
    frac -= 10_000;
  }
  const four = String(frac).padStart(4, "0");
  const amount = `${whole}.${four.endsWith("00") ? four.slice(0, 2) : four}`;
  return currency.toUpperCase() === "USD" ? `${sign}$${amount}` : `${sign}${amount} ${currency}`;
}

/** A length of time for one line: "0.8s", "41s", "3m 5s", "1h 12m". */
export function formatElapsed(ms: number): string {
  if (ms < 1000) return `${(Math.max(ms, 0) / 1000).toFixed(1)}s`;
  const s = Math.round(ms / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return s % 60 ? `${m}m ${s % 60}s` : `${m}m`;
  const h = Math.floor(m / 60);
  return m % 60 ? `${h}h ${m % 60}m` : `${h}h`;
}
