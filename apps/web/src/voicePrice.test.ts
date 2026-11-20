import assert from "node:assert/strict";
import { test } from "node:test";
import { dollarsToMicros, microsToDollars } from "./voicePrice.ts";

test("micro-USD read as dollars and back without rounding", () => {
  assert.equal(microsToDollars(40_000), "0.04");
  assert.equal(microsToDollars(22_000_000), "22");
  assert.equal(microsToDollars(360_000), "0.36");
  assert.equal(microsToDollars(1), "0.000001");
  assert.equal(microsToDollars(0), "0");
  for (const m of [0, 1, 40_000, 360_000, 15_000_000, 22_000_000, 1_000_000_000]) {
    assert.equal(dollarsToMicros(microsToDollars(m)), m);
  }
});

test("only a price up to a thousand dollars with six decimals is taken", () => {
  assert.equal(dollarsToMicros(" 0.04 "), 40_000);
  assert.equal(dollarsToMicros("15"), 15_000_000);
  assert.equal(dollarsToMicros("1000"), 1_000_000_000);
  for (const bad of ["", "-1", "1e3", "0.0000001", "1000.01", "abc", "1,5", "12345"]) {
    assert.equal(dollarsToMicros(bad), null, bad);
  }
});
