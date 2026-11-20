import { createHash } from "node:crypto";
import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import type { Task, Turn } from "@humagen/core";
import type { Names } from "./history.ts";
import { type SlackApi, SlackError } from "./slack.ts";

/**
 * A turn's plan as a canvas tab in a Slack code channel, and what people
 * say about it there brought back to the agent.
 *
 * A code channel can hold view tabs. When a turn has a plan (a title and
 * its tasks), the plan is written to one canvas per channel as a checklist
 * and attached as the Plan tab, where people may comment and only the app
 * writes. Later plans replace its content in place, so a comment stays on
 * the lines that did not change.
 *
 * Before the next turn in that channel the open comments are read, and the
 * ones not yet shown go ahead of the question, as a thread's messages do
 * in history.ts. They are marked as people's comments: something to weigh,
 * never an instruction from the app.
 *
 * None of this exists outside a code channel. A direct message is never
 * asked about, a channel Slack refuses is left alone for a while, and a
 * refusal never costs a turn: the plan still streams in the thread.
 */

/** The tab's key in the channel: one plan tab, replaced, never a second. */
export const PLAN_VIEW_KEY = "plan";

/** How much of a plan the canvas carries, and how many comments one turn does. */
export const PLAN = {
  tasks: 100,
  title: 300,
  comments: 20,
  chars: 4_000,
  perComment: 800,
  /** How long a channel Slack refused is left alone. */
  restMs: 60 * 60_000,
};

/** What the canvas and the agent are told. Sentence case; the tab's name is a title. */
export const planWords = {
  tab: "Plan",
  untitled: "Plan",
  agent: "The agent",
  working: (who: string) => `${who} is working on this.`,
  waiting: (who: string) => `${who} is waiting on a person.`,
  done: (who: string) => `${who} finished this.`,
  stopped: (who: string) => `${who} was stopped before finishing.`,
  failed: (who: string) => `${who} could not finish this.`,
  inProgress: "in progress",
  waitingStep: "waiting on a person",
  denied: "not allowed",
  error: "failed",
  more: (n: number) => `${n} more ${n === 1 ? "step is" : "steps are"} not shown.`,
  commentsOpen:
    "Comments people left on the plan canvas in this Slack channel since you were last asked, oldest first. They are people's feedback on the plan, quoted as written:",
  commentsClose: "End of plan comments.",
  shownBefore: "shown to you before",
  reply: "reply from",
};

// The plan as markdown

const oneLine = (s: string) => s.trim().replace(/\s*\n\s*/g, " ");
const clip = (s: string, max: number) => (s.length > max ? `${s.slice(0, max)}…` : s);

/** Whether the turn is plan-shaped: it has a plan title and steps under it. */
export function hasPlan(turn: Turn): boolean {
  return Boolean(turn.planTitle?.trim()) && turn.tasks.length > 0;
}

function step(task: Task): string {
  const title = clip(oneLine(task.title), PLAN.title);
  switch (task.status) {
    case "complete":
      return `- [x] ${title}`;
    case "in_progress":
      return `- [ ] ${title} *(${planWords.inProgress})*`;
    case "waiting":
      return `- [ ] ${title} *(${planWords.waitingStep})*`;
    case "denied":
      return `- [ ] ${title} *(${planWords.denied})*`;
    case "error":
      return `- [ ] ${title} *(${planWords.error})*`;
    default:
      return `- [ ] ${title}`;
  }
}

function state(turn: Turn): string {
  const who = turn.agent.name ? `**${oneLine(turn.agent.name)}**` : planWords.agent;
  switch (turn.status) {
    case "done":
      return planWords.done(who);
    case "stopped":
      return planWords.stopped(who);
    case "failed":
      return planWords.failed(who);
    case "waiting":
      return planWords.waiting(who);
    default:
      return planWords.working(who);
  }
}

