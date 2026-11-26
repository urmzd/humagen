import {
  ApprovalBatch,
  type ApprovalBatchProps,
  ApprovalCard,
  type ApprovalCardProps,
  ClarificationCard,
  type ClarificationCardProps,
  type Copy,
  defaultCopy,
  previewFields,
  type SystemAlertPartProps,
  untrusted,
} from "@humagen/components";
import {
  ActionId,
  type ActionPayload,
  type Approval,
  type Clarification,
  canDecide,
  encodeAction,
  type Judgement,
  SERVICE_TONE,
  type Tone,
  timeToken,
} from "@humagen/core";
import {
  type ActionRef,
  Actions,
  Button,
  type ButtonProps,
  Context,
  Menu,
  type MenuProps,
  type Override,
  type Renderer,
  useSurface,
} from "@humagen/primitives";
import { createElement, type ReactElement, type ReactNode } from "react";
import type { Block, Placement } from "./blocks";
import { LIMITS, NATIVE, NATIVE_MORE } from "./blocks";
import { renderToSlack } from "./index";
import { escapeSlackText, toMrkdwn, toPlain } from "./mrkdwn";

/**
 * Slack's own blocks for what waits on a person: an approval as a `card`,
 * several as a `carousel` of cards, an alert as a `card` (an `alert` block in
 * a modal, the only place Slack draws one) and a question as a `container`.
 *
 * `slackOverrides` draws the shared ApprovalCard, ApprovalBatch,
 * ClarificationCard and SystemAlertPart this way in a message, so every
 * message `renderToSlack` draws (a turn, an inbox message) carries them.
 * Each keeps the shared drawing beside its blocks. `renderToSlack` returns
 * it as `fallback`, which is posted in place of the native blocks: by the
 * override when a text is longer than the block allows, and by a host whose
 * post of the native blocks Slack refuses (`fallbackOf`). Slack takes no
 * per-block fallback (`blocks.validate` refuses a `fallback` property on
 * card, carousel and container). The message's text is the shared
 * drawing's, since a notification reads only that.
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
  /**
   * Characters of button labels a card shows on desktop without cutting one:
   * measured in Block Kit Builder, where "Roll Back", "Ask a Question" and
   * "Decline" (28) showed "Roll …". Slack documents no width.
   */
  cardButtonChars: 24,
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

const labelChars = (buttons: ButtonProps[]) => buttons.reduce((n, b) => n + b.label.length, 0);

/**
 * A card, or why it can't be one. A text over its limit is never clipped:
 * the shared drawing is posted instead, so no meaning is cut.
 */
