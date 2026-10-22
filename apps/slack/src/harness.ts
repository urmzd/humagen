/**
 * Test doubles for both sides of the app: a Slack that records every call and
 * a warp whose runs the test drives one event at a time. Every id here is
 * made up.
 */
import {
  type AnswerRequest,
  type BusEvent,
  type Http,
  mailTransport,
  type Run,
} from "@warp/adapter";
import { App, type AppOptions } from "./app.tsx";
import { runId, tagId } from "./keys.ts";
import {
  type ActAs,
  type Agent,
  type Binding,
  type ClientRecord,
  type InboxNotice,
  type Receipt,
  type RunRequest,
  type StreamItem,
  type ThreadPosted,
  type ThreadPostRequest,
  type Warp,
  WarpError,
  type WireMessage,
  type WireProject,
} from "./warp.ts";

/** A run asked for: the body and the key, as `createRun` takes them. */
export interface Asked extends RunRequest {
  idempotencyKey: string;
}

import { type Envelope, type SlackApi, SlackError } from "./slack.ts";

export const TEAM = "T01ABCDEF";
export const OTHER_TEAM = "T09ZZZZZZ";
export const CHANNEL = "C01ABCDEF";
export const DM = "D01ABCDEF";
export const USER = "U01ABCDEF";
export const OTHER_USER = "U02ABCDEF";
export const BOT = "U00BOTBOT";
export const SUBJECT = "svc-slack";

export interface Call {
  method: string;
  args: Record<string, unknown>;
}

/** A Slack that answers every call and remembers it. */
export class FakeSlack implements SlackApi {
  readonly calls: Call[] = [];
  readonly responses: { url: string; body: Record<string, unknown> }[] = [];
  /** Methods that fail, by error code. A function decides per call. */
  failing: Record<string, string | ((args: Record<string, unknown>) => string | undefined)> = {};
  /** What `conversations.replies` reads of each thread, by `channel/ts`. */
  readonly replies = new Map<string, { ts: string; user?: string; text?: string }[]>();
  /** Who `users.info` says each Slack person is, by display name. */
  readonly users = new Map<string, string>();
  private clock = 1_800_000_000;

  async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
    this.calls.push({ method, args });
    const fail = this.failing[method];
    const code = typeof fail === "function" ? fail(args) : fail;
    if (code) throw new SlackError(method, code);
    if (method === "conversations.replies") {
      return { ok: true, messages: this.replies.get(`${args.channel}/${args.ts}`) ?? [] } as T;
    }
    if (method === "users.info") {
      const name = this.users.get(String(args.user));
      return { ok: true, ...(name ? { user: { profile: { display_name: name } } } : {}) } as T;
    }
    if (method === "chat.startStream" || method === "chat.postMessage") {
      this.clock += 1;
      const channel = String(args.channel).startsWith("U") ? DM : args.channel;
      return { ok: true, ts: `${this.clock}.000100`, channel } as T;
    }
    return { ok: true } as T;
  }

  async respond(url: string, body: Record<string, unknown>) {
    this.responses.push({ url, body });
  }

  /** The names of the calls made, in order. */
  get methods(): string[] {
    return this.calls.map((c) => c.method);
  }

  of(method: string): Record<string, unknown>[] {
    return this.calls.filter((c) => c.method === method).map((c) => c.args);
  }

  /** Session statuses in the order they were set. */
  get statuses(): string[] {
    return this.of("agents.sessions.setStatus").map((a) => String(a.status));
  }

  /** Everything streamed as answer text, joined. */
  get streamed(): string {
    return this.of("chat.appendStream")
      .flatMap((a) => a.chunks as { type: string; text?: string }[])
      .filter((c) => c.type === "markdown_text")
      .map((c) => c.text ?? "")
      .join("");
  }
}

/** A queue a test can push into and an async reader can drain. */
class Feed<T> {
  private readonly items: T[] = [];
  private waiters: (() => void)[] = [];
  push(item: T) {
    this.items.push(item);
    for (const w of this.waiters.splice(0)) w();
  }
  async *read(signal: AbortSignal): AsyncGenerator<T> {
    for (;;) {
      while (this.items.length > 0) yield this.items.shift() as T;
      if (signal.aborted) return;
      await new Promise<void>((resolve) => {
        this.waiters.push(resolve);
        signal.addEventListener("abort", () => resolve(), { once: true });
      });
      if (signal.aborted && this.items.length === 0) return;
    }
  }
}

const terminalStatus = (s: Run["status"]) => s === "completed" || s === "failed" || s === "stopped";

const b64 = (v: unknown) => Buffer.from(JSON.stringify(v)).toString("base64");

