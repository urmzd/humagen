/**
 * Names the Slack app had before the product was renamed to Humagen, still
 * read. This is the one file in the app that names them.
 */

/** The stream journal's directory under the state directory. Kept: the next start reads it. */
export const JOURNAL_DIR = "warp-slack";

const PREFIX = "HUMAGEN_";
const OLD_PREFIX = "WARP_";

/** The settings the app reads, without the prefix. Only these are adopted from their old names. */
export const SETTINGS = [
  "URL",
  "TENANT",
  "TOKEN",
  "PUBLIC_URL",
  "CLIENT",
  "DEFAULT_AGENT",
  "OIDC_TOKEN_URL",
  "OIDC_CLIENT_ID",
  "OIDC_CLIENT_SECRET",
  "OIDC_SCOPES",
  "OIDC_AUDIENCE",
] as const;

type Env = Record<string, string | undefined>;

/**
 * The environment with each setting set only under its old name copied to
 * its new one, and a note for each old name in use. The new name wins when
 * both are set, as on the server. Blank counts as unset, nothing is removed,
 * and a note names variables, never a value.
 */
export function adoptEnv(env: Env): { env: Env; notes: string[] } {
  const out: Env = { ...env };
  const notes: string[] = [];
  for (const s of SETTINGS) {
    const oldName = OLD_PREFIX + s;
    const newName = PREFIX + s;
    const old = env[oldName];
    if (!old?.trim()) continue;
    const cur = env[newName];
    if (!cur?.trim()) {
      out[newName] = old;
      notes.push(`${oldName} is the old name of ${newName}; it still works, rename it`);
    } else if (cur === old) {
      notes.push(`${oldName} is set beside ${newName} and is ignored; remove it`);
    } else {
      notes.push(`${newName} and ${oldName} differ; using ${newName}`);
    }
  }
  return { env: out, notes: notes.sort() };
}
