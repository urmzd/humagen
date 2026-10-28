/**
 * Everything this app asks of humagen, and a hand-written client for it.
 *
 * The rest of the app depends on the `Humagen` interface only. `httpHumagen`
 * is a stand-in for the client generated from the platform's OpenAPI document:
 * when that client lands, this file is the one that changes.
 */
import {
  type AnswerRequest,
  answerPath,
  type Http,
  mailTransport,
  type Run,
  type RunEvent,
  type Transport,
  type WireProject,
} from "@humagen/adapter";

export type { Run, RunEvent, WireProject };

/** Who the platform says is calling, as `GET /api/me` answers. */
export interface Me {
  principal: { subject: string; tenant: string; kind: "user" | "machine"; name?: string };
  permissions: string[];
  first_admin?: boolean;
}

export interface Agent {
  name: string;
  description?: string;
}

/** An agent revision, as `GET /api/agents` lists them. Only the fields this app reads. */
export interface Revision {
  spec: Agent;
  rev?: number;
}

/** The part of a client record this app reads: who answers, and who may. */
export interface ClientRecord {
  capabilities?: { events: string[]; actions: string[]; deliver_outcomes: boolean };
  id: string;
  surface: string;
  enabled: boolean;
  default_agent?: string;
  /** Agents the client may route to besides its default. `*` means any. */
  agents?: string[];
  identity?: { team?: string; app_id?: string };
  /** The credential that may act for Slack people, and who each of them is in the workspace. */
  slack?: {
    machine?: string;
    people?: Record<string, string>;
    commands?: { name: string; description: string; argument_hint?: string }[];
  };
}

/** A Slack person this app speaks for, as the client that maps them names them. */
export interface ActAs {
  client: string;
  user: string;
}

/**
 * The body of `POST /api/runs`, and nothing more: the route refuses fields it
 * does not know, so who asked in Slack, the client and the thread cannot be
 * sent. See the README for what the API would need.
 */
export interface RunRequest {
  /** Who should answer. Empty lets the platform pick. */
  agent?: string;
  text: string;
  /** The turn this one follows. Left out, it starts a conversation. */
  after?: string;
  project?: string;
  /** The Slack person asking, when the client maps them: the run is theirs, not the app's. */
  act_as?: ActAs;
}

/**
 * Where on Slack a message was posted: the thread's ts, the message's own,
 * and this app's bot user, whose mention is not a tag. Only the client's
 * machine sends one, and the platform files the thread from the binding.
 */
export interface SurfaceRef {
  files?: boolean;
  client: string;
  channel: string;
  ts: string;
  message?: string;
  app_user?: string;
}

/** The body of `POST /api/threads` with a surface: a Slack person's message. */
export interface ThreadPostRequest {
  text: string;
  act_as: ActAs;
  surface: SurfaceRef;
  post_only?: boolean;
}

/** What posting a message did, as `POST /api/threads` answers. Only the fields this app reads. */
export interface ThreadPosted {
  thread: { id: string; project: string; channel: string; visibility: string };
  message: { id: string; text: string };
  runs: Run[];
  notified: string[];
  refused: { kind: string; name: string; refused?: string }[];
  rule: string;
  reason?: string;
  /** Posted before, by its ts: nothing was started again. */
  repeated?: boolean;
}

/** An inbox message as the platform sends it: only the fields this app reads on its own. */
export interface WireMessage {
  id: string;
  kind: string;
  state: string;
  subject: string;
  /** When it was raised, which never changes. */
  at?: string;
  /** When it last changed for everyone, such as when it was withdrawn. */
  updated_at?: string;
  /** Set aside by the reader alone: its state is theirs, not everyone's. */
  hidden?: boolean;
  source?: { project?: string; thread?: string };
  parts: { type: string }[];
  /** Where this app recorded posting it, by client. Only the client's own machine is shown its own. */
  deliveries?: Record<string, Receipt>;
  [field: string]: unknown;
}

/**
 * Where a client posted a message, and when it last said the post shows the
 * message as it stands: a message updated after `at` has changed since.
 */
export interface Receipt {
  channel: string;
  ts: string;
  at?: string;
}