/** A warp that keeps runs, conversations and an inbox in memory, shaped like the real API. */
export class FakeWarp implements Warp {
  readonly runs = new Map<string, Run>();
  /** Every turn asked for, including ones that joined an existing run. */
  readonly asked: Asked[] = [];
  /** Turns that started a run. */
  readonly started: Asked[] = [];
  readonly stops: string[] = [];
  readonly resolved: { id: string; path: string; body: unknown }[] = [];
  agentList: Agent[] = [
    { name: "lead", description: "Takes a request and may call the helper." },
    { name: "helper", description: "Does one piece of work for the lead." },
  ];
  clients = new Map<string, ClientRecord>();
  /** Inbox messages as the platform sends them. */
  readonly mailbox = new Map<string, Record<string, unknown>>();
  /** Set to refuse the next turn, as a limit would. */
  refuse: WarpError | null = null;
  /** Set to make stop fail. */
  stopFails = false;
  /** Every answer asked for, and for whom. */
  readonly answered: { answer: AnswerRequest; as: ActAs }[] = [];
  /** Every message posted from a Slack thread, in order. */
  readonly posts: ThreadPostRequest[] = [];
  /** The warp thread each Slack thread is, by client, channel and ts. */
  private readonly surfaces = new Map<string, string>();
  /** Each message posted, by its ts: what it did. */
  private readonly postedOnce = new Map<string, ThreadPosted>();
  /** Set to make an untagged message start nothing, as a project with no default does. */
  noDefault = false;
  /** The Slack channels each client is bound to. */
  readonly bound = new Map<string, Binding[]>();
  /** The announcement each warp thread was started from, by thread. */
  readonly seeded = new Map<string, string>();
  /** Where the app recorded it posted each inbox message. */
  readonly deliveries: { message: string; client: string; channel: string; ts: string }[] = [];
  /** Each mapped person's own inbox, by subject, as act_as reads it. */
  readonly personal = new Map<string, Record<string, unknown>[]>();
  /** Every inbox read made for someone, and for whom. */
  readonly readFor: ActAs[] = [];
  /** Each mapped person's projects, by subject, as act_as lists them. */
  readonly projects = new Map<string, WireProject[]>();
  /** Every projects read made for someone, and for whom. */
  readonly projectsReadFor: ActAs[] = [];
  /** Set to refuse the next answer, as warp does someone who may not answer. */
  refuseAnswer: WarpError | null = null;
  /**
   * Set when the app's own credential is a person, as under dev sign-in: what
   * it asks as itself then acts for that person, as warp's ActorOf records it.
   */
  person = false;
  private readonly feeds = new Map<string, Feed<StreamItem>>();
  private readonly notes = new Feed<InboxNotice>();
  private seq = new Map<string, number>();
  /**
   * warp's clock, which the app's is too (world()). It starts after every
   * fixture's own times, so what the fake records, such as a receipt, comes
   * after the messages it is about, as it does in warp.
   */
  private clock = Date.parse("2026-10-09T01:00:00Z");

  private at() {
    this.clock += 1000;
    return new Date(this.clock).toISOString();
  }

  /** What time it is for warp, as the app reads it. */
  now() {
    return this.clock;
  }

  /** A time some way back on warp's clock, for a message raised then. */
  ago(ms: number) {
    return new Date(this.clock - ms).toISOString();
  }

  private feed(id: string) {
    let f = this.feeds.get(id);
    if (!f) {
      f = new Feed();
      this.feeds.set(id, f);
    }
    return f;
  }

  async getMe() {
    return {
      principal: { subject: SUBJECT, tenant: "acme", kind: "machine" as const },
      permissions: ["runs:read", "runs:write", "agents:read"],
    };
  }
  async listAgents() {
    return { agents: this.agentList.map((spec) => ({ spec, rev: 1 })), builtin: [] };
  }
  async getClient(id: string) {
    return this.clients.get(id) ?? null;
  }

  async createRun(body: RunRequest, key = "random"): Promise<Run> {
    const t: Asked = { ...body, idempotencyKey: key };
    this.asked.push(t);
    if (this.refuse) {
      const e = this.refuse;
      this.refuse = null;
      throw e;
    }
    // As the real route does: a person the client maps is the run's actor, so
    // it acts for them; anyone else is asked for by the app, for nobody.
    let actor: Run["actor"] = this.person
      ? { id: SUBJECT, display: "Developer", user: SUBJECT }
      : { id: SUBJECT, bot: true };
    if (t.act_as) {
      const person = this.clients.get(t.act_as.client)?.slack?.people?.[t.act_as.user];
      if (!person) throw new WarpError(403, "this client does not map that person");
      actor = { id: person, display: person, user: person };
    }
    const id = runId("acme", SUBJECT, t.idempotencyKey);
    const existing = this.runs.get(id);
    if (existing) return existing;
    const prev = t.after ? this.runs.get(t.after) : undefined;
    if (t.after && !prev) throw new WarpError(404, "not found");
    const now = this.at();
    const run: Run = {
      id,
      tag_id: tagId(SUBJECT, t.idempotencyKey),
      tenant: "acme",
      // An empty agent leaves it to the default the platform's projects set, here lead.
      agent: t.agent || "lead",
      rev: 1,
      origin: "api",
      text: t.text,
      actor,
      root: id,
      depth: 0,
      thread: prev?.thread ?? id,
      ...(t.after ? { prev: t.after } : {}),
      lease: 0,
      granted: 0,
      calls: 0,
      status: "accepted",
      created_at: now,
      updated_at: now,
    };
    this.runs.set(id, run);
    this.started.push(t);
    return run;
  }