function card(w: CardWords, firstIndex = 0): { block: Block } | { refused: string } {
  const parts: [string, Text | undefined, number][] = [
    ["title", mrkdwn(`**${w.title}**`), NATIVE_LIMITS.cardTitle],
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
  const chars = labelChars(w.buttons);
  if (chars > NATIVE_LIMITS.cardButtonChars) {
    return {
      refused: `card buttons need ${chars} chars; a card shows ${NATIVE_LIMITS.cardButtonChars} uncut`,
    };
  }
  const block: Block = { type: "card", block_id: w.blockId.slice(0, LIMITS.blockId) };
  for (const [name, text] of parts) if (text) block[name] = text;
  if (w.buttons.length) block.actions = w.buttons.map((b, i) => button(b, firstIndex + i));
  return { block };
}

/**
 * A shared component drawn as Slack's own `blocks`, with `more` drawn under
 * them from primitives, and the shared drawing (`children`) kept for the
 * fallback and the message's text. Without `blocks` only the shared drawing
 * is drawn, and `warnings` say why.
 */
function Native({
  blocks,
  warnings,
  more,
  children,
}: {
  blocks?: Block[];
  warnings?: string[];
  more?: ReactNode;
  children: ReactNode;
}): ReactElement {
  return createElement(
    NATIVE,
    { blocks, warnings },
    more ? createElement(NATIVE_MORE, null, more) : null,
    children,
  );
}

const withReason = (line: string, reason?: string) => (reason ? `${line}: "${reason}"` : line);

/** The approval agent's view of a call, as the one line Slack's shared drawing has it. */
function judgementLine(j: Judgement, copy: Copy): string {
  const words = copy.judgement;
  const references = j.references ?? [];
  return [
    ...(j.fields ?? []).map((f) => `**${untrusted(f.label)}:** ${untrusted(f.value)}`),
    withReason(`**${words.by}:** ${words.decision[j.decision]}`, untrusted(j.reason)),
    ...(references.length > 0 ? [words.basedOn(references.map(untrusted))] : []),
  ].join(" | ");
}

/**
 * An approval's card: the words, the buttons the card holds, and what does
 * not fit on it (Edit, Always Allow, Set This Thread to Auto), for a row under it.
 */
function approvalWords(
  a: Approval,
  o: ApprovalCardProps,
): CardWords & { more: ButtonProps[]; menu?: MenuProps } {
  const copy = o.copy ?? defaultCopy;
  const payload: ActionPayload = { turnId: o.turnId, target: a.id };
  const what = previewFields(a, copy)
    .map((f) => `${f.label}: ${f.value}`)
    .join("\n");
  const { decision, judgement } = a;
  const judged = judgement ? judgementLine(judgement, copy) : "";
  // When the approval agent decided, the record's note repeats its reason, which the
  // judgement already shows: the record keeps only who and when.
  const noted = (reason?: string) =>
    reason && judgement?.reason && reason.endsWith(judgement.reason) ? undefined : reason;
  // Slack shows a card's title on one line, so it says what kind of card this is;
  // the call itself, which must be read whole, leads the body.
  const titles = copy.slackBlocks.approvalTitle;
  const base = {
    blockId: `approval:${a.id}:r${a.revision ?? 0}`,
    title:
      decision.kind === "pending"
        ? a.budget
          ? titles.limit
          : titles.pending
        : decision.kind === "asked"
          ? titles.asked
          : decision.kind === "approved"
            ? titles.approved
            : titles.declined,
    ...(a.origin ? { subtitle: a.origin } : {}),
    body: [`**${a.summary}**`, what, judged].filter(Boolean).join("\n"),
    buttons: [] as ButtonProps[],
    more: [] as ButtonProps[],
  };
  if (decision.kind === "asked") {
    return {
      ...base,
      subtext: withReason(copy.askedBy(decision.by, timeToken(decision.at)), decision.question),
    };
  }
  if (decision.kind === "approved") {
    return {
      ...base,
      subtext: withReason(
        copy.approvedBy(decision.by, timeToken(decision.at)),
        noted(decision.reason),
      ),
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
      // Kept struck through, then the outcome, as the shared drawing has it.
      body: [`~~${a.summary}~~ ${outcome}`, judged].filter(Boolean).join("\n"),
      ...(decision.kind === "denied"
        ? {
            subtext: withReason(
              (a.budget ? copy.budgetHold.stoppedBy : copy.rejectedBy)(
                decision.by,
                timeToken(decision.at),
              ),
              noted(decision.reason),
            ),
          }
        : {}),
    };
  }
  // Short on the card, so no label is cut; a screen reader says it in full.
  const ask: ButtonProps = {
    label: copy.slackBlocks.ask,
    accessibilityLabel: `${copy.askApproval}: ${a.summary}`,
    action: { id: ActionId.ask, payload },
  };
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
  // The shared drawing's order: the way forward, then Ask, then Decline.
  const decide: ButtonProps[] = [
    {
      label: approveLabel,
      style: a.destructive ? "danger" : "primary",
      action: { id: ActionId.approve, payload },
      // A budget question's summary is long: its buttons say enough alone.
      ...(a.budget ? {} : { accessibilityLabel: `${approveLabel}: ${a.summary}` }),
      ...(a.destructive ? { confirm: copy.confirmDestructive } : {}),
    },
    {
      label: denyLabel,
      action: { id: ActionId.deny, payload },
      ...(a.budget ? {} : { accessibilityLabel: `${denyLabel}: ${a.summary}` }),
    },
  ];
  // Ask sits between them when the labels leave room for it, else in the row under the card.
  const asks = !a.budget && labelChars([...decide, ask]) <= NATIVE_LIMITS.cardButtonChars;
  const choices = asks ? [decide[0] as ButtonProps, ask, decide[1] as ButtonProps] : decide;
  const more: ButtonProps[] = [
    ...(!a.budget && !asks ? [ask] : []),
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
    buttons: choices,
    more,
    // The conversation's mode, not this call: a menu, so it never reads as a decision.
    ...(o.offerAgent && !a.budget
      ? {
          menu: {
            label: copy.moreOptions,
            items: [
              {
                label: copy.approvals.letAgentDecide,
                action: {
                  id: ActionId.approvalMode,
                  payload: { turnId: o.turnId, target: "auto" },
                },
              },
            ],
          },
        }
      : {}),
  };
}

/** True where Slack draws the native blocks: a message. A modal and Home keep the sections. */
function inMessage(): boolean {
  return (useSurface().placement ?? "message") === "message";
}

/**
 * One approval as a card. Pending: Approve (primary, or danger when it
 * can't be undone), Ask and Decline on the card; Edit, Always Allow and the
 * thread's Auto mode in a row under it. Decided: the same card in place, with
 * no buttons and the record as its subtext ("Approved by Dana | ...").
 */
const SlackApprovalCard: Override<ApprovalCardProps> = ({ base: Base, ...props }) => {
  const shared = <Base {...props} />;
  if (!inMessage()) return shared;
  const words = approvalWords(props.approval, props);
  const made = card(words);
  if ("refused" in made) return <Native warnings={[made.refused]}>{shared}</Native>;
  const more =
    words.more.length || words.menu ? (
      <Actions blockId={`${words.blockId}:more`}>
        {words.more.map((b) => (
          <Button key={b.label} {...b} />
        ))}
        {words.menu && <Menu {...words.menu} />}
      </Actions>
    ) : null;
  return (
    <Native blocks={[made.block]} more={more}>
      {shared}
    </Native>
  );
};

/**
 * Several approvals at once: one line, a carousel of their cards, each
 * decided on its own card, then a menu with the thread's Auto mode. More than ten, or a card that cannot hold its
 * words or its choices: the shared checkbox layout.
 */
const SlackApprovalBatch: Override<ApprovalBatchProps> = ({ base: Base, ...props }) => {
  const shared = <Base {...props} />;
  if (!inMessage()) return shared;
  const { approvals, turnId } = props;
  const copy = props.copy ?? defaultCopy;
  const refuse = (why: string) => <Native warnings={[why]}>{shared}</Native>;
  if (approvals.length > NATIVE_LIMITS.carouselCards) {
    return refuse(`${approvals.length} approvals; a carousel holds ${NATIVE_LIMITS.carouselCards}`);
  }
  const cards: Block[] = [];
  let index = 0;
  for (const a of approvals) {
    // The thread's mode is offered once, under the carousel.
    const words = approvalWords(a, {
      turnId,
      approval: a,
      copy,
      ...(props.viewer !== undefined ? { viewer: props.viewer } : {}),
    });
    if (words.more.length) return refuse(`${a.id} offers more choices than a card holds`);
    const made = card(words, index);
    if ("refused" in made) return refuse(`${a.id}: ${made.refused}`);
    index += words.buttons.length;
    cards.push(made.block);
  }
  const name = `batch:${turnId}`;
  const pending = approvals.some((a) => a.decision.kind === "pending");
  const acting = approvals.filter((a) => a.actsForPerson).map((a) => a.summary);
  const auto = props.offerAgent && pending && !approvals.some((a) => a.budget);
  const blocks: Block[] = [
    { type: "section", text: mrkdwn(`**${copy.batchTitle(approvals.length)}**`) },
    { type: "carousel", block_id: `${name}:cards`.slice(0, LIMITS.blockId), elements: cards },
  ];
  // Each card decides its own call. Nothing under the carousel acts on all of
  // them at once: the app has no handler for that yet.
  const more = (
    <>
      {auto && (
        <Actions blockId={`${name}:actions`}>
          <Menu
            label={copy.moreOptions}
            items={[
              {
                label: copy.approvals.letAgentDecide,
                action: { id: ActionId.approvalMode, payload: { turnId, target: "auto" } },
              },
            ]}
          />
        </Actions>
      )}
      {acting.length > 0 && <Context items={[copy.actsForPersonIn(acting)]} />}
    </>
  );
  return (
    <Native blocks={blocks} more={more}>
      {shared}
    </Native>
  );
};

/** Where an input's action is kept: its block_id, since Slack sends the typed words as its value. */
export const TEXT_BLOCK = "aui.text:";

/**
 * A question as a container: the question as its title, the options as
 * buttons, and an answer of the reader's own as an input that sends on Enter.
 * The input sends `ActionId.clarifyOther` with the words as `text`
 * (`decodeSlackAction` reads it), as the web's Something Else does. Settled:
 * the question and its record.
 */
const SlackClarificationCard: Override<ClarificationCardProps> = ({ base: Base, ...props }) => {
  const shared = <Base {...props} />;
  if (!inMessage()) return shared;
  const { clarification, turnId } = props;
  const copy = props.copy ?? defaultCopy;
  const refuse = (why: string) => <Native warnings={[why]}>{shared}</Native>;
  const { answer, options, judgement } = clarification;
  // Slack shows a container's title on one line, so it says what kind of card
  // this is; the question, which must be read whole, is its first block.
  const open = answer.kind === "pending" && !clarification.forOthers;
  const title = plain(copy.slackBlocks.questionTitle[open ? "open" : "settled"]);
  const described = open ? options.filter((x) => x.description) : [];
  const asked: Block = {
    type: "section",
    text: mrkdwn(
      [
        open ? `**${clarification.question}**` : clarification.question,
        ...described.map((x) => `• **${x.label}**: ${x.description}`),
      ].join("\n"),
    ),
  };
  const blockId = `clarification:${clarification.id}`.slice(0, LIMITS.blockId);
  const context = (line: string): Block => ({ type: "context", elements: [mrkdwn(line)] });
  const payload: ActionPayload = { turnId, target: clarification.id };
  // The approval agent's part, in Auto: an answer it gave, or why it left the question.
  const judged = judgement
    ? [
        context(
          withReason(
            judgement.decision === "allow"
              ? copy.judgement.answered
              : `**${copy.judgement.by}:** ${copy.judgement.decision[judgement.decision]}`,
            untrusted(judgement.reason),
          ),
        ),
      ]
    : [];
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
      ...judged,
    ];
  } else if (answer.kind === "unsure") {
    children = [context(copy.unsureBy(answer.by, timeToken(answer.at)))];
  } else if (answer.kind === "declined") {
    children = [
      context(withReason(copy.declinedBy(answer.by, timeToken(answer.at)), answer.reason)),
    ];
  } else if (answer.kind === "expired" || answer.kind === "cancelled") {
    const line =
      answer.kind === "expired"
        ? (answer.note ?? copy.questionExpired)
        : copy.questionCancelled(answer.note);
    children = [context(`${line} | ${timeToken(answer.at)}`)];
  } else if (clarification.forOthers) {
    children = [context(clarification.forOthers)];
  } else {
    const textBlock = `${TEXT_BLOCK}${encodeAction(payload)}`;
    // Beside options, an answer of the reader's own is typed in place. The words
    // ride in the input's block_id; one too long for it asks with a button
    // instead, as does a question with no options, whose form shows the
    // question above a field of several lines.
    const typed =
      clarification.allowOther && options.length > 0 && textBlock.length <= LIMITS.blockId;
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
      ...judged,
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
  return (
    <Native
      blocks={[{ type: "container", block_id: blockId, title, child_blocks: [asked, ...children] }]}
    >
      {shared}
    </Native>
  );
};

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
 * An open incident in a message as a card: the service and its state as its
 * title, its status in words as its subtitle, what is wrong and what people
 * notice as its body, when it started as its subtext, then Acknowledge, Snooze and Open
 * Rule. Resolved, its record is the subtext and the buttons are gone. One
 * whose service is back is the shared one-line record.
 */