/** A Slack channel the client is bound to, and the channel it is bound to in humagen. */
export interface Binding {
  direction?: "two_way" | "inbound" | "outcomes";
  slack_channel: string;
  project: string;
  channel: string;
  /** When the Slack channel was bound there. Nothing announced before it is caught up on. */
  bound_at?: string;
}

/** Every call takes an optional signal, as the generated client's methods do. */
export interface CallOptions {
  signal?: AbortSignal;
}

/** The inbox changed. It names a message and nothing more. */
export interface InboxNotice {
  id: string;
  kind: string;
  state: string;
  updated_at: string;
  /** To this app's machine only: the person it maps a mention or a question is addressed to. */
  for?: ActAs;
}

export type StreamItem =
  | { type: "event"; event: RunEvent }
  | { type: "inbox"; notice: InboxNotice };

/** A limit that turned a run away, as the platform reports it. */
export interface Refusal {
  limit?: string;
  remaining?: number;
  resets_at?: string;
  policy?: { scope?: { kind: string; name?: string }; version?: number; period?: string };
}

export class HumagenError extends Error {
  constructor(
    readonly status: number,
    message: string,
    /** Set when a limit refused the run (429 or 402). */
    readonly refusal?: Refusal,
  ) {
    super(message);
  }
}

/**
 * What this app asks of humagen. The names and shapes follow the generated
 * `@humagen/client` (`createClient`), so swapping it in is mechanical. Where
 * this interface answers null for a 404, the generated client throws: the
 * swap wraps those three calls.
 */
export interface FeedbackInput {
  rating: "up" | "down";
  reason?: string;
  comment?: string;
  act_as?: ActAs;
}

export interface ToolConnections {
  providers: { provider: string; connected: boolean; endpoint?: string }[];
  references: string[];
  values: boolean;
}
export interface Humagen {
  deliveryAllowed(client: string, channel: string, run: string): Promise<boolean>;
  connections(as: ActAs): Promise<ToolConnections>;
  connect(provider: string, ref: string, as: ActAs): Promise<unknown>;
  disconnect(provider: string, as: ActAs): Promise<void>;
  saveFeedback(run: string, body: FeedbackInput): Promise<unknown>;
  getMe(options?: CallOptions): Promise<Me>;
  listAgents(options?: CallOptions): Promise<{ agents: Revision[]; builtin?: string[] }>;
  /** One client record, or null when it does not exist for this caller. */
  getClient(id: string, options?: CallOptions): Promise<ClientRecord | null>;
  /** Start a conversation or continue one. The key goes only in the `Idempotency-Key` header. */
  createRun(body: RunRequest, idempotencyKey?: string, options?: CallOptions): Promise<Run>;
  /** A run's record, or null when it does not exist for this caller. */
  getRun(id: string, options?: CallOptions): Promise<Run | null>;
  getRunFor(id: string, as: ActAs): Promise<Run | null>;
  /**
   * The agents' turns of a conversation, its newest 200, or null when there
   * is no such conversation. People's messages in a shared thread are left
   * out: this app reads a conversation to place and stop runs.
   */
  getThread(id: string, options?: CallOptions): Promise<{ turns: Run[] } | null>;
  stopRun(id: string, options?: CallOptions): Promise<Run>;
  /**
   * Answer the question a run waits on, for the Slack person who clicked.
   * Returns the question's inbox message, settled with what was recorded.
   */
  answerQuestion(answer: AnswerRequest, actAs: ActAs, options?: CallOptions): Promise<unknown>;
  /**
   * A run's events as they happen, from its first. The stream also carries
   * inbox notices for the whole workspace. It ends when the signal aborts.
   */
  runEvents(
    run: { agent: string; id: string },
    options: CallOptions & { from?: number },
  ): AsyncIterable<StreamItem>;
  /**
   * Inbox notices alone, for as long as the signal allows. The stream does
   * not replay what it missed while it was closed: `opened` is called each
   * time it opens, first and after every reconnect, so the caller can catch
   * up on what changed meanwhile.
   */
  notices(options: CallOptions & { opened?: () => void }): AsyncIterable<InboxNotice>;
  /**
   * Post a Slack person's message into the humagen thread its Slack thread is,
   * found by its surface or started where the platform files it.
   */
  postThread(body: ThreadPostRequest, options?: CallOptions): Promise<ThreadPosted>;
  /** One inbox message as the platform sends it, or null when it does not exist for this caller. */
  getInboxMessage(id: string, options?: CallOptions): Promise<WireMessage | null>;
  /** Where a humagen thread is on a client's surface, or null when it is on none or does not exist here. */
  threadSurface(
    id: string,
    options?: CallOptions,
  ): Promise<{ client: string; channel: string; ts: string } | null>;
  /** One inbox message read for a person the client maps, or null when it is not theirs to read. */
  inboxMessageFor(id: string, actAs: ActAs, options?: CallOptions): Promise<WireMessage | null>;
  /** Act on a part of a person's message, for them: a mention's reply or done, a question's answer. */
  resolveFor(
    id: string,
    part: string,
    body: Record<string, unknown>,
    actAs: ActAs,
    options?: CallOptions,
  ): Promise<WireMessage>;
  /** The Slack channels a client is bound to. */
  bindings(client: string, options?: CallOptions): Promise<Binding[]>;
  /** Record where the app posted an inbox message, so it edits that post later. */
  recordDelivery(
    message: string,
    client: string,
    at: { channel: string; ts: string },
    options?: CallOptions,
  ): Promise<void>;
  /**
   * The newest announcements of a project, withdrawn ones too, as this app's
   * own credential reads them in the project's inbox ("" is the
   * workspace's): at most `limit`, newest first, each with this client's
   * receipt when it has one.
   */
  announcements(project: string, limit: number, options?: CallOptions): Promise<WireMessage[]>;
  /**
   * One page of a person's inbox, read for them through the client that maps
   * them. `needs` leaves out posted announcements before the page is cut, so
   * the page holds what `open` counts; `news` keeps only posted
   * announcements, what `updates` counts.
   */
  inboxFor(
    actAs: ActAs,
    query: { state?: string; kind?: string; needs?: boolean; news?: boolean; limit?: number },
    options?: CallOptions,
  ): Promise<{
    messages: Record<string, unknown>[];
    open: number;
    unread: number;
    updates?: number;
  }>;
  /**
   * Every project a person may see, each as they see it and `mine` on the
   * ones that name them, read for them through the client that maps them: at
   * most the 500 a workspace holds.
   */
  projectsFor(actAs: ActAs, options?: CallOptions): Promise<WireProject[]>;
  /**
   * The inbox, through the shared adapter's transport: `listInbox`,
   * `getInboxMessage` and `resolveInboxPart` (plus read, dismiss and snooze)
   * in the generated client.
   */
  inbox: Transport;
}

