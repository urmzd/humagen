/**
 * Where messages come from and what they point at: a session's place (a
 * Slack thread, a pull request, a schedule, the console), references as a
 * person reads them with links back, and the chips a message carries. A
 * response's own work is folded from its stream by `@humagen/core`. No React.
 */

/** The first two non-empty lines of some text, at most 240 characters. */
export function firstLines(text: string): string {
  const lines = text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .slice(0, 2)
    .join(" ");
  return lines.length > 240 ? `${lines.slice(0, 240)}…` : lines;
}

/** A reference an agent read or a session started on, as a person reads it, with a link when it has one. */
export type Reference = {
  kind: "slack" | "github" | "schedule" | "url" | "other";
  label: string;
  url?: string;
};

/**
 * `slack://T/C/ts`, `github://o/r/pull/N`, `github://o/r/issue/N`,
 * `schedule://name`, or a web address. Slack threads open in Slack's own
 * web client by team, channel and thread; a channel id starting with D is a
 * direct message.
 */
export function referenceOf(ref: string): Reference {
  const slack = /^slack:\/\/([^/]+)\/([^/]+)(?:\/([^/]+))?$/.exec(ref);
  if (slack) {
    const [, team = "", channel = "", ts] = slack;
    return {
      kind: "slack",
      label: channel.startsWith("D") ? "Slack DM" : ts ? "Slack thread" : "Slack channel",
      url: slackUrl(team, channel, ts),
    };
  }
  const gh = /^github:\/\/([^/]+)\/([^/]+)\/(pull|issue|issues)\/(\d+)$/.exec(ref);
  if (gh) {
    const [, owner, repo, kind, n] = gh;
    const path = kind === "pull" ? "pull" : "issues";
    return {
      kind: "github",
      label: `${owner}/${repo} ${kind === "pull" ? "PR" : "issue"} #${n}`,
      url: `https://github.com/${owner}/${repo}/${path}/${n}`,
    };
  }
  const repo = /^github:\/\/([^/]+)\/([^/]+)$/.exec(ref);
  if (repo) {
    const [, owner, name] = repo;
    return {
      kind: "github",
      label: `${owner}/${name}`,
      url: `https://github.com/${owner}/${name}`,
    };
  }
  // `schedule://<name>`, or `schedule://<name>/<occurrence>` for one run of it.
  if (ref.startsWith("schedule://"))
    return { kind: "schedule", label: `Schedule ${ref.slice("schedule://".length).split("/")[0]}` };
  if (/^https?:\/\//.test(ref)) {
    try {
      const u = new URL(ref);
      return { kind: "url", label: `${u.host}${u.pathname === "/" ? "" : u.pathname}`, url: ref };
    } catch {
      return { kind: "other", label: ref };
    }
  }
  return { kind: "other", label: ref };
}

/** A Slack thread, or channel, in Slack's web client. Without a team only the channel can be opened. */
export function slackUrl(team: string, channel: string, ts?: string): string {
  const e = encodeURIComponent;
  if (!team) return `https://slack.com/app_redirect?channel=${e(channel)}`;
  return ts
    ? `https://app.slack.com/client/${e(team)}/${e(channel)}/thread/${e(channel)}-${e(ts)}`
    : `https://app.slack.com/client/${e(team)}/${e(channel)}`;
}

/** Where a session started and who can read it there, for its header. */
export type Place = {
  kind: Reference["kind"] | "console";
  label: string;
  url?: string;
  /** The origin as a reference, such as `slack://T/C/ts`: what `get_context` reads by default. */
  origin?: string;
  audience: "dm" | "channel" | "publicChannel" | "privateChannel" | "group" | "private" | "shared";
};

/** Who a root's place was asked in front of, as the platform reports it (`MessageAudience`). */
export type AudienceKind = "public" | "private" | "group" | "direct";

/** The words for an audience the platform reported, at a place of `kind`. */
export function audienceAt(kind: Place["kind"], audience: AudienceKind): Place["audience"] {
  if (kind === "slack")
    return audience === "public"
      ? "publicChannel"
      : audience === "private"
        ? "privateChannel"
        : audience === "group"
          ? "group"
          : "dm";
  return audience === "public" ? "shared" : "private";
}

/**
 * A session's place, from its thread: a Slack thread by its surface (the
 * team, when known, opens it in Slack's web client), a PR, issue or schedule
 * by the root session's parent, or else the console. Its audience is the one
 * the platform reported for the place (`reported`), when it did; otherwise a
 * Slack audience is a DM for a D-prefixed channel and a channel otherwise.
 */
export function placeOf(
  thread: {
    surface?: { client: string; channel: string; ts: string };
    session?: { parent: string };
    visibility: "shared" | "private";
  },
  team = "",
  reported?: AudienceKind,
): Place {
  const place = guessPlace(thread, team);
  return reported && place.kind !== "console"
    ? { ...place, audience: audienceAt(place.kind, reported) }
    : place;
}

function guessPlace(thread: Parameters<typeof placeOf>[0], team: string): Place {
  const s = thread.surface;
  if (s) {
    const dm = s.channel.startsWith("D");
    return {
      kind: "slack",
      label: dm ? "Slack DM" : "Slack thread",
      url: slackUrl(team, s.channel, s.ts),
      origin: `slack://${team || s.client}/${s.channel}/${s.ts}`,
      audience: dm ? "dm" : "channel",
    };
  }
  const parent = thread.session?.parent ?? "";
  if (/^(slack|github|schedule):\/\//.test(parent)) {
    const ref = referenceOf(parent);
    return {
      ...ref,
      origin: parent,
      audience:
        ref.kind === "slack"
          ? ref.label === "Slack DM"
            ? "dm"
            : "channel"
          : thread.visibility === "private"
            ? "private"
            : "shared",
    };
  }
  return {
    kind: "console",
    label: "Console",
    audience: thread.visibility === "private" ? "private" : "shared",
  };
}

/** One reference under a message: where it came from, what it read, what it cites, or something a person should see. */
export type Chip = {
  kind: "origin" | "context" | "source" | "approval" | "failure";
  label: string;
  url?: string;
  /** A short line on hover. */
  detail?: string;
  /** An approval's decision and who made it; the label is the action. */
  decision?: "allow" | "deny" | "ask";
  by?: string;
};
