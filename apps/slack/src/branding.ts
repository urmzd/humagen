import { readFileSync } from "node:fs";
import { type Copy, defaultCopy } from "@warp/components";
import type { SuggestedPrompt } from "@warp/core";
import { words as defaultWords } from "./host.ts";

/**
 * What a deployment may rename or reword without forking: the name people
 * ask, a tagline over the suggested prompts, the disclaimer under every
 * answer, and the prompts Slack offers in the app's messages. It is read
 * from a JSON file named by `SLACK_BRANDING`. Without one, the app reads as
 * it always has.
 */
export interface Branding {
  /** What people call the app, as in "Ask {name}". Left out, it is "the agent". */
  name?: string;
  /** The title over the suggested prompts. */
  tagline?: string;
  /** The line under every answer. `{name}` is replaced by the agent that answered. */
  disclaimer?: string;
  /** What Slack offers to send in the app's messages. At most four. */
  prompts: SuggestedPrompt[];
}

export const DEFAULT_BRANDING: Branding = { prompts: [] };

/** Slack's limits: a modal title holds 24 characters, a prompt's title 75, and four prompts show. */
const LIMIT = {
  name: 20,
  tagline: 150,
  disclaimer: 500,
  prompts: 4,
  promptTitle: 75,
  message: 2000,
};
const KEYS = ["name", "tagline", "disclaimer", "prompts"];

/**
 * Check a parsed branding file. Every problem is reported at once, so a
 * file is fixed in one pass. Unknown keys are refused: a typo must not
 * quietly fall back to the default.
 */
export function parseBranding(raw: unknown): Branding {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("must be a JSON object");
  }
  const o = raw as Record<string, unknown>;
  const problems: string[] = [];
  for (const k of Object.keys(o)) {
    if (!KEYS.includes(k)) problems.push(`unknown key "${k}" (known: ${KEYS.join(", ")})`);
  }
  const text = (key: string, value: unknown, max: number): string | undefined => {
    if (value === undefined) return undefined;
    if (typeof value !== "string" || !value.trim()) {
      problems.push(`${key} must be a non-empty string`);
      return undefined;
    }
    if (value.trim().length > max) problems.push(`${key} must be at most ${max} characters`);
    return value.trim();
  };
  const out: Branding = { prompts: [] };
  const name = text("name", o.name, LIMIT.name);
  const tagline = text("tagline", o.tagline, LIMIT.tagline);
  const disclaimer = text("disclaimer", o.disclaimer, LIMIT.disclaimer);
  if (name) out.name = name;
  if (tagline) out.tagline = tagline;
  if (disclaimer) out.disclaimer = disclaimer;
  if (o.prompts !== undefined) {
    if (!Array.isArray(o.prompts)) {
      problems.push("prompts must be a list");
    } else {
      if (o.prompts.length > LIMIT.prompts) {
        problems.push(`prompts must hold at most ${LIMIT.prompts}, Slack shows no more`);
      }
      for (const [i, p] of o.prompts.entries()) {
        if (!p || typeof p !== "object" || Array.isArray(p)) {
          problems.push(`prompts[${i}] must be an object with title and message`);
          continue;
        }
        const e = p as Record<string, unknown>;
        for (const k of Object.keys(e)) {
          if (k !== "title" && k !== "message")
            problems.push(`prompts[${i}] has unknown key "${k}"`);
        }
        const title = text(`prompts[${i}].title`, e.title ?? "", LIMIT.promptTitle);
        const message = text(`prompts[${i}].message`, e.message ?? "", LIMIT.message);
        if (title && message) out.prompts.push({ title, message });
      }
    }
  }
  if (problems.length > 0) throw new Error(problems.join("; "));
  return out;
}

/** Read and check a branding file. The error names the file and every problem in it. */
export function readBranding(path: string): Branding {
  let raw: unknown;
  try {
    raw = JSON.parse(readFileSync(path, "utf8"));
  } catch (e) {
    throw new Error(`SLACK_BRANDING (${path}) could not be read: ${(e as Error).message}`);
  }
  try {
    return parseBranding(raw);
  } catch (e) {
    throw new Error(`SLACK_BRANDING (${path}) is not valid: ${(e as Error).message}`);
  }
}

/** The shared copy, with the disclaimer this deployment gives. */
export function brandedCopy(b: Branding): Copy {
  const { disclaimer, name } = b;
  if (!disclaimer && !name) return defaultCopy;
  return {
    ...defaultCopy,
    disclaimer: (agent: string) =>
      disclaimer ? disclaimer.replaceAll("{name}", agent) : defaultCopy.disclaimer(name ?? agent),
  };
}

/** The host's own words, with the name this deployment gives. */
export function brandedWords(b: Branding): typeof defaultWords {
  return b.name ? { ...defaultWords, askTitle: `Ask ${b.name}` } : defaultWords;
}
