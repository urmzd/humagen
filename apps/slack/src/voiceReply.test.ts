import assert from "node:assert/strict";
import { test } from "node:test";
import { NEEDED, USEFUL } from "./config.ts";
import { type SlackApi, slackClient } from "./slack.ts";
import { SpeechError, type SpokenAnswer, speakAnswer, speechCall } from "./voiceReply.ts";

const clip: SpokenAnswer = {
  audio: Buffer.from("ID3-mp3-bytes").toString("base64"),
  media_type: "audio/mpeg",
  text: "The room is booked.",
  whole: true,
  cost_micros: 418,
};

function fakeSlack() {
  const calls: { method: string; args: Record<string, unknown> }[] = [];
  const slack: SlackApi = {
    async call<T>(method: string, args: Record<string, unknown> = {}) {
      calls.push({ method, args });
      if (method === "files.getUploadURLExternal") {
        return { upload_url: "https://files.slack.com/upload/v1/abc", file_id: "F123" } as T;
      }
      return { ok: true } as T;
    },
    async respond() {},
  };
  return { slack, calls };
}

test("a spoken answer is uploaded as an audio clip in the thread", async () => {
  const { slack, calls } = fakeSlack();
  const puts: { url: string; body: Buffer }[] = [];
  const fetch = (async (url: string, init: RequestInit) => {
    puts.push({ url, body: Buffer.from(init.body as Uint8Array) });
    return new Response(null, { status: 200 });
  }) as typeof globalThis.fetch;
  const asked: string[] = [];
  const out = await speakAnswer(
    { speech: async (run) => (asked.push(run), clip), slack, fetch },
    { run: "r_1", channel: "C1", threadTs: "1700000000.000100" },
  );
  assert.deepEqual(out, { posted: true, file: "F123" });
  assert.deepEqual(asked, ["r_1"]);
  assert.deepEqual(calls[0], {
    method: "files.getUploadURLExternal",
    args: { filename: "answer.mp3", length: 13 },
  });
  assert.equal(puts[0]?.url, "https://files.slack.com/upload/v1/abc");
  assert.equal(puts[0]?.body.toString(), "ID3-mp3-bytes");
  assert.deepEqual(calls[1], {
    method: "files.completeUploadExternal",
    args: {
      files: [{ id: "F123", title: "Spoken answer" }],
      channel_id: "C1",
      thread_ts: "1700000000.000100",
    },
  });
});

test("an expected refusal skips the clip and touches nothing in Slack; anything else throws", async () => {
  for (const [status, skipped] of [
    [409, "no-voice"],
    [403, "not-allowed"],
    [422, "no-words"],
    [402, "spend-limit"],
  ] as const) {
    const { slack, calls } = fakeSlack();
    const out = await speakAnswer(
      {
        speech: async () => {
          throw new SpeechError(status, "refused");
        },
        slack,
      },
      { run: "r_1", channel: "C1", threadTs: "1.2" },
    );
    assert.deepEqual(out, { posted: false, skipped });
    assert.equal(calls.length, 0);
  }
  const { slack } = fakeSlack();
  await assert.rejects(
    speakAnswer(
      {
        speech: async () => {
          throw new SpeechError(503, "the voice provider did not answer");
        },
        slack,
      },
      { run: "r_1", channel: "C1", threadTs: "1.2" },
    ),
    /did not answer/,
  );
});

test("the speech call sends the app's token, asks once more on a stale one, and keeps the refusal's status", async () => {
  const seen: { url: string; auth?: string }[] = [];
  let first = true;
  const fetch = (async (url: string, init: RequestInit) => {
    const auth = (init.headers as Record<string, string>).authorization;
    seen.push({ url, auth });
    if (first) {
      first = false;
      return new Response("{}", { status: 401 });
    }
    return Response.json(clip);
  }) as typeof globalThis.fetch;
  const speech = speechCall({
    baseUrl: "http://humagen.test/",
    token: async (o) => (o?.fresh ? "fresh" : "stale"),
    fetch,
  });
  assert.deepEqual(await speech("r 1"), clip);
  assert.deepEqual(seen, [
    { url: "http://humagen.test/api/runs/r%201/speech", auth: "Bearer stale" },
    { url: "http://humagen.test/api/runs/r%201/speech", auth: "Bearer fresh" },
  ]);
  const refused = speechCall({
    baseUrl: "http://humagen.test",
    token: async () => "t",
    fetch: (async () =>
      Response.json({ error: "voice mode is off" }, { status: 409 })) as typeof globalThis.fetch,
  });
  await assert.rejects(
    refused("r_1"),
    (e: unknown) => e instanceof SpeechError && e.status === 409,
  );
});

test("Slack's upload calls go as forms, with the file list as a JSON string", async () => {
  const sent: { url: string; body: unknown; type?: string }[] = [];
  const fetch = (async (url: string, init: RequestInit) => {
    sent.push({
      url,
      body: init.body,
      type: (init.headers as Record<string, string>)["content-type"],
    });
    return Response.json({ ok: true, upload_url: "u", file_id: "F1" });
  }) as typeof globalThis.fetch;
  const slack = slackClient("xoxb-test", { fetch });
  await slack.call("files.getUploadURLExternal", { filename: "answer.mp3", length: 5 });
  await slack.call("files.completeUploadExternal", {
    files: [{ id: "F1", title: "Spoken answer" }],
    channel_id: "C1",
  });
  const form = sent[0]?.body as URLSearchParams;
  assert.ok(form instanceof URLSearchParams);
  assert.equal(form.get("length"), "5");
  const done = sent[1]?.body as URLSearchParams;
  assert.equal(done.get("files"), JSON.stringify([{ id: "F1", title: "Spoken answer" }]));
});

test("speech:write is useful, not needed: without it voice clips are answered in text", () => {
  assert.ok(USEFUL.includes("speech:write"));
  assert.ok(!NEEDED.includes("speech:write"));
});
