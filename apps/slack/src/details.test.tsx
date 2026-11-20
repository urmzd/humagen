import "./react.ts";
import assert from "node:assert/strict";
import { test } from "node:test";
import { defaultCopy } from "@humagen/components";
import { workTurn } from "@humagen/core/fixtures";
import { offersRunDetails, runDetailsReply } from "./details.tsx";
import {
  CHANNEL,
  click,
  FakeSlack,
  mention,
  OTHER_USER,
  SUBJECT,
  settle,
  TEAM,
  USER,
  type World,
  world,
} from "./harness.ts";
import type { ActAs, ClientRecord } from "./humagen.ts";
import { Presenter } from "./present.tsx";
import { validateBlocks } from "./validate.ts";

const TS = "1700000000.000100";
const CLIENT = "c_slack";
const STRANGER = "U0STRANGER";

/** Every Block Kit payload these tests hand to Slack, for the validity check at the end. */
const produced: { from: string; blocks: unknown[] }[] = [];
function collect(name: string, slack: FakeSlack) {
  for (const c of slack.calls) {
    if (Array.isArray(c.args.blocks) && c.args.blocks.length > 0) {
      produced.push({ from: `${name}: ${c.method}`, blocks: c.args.blocks });
    }
  }
}

async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0]) {
  await w.app.handle(envelope);
  await settle();
}
async function emit(w: World, kind: string, payload?: unknown) {
  w.humagen.emit(w.humagen.last.id, kind, payload);
  await settle();
}
const blocksOf = (args: Record<string, unknown> | undefined) => (args?.blocks ?? []) as unknown[];

/** A world whose client names this app and maps two Slack people. */
function mapped(): World {
  const w = world({ clientId: CLIENT });
  w.humagen.clients.set(CLIENT, {
    id: CLIENT,
    surface: "slack",
    enabled: true,
    default_agent: "lead",
    identity: { team: TEAM },
    slack: { machine: SUBJECT, people: { [USER]: "ana", [OTHER_USER]: "olu" } },
  } as ClientRecord);
  return w;
}

/**
 * A run that calls the platform's `shell` tool twice, in the wire shape the
 * server sends: the command in the call's arguments, and a result that opens
 * with `exit <code>` (`internal/platform/isolation.go`) whether it passed or not.
 */
async function runWithCommands(w: World) {
  await deliver(w, mention("fix the refund rounding", TS));
  await emit(w, "run.accepted.v1");
  await emit(w, "run.started.v1", { attempt: 1 });
  await emit(w, "run.delta.tool.call.v1", {
    id: "t1",
    name: "shell",
    phase: "executing",
    arguments: { command: "pnpm test src/refund.test.ts" },
  });
  await emit(w, "run.delta.tool.done.v1", {
    id: "t1",
    name: "shell",
    result: "exit 0\n Tests  7 passed (7)",
  });
  await emit(w, "run.delta.tool.call.v1", {
    id: "t2",
    name: "shell",
    phase: "executing",
    arguments: { command: "pnpm exec biome check src" },
  });
  await emit(w, "run.delta.tool.done.v1", {
    id: "t2",
    name: "shell",
    result: "exit 2\nsrc/ledger.ts:3:10 lint/correctness/noUnusedImports\nFound 1 error.",
  });
  await emit(w, "run.completed.v1", { text: "Rounding is fixed. Lint still fails." });
  await w.app.idle();
  const final = w.slack.of("chat.stopStream").at(-1);
  assert.ok(final, "the streamed turn was closed with its final blocks");
  return final;
}

test("a shell command that exits non-zero reaches Slack with its command, exit code and output", async () => {
  const w = mapped();
  const final = await runWithCommands(w);
  const text = JSON.stringify(blocksOf(final));
  assert.match(text, /\*Failed\* · exit 2/);
  assert.match(text, /\$ pnpm exec biome check src/);
  assert.match(text, /Found 1 error\./);
  // The command that passed is not drawn: it stays a step.
  assert.doesNotMatch(text, /pnpm test src\/refund\.test\.ts/);
  assert.doesNotMatch(text, /Passed/);
  collect("failed command", w.slack);
});

test("Run Details answers the person who clicked, alone, with what the run used", async () => {
  const w = mapped();
  const final = await runWithCommands(w);
  const run = w.humagen.last.id;
  w.humagen.set(run, {
    usage: {
      own: {},
      total: {
        prompt_tokens: 48_210,
        cached_prompt_tokens: 39_000,
        completion_tokens: 1_840,
        cost: { micros: 42_100, currency: "USD" },
      },
      context: { model: "claude-sonnet-4-5" },
    },
  });
  // The turn itself says nothing of tokens or cost.
  assert.doesNotMatch(JSON.stringify(blocksOf(final)), /token|Cost|\$0\./i);
  const read: { id: string; as: ActAs }[] = [];
  const getRunFor = w.humagen.getRunFor.bind(w.humagen);
  w.humagen.getRunFor = async (id, as) => {
    read.push({ id, as });
    return getRunFor(id, as);
  };
  await deliver(
    w,
    click(blocksOf(final), defaultCopy.runDetails.button, {
      ts: String(final.ts),
      threadTs: TS,
      user: OTHER_USER,
    }),
  );
  await w.app.idle();
  // Read as the person who clicked, through the client, never as the app's own machine.
  assert.deepEqual(read, [{ id: run, as: { client: CLIENT, user: OTHER_USER } }]);
  const shown = w.slack.of("chat.postEphemeral").at(-1);
  assert.equal(shown?.user, OTHER_USER);
  assert.equal(shown?.channel, CHANNEL);
  assert.equal(shown?.thread_ts, TS);
  const text = JSON.stringify(shown?.blocks);
  for (const want of [
    "*Agent*\\nlead",
    "*Model*\\nclaude-sonnet-4-5",
    "*Input tokens*\\n48,210 (39,000 from cache)",
    "*Output tokens*\\n1,840",
    "*Cost*\\n$0.0421",
    "*Elapsed*",
    "Only you can see this.",
  ]) {
    assert.ok(text.includes(want), `${want} is missing from ${text}`);
  }
  // Nothing was posted for the channel to read.
  assert.equal(w.slack.of("chat.postMessage").length, 0);
  collect("run details", w.slack);
});

