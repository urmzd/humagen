import assert from "node:assert/strict";
import { type ChildProcess, execFileSync, spawn } from "node:child_process";
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { createServer as createHTTPServer, type Server } from "node:http";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { after, before, test } from "node:test";
import { follow } from "@humagen/adapter";
import { defaultCopy } from "@humagen/components";
import { App } from "./app.tsx";
import {
  CHANNEL,
  FakeSlack,
  interactive,
  mention,
  OTHER_USER,
  settle,
  stopPressed,
  TEAM,
  USER,
} from "./harness.ts";
import { clientCredentials, type Humagen, httpHumagen, type ThreadPosted } from "./humagen.ts";
import { runId, threadKey } from "./keys.ts";

/**
 * The stand-in client and the app against the real humagen server, built
 * from this checkout and started with its development identity provider on
 * loopback (`-auth=mock`): the app signs in as a machine of its own, as it
 * does in production, and the tests' own calls as an admin machine. There is no
 * built-in model, so its agents, lead and helper, run on a stand-in model
 * served here: an OpenAI-compatible server that answers every call with
 * REPLY. No provider key is in its environment. Slack is the fake.
 *
 * Run with `pnpm test:server`. It needs Go.
 */
const enabled = process.env.HUMAGEN_SERVER_TEST === "1";
const root = new URL("../../..", import.meta.url).pathname;
let server: ChildProcess | undefined;
let model: Server | undefined;
let dir = "";
let base = "";
let idp = "";

/** The machines the identity provider signs in: the Slack app, and an admin for the tests' own calls. */
const APP_SCOPES = ["runs:read", "runs:write", "agents:read", "clients:read", "projects:read"];
const ADMIN_SCOPES = [
  "agents:read",
  "agents:write",
  "runs:read",
  "runs:write",
  "alerts:read",
  "alerts:write",
  "clients:read",
  "clients:write",
  "projects:read",
  "projects:write",
];
/** Whom the app's client maps USER to: a person the policy grants operator. */
const PERSON = "ana";

/** What the stand-in model answers every call with. */
const REPLY = "Answered by the stand-in model.";

/** An OpenAI-compatible chat completions server that streams REPLY. */
async function standInModel(): Promise<{ server: Server; base: string }> {
  const s = createHTTPServer(async (req, res) => {
    for await (const _ of req) {
      // The request is read and not looked at.
    }
    if (req.method !== "POST" || !req.url?.endsWith("/chat/completions")) {
      res.writeHead(404).end();
      return;
    }
    const chunk = (delta: unknown, finish_reason?: string) =>
      `data: ${JSON.stringify({ id: "stand-in", object: "chat.completion.chunk", model: "stand-in", choices: [{ index: 0, delta, finish_reason }] })}\n\n`;
    res.writeHead(200, { "Content-Type": "text/event-stream" });
    res.write(chunk({ role: "assistant", content: REPLY }));
    res.write(chunk({}, "stop"));
    res.end("data: [DONE]\n\n");
  });
  await new Promise<void>((resolve) => s.listen(0, "127.0.0.1", resolve));
  return { server: s, base: `http://127.0.0.1:${(s.address() as { port: number }).port}/v1` };
}

/**
 * The spec the server starts with: the stand-in model, a model server on a
 * loopback port nothing listens on, lead and helper, and lead as the default.
 */
const specFor = (modelBase: string, closedBase: string) => `api_version: humagen/v1
kind: Platform
metadata: {name: default}
spec:
  models:
    - name: stand-in
      kind: openai
      openai: {base_url: "${modelBase}"}
    - name: unreachable
      kind: openai
      openai: {base_url: "${closedBase}"}
  workspace:
    agents: {enable: ["*"], default: lead}
  agents:
    - spec:
        name: lead
        description: Takes a request and may call the helper.
        provider: stand-in
        model: stand-in
        system_prompt: Take the request and answer it.
        access: {spawn: [helper]}
    - spec:
        name: helper
        description: Does one piece of work for the lead.
        provider: stand-in
        model: stand-in
        system_prompt: Do the one piece of work you are given.
`;

const freePort = () =>
  new Promise<number>((resolve, reject) => {
    const s = createServer();
    s.once("error", reject);
    s.listen(0, "127.0.0.1", () => {
      const port = (s.address() as { port: number }).port;
      s.close(() => resolve(port));
    });
  });

