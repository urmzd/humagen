import { type Citation, encodeAction, formatBytes, type Tone } from "@agent-ui/core";
import type { PrimitiveName, PrimitiveProps, TaskProps } from "@agent-ui/primitives";
import { toMrkdwn, toPlain } from "./mrkdwn";
import type { HostChild, HostNode } from "./reconciler";
import { nextBreak } from "./split";
import { slackTaskNote, slackTaskStatus } from "./status";

/** Block Kit JSON. Kept loose: Slack's schema moves faster than any typing of it. */
export type Block = Record<string, unknown> & { type: string };

export type Placement = "message" | "modal" | "home";

/** Documented Block Kit limits (docs.slack.dev/reference/block-kit). */
export const LIMITS = {
  blocks: { message: 50, modal: 100, home: 100 },
  header: 150,
  sectionText: 3000,
  field: 2000,
  fields: 10,
  contextElements: 10,
  actionsElements: 25,
  buttonText: 75,
  actionId: 255,
  markdownTotal: 12000,
  alertText: 200,
  planTasks: 50,
  tableRows: 100,
  tableCols: 20,
  fallbackText: 4000,
  blockId: 255,
  terminalLines: 40,
  terminalChars: 2500,
  taskText: 256,
} as const;

export interface SerializeResult {
  /** The first message. Complete on its own unless `pages` has more than one entry. */
  blocks: Block[];
  /**
   * Everything, split into messages Slack accepts. `pages[0]` is `blocks`; post the
   * rest as follow-ups in the same thread, in order. Modals and Home have one page.
   */
  pages: Block[][];
  /** Top-level fallback text: notifications and screen readers read this, not blocks. */
  text: string;
  /** Limits hit and degradations applied. Empty when the output is lossless. */
  warnings: string[];
}

const TONE_PREFIX: Record<Tone, string> = {
  ok: ":white_check_mark:",
  warn: ":warning:",
  bad: ":x:",
  info: ":information_source:",
  neutral: "",
};

const ALERT_LEVEL: Record<Tone, string> = {
  ok: "success",
  warn: "warning",
  bad: "error",
  info: "info",
  neutral: "default",
};

class Serializer {
  blocks: Block[] = [];
  fallback: string[] = [];
  warnings: string[] = [];
  markdownUsed = 0;
  tables = 0;
  summary: string | null = null;
  /** Block indexes where a new message must start (the markdown budget ran out). */
  breaks = new Set<number>();

  constructor(readonly placement: Placement) {}

  clip(text: string, max: number, what: string): string {
    if (text.length <= max) return text;
    this.warnings.push(`${what} clipped from ${text.length} to ${max} chars`);
    return `${text.slice(0, max - 1)}…`;
  }

  plain(text: string, max: number, what: string) {
    return { type: "plain_text", text: this.clip(text, max, what), emoji: true };
  }

  mrkdwn(text: string, max: number, what: string) {
    return { type: "mrkdwn", text: this.clip(toMrkdwn(text), max, what) };
  }

  /** Button action_ids must be unique per block; suffix the shared id with its index. */
  actionId(base: string, index: number) {
    return this.clip(`${base}#${index}`, LIMITS.actionId, "action_id");
  }

  richText(text: string): Block {
    return {
      type: "rich_text",
      elements: [{ type: "rich_text_section", elements: [{ type: "text", text }] }],
    };
  }

  task(t: TaskProps, inPlan = false) {
    const note = slackTaskNote(t.status);
    const output = [note, t.output].filter(Boolean).join(" · ") || undefined;
    return {
      task_id: t.id,
      title: this.clip(t.title, 256, "task title"),
      status: slackTaskStatus(t.status, inPlan),
      ...(t.details
        ? { details: this.richText(this.clip(t.details, LIMITS.taskText, "task details")) }
        : {}),
      ...(output
        ? { output: this.richText(this.clip(output, LIMITS.taskText, "task output")) }
        : {}),
      ...(t.sources?.length
        ? {
            sources: t.sources
              .filter((s) => s.url)
              .map((s) => ({ type: "url", url: s.url, text: s.title })),
          }
        : {}),
    };
  }

  citationLine(c: Citation, n: number): string {
    const title = c.url ? `<${c.url}|${c.title}>` : c.title;
    return `[${n}] ${title}${c.source ? ` · ${c.source}` : ""}`;
  }

