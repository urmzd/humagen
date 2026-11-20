import { describe, expect, it } from "vitest";
import { formatElapsed, formatMicros } from "./usage";

describe("formatMicros", () => {
  it("writes dollars with two decimals, or four when the sum needs them", () => {
    expect(formatMicros(1_500_000, "USD")).toBe("$1.50");
    expect(formatMicros(42_100, "USD")).toBe("$0.0421");
    expect(formatMicros(0, "USD")).toBe("$0.00");
    expect(formatMicros(12_000_000, "usd")).toBe("$12.00");
  });

  it("rounds the fifth decimal half up, carrying into the units", () => {
    expect(formatMicros(42_149, "USD")).toBe("$0.0421");
    expect(formatMicros(42_150, "USD")).toBe("$0.0422");
    expect(formatMicros(999_950, "USD")).toBe("$1.00");
    expect(formatMicros(49, "USD")).toBe("$0.00");
  });

  it("names any other currency after the amount and never converts it", () => {
    expect(formatMicros(2_340_000, "EUR")).toBe("2.34 EUR");
    expect(formatMicros(-500_000, "USD")).toBe("-$0.50");
  });
});

describe("formatElapsed", () => {
  it("reads as seconds, minutes or hours", () => {
    expect(formatElapsed(800)).toBe("0.8s");
    expect(formatElapsed(52_000)).toBe("52s");
    expect(formatElapsed(185_000)).toBe("3m 5s");
    expect(formatElapsed(180_000)).toBe("3m");
    expect(formatElapsed(4_320_000)).toBe("1h 12m");
  });
});