  async getRun(id: string) {
    return this.runs.get(id) ?? null;
  }

  /**
   * As the platform does for the client's machine: the Slack thread is one
   * warp thread, each message is posted once by its ts, and a message tags
   * an agent by its name, a person by @handle, or else the default answers.
   */
  async postThread(body: ThreadPostRequest): Promise<ThreadPosted> {
    const client = this.clients.get(body.surface.client);
    if (!client || client.slack?.machine !== SUBJECT) throw new WarpError(404, "not found");
    if (!client.slack?.people?.[body.act_as.user]) {
      throw new WarpError(403, "that person is not mapped to anyone in this workspace");
    }
    this.posts.push(body);
    const s = body.surface;
    const once = `${s.client}/${s.channel}/${s.ts}/${s.message ?? s.ts}`;
    const before = this.postedOnce.get(once);
    if (before) return { ...before, repeated: true };
    const where = `${s.client}/${s.channel}/${s.ts}`;
    const bound = (this.bound.get(s.client) ?? []).find((b) => b.slack_channel === s.channel);
    let thread = this.surfaces.get(where);
    if (!thread) {
      // As the platform does: a Slack thread under a post this app recorded
      // is that announcement's while the Slack channel is bound to its
      // project, and there one withdrawn starts nothing.
      const receipt = this.deliveries.find(
        (d) => d.client === s.client && d.channel === s.channel && d.ts === s.ts,
      );
      const found = receipt && this.mailbox.get(receipt.message);
      const source = found?.source as { project?: string } | undefined;
      const seed = bound && source?.project === bound.project ? found : undefined;
      if (seed && seed.state !== "open") {
        throw new WarpError(410, "a reply to this announcement starts nothing: it was withdrawn");
      }
      thread = `th-${String(this.surfaces.size + 1).padStart(16, "0")}`;
      this.surfaces.set(where, thread);
      if (receipt && seed) this.seeded.set(thread, receipt.message);
    }
    const n = [...this.postedOnce.keys()].filter((k) => k.startsWith(`${where}/`)).length + 1;
    const message = { id: `m-${n}`, text: body.text.replaceAll(`<@${s.app_user}>`, "").trim() };
    const posted: ThreadPosted = {
      thread: {
        id: thread,
        project: bound?.project ?? "",
        channel: bound?.channel ?? "general",
        visibility: s.channel.startsWith("D") ? "private" : "shared",
      },
      message,
      runs: [],
      notified: [],
      refused: [],
      rule: "default",
    };
    const tagged = this.agentList.find((a) =>
      new RegExp(`(^|\\s)@${a.name}\\b`).test(message.text),
    );
    const person = /(^|\s)@([a-z][a-z0-9-]*)/.exec(message.text)?.[2];
    if (body.post_only) posted.rule = "post_only";
    else if (tagged) posted.rule = "tagged";
    else if (person) {
      posted.rule = "people";
      posted.notified = [person];
    } else if (this.noDefault) {
      posted.rule = "none";
      posted.reason = "Nobody answers untagged messages here. Tag an agent with @.";
    }
    if (posted.rule === "tagged" || posted.rule === "default") {
      const agent = tagged?.name ?? client.default_agent ?? "lead";
      const run = await this.createRun(
        { agent, text: message.text, act_as: body.act_as },
        `thread-${thread}-${message.id}-${agent}`,
      );
      this.set(run.id, { thread, message: message.id });
      posted.runs = [this.runs.get(run.id) as Run];
    }
    this.postedOnce.set(once, posted);
    return posted;
  }

  async getInboxMessage(id: string) {
    return (this.mailbox.get(id) as WireMessage | undefined) ?? null;
  }

  /** The surface of each warp thread the fake started from one. */
  async threadSurface(id: string) {
    for (const [where, thread] of this.surfaces) {
      if (thread !== id) continue;
      const [client = "", channel = "", ts = ""] = where.split("/");
      return { client, channel, ts };
    }
    return null;
  }

  async inboxMessageFor(id: string, actAs: ActAs) {
    this.readFor.push(actAs);
    const subject = this.clients.get(actAs.client)?.slack?.people?.[actAs.user];
    const m = subject ? (this.personal.get(subject) ?? []).find((x) => x.id === id) : undefined;
    return (m as WireMessage | undefined) ?? null;
  }

  /** Every action taken for someone on their own messages. */
  readonly resolvedFor: { id: string; part: string; body: Record<string, unknown>; as: ActAs }[] =
    [];

