import assert from "node:assert/strict";
import { mkdtempSync, readFileSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { App } from "./app.tsx";
import { BOT, CHANNEL, FakeHumagen, FakeSlack, mention, SUBJECT, settle, TEAM } from "./harness.ts";
import { words } from "./host.ts";
import {
  defaultJournalPath,
  fileJournal,
  journaled,
  memoryJournal,
  recoverStreams,
} from "./journal.ts";

const TS = "1700000000.000100";

const path = () => join(mkdtempSync(join(tmpdir(), "journal-")), "streams.json");

function appOver(slack: FakeSlack, journalPath: string, humagen = new FakeHumagen()) {
  const journal = fileJournal(journalPath);
  const app = new App(journaled(slack, journal), humagen, {
    team: TEAM,
    botUser: BOT,
    subject: SUBJECT,
    tenant: "acme",
    defaultAgent: "lead",
    flushMs: 0,
    pollMs: 20,
  });
  return { app, humagen, journal };
}

test("journal: a stream is recorded while it is open and forgotten once it is stopped", async () => {
  const slack = new FakeSlack();
  const file = path();
  const { app, humagen, journal } = appOver(slack, file);
  await app.handle(mention("ping", TS));
  await settle();
  const [open] = journal.list();
  assert.equal(open?.channel, CHANNEL);
  assert.equal(open?.threadTs, TS);
  assert.ok(open?.ts, "the streamed message is named");
  assert.equal(JSON.parse(readFileSync(file, "utf8")).streams.length, 1, "it is on disk");

  humagen.emit(humagen.last.id, "lifecycle.accepted");
  await settle();
  humagen.emit(humagen.last.id, "lifecycle.completed", { text: "pong" });
  await settle();
  await app.idle();
  assert.equal(journal.list().length, 0);
  assert.deepEqual(JSON.parse(readFileSync(file, "utf8")).streams, []);
});

test("journal: the next start closes what a crashed process left open, with a note, and settles the session", async () => {
  const file = path();
  await journaled(new FakeSlack(), fileJournal(file)).call("chat.startStream", {
    channel: CHANNEL,
    thread_ts: TS,
  });
  // The process dies here: nothing stops the stream.

  const after = new FakeSlack();
  const journal = fileJournal(file);
  assert.equal(journal.list().length, 1, "the new process reads what the old one left");
  const left = journal.list()[0];
  assert.equal(await recoverStreams(after, journal), 1);
  assert.deepEqual(after.of("chat.stopStream"), [
    {
      channel: CHANNEL,
      ts: left?.ts,
      chunks: [{ type: "markdown_text", text: words.interrupted }],
    },
  ]);
  assert.deepEqual(after.of("agents.sessions.setStatus"), [
    { channel_id: CHANNEL, thread_ts: TS, status: "active" },
  ]);
  assert.equal(fileJournal(file).list().length, 0, "it is closed once");
  await recoverStreams(after, fileJournal(file));
  assert.equal(after.of("chat.stopStream").length, 1);
});

test("journal: a workspace that refuses the note gets a bare stop and the note in the thread", async () => {
  const slack = new FakeSlack();
  slack.failing["chat.stopStream"] = (args) => (args.chunks ? "invalid_chunks" : undefined);
  const journal = memoryJournal();
  journal.open({ channel: CHANNEL, ts: "1.1", threadTs: TS, at: "then" });
  assert.equal(await recoverStreams(slack, journal), 1);
  assert.equal(slack.of("chat.stopStream").length, 2);
  assert.equal(slack.of("chat.stopStream")[1]?.chunks, undefined);
  assert.deepEqual(slack.of("chat.postMessage")[0], {
    channel: CHANNEL,
    thread_ts: TS,
    text: words.interrupted,
  });
  assert.equal(journal.list().length, 0);
});

test("journal: a stream Slack no longer has is let go; one the network kept from closing is kept", async () => {
  const gone = new FakeSlack();
  gone.failing["chat.stopStream"] = "message_not_found";
  const j1 = memoryJournal();
  j1.open({ channel: CHANNEL, ts: "1.1", at: "then" });
  assert.equal(await recoverStreams(gone, j1), 0);
  assert.equal(j1.list().length, 0);

  const offline = new FakeSlack();
  offline.call = async () => {
    throw new TypeError("fetch failed");
  };
  const j2 = memoryJournal();
  j2.open({ channel: CHANNEL, ts: "1.2", at: "then" });
  const logs: string[] = [];
  assert.equal(await recoverStreams(offline, j2, (l) => logs.push(l)), 0);
  assert.equal(j2.list().length, 1, "it is tried again at the next start");
  assert.match(logs.join("\n"), /could not be closed yet/);
});

test("journal: a stop that fails for a reason other than a gone stream keeps the record", async () => {
  const slack = new FakeSlack();
  const journal = memoryJournal();
  const s = journaled(slack, journal);
  await s.call("chat.startStream", { channel: CHANNEL, thread_ts: TS });
  slack.failing["chat.stopStream"] = "ratelimited";
  await assert.rejects(s.call("chat.stopStream", { channel: CHANNEL, ts: journal.list()[0]?.ts }));
  assert.equal(journal.list().length, 1);
  slack.failing["chat.stopStream"] = "message_not_in_streaming_state";
  await assert.rejects(s.call("chat.stopStream", { channel: CHANNEL, ts: journal.list()[0]?.ts }));
  assert.equal(journal.list().length, 0);
});

test("journal: a malformed file is reported and read as empty; the default path is per team", () => {
  const file = path();
  writeFileSync(file, "{ not json");
  const logs: string[] = [];
  assert.equal(fileJournal(file, (l) => logs.push(l)).list().length, 0);
  assert.match(logs[0] ?? "", /could not be read/);
  assert.equal(
    defaultJournalPath(TEAM, { XDG_STATE_HOME: "/state" }),
    `/state/warp-slack/streams-${TEAM}.json`,
  );
});