  node(node: HostNode) {
    const name = node.type.replace(/^aui:/, "") as PrimitiveName;
    const p = node.props as never;
    switch (name) {
      case "Header":
        return this.header(p);
      case "Text":
        return this.text(p);
      case "Markdown":
        return this.markdown(p);
      case "Fields":
        return this.fields(p);
      case "Context":
        return this.context(p);
      case "Divider":
        return this.blocks.push({ type: "divider" });
      case "Code":
        return this.code(p);
      case "Callout":
        return this.callout(p);
      case "Actions":
        return this.actions(node);
      case "Menu":
        return this.actions({ ...node, props: {}, children: [node] });
      case "Button":
        return this.actions({ ...node, children: [node] });
      case "Task":
        return this.blocks.push({ type: "task_card", ...this.task(p) });
      case "Plan":
        return this.plan(node);
      case "Sources":
        return this.sources(p);
      case "Feedback":
        return this.feedback(p);
      case "Table":
        return this.table(p);
      case "Terminal":
        return this.terminal(p);
      case "Checkboxes":
        return this.checkboxes(p);
      case "File":
        return this.file(p);
      case "Summary":
        // First summary wins: the outermost component states the point.
        this.summary ??= toPlain((p as PrimitiveProps["Summary"]).text);
        return;
      default:
        this.warnings.push(`unknown host node ${node.type} dropped`);
    }
  }

  header({ text }: PrimitiveProps["Header"]) {
    this.fallback.push(text);
    this.blocks.push({ type: "header", text: this.plain(text, LIMITS.header, "header") });
  }

  text({ text }: PrimitiveProps["Text"]) {
    this.fallback.push(toPlain(text));
    this.blocks.push({
      type: "section",
      text: this.mrkdwn(text, LIMITS.sectionText, "section text"),
    });
  }

  markdown({ text }: PrimitiveProps["Markdown"]) {
    if (this.placement !== "message") {
      // markdown blocks are message-only; degrade to section mrkdwn elsewhere.
      return this.text({ text });
    }
    this.fallback.push(text);
    // Markdown shares 12k per message: what doesn't fit continues in the next one.
    let rest = text;
    for (;;) {
      const left = LIMITS.markdownTotal - this.markdownUsed;
      // Too little room for more than a fragment: the whole text moves on.
      const cut =
        left < LIMITS.markdownTotal / 4 && rest.length > left
          ? { head: "", tail: rest }
          : nextBreak(rest, left);
      if (!cut) break;
      if (cut.head.trim()) this.blocks.push({ type: "markdown", text: cut.head });
      rest = cut.tail;
      this.breaks.add(this.blocks.length);
      this.markdownUsed = 0;
    }
    this.markdownUsed += rest.length;
    if (rest.trim()) this.blocks.push({ type: "markdown", text: rest });
  }

  fields({ items }: PrimitiveProps["Fields"]) {
    for (let i = 0; i < items.length; i += LIMITS.fields) {
      this.blocks.push({
        type: "section",
        fields: items
          .slice(i, i + LIMITS.fields)
          .map((f) => this.mrkdwn(`**${f.label}**\n${f.value}`, LIMITS.field, "field")),
      });
    }
  }

  context({ items }: PrimitiveProps["Context"]) {
    for (let i = 0; i < items.length; i += LIMITS.contextElements) {
      this.blocks.push({
        type: "context",
        elements: items
          .slice(i, i + LIMITS.contextElements)
          .map((t) => this.mrkdwn(t, LIMITS.sectionText, "context")),
      });
    }
  }

  terminal({ command, cwd, status, exitCode, durationMs, output }: PrimitiveProps["Terminal"]) {
    const state =
      status === "in_progress"
        ? "Running"
        : status === "pending"
          ? "Queued"
          : exitCode === undefined
            ? status === "error"
              ? "Failed"
              : "Done"
            : exitCode === 0
              ? "Passed"
              : "Failed";
    const meta = [
      `*${state}*`,
      exitCode !== undefined ? `exit ${exitCode}` : null,
      durationMs !== undefined ? `${(durationMs / 1000).toFixed(1)}s` : null,
      cwd ? `\`${cwd}\`` : null,
    ].filter(Boolean);
    this.fallback.push(`$ ${command} (${state.toLowerCase()})`);
    this.blocks.push({
      type: "context",
      elements: [{ type: "mrkdwn", text: meta.join(" · ") }],
    });
    // Coding agents need the output, but only the end of it: keep the tail and say what was cut.
    const lines = (output ?? "").split("\n");
    let tail = lines.slice(-LIMITS.terminalLines).join("\n");
    if (tail.length > LIMITS.terminalChars) tail = tail.slice(-LIMITS.terminalChars);
    const cut = lines.length - tail.split("\n").length;
    const body = [`$ ${command}`, cut > 0 ? `… ${cut} earlier lines` : null, tail || null]
      .filter((x) => x !== null)
      .join("\n");
    this.blocks.push(this.preformatted(body));
  }