  async resolveFor(id: string, part: string, body: Record<string, unknown>, actAs: ActAs) {
    this.resolvedFor.push({ id, part, body, as: actAs });
    const subject = this.clients.get(actAs.client)?.slack?.people?.[actAs.user] ?? "";
    const m = (this.personal.get(subject) ?? []).find((x) => x.id === id);
    if (!m) throw new WarpError(404, "not found");
    m.state = "resolved";
    m.parts = (m.parts as Record<string, unknown>[]).map((p) =>
      p.id === part
        ? {
            ...p,
            state: "resolved",
            actions: [],
            outcome: { action: body.action, by: subject, at: this.at() },
          }
        : p,
    );
    return m as WireMessage;
  }

  async bindings(client: string) {
    if (!this.clients.has(client)) throw new WarpError(404, "not found");
    return this.bound.get(client) ?? [];
  }

  /** Set to make the next receipt fail, as when warp goes away after a post. */
  refuseDelivery: WarpError | null = null;

  /**
   * As the real route does: the same post recorded again moves the receipt's
   * time on only when the message changed since, and otherwise changes nothing.
   */
  async recordDelivery(message: string, client: string, at: { channel: string; ts: string }) {
    if (this.refuseDelivery) {
      const e = this.refuseDelivery;
      this.refuseDelivery = null;
      throw e;
    }
    const m = this.mailbox.get(message);
    const have = (m?.deliveries as Record<string, Receipt> | undefined)?.[client];
    if (
      have?.channel === at.channel &&
      have.ts === at.ts &&
      !(Date.parse(String(m?.updated_at)) > Date.parse(have.at ?? ""))
    )
      return;
    this.deliveries.push({ message, client, ...at });
    if (m)
      m.deliveries = { ...((m.deliveries as object) ?? {}), [client]: { ...at, at: this.at() } };
  }

  /** Every read of a project's announcements, by project. */
  readonly newsReads: string[] = [];

  /** As the project's inbox pages them for the app's machine: newest first, withdrawn ones too. */
  async announcements(project: string, limit: number) {
    this.newsReads.push(project);
    return [...this.mailbox.values()]
      .filter(
        (m) => m.kind === "announcement" && (m.source as { project?: string })?.project === project,
      )
      .sort((a, b) => Date.parse(String(b.at)) - Date.parse(String(a.at)))
      .slice(0, limit) as WireMessage[];
  }

  /**
   * Change a message for everyone, as warp does, without telling anyone: a
   * notice the stream would have carried while the app was not listening.
   */
  quietly(id: string, change: Record<string, unknown>) {
    const m = this.mailbox.get(id);
    if (m) Object.assign(m, change, { updated_at: this.at() });
  }

  async inboxFor(actAs: ActAs, query: { state?: string; kind?: string; limit?: number }) {
    this.readFor.push(actAs);
    const subject = this.clients.get(actAs.client)?.slack?.people?.[actAs.user];
    if (!subject) throw new WarpError(403, "that person is not mapped to anyone in this workspace");
    const all = this.personal.get(subject) ?? [];
    const mine = all.filter(
      (m) =>
        (!query.kind || m.kind === query.kind) &&
        (query.state === "all" || m.state === (query.state ?? "open")),
    );
    // The counts are the whole inbox's, as warp's are: announcements count only as updates.
    const open = all.filter((m) => m.state === "open");
    const news = open.filter((m) => m.kind === "announcement");
    return {
      messages: mine.slice(0, query.limit ?? 50),
      open: open.length - news.length,
      unread: open.filter((m) => m.kind !== "announcement" && m.unread).length,
      updates: news.filter((m) => m.unread).length,
    };
  }

  async projectsFor(actAs: ActAs) {
    this.projectsReadFor.push(actAs);
    const subject = this.clients.get(actAs.client)?.slack?.people?.[actAs.user];
    if (!subject) throw new WarpError(403, "that person is not mapped to anyone in this workspace");
    const mine = this.projects.get(subject);
    if (!mine) throw new WarpError(403, "that person cannot read projects");
    return mine;
  }

  async getThread(id: string) {
    const turns = [...this.runs.values()].filter((r) => r.thread === id);
    return turns.length > 0 ? { turns } : null;
  }

  runEvents(run: { id: string }, options: { signal?: AbortSignal }) {
    return this.feed(run.id).read(options.signal ?? new AbortController().signal);
  }

  /** What the app does each time its notices stream opens, for reopen(). */
  private opened: (() => void) | undefined;

  notices(options: { signal?: AbortSignal; opened?: () => void }) {
    this.opened = options.opened;
    const read = this.notes.read(options.signal ?? new AbortController().signal);
    options.opened?.();
    return read;
  }

  /** The notices stream reconnects, having carried nothing while it was closed. */
  reopen() {
    this.opened?.();
  }

  /**
   * As the real route does: a run that has ended is returned as it is (200),
   * and one still going is marked stopping and returned at once (202). The
   * runtime then ends it a moment later with lifecycle.stopped, carrying what
   * it had said, unless `stopsFinishFirst` makes it complete instead.
   */
  async stopRun(id: string): Promise<Run> {
    this.stops.push(id);
    if (this.stopFails) throw new WarpError(500, "something went wrong on our side");
    const run = this.runs.get(id);
    if (!run) throw new WarpError(404, "not found");
    if (terminalStatus(run.status)) return run;
    if (!run.stopping) {
      this.set(id, { stopping: true, stop_requested_at: this.at() });
      setTimeout(() => this.confirmStop(id), this.stopDelayMs);
    }
    return this.runs.get(id) as Run;
  }