test("someone the client maps to nobody is told so, and Humagen is not asked", async () => {
  const w = mapped();
  const final = await runWithCommands(w);
  let asked = 0;
  w.humagen.getRunFor = async () => {
    asked += 1;
    return null;
  };
  await deliver(
    w,
    click(blocksOf(final), defaultCopy.runDetails.button, {
      ts: String(final.ts),
      threadTs: TS,
      user: STRANGER,
    }),
  );
  await w.app.idle();
  assert.equal(asked, 0);
  const shown = w.slack.of("chat.postEphemeral").at(-1);
  assert.equal(shown?.user, STRANGER);
  assert.equal(shown?.text, defaultCopy.runDetails.notConnected);
  assert.equal(shown?.blocks, undefined);
});

test("a person who may not read the run is refused in Humagen's answer, with no figures", async () => {
  const w = mapped();
  const final = await runWithCommands(w);
  // Humagen answers 404 for a run this person may not read.
  w.humagen.getRunFor = async () => null;
  await deliver(
    w,
    click(blocksOf(final), defaultCopy.runDetails.button, {
      ts: String(final.ts),
      threadTs: TS,
      user: OTHER_USER,
    }),
  );
  await w.app.idle();
  const shown = w.slack.of("chat.postEphemeral").at(-1);
  assert.equal(shown?.text, defaultCopy.runDetails.notReadable);
  assert.equal(shown?.blocks, undefined);
});

test("an app whose client maps nobody offers no Run Details", async () => {
  const w = world();
  const final = await runWithCommands(w);
  assert.doesNotMatch(JSON.stringify(blocksOf(final)), /Run Details/);
  assert.equal(offersRunDetails(undefined, SUBJECT), false);
  const client = {
    id: CLIENT,
    surface: "slack",
    enabled: true,
    agents: [],
    slack: { machine: SUBJECT, people: { [USER]: "ana" } },
  } as ClientRecord;
  assert.equal(offersRunDetails(client, SUBJECT), true);
  assert.equal(offersRunDetails(client, "another-machine"), false);
  assert.equal(offersRunDetails({ ...client, enabled: false }, SUBJECT), false);
  assert.equal(
    offersRunDetails(
      { ...client, slack: { machine: SUBJECT, people: {} } } as ClientRecord,
      SUBJECT,
    ),
    false,
  );
});

test("a bridged outcome offers no Run Details and shows no command output", async () => {
  const slack = new FakeSlack();
  const presenter = new Presenter(
    slack,
    { channel: CHANNEL, threadTs: TS, triggerTs: TS, recipientUser: USER, recipientTeam: TEAM },
    { outcomesOnly: true, capabilities: { runDetails: true } },
  );
  await presenter.finish(workTurn);
  const text = JSON.stringify(slack.of("chat.postMessage").at(-1)?.blocks);
  assert.doesNotMatch(text, /Run Details/);
  assert.doesNotMatch(text, /biome check/);
  const direct = new FakeSlack();
  await new Presenter(
    direct,
    { channel: CHANNEL, threadTs: TS, triggerTs: TS, recipientUser: USER, recipientTeam: TEAM },
    { native: false, capabilities: { runDetails: true } },
  ).finish(workTurn);
  const posted = JSON.stringify(direct.of("chat.postMessage").at(-1)?.blocks);
  assert.match(posted, /Run Details/);
  assert.match(
    posted,
    /Ran 2 searches, read 3 files, ran 3 commands \(2 failed\), updated 2 files \(\+4\)/,
  );
  collect("posted turn", direct);
});

test("a read that fails says so and shows nothing", async () => {
  const reply = await runDetailsReply(
    {
      humagen: {
        getRunFor: async () => {
          throw new Error("503");
        },
      },
      actAs: async (user) => ({ client: CLIENT, user }),
    },
    "r1",
    USER,
  );
  assert.deepEqual(reply, { refused: "unavailable", text: defaultCopy.runDetails.unavailable });
});

test(
  "every Block Kit payload these tests produced is one Slack accepts",
  { skip: !process.env.SLACK_VALIDATE },
  async () => {
    assert.ok(produced.length > 3);
    const failed: string[] = [];
    for (const p of produced) {
      const res = await validateBlocks(p.blocks);
      if (!res.ok) failed.push(`${p.from}: ${JSON.stringify(res.errors ?? res.error)}`);
    }
    assert.deepEqual(failed, []);
  },
);
