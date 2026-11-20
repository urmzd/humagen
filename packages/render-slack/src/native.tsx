import {
  ApprovalBatch,
  ApprovalCard,
  ClarificationCard,
  type Copy,
  defaultCopy,
  previewFields,
} from "@humagen/components";
import {
  ActionId,
  type ActionPayload,
  type Approval,
  type Clarification,
  canDecide,
  encodeAction,
  type Tone,
  timeToken,
} from "@humagen/core";
import type { ActionRef, ButtonProps } from "@humagen/primitives";
import type { Block, Placement } from "./blocks";
import { LIMITS } from "./blocks";
import { renderToSlack } from "./index";
import { escapeSlackText, toMrkdwn, toPlain } from "./mrkdwn";

/**
 * Slack's own blocks for what waits on a person: an approval as a `card`,
 * several as a `carousel` of cards, an alert as a `card` (an `alert` block in
 * a modal, the only place Slack draws one) and a question as a `container`.
 *
 * Each builder returns the native blocks and, beside them, the same meaning
 * as the sections and actions render-slack drew before (`fallback`). Slack
 * takes no per-block fallback (`blocks.validate` refuses a `fallback`
 * property on card, carousel and container), so the fallback is posted in
 * place of the native blocks: by the builder when a text is longer than the
 * block allows, and by a host whose post of the native blocks is refused.
 * `text` is always set, since a notification reads only that.
 *
 * Action ids and values are the ones the shared components send, so a click
 * on a card is handled where a click on the sections was.
 */
export interface NativeBlocks {
  /** What to post: the native blocks, or `fallback` when they would not hold the words. */
  blocks: Block[];
  /** The same content as sections and actions, for a client or call that refuses the native block. */
  fallback: Block[];
  /** Top-level message text. Never empty. */
  text: string;
  /** False when `blocks` is the fallback. */
  native: boolean;
  /** Why the fallback was used, when it was. */
  warnings: string[];
}

/** Documented limits of the native blocks (docs.slack.dev/reference/block-kit/blocks). */
export const NATIVE_LIMITS = {
  cardTitle: 150,
  cardSubtitle: 150,
  cardBody: 200,
  cardSubtext: 200,
  cardButtons: 3,
  carouselCards: 10,
  containerTitle: 150,
  containerChildren: 10,
  inputLabel: 2000,
  placeholder: 150,
} as const;

type Text = { type: "mrkdwn"; text: string } | { type: "plain_text"; text: string; emoji: true };

const mrkdwn = (text: string): Text => ({ type: "mrkdwn", text: toMrkdwn(text) });
const plain = (text: string): Text => ({ type: "plain_text", text: toPlain(text), emoji: true });

const TONE_EMOJI: Record<Tone, string> = {
  ok: ":white_check_mark:",
  warn: ":warning:",
  bad: ":x:",
  info: ":information_source:",
  neutral: "",
};

/** One button as render-slack draws it in an actions block: `<ActionId>#<index>`, the payload as its value. */
function button(b: ButtonProps, index: number): Block {
  return {
    type: "button",
    text: { type: "plain_text", text: b.label.slice(0, LIMITS.buttonText), emoji: true },
    action_id: b.action ? `${b.action.id}#${index}` : `aui.link#${index}`,
    ...(b.action ? { value: encodeAction(b.action.payload) } : {}),
    ...(b.url ? { url: b.url } : {}),
    ...(b.style ? { style: b.style } : {}),
    ...(b.accessibilityLabel
      ? { accessibility_label: b.accessibilityLabel.slice(0, LIMITS.accessibilityLabel) }
      : {}),
    ...(b.confirm
      ? {
          confirm: {
            title: { type: "plain_text", text: b.confirm.title },
            text: { type: "plain_text", text: b.confirm.text },
            confirm: { type: "plain_text", text: b.confirm.confirm },
            deny: { type: "plain_text", text: b.confirm.deny },
            // The one confirm step there is guards what can't be undone.
            style: "danger",
          },
        }
      : {}),
  };
}

/** The words of a card, before they are measured against its limits. */
interface CardWords {
  blockId: string;
  title: string;
  subtitle?: string;
  body?: string;
  subtext?: string;
  buttons: ButtonProps[];
}