  /**
   * As the real route does for someone it admits: the answer is recorded as
   * the person the client maps, and the run resumes with it.
   */
  async answerQuestion(answer: AnswerRequest, as: ActAs): Promise<unknown> {
    this.answered.push({ answer, as });
    if (this.refuseAnswer) {
      const e = this.refuseAnswer;
      this.refuseAnswer = null;
      throw e;
    }
    const by = this.clients.get(as.client)?.slack?.people?.[as.user] ?? as.user;
    const { action, choice, text } = answer.body;
    this.emit(answer.run, "lifecycle.resumed", {
      attempt: 2,
      answer: {
        question_id: answer.question,
        ...(action === "approve" || action === "deny" ? { decision: action } : {}),
        ...(choice ? { choice } : {}),
        ...(text ? { text } : {}),
        by,
        at: this.at(),
      },
    });
    return {};
  }

  /** How long the runtime takes to confirm a stop. */
  stopDelayMs = 30;
  /** Set to make a run asked to stop complete instead, as one whose turn finished first does. */
  stopsFinishFirst = false;

  private confirmStop(id: string) {
    const run = this.runs.get(id);
    if (!run || terminalStatus(run.status)) return;
    const said = this.said.get(id) ?? "";
    if (this.stopsFinishFirst) {
      this.emit(id, "lifecycle.completed", { text: said });
      this.set(id, { stopping: undefined, stop_requested_at: undefined });
      return;
    }
    const cause = `Stopped by ${SUBJECT}`;
    this.emit(id, "lifecycle.stopped", { text: said, reason: cause, by: SUBJECT });
    this.set(id, {
      status: "stopped",
      answer: said,
      cause,
      stopping: undefined,
      stop_requested_at: undefined,
    });
  }

  /** What each run has said so far, from its text deltas. */
  private readonly said = new Map<string, string>();

  /** The inbox routes, over the real mapping, so the wire shapes are the ones exercised. */
  private http: Http = async <T>(method: string, path: string, json?: unknown): Promise<T> => {
    const url = new URL(path, "http://warp.test");
    const [, , , id, ...rest] = url.pathname.split("/");
    if (method === "GET" && !id) {
      const messages = [...this.mailbox.values()].filter((m) => m.state === "open");
      return { messages, next: null, open: messages.length, unread: 0 } as T;
    }
    const message = id ? this.mailbox.get(decodeURIComponent(id)) : undefined;
    if (!message) throw new WarpError(404, "not found");
    if (method === "GET") return { message, history: [] } as T;
    this.resolved.push({ id: String(message.id), path: rest.join("/"), body: json });
    if (rest[0] === "parts") {
      const body = json as { action: string; ids?: string[] };
      const outcome = {
        action: body.action,
        by: SUBJECT,
        at: this.at(),
        ...(body.ids ? { note: `Kept ${body.ids.length} of 2` } : {}),
      };
      message.parts = (message.parts as Record<string, unknown>[]).map((p) =>
        p.id === rest[1] ? { ...p, state: "resolved", actions: [], outcome } : p,
      );
      message.state = "resolved";
    }
    if (rest[0] === "dismiss") message.state = "dismissed";
    return message as T;
  };
  inbox = mailTransport(this.http);

  // What a test does to the platform.

  /** Change a run's record, as the ledger would. */
  set(id: string, change: Partial<Run>) {
    const run = this.runs.get(id);
    if (run) this.runs.set(id, { ...run, ...change, updated_at: this.at() });
  }

  /** Publish one event on a run's stream. */
  emit(id: string, kind: string, payload?: unknown) {
    const run = this.runs.get(id);
    if (!run) throw new Error(`no run ${id}`);
    const seq = (this.seq.get(id) ?? 0) + 1;
    this.seq.set(id, seq);
    const event: BusEvent = {
      seq,
      topic: `agent:${run.agent}:${id}`,
      run_id: id,
      tenant: "acme",
      agent: run.agent,
      rev: 1,
      origin: "api",
      kind,
      payload: payload === undefined ? undefined : b64(payload),
      at: this.at(),
    };
    if (kind === "lifecycle.started" || kind === "lifecycle.resumed")
      this.set(id, { status: "running" });
    if (kind === "lifecycle.parked") this.set(id, { status: "parked" });
    if (kind === "delta.text") {
      this.said.set(id, (this.said.get(id) ?? "") + ((payload as { text?: string })?.text ?? ""));
    }
    if (kind === "lifecycle.completed") {
      this.set(id, { status: "completed", answer: (payload as { text?: string })?.text });
    }
    if (kind === "lifecycle.failed" && (payload as { terminal?: boolean })?.terminal) {
      this.set(id, { status: "failed", error: (payload as { error?: string }).error });
    }
    this.feed(id).push({ type: "event", event });
  }

