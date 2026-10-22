/**
 * Everything this app asks of Slack: Web API calls, and a Socket Mode
 * connection that delivers events without a public URL.
 *
 * The Web API client follows the one in humagen's Slack sandbox: JSON bodies,
 * typed errors, and waiting out a 429 for as long as Slack says.
 */

export class SlackError extends Error {
  constructor(
    readonly method: string,
    readonly code: string,
    readonly detail?: unknown,
  ) {
    super(`${method}: ${code}${detail ? ` ${JSON.stringify(detail)}` : ""}`);
  }
}

export interface SlackApi {
  call<T = Record<string, unknown>>(method: string, args?: Record<string, unknown>): Promise<T>;
  /** Answer a slash command or a shortcut through the address Slack gave for it. */
  respond(url: string, body: Record<string, unknown>): Promise<void>;
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** Only Slack's own hooks are ever posted to, whatever a payload says. */
function slackHook(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === "https:" && u.hostname === "hooks.slack.com";
  } catch {
    return false;
  }
}

/** Read methods that take a form, not JSON. */
const FORM = new Set(["conversations.replies", "users.info"]);

function formOf(args: Record<string, unknown>): URLSearchParams {
  const form = new URLSearchParams();
  for (const [k, v] of Object.entries(args)) {
    if (v !== undefined) form.set(k, typeof v === "string" ? v : JSON.stringify(v));
  }
  return form;
}