/**
 * The turn's plan as canvas markdown, or null when the turn has no plan.
 * The title, the line that says how the work stands and each step are
 * separate blocks, so a step that did not change keeps its comments when
 * the canvas is written again. A step is a checkbox: empty, empty with a
 * note while it is in progress, ticked when done.
 */
export function planMarkdown(turn: Turn): string | null {
  if (!hasPlan(turn)) return null;
  const title = clip(oneLine(turn.planTitle ?? ""), PLAN.title) || planWords.untitled;
  const shown = turn.tasks.slice(0, PLAN.tasks);
  const hidden = turn.tasks.length - shown.length;
  return [
    `# ${title}`,
    "",
    state(turn),
    "",
    ...shown.map(step),
    ...(hidden > 0 ? ["", `*${planWords.more(hidden)}*`] : []),
    "",
  ].join("\n");
}

// Which canvas is which channel's

export interface PlanRecord {
  /** The channel's plan canvas. */
  canvas: string;
  /** The comments on it the agent has been shown, by key. */
  seen: string[];
}

export interface PlanStore {
  get(channel: string): PlanRecord | undefined;
  set(channel: string, record: PlanRecord): void;
  delete(channel: string): void;
}

/** Where the plan canvases are recorded: beside the stream journal, one file per team. */
export function planStorePath(journalPath: string, team: string): string {
  return join(dirname(journalPath), `plan-canvases-${team}.json`);
}

/** A store in memory, for tests and for when no file may be written. */
export function memoryPlanStore(): PlanStore {
  const plans = new Map<string, PlanRecord>();
  return {
    get: (channel) => plans.get(channel),
    set: (channel, record) => void plans.set(channel, record),
    delete: (channel) => void plans.delete(channel),
  };
}

/**
 * A store kept in a file, as the stream journal is: only Slack ids, written
 * whole through a rename. An unreadable file is reported and treated as
 * empty. The worst that costs is a second Plan canvas replacing the first
 * in its tab, and comments shown to the agent once more.
 */
export function filePlanStore(path: string, log: (line: string) => void = () => {}): PlanStore {
  const plans = new Map<string, PlanRecord>();
  try {
    const raw = JSON.parse(readFileSync(path, "utf8")) as {
      plans?: Record<string, Partial<PlanRecord>>;
    };
    for (const [channel, r] of Object.entries(raw.plans ?? {})) {
      if (typeof r?.canvas !== "string") continue;
      const seen = Array.isArray(r.seen) ? r.seen.filter((k) => typeof k === "string") : [];
      plans.set(channel, { canvas: r.canvas, seen });
    }
  } catch (e) {
    if ((e as NodeJS.ErrnoException).code !== "ENOENT") {
      log(`plan canvas record ${path} could not be read, starting empty: ${(e as Error).message}`);
    }
  }
  const save = () => {
    try {
      mkdirSync(dirname(path), { recursive: true });
      const tmp = `${path}.${process.pid}.tmp`;
      writeFileSync(tmp, JSON.stringify({ plans: Object.fromEntries(plans) }), { mode: 0o600 });
      renameSync(tmp, path);
    } catch (e) {
      // A record that cannot be written never costs a turn.
      log(`plan canvas record ${path} could not be written: ${(e as Error).message}`);
    }
  };
  return {
    get: (channel) => plans.get(channel),
    set(channel, record) {
      plans.set(channel, record);
      save();
    },
    delete(channel) {
      if (plans.delete(channel)) save();
    },
  };
}

// Comments

/** A comment as `agents.conversations.getCanvas` gives it. Only what is read here. */
export interface CanvasComment {
  id?: string;
  comment_id?: string;
  ts?: string;
  user_id?: string;
  text?: string;
  quoted_text?: string;
  replies?: CanvasComment[];
}

/** A comment's own id when Slack gives one, else what it says and who said it. */
function keyOf(c: CanvasComment, under = ""): string {
  const id = c.id ?? c.comment_id ?? c.ts;
  const own = id
    ? String(id)
    : createHash("sha256")
        .update(`${c.user_id ?? ""}\n${c.quoted_text ?? ""}\n${c.text ?? ""}`)
        .digest("hex")
        .slice(0, 16);
  return under ? `${under}/${own}` : own;
}