const SlackSystemAlertPart: Override<SystemAlertPartProps> = ({ base: Base, ...props }) => {
  const shared = <Base {...props} />;
  const { alert } = props;
  if (!inMessage() || alert.status === "resolved" || alert.status === "operational") return shared;
  const copy = props.copy ?? defaultCopy;
  const status = copy.serviceStatus[alert.status];
  const emoji = TONE_EMOJI[SERVICE_TONE[alert.status]];
  const meta = [
    copy.started(timeToken(alert.startedAt)),
    alert.updatedAt !== alert.startedAt ? copy.updated(timeToken(alert.updatedAt)) : null,
    alert.url ? `[${copy.statusPage}](${alert.url})` : null,
  ].filter((x): x is string => x !== null);
  const all: ButtonProps[] = props.record
    ? []
    : [
        ...(props.acknowledge ? [{ label: copy.acknowledge, action: props.acknowledge }] : []),
        ...(props.snooze ? [{ label: copy.snooze, action: props.snooze }] : []),
        ...(props.ruleUrl ? [{ label: copy.openRule, url: props.ruleUrl }] : []),
      ];
  const body = [alert.body, alert.affects?.length ? copy.affects(alert.affects.join(", ")) : null]
    .filter(Boolean)
    .join(" ");
  // The buttons whose labels fit uncut on the card, in order; the rest in a row under it.
  const fits = all.findIndex(
    (_, i) => labelChars(all.slice(0, i + 1)) > NATIVE_LIMITS.cardButtonChars,
  );
  const buttons = fits < 0 ? all : all.slice(0, fits);
  const rest = fits < 0 ? [] : all.slice(fits);
  // The title is one line: the service and its state. What is wrong leads the body.
  const made = card({
    blockId: props.blockId,
    title: `${alert.service} ${status.toLowerCase()}`,
    subtitle: [emoji, status].filter(Boolean).join(" "),
    body: [`**${alert.title}**`, body].filter(Boolean).join("\n"),
    subtext: props.record ?? meta.join(" | "),
    buttons,
  });
  if ("refused" in made) return <Native warnings={[made.refused]}>{shared}</Native>;
  const more = rest.length ? (
    <Actions blockId={`${props.blockId}:more`}>
      {rest.map((b) => (
        <Button key={b.label} {...b} />
      ))}
    </Actions>
  ) : null;
  return (
    <Native blocks={[made.block]} more={more}>
      {shared}
    </Native>
  );
};