/**
 * A card, or why it can't be one. A text over its limit is never clipped:
 * the caller posts the fallback instead, so no meaning is cut.
 */
function card(w: CardWords, firstIndex = 0): { block: Block } | { refused: string } {
  const title = mrkdwn(`**${w.title}**`);
  const parts: [string, Text | undefined, number][] = [
    ["title", title, NATIVE_LIMITS.cardTitle],
    ["subtitle", w.subtitle ? mrkdwn(w.subtitle) : undefined, NATIVE_LIMITS.cardSubtitle],
    ["body", w.body ? mrkdwn(w.body) : undefined, NATIVE_LIMITS.cardBody],
    ["subtext", w.subtext ? mrkdwn(w.subtext) : undefined, NATIVE_LIMITS.cardSubtext],
  ];
  for (const [name, text, max] of parts) {
    if (text && text.text.length > max) {
      return { refused: `card ${name} is ${text.text.length} chars; a card holds ${max}` };
    }
  }
  if (w.buttons.length > NATIVE_LIMITS.cardButtons) {
    return {
      refused: `card has ${w.buttons.length} buttons; a card holds ${NATIVE_LIMITS.cardButtons}`,
    };
  }
  const block: Block = { type: "card", block_id: w.blockId.slice(0, LIMITS.blockId) };
  for (const [name, text] of parts) if (text) block[name] = text;
  if (w.buttons.length) block.actions = w.buttons.map((b, i) => button(b, firstIndex + i));
  return { block };
}

/** Where an approval was asked, and why a person is asked it. */
export interface ApprovalContext {
  /** The run or message the approval is on: the `turnId` of its actions. */
  turnId: string;
  /** The agent that wants to run it. */
  agent?: string;
  /** The session's title. */
  session?: string;
  /** When it was asked, ISO 8601. */
  at?: string;
  /** The approval agent's reason, when it handed the decision to a person. */
  reason?: string;
  /** The approval agent's name, when it is not `agent`. */
  reasonBy?: string;
  /** Who is looking. Without one, anyone may decide. */
  viewer?: string;
  placement?: Placement;
  copy?: Copy;
}