before(async () => {
  if (!enabled) return;
  dir = mkdtempSync(join(tmpdir(), "humagen-slack-"));
  const bin = join(dir, "humagen");
  execFileSync("go", ["build", "-o", bin, "./cmd/humagen"], { cwd: root, stdio: "inherit" });
  const port = await freePort();
  base = `http://127.0.0.1:${port}`;
  const idpPort = await freePort();
  idp = `http://127.0.0.1:${idpPort}`;
  const personas = join(dir, "personas.json");
  writeFileSync(
    personas,
    JSON.stringify({
      machines: [
        {
          client_id: "slack-app",
          secret: "slack-secret",
          scopes: APP_SCOPES.map((s) => `humagen:${s}`),
        },
        {
          client_id: "admin-ci",
          secret: "admin-secret",
          scopes: ADMIN_SCOPES.map((s) => `humagen:${s}`),
        },
      ],
    }),
  );
  const stand = await standInModel();
  model = stand.server;
  const spec = join(dir, "humagen.yaml");
  // A port taken and given back: nothing listens on it, so every call to it is refused.
  const closed = `http://127.0.0.1:${await freePort()}/v1`;
  writeFileSync(spec, specFor(stand.base, closed));
  // The built-in policy, and the person the app's client maps granted
  // operator by subject: the Slack app asks for a person the client maps,
  // and that person must hold runs:write by what the policy grants them.
  const policy = join(dir, "policy.authz");
  writeFileSync(
    policy,
    `${readFileSync(join(root, "pkg/auth/default.authz"), "utf8")}\ngrant operator to subject "${PERSON}"\n`,
  );
  // Only what the server needs to start. No provider key is passed on; the
  // openai provider points at a local model server, as Ollama serves it.
  const env = {
    ...Object.fromEntries(
      ["PATH", "HOME", "TMPDIR"].flatMap((k) => (process.env[k] ? [[k, process.env[k]]] : [])),
    ),
    OPENAI_BASE_URL: "http://127.0.0.1:11434/v1",
    HUMAGEN_AUTHZ_POLICY: policy,
  };
  server = spawn(
    bin,
    [
      "serve",
      "-auth=mock",
      "-mock-idp-addr",
      `127.0.0.1:${idpPort}`,
      "-mock-personas",
      personas,
      "-f",
      spec,
      "-sandbox=unisolated",
      "-workspace",
      join(dir, "workspace"),
      "-addr",
      `127.0.0.1:${port}`,
    ],
    {
      cwd: dir,
      env,
      stdio: ["ignore", "ignore", "pipe"],
    },
  );
  let startup = "";
  server.stderr?.on("data", (chunk) => {
    startup = (startup + String(chunk)).slice(-4000);
  });
  for (let i = 0; i < 100; i++) {
    const up = await fetch(`${base}/healthz`).then(
      (r) => r.ok,
      () => false,
    );
    if (up) return;
    await new Promise((r) => setTimeout(r, 100));
  }
  throw new Error(`humagen did not start: ${startup}`);
});

after(() => {
  server?.kill();
  model?.close();
  if (dir) rmSync(dir, { recursive: true, force: true });
});

/** A machine's token from the identity provider, by client credentials. */
const machine = (clientId: string, secret: string, scopes: string[]) =>
  clientCredentials({
    tokenUrl: `${idp}/token`,
    clientId,
    clientSecret: secret,
    scopes: scopes.map((s) => `humagen:${s}`),
    audience: "api://humagen-dev",
  });
const admin = () => machine("admin-ci", "admin-secret", ADMIN_SCOPES);
/** The tests' own client: an admin machine. */
const client = (): Humagen => httpHumagen({ baseUrl: base, token: admin() });
/** The Slack app's client: a machine of its own, which its client record names. */
const appHumagen = (): Humagen =>
  httpHumagen({ baseUrl: base, token: machine("slack-app", "slack-secret", APP_SCOPES) });
/** A call the tests make themselves, as the admin machine. */
const asAdmin = async (path: string, init: RequestInit) =>
  fetch(`${base}${path}`, {
    ...init,
    headers: { ...(init.headers ?? {}), authorization: `Bearer ${await admin()()}` },
  });

/**
 * The client the app is, naming its credential and mapping USER to that same
 * person: only a person the client maps asks anything (deny by default).
 * Made once, on the first test that needs it.
 */
