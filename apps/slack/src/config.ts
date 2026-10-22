import { type Branding, DEFAULT_BRANDING, readBranding } from "./branding.ts";
import { clientCredentials, staticToken, type TokenSource } from "./humagen.ts";
import { defaultJournalPath } from "./journal.ts";
import { adoptEnv } from "./legacy.ts";

/** The permissions the app cannot work without. Others unlock more of the inbox, see the README. */
export const NEEDED = ["runs:read", "runs:write", "agents:read"];
/** What the Home and some buttons can use when granted. Anything beyond is more than the app needs. */
export const USEFUL = [
  ...NEEDED,
  "clients:read",
  "projects:read",
  "agents:write",
  "alerts:read",
  "alerts:write",
];

export interface Config {
  slack: {
    botToken: string;
    appToken: string;
    /** The one Slack team this instance serves. Events from any other are refused. */
    team: string;
    /** Slack user ids who may settle any inbox item from Slack, and see the whole inbox on Home. */
    operators: string[];
    /** Name, tagline, disclaimer and suggested prompts, from the file `SLACK_BRANDING` names. */
    branding: Branding;
    /** The file that records open streams, so the next start can close them. */
    streamJournal: string;
    /** Socket Mode liveness: replace a silent connection, and how many failures in a row end the app. */
    socket: { idleMs?: number; maxAttempts?: number };
  };
  /** What to tell the operator at start: each old setting name in use. */
  notes: string[];
  humagen: {
    url: string;
    /** The one workspace this instance serves. Startup fails if the credential is for another. */
    tenant: string;
    token: TokenSource;
    /** The client record this app is, when it is one. */
    client?: string;
    defaultAgent?: string;
    /** Where people reach the console, for links to files. */
    publicUrl?: string;
  };
}

const list = (v: string | undefined) =>
  (v ?? "")
    .split(/[\s,]+/)
    .map((s) => s.trim())
    .filter(Boolean);

/** Read the environment. Every missing name is reported at once, and no value is ever echoed. */
export function readConfig(given: Record<string, string | undefined> = process.env): Config {
  // A setting under its old WARP_ name is read as its HUMAGEN_ one, with a note.
  const { env, notes } = adoptEnv(given);
  const missing: string[] = [];
  const need = (name: string) => {
    const v = env[name]?.trim();
    if (!v) missing.push(name);
    return v ?? "";
  };
  const slack = {
    botToken: need("SLACK_BOT_TOKEN"),
    appToken: need("SLACK_APP_TOKEN"),
    team: need("SLACK_TEAM_ID"),
    operators: list(env.SLACK_OPERATORS),
  };
  // A branding file that is named but cannot be used stops the app here, before it connects.
  const brandingPath = env.SLACK_BRANDING?.trim();
  const tenant = need("HUMAGEN_TENANT");
  const tokenUrl = env.HUMAGEN_OIDC_TOKEN_URL?.trim();
  let token: TokenSource = staticToken(env.HUMAGEN_TOKEN?.trim());
  if (tokenUrl) {
    token = clientCredentials({
      tokenUrl,
      clientId: need("HUMAGEN_OIDC_CLIENT_ID"),
      clientSecret: need("HUMAGEN_OIDC_CLIENT_SECRET"),
      scopes: list(env.HUMAGEN_OIDC_SCOPES),
      audience: env.HUMAGEN_OIDC_AUDIENCE?.trim() || undefined,
    });
  }
  const bad: string[] = [];
  const count = (name: string, min: number): number | undefined => {
    const v = env[name]?.trim();
    if (!v) return undefined;
    const n = Number(v);
    if (!Number.isInteger(n) || n < min)
      bad.push(`${name} must be a whole number of at least ${min}`);
    return n;
  };
  const idleMs = count("SLACK_SOCKET_IDLE_MS", 10_000);
  const maxAttempts = count("SLACK_SOCKET_MAX_ATTEMPTS", 1);
  if (bad.length > 0) throw new Error(bad.join("; "));
  if (missing.length > 0) {
    throw new Error(
      `Missing ${missing.join(", ")}. Copy apps/slack/.envrc.local.example to the repository root as .envrc.local, fill it in and run direnv allow.`,
    );
  }
  return {
    notes,
    slack: {
      ...slack,
      branding: brandingPath ? readBranding(brandingPath) : DEFAULT_BRANDING,
      streamJournal: env.SLACK_STREAM_JOURNAL?.trim() || defaultJournalPath(slack.team, env),
      socket: {
        ...(idleMs !== undefined ? { idleMs } : {}),
        ...(maxAttempts !== undefined ? { maxAttempts } : {}),
      },
    },
    humagen: {
      url: env.HUMAGEN_URL?.trim() || "http://127.0.0.1:8080",
      tenant,
      token,
      client: env.HUMAGEN_CLIENT?.trim() || undefined,
      defaultAgent: env.HUMAGEN_DEFAULT_AGENT?.trim() || undefined,
      publicUrl: env.HUMAGEN_PUBLIC_URL?.trim().replace(/\/+$/, "") || undefined,
    },
  };
}