/** Where a bearer token comes from. Called before every request, so it may refresh. */
export type TokenSource = (opts?: { fresh?: boolean }) => Promise<string | undefined>;

/** A token that never changes, or none at all for a loopback server in dev auth. */
export const staticToken =
  (token?: string): TokenSource =>
  async () =>
    token || undefined;

/**
 * An OAuth client-credentials grant, which is how a machine gets a humagen
 * token from the identity provider. The token is kept until a minute before it
 * expires, and asked for again when the platform refuses it.
 */
export function clientCredentials(cfg: {
  tokenUrl: string;
  clientId: string;
  clientSecret: string;
  scopes: string[];
  audience?: string;
  fetch?: typeof fetch;
  now?: () => number;
}): TokenSource {
  const now = cfg.now ?? Date.now;
  let held: { token: string; until: number } | undefined;
  return async ({ fresh } = {}) => {
    if (held && !fresh && now() < held.until) return held.token;
    const body = new URLSearchParams({ grant_type: "client_credentials" });
    if (cfg.scopes.length > 0) body.set("scope", cfg.scopes.join(" "));
    if (cfg.audience) body.set("audience", cfg.audience);
    const basic = Buffer.from(`${cfg.clientId}:${cfg.clientSecret}`).toString("base64");
    const res = await (cfg.fetch ?? fetch)(cfg.tokenUrl, {
      method: "POST",
      headers: {
        authorization: `Basic ${basic}`,
        "content-type": "application/x-www-form-urlencoded",
      },
      body,
    });
    const data = (await res.json().catch(() => ({}))) as {
      access_token?: string;
      expires_in?: number;
    };
    if (!res.ok || !data.access_token) {
      // The provider's own words may carry the request back, so they are not repeated.
      throw new HumagenError(res.status, "the identity provider did not issue a token");
    }
    held = { token: data.access_token, until: now() + ((data.expires_in ?? 300) - 60) * 1000 };
    return held.token;
  };
}