let mapped: Promise<void> | null = null;
function appClient(subject: string): Promise<void> {
  mapped ??= asAdmin("/api/clients", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      id: "app",
      name: "Slack app",
      surface: "slack",
      enabled: true,
      agents: ["*"],
      identity: { app_id: "A01APP", team: TEAM },
      slack: { machine: subject, people: { [USER]: PERSON } },
      credentials: { token: { value: "xoxb-server-test" }, signing_secret: { value: "signing" } },
    }),
  }).then(async (r) => assert.equal(r.status, 201, await r.text()));
  return mapped;
}

/**
 * USER asks about OTHER_USER's message, as Slack sends the message action.
 * The app asks it for nobody, at USER's asking, keyed by the thread.
 */
const askAbout = (text: string, ts: string, threadTs?: string) =>
  interactive({
    type: "message_action",
    callback_id: "aui_ask_about",
    // Slack sends a redelivery with the trigger it first sent.
    trigger_id: `trigger-${ts}`,
    channel: { id: CHANNEL },
    message: { ts, text, user: OTHER_USER, ...(threadTs ? { thread_ts: threadTs } : {}) },
  });

/** Every event of a run until it ends, folded into its turn. */
async function finish(c: Humagen, run: { id: string; agent: string }, patience = 10_000) {
  const stop = new AbortController();
  const timer = setTimeout(() => stop.abort(), patience);
  const f = follow(run);
  const kinds: string[] = [];
  for await (const item of c.runEvents(run, { signal: stop.signal })) {
    if (item.type !== "event" || item.event.run !== run.id) continue;
    kinds.push(item.event.type);
    f.apply(item.event);
    if (["done", "failed", "stopped"].includes(f.turn.status)) break;
  }
  clearTimeout(timer);
  stop.abort();
  return { turn: f.turn, kinds };
}

test("the stand-in client against the real routes", { skip: !enabled }, async () => {
  const c = client();
  const me = (await c.getMe()).principal;
  assert.equal(me.subject, "admin-ci");
  assert.equal(me.tenant, "default");
  const { permissions } = await c.getMe();
  for (const p of ["runs:read", "runs:write", "agents:read"]) assert.ok(permissions.includes(p));

  const { agents } = await c.listAgents();
  assert.ok(agents.some((a) => a.spec.name === "lead"));

  // The run id is the caller and the key, and the first run names its conversation.
  const key = threadKey({ team: TEAM, channel: CHANNEL, threadTs: "1700000000.000100" });
  const run = await c.createRun({ agent: "lead", text: "hello" }, key);
  assert.equal(run.id, runId(me.tenant, me.subject, key));
  assert.equal(run.thread, run.id);
  const again = await c.createRun({ agent: "lead", text: "hello" }, key);
  assert.equal(again.id, run.id, "the same key joins the run it started");

  const { turn, kinds } = await finish(c, run);
  assert.equal(kinds[0], "run.accepted.v1");
  assert.equal(kinds.at(-1), "run.completed.v1");
  assert.equal(turn.status, "done");
  assert.equal(turn.text, REPLY);

  const next = await c.createRun({ agent: "", text: "and then?", after: run.id }, `${key}-2`);
  assert.equal(next.thread, run.id);
  assert.equal(next.prev, run.id);
  // The spec makes the lead the Workspace Default: it answers what tags no agent.
  assert.equal(next.agent, "lead", "an empty agent is the default the workspace sets");
  assert.ok(!next.picked, "a default that names an agent picks nothing");
  await finish(c, next);
  const turns = (await c.getThread(run.id))?.turns;
  assert.deepEqual(turns?.map((t) => t.id).sort(), [run.id, next.id].sort());

  // * asks every agent whether to take it. The router is off by default, so
  // none does; the message still lands on one, and never waits for a person.
  const landed = await c.createRun({ agent: "*", text: "do one task well" }, `${key}-everyone`);
  assert.ok(landed.agent, "a message no agent takes still lands on one");

  assert.equal(await c.getRun("api-dev-no-such-run"), null);
  assert.equal(await c.getThread("api-dev-no-such-run"), null);
  assert.equal(await c.getClient("no-such-client"), null);
  // Stopping a run that has ended changes nothing.
  assert.equal((await c.stopRun(run.id)).status, "completed");

  const page = await c.inbox.list({ limit: 5, state: "open" });
  assert.ok(Array.isArray(page.messages));

  // A catch-up reads the workspace's announcements, withdrawn ones too.
  assert.ok(Array.isArray(await c.announcements("", 5)));

  // The inbox-only stream opens, says so, and closes cleanly.
  const quiet = new AbortController();
  setTimeout(() => quiet.abort(), 300);
  let opened = 0;
  for await (const _ of c.notices({ signal: quiet.signal, opened: () => opened++ })) {
    // Nothing is raised here.
  }
  assert.equal(opened, 1);
});

