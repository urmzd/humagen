import assert from "node:assert/strict";
import { test } from "node:test";
import {
  deliverFor,
  deliverOptions,
  recapAgent,
  recapCron,
  recapName,
  weeklyRecap,
} from "./recap.ts";

test("a recap runs on Monday between 08:01 and 08:59, never on :00 or :30", () => {
  const minutes = new Set<number>();
  for (let i = 0; i < 500; i++) {
    const cron = recapCron(`Project ${i}`);
    const [minute, hour, dom, month, dow] = cron.split(" ");
    assert.equal(`${hour} ${dom} ${month} ${dow}`, "8 * * 1", cron);
    const m = Number(minute);
    assert.ok(Number.isInteger(m) && m >= 1 && m <= 59 && m !== 30, cron);
    minutes.add(m);
  }
  // Spread over the hour, so a workspace's recaps do not fire together.
  assert.ok(minutes.size > 40, `only ${minutes.size} minutes used`);
});

test("a project's recap time is the same every time it is asked", () => {
  assert.equal(recapCron("Globex"), recapCron("Globex"));
  assert.notEqual(recapCron("Globex"), recapCron("Initech"));
});

test("a recap's schedule name is one a schedule can have", () => {
  for (const [project, want] of [
    ["Field", "field-recap"],
    ["Company news", "company-news-recap"],
    ["__x__", "x-recap"],
    ["---", "project-recap"],
  ] as const) {
    assert.equal(recapName(project), want);
  }
  assert.match(recapName("A".repeat(63)), /^[a-z0-9-]{1,48}-recap$/);
});

test("Add Weekly Recap posts the agent's answer to the project", () => {
  const s = weeklyRecap(
    "Field",
    "humagen-analyst",
    "What changed in Field engineering since the last recap?",
    "Weekly",
  );
  assert.deepEqual(s, {
    name: "field-recap",
    expression: recapCron("Field"),
    agent: "humagen-analyst",
    text: "What changed in Field engineering since the last recap?",
    project: "Field",
    description: "Weekly",
    deliver: "project",
  });
});

test("the workspace's recap is filed in the workspace, ~", () => {
  for (const root of ["", "~"]) {
    const s = weeklyRecap(root, "humagen-analyst", "What changed?", "Weekly");
    assert.equal(s.name, "workspace-recap");
    assert.equal(s.project, "~");
    assert.equal(s.expression, recapCron("~"));
  }
});

test("only a schedule filed in a project can post to one", () => {
  const words = {
    deliverNone: "Keep",
    deliverInbox: (mine: boolean) => (mine ? "Mine" : "Theirs"),
    deliverProject: "Post",
  };
  assert.deepEqual(
    deliverOptions(undefined, words, true).map((o) => o.value),
    ["", "inbox"],
  );
  assert.deepEqual(
    deliverOptions("Field", words, false).map((o) => o.label),
    ["Keep", "Theirs", "Post"],
  );
  assert.equal(deliverFor(undefined, "project"), undefined);
  assert.equal(deliverFor("Field", "project"), "project");
  assert.equal(deliverFor(undefined, "inbox"), "inbox");
});

test("a recap asks humagen-analyst when it is there", () => {
  assert.equal(recapAgent(["lead", "humagen-analyst"]), "humagen-analyst");
  assert.equal(recapAgent(["lead"]), "lead");
  assert.equal(recapAgent([]), undefined);
});
