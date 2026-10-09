/**
 * Minimal Slack Web API client. JSON bodies, typed errors, and Retry-After
 * handling for 429s. No SDK: the sandbox exercises exactly the calls a host
 * would make.
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

export interface SlackClient {
  call<T = Record<string, unknown>>(method: string, args?: Record<string, unknown>): Promise<T>;
}

export function slackClient(token: string | undefined): SlackClient {
  return {
    async call(method, args = {}) {
      for (let attempt = 0; ; attempt++) {
        const res = await fetch(`https://slack.com/api/${method}`, {
          method: "POST",
          headers: {
            "content-type": "application/json; charset=utf-8",
            ...(token ? { authorization: `Bearer ${token}` } : {}),
          },
          body: JSON.stringify(args),
        });
        if (res.status === 429 && attempt < 3) {
          const wait = Number(res.headers.get("retry-after") ?? "1");
          await new Promise((r) => setTimeout(r, wait * 1000));
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
        if (body.warning) console.warn(`  ${method} warning: ${String(body.warning)}`);
        return body as never;
      }
    },
  };
}

/** blocks.validate is public: no token. Form-encoded, per the docs. */
export async function validateBlocks(blocks: unknown[], view?: object) {
  const form = new URLSearchParams(
    view ? { view: JSON.stringify(view) } : { blocks: JSON.stringify(blocks) },
  );
  for (let attempt = 0; ; attempt++) {
    const res = await fetch("https://slack.com/api/blocks.validate", {
      method: "POST",
      body: form,
    });
    const body = (await res.json()) as {
      ok: boolean;
      error?: string;
      errors?: { message: string; pointer: string }[];
    };
    if (body.error !== "ratelimited" || attempt >= 5) return body;
    const wait = Number(res.headers.get("retry-after") ?? "2");
    await new Promise((r) => setTimeout(r, wait * 1000));
  }
}

export interface SandboxEnv {
  botToken: string;
  appToken?: string;
  channel: string;
  /** The human who receives streams in the channel (required by chat.startStream in channels). */
  testUser?: string;
}

export function readEnv(): SandboxEnv {
  const botToken = process.env.SLACK_BOT_TOKEN;
  const channel = process.env.SLACK_CHANNEL;
  const missing = [!botToken && "SLACK_BOT_TOKEN", !channel && "SLACK_CHANNEL"].filter(Boolean);
  if (missing.length > 0) {
    throw new Error(
      `Missing ${missing.join(", ")}. Copy apps/slack-sandbox/.envrc.local.example to the repo root as .envrc.local and run direnv allow.`,
    );
  }
  return {
    botToken: botToken as string,
    channel: channel as string,
    appToken: process.env.SLACK_APP_TOKEN,
    testUser: process.env.SLACK_TEST_USER,
  };
}