/** An approval's card: the words, the buttons the card holds, and any that do not fit on it. */
function approvalWords(a: Approval, o: ApprovalContext): CardWords & { more: ButtonProps[] } {
  const copy = o.copy ?? defaultCopy;
  const payload: ActionPayload = { turnId: o.turnId, target: a.id };
  const what = previewFields(a, copy)
    .map((f) => `${f.label}: ${f.value}`)
    .join("\n");
  const handed = o.reason
    ? o.reasonBy || o.agent
      ? copy.slackBlocks.handedOver((o.reasonBy || o.agent) as string, o.reason)
      : o.reason
    : "";
  const base = {
    blockId: `approval:${a.id}:r${a.revision ?? 0}`,
    title: a.summary,
    subtitle: [o.agent, o.session, o.at ? timeToken(o.at) : null].filter(Boolean).join(" · "),
    body: [what, handed].filter(Boolean).join("\n"),
    buttons: [] as ButtonProps[],
    more: [] as ButtonProps[],
  };
  const { decision } = a;
  const said = (line: string, reason?: string) => (reason ? `${line}: "${reason}"` : line);
  if (decision.kind === "asked") {
    return {
      ...base,
      subtext: said(copy.askedBy(decision.by, timeToken(decision.at)), decision.question),
    };
  }
  if (decision.kind === "approved") {
    return {
      ...base,
      subtext: said(copy.approvedBy(decision.by, timeToken(decision.at)), decision.reason),
    };
  }
  if (decision.kind !== "pending") {
    const outcome = a.budget
      ? copy.budgetHold.stopped
      : decision.kind === "denied"
        ? copy.rejected
        : decision.kind === "expired"
          ? copy.timedOut
          : copy.cancelled;
    return {
      ...base,
      body: outcome,
      ...(decision.kind === "denied"
        ? {
            subtext: said(
              (a.budget ? copy.budgetHold.stoppedBy : copy.rejectedBy)(
                decision.by,
                timeToken(decision.at),
              ),
              decision.reason,
            ),
          }
        : {}),
    };
  }
  const ask: ButtonProps = { label: copy.askApproval, action: { id: ActionId.ask, payload } };
  if (o.viewer !== undefined && !canDecide(a, o.viewer)) {
    return {
      ...base,
      subtext: [copy.notYours, ...(a.actsForPerson ? [copy.actsForPerson] : [])].join(" "),
      buttons: [ask],
    };
  }
  const approveLabel = a.budget
    ? copy.budgetHold.grant(a.budget.grant)
    : (a.approveLabel ?? copy.approve);
  const denyLabel = a.budget ? copy.budgetHold.stop : copy.declineAction;
  // Deny first, then the one way forward, as Block Kit Builder's approval template has them.
  const choices: ButtonProps[] = [
    {
      label: denyLabel,
      style: "danger",
      action: { id: ActionId.deny, payload },
      ...(a.budget ? {} : { accessibilityLabel: `${denyLabel}: ${a.summary}` }),
    },
    {
      label: approveLabel,
      style: "primary",
      action: { id: ActionId.approve, payload },
      ...(a.budget ? {} : { accessibilityLabel: `${approveLabel}: ${a.summary}` }),
      ...(a.destructive ? { confirm: copy.confirmDestructive } : {}),
    },
    ...(a.budget ? [] : [ask]),
    ...(a.editable && !a.actsForPerson
      ? [{ label: copy.edit, action: { id: ActionId.edit, payload } }]
      : []),
    ...(a.rememberable
      ? [{ label: copy.alwaysAllow(a.tool), action: { id: ActionId.remember, payload } }]
      : []),
  ];
  const meta = [
    a.actingAs ? copy.actingAs(a.actingAs) : null,
    a.actsForPerson ? copy.actsForPerson : null,
    a.expiresAt ? copy.expires(timeToken(a.expiresAt)) : null,
  ].filter((x): x is string => x !== null);
  return {
    ...base,
    ...(meta.length ? { subtext: meta.join(" | ") } : {}),
    buttons: choices.slice(0, NATIVE_LIMITS.cardButtons),
    more: choices.slice(NATIVE_LIMITS.cardButtons),
  };
}

/**
 * The result. `text` is raw words, escaped here; without it the fallback's
 * text, which renderToSlack escaped already.
 */
function finish(
  native: Block[] | null,
  fallback: { blocks: Block[]; text: string },
  text: string | null,
  warnings: string[],
): NativeBlocks {
  const said = text?.trim() ? escapeSlackText(text.trim()) : fallback.text;
  return {
    blocks: native ?? fallback.blocks,
    fallback: fallback.blocks,
    text: said.length > LIMITS.fallbackText ? `${said.slice(0, LIMITS.fallbackText - 1)}…` : said,
    native: native !== null,
    warnings,
  };
}

/**
 * One approval as a card. Pending: Deny (danger), Approve (primary) and Ask on
 * the card; a choice past the card's three (Edit, Always Allow) in an actions
 * row under it. Decided: the same card in place, with no buttons and the
 * record as its subtext ("Approved by Dana | ...").
 */
export function approvalCard(approval: Approval, o: ApprovalContext): NativeBlocks {
  const placement = o.placement ?? "message";
  const fallback = renderToSlack(
    <ApprovalCard turnId={o.turnId} approval={approval} viewer={o.viewer} copy={o.copy} />,
    { placement },
  );
  const words = approvalWords(approval, o);
  const made = card(words);
  if ("refused" in made) return finish(null, fallback, null, [made.refused]);
  const blocks = [made.block];
  if (words.more.length) {
    blocks.push({
      type: "actions",
      block_id: `${words.blockId}:more`.slice(0, LIMITS.blockId),
      elements: words.more.map((b, i) => button(b, words.buttons.length + i)),
    });
  }
  const copy = o.copy ?? defaultCopy;
  const pending = approval.decision.kind === "pending";
  const text = pending
    ? [copy.approvalNeeded(toPlain(approval.summary)), toPlain(words.subtitle ?? "")]
        .filter(Boolean)
        .join("\n")
    : [toPlain(approval.summary), toPlain(words.subtext ?? words.body ?? "")]
        .filter(Boolean)
        .join("\n");
  return finish(blocks, fallback, text, []);
}