  /** Put a message in the inbox and announce it. */
  raise(message: Record<string, unknown>) {
    const was = this.mailbox.get(String(message.id));
    // A change is a change for everyone, as warp's updated_at says.
    if (was && was !== message && was.updated_at === message.updated_at)
      message = { ...message, updated_at: this.at() };
    this.mailbox.set(String(message.id), message);
    this.notes.push({
      id: String(message.id),
      kind: String(message.kind),
      state: String(message.state),
      updated_at: this.at(),
    });
  }

  /** Put a message in a person's own inbox, and tell the app's machine for whom. */
  tellFor(subject: string, actAs: ActAs, message: Record<string, unknown>) {
    const mine = this.personal.get(subject) ?? [];
    this.personal.set(subject, [...mine.filter((m) => m.id !== message.id), message]);
    this.notes.push({
      id: String(message.id),
      kind: String(message.kind),
      state: String(message.state),
      updated_at: this.at(),
      for: actAs,
    });
  }

  /** The run most recently started. */
  get last(): Run {
    const run = [...this.runs.values()].at(-1);
    if (!run) throw new Error("no run was started");
    return run;
  }
}

/** Let everything already scheduled run: promise chains, zero timers, and the turn pump. */
export async function settle(rounds = 8): Promise<void> {
  for (let i = 0; i < rounds; i++) await new Promise((r) => setTimeout(r, 0));
}

export interface World {
  slack: FakeSlack;
  warp: FakeWarp;
  app: App;
  logs: string[];
  /** A second app over the same Slack and warp, as after a restart. */
  restart(): App;
}

export function world(opts: Partial<AppOptions> = {}, from?: Pick<World, "slack" | "warp">): World {
  const slack = from?.slack ?? new FakeSlack();
  const warp = from?.warp ?? new FakeWarp();
  const logs: string[] = [];
  const make = () =>
    new App(slack, warp, {
      team: TEAM,
      botUser: BOT,
      subject: SUBJECT,
      tenant: "acme",
      defaultAgent: "lead",
      flushMs: 0,
      pollMs: 20,
      now: () => warp.now(),
      log: (line) => logs.push(line),
      ...opts,
    });
  return { slack, warp, app: make(), logs, restart: make };
}

// Slack deliveries, as Socket Mode hands them over.

let envelopes = 0;
const envelope = (type: string, payload: Record<string, unknown>): Envelope => {
  envelopes += 1;
  return { type, envelope_id: `env-${envelopes}`, payload };
};

export const mention = (text: string, ts: string, over: Record<string, unknown> = {}): Envelope =>
  envelope("events_api", {
    team_id: TEAM,
    event_id: `Ev${ts.replace(".", "")}`,
    event: {
      type: "app_mention",
      channel: CHANNEL,
      user: USER,
      text: `<@${BOT}> ${text}`,
      ts,
      ...over,
    },
    ...(over.team_id ? { team_id: over.team_id } : {}),
  });

export const direct = (text: string, ts: string, over: Record<string, unknown> = {}): Envelope =>
  envelope("events_api", {
    team_id: TEAM,
    event: { type: "message", channel_type: "im", channel: DM, user: USER, text, ts, ...over },
  });

export const stopPressed = (threadTs: string, channel = CHANNEL): Envelope =>
  envelope("events_api", {
    team_id: TEAM,
    event: { type: "agent_session_stopped", channel, thread_ts: threadTs, user: USER },
  });

export const homeOpened = (user = USER): Envelope =>
  envelope("events_api", {
    team_id: TEAM,
    event: { type: "app_home_opened", tab: "home", user },
  });

/** A click on a button the app rendered: the button is found in the blocks and sent back as Slack would. */
export function click(
  blocks: unknown[],
  label: string,
  at: { channel?: string; ts: string; threadTs?: string; user?: string; state?: unknown },
): Envelope {
  type Button = { text?: { text: string }; value?: string };
  type Element = Button & {
    type: string;
    action_id?: string;
    positive_button?: Button;
    negative_button?: Button;
  };
  const elements = (blocks as { type: string; elements?: Element[] }[]).flatMap(
    (b) => (b.type === "actions" || b.type === "context_actions" ? b.elements : undefined) ?? [],
  );
  // A rating control is one element with two buttons, each with its own value.
  const buttons = elements.flatMap((e) =>
    e.type === "feedback_buttons"
      ? [e.positive_button, e.negative_button].map((b) => ({
          action_id: e.action_id,
          value: b?.value,
          label: b?.text?.text,
        }))
      : e.type === "button"
        ? [{ action_id: e.action_id, value: e.value, label: e.text?.text }]
        : [],
  );
  const button = buttons.find((b) => b.label === label);
  if (!button) {
    throw new Error(
      `no button "${label}" among: ${buttons.map((b) => b.label).join(", ") || "none"}`,
    );
  }
  envelopes += 1;
  return envelope("interactive", {
    type: "block_actions",
    team: { id: TEAM },
    user: { id: at.user ?? USER, team_id: TEAM },
    channel: { id: at.channel ?? CHANNEL },
    container: { type: "message", channel_id: at.channel ?? CHANNEL },
    trigger_id: `trigger-${envelopes}`,
    message: { ts: at.ts, thread_ts: at.threadTs },
    actions: [{ action_id: button.action_id, value: button.value }],
    ...(at.state ? { state: at.state } : {}),
  });
}

