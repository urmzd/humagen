import assert from "node:assert/strict";
import { test } from "node:test";
import { readConfig } from "./config.ts";
import {
  type Envelope,
  type SlackApi,
  SocketGaveUp,
  type SocketLike,
  socketMode,
} from "./slack.ts";

/** A socket the test speaks for Slack through. */
class FakeSocket implements SocketLike {
  onopen: ((ev: unknown) => void) | null = null;
  onmessage: ((ev: { data: unknown }) => void) | null = null;
  onclose: ((ev: unknown) => void) | null = null;
  onerror: ((ev: unknown) => void) | null = null;
  readonly sent: string[] = [];
  closed = false;
  constructor(readonly url: string) {}
  send(data: string) {
    this.sent.push(data);
  }
  close() {
    if (this.closed) return;
    this.closed = true;
    queueMicrotask(() => this.onclose?.({}));
  }
  deliver(envelope: Record<string, unknown>) {
    this.onmessage?.({ data: JSON.stringify(envelope) });
  }
  /** The network went away under it: no close, no frames. */
  die() {
    this.onmessage = null;
    this.onclose = null;
  }
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

async function until(check: () => boolean, ms = 2_000) {
  const end = Date.now() + ms;
  while (!check()) {
    if (Date.now() > end) throw new Error("timed out");
    await wait(2);
  }
}

function slackWith(opts: { fail?: number | "always"; hello?: boolean } = {}) {
  const sockets: FakeSocket[] = [];
  let opens = 0;
  const app: SlackApi = {
    async call<T>(method: string): Promise<T> {
      assert.equal(method, "apps.connections.open");
      opens += 1;
      if (opts.fail === "always" || (typeof opts.fail === "number" && opens <= opts.fail)) {
        throw new Error("fetch failed");
      }
      return { url: `wss://slack.test/${opens}` } as T;
    },
    async respond() {},
  };
  const socket = (url: string) => {
    const ws = new FakeSocket(url);
    sockets.push(ws);
    // Slack says hello as soon as a connection is up.
    if (opts.hello !== false) queueMicrotask(() => ws.deliver({ type: "hello" }));
    return ws;
  };
  return { app, socket, sockets, opens: () => opens };
}

const fast = { tickMs: 10, helloMs: 100, backoffMs: 1, maxBackoffMs: 5 };

test("socket: a delivery is acknowledged before it is handled", async () => {
  const s = slackWith();
  const stop = new AbortController();
  const got: Envelope[] = [];
  const done = socketMode(s.app, async (e) => void got.push(e), {
    ...fast,
    socket: s.socket,
    signal: stop.signal,
  });
  await until(() => s.sockets.length === 1);
  await wait(5);
  s.sockets[0]?.deliver({ type: "events_api", envelope_id: "e1", payload: {} });
  assert.deepEqual(s.sockets[0]?.sent, [JSON.stringify({ envelope_id: "e1" })]);
  assert.equal(got.at(-1)?.envelope_id, "e1");
  stop.abort();
  await done;
  assert.ok(s.sockets[0]?.closed, "shutdown closes the socket");
});

test("socket: a silent connection is replaced, the new one first, and nothing more is read from the old", async () => {
  const s = slackWith();
  const stop = new AbortController();
  const logs: string[] = [];
  const done = socketMode(s.app, async () => {}, {
    ...fast,
    idleMs: 60,
    socket: s.socket,
    signal: stop.signal,
    log: (l) => logs.push(l),
  });
  await until(() => s.sockets.length === 1);
  s.sockets[0]?.die();
  await until(() => s.sockets.length === 2 && Boolean(s.sockets[0]?.closed));
  assert.match(logs.join("\n"), /nothing from Slack for 60ms/);
  assert.match(logs.join("\n"), /reconnected to Slack/);
  stop.abort();
  await done;
});

test("socket: waking from sleep replaces the connection", async () => {
  const s = slackWith();
  const stop = new AbortController();
  const logs: string[] = [];
  const done = socketMode(s.app, async () => {}, {
    ...fast,
    sleepMs: 50,
    socket: s.socket,
    signal: stop.signal,
    log: (l) => logs.push(l),
  });
  await until(() => s.sockets.length === 1);
  await wait(15);
  // The process is frozen, as a laptop lid closing would freeze it.
  const frozen = Date.now() + 150;
  while (Date.now() < frozen) {}
  await until(() => s.sockets.length === 2);
  assert.match(logs.join("\n"), /asleep/);
  stop.abort();
  await done;
});

test("socket: a close or a disconnect opens the next connection", async () => {
  const s = slackWith();
  const stop = new AbortController();
  const done = socketMode(s.app, async () => {}, {
    ...fast,
    socket: s.socket,
    signal: stop.signal,
  });
  await until(() => s.sockets.length === 1);
  await wait(5);
  s.sockets[0]?.close();
  await until(() => s.sockets.length === 2);
  await wait(5);
  s.sockets[1]?.deliver({ type: "disconnect", reason: "refresh_requested" });
  await until(() => s.sockets.length === 3 && Boolean(s.sockets[1]?.closed));
  stop.abort();
  await done;
});

test("socket: failures back off and recover; too many in a row give up so a supervisor restarts", async () => {
  const flaky = slackWith({ fail: 3 });
  const stop = new AbortController();
  const done = socketMode(flaky.app, async () => {}, {
    ...fast,
    maxAttempts: 5,
    socket: flaky.socket,
    signal: stop.signal,
  });
  await until(() => flaky.sockets.length === 1);
  assert.equal(flaky.opens(), 4);
  stop.abort();
  await done;

  const down = slackWith({ fail: "always" });
  await assert.rejects(
    socketMode(down.app, async () => {}, { ...fast, maxAttempts: 4, socket: down.socket }),
    (e: Error) => e instanceof SocketGaveUp && /4 failed attempts/.test(e.message),
  );
  assert.equal(down.opens(), 4);

  const mute = slackWith({ hello: false });
  await assert.rejects(
    socketMode(mute.app, async () => {}, {
      ...fast,
      helloMs: 10,
      maxAttempts: 2,
      socket: mute.socket,
    }),
    /no hello within 10ms/,
  );
  assert.equal(mute.sockets.length, 2);
  assert.ok(mute.sockets.every((ws) => ws.closed));
});

test("socket: liveness settings are read from the environment and checked", () => {
  const env = {
    SLACK_BOT_TOKEN: "b",
    SLACK_APP_TOKEN: "a",
    SLACK_TEAM_ID: "T1",
    HUMAGEN_TENANT: "t",
  };
  assert.deepEqual(readConfig(env).slack.socket, {});
  assert.deepEqual(
    readConfig({ ...env, SLACK_SOCKET_IDLE_MS: "60000", SLACK_SOCKET_MAX_ATTEMPTS: "3" }).slack
      .socket,
    { idleMs: 60_000, maxAttempts: 3 },
  );
  assert.throws(
    () => readConfig({ ...env, SLACK_SOCKET_IDLE_MS: "5", SLACK_SOCKET_MAX_ATTEMPTS: "x" }),
    /SLACK_SOCKET_IDLE_MS must be .*; SLACK_SOCKET_MAX_ATTEMPTS must be/,
  );
});
