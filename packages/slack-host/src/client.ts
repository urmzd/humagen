/**
 * A small Slack Web API client: JSON bodies, typed errors, and waiting out a
 * 429 for as long as Slack says. No SDK: a host makes exactly these calls.
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

/**
 * Slack's error code for a failed call, from any client's error that carries
 * one as `code` (a `SlackError`, or a host's own), or undefined.
 */
export function slackCode(e: unknown): string | undefined {
  const code = e instanceof Error ? (e as Error & { code?: unknown }).code : undefined;
  return typeof code === "string" ? code : undefined;
}

/** All a turn needs of Slack: one Web API call. A host's own client fits as it is. */
export interface SlackCaller {
  call<T = Record<string, unknown>>(method: string, args?: Record<string, unknown>): Promise<T>;
}

export interface SlackClient extends SlackCaller {
  /** Form-encoded call, for methods that don't take JSON (files.getUploadURLExternal). */
  form<T = Record<string, unknown>>(method: string, args: Record<string, string>): Promise<T>;
  /**
   * Answer a slash command or a shortcut through the `response_url` Slack gave
   * for it. Only Slack's own hooks are posted to, whatever a payload says.
   */
  respond(url: string, body: Record<string, unknown>): Promise<void>;
}

export interface ClientOptions {
  fetch?: typeof fetch;
  /** How many times a 429 is waited out before it is an error. Default 3. */
  retries?: number;
  /** Where Slack's warnings go. */
  log?: (line: string) => void;
}

const wait = (ms: number) => new Promise((r) => setTimeout(r, ms));

/** The longest Retry-After waited out; a longer one is an error to the caller. */
const MAX_RETRY_AFTER_S = 60;

/** Seconds to wait from a Retry-After header: one when missing or unreadable, capped. */
function retryAfter(res: Response): number {
  const s = Number(res.headers.get("retry-after"));
  if (!Number.isFinite(s) || s <= 0) return 1;
  return Math.min(s, MAX_RETRY_AFTER_S);
}

function slackHook(url: string): boolean {
  try {
    const u = new URL(url);
    return u.protocol === "https:" && u.hostname === "hooks.slack.com";
  } catch {
    return false;
  }
}

type Body = { ok: boolean; error?: string } & Record<string, unknown>;

/** Slack's answer, or a SlackError naming the method when it isn't JSON (a 5xx page). */
async function read(method: string, res: Response): Promise<Body> {
  try {
    return (await res.json()) as Body;
  } catch {
    throw new SlackError(method, `http_${res.status}`);
  }
}

/** A client for one token. Without a token it can still call public methods (blocks.validate). */
export function slackClient(token: string | undefined, opts: ClientOptions = {}): SlackClient {
  const send = opts.fetch ?? fetch;
  const retries = opts.retries ?? 3;
  const auth: Record<string, string> = token ? { authorization: `Bearer ${token}` } : {};
  /** One call, with a 429 waited out up to `retries` times. */
  const request = async (method: string, init: () => RequestInit): Promise<Body> => {
    for (let attempt = 0; ; attempt++) {
      const res = await send(`https://slack.com/api/${method}`, { method: "POST", ...init() });
      if (res.status === 429 && attempt < retries) {
        await wait(retryAfter(res) * 1000);
        continue;
      }
      const body = await read(method, res);
      if (!body.ok) {
        throw new SlackError(
          method,
          body.error ?? `http_${res.status}`,
          body.response_metadata ?? body.errors,
        );
      }
      if (body.warning) opts.log?.(`${method} warning: ${String(body.warning)}`);
      return body;
    }
  };
  return {
    async call(method, args = {}) {
      return (await request(method, () => ({
        headers: { "content-type": "application/json; charset=utf-8", ...auth },
        body: JSON.stringify(args),
      }))) as never;
    },
    async form(method, args) {
      return (await request(method, () => ({
        headers: auth,
        body: new URLSearchParams(args),
      }))) as never;
    },
    async respond(url, body) {
      if (!slackHook(url)) return;
      const res = await send(url, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      if (!res.ok) throw new SlackError("respond", `http_${res.status}`);
    },
  };
}

/**
 * Upload a file the way Slack recommends: reserve an upload URL, POST the
 * bytes, then complete the upload into a channel or thread. Returns the file
 * id and its permalink, for an artifact's `url`.
 */
export async function uploadFile(
  slack: SlackClient,
  file: { name: string; bytes: Uint8Array; title?: string },
  where: { channel: string; threadTs?: string; comment?: string },
  opts: { fetch?: typeof fetch } = {},
) {
  const { upload_url, file_id } = await slack.form<{ upload_url: string; file_id: string }>(
    "files.getUploadURLExternal",
    { filename: file.name, length: String(file.bytes.byteLength) },
  );
  const put = await (opts.fetch ?? fetch)(upload_url, {
    method: "POST",
    body: new Blob([file.bytes as Uint8Array<ArrayBuffer>]),
  });
  if (!put.ok) throw new SlackError("upload", `http_${put.status}`);
  const done = await slack.call<{ files: { id: string; permalink?: string }[] }>(
    "files.completeUploadExternal",
    {
      files: [{ id: file_id, title: file.title ?? file.name }],
      channel_id: where.channel,
      ...(where.threadTs ? { thread_ts: where.threadTs } : {}),
      ...(where.comment ? { initial_comment: where.comment } : {}),
    },
  );
  return { id: file_id, permalink: done.files[0]?.permalink };
}
