import { defaultCopy } from "@humagen/components";
import type { TurnEvent } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { describe, expect, it } from "vitest";
import { SlackError, slackClient, slackCode } from "./client";
import { type StreamTarget, streamTurn, TurnStream } from "./turn";

type Call = { method: string; args: Record<string, unknown> };

/** A Slack that records every call and answers each method as told. */
function fakeSlack(
  refuse: Record<string, string> = {},
  /** Refuse one call: the nth (1-based) call of a method. */
  once?: { method: string; nth: number; code: string },
) {
  const calls: Call[] = [];
  let n = 0;
  return {
    calls,
    methods: () => calls.map((c) => c.method),
    async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
      calls.push({ method, args });
      // Let other callers interleave, as a real network would.
      await Promise.resolve();
      const code = refuse[method];
      if (code) throw new SlackError(method, code);
      if (
        once &&
        method === once.method &&
        calls.filter((c) => c.method === method).length === once.nth
      ) {
        throw new SlackError(method, once.code);
      }
      n += 1;
      return { ok: true, ts: `100.${n}`, channel: "C1" } as T;
    },
  };
}

const target: StreamTarget = {
  channel: "C1",
  threadTs: "1.0",
  triggerTs: "1.0",
  recipientUser: "U1",
  recipientTeam: "T1",
};

const answer: TurnEvent[] = [
  {
    type: "turn.started",
    turnId: "t1",
    agent: { name: "Assistant" },
    sessionUrl: "https://example.com/t/1",
  },
  ...["The ", "rate ", "is ", "back ", "under ", "0.1%."].map((text) => ({
    type: "text.delta" as const,
    text,
  })),
  { type: "turn.completed" },
];

describe("streamTurn", () => {
  it("streams natively: reaction, session, start, appends, then stop with the final blocks", async () => {
    const slack = fakeSlack();
    const { turn, ts, messages } = await streamTurn(slack, target, answer, { tickMs: 0 });
    expect(turn.status).toBe("done");
    expect(turn.text).toBe("The rate is back under 0.1%.");
    const methods = slack.methods();
    expect(methods.slice(0, 3)).toEqual([
      "reactions.add",
      "agents.sessions.setStatus",
      "chat.startStream",
    ]);
    expect(methods).toContain("chat.appendStream");
    const stop = slack.calls.find((c) => c.method === "chat.stopStream");
    expect(stop?.args.blocks).toEqual(
      expect.arrayContaining([expect.objectContaining({ type: "context_actions" })]),
    );
    expect(messages).toEqual([ts]);
    // The last thing told is the settled session.
    expect(slack.calls.at(-1)).toMatchObject({
      method: "agents.sessions.setStatus",
      args: { status: "active" },
    });
  });

  it("leaves out feedback when the host has nowhere to keep it", async () => {
    const slack = fakeSlack();
    await streamTurn(slack, target, answer, { tickMs: 0, capabilities: { feedback: false } });
    const stop = slack.calls.find((c) => c.method === "chat.stopStream");
    expect(JSON.stringify(stop?.args.blocks)).not.toContain("feedback_buttons");
  });

  it("posts and edits when the workspace refuses native streaming", async () => {
    const slack = fakeSlack({ "chat.startStream": "not_allowed_token_type" });
    const stream = new TurnStream(slack, target);
    await stream.update({ ...fx.doneTurn, status: "working" });
    expect(stream.streaming).toBe(false);
    await stream.finish(fx.doneTurn);
    expect(slack.methods()).toEqual(expect.arrayContaining(["chat.postMessage", "chat.update"]));
    expect(slack.methods()).not.toContain("chat.appendStream");
  });

  it("continues an overflowing turn with the shared continued copy", async () => {
    const slack = fakeSlack();
    const stream = new TurnStream(slack, target, { native: false });
    const huge = { ...fx.doneTurn, text: "word ".repeat(9000) };
    const { messages } = await stream.finish(huge);
    const posts = slack.calls.filter((c) => c.method === "chat.postMessage");
    expect(posts.length).toBeGreaterThan(1);
    expect(messages).toHaveLength(posts.length);
    const text = String(posts[1]?.args.text);
    expect(text.startsWith(defaultCopy.continued(2, posts.length, "").trim())).toBe(true);
  });

  it("stops a turn whose signal was aborted", async () => {
    const slack = fakeSlack();
    const abort = new AbortController();
    abort.abort();
    const { turn } = await streamTurn(slack, target, answer, { tickMs: 0, signal: abort.signal });
    expect(turn.status).toBe("stopped");
  });
});

