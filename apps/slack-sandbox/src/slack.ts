import { slackClient as hostClient, type SlackClient } from "@vultus/slack-host";

/**
 * The sandbox's Slack: the shared host client from `@vultus/slack-host`, with
 * Slack's warnings printed, plus what only a test bench needs.
 */

export { type SlackClient, SlackError, uploadFile } from "@vultus/slack-host";

/** A client for one token that prints Slack's warnings. */
export function slackClient(token: string | undefined): SlackClient {
  return hostClient(token, { log: (line) => console.warn(`  ${line}`) });
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