/**
 * A click on the Home tab, as Slack sends it: from a view, with no message
 * and no channel. The button is found in an actions block or beside a row.
 */
export function homeClick(blocks: unknown[], label: string, user = USER): Envelope {
  type Button = { type: string; text?: { text: string }; action_id?: string; value?: string };
  const buttons = (blocks as { type: string; elements?: Button[]; accessory?: Button }[])
    .flatMap((b) => (b.type === "actions" ? (b.elements ?? []) : b.accessory ? [b.accessory] : []))
    .filter((e) => e.type === "button");
  const button = buttons.find((b) => b.text?.text === label);
  if (!button) {
    throw new Error(
      `no button "${label}" among: ${buttons.map((b) => b.text?.text).join(", ") || "none"}`,
    );
  }
  envelopes += 1;
  return envelope("interactive", {
    type: "block_actions",
    team: { id: TEAM },
    user: { id: user, team_id: TEAM },
    container: { type: "view", view_id: "V01HOME" },
    view: { type: "home" },
    trigger_id: `trigger-${envelopes}`,
    actions: [{ action_id: button.action_id, ...(button.value ? { value: button.value } : {}) }],
  });
}

export const command = (text: string, over: Record<string, unknown> = {}): Envelope => {
  envelopes += 1;
  return envelope("slash_commands", {
    command: "/agent",
    text,
    team_id: TEAM,
    user_id: USER,
    channel_id: CHANNEL,
    trigger_id: `trigger-${envelopes}`,
    response_url: "https://hooks.slack.com/commands/T01ABCDEF/1/fake",
    ...over,
  });
};

export const interactive = (payload: Record<string, unknown>): Envelope => {
  envelopes += 1;
  return envelope("interactive", {
    team: { id: TEAM },
    user: { id: USER, team_id: TEAM },
    trigger_id: `trigger-${envelopes}`,
    ...payload,
  });
};

/** Proposed memories about a run, as the platform's inbox sends them. */
export function memoryMail(run: string): Record<string, unknown> {
  return {
    id: "m-memory",
    kind: "memory",
    subject: "2 proposed memories for lead",
    from: { kind: "agent", name: "lead" },
    at: "2026-10-09T00:10:00Z",
    updated_at: "2026-10-09T00:10:00Z",
    severity: "info",
    state: "open",
    unread: true,
    source: { run, agent: "lead" },
    parts: [
      {
        id: "p1",
        type: "memory",
        state: "open",
        actions: ["keep", "discard"],
        memory: {
          owner: "lead",
          agent: "lead",
          notes: [
            { id: "n1", text: "Deploys are frozen on Fridays." },
            { id: "n2", text: "The staging cluster is rebuilt every Monday." },
          ],
        },
      },
    ],
  };
}

/** The warp thread a run started from a mention is in. */
export const RUN_THREAD = "th-5a1c0ffee0d15ea5";

/**
 * A delegated run's answer, as the platform's inbox sends it: about the run
 * and the thread it is in. An artifact, a recap and a draft carry the same.
 */
export function resultMail(run: string): Record<string, unknown> {
  return {
    id: "m-result",
    kind: "result",
    subject: "lead finished: check the deploy",
    from: { kind: "agent", name: "lead" },
    at: "2026-10-09T00:10:00Z",
    updated_at: "2026-10-09T00:10:00Z",
    severity: "info",
    state: "open",
    unread: true,
    source: { run, thread: RUN_THREAD, agent: "lead", project: "Globex" },
    parts: [{ id: "p1", type: "text", state: "info", text: "The deploy is green." }],
  };
}

/** A failed run no rule caught, as the platform's inbox sends it. */
export function failureMail(run: string): Record<string, unknown> {
  return {
    id: "m-failure",
    kind: "failure",
    subject: "lead failed",
    from: { kind: "agent", name: "lead" },
    at: "2026-10-09T00:10:00Z",
    updated_at: "2026-10-09T00:10:00Z",
    severity: "warning",
    state: "open",
    unread: true,
    // A run is in a thread, as the platform sends one it started for a mention.
    source: { run, thread: RUN_THREAD, agent: "lead" },
    parts: [
      {
        id: "p1",
        type: "failure",
        state: "open",
        actions: ["retry", "acknowledge"],
        failure: { run, agent: "lead", error: "provider: 529 overloaded", retryable: true },
      },
    ],
  };
}