const enc = encodeURIComponent;

/**
 * Split a server-sent event stream into frames. A frame is the lines up to a
 * blank one: `event:` names it, `data:` lines are its body, and a line that
 * starts with a colon is a comment the server sends to keep the stream open.
 */
export async function* frames(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<{ event: string; data: string }> {
  const decoder = new TextDecoder();
  let buffer = "";
  const reader = body.getReader();
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) return;
      buffer += decoder.decode(value, { stream: true }).replace(/\r\n/g, "\n");
      for (let end = buffer.indexOf("\n\n"); end !== -1; end = buffer.indexOf("\n\n")) {
        const frame = buffer.slice(0, end);
        buffer = buffer.slice(end + 2);
        let event = "message";
        const data: string[] = [];
        for (const line of frame.split("\n")) {
          if (line.startsWith("event:")) event = line.slice(6).trim();
          else if (line.startsWith("data:")) data.push(line.slice(5).replace(/^ /, ""));
        }
        if (data.length > 0) yield { event, data: data.join("\n") };
      }
    }
  } finally {
    reader.cancel().catch(() => {});
  }
}

const sleep = (ms: number, signal: AbortSignal) =>
  new Promise<void>((resolve) => {
    if (signal.aborted) return resolve();
    const done = () => {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    };
    const timer = setTimeout(done, ms);
    signal.addEventListener("abort", done);
  });

/**
 * The stand-in client, over fetch, against the routes in `internal/httpapi`.
 *
 * | Call | Route |
 * |------|-------|
 * | getMe | `GET /api/me` |
 * | listAgents | `GET /api/agents` |
 * | getClient | `GET /api/clients/{id}` |
 * | createRun | `POST /api/runs` with an `Idempotency-Key` header |
 * | getRun | `GET /api/runs/{id}` |
 * | getThread | `GET /api/threads/{id}` |
 * | runEvents, notices | `GET /api/events?run=` as server-sent events, resumed with `from` |
 * | stopRun | `POST /api/runs/{id}/stop` |
 * | answerQuestion | `POST /api/runs/{id}/questions/{question}/answer` |
 * | postThread | `POST /api/threads` with a surface |
 * | getInboxMessage, inboxMessageFor | `GET /api/inbox/{id}`, for a person with `act_as_client` and `act_as_user` |
 * | resolveFor | `POST /api/inbox/{id}/parts/{part}/resolve` with `act_as` |
 * | bindings | `GET /api/clients/{id}/bindings` |
 * | recordDelivery | `PUT /api/inbox/{id}/deliveries/{client}` |
 * | announcements | `GET /api/projects/{name}/inbox?kind=announcement&state=all` |
 * | inboxFor | `GET /api/inbox` with `act_as_client` and `act_as_user` |
 * | projectsFor | `GET /api/projects` with `act_as_client` and `act_as_user`, every page |
 * | inbox | `GET /api/inbox`, `GET /api/inbox/{id}`, `POST /api/inbox/{id}/read`, `/dismiss`, `/snooze`, `/parts/{part}/resolve` |
 */
