import assert from "node:assert/strict";
import { mkdtempSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { defaultCopy } from "@warp/components";
import { DEFAULT_BRANDING, parseBranding } from "./branding.ts";
import { readConfig } from "./config.ts";
import { DM, interactive, mention, settle, TEAM, USER, type World, world } from "./harness.ts";
import { words } from "./host.ts";

const BRAND = {
  name: "Acme Help",
  tagline: "Ask Acme Help anything",
  disclaimer: "{name} drafts answers. A person at Acme checks anything that matters.",
  prompts: [
    { title: "What changed?", message: "What changed in the last deploy?" },
    { title: "Who is on call?", message: "Who is on call this week?" },
  ],
};

const ENV = {
  SLACK_BOT_TOKEN: "b",
  SLACK_APP_TOKEN: "a",
  SLACK_TEAM_ID: TEAM,
  WARP_TENANT: "acme",
};

function file(content: string): string {
  const path = join(mkdtempSync(join(tmpdir(), "branding-")), "branding.json");
  writeFileSync(path, content);
  return path;
}

async function deliver(w: World, envelope: Parameters<World["app"]["handle"]>[0]) {
  await w.app.handle(envelope);
  await settle();
}

const messagesOpened = (channel = DM) => ({
  type: "events_api",
  envelope_id: "env-messages",
  payload: {
    team_id: TEAM,
    event: { type: "app_home_opened", tab: "messages", user: USER, channel },
  },
});

test("branding: no file keeps today's words, and nothing is suggested", async () => {
  const cfg = readConfig(ENV);
  assert.deepEqual(cfg.slack.branding, DEFAULT_BRANDING);

  const w = world();
  await deliver(w, messagesOpened());
  assert.equal(w.slack.of("assistant.threads.setSuggestedPrompts").length, 0);
  await deliver(w, interactive({ type: "shortcut", callback_id: "aui_ask", trigger_id: "tr-1" }));
  const view = w.slack.of("views.open")[0]?.view as { title: { text: string } };
  assert.equal(view.title.text, words.askTitle);
});

test("branding: a file is read, checked and used", () => {
  const cfg = readConfig({ ...ENV, SLACK_BRANDING: file(JSON.stringify(BRAND)) });
  assert.equal(cfg.slack.branding.name, "Acme Help");
  assert.equal(cfg.slack.branding.prompts.length, 2);
});

test("branding: a bad file stops startup and names every problem", () => {
  const bad = {
    name: "A name far too long for a modal title",
    tagline: "",
    colour: "red",
    prompts: [{ title: "x" }, { title: "y", message: "z", extra: 1 }, {}, {}, {}],
  };
  assert.throws(
    () => readConfig({ ...ENV, SLACK_BRANDING: file(JSON.stringify(bad)) }),
    (e: Error) =>
      e.message.includes("SLACK_BRANDING") &&
      /name must be at most 20/.test(e.message) &&
      /tagline must be a non-empty string/.test(e.message) &&
      /unknown key "colour"/.test(e.message) &&
      /prompts must hold at most 4/.test(e.message) &&
      /prompts\[0\]\.message must be a non-empty string/.test(e.message) &&
      /prompts\[1\] has unknown key "extra"/.test(e.message),
  );
  assert.throws(
    () => readConfig({ ...ENV, SLACK_BRANDING: file("{ not json") }),
    /SLACK_BRANDING .* could not be read/,
  );
  assert.throws(
    () => readConfig({ ...ENV, SLACK_BRANDING: "/nowhere/branding.json" }),
    /could not be read/,
  );
  assert.throws(() => parseBranding([]), /must be a JSON object/);
});

test("branding: the prompts are suggested in the app's messages and in a new assistant thread", async () => {
  const w = world({ branding: parseBranding(BRAND) });
  await deliver(w, messagesOpened());
  const [set] = w.slack.of("assistant.threads.setSuggestedPrompts");
  assert.deepEqual(set, {
    channel_id: DM,
    title: BRAND.tagline,
    prompts: BRAND.prompts,
  });

  await deliver(w, {
    type: "events_api",
    envelope_id: "env-thread",
    payload: {
      team_id: TEAM,
      event: {
        type: "assistant_thread_started",
        assistant_thread: { user_id: USER, channel_id: DM, thread_ts: "1700000000.000900" },
      },
    },
  });
  assert.equal(
    w.slack.of("assistant.threads.setSuggestedPrompts")[1]?.thread_ts,
    "1700000000.000900",
  );
});

test("branding: the ask form and every answer carry the brand", async () => {
  const w = world({ branding: parseBranding(BRAND) });
  await deliver(w, interactive({ type: "shortcut", callback_id: "aui_ask", trigger_id: "tr-2" }));
  const view = w.slack.of("views.open")[0]?.view as { title: { text: string } };
  assert.equal(view.title.text, "Ask Acme Help");

  await deliver(w, mention("ping", "1700000000.000100"));
  w.warp.emit(w.warp.last.id, "lifecycle.accepted");
  await settle();
  w.warp.emit(w.warp.last.id, "lifecycle.completed", { text: "pong" });
  await settle();
  await w.app.idle();
  const final = JSON.stringify(w.slack.of("chat.stopStream").at(-1)?.blocks ?? []);
  assert.match(final, /lead drafts answers\. A person at Acme checks anything that matters\./);
  assert.ok(!final.includes(defaultCopy.disclaimer("lead")), "the default disclaimer is replaced");
});

test("branding: a name alone puts the brand in the default disclaimer", async () => {
  const w = world({ branding: parseBranding({ name: "Acme Help" }) });
  await deliver(w, mention("ping", "1700000000.000200"));
  w.warp.emit(w.warp.last.id, "lifecycle.accepted");
  await settle();
  w.warp.emit(w.warp.last.id, "lifecycle.completed", { text: "pong" });
  await settle();
  await w.app.idle();
  const final = JSON.stringify(w.slack.of("chat.stopStream").at(-1)?.blocks ?? []);
  assert.match(final, /Acme Help is AI/);
});