/**
 * Several approvals at once as a carousel of their cards, one line above it
 * ("lead wants to run 2 actions") and Approve All and Cancel All under it.
 * More than ten, a card that cannot hold its words or its choices, or a modal
 * (which draws no carousel): the checkbox layout render-slack drew before.
 */
export function approvalCarousel(
  approvals: Approval[],
  o: ApprovalContext & { agent?: string },
): NativeBlocks {
  const copy = o.copy ?? defaultCopy;
  const placement = o.placement ?? "message";
  if (approvals.length === 1) return approvalCard(approvals[0] as Approval, o);
  const fallback = renderToSlack(
    <ApprovalBatch turnId={o.turnId} approvals={approvals} copy={o.copy} />,
    { placement },
  );
  const refuse = (why: string) => finish(null, fallback, null, [why]);
  if (placement === "modal") return refuse("a modal draws no carousel");
  if (approvals.length > NATIVE_LIMITS.carouselCards) {
    return refuse(`${approvals.length} approvals; a carousel holds ${NATIVE_LIMITS.carouselCards}`);
  }
  const cards: Block[] = [];
  let index = 0;
  for (const a of approvals) {
    const words = approvalWords(a, o);
    if (words.more.length) return refuse(`${a.id} offers more choices than a card holds`);
    const made = card(words, index);
    if ("refused" in made) return refuse(`${a.id}: ${made.refused}`);
    index += words.buttons.length;
    cards.push(made.block);
  }
  const line = o.agent
    ? copy.slackBlocks.batch(o.agent, approvals.length)
    : copy.batchTitle(approvals.length);
  const target = `batch:${o.turnId}`;
  const pending = approvals.filter((a) => a.decision.kind === "pending").length;
  const blocks: Block[] = [
    { type: "section", text: mrkdwn(`**${line}**`) },
    { type: "carousel", block_id: `${target}:cards`.slice(0, LIMITS.blockId), elements: cards },
  ];
  if (pending > 1) {
    blocks.push({
      type: "actions",
      block_id: `${target}:actions`.slice(0, LIMITS.blockId),
      elements: [
        button(
          {
            label: copy.approveAll(pending),
            action: { id: ActionId.approveAll, payload: { turnId: o.turnId, target } },
          },
          0,
        ),
        button(
          {
            label: copy.denyAll,
            action: { id: ActionId.denyAll, payload: { turnId: o.turnId, target } },
          },
          1,
        ),
      ],
    });
  }
  const text = [
    pending ? copy.approvalNeeded(line) : line,
    ...approvals.map((a) => `• ${toPlain(a.summary)}`),
  ].join("\n");
  return finish(blocks, fallback, text, []);
}

/** An alert as the platform sends it in an inbox part (`part.alert`). */
export interface AlertInput {
  id: string;
  title: string;
  body?: string;
  /** What raised it: "failure_rate", "knowledge". */
  kind?: string;
  /** "critical", "warning" or "info". */
  severity?: string;
  /** "urgent" or "normal". */
  priority?: string;
  /** "open", or "resolved" once someone resolved it. */
  state?: string;
  /** The agent it is about. */
  agent?: string;
  /** Where the console shows it, a path: "/knowledge/hr-policies". */
  action?: string;
}

export interface AlertOptions {
  placement?: Placement;
  /** The console's origin, so `action` becomes a link: "https://platform.example.com". */
  consoleUrl?: string;
  /** Open Session: a link to the session, or the action the host takes for it. */
  session?: string | ActionRef;
  /** What Resolve sends, usually `partAction(message, part, ActionId.acknowledge)`. Leave it out to offer none. */
  resolve?: ActionRef;
  /** Who resolved it, for a resolved alert. */
  resolvedBy?: { by: string; at: string };
  copy?: Copy;
}

/** How bad an alert is, as one of Slack's alert levels. */
export function alertLevel(a: Pick<AlertInput, "priority" | "severity" | "state">) {
  if (a.state === "resolved") return "success" as const;
  if (a.priority === "urgent") return "error" as const;
  return a.severity === "warning" || a.severity === "critical"
    ? ("warning" as const)
    : ("info" as const);
}