  /** Checkboxes live in an actions block; the host reads state.values when a button is clicked. */
  checkboxes({ name, options, selected }: PrimitiveProps["Checkboxes"]) {
    const option = (o: (typeof options)[number]) => ({
      text: this.mrkdwn(`**${o.label}**`, 150, "checkbox label"),
      ...(o.description
        ? { description: this.plain(o.description, 150, "checkbox description") }
        : {}),
      value: o.id,
    });
    const enabled = options.filter((o) => !o.disabled).slice(0, 10);
    if (options.length > 10) this.warnings.push(`${options.length} checkboxes; Slack shows 10`);
    const initial = enabled.filter((o) => selected.includes(o.id)).map(option);
    this.blocks.push({
      type: "actions",
      block_id: this.clip(`checkboxes:${name}`, LIMITS.blockId, "block_id"),
      elements: [
        {
          type: "checkboxes",
          action_id: this.clip(`aui.select#${name}`, LIMITS.actionId, "action_id"),
          options: enabled.map(option),
          ...(initial.length ? { initial_options: initial } : {}),
        },
      ],
    });
  }

  /**
   * Ready files become a card (title, type and size, description, Download/Open).
   * Other phases are a single muted line, so a pending file never looks downloadable.
   */
  file(f: PrimitiveProps["File"]) {
    const kind = f.kind.charAt(0).toUpperCase() + f.kind.slice(1);
    const meta = [
      kind,
      f.sizeBytes !== undefined ? formatBytes(f.sizeBytes) : null,
      f.version ? `v${f.version}` : null,
    ]
      .filter(Boolean)
      .join(" · ");
    const name = `\`${f.title}\``;
    const line: Record<Exclude<typeof f.phase, "ready" | "failed">, string> = {
      queued: `Queued: ${name}`,
      generating: `Creating ${name}…`,
      waiting: `Waiting for approval to create ${name}`,
      finalizing: `Finishing ${name}…`,
      denied: `~~${f.title}~~ Not created: approval denied`,
    };
    if (f.phase === "failed") {
      this.fallback.push(`Couldn't create ${f.title}`);
      this.blocks.push({
        type: "section",
        text: this.mrkdwn(
          `:x: **Couldn't create ${f.title}**${f.error ? `\n${f.error}` : ""}`,
          LIMITS.sectionText,
          "file error",
        ),
      });
      return;
    }
    if (f.phase !== "ready") {
      this.blocks.push({
        type: "context",
        elements: [{ type: "mrkdwn", text: toMrkdwn(line[f.phase]) }],
      });
      return;
    }
    this.fallback.push(`${f.title} (${meta})`);
    const actions = [
      f.downloadUrl ? { label: "Download", url: f.downloadUrl } : null,
      f.url ? { label: "Open", url: f.url } : null,
    ].filter((a): a is { label: string; url: string } => a !== null);
    this.blocks.push({
      type: "card",
      title: this.mrkdwn(`**${f.title}**`, 150, "card title"),
      subtitle: this.mrkdwn(meta, 150, "card subtitle"),
      ...(f.description || f.previewText
        ? { body: this.mrkdwn(f.description ?? f.previewText ?? "", 200, "card body") }
        : {}),
      ...(actions.length
        ? {
            actions: actions.map((a, i) => ({
              type: "button",
              text: this.plain(a.label, LIMITS.buttonText, "button label"),
              url: a.url,
              action_id: this.actionId(`aui.file.${a.label.toLowerCase()}`, i),
              accessibility_label: `${a.label} ${f.title}`,
            })),
          }
        : {}),
    });
  }

  preformatted(text: string): Block {
    return {
      type: "rich_text",
      elements: [
        {
          type: "rich_text_preformatted",
          elements: [{ type: "text", text: this.clip(text, LIMITS.sectionText, "code") }],
        },
      ],
    };
  }

  code({ text, title }: PrimitiveProps["Code"]) {
    if (title) {
      this.blocks.push({ type: "context", elements: [{ type: "mrkdwn", text: `\`${title}\`` }] });
    }
    this.blocks.push(this.preformatted(text));
  }