test(
  "the app against the real server: a mention, a redelivery, a reply and a stop",
  { skip: !enabled },
  async () => {
    // What humagen answers each message the app posts, so the test finds the runs it started.
    const posted: ThreadPosted[] = [];
    const inner = appHumagen();
    const c: Humagen = {
      ...inner,
      postSession: async (body, options) => {
        const p = await inner.postSession(body, options);
        posted.push(p);
        return p;
      },
      postThread: async (body, options) => {
        const p = await inner.postThread(body, options);
        posted.push(p);
        return p;
      },
    };
    const me = (await c.getMe()).principal;
    await appClient(me.subject);
    const slack = new FakeSlack();
    const make = () =>
      new App(slack, c, {
        team: TEAM,
        botUser: "U00BOTBOT",
        subject: me.subject,
        tenant: me.tenant,
        defaultAgent: "lead",
        clientId: "app",
        flushMs: 0,
        pollMs: 200,
      });
    const app = make();
    const ts = "1700000999.000100";
    const waitFor = async (check: () => boolean) => {
      for (let i = 0; i < 200 && !check(); i++) await new Promise((r) => setTimeout(r, 25));
      assert.ok(check());
    };

    // A person the client maps: their message is theirs in the session, and so is its run.
    await app.handle(mention("what can you do?", ts));
    await waitFor(() => slack.methods.includes("chat.stopStream"));
    await app.idle();
    const first = posted[0]?.runs[0];
    assert.ok(first, "the mention started a run");
    const done = await c.getRun(first.id);
    assert.equal(done?.status, "completed");
    assert.equal(done?.actor?.user, PERSON, "the run acts for the person the client maps");
    assert.ok(slack.streamed.includes(REPLY), slack.streamed);
    assert.deepEqual(slack.statuses, ["processing", "active"]);
    assert.equal(slack.of("reactions.add").at(-1)?.name, "white_check_mark");

    // A new process, the same Slack event: humagen has it, so nothing starts.
    const fresh = make();
    const calls = slack.calls.length;
    await fresh.handle(mention("what can you do?", ts));
    await settle();
    await fresh.idle();
    assert.equal(posted.at(-1)?.repeated, true);
    assert.equal(slack.calls.length, calls);

    await fresh.handle(mention("@helper and you?", "1700001000.000100", { thread_ts: ts }));
    await waitFor(() => slack.of("chat.stopStream").length === 2);
    await fresh.idle();
    const reply = posted.at(-1)?.runs[0];
    assert.equal(reply?.agent, "helper");
    assert.equal(reply?.thread, first.thread, "one Slack thread is one session");

    // Someone the client does not map starts nothing, and is told privately.
    const before = posted.length;
    await fresh.handle(
      mention("and me?", "1700001100.000100", { thread_ts: ts, user: OTHER_USER }),
    );
    await settle();
    await fresh.idle();
    assert.equal(posted.length, before);
    assert.equal(slack.of("chat.postEphemeral").at(-1)?.user, OTHER_USER);

    // Nothing is running, so stop only settles the session.
    await fresh.handle(stopPressed(ts));
    await fresh.idle();
    assert.equal(slack.calls.at(-1)?.method, "agents.sessions.setStatus");
    assert.equal(slack.calls.at(-1)?.args.status, "active");
    assert.equal(USER, slack.of("chat.startStream")[0]?.recipient_user_id);
  },
);