export function slackClient(
  token: string,
  opts: { fetch?: typeof fetch; retries?: number } = {},
): SlackApi {
  const call = opts.fetch ?? fetch;
  return {
    async call(method, args = {}) {
      for (let attempt = 0; ; attempt++) {
        const authorization = `Bearer ${token}`;
        const res = await call(
          `https://slack.com/api/${method}`,
          FORM.has(method)
            ? { method: "POST", headers: { authorization }, body: formOf(args) }
            : {
                method: "POST",
                headers: { "content-type": "application/json; charset=utf-8", authorization },
                body: JSON.stringify(args),
              },
        );
        if (res.status === 429 && attempt < (opts.retries ?? 3)) {
          await wait(Number(res.headers.get("retry-after") ?? "1") * 1000);
          continue;
        }
        const body = (await res.json()) as { ok: boolean; error?: string } & Record<
          string,
          unknown
        >;
        if (!body.ok) {
          throw new SlackError(
            method,
            body.error ?? `http_${res.status}`,
            body.response_metadata ?? body.errors,
          );
        }
        return body as never;
      }
    },
    async respond(url, body) {
      if (!slackHook(url)) return;
      await call(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
    },
  };
}

/** One delivery from Slack over Socket Mode, already acknowledged. */
export interface Envelope {
  type: string;
  envelope_id?: string;
  payload?: Record<string, unknown>;
  retry_attempt?: number;
  retry_reason?: string;
}

/** The part of a WebSocket the connection uses, so a test can stand in for one. */
export interface SocketLike {
  onopen: ((ev: unknown) => void) | null;
  onmessage: ((ev: { data: unknown }) => void) | null;
  onclose: ((ev: unknown) => void) | null;
  onerror: ((ev: unknown) => void) | null;
  send(data: string): void;
  close(): void;
}

export interface SocketOptions {
  log?: (line: string) => void;
  signal?: AbortSignal;
  /** No frame for this long and the connection is replaced, in case it died without closing. */
  idleMs?: number;
  /** How long a new connection has to say hello before it counts as failed. */
  helloMs?: number;
  /** How often the watchdog looks. */
  tickMs?: number;
  /** A gap this much longer than a tick means the machine slept: the connection is replaced. */
  sleepMs?: number;
  /** Failed attempts in a row before giving up, so a supervisor can restart the app. */
  maxAttempts?: number;
  /** The first wait after a failure. It doubles on each one, up to `maxBackoffMs`. */
  backoffMs?: number;
  maxBackoffMs?: number;
  /** Opens a socket. The global WebSocket by default. */
  socket?: (url: string) => SocketLike;
}

/** The connection could not be brought back. The app should exit non-zero. */
export class SocketGaveUp extends Error {}

/**
 * Hold a Socket Mode connection open and hand every delivery to `handle`.
 * Each is acknowledged before it is handled, because Slack sends again
 * anything not acknowledged within three seconds.
 *
 * A connection can die without closing: after the machine sleeps, or when
 * the network drops under it. So beside reopening on a close or a
 * `disconnect`, a watchdog replaces the connection when nothing has arrived
 * for `idleMs`, or when its own timer fired far too late, which means the
 * process was asleep. A replacement opens before the old connection closes,
 * so no delivery is missed. Failed attempts back off, and after
 * `maxAttempts` in a row the returned promise rejects with `SocketGaveUp`.
 * It resolves when the signal aborts.
 */
export function socketMode(
  app: SlackApi,
  handle: (envelope: Envelope) => Promise<void>,
  opts: SocketOptions = {},
): Promise<void> {
  const log = opts.log ?? (() => {});
  const idleMs = opts.idleMs ?? 120_000;
  const helloMs = opts.helloMs ?? 15_000;
  const tickMs = opts.tickMs ?? 5_000;
  const sleepMs = opts.sleepMs ?? 30_000;
  const maxAttempts = opts.maxAttempts ?? 10;
  const backoffMs = opts.backoffMs ?? 1_000;
  const maxBackoffMs = opts.maxBackoffMs ?? 60_000;
  const open = opts.socket ?? ((url: string) => new WebSocket(url) as unknown as SocketLike);

  return new Promise<void>((resolve, reject) => {
    /** The connection deliveries are expected on: the last one that said hello. */
    let current: SocketLike | null = null;
    /** Every socket opened and not yet closed, so shutdown closes them all. */
    const sockets = new Set<SocketLike>();
    let connecting = false;
    let failures = 0;
    let lastFrame = Date.now();
    let lastTick = Date.now();
    let retry: ReturnType<typeof setTimeout> | undefined;
    let over = false;

    const end = (error?: Error) => {
      if (over) return;
      over = true;
      clearInterval(watchdog);
      clearTimeout(retry);
      for (const ws of sockets) ws.close();
      sockets.clear();
      if (error) reject(error);
      else resolve();
    };

    const failed = (why: string) => {
      connecting = false;
      if (over || opts.signal?.aborted) return;
      failures += 1;
      if (failures >= maxAttempts) {
        log(`could not reach Slack after ${failures} attempts (${why}), giving up`);
        end(new SocketGaveUp(`Socket Mode: ${failures} failed attempts in a row, last: ${why}`));
        return;
      }
      // Jitter keeps a fleet of restarted apps from reconnecting in step.
      const wait = Math.min(backoffMs * 2 ** (failures - 1), maxBackoffMs);
      const jittered = Math.round(wait * (0.8 + Math.random() * 0.4));
      log(
        `connecting to Slack failed (${why}), attempt ${failures} of ${maxAttempts}, next in ${jittered}ms`,
      );
      clearTimeout(retry);
      retry = setTimeout(connect, jittered);
    };

    /** Open a new connection. The one in use keeps serving until the new one says hello. */
    const connect = () => {
      if (over || connecting || opts.signal?.aborted) return;
      connecting = true;
      app.call<{ url: string }>("apps.connections.open").then(
        ({ url }) => {
          if (over || opts.signal?.aborted) {
            connecting = false;
            return;
          }
          let ws: SocketLike;
          try {
            ws = open(url);
          } catch (e) {
            return failed((e as Error).message);
          }
          sockets.add(ws);
          let hello = false;
          let abandoned = false;
          const helloTimer = setTimeout(() => {
            if (hello) return;
            abandoned = true;
            ws.close();
            sockets.delete(ws);
            failed(`no hello within ${helloMs}ms`);
          }, helloMs);
          ws.onmessage = (message) => {
            lastFrame = Date.now();
            let envelope: Envelope;
            try {
              envelope = JSON.parse(String(message.data)) as Envelope;
            } catch {
              return;
            }
            if (envelope.envelope_id)
              ws.send(JSON.stringify({ envelope_id: envelope.envelope_id }));
            if (envelope.type === "hello") {
              hello = true;
              clearTimeout(helloTimer);
              connecting = false;
              failures = 0;
              const old = current;
              current = ws;
              log(old ? "reconnected to Slack" : "connected to Slack");
              if (old) {
                sockets.delete(old);
                old.close();
              }
            }
            if (envelope.type === "disconnect") {
              // Slack is about to close this one: open the next now.
              if (ws === current) connect();
              return;
            }
            handle(envelope).catch((e: Error) => log(`handler failed: ${e.message}`));
          };
          ws.onerror = () => {};
          ws.onclose = () => {
            sockets.delete(ws);
            if (over || opts.signal?.aborted || abandoned) return;
            if (!hello) {
              clearTimeout(helloTimer);
              return failed("closed before hello");
            }
            if (ws !== current) return;
            current = null;
            log("socket closed, reconnecting");
            connect();
          };
        },
        (e: Error) => failed(e.message),
      );
    };

    const watchdog = setInterval(() => {
      const now = Date.now();
      const late = now - lastTick - tickMs;
      lastTick = now;
      if (late > sleepMs) {
        log(
          `the process was asleep for about ${Math.round(late / 1000)}s, replacing the connection`,
        );
        lastFrame = now;
        return connect();
      }
      if (current && now - lastFrame > idleMs) {
        log(`nothing from Slack for ${idleMs}ms, replacing the connection`);
        lastFrame = now;
        connect();
      }
    }, tickMs);

    opts.signal?.addEventListener("abort", () => end(), { once: true });
    if (opts.signal?.aborted) return end();
    connect();
  });
}