/** The shared components Slack draws as its own blocks, for `renderToSlack`. */
export const slackOverrides: NonNullable<Renderer["overrides"]> = {
  ApprovalCard: SlackApprovalCard,
  ApprovalBatch: SlackApprovalBatch,
  ClarificationCard: SlackClarificationCard,
  SystemAlertPart: SlackSystemAlertPart,
};

/** One shared component as `renderToSlack` posts it: its native blocks, and the sections beside them. */
export function nativeBlocks(element: ReactNode, placement: Placement = "message"): NativeBlocks {
  const out = renderToSlack(element, { placement });
  return {
    blocks: out.blocks,
    fallback: out.fallback?.blocks ?? out.blocks,
    text: out.text,
    native: out.fallback !== undefined,
    warnings: out.warnings,
  };
}

type Where = { placement?: Placement };

/** One approval, as `ApprovalCard` posts it in a message. */
export function approvalCard(
  approval: Approval,
  o: Omit<ApprovalCardProps, "approval"> & Where,
): NativeBlocks {
  const { placement, ...props } = o;
  return nativeBlocks(<ApprovalCard approval={approval} {...props} />, placement);
}

/** Several approvals, as `ApprovalBatch` posts them in a message. */
export function approvalCarousel(
  approvals: Approval[],
  o: Omit<ApprovalBatchProps, "approvals"> & Where,
): NativeBlocks {
  const { placement, ...props } = o;
  return nativeBlocks(<ApprovalBatch approvals={approvals} {...props} />, placement);
}

/** A question, as `ClarificationCard` posts it in a message. */
export function questionContainer(
  clarification: Clarification,
  o: Omit<ClarificationCardProps, "clarification"> & Where,
): NativeBlocks {
  const { placement, ...props } = o;
  return nativeBlocks(<ClarificationCard clarification={clarification} {...props} />, placement);
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
