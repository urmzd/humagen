import assert from "node:assert/strict";
import { test } from "node:test";
import { inWords } from "./cron.ts";
import { recapCron } from "./recap.ts";

test("inWords: a project's weekly recap reads as a day and a time, not as cron", () => {
  assert.equal(inWords("9 8 * * 1"), "Mondays at 08:09 UTC");
  assert.match(inWords(recapCron("Globex")), /^Mondays at 08:\d\d UTC$/);
});

test("inWords: the common shapes are words", () => {
  assert.equal(inWords("0 9 * * 1-5"), "Weekdays at 09:00 UTC");
  assert.equal(inWords("30 17 * * 1-5"), "Weekdays at 17:30 UTC");
  assert.equal(inWords("0 10 * * 0,6"), "Weekends at 10:00 UTC");
  assert.equal(inWords("15 6 * * *"), "Daily at 06:15 UTC");
  assert.equal(inWords("5 * * * *"), "Hourly at minute 5");
  assert.equal(inWords("0 12 * * 0"), "Sundays at 12:00 UTC");
  assert.equal(inWords("0 12 * * 7"), "Sundays at 12:00 UTC", "7 is Sunday too");
});

test("inWords: anything else is shown as written", () => {
  for (const e of ["*/5 * * * *", "0 9 1 * *", "0 9 * 1 *", "0 9 * * 1,3", "0 25 * * *", "75 * * * *", "nonsense"]) assert.equal(inWords(e), e);
});