export function httpHumagen(cfg: {
  baseUrl: string;
  token: TokenSource;
  fetch?: typeof fetch;
  /** How long to wait before reopening an event stream that ended. */
  reconnectMs?: number;
}): Humagen {
  const base = cfg.baseUrl.replace(/\/+$/, "");
  const call = cfg.fetch ?? fetch;

  const send = async (
    method: string,
    path: string,
    init: { json?: unknown; headers?: Record<string, string>; signal?: AbortSignal } = {},
  ): Promise<Response> => {
    const once = async (fresh: boolean) => {
      const token = await cfg.token({ fresh });
      return call(`${base}${path}`, {
        method,
        signal: init.signal,
        headers: {
          ...(token ? { authorization: `Bearer ${token}` } : {}),
          ...(init.json === undefined ? {} : { "content-type": "application/json" }),
          ...init.headers,
        },
        body: init.json === undefined ? undefined : JSON.stringify(init.json),
      });
    };
    const res = await once(false);
    // A token that expired between the check and the call is asked for once more.
    return res.status === 401 ? once(true) : res;
  };

  const http = async <T>(
    method: string,
    path: string,
    json?: unknown,
    headers?: Record<string, string>,
  ): Promise<T> => {
    const res = await send(method, path, { json, headers });
    if (res.status === 204) return undefined as T;
    const data = (await res.json().catch(() => ({}))) as { error?: string } & Refusal;
    if (!res.ok) {
      const refused = res.status === 429 || res.status === 402;
      throw new HumagenError(
        res.status,
        data.error ?? `Request failed (${res.status})`,
        refused
          ? {
              limit: data.limit,
              remaining: data.remaining,
              resets_at: data.resets_at,
              policy: data.policy,
            }
          : undefined,
      );
    }
    return data as T;
  };

  /** Null for what does not exist to this caller, which is also what another tenant's object answers. */
  const orNull = async <T>(work: Promise<T>): Promise<T | null> => {
    try {
      return await work;
    } catch (e) {
      if (e instanceof HumagenError && e.status === 404) return null;
      throw e;
    }
  };

  /**
   * One run's stream, reopened whenever it ends. Every open replays what the
   * bus still holds, so run events are told apart by their sequence number and
   * each is handed on once. Inbox notices are not replayed: `opened` is
   * called once each open is answered, before anything on it is handed on.
   */
  async function* stream(
    run: string,
    signal: AbortSignal,
    from?: number,
    opened?: () => void,
  ): AsyncGenerator<StreamItem> {
    const seen = new Map<string, number>();
    while (!signal.aborted) {
      try {
        const query = `run=${enc(run)}${from === undefined ? "" : `&from=${from}`}`;
        const res = await send("GET", `/api/events?${query}`, {
          signal,
          headers: { accept: "text/event-stream" },
        });
        if (!res.ok || !res.body) {
          throw new HumagenError(res.status, `the event stream answered ${res.status}`);
        }
        opened?.();
        for await (const frame of frames(res.body)) {
          let parsed: unknown;
          try {
            parsed = JSON.parse(frame.data);
          } catch {
            // A frame that is not JSON is ignored rather than fatal.
            continue;
          }
          if (frame.event === "inbox") {
            yield { type: "inbox", notice: parsed as InboxNotice };
            continue;
          }
          const event = parsed as RunEvent;
          const at = event.seq ?? 0;
          if ((seen.get(event.run ?? "") ?? -1) >= at) continue;
          seen.set(event.run ?? "", at);
          yield { type: "event", event };
        }
      } catch (e) {
        if (signal.aborted) return;
        // Not found and not allowed do not get better by asking again.
        if (e instanceof HumagenError && [400, 401, 403, 404].includes(e.status)) throw e;
      }
      await sleep(cfg.reconnectMs ?? 1000, signal);
    }
  }

  const never = new AbortController().signal;
  return {
    connections: (as) =>
      http(
        "GET",
        `/api/me/credentials?${new URLSearchParams({ act_as_client: as.client, act_as_user: as.user })}`,
      ),
    connect: (provider, ref, as) =>
      http(
        "PUT",
        `/api/me/credentials/${enc(provider)}?${new URLSearchParams({ act_as_client: as.client, act_as_user: as.user })}`,
        { ref },
      ),
    disconnect: (provider, as) =>
      http(
        "DELETE",
        `/api/me/credentials/${enc(provider)}?${new URLSearchParams({ act_as_client: as.client, act_as_user: as.user })}`,
      ),
    saveFeedback: (run, body) => http("PUT", `/api/runs/${enc(run)}/feedback`, body),
    getRunFor: (id, as) =>
      orNull(
        http(
          "GET",
          `/api/runs/${enc(id)}?${new URLSearchParams({ act_as_client: as.client, act_as_user: as.user })}`,
        ),
      ),
    getMe: () => http<Me>("GET", "/api/me"),
    listAgents: () => http<{ agents: Revision[]; builtin?: string[] }>("GET", "/api/agents"),
    getClient: (id) => orNull(http<ClientRecord>("GET", `/api/clients/${enc(id)}`)),
    createRun: (body, key) =>
      http<Run>("POST", "/api/runs", body, key ? { "Idempotency-Key": key } : undefined),
    getRun: (id) => orNull(http<Run>("GET", `/api/runs/${enc(id)}`)),
    getThread: (id) =>
      orNull(http<{ turns: { kind: string; run?: Run }[] }>("GET", `/api/threads/${enc(id)}`)).then(
        (page) => page && { turns: page.turns.flatMap((t) => (t.run ? [t.run] : [])) },
      ),
    stopRun: (id) => http<Run>("POST", `/api/runs/${enc(id)}/stop`),
    answerQuestion: (answer, actAs) =>
      http<unknown>("POST", answerPath(answer), { ...answer.body, act_as: actAs }),
    runEvents: (run, options) => stream(run.id, options.signal ?? never, options.from),
    notices: async function* (options) {
      // No run is called this, so the stream carries no run event and is the inbox's alone.
      for await (const item of stream(
        "_inbox",
        options.signal ?? never,
        undefined,
        options.opened,
      )) {
        if (item.type === "inbox") yield item.notice;
      }
    },
    postThread: (body) => http<ThreadPosted>("POST", "/api/threads", body),
    threadSurface: (id) =>
      orNull(
        http<{ thread: { surface?: { client: string; channel: string; ts: string } } }>(
          "GET",
          `/api/threads/${enc(id)}?limit=1`,
        ),
      ).then((page) => page?.thread.surface ?? null),
    inboxMessageFor: (id, actAs) =>
      orNull(
        http<{ message: WireMessage }>(
          "GET",
          `/api/inbox/${enc(id)}?${new URLSearchParams({ act_as_client: actAs.client, act_as_user: actAs.user })}`,
        ),
      ).then((r) => r?.message ?? null),
    resolveFor: (id, part, body, actAs) =>
      http<WireMessage>("POST", `/api/inbox/${enc(id)}/parts/${enc(part)}/resolve`, {
        ...body,
        act_as: actAs,
      }),
    getInboxMessage: (id) =>
      orNull(http<{ message: WireMessage }>("GET", `/api/inbox/${enc(id)}`)).then(
        (r) => r?.message ?? null,
      ),
    deliveryAllowed: (client, channel, run) =>
      http<{ allowed: boolean }>(
        "GET",
        `/api/clients/${enc(client)}/delivery?${new URLSearchParams({ channel, run })}`,
      ).then((r) => r.allowed),
    bindings: (client) =>
      http<{ bindings: Binding[] }>("GET", `/api/clients/${enc(client)}/bindings`).then(
        (r) => r.bindings,
      ),
    recordDelivery: (message, client, at) =>
      http<void>("PUT", `/api/inbox/${enc(message)}/deliveries/${enc(client)}`, at),
    announcements: (project, limit) => {
      const q = new URLSearchParams({ kind: "announcement", state: "all", limit: String(limit) });
      return http<{ messages: WireMessage[] }>(
        "GET",
        `/api/projects/${enc(project || "~")}/inbox?${q}`,
      ).then((r) => r.messages);
    },
    inboxFor: (actAs, query) => {
      const q = new URLSearchParams({ act_as_client: actAs.client, act_as_user: actAs.user });
      if (query.state) q.set("state", query.state);
      if (query.kind) q.set("kind", query.kind);
      if (query.needs) q.set("needs", "true");
      if (query.news) q.set("news", "true");
      if (query.limit) q.set("limit", String(query.limit));
      return http("GET", `/api/inbox?${q}`);
    },
    projectsFor: async (actAs) => {
      const all: WireProject[] = [];
      let before: string | null = null;
      // A page holds 200 and a workspace at most 500, so three pages are all of them.
      for (let page = 0; page < 3; page++) {
        const q = new URLSearchParams({
          act_as_client: actAs.client,
          act_as_user: actAs.user,
          limit: "200",
        });
        if (before) q.set("before", before);
        const r: { projects: WireProject[]; next: string | null } = await http(
          "GET",
          `/api/projects?${q}`,
        );
        all.push(...r.projects);
        before = r.next;
        if (!before) break;
      }
      return all;
    },
    inbox: mailTransport(http as Http),
  };
}
