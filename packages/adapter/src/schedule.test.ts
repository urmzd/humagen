import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components/copy";
import { interruptOf } from "./question.ts";
import { cronWords, nextRun, schedulePreview } from "./schedule.ts";

const words = defaultCopy.schedules.proposal;

test("cronWords: steps and the common shapes are words, anything else is the expression", () => {
  assert.equal(cronWords("*/15 * * * *"), "Every 15 minutes");
  assert.equal(cronWords("*/1 * * * *"), "Every minute");
  assert.equal(cronWords("0 */2 * * *"), "Every 2 hours");
  assert.equal(cronWords("0 9 * * 1-5"), "Weekdays at 09:00 UTC");
  assert.equal(cronWords("5 * * * *"), "Hourly at minute 5");
  for (const e of ["0 9 1 * *", "30 */6 * * *", "nonsense", "0 9 * * 1,3"])
    assert.equal(cronWords(e), e);
});

test("nextRun: the next time the cron fires after a time, in UTC", () => {
  const at = new Date("2026-11-20T14:07:30Z");
  assert.equal(nextRun("*/15 * * * *", at)?.toISOString(), "2026-11-20T14:15:00.000Z");
  assert.equal(
    nextRun("0 9 * * 1-5", at)?.toISOString(),
    "2026-11-23T09:00:00.000Z",
    "Friday afternoon: Monday",
  );
  assert.equal(nextRun("0 0 1 1 *", at)?.toISOString(), "2027-01-01T00:00:00.000Z");
  assert.equal(nextRun("0 12 * * 7", at)?.toISOString(), "2026-11-22T12:00:00.000Z", "7 is Sunday");
  assert.equal(
    nextRun("0 0 13 * 5", at)?.toISOString(),
    "2026-11-27T00:00:00.000Z",
    "either restricted day field fires: the next Friday",
  );
  assert.equal(nextRun("0 0 31 2 *", at), undefined, "never fires");
  assert.equal(nextRun("nonsense", at), undefined);
  assert.equal(nextRun("61 * * * *", at), undefined);
});

test("schedulePreview: a proposed schedule's card says what it checks, how often, who, where and when", () => {
  const now = new Date("2026-11-20T14:07:00Z");
  const args = {
    name: "plaud-new",
    cron: "*/15 * * * *",
    check: "Look for new Plaud recordings. Answer NONE when there are none.",
    post: "here",
  };
  assert.deepEqual(schedulePreview("create_schedule", args, "lead", now), [
    { label: words.name, value: "plaud-new" },
    { label: words.checks, value: args.check },
    { label: words.every, value: "Every 15 minutes" },
    { label: words.agent, value: "lead" },
    { label: words.posts, value: words.postsHere },
    { label: words.nextRun, value: "{{time:2026-11-20T14:15:00.000Z}}" },
  ]);
  const elsewhere = schedulePreview(
    "update_schedule",
    { ...args, agent: "helper", post: "slack", to: "C0123ABC" },
    "lead",
    now,
  );
  assert.equal(elsewhere?.find((f) => f.label === words.agent)?.value, "helper");
  assert.equal(
    elsewhere?.find((f) => f.label === words.posts)?.value,
    words.postsSlack("C0123ABC"),
  );
  const kept = schedulePreview("create_schedule", { ...args, post: undefined }, "lead", now);
  assert.equal(kept?.find((f) => f.label === words.posts)?.value, words.postsNowhere);
  // A link or a time token a model wrote stays inert.
  const sneaky = schedulePreview(
    "create_schedule",
    { ...args, check: "see [x](https://evil) {{time:x}}" },
    "lead",
    now,
  );
  const checks = sneaky?.find((f) => f.label === words.checks)?.value ?? "";
  assert.ok(!checks.includes("](") && !checks.includes("{{"));
  assert.equal(schedulePreview("run_schedule", { name: "plaud-new" }, "lead", now), undefined);
  assert.equal(schedulePreview("file_write", { path: "x" }, "lead", now), undefined);
});

test("interruptOf: a create_schedule approval previews the schedule; run_schedule is its summary", () => {
  const card = interruptOf({
    id: "q-s",
    kind: "approval",
    agent: "lead",
    prompt:
      "Create a schedule: plaud-new, every 15 minutes, posts in this Slack channel (C0PLAUD) only when something fires",
    tool: {
      name: "create_schedule",
      call_id: "c1",
      arguments: { name: "plaud-new", cron: "*/15 * * * *", check: "New?", post: "here" },
    },
  });
  assert.equal(card?.type, "approval");
  if (card?.type !== "approval") return;
  assert.ok(card.preview?.some((f) => f.label === words.every && f.value === "Every 15 minutes"));
  assert.ok(card.preview?.some((f) => f.label === words.nextRun));
  const once = interruptOf({
    id: "q-r",
    kind: "approval",
    agent: "lead",
    prompt: "Run plaud-new once now",
    tool: { name: "run_schedule", call_id: "c2", arguments: { name: "plaud-new" } },
  });
  assert.ok(
    once?.type === "approval" &&
      once.preview === undefined &&
      once.summary === "Run plaud-new once now",
  );
});