test(
  "a failed run is ended by the ledger, reaches the inbox, and is settled through the client",
  { skip: !enabled, timeout: 120_000 },
  async () => {
    const c = client();
    // An agent on a model server nothing listens on: every attempt's call is
    // refused, which a retry may get past, so each attempt fails and is retried
    // until the ledger ends the run, without leaving the machine.
    const made = await asAdmin("/api/agents", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: "nomodel",
        description: "fails on every attempt",
        model: "no-such-model",
        provider: "unreachable",
        system_prompt: "x",
      }),
    });
    assert.equal(made.status, 201);
    const run = await c.createRun(
      {
        agent: "nomodel",
        text: "hello",
      },
      "server-test-fail",
    );
    // No event says the last attempt was the last: the ledger ends the run, and its record says so.
    let record = await c.getRun(run.id);
    for (let i = 0; i < 300 && record?.status !== "failed"; i++) {
      await new Promise((r) => setTimeout(r, 100));
      record = await c.getRun(run.id);
    }
    assert.equal(record?.status, "failed");
    const { turn, kinds } = await finish(c, run, 1_000);
    assert.ok(kinds.includes("run.failed.v1"));
    assert.equal(turn.status, "working", "the events alone never end the turn");
    assert.equal(
      follow(run).settle(record as NonNullable<typeof record>).status,
      "failed",
      "the record does",
    );

    // The workspace's default rule raises it as an alert, with the failed run as a part.
    let message = null;
    for (let i = 0; i < 100 && !message; i++) {
      const page = await c.inbox.list({ limit: 30, state: "open" });
      message =
        page.messages.find((m) => m.about.some((s) => s.kind === "run" && s.id === run.id)) ?? null;
      if (!message) await new Promise((r) => setTimeout(r, 50));
    }
    assert.ok(message, "the failure is in the inbox");
    const part = message.parts.find((p) => p.type === "failure");
    assert.ok(part?.allowed?.length, "this caller may act on it");
    const settled = await c.inbox.send(message, {
      call: "resolve",
      partId: part.id,
      action: "acknowledge",
    });
    assert.equal(settled.parts.find((p) => p.id === part.id && "resolution" in p)?.type, "failure");
    const again = await c.inbox.get(message.id);
    assert.equal(
      (again.parts.find((p) => p.id === part.id) as { resolution?: { outcome: string } }).resolution
        ?.outcome,
      "acknowledge",
    );
  },
);

test(
  "a stop pressed while a run is in flight is acknowledged, then confirmed when the run ends stopped",
  { skip: !enabled, timeout: 120_000 },
  async () => {
    const c = appHumagen();
    const me = (await c.getMe()).principal;
    // A model server nothing listens on keeps the run retrying its turn, so the run is in flight.
    const made = await asAdmin("/api/agents", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        name: "stalling",
        description: "never answers",
        model: "no-such-model",
        provider: "unreachable",
        system_prompt: "x",
      }),
    });
    assert.equal(made.status, 201);
    await appClient(me.subject);
    const slack = new FakeSlack();
    const app = new App(slack, c, {
      team: TEAM,
      botUser: "U00BOTBOT",
      subject: me.subject,
      tenant: me.tenant,
      defaultAgent: "stalling",
      clientId: "app",
      flushMs: 0,
      pollMs: 200,
    });
    const ts = "1700002000.000100";
    const id = runId(
      me.tenant,
      me.subject,
      threadKey({ team: TEAM, channel: CHANNEL, threadTs: ts }),
    );
    const handled = app.handle(askAbout("are you there?", ts));
    let record = await c.getRun(id);
    for (let i = 0; i < 200 && record?.status !== "running"; i++) {
      await new Promise((r) => setTimeout(r, 25));
      record = await c.getRun(id);
    }
    assert.equal(record?.status, "running", "the run is in flight when stop is pressed");

    // The route answers 202 and the run is stopping; the app follows it to the end.
    await app.handle(stopPressed(ts));
    await handled;
    await app.idle();
    for (let i = 0; i < 400 && record?.status !== "stopped"; i++) {
      await new Promise((r) => setTimeout(r, 50));
      record = await c.getRun(id);
    }
    assert.equal(record?.status, "stopped");
    assert.equal(record?.stopping, undefined);
    const ended = JSON.stringify(slack.of("chat.stopStream").at(-1) ?? {});
    assert.ok(
      !ended.includes("may still finish elsewhere"),
      "the stop was confirmed, not reported as unconfirmed",
    );
    assert.ok(ended.includes(defaultCopy.stopPhase.confirmed), "the stop is shown as confirmed");
    assert.equal(slack.statuses.at(-1), "active");
  },
);