const LEVEL_TONE = { success: "ok", error: "bad", warning: "warn", info: "info" } as const;

/**
 * An alert in a message or on Home as a card: its title, its priority in
 * words, its body, and Open, Open Session and Resolve. Slack draws an `alert`
 * block only in a modal, so a modal gets that, with the buttons under it.
 */
export function alertCard(alert: AlertInput, o: AlertOptions = {}): NativeBlocks {
  const copy = o.copy ?? defaultCopy;
  const placement = o.placement ?? "message";
  const level = alertLevel(alert);
  const emoji = TONE_EMOJI[LEVEL_TONE[level]];
  const resolved = level === "success";
  const priority =
    copy.slackBlocks.priority[
      resolved ? "resolved" : alert.priority === "urgent" ? "urgent" : "normal"
    ];
  const subtitle = [`${emoji} ${priority}`, alert.agent].filter(Boolean).join(" · ");
  const buttons: ButtonProps[] = [];
  if (alert.action) {
    const url = linkTo(alert.action, o.consoleUrl);
    buttons.push({
      label: copy.slackBlocks.open,
      accessibilityLabel: `${copy.slackBlocks.open}: ${alert.title}`,
      ...(url
        ? { url }
        : { action: { id: ActionId.open, payload: { turnId: alert.id, target: alert.action } } }),
    });
  }
  if (o.session) {
    buttons.push({
      label: copy.slackBlocks.openSession,
      ...(typeof o.session === "string" ? { url: o.session } : { action: o.session }),
    });
  }
  if (o.resolve && !resolved) {
    buttons.push({ label: copy.slackBlocks.resolve, action: o.resolve });
  }
  const record = o.resolvedBy
    ? copy.slackBlocks.resolvedBy(o.resolvedBy.by, timeToken(o.resolvedBy.at))
    : undefined;
  const blockId = `alert:${alert.id}`.slice(0, LIMITS.blockId);
  const actions = (from: number): Block[] =>
    buttons.length
      ? [
          {
            type: "actions",
            block_id: `${blockId}:actions`,
            elements: buttons.map((b, i) => button(b, from + i)),
          },
        ]
      : [];
  // The same words as sections: what render-slack's Callout drew outside a modal.
  const fallback: Block[] = [
    {
      type: "section",
      block_id: blockId,
      text: mrkdwn(
        `${emoji ? `${emoji} ` : ""}**${alert.title}**${alert.body ? `\n${alert.body}` : ""}`,
      ),
    },
    { type: "context", elements: [mrkdwn([subtitle, record].filter(Boolean).join(" | "))] },
    ...actions(0),
  ];
  const text = [`${priority}: ${toPlain(alert.title)}`, alert.body ? toPlain(alert.body) : ""]
    .filter(Boolean)
    .join("\n");
  const sections = { blocks: fallback, text };
  if (placement === "modal") {
    const said = mrkdwn(`**${alert.title}**${alert.body ? `: ${alert.body}` : ""}`);
    if (said.text.length > LIMITS.alertText) {
      return finish(null, sections, text, [
        `alert text is ${said.text.length} chars; an alert block holds ${LIMITS.alertText}`,
      ]);
    }
    return finish(
      [{ type: "alert", block_id: blockId, text: said, level }, ...actions(0)],
      sections,
      text,
      [],
    );
  }
  const made = card({
    blockId,
    title: alert.title,
    subtitle,
    ...(alert.body ? { body: alert.body } : {}),
    ...(record ? { subtext: record } : {}),
    buttons,
  });
  if ("refused" in made) return finish(null, sections, text, [made.refused]);
  return finish([made.block], sections, text, []);
}