/** A fired alert rule, as the platform's inbox sends it. */
export function alertMail(): Record<string, unknown> {
  return {
    id: "m-alert",
    kind: "alert",
    subject: "oncall is failing more than half its runs",
    from: { kind: "rule", name: "oncall-failure-rate" },
    at: "2026-10-09T00:20:00Z",
    updated_at: "2026-10-09T00:20:00Z",
    severity: "critical",
    state: "open",
    unread: true,
    source: { alert: "al-1", rule: "oncall-failure-rate", agent: "oncall" },
    parts: [
      {
        id: "p1",
        type: "alert",
        state: "open",
        actions: ["acknowledge"],
        alert: {
          id: "al-1",
          rule: "oncall-failure-rate",
          kind: "failure_rate",
          severity: "critical",
          title: "6 of 10 runs failed in the last 15 minutes",
          body: "The provider returned 529 for each failed run.",
        },
      },
    ],
  };
}

/** A reached limit, as the platform's inbox sends it. */
export function budgetMail(): Record<string, unknown> {
  return {
    id: "m-budget",
    kind: "budget",
    subject: "Project Launch reached its daily run limit",
    from: { kind: "system", name: "Limits" },
    at: "2026-10-09T00:30:00Z",
    updated_at: "2026-10-09T00:30:00Z",
    severity: "warning",
    state: "open",
    unread: false,
    source: { project: "Launch" },
    parts: [
      {
        id: "p1",
        type: "budget",
        state: "open",
        actions: ["raise_limit", "acknowledge"],
        budget: {
          scope_kind: "project",
          scope_name: "Launch",
          limit: "runs",
          period: "day",
          version: 2,
          used: 100,
          refused: true,
          current: { max_runs: 100 },
        },
      },
    ],
  };
}

/** A message in a shared thread that tags the reader, as the platform's inbox sends it. */
export function mentionMail(): Record<string, unknown> {
  return {
    id: "m-mention",
    kind: "mention",
    to: "dana",
    subject: "Ana mentioned you in #renewals",
    from: { kind: "person", name: "Ana" },
    at: "2026-10-09T00:40:00Z",
    updated_at: "2026-10-09T00:40:00Z",
    severity: "info",
    state: "open",
    unread: true,
    source: { thread: "th-0123456789abcdef", project: "Globex" },
    parts: [
      {
        id: "p1",
        type: "mention",
        state: "open",
        actions: ["reply", "done"],
        mention: {
          thread: "th-0123456789abcdef",
          message: "m-3",
          project: "Globex",
          channel: "renewals",
          by: "ana",
          excerpt: "@dana can you confirm the renewal date with Globex?",
        },
      },
    ],
  };
}

/** The question a run asks of each kind, as the platform's stream and inbox carry it. */
export const QUESTIONS = {
  approval: {
    id: "q-approve",
    kind: "approval",
    agent: "lead",
    prompt: "Allow lead to call deploy?",
    tool: { name: "deploy", call_id: "call-1", arguments: { service: "web" } },
    expires_at: "2026-10-10T00:00:00Z",
  },
  choice: {
    id: "q-choose",
    kind: "choice",
    agent: "lead",
    prompt: "Which region?",
    options: ["us-east", "eu-west"],
  },
  reply: { id: "q-reply", kind: "reply", agent: "lead", prompt: "What is the ticket number?" },
} as const;

/** A question a run waits on, as the platform's inbox sends it. */
export function questionMail(
  run: string,
  kind: keyof typeof QUESTIONS,
  outcome?: { action: string; by: string; at: string; note?: string },
): Record<string, unknown> {
  return {
    id: `m-${kind}`,
    kind: kind === "approval" ? "approval" : "clarification",
    subject:
      kind === "approval" ? "lead wants to run deploy" : `lead asks: ${QUESTIONS[kind].prompt}`,
    from: { kind: "agent", name: "lead" },
    at: "2026-10-09T00:10:00Z",
    updated_at: "2026-10-09T00:10:00Z",
    severity: kind === "approval" ? "warning" : "info",
    state: outcome ? "resolved" : "open",
    unread: true,
    source: { run, thread: RUN_THREAD, agent: "lead" },
    parts: [
      {
        id: "p1",
        type: kind,
        state: outcome ? "resolved" : "open",
        actions: outcome
          ? []
          : kind === "approval"
            ? ["approve", "deny"]
            : kind === "choice"
              ? ["choose"]
              : ["reply"],
        question: { ...QUESTIONS[kind], run },
        ...(outcome ? { outcome } : {}),
      },
    ],
  };
}

/** An announcement posted in Field, as the platform's inbox sends it. */
export function announcementMail(over: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    id: "m-news",
    kind: "announcement",
    subject: "Field offsite",
    from: { kind: "person", name: "ana" },
    at: "2026-10-09T00:50:00Z",
    updated_at: "2026-10-09T00:50:00Z",
    severity: "info",
    state: "open",
    unread: true,
    source: { project: "Field" },
    parts: [{ id: "p1", type: "text", state: "info", actions: [], text: "Lisbon, in May." }],
    ...over,
  };
}