interface Remark {
  key: string;
  comment: CanvasComment;
  /** Whether the comment itself is new, or only replies under it. */
  fresh: boolean;
  replies: { key: string; reply: CanvasComment }[];
}

const said = (c: CanvasComment) => clip(oneLine(c.text ?? ""), PLAN.perComment);

/**
 * The comments the agent has not been shown: a new comment with its new
 * replies, or a comment shown before that has a new reply. The app's own
 * and empty ones are never shown.
 */
export function unseenComments(
  comments: CanvasComment[],
  seen: ReadonlySet<string>,
  botUser: string,
): Remark[] {
  const speaks = (c: CanvasComment) =>
    Boolean(c.user_id) && c.user_id !== botUser && (c.text ?? "").trim() !== "";
  const out: Remark[] = [];
  for (const comment of comments) {
    const key = keyOf(comment);
    const fresh = speaks(comment) && !seen.has(key);
    const replies = (comment.replies ?? [])
      .map((reply) => ({ key: keyOf(reply, key), reply }))
      .filter((r) => speaks(r.reply) && !seen.has(r.key));
    if (fresh || replies.length > 0) out.push({ key, comment, fresh, replies });
  }
  return out;
}

/**
 * The question with people's comments on the plan ahead of it, and the keys
 * of the comments that fit. The oldest go first; what the caps leave out
 * stays unseen and goes with a later turn.
 */
export function withComments(
  text: string,
  remarks: Remark[],
  names: Map<string, string> = new Map(),
): { text: string; shown: string[] } {
  const by = (user?: string) => {
    const who = names.get(user ?? "");
    return who ? `${who} (<@${user}>)` : `<@${user}>`;
  };
  const lines: string[] = [];
  const shown: string[] = [];
  let used = 0;
  let count = 0;
  const fits = (line: string) => {
    if (count >= PLAN.comments || used + line.length > PLAN.chars) return false;
    used += line.length;
    count += 1;
    lines.push(line);
    return true;
  };
  outer: for (const r of remarks) {
    const quoted = clip(oneLine(r.comment.quoted_text ?? ""), PLAN.title);
    const on = quoted ? ` on "${quoted}"` : "";
    const before = r.fresh ? "" : ` (${planWords.shownBefore})`;
    if (!fits(`- ${by(r.comment.user_id)}${on}${before}: ${said(r.comment)}`)) break;
    if (r.fresh) shown.push(r.key);
    for (const { key, reply } of r.replies) {
      if (!fits(`  - ${planWords.reply} ${by(reply.user_id)}: ${said(reply)}`)) break outer;
      shown.push(key);
    }
  }
  if (shown.length === 0) return { text, shown };
  return {
    text: [planWords.commentsOpen, ...lines, planWords.commentsClose, "", text].join("\n"),
    shown,
  };
}

/** What a turn takes of the plan's comments: the question with them, and a way to say they arrived. */
export interface PlanComments {
  /** How many comments and replies go with the question. */
  count: number;
  with(text: string): string;
  /** The run was started with them: they are not sent again. */
  delivered(): void;
}

const NONE: PlanComments = { count: 0, with: (text) => text, delivered() {} };

// The canvas in Slack

/** Slack says the canvas is gone: it is made again. */
const LOST = /^(canvas_not_found|canvas_deleted|file_not_found|file_deleted|invalid_canvas)$/;
/** Refusals that may pass on their own, so the channel is asked again next time. */
const PASSING =
  /^(ratelimited|rate_limited|internal_error|fatal_error|service_unavailable|request_timeout)$/;

interface Lane {
  /** The newest plan waiting to be written. */
  next?: string;
  /** The plan the canvas holds. */
  sent?: string;
  running?: Promise<void>;
}