/** A console path as a link Slack takes (absolute http or https), or null without an origin. */
function linkTo(path: string, origin?: string): string | null {
  if (/^https?:\/\//.test(path)) return path;
  if (!origin) return null;
  try {
    return new URL(path, origin).toString();
  } catch {
    return null;
  }
}

/** Where an input's action is kept: its block_id, since Slack sends the typed words as its value. */
export const TEXT_BLOCK = "aui.text:";

/**
 * A question as a container: the question as its title, the options as
 * buttons, and an answer of the reader's own as an input that sends on Enter.
 * The input sends `ActionId.clarifyOther` with the words as `text`
 * (`decodeSlackAction` reads it), as the web's Something Else does. Answered:
 * the question and its record. A modal draws no container: the sections.
 */
export function questionContainer(
  clarification: Clarification,
  o: { turnId: string; placement?: Placement; copy?: Copy },
): NativeBlocks {
  const copy = o.copy ?? defaultCopy;
  const placement = o.placement ?? "message";
  const fallback = renderToSlack(
    <ClarificationCard turnId={o.turnId} clarification={clarification} copy={o.copy} />,
    { placement },
  );
  const refuse = (why: string) => finish(null, fallback, null, [why]);
  if (placement === "modal") return refuse("a modal draws no container");
  const title = plain(clarification.question);
  if (title.text.length > NATIVE_LIMITS.containerTitle) {
    return refuse(
      `container title is ${title.text.length} chars; a container holds ${NATIVE_LIMITS.containerTitle}`,
    );
  }
  const { answer, options } = clarification;
  const blockId = `clarification:${clarification.id}`.slice(0, LIMITS.blockId);
  const context = (line: string): Block => ({ type: "context", elements: [mrkdwn(line)] });
  const payload: ActionPayload = { turnId: o.turnId, target: clarification.id };
  let children: Block[];
  if (answer.kind === "answered") {
    const picked = options.filter((x) => answer.optionIds.includes(x.id)).map((x) => x.label);
    children = [
      context(
        picked.length === 0 && answer.other
          ? copy.repliedBy(answer.by, answer.other, timeToken(answer.at))
          : copy.answeredBy(
              answer.by,
              picked.concat(answer.other ? [`"${answer.other}"`] : []).join(", "),
              timeToken(answer.at),
            ),
      ),
    ];
  } else if (answer.kind === "unsure") {
    children = [context(copy.unsureBy(answer.by, timeToken(answer.at)))];
  } else if (answer.kind === "declined") {
    const line = copy.declinedBy(answer.by, timeToken(answer.at));
    children = [context(answer.reason ? `${line}: "${answer.reason}"` : line)];
  } else if (clarification.forOthers) {
    children = [context(clarification.forOthers)];
  } else {
    const described = options.filter((x) => x.description);
    const textBlock = `${TEXT_BLOCK}${encodeAction(payload)}`;
    // The words ride in the input's block_id; one too long for it asks with a button instead.
    const typed = clarification.allowOther && textBlock.length <= LIMITS.blockId;
    const buttons: ButtonProps[] = [
      ...options.map((x) => ({
        label: x.label,
        action: { id: ActionId.clarify, payload: { ...payload, option: x.id } },
      })),
      ...(clarification.allowOther && !typed
        ? [{ label: copy.somethingElse, action: { id: ActionId.clarifyOther, payload } }]
        : []),
      ...(clarification.allowUnsure
        ? [{ label: copy.unsure, action: { id: ActionId.unsure, payload } }]
        : []),
    ];
    if (buttons.length > LIMITS.actionsElements) {
      return refuse(`${buttons.length} options; an actions block holds ${LIMITS.actionsElements}`);
    }
    children = [
      ...(described.length
        ? [
            {
              type: "section",
              text: mrkdwn(described.map((x) => `• **${x.label}**: ${x.description}`).join("\n")),
            },
          ]
        : []),
      ...(buttons.length
        ? [
            {
              type: "actions",
              block_id: `${blockId}:options`,
              elements: buttons.map((b, i) => button(b, i)),
            },
          ]
        : []),
      ...(typed
        ? [
            {
              type: "input",
              block_id: textBlock,
              dispatch_action: true,
              label: plain(copy.slackBlocks.other),
              element: {
                type: "plain_text_input",
                action_id: `${ActionId.clarifyOther}#0`,
                placeholder: plain(copy.slackBlocks.otherPlaceholder),
                dispatch_action_config: { trigger_actions_on: ["on_enter_pressed"] },
              },
            },
          ]
        : []),
    ];
  }
  const blocks: Block[] = [{ type: "container", block_id: blockId, title, child_blocks: children }];
  return finish(blocks, fallback, null, []);
}
