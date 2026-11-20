// Prices are micro-USD on the wire and dollars on screen. Both ways are
// integer arithmetic on the digits, so no float rounds a price.

/** The most a voice price can be, in micro-USD: $1,000. */
export const MAX_VOICE_PRICE = 1_000_000_000;

/** Micro-USD as dollars, without trailing zeros: 40000 is "0.04". */
export function microsToDollars(micros: number): string {
  const m = Math.max(0, Math.trunc(micros));
  const whole = Math.floor(m / 1_000_000);
  const frac = String(m % 1_000_000)
    .padStart(6, "0")
    .replace(/0+$/, "");
  return frac ? `${whole}.${frac}` : String(whole);
}

/** Dollars as micro-USD, or null for what is not a price up to $1,000 with at most six decimals. */
export function dollarsToMicros(text: string): number | null {
  const m = /^\s*(\d{1,4})(?:\.(\d{1,6}))?\s*$/.exec(text);
  if (!m) return null;
  const micros = Number(m[1]) * 1_000_000 + Number((m[2] ?? "").padEnd(6, "0"));
  return micros <= MAX_VOICE_PRICE ? micros : null;
}