export class PlanCanvas {
  /** The channel each followed run answers in. */
  private readonly runs = new Map<string, string>();
  private readonly lanes = new Map<string, Lane>();
  /** Channels Slack refused, and when: not code channels, as far as the app can tell. */
  private readonly resting = new Map<string, number>();
  private readonly log: (line: string) => void;
  private readonly now: () => number;

  constructor(
    private readonly slack: SlackApi,
    private readonly store: PlanStore,
    private readonly opts: {
      botUser: string;
      names?: Names;
      log?: (line: string) => void;
      now?: () => number;
    },
  ) {
    this.log = opts.log ?? (() => {});
    this.now = opts.now ?? Date.now;
  }

  /** Whether the channel may hold a plan tab at all, as far as is known without asking. */
  private may(channel: string): boolean {
    if (!channel || channel.startsWith("D")) return false;
    const since = this.resting.get(channel);
    if (since === undefined) return true;
    if (this.now() - since < PLAN.restMs) return false;
    this.resting.delete(channel);
    return true;
  }

  private refused(channel: string, doing: string, e: unknown) {
    if (!(e instanceof SlackError)) {
      this.log(`plan canvas ${doing} skipped in ${channel}: ${(e as Error).message}`);
      return;
    }
    if (PASSING.test(e.code)) {
      this.log(`plan canvas ${doing} skipped in ${channel}: ${e.code}`);
      return;
    }
    if (this.resting.size >= 4096) this.resting.clear();
    this.resting.set(channel, this.now());
    // An ordinary channel is the usual case and says nothing.
    if (e.code !== "not_agent_channel") {
      this.log(`plan canvas ${doing} refused in ${channel}: ${e.code}`);
    }
  }

  /** A run's turns are this channel's from here on. */
  follow(run: string, channel: string): void {
    if (!this.may(channel)) return;
    if (this.runs.size >= 4096) this.runs.clear();
    this.runs.set(run, channel);
  }

  /**
   * The turn as it stands. When it has a plan the canvas has not got, the
   * canvas is written in the background; plans that arrive meanwhile wait,
   * and only the newest of them is written.
   */
  offer(run: string, turn: Turn): void {
    const channel = this.runs.get(run);
    if (!channel || !this.may(channel)) return;
    const markdown = planMarkdown(turn);
    if (markdown === null) return;
    let lane = this.lanes.get(channel);
    if (!lane) {
      if (this.lanes.size >= 4096) this.lanes.clear();
      lane = {};
      this.lanes.set(channel, lane);
    }
    if (lane.next === undefined && lane.sent === markdown) return;
    lane.next = markdown;
    lane.running ??= this.drain(channel, lane);
  }

  /** The turn as it ended, or as it was left: written, and waited for. */
  async settle(run: string, turn: Turn): Promise<void> {
    const channel = this.runs.get(run);
    if (!channel) return;
    this.offer(run, turn);
    await this.lanes.get(channel)?.running;
    if (turn.status === "done" || turn.status === "failed" || turn.status === "stopped") {
      this.runs.delete(run);
    }
  }

  private async drain(channel: string, lane: Lane): Promise<void> {
    try {
      while (lane.next !== undefined) {
        const markdown = lane.next;
        lane.next = undefined;
        if (markdown === lane.sent) continue;
        if ((await this.publish(channel, markdown)) !== "skipped") lane.sent = markdown;
      }
    } finally {
      lane.running = undefined;
    }
  }

