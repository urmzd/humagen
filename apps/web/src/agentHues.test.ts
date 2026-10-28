import assert from "node:assert/strict";
import { test } from "node:test";
import {
  agentHue,
  agentHuePath,
  configureAgentHues,
  requestAgentHue,
  setAgentHues,
  subscribeHues,
} from "./agentHues.ts";

test("unknown marks wait for a bounded lookup, then share server reservations", async () => {
  const batches: string[][] = [];
  configureAgentHues(async (names) => {
    batches.push(names);
    return Object.fromEntries(names.map((name, i) => [name, i * 0.25]));
  });
  let notices = 0;
  const unsubscribe = subscribeHues(() => notices++);
  try {
    assert.equal(agentHue("lookup-0"), undefined);
    for (let i = 0; i < 201; i++) requestAgentHue(`lookup-${i}`);
    requestAgentHue("lookup-0");
    await new Promise((resolve) => setTimeout(resolve, 10));
    assert.deepEqual(
      batches.map((names) => names.length),
      [100, 100, 1],
    );
    assert.equal(notices, 3);
    assert.equal(agentHue("lookup-0"), 0);
    setAgentHues({ extra: 123 });
    assert.equal(agentHue("lookup-0"), 0);
    assert.equal(agentHue("extra"), 123);
  } finally {
    unsubscribe();
    configureAgentHues(undefined);
  }
});

test("changing identity clears reservations and ignores in-flight results", async () => {
  let finish!: (hues: Record<string, number>) => void;
  configureAgentHues(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  requestAgentHue("shared");
  await Promise.resolve();
  configureAgentHues(async () => ({ shared: 75 }));
  assert.equal(agentHue("shared"), undefined);
  requestAgentHue("shared");
  await new Promise((resolve) => setTimeout(resolve, 10));
  finish({ shared: 15 });
  await Promise.resolve();
  assert.equal(agentHue("shared"), 75);
  configureAgentHues(undefined);
});

test("a transient failure retries without needing to remount the mark", async () => {
  let calls = 0;
  configureAgentHues(async () => {
    if (++calls === 1) throw new Error("temporarily unavailable");
    return { retried: 135 };
  });
  requestAgentHue("retried");
  await new Promise((resolve) => setTimeout(resolve, 600));
  assert.equal(calls, 2);
  assert.equal(agentHue("retried"), 135);
  configureAgentHues(undefined);
});

test("a project-only reader resolves marks without run or agent permissions", async () => {
  assert.equal(agentHuePath(["projects:read"]), "/api/project-agent-hues");
  assert.equal(agentHuePath(["runs:read"]), "/api/agent-hues");
  assert.equal(agentHuePath(["agents:read"]), undefined);
  assert.equal(agentHuePath([]), undefined);
  configureAgentHues(async () => ({ projectLead: 45 }));
  requestAgentHue("projectLead");
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.equal(agentHue("projectLead"), 45);
  configureAgentHues(undefined);
});