describe("TurnStream", () => {
  it("serializes calls made at once, so one turn is one message", async () => {
    const slack = fakeSlack({ "chat.startStream": "not_allowed_token_type" });
    const stream = new TurnStream(slack, target);
    const working = { ...fx.doneTurn, status: "working" as const };
    await Promise.all([stream.update(working), stream.update(working), stream.finish(fx.doneTurn)]);
    expect(slack.methods().filter((m) => m === "chat.postMessage")).toHaveLength(1);
  });

  it("sends only the pages Slack doesn't have when a continuation fails", async () => {
    const slack = fakeSlack({}, { method: "chat.postMessage", nth: 2, code: "internal_error" });
    const stream = new TurnStream(slack, target, { native: false });
    const huge = { ...fx.doneTurn, text: "word ".repeat(9000) };
    await stream.finish(huge);
    const posts = slack.calls.filter((c) => c.method === "chat.postMessage");
    const firsts = posts.filter((c) => !String(c.args.text).startsWith("(2/"));
    // Page 1 once, page 2 twice (the failed try and its retry), nothing posted whole again.
    expect(firsts.filter((c) => !String(c.args.text).startsWith("("))).toHaveLength(1);
    expect(posts.filter((c) => String(c.args.text).startsWith("(2/"))).toHaveLength(2);
  });

  it("labels a continuation in words when the workspace refuses its header block", async () => {
    let starts = 0;
    const slack = {
      calls: [] as Call[],
      async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
        this.calls.push({ method, args });
        if (method === "chat.startStream" && args.chunks) {
          starts += 1;
          throw new SlackError(method, "invalid_chunks");
        }
        if (
          method === "chat.appendStream" &&
          this.calls.filter((c) => c.method === method).length === 1
        ) {
          throw new SlackError(method, "msg_too_long");
        }
        return { ok: true, ts: `9.${this.calls.length}` } as T;
      },
    };
    const stream = new TurnStream(slack, target);
    await stream.update({ ...fx.doneTurn, status: "working" });
    const reopened = slack.calls.filter((c) => c.method === "chat.startStream").at(-1);
    expect(starts).toBeGreaterThan(0);
    expect(reopened?.args.markdown_text).toBe(`_${defaultCopy.part(2)}_\n\n`);
  });
});

describe("slackClient", () => {
  it("throws Slack's error code, readable from any client's error", async () => {
    const fetch = async () =>
      new Response(JSON.stringify({ ok: false, error: "channel_not_found" }), { status: 200 });
    const slack = slackClient("xoxb-test", { fetch });
    const e = await slack.call("chat.postMessage").catch((x: unknown) => x);
    expect(e).toBeInstanceOf(SlackError);
    expect(slackCode(e)).toBe("channel_not_found");
    expect(slackCode(new Error("plain"))).toBeUndefined();
  });

  it("answers only through Slack's own hooks", async () => {
    const seen: string[] = [];
    const fetch = async (url: string | URL | Request) => {
      seen.push(String(url));
      return new Response("ok");
    };
    const slack = slackClient("xoxb-test", { fetch: fetch as typeof globalThis.fetch });
    await slack.respond("https://evil.example.com/hook", { text: "x" });
    await slack.respond("https://hooks.slack.com/commands/T/1/abc", { text: "x" });
    expect(seen).toEqual(["https://hooks.slack.com/commands/T/1/abc"]);
  });

  it("waits a second on an unreadable Retry-After, and never longer than a minute", async () => {
    const waits: number[] = [];
    const real = globalThis.setTimeout;
    globalThis.setTimeout = ((fn: () => void, ms: number) => {
      waits.push(ms);
      return real(fn, 0);
    }) as typeof setTimeout;
    try {
      let n = 0;
      const fetch = async () => {
        n += 1;
        if (n === 1) return new Response("", { status: 429, headers: { "retry-after": "soon" } });
        if (n === 2) return new Response("", { status: 429, headers: { "retry-after": "3600" } });
        return new Response(JSON.stringify({ ok: true }));
      };
      await slackClient("xoxb-test", { fetch }).form("files.getUploadURLExternal", {});
      expect(waits).toEqual([1000, 60_000]);
    } finally {
      globalThis.setTimeout = real;
    }
  });

  it("names the method when Slack answers with something that isn't JSON", async () => {
    const fetch = async () => new Response("<html>bad gateway</html>", { status: 502 });
    const e = await slackClient("xoxb-test", { fetch, retries: 0 })
      .call("chat.update")
      .catch((x: unknown) => x);
    expect(e).toBeInstanceOf(SlackError);
    expect(e).toMatchObject({ method: "chat.update", code: "http_502" });
  });

  it("says so when a response_url refuses the answer", async () => {
    const fetch = async () => new Response("expired", { status: 404 });
    const slack = slackClient("xoxb-test", { fetch });
    await expect(
      slack.respond("https://hooks.slack.com/commands/T/1/abc", { text: "x" }),
    ).rejects.toMatchObject({ code: "http_404" });
  });
});