  callout({ tone, title, text }: PrimitiveProps["Callout"]) {
    const line = title ? `${title}: ${text}` : text;
    this.fallback.push(line);
    // alert blocks exist only in modals; messages and Home get a framed section.
    if (
      this.placement === "modal" &&
      toMrkdwn(title ? `**${title}**: ${text}` : text).length <= LIMITS.alertText
    ) {
      this.blocks.push({
        type: "alert",
        text: this.mrkdwn(title ? `**${title}**: ${text}` : text, LIMITS.alertText, "alert"),
        level: ALERT_LEVEL[tone],
      });
      return;
    }
    const prefix = TONE_PREFIX[tone];
    const head = title ? `**${title}**\n` : "";
    this.blocks.push({
      type: "section",
      text: this.mrkdwn(
        `${prefix ? `${prefix} ` : ""}${head}${text}`,
        LIMITS.sectionText,
        "callout",
      ),
    });
  }

  button(p: PrimitiveProps["Button"], index: number): Block {
    return {
      type: "button",
      text: this.plain(p.label, LIMITS.buttonText, "button label"),
      ...(p.action
        ? { action_id: this.actionId(p.action.id, index), value: encodeAction(p.action.payload) }
        : { action_id: this.actionId("aui.link", index) }),
      ...(p.url ? { url: p.url } : {}),
      ...(p.style ? { style: p.style } : {}),
      ...(p.accessibilityLabel ? { accessibility_label: p.accessibilityLabel } : {}),
      ...(p.confirm
        ? {
            confirm: {
              title: this.plain(p.confirm.title, 100, "confirm title"),
              text: this.plain(p.confirm.text, 300, "confirm text"),
              confirm: this.plain(p.confirm.confirm, 30, "confirm button"),
              deny: this.plain(p.confirm.deny, 30, "deny button"),
              ...(p.style === "danger" ? { style: "danger" } : {}),
            },
          }
        : {}),
    };
  }

  actions(node: HostNode) {
    const children = flatten(node.children);
    const buttons = children.filter((c) => c.type === "aui:Button");
    const menus = children.filter((c) => c.type === "aui:Menu");
    if (buttons.length > LIMITS.actionsElements) {
      this.warnings.push(
        `${buttons.length} buttons exceed ${LIMITS.actionsElements} per actions block; extras dropped (use a modal with select options)`,
      );
    }
    const { blockId } = node.props as { blockId?: string };
    this.blocks.push({
      type: "actions",
      ...(blockId ? { block_id: this.clip(blockId, LIMITS.blockId, "block_id") } : {}),
      elements: [
        ...buttons
          .slice(0, LIMITS.actionsElements - menus.length)
          .map((b, i) => this.button(b.props as unknown as PrimitiveProps["Button"], i)),
        ...menus.map((m, i) => this.overflow(m.props as unknown as PrimitiveProps["Menu"], i)),
      ],
    });
  }

  /**
   * Slack's `overflow` has one action_id for the whole menu, so each option's
   * value carries its own agent-ui action. Decode with `decodeSlackAction`.
   */
  overflow({ items }: PrimitiveProps["Menu"], index: number): Block {
    if (items.length > 5)
      this.warnings.push(`menu has ${items.length} items; Slack overflow shows 5`);
    return {
      type: "overflow",
      action_id: this.actionId("aui.menu", index),
      options: items.slice(0, 5).map((item) => {
        const value = JSON.stringify({ id: item.action.id, p: item.action.payload });
        if (value.length > 150)
          this.warnings.push(`menu value for "${item.label}" exceeds 150 chars`);
        return {
          text: this.plain(item.label, 75, "menu label"),
          ...(item.description
            ? { description: this.plain(item.description, 75, "menu description") }
            : {}),
          value: value.slice(0, 150),
        };
      }),
    };
  }

  plan(node: HostNode) {
    const { title } = node.props as unknown as Omit<PrimitiveProps["Plan"], "children">;
    const tasks = flatten(node.children)
      .filter((c) => c.type === "aui:Task")
      .map((c) => this.task(c.props as unknown as TaskProps, true));
    if (tasks.length > LIMITS.planTasks) {
      this.warnings.push(`plan has ${tasks.length} tasks; Slack shows ${LIMITS.planTasks}`);
    }
    this.blocks.push({
      type: "plan",
      title: this.clip(title, 256, "plan title"),
      tasks: tasks.slice(0, LIMITS.planTasks),
    });
  }

  sources({ items }: PrimitiveProps["Sources"]) {
    const lines = items.map((c, i) => this.citationLine(c, i + 1));
    for (let i = 0; i < lines.length; i += LIMITS.contextElements) {
      this.blocks.push({
        type: "context",
        elements: lines
          .slice(i, i + LIMITS.contextElements)
          .map((t) => ({ type: "mrkdwn", text: t })),
      });
    }
  }

