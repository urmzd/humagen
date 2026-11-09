import assert from "node:assert/strict";
import { test } from "node:test";
import { age, ago, daysAgo } from "./format.ts";

// Local wall-clock times, so the test holds in any time zone.
const at = (day: number, hour: number, minute = 0) =>
  new Date(2026, 10, day, hour, minute).toISOString();
const now = new Date(2026, 10, 9, 14, 45).getTime();
const en = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" });

test("a time is said one way: its short age, its words and its day never disagree", () => {
  // 37 hours before: dated yesterday. Rounding the hours said "2 days ago" beside "1d".
  const yesterdayEarly = at(8, 1, 24);
  assert.equal(daysAgo(yesterdayEarly, now), 1);
  assert.equal(age(yesterdayEarly, now), "1d");
  assert.equal(ago(yesterdayEarly, now), en.format(-1, "day"));

  // 47 hours before: dated two days ago, so it is not "1d" under Earlier.
  const twoDays = at(7, 15, 30);
  assert.equal(daysAgo(twoDays, now), 2);
  assert.equal(age(twoDays, now), "2d");
  assert.equal(ago(twoDays, now), en.format(-2, "day"));

  // 25 hours before, still dated yesterday.
  assert.equal(age(at(8, 13, 30), now), "1d");
  assert.equal(ago(at(8, 13, 30), now), en.format(-1, "day"));
});

test("under a day, whole hours and minutes that have passed, never rounded up", () => {
  assert.equal(age(at(9, 14, 44), now), "1m");
  assert.equal(age(at(9, 13, 46), now), "59m");
  assert.equal(ago(at(9, 13, 46), now), en.format(-59, "minute"));
  // 23 hours 45 minutes is 23 hours, not a day, though it is dated yesterday.
  assert.equal(age(at(8, 15, 0), now), "23h");
  assert.equal(ago(at(8, 15, 0), now), en.format(-23, "hour"));
  assert.equal(age(new Date(now - 30_000).toISOString(), now), "now");
  assert.equal(ago(new Date(now - 30_000).toISOString(), now), "just now");
});

test("a time ahead is said in words, and has no age", () => {
  assert.equal(ago(at(9, 17, 45), now), en.format(3, "hour"));
  assert.equal(ago(at(12, 9, 0), now), en.format(3, "day"));
  assert.equal(age(at(12, 9, 0), now), "now");
});

test("what is no date is not a number of days", () => {
  assert.equal(age("", now), "now");
  assert.equal(ago("not a date", now), "just now");
});