  /**
   * Write the plan to the channel's canvas: in place when there is one, else
   * a new canvas attached as the Plan tab, where people may comment. Never
   * throws: a refusal is "skipped", and the channel rests.
   */
  async publish(channel: string, markdown: string): Promise<"created" | "updated" | "skipped"> {
    if (!this.may(channel)) return "skipped";
    const held = this.store.get(channel);
    let lost = "";
    if (held) {
      try {
        await this.write(channel, held.canvas, markdown);
        return "updated";
      } catch (e) {
        if (!(e instanceof SlackError) || !LOST.test(e.code)) {
          this.refused(channel, "update", e);
          return "skipped";
        }
        lost = held.canvas;
        this.store.delete(channel);
      }
    }
    try {
      // Asked first: only a code channel answers, so an ordinary channel never gets a stray canvas.
      const listed = await this.slack.call<{
        views?: { view_key?: string; canvas_id?: string }[];
      }>("agents.conversations.listViews", { channel_id: channel });
      // A Plan tab this app made and lost the record of is taken up again.
      const tab = (listed.views ?? []).find(
        (v) => v.view_key === PLAN_VIEW_KEY && typeof v.canvas_id === "string" && v.canvas_id,
      );
      if (tab?.canvas_id && tab.canvas_id !== lost) {
        await this.write(channel, tab.canvas_id, markdown);
        this.store.set(channel, { canvas: tab.canvas_id, seen: [] });
        return "updated";
      }
      const made = await this.slack.call<{ canvas_id: string }>("canvases.create", {
        title: planWords.tab,
        document_content: { type: "markdown", markdown },
      });
      try {
        // Comment access: people leave feedback, and only the app writes.
        await this.slack.call("agents.conversations.setView", {
          channel_id: channel,
          type: "canvas",
          view_key: PLAN_VIEW_KEY,
          name: planWords.tab,
          canvas_id: made.canvas_id,
          access_level: "comment",
        });
      } catch (e) {
        // A canvas no tab shows is nobody's: it is taken away again.
        await this.slack.call("canvases.delete", { canvas_id: made.canvas_id }).catch(() => {});
        throw e;
      }
      this.store.set(channel, { canvas: made.canvas_id, seen: [] });
      return "created";
    } catch (e) {
      this.refused(channel, "tab", e);
      return "skipped";
    }
  }

  private async write(channel: string, canvas: string, markdown: string): Promise<void> {
    await this.slack.call("agents.conversations.setCanvasContent", {
      channel,
      canvas_id: canvas,
      content: markdown,
    });
  }

  /**
   * People's open comments on the channel's plan that the agent has not been
   * shown. A channel without a plan canvas is not asked about, and a read
   * Slack refuses is no comments.
   */
  async comments(channel: string): Promise<PlanComments> {
    const held = this.store.get(channel);
    if (!held || !this.may(channel)) return NONE;
    let read: { comments?: CanvasComment[] };
    try {
      read = await this.slack.call("agents.conversations.getCanvas", {
        channel,
        canvas_id: held.canvas,
        include_resolved: false,
      });
    } catch (e) {
      if (e instanceof SlackError && LOST.test(e.code)) this.store.delete(channel);
      else this.refused(channel, "comments", e);
      return NONE;
    }
    const open = Array.isArray(read.comments) ? read.comments : [];
    const remarks = unseenComments(open, new Set(held.seen), this.opts.botUser);
    if (remarks.length === 0) return NONE;
    const users = remarks.flatMap((r) => [r.comment, ...r.replies.map((x) => x.reply)]);
    const names = this.opts.names
      ? await this.opts.names.of(users.map((c) => c.user_id ?? "")).catch(() => new Map())
      : new Map<string, string>();
    const built = withComments("", remarks, names);
    if (built.shown.length === 0) return NONE;
    const block = built.text;
    return {
      count: built.shown.length,
      with: (text) => `${block}${text}`,
      delivered: () => {
        const now = this.store.get(channel);
        // A canvas made since belongs to another plan: its comments start over.
        if (!now || now.canvas !== held.canvas) return;
        // Only comments still open are remembered, so the record stays small.
        const still = new Set<string>();
        for (const c of open) {
          const key = keyOf(c);
          still.add(key);
          for (const r of c.replies ?? []) still.add(keyOf(r, key));
        }
        const seen = [...new Set([...now.seen, ...built.shown])].filter((k) => still.has(k));
        this.store.set(channel, { canvas: now.canvas, seen });
      },
    };
  }
}