  feedback({ up, down, labels, blockId }: PrimitiveProps["Feedback"]) {
    this.blocks.push({
      type: "context_actions",
      ...(blockId ? { block_id: this.clip(blockId, LIMITS.blockId, "block_id") } : {}),
      elements: [
        {
          type: "feedback_buttons",
          action_id: this.actionId(up.id, 0),
          positive_button: {
            text: this.plain(labels?.up ?? "Helpful", LIMITS.buttonText, "feedback label"),
            value: encodeAction(up.payload),
          },
          negative_button: {
            text: this.plain(labels?.down ?? "Not helpful", LIMITS.buttonText, "feedback label"),
            value: encodeAction(down.payload),
          },
        },
      ],
    });
  }

  table({ columns, rows }: PrimitiveProps["Table"]) {
    this.tables += 1;
    if (this.tables > 1 || this.placement === "modal") {
      // One table per message; extras degrade to preformatted text.
      this.warnings.push("extra table rendered as preformatted text");
      const head = columns.map((c) => c.label).join(" | ");
      const body = rows.map((r) => columns.map((c) => String(r[c.key] ?? "")).join(" | "));
      return this.code({ text: [head, ...body].join("\n") });
    }
    const cols = columns.slice(0, LIMITS.tableCols);
    const cell = (v: string | number) => ({ type: "raw_text", text: String(v) });
    if (rows.length + 1 > LIMITS.tableRows) {
      this.warnings.push(`table has ${rows.length} rows; truncated to ${LIMITS.tableRows - 1}`);
    }
    this.blocks.push({
      type: "table",
      column_settings: cols.map((c) => (c.align ? { align: c.align } : null)),
      rows: [
        cols.map((c) => cell(c.label)),
        ...rows.slice(0, LIMITS.tableRows - 1).map((r) => cols.map((c) => cell(r[c.key] ?? ""))),
      ],
    });
  }

  /** Messages can continue in a follow-up, so nothing is dropped: split, and say so. */
  paginate(): Block[][] {
    const max = LIMITS.blocks.message;
    const pages: Block[][] = [[]];
    this.blocks.forEach((block, i) => {
      const page = pages[pages.length - 1] as Block[];
      const last = i === this.blocks.length - 1;
      // Keep one slot free for the continuation note unless this block ends the message.
      const full = page.length >= (last ? max : max - 1);
      if (page.length > 0 && (this.breaks.has(i) || full)) pages.push([block]);
      else page.push(block);
    });
    if (pages.length > 1) {
      this.warnings.push(
        `${this.blocks.length} blocks${this.breaks.size ? " and over 12k of markdown" : ""} exceed ${max} per message; split into ${pages.length} messages (post pages[1..] as follow-ups)`,
      );
    }
    for (const page of pages.slice(0, -1)) {
      page.push({
        type: "context",
        elements: [{ type: "mrkdwn", text: "_Continues in the next message_" }],
      });
    }
    return pages;
  }

  finish(): SerializeResult {
    const max = LIMITS.blocks[this.placement];
    const pages = this.placement === "message" ? this.paginate() : null;
    let blocks = pages?.[0] ?? this.blocks;
    if (blocks.length > max) {
      const dropped = blocks.length - (max - 1);
      this.warnings.push(
        `${blocks.length} blocks exceed ${max}; last ${dropped} replaced by a note`,
      );
      blocks = [
        ...blocks.slice(0, max - 1),
        {
          type: "context",
          elements: [{ type: "mrkdwn", text: `_${dropped} more blocks not shown_` }],
        },
      ];
    }
    const text = (this.summary ?? this.fallback.join("\n\n")).trim();
    return {
      blocks,
      pages: pages ?? [blocks],
      text: text.length > LIMITS.fallbackText ? `${text.slice(0, LIMITS.fallbackText - 1)}…` : text,
      warnings: this.warnings,
    };
  }
}

/** Flatten through any non-primitive wrappers so containers find their children. */
function flatten(children: HostChild[]): HostNode[] {
  return children.flatMap((c) => (c.type === "#text" ? [] : [c as HostNode]));
}

export function serialize(tree: HostChild[], placement: Placement): SerializeResult {
  const s = new Serializer(placement);
  for (const child of tree) {
    if (child.type === "#text") {
      s.warnings.push("bare text outside a primitive dropped; wrap it in <Text>");
      continue;
    }
    s.node(child as HostNode);
  }
  return s.finish();
}
