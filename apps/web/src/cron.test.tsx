import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@warp/components";
import { inWords, presets } from "./cron.ts";
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
  for (const e of ["*/5 * * * *", "0 9 1 * *", "0 9 * 1 *", "0 9 * * 1,3", "0 25 * * *", "75 * * * *", "0 9 * * * 2027", "nonsense"]) assert.equal(inWords(e), e);
});

test("inWords: the words are the copy table's, so a host that changes them changes these", () => {
  const when = defaultCopy.schedules.when;
  assert.equal(inWords("9 8 * * 1"), when.weekly(1, "08:09"));
  assert.equal(inWords("0 9 * * 1-5"), when.weekdays("09:00"));
  assert.equal(inWords("0 10 * * 0,6"), when.weekends("10:00"));
  assert.equal(inWords("15 6 * * *"), when.daily("06:15"));
  assert.equal(inWords("5 * * * *"), when.hourlyAt(5));
  assert.equal(inWords("0 * * * *"), when.hourly);
  assert.equal(inWords("* * * * *"), when.everyMinute);
});

test("presets: each shape offered when making a schedule is named in the same words", () => {
  assert.deepEqual(
    presets.map((p) => p.label),
    ["Every minute", "Hourly", "Daily at 09:00 UTC", "Weekdays at 09:00 UTC"],
  );
  for (const p of presets) assert.equal(p.label, inWords(p.expression));
});
