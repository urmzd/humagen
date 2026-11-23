import {
  ActionId,
  type Approval,
  type Approval as ApprovalT,
  type Attachment,
  budgetPressure,
  type Citation,
  type Clarification,
  canDecide,
  contextPressure,
  type Failure,
  type Field,
  fileEdits,
  formatAmount,
  formatTokens,
  type InboxItem,
  type Interrupt,
  isRetryable,
  type Judgement,
  type Notice,
  paragraphsAtSteps,
  readAgentChange,
  readReview,
  recoveredCalls,
  SERVICE_TONE,
  type Segment,
  type SessionState,
  type SystemAlert,
  type Task as TaskT,
  type Turn,
  timeline,
  timeToken,
} from "@humagen/core";
import {
  type ActionRef,
  Actions,
  Amount,
  Button,
  Callout,
  Checkboxes,
  Code,
  Context,
  Divider,
  Feedback,
  Fields,
  File,
  Header,
  Markdown,
  Menu,
  Only,
  Plan,
  Request,
  Row,
  Sources,
  Summary,
  Task,
  Terminal,
  Text,
  useSurface,
} from "@humagen/primitives";
import type { ReactNode } from "react";
import { AgentChangeCards } from "./agent-change";
import { type Copy, defaultCopy } from "./copy";
import { AnswerText, reviewCountLine } from "./review";
import { toolsLine } from "./timeline";
import { ChangedFiles, turnSettled } from "./turn-changes";
import { FailedCommands, workSummary } from "./work";

interface WithCopy {
  copy?: Copy;
}

/**
 * What the host can act on, so a surface never offers a control that leads
 * nowhere. Leave a key out (or true) to offer it; false hides it on every surface.
 */
export interface TurnCapabilities {
  /** Rating the answer (Helpful / Not helpful) and commenting on it. */
  feedback?: boolean;
  /** Regenerate on an answered turn, where a surface offers it. */
  regenerate?: boolean;
  /** Report Issue on an internal failure. */
  report?: boolean;
  /** Ask Follow-up on a review's findings, which puts words in the host's composer. */
  followUp?: boolean;
  /**
   * Continue in a Code Channel on a finished answer. Unlike the others it is
   * left out unless a host turns it on: only Slack has the place, and only
   * where the workspace opted in.
   */
  codeChannel?: boolean;
  /**
   * Run Details on a finished turn: what the run used, shown only to the
   * person who asks. Unlike the others it is offered only when a host says
   * it can answer (`true`), since usage is never shown on the turn itself.
   */
  runDetails?: boolean;
  /**
   * "Let the approval agent decide in this thread" in a menu on an approval
   * that waits for a person. Offered only when a host says it can (`true`):
   * the console sets the mode with its own toggle under the composer.
   */
  approvalAgent?: boolean;
}

/** Fallback preview when a tool supplies none: scalar args as fields, never a JSON dump. */
function previewFields(approval: Approval, copy: Copy = defaultCopy): Field[] {
  if (approval.budget) {
    const b = approval.budget;
    return [
      { label: copy.budgetHold.limit, value: `${b.max} (${b.scope})` },
      { label: copy.budgetHold.used, value: b.used },
      ...(b.granted ? [{ label: copy.budgetHold.granted, value: b.granted }] : []),
      { label: copy.budgetHold.grants, value: b.grant },
    ];
  }
  if (approval.preview) return approval.preview;
  return Object.entries(approval.args ?? {})
    .filter(([, v]) => ["string", "number", "boolean"].includes(typeof v))
    .map(([label, value]) => ({ label, value: String(value) }));
}

function withReason(line: string, reason?: string) {
  return reason ? `${line}: "${reason}"` : line;
}

/**
 * Words a model wrote, made inert for the Text primitive: a link's markup and a
 * time token are broken with a zero-width space, so they show as typed and
 * nothing in them becomes a link. Each renderer already escapes what would
 * ping on its surface.
 */
export function untrusted(text: string): string {
  return text.replaceAll("](", "]\u200B(").replaceAll("{{", "{\u200B{");
}

/**
 * The approval agent's view of a call: what it decided and why, then each
 * field the workspace's schema adds as a label and its value, then what it
 * rests on. Muted lines, so the call itself stays the point of the card.
 */
function JudgementView({ judgement: j, copy }: { judgement: Judgement; copy: Copy }) {
  const words = copy.judgement;
  const fields = (j.fields ?? []).map((f) => `**${untrusted(f.label)}:** ${untrusted(f.value)}`);
  const references = j.references ?? [];
  // Slack reads it as one line under the call: the workspace's fields first
  // (a risk level), then the decision and its reason, then what it rests on.
  if (useSurface().surface === "slack") {
    return (
      <Context
        items={[
          ...fields,
          withReason(`**${words.by}:** ${words.decision[j.decision]}`, untrusted(j.reason)),
          ...(references.length > 0 ? [words.basedOn(references.map(untrusted))] : []),
        ]}
      />
    );
  }
  return (
    <>
      <Context
        items={[withReason(`**${words.by}:** ${words.decision[j.decision]}`, untrusted(j.reason))]}
      />
      {fields.length > 0 && <Context items={fields} />}
      {references.length > 0 && <Context items={[words.basedOn(references.map(untrusted))]} />}
    </>
  );
}

/**
 * Approval gate. Point first, a preview of exactly what will happen, specific
 * verbs, one emphasized button, and a record in place of the controls once decided.
 */
export function ApprovalCard({
  turnId,
  approval,
  viewer,
  offerAgent = false,
  copy = defaultCopy,
}: {
  turnId: string;
  approval: Approval;
  /** Who is looking. Non-initiators can ask but never approve. */
  viewer?: string;
  /** Offer to let the approval agent decide the conversation's next actions (`TurnCapabilities.approvalAgent`). */
  offerAgent?: boolean;
} & WithCopy) {
  const compactWebDecision = useSurface().surface === "web" && !approval.budget;
  const { decision } = approval;
  const fields = previewFields(approval, copy);
  // A run held at a limit is granted more or stopped, not approved or cancelled.
  const approveLabel = approval.budget
    ? copy.budgetHold.grant(approval.budget.grant)
    : (approval.approveLabel ?? copy.approve);
  const denyLabel = approval.budget ? copy.budgetHold.stop : copy.declineAction;
  const payload = { turnId, target: approval.id };
  const allowed = viewer === undefined || canDecide(approval, viewer);
  const { judgement } = approval;
  const judged = judgement ? <JudgementView judgement={judgement} copy={copy} /> : null;
  // When the approval agent decided, the record's note repeats its reason, which the
  // judgement already shows: the record keeps only who and when.
  const noted = (reason?: string) =>
    reason && judgement?.reason && reason.endsWith(judgement.reason) ? undefined : reason;

  if (decision.kind === "asked") {
    // Asking halts the call; the agent answers, then the gate is raised again.
    return (
      <>
        <Text text={`**${approval.summary}**`} />
        {fields.length > 0 && <Fields items={fields} />}
        {judged}
        <Context
          items={[`${copy.askedBy(decision.by, timeToken(decision.at))}: "${decision.question}"`]}
        />
      </>
    );
  }

  if (decision.kind === "pending" && !allowed) {
    return (
      <>
        <Text text={`**${approval.summary}**`} />
        {fields.length > 0 && <Fields items={fields} />}
        {judged}
        <Actions blockId={`approval:${approval.id}:r${approval.revision ?? 0}`}>
          <Button label={copy.askApproval} action={{ id: ActionId.ask, payload }} />
        </Actions>
        <Context items={[copy.notYours, ...(approval.actsForPerson ? [copy.actsForPerson] : [])]} />
      </>
    );
  }

  if (decision.kind === "pending") {
    const meta = [
      approval.actingAs ? copy.actingAs(approval.actingAs) : null,
      approval.actsForPerson ? copy.actsForPerson : null,
      approval.expiresAt ? copy.expires(timeToken(approval.expiresAt)) : null,
    ].filter((x): x is string => x !== null);
    return (
      <>
        <Summary text={copy.approvalNeeded(approval.summary)} />
        <Text text={`**${approval.summary}**`} />
        {fields.length > 0 && <Fields items={fields} />}
        {judged}
        <Actions blockId={`approval:${approval.id}:r${approval.revision ?? 0}`}>
          <Button
            label={approveLabel}
            style={approval.destructive ? "danger" : "primary"}
            action={{ id: ActionId.approve, payload }}
            // A budget question's summary is long: its buttons say enough alone.
            accessibilityLabel={
              approval.budget ? undefined : `${approveLabel}: ${approval.summary}`
            }
            confirm={approval.destructive ? copy.confirmDestructive : undefined}
          />
          {compactWebDecision && (
            <Button
              label={copy.decline}
              action={{ id: ActionId.deny, payload }}
              accessibilityLabel={`${denyLabel}: ${approval.summary}`}
            />
          )}
          {approval.editable && !approval.actsForPerson && (
            <Button label={copy.edit} action={{ id: ActionId.edit, payload }} />
          )}
          {!approval.budget && (
            <Button label={copy.askApproval} action={{ id: ActionId.ask, payload }} />
          )}
          {approval.rememberable && (
            <Button
              label={copy.alwaysAllow(approval.tool)}
              action={{ id: ActionId.remember, payload }}
            />
          )}
          {!compactWebDecision && (
            <Button
              label={denyLabel}
              action={{ id: ActionId.deny, payload }}
              accessibilityLabel={approval.budget ? undefined : `${denyLabel}: ${approval.summary}`}
            />
          )}
          {/* The conversation's mode, not this call: a menu, so it never reads as a decision. */}
          {offerAgent && !approval.budget && (
            <Menu
              label={copy.moreOptions}
              items={[
                {
                  label: copy.approvals.letAgentDecide,
                  action: { id: ActionId.approvalMode, payload: { turnId, target: "auto" } },
                },
              ]}
            />
          )}
        </Actions>
        {meta.length > 0 && <Context items={meta} />}
      </>
    );
  }

  if (decision.kind === "approved") {
    return (
      <>
        <Text text={`**${approval.summary}**`} />
        {fields.length > 0 && <Fields items={fields} />}
        {judged}
        <Context
          items={[
            withReason(
              copy.approvedBy(decision.by, timeToken(decision.at)),
              noted(decision.reason),
            ),
          ]}
        />
      </>
    );
  }

  // Declined, timed out, or cancelled: keep the preview struck through, then the outcome.
  const outcome = approval.budget
    ? copy.budgetHold.stopped
    : decision.kind === "denied"
      ? copy.rejected
      : decision.kind === "expired"
        ? copy.timedOut
        : copy.cancelled;
  return (
    <>
      <Text text={`~~${approval.summary}~~ ${outcome}`} />
      {judged}
      {decision.kind === "denied" && (
        <Context
          items={[
            withReason(
              (approval.budget ? copy.budgetHold.stoppedBy : copy.rejectedBy)(
                decision.by,
                timeToken(decision.at),
              ),
              noted(decision.reason),
            ),
          ]}
        />
      )}
    </>
  );
}

/**
 * Several gated calls at once: one message, every item listed, all checked by
 * default, one "Approve Selected". Unchecked items are denied, never run.
 */
export function ApprovalBatch({
  turnId,
  approvals,
  offerAgent = false,
  copy = defaultCopy,
}: {
  turnId: string;
  approvals: ApprovalT[];
  /** Offer letting the approval agent decide from here on, as one approval's card does. */
  offerAgent?: boolean;
} & WithCopy) {
  const name = `batch:${turnId}`;
  const acting = approvals.filter((a) => a.actsForPerson).map((a) => a.summary);
  return (
    <>
      <Summary text={copy.approvalNeeded(copy.batchTitle(approvals.length))} />
      <Text text={`**${copy.batchTitle(approvals.length)}**`} />
      <Checkboxes
        name={name}
        options={approvals.map((a) => ({
          id: a.id,
          label: a.summary,
          description: previewFields(a)
            .map((f) => `${f.label}: ${f.value}`)
            .join(" "),
        }))}
        selected={approvals.map((a) => a.id)}
      />
      <Actions blockId={`${name}:actions`}>
        <Button
          label={copy.approveSelected}
          style="primary"
          collects={name}
          action={{ id: ActionId.approveSelected, payload: { turnId, target: name } }}
        />
        <Menu
          label={copy.moreOptions}
          items={[
            {
              label: copy.approveAll(approvals.length),
              action: { id: ActionId.approveAll, payload: { turnId, target: name } },
            },
            // The conversation's mode, not these calls. A run held at a limit is the person's to decide.
            ...(offerAgent && !approvals.some((a) => a.budget)
              ? [
                  {
                    label: copy.approvals.letAgentDecide,
                    action: { id: ActionId.approvalMode, payload: { turnId, target: "auto" } },
                  },
                ]
              : []),
            {
              label: copy.denyAll,
              action: { id: ActionId.denyAll, payload: { turnId, target: name } },
              danger: true,
            },
          ]}
        />
        <Button
          label={copy.decline}
          collects={name}
          action={{ id: ActionId.deny, payload: { turnId, target: name } }}
        />
      </Actions>
      {acting.length > 0 && <Context items={[copy.actsForPersonIn(acting)]} />}
    </>
  );
}

/** Clarifying choice: two to four unstyled options, each with a line of context, plus a way out. */
export function ClarificationCard({
  turnId,
  clarification,
  copy = defaultCopy,
}: { turnId: string; clarification: Clarification } & WithCopy) {
  const { answer, options, judgement } = clarification;
  // The approval agent's part, in Auto: an answer it gave, which the person
  // can tell from their own, or why it left the question for them.
  const judged = judgement ? (
    <Context
      items={[
        withReason(
          judgement.decision === "allow"
            ? copy.judgement.answered
            : `**${copy.judgement.by}:** ${copy.judgement.decision[judgement.decision]}`,
          untrusted(judgement.reason),
        ),
      ]}
    />
  ) : null;
  if (answer.kind === "answered") {
    const picked = options.filter((o) => answer.optionIds.includes(o.id)).map((o) => o.label);
    // Words typed instead of a pick read as an answer, not a choice.
    const line =
      picked.length === 0 && answer.other
        ? copy.repliedBy(answer.by, answer.other, timeToken(answer.at))
        : copy.answeredBy(
            answer.by,
            picked.concat(answer.other ? [`"${answer.other}"`] : []).join(", "),
            timeToken(answer.at),
          );
    return (
      <>
        <Text text={clarification.question} />
        <Context items={[line]} />
        {judged}
      </>
    );
  }
  if (answer.kind === "unsure") {
    return (
      <>
        <Text text={clarification.question} />
        <Context items={[copy.unsureBy(answer.by, timeToken(answer.at))]} />
      </>
    );
  }
  if (answer.kind === "declined") {
    return (
      <>
        <Text text={clarification.question} />
        <Context
          items={[withReason(copy.declinedBy(answer.by, timeToken(answer.at)), answer.reason)]}
        />
      </>
    );
  }
  if (answer.kind === "expired" || answer.kind === "cancelled") {
    // Nobody answered: say what happened instead, with no way left to answer.
    const line =
      answer.kind === "expired"
        ? (answer.note ?? copy.questionExpired)
        : copy.questionCancelled(answer.note);
    return (
      <>
        <Text text={clarification.question} />
        <Context items={[`${line} | ${timeToken(answer.at)}`]} />
      </>
    );
  }
  // Someone it is not for reads it, and is told whom it is for.
  if (clarification.forOthers) {
    return (
      <>
        <Text text={clarification.question} />
        <Context items={[clarification.forOthers]} />
      </>
    );
  }
  const described = options.filter((o) => o.description);
  const body = [
    `**${clarification.question}**`,
    ...described.map((o) => `• **${o.label}**: ${o.description}`),
  ].join("\n");
  return (
    <>
      <Summary text={copy.inputNeeded(clarification.question)} />
      <Text text={body} />
      {judged}
      <Actions
        blockId={`clarification:${clarification.id}`}
        confirmChoice={{
          label: copy.conversation.answerChoice,
          hint: copy.conversation.questionHint,
        }}
      >
        {options.map((o) => (
          <Button
            key={o.id}
            label={o.label}
            action={{
              id: ActionId.clarify,
              payload: { turnId, target: clarification.id, option: o.id },
            }}
          />
        ))}
        {clarification.allowOther && (
          <Button
            label={copy.somethingElse}
            action={{ id: ActionId.clarifyOther, payload: { turnId, target: clarification.id } }}
          />
        )}
        {clarification.allowUnsure && (
          <Button
            label={copy.unsure}
            action={{ id: ActionId.unsure, payload: { turnId, target: clarification.id } }}
          />
        )}
      </Actions>
    </>
  );
}

export function InterruptCard({
  turnId,
  interrupt,
  viewer,
  offerAgent,
  copy,
}: { turnId: string; interrupt: Interrupt; viewer?: string; offerAgent?: boolean } & WithCopy) {
  return interrupt.type === "approval" ? (
    <ApprovalCard
      turnId={turnId}
      approval={interrupt}
      viewer={viewer}
      offerAgent={offerAgent}
      copy={copy}
    />
  ) : (
    <ClarificationCard turnId={turnId} clarification={interrupt} copy={copy} />
  );
}

/** Severity stated in words, with recovery actions instead of a dead end. */
export function NoticeView({ notice, copy = defaultCopy }: { notice: Notice } & WithCopy) {
  // What the runtime did on its own is a quiet record, not an alert.
  if (notice.kind === "silence" || notice.kind === "model_fallback" || notice.kind === "retried") {
    return <Context items={[notice.title ? `**${notice.title}**: ${notice.body}` : notice.body]} />;
  }
  return (
    <>
      <Callout
        tone={notice.tone}
        title={notice.title ?? copy.tone[notice.tone]}
        text={notice.body}
      />
      {notice.actions && notice.actions.length > 0 && (
        <Actions blockId={`notice:${notice.id}`}>
          {notice.actions.map((a) => (
            <Button key={a.label} label={a.label} action={a.action} />
          ))}
        </Actions>
      )}
    </>
  );
}

function capitalize(text: string) {
  return text.charAt(0).toUpperCase() + text.slice(1);
}

export function failureTitle(failure: Failure, copy: Copy = defaultCopy): string {
  return failure.title || capitalize(copy.failure[failure.kind](failure.service));
}

/** Keep credential configuration paths in authorized run details, not the answer body. */
function failureMessage(failure: Failure, copy: Copy): string {
  return /(?:op:\/\/|1password|credential|api[_ -]?key|secret reference)/i.test(failure.message) &&
    failure.kind === "internal"
    ? copy.credentialUnavailable
    : failure.message;
}

/**
 * A failed run, by who can fix it: the user (reconnect), time (retry later),
 * or us (report). Severity is in the words, recovery is a button, never a dead end.
 */
export function FailureAlert({
  turnId,
  failure,
  report = true,
  copy = defaultCopy,
}: {
  turnId: string;
  failure: Failure;
  /** Offer Report Issue. False when the host has nowhere to send a report. */
  report?: boolean;
} & WithCopy) {
  const lines = [failureMessage(failure, copy)];
  if (failure.retryAfterSeconds) lines.push(copy.retryIn(failure.retryAfterSeconds));
  const payload = { turnId, target: failure.kind };
  const actions =
    failure.kind === "auth_required"
      ? [{ label: copy.reconnect(failure.service ?? "App"), id: ActionId.reconnect }]
      : failure.kind === "forbidden"
        ? []
        : failure.kind === "internal"
          ? [
              { label: copy.retry, id: ActionId.retry, style: "primary" as const },
              { label: copy.report, id: ActionId.report },
            ]
          : [{ label: copy.retry, id: ActionId.retry, style: "primary" as const }];
  // Someone else may retry it: the reader is told why they are not offered it.
  const notOffered = isRetryable(failure) ? failure.retryNotOffered : undefined;
  const offered = actions.filter(
    (a) =>
      (a.id !== ActionId.retry || (isRetryable(failure) && !notOffered)) &&
      (a.id !== ActionId.report || report),
  );
  return (
    <>
      <Callout
        tone={failure.kind === "auth_required" || failure.kind === "forbidden" ? "warn" : "bad"}
        title={failureTitle(failure, copy)}
        text={lines.join(" ")}
      />
      {/* Always the row, even with no button: a renderer leaves an empty row out, and a host may add its own controls to it. */}
      <Actions blockId={`failure:${turnId}`}>
        {offered.map((a) => (
          <Button
            key={a.id}
            label={a.label}
            action={{ id: a.id, payload }}
            style={"style" in a ? a.style : undefined}
          />
        ))}
      </Actions>
      {!isRetryable(failure) && failure.kind !== "auth_required" && copy.cantRetry && (
        <Context items={[copy.cantRetry]} />
      )}
      {notOffered && <Context items={[notOffered]} />}
    </>
  );
}

/** Commands and code the agent ran or produced. */
export function AttachmentView({ attachment }: { attachment: Attachment }) {
  if (attachment.type === "artifact") {
    const { type: _type, ...file } = attachment;
    return <File {...file} />;
  }
  if (attachment.type === "code") {
    return <Code text={attachment.code} language={attachment.language} title={attachment.path} />;
  }
  const { type: _type, id: _id, ...run } = attachment;
  return <Terminal {...run} />;
}

/** Whether a question still waits on a person: not yet answered, or asked about. */
function waitingOn(i: Interrupt): boolean {
  return i.type === "approval"
    ? i.decision.kind === "pending" || i.decision.kind === "asked"
    : i.answer.kind === "pending";
}

/** Plan title tracks where the run is, then ends as the outcome. */
export function planTitle(turn: Turn, copy: Copy = defaultCopy): string {
  switch (turn.status) {
    case "done":
      return turn.durationMs !== undefined
        ? copy.planDone(Math.round(turn.durationMs / 1000))
        : (turn.planTitle ?? copy.planWorking);
    case "failed":
      return copy.planError;
    case "stopped":
      return copy.planStopped;
    default:
      return turn.planTitle ?? copy.planWorking;
  }
}

/** Tasks as one plan when the run is plan-shaped, otherwise as a timeline of task cards. */
/** Inline decision controls for a gated task, so the tool row holds its own options. */
function taskDecision(turn: Turn, interruptId: string | undefined, copy: Copy) {
  const approval = turn.interrupts.find(
    (i): i is ApprovalT => i.type === "approval" && i.id === interruptId,
  );
  if (!approval || approval.decision.kind !== "pending") return undefined;
  const payload = { turnId: turn.id, target: approval.id };
  const pendingCount = turn.interrupts.filter(
    (i) => i.type === "approval" && i.decision.kind === "pending",
  ).length;
  const menu = [
    ...(pendingCount > 1
      ? [
          {
            label: copy.approveAll(pendingCount),
            action: {
              id: ActionId.approveAll,
              payload: { turnId: turn.id, target: `batch:${turn.id}` },
            },
          },
        ]
      : []),
    ...(approval.rememberable
      ? [{ label: copy.alwaysAllow(approval.tool), action: { id: ActionId.remember, payload } }]
      : []),
    { label: copy.ask, action: { id: ActionId.ask, payload } },
  ];
  return {
    approve: {
      label: approval.budget
        ? copy.budgetHold.grant(approval.budget.grant)
        : (approval.approveLabel ?? copy.approve),
      action: { id: ActionId.approve, payload },
      style: approval.destructive ? ("danger" as const) : ("primary" as const),
      confirm: approval.destructive ? copy.confirmDestructive : undefined,
    },
    decline: {
      label: approval.budget ? copy.budgetHold.stop : copy.decline,
      action: { id: ActionId.deny, payload },
    },
    menu,
  };
}

/** Approvals tied to a task get inline controls on the web; Slack can't, so it shows the card. */
function taskOwned(turn: Turn): Set<string> {
  return new Set(turn.tasks.map((t) => t.interruptId).filter((x): x is string => Boolean(x)));
}

export function TaskList({ turn, copy = defaultCopy }: { turn: Turn } & WithCopy) {
  // The web draws a delegated step as the agent itself; Slack task rows all look alike.
  const inWords = useSurface().surface === "slack";
  if (turn.tasks.length === 0) return null;
  const tasks = turn.tasks.map(({ subagent, interruptId, ...t }) => (
    <Task
      key={t.id}
      {...t}
      output={inWords && readAgentChange(t) ? undefined : t.output}
      sourceLabels={copy.citation}
      title={subagent && inWords ? copy.delegated(subagent.name, t.title) : t.title}
      decision={taskDecision(turn, interruptId, copy)}
      details={
        subagent
          ? [copy.subagent(subagent.name, subagent.steps, subagent.tokens), t.details]
              .filter(Boolean)
              .join(" ")
          : t.details
      }
    />
  ));
  return turn.planTitle ? <Plan title={planTitle(turn, copy)}>{tasks}</Plan> : tasks;
}

/** The most lines a run of steps takes in one Slack message; the rest are counted in the last. */
const MAX_TOOL_LINES = 8;

/** A run of tool calls as one line of words, with its counts and what needs saying after it. */
export function toolsLineText(
  tasks: readonly TaskT[],
  copy: Copy = defaultCopy,
  turnEdits?: ReturnType<typeof fileEdits>,
  recovered?: ReadonlySet<string>,
): string {
  const line = toolsLine(tasks, copy, turnEdits, recovered);
  const w = copy.conversation.toolLine;
  return [
    line.counts && line.file && !line.running ? `${line.text} ${line.counts}` : line.text,
    line.failed > 0 ? w.failed(line.failed) : "",
    line.retried > 0 ? w.retried(line.retried) : "",
    line.waiting ? w.waiting : "",
  ]
    .filter(Boolean)
    .join(" | ");
}

/**
 * A turn's steps where a surface has room for one line each. While the turn
 * is going: every run of tool calls between two parts of the answer as one
 * context line, in the order they came, so the last line says what is
 * happening now. Once it has ended: one line that counts the work by kind
 * (`workSummary`). Each call to another agent keeps its own row either way.
 * Slack draws this; the web opens each run in place.
 */
export function ToolLines({ turn, copy = defaultCopy }: { turn: Turn } & WithCopy) {
  const { segments, rest } = timeline(turn);
  const edits = fileEdits(turn.tasks);
  const recovered = recoveredCalls(turn.tasks);
  const all = [...segments, ...rest].filter(
    (s): s is Extract<Segment, { kind: "tools" | "call" }> =>
      s.kind === "tools" || s.kind === "call",
  );
  if (all.length === 0) return null;
  const ended = turn.status === "done" || turn.status === "failed" || turn.status === "stopped";
  const own = turn.tasks.filter((t) => !t.subagent);
  // A turn that made one call says what it was pointed at; more are counted.
  const summary =
    ended && own.length > 0
      ? own.length === 1
        ? toolsLineText(own, copy, edits, recovered)
        : workSummary(turn.tasks, copy, edits, recovered)
      : undefined;
  const runs = ended ? all.filter((s) => s.kind === "call") : all;
  const shown = runs.length > MAX_TOOL_LINES ? runs.slice(0, MAX_TOOL_LINES - 1) : runs;
  const more = runs.slice(shown.length).flatMap((s) => (s.kind === "tools" ? s.tasks : [s.task]));
  return (
    <>
      {summary && <Context key="work" items={[summary]} />}
      {shown.map((s) =>
        s.kind === "call" ? (
          <TaskList
            key={s.key}
            turn={{ ...turn, planTitle: undefined, tasks: [s.task] }}
            copy={copy}
          />
        ) : (
          <Context key={s.key} items={[toolsLineText(s.tasks, copy, edits, recovered)]} />
        ),
      )}
      {more.length > 0 && (
        <Context key="more" items={[copy.conversation.toolLine.tool.many(more.length)]} />
      )}
    </>
  );
}

/** Replace `[[citation-id]]` markers with numbered inline links into the source list. */
export function linkCitations(text: string, citations: Citation[]): string {
  return text.replace(/\[\[([^\]]+)\]\]/g, (_m, id: string) => {
    const i = citations.findIndex((c) => c.id === id);
    if (i === -1) return "";
    const c = citations[i] as Citation;
    return c.url ? `[[${i + 1}]](${c.url})` : `[${i + 1}]`;
  });
}

const FENCE_LINE = /^ {0,3}(`{3,}|~{3,})(.*)$/;
/** A table's delimiter row, with or without pipes at its edges. */
const TABLE_DELIMITER = /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/;

/**
 * Room for markdown in one Slack message, as render-slack counts it
 * (`LIMITS.markdownTotal`): the arrow never makes an answer need another.
 */
const SLACK_MARKDOWN = 12_000;

/**
 * Whether markdown can end with a link on its last line, as Slack ends an
 * answer with the arrow to its conversation: not when it ends in code (a
 * fence, or a line indented as code), a table (with or without pipes at its
 * edges), a rule, a heading's underline or a link reference definition, where
 * a link would be swallowed or would change what is drawn.
 */
export function answerTakesLink(markdown: string): boolean {
  const lines = markdown.trimEnd().split("\n");
  const last = lines.at(-1) ?? "";
  if (!last.trim()) return false;
  let open: { ch: string; len: number } | null = null;
  for (const line of lines) {
    const m = FENCE_LINE.exec(line);
    if (!m) continue;
    const mark = m[1] as string;
    if (!open) open = { ch: mark[0] as string, len: mark.length };
    else if (mark[0] === open.ch && mark.length >= open.len && !(m[2] ?? "").trim()) open = null;
  }
  if (open || FENCE_LINE.test(last)) return false;
  if (/^( {4}|\t)/.test(last)) return false;
  // The last block is a table when any of its lines is a delimiter row.
  const block = lines.slice(lines.findLastIndex((l) => !l.trim()) + 1);
  if (block.some((l) => TABLE_DELIMITER.test(l)) || /^\s*\|.*\|\s*$/.test(last)) return false;
  // A setext underline (and a rule, which three or more dashes also are).
  if (/^ {0,3}(=+|-+)[ \t]*$/.test(last) && block.length > 1) return false;
  if (/^\s*([-*_])(\s*\1){2,}\s*$/.test(last)) return false;
  return !/^ {0,3}\[[^\]]+\]:\s*\S/.test(last);
}

/** The point of the turn in one line, for notifications and screen readers. */
export function summarizeTurn(turn: Turn, copy: Copy = defaultCopy): string {
  const pending = turn.interrupts.find((i) =>
    i.type === "approval" ? i.decision.kind === "pending" : i.answer.kind === "pending",
  );
  if (pending) {
    return pending.type === "approval"
      ? copy.approvalNeeded(pending.summary)
      : copy.inputNeeded(pending.question);
  }
  if (turn.status === "failed" && turn.failure) {
    return `${failureTitle(turn.failure, copy)}: ${failureMessage(turn.failure, copy)}`;
  }
  if (turn.status === "stopped") return copy.stopped;
  const review = readReview(turn.text);
  if (review.kind === "review") {
    return copy.reviewReport.point(reviewCountLine(review.report, copy), review.report.summary);
  }
  const answer = turn.text.replace(/\[\[[^\]]+\]\]/g, "").trim();
  if (answer) return answer.length > 300 ? `${answer.slice(0, 299)}…` : answer;
  return planTitle(turn, copy);
}

/**
 * A full agent turn, in Slack's recommended order: progress, then the answer
 * (which stands on its own), then sources, decisions, notices, and finally
 * feedback and the footer.
 */
export function TurnView({
  turn,
  copy = defaultCopy,
  streamed = false,
  viewer,
  capabilities,
  hideTasks = false,
  linked = false,
}: {
  turn: Turn;
  /**
   * Slack: the streamed answer already ends with the arrow to the
   * conversation (`streamLink`), so the final blocks do not repeat it.
   */
  linked?: boolean;
  /** What the host can act on. Controls it can't act on are left out. */
  capabilities?: TurnCapabilities;
  /** A web message frame may group these same tasks above the answer. */
  hideTasks?: boolean;
  /** Who is looking, for approval gating. */
  viewer?: string;
  /**
   * The progress and answer were already streamed (Slack chat.appendStream), so
   * render only what belongs in the final chat.stopStream call.
   */
  streamed?: boolean;
} & WithCopy) {
  const final = turn.status === "done";
  const owned = taskOwned(turn);
  // A question that was settled is part of how the answer came about, so it
  // sits between the steps and the answer. One still open waits below it.
  const settled = turn.interrupts.filter((i) => !waitingOn(i));
  const open = turn.interrupts.filter(waitingOn);
  const pendingApprovals = open.filter(
    (i): i is ApprovalT => i.type === "approval" && i.decision.kind === "pending",
  );
  const offerAgent = capabilities?.approvalAgent === true;
  // One link: the session, where a person goes to see the run. The source's own page
  // stands in only when there is no session.
  const runUrl = turn.sessionUrl ?? turn.source?.url;
  const disclaimer = turn.disclaimer ?? copy.disclaimer(turn.agent.name);
  const footer = disclaimer && turn.status !== "queued";
  // On Slack the session is one small arrow, on no line of its own: at the end of
  // a finished answer's last line, or else at the end of the disclaimer.
  const slack = useSurface().surface === "slack";
  // Slack shows the answer as one text with its steps above it: words said
  // before a call and after it are kept apart as paragraphs, as a stream does.
  const words = slack ? paragraphsAtSteps(turn.text, turn.tasks) : turn.text;
  const arrow = slack && turn.sessionUrl ? `[${copy.conversationLink}](${turn.sessionUrl})` : "";
  const answerDrawn =
    !streamed &&
    !!turn.text.trim() &&
    turn.text.trim() !== turn.outcome?.summary.trim() &&
    readReview(turn.text, turn.status === "working" && !turn.detached).kind === "text";
  const answer = linkCitations(words, turn.citations).trimEnd();
  // Markdown the message holds before the answer (the outcome), counted toward Slack's room.
  const before = turn.outcome
    ? turn.outcome.summary.length +
      (turn.outcome.decisions ?? []).reduce((n, d) => n + d.length + 3, 0) +
      (turn.outcome.nextAction?.length ?? 0)
    : 0;
  const inAnswer =
    !!arrow &&
    (streamed
      ? linked
      : answerDrawn &&
        (turn.status === "done" || turn.status === "waiting") &&
        answerTakesLink(answer) &&
        // Where the arrow would push the answer into another message, the disclaimer has it.
        before + answer.length + arrow.length + 1 <= SLACK_MARKDOWN);
  const inFooter = !!arrow && !inAnswer && !!footer;
  const session = arrow ? (
    !inAnswer && !inFooter ? (
      <Context items={[arrow]} />
    ) : null
  ) : runUrl ? (
    <Context
      items={[turn.sessionUrl ? `[${copy.openConversation}](${runUrl})` : copy.runLink(runUrl)]}
    />
  ) : null;

  return (
    <>
      <Summary text={summarizeTurn(turn, copy)} />
      {/* Slack shows status as a reaction and the session status (see render-slack lifecycle). */}
      <Only surface="web">
        <Context items={[`**${turn.agent.name}**: ${copy.status[turn.status]}`]} />
      </Only>
      {turn.handoff && (
        <Context
          items={[
            [`→ ${copy.handoff(turn.handoff.to)}`, turn.handoff.reason].filter(Boolean).join(" "),
          ]}
        />
      )}
      {!streamed &&
        !hideTasks &&
        (turn.planTitle ? (
          <TaskList turn={turn} copy={copy} />
        ) : (
          <>
            <Only surface="web">
              <TaskList turn={turn} copy={copy} />
            </Only>
            <Only surface="slack">
              <ToolLines turn={turn} copy={copy} />
            </Only>
          </>
        ))}
      {settled.map((i) => (
        <InterruptCard key={i.id} turnId={turn.id} interrupt={i} viewer={viewer} copy={copy} />
      ))}
      {turn.outcome && (
        <>
          <Header text={copy.conversation.outcomeKind[turn.outcome.kind]} />
          <Markdown text={linkCitations(turn.outcome.summary, turn.citations)} />
          {!!turn.outcome.decisions?.length && (
            <>
              <Header text={copy.conversation.outcomeDecisions} />
              <Markdown
                text={turn.outcome.decisions.map((decision) => `- ${decision}`).join("\n")}
              />
            </>
          )}
          {turn.outcome.nextAction && (
            <>
              <Header text={copy.conversation.outcomeNext} />
              <Markdown text={turn.outcome.nextAction} />
            </>
          )}
        </>
      )}
      {/*
        A review is never streamed as words (render-slack holds it back), so it is
        drawn here: as a card, or as the text it was if it did not parse.
      */}
      {(!streamed || readReview(turn.text, true).kind !== "text") &&
        turn.text &&
        turn.text.trim() !== turn.outcome?.summary.trim() && (
          <AnswerText
            turnId={turn.id}
            agent={turn.agent.name}
            text={turn.text}
            markdown={
              inAnswer && !streamed ? `${answer} ${arrow}` : linkCitations(words, turn.citations)
            }
            streaming={turn.status === "working" && !turn.detached}
            pullRequestHint={turn.source?.url}
            followUp={capabilities?.followUp !== false}
            copy={copy}
          />
        )}
      {turn.attachments?.map((a) => (
        <AttachmentView key={a.id} attachment={a} />
      ))}
      {/* A command that failed is shown with its output; one that passed is a count in the steps. */}
      <Only surface="slack">
        <FailedCommands turn={turn} copy={copy} />
      </Only>
      {/* The web shows what a turn changed as its own card (`TurnChanges` in `@humagen/web`). */}
      {turnSettled(turn.status) && (
        <Only surface="slack">
          <ChangedFiles tasks={turn.tasks} copy={copy} />
        </Only>
      )}
      <Only surface="slack">
        <AgentChangeCards tasks={turn.tasks} />
      </Only>
      {/* Gated tool rows hold their own controls on the web; Slack gets the cards. */}
      {pendingApprovals.length > 1 && owned.size > 0 ? (
        <Only surface="slack">
          <ApprovalBatch
            turnId={turn.id}
            approvals={pendingApprovals}
            offerAgent={offerAgent}
            copy={copy}
          />
        </Only>
      ) : pendingApprovals.length > 1 ? (
        <>
          <ApprovalBatch
            turnId={turn.id}
            approvals={pendingApprovals}
            offerAgent={offerAgent}
            copy={copy}
          />
          {open
            .filter((i) => !pendingApprovals.includes(i as ApprovalT))
            .map((i) => (
              <InterruptCard
                key={i.id}
                turnId={turn.id}
                interrupt={i}
                viewer={viewer}
                offerAgent={offerAgent}
                copy={copy}
              />
            ))}
        </>
      ) : (
        open.map((i) =>
          owned.has(i.id) && i.type === "approval" && i.decision.kind === "pending" ? (
            <Only key={i.id} surface="slack">
              <InterruptCard
                turnId={turn.id}
                interrupt={i}
                viewer={viewer}
                offerAgent={offerAgent}
                copy={copy}
              />
            </Only>
          ) : (
            <InterruptCard
              key={i.id}
              turnId={turn.id}
              interrupt={i}
              viewer={viewer}
              offerAgent={offerAgent}
              copy={copy}
            />
          ),
        )
      )}
      {turn.notices.map((n) => (
        <NoticeView key={n.id} notice={n} copy={copy} />
      ))}
      {turn.detached && (turn.status === "working" || turn.status === "queued") && (
        <Context items={[copy.stillWorking(turn.agent.name)]} />
      )}
      {turn.status === "failed" && turn.failure && (
        <FailureAlert
          turnId={turn.id}
          failure={turn.failure}
          report={capabilities?.report !== false}
          copy={copy}
        />
      )}
      {turn.status === "stopped" && <Text text={copy.stopped} />}
      {turn.status === "stopped" && turn.stop && <Context items={[copy.stopPhase[turn.stop]]} />}
      {turn.status === "stopped" && turn.stop === "confirmed" && (
        <Actions blockId={`resume:${turn.id}`}>
          <Button
            label={copy.resume}
            action={{ id: ActionId.resume, payload: { turnId: turn.id, target: turn.id } }}
          />
        </Actions>
      )}
      {final && capabilities?.codeChannel === true && (
        <Actions blockId={`code-channel:${turn.id}`}>
          <Button
            label={copy.continueInCodeChannel}
            action={{ id: ActionId.codeChannel, payload: { turnId: turn.id, target: turn.id } }}
          />
        </Actions>
      )}
      {final && capabilities?.feedback !== false && (
        <Feedback
          up={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "up" } }}
          down={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "down" } }}
          selected={turn.feedback}
          labels={{ up: copy.helpful, down: copy.notHelpful, rated: copy.rated }}
          prompt={copy.feedbackPrompt}
          blockId={`feedback:${turn.id}`}
        />
      )}
      {capabilities?.runDetails === true &&
        (final || turn.status === "failed" || turn.status === "stopped") && (
          <Only surface="slack">
            <Actions blockId={`details:${turn.id}`}>
              <Button
                label={copy.runDetails.button}
                action={{ id: ActionId.runDetails, payload: { turnId: turn.id, target: turn.id } }}
              />
            </Actions>
          </Only>
        )}
      {/*
        Every message ends the same way, whatever happened in it: the references, then the
        one link to the run, then the disclaimer. Nothing sits between them and nothing
        comes after, so cards, notices and feedback all go above. On Slack the link is the
        arrow at the end of the answer or of the disclaimer, and a line only with neither.
      */}
      {turn.citations.length > 0 && (
        <Sources items={turn.citations} labels={copy.citation} scope={turn.id} />
      )}
      {session}
      {footer && <Context items={[inFooter ? `${disclaimer} ${arrow}` : disclaimer]} />}
    </>
  );
}

/**
 * What a person asked, in their own words. The web draws it as their chat
 * bubble; on Slack it is the message a turn's thread hangs from (post it,
 * then answer in its thread). `by` is a name, or on Slack a mention (`<@U123>`).
 */
export function RequestView({ by, text }: { by: string; text: string }) {
  return <Request by={by} text={text} />;
}

/**
 * The agents a person can ask, the same list the web's agent menu shows:
 * each one's handle and what it is for. Slack sends it for `/agent help`.
 */
export function AgentList({
  agents,
  copy = defaultCopy,
}: { agents: { name: string; description?: string }[] } & WithCopy) {
  if (agents.length === 0) {
    return (
      <>
        <Summary text={copy.agentList.empty} />
        <Text text={copy.agentList.empty} />
      </>
    );
  }
  return (
    <>
      <Summary text={copy.agentList.title} />
      <Text text={`**${copy.agentList.title}**`} />
      {agents.map((a) => (
        <Row
          key={a.name}
          text={`\`${copy.agentList.handle(a.name)}\``}
          detail={a.description || copy.agentList.noDescription}
        />
      ))}
      <Context items={[copy.agentList.hint]} />
    </>
  );
}

/**
 * Raise a limit to an amount the reader types, in the meter's unit. Sends
 * `ActionId.raiseBudget` (the `action` given) with the new limit as `option`.
 * The web draws a number field; Slack a button whose click opens `amountModal`.
 */
export function RaiseLimit({
  unit,
  limit,
  max,
  action,
  copy = defaultCopy,
}: {
  /** The meter's unit: "usd" for money, otherwise what is counted ("runs"). */
  unit: string;
  /** The limit in force. The new one must be more. */
  limit: number;
  /** The most it may be raised to. */
  max?: number;
  /** Usually `partAction(message, part, ActionId.raiseBudget)`. */
  action: ActionRef;
} & WithCopy) {
  return (
    <Amount
      label={copy.newLimit}
      unit={unit}
      above={limit}
      {...(max !== undefined ? { max } : {})}
      hint={copy.moreThan(formatAmount(limit, unit))}
      submit={{ label: copy.raiseLimit, action }}
    />
  );
}

/**
 * Conversation-level notices for surfaces without a status line (Slack).
 * The web shows the same state continuously in its status line instead.
 */
export function SessionNotices({
  session,
  copy = defaultCopy,
}: { session: SessionState } & WithCopy) {
  const c = session.context ? contextPressure(session.context) : null;
  const b = session.budget ? budgetPressure(session.budget) : null;
  const compaction = session.compaction;
  return (
    <>
      {compaction?.kind === "compacting" && <Context items={[copy.compacting]} />}
      {compaction?.kind === "compacted" && (
        <Context
          items={[
            [copy.compacted(formatTokens(compaction.savedTokens)), compaction.summary]
              .filter(Boolean)
              .join(" "),
          ]}
        />
      )}
      {compaction?.kind === "failed" && (
        <Callout
          tone="warn"
          title={copy.compactFailed(compaction.reason)}
          text={c ? copy.contextNearlyFull(Math.round(c.ratio * 100)) : ""}
        />
      )}
      {c &&
        c.pressure !== "ok" &&
        compaction?.kind !== "compacting" &&
        compaction?.kind !== "failed" && (
          <Callout
            tone={c.pressure === "critical" ? "bad" : "warn"}
            title={copy.contextNearlyFull(Math.round(c.ratio * 100))}
            text="Older messages will be summarized to make room."
          />
        )}
      {c && c.pressure !== "ok" && (
        <Actions blockId="session:context">
          <Button
            label={copy.compactNow}
            action={{ id: ActionId.compact, payload: { turnId: "session", target: "compact" } }}
          />
          <Button
            label={copy.startFresh}
            action={{ id: ActionId.retry, payload: { turnId: "session", target: "fresh" } }}
          />
        </Actions>
      )}
      {b?.blocked && session.budget && (
        <Callout
          tone="warn"
          title={
            session.budget.spentUsd >= session.budget.limitUsd
              ? "Spend cap reached"
              : "Next turn would pass the spend cap"
          }
          text={`$${session.budget.spentUsd.toFixed(2)} of $${session.budget.limitUsd.toFixed(2)} this ${session.budget.period}.`}
        />
      )}
    </>
  );
}

/** A dependency incident, stated in words, updated in place as it develops. */
export function SystemAlertView({ alert, copy = defaultCopy }: { alert: SystemAlert } & WithCopy) {
  const status = copy.serviceStatus[alert.status];
  const meta = [
    copy.started(timeToken(alert.startedAt)),
    alert.updatedAt !== alert.startedAt ? copy.updated(timeToken(alert.updatedAt)) : null,
    alert.url ? `[${copy.statusPage}](${alert.url})` : null,
  ].filter((x): x is string => x !== null);
  if (alert.status === "resolved" || alert.status === "operational") {
    return <Context items={[`**${alert.service}**: ${status}. ${alert.title}`, ...meta]} />;
  }
  const body = [alert.body, alert.affects?.length ? copy.affects(alert.affects.join(", ")) : null]
    .filter(Boolean)
    .join(" ");
  return (
    <>
      <Summary text={`${alert.service} ${status.toLowerCase()}: ${alert.title}`} />
      <Callout
        tone={SERVICE_TONE[alert.status]}
        title={`${alert.service} ${status.toLowerCase()}: ${alert.title}`}
        text={body}
      />
      <Context items={meta} />
    </>
  );
}

function itemActions(item: InboxItem, copy: Copy) {
  const payload = { turnId: item.source.turnId ?? "inbox", target: item.id };
  return (
    <Actions blockId={`inbox:${item.id}`}>
      {item.source.url && <Button label={copy.openConversation} url={item.source.url} />}
      <Menu
        label={copy.moreOptions}
        items={[
          { label: copy.snooze, action: { id: ActionId.snooze, payload } },
          { label: copy.dismiss, action: { id: ActionId.dismiss, payload } },
        ]}
      />
    </Actions>
  );
}

/**
 * Everything waiting on a person, across conversations. Slack publishes it as
 * the App Home tab (placement "home"); the web wraps it in an inbox panel.
 * Decisions resolve the item in place; the next refresh moves it to Done.
 */
export function InboxView({
  items,
  viewer,
  copy = defaultCopy,
}: { items: InboxItem[]; viewer?: string } & WithCopy) {
  const open = items.filter((i) => i.state === "open");
  const decisions = open.filter((i) => i.kind === "approval" || i.kind === "clarification");
  const alerts = open.filter((i) => i.kind === "alert");
  const failures = open.filter((i) => i.kind === "failure");
  const resolved = items.filter((i) => i.state !== "open").slice(0, 10);
  const from = (i: InboxItem) => <Context items={[copy.from(i.source.label)]} />;
  return (
    <>
      <Summary text={`${copy.inbox}: ${copy.inboxCount(decisions.length + failures.length)}`} />
      <Header text={copy.inbox} />
      <Context items={[copy.inboxCount(decisions.length + failures.length)]} />
      {alerts.length > 0 && (
        <>
          <Header text={copy.alerts} />
          {alerts.map((i) =>
            i.kind === "alert" ? <SystemAlertView key={i.id} alert={i.alert} copy={copy} /> : null,
          )}
        </>
      )}
      {decisions.length > 0 && (
        <>
          <Header text={copy.needsYou} />
          {decisions.map((i) => (
            <InboxEntry key={i.id} item={i} viewer={viewer} copy={copy} from={from(i)} />
          ))}
        </>
      )}
      {failures.length > 0 && (
        <>
          <Header text={copy.failures} />
          {failures.map((i) => (
            <InboxEntry key={i.id} item={i} viewer={viewer} copy={copy} from={from(i)} />
          ))}
        </>
      )}
      {resolved.length > 0 && (
        <>
          <Divider />
          <Context
            items={[
              `**${copy.resolved}**`,
              ...resolved.map((i) => `${inboxTitle(i)}: ${i.source.label}`),
            ]}
          />
        </>
      )}
    </>
  );
}

function inboxTitle(i: InboxItem): string {
  switch (i.kind) {
    case "approval":
      return i.approval.summary;
    case "clarification":
      return i.clarification.question;
    case "failure":
      return i.failure.message;
    case "alert":
      return `${i.alert.service}: ${i.alert.title}`;
  }
}

function InboxEntry({
  item,
  viewer,
  copy,
  from,
}: {
  item: InboxItem;
  viewer?: string;
  copy: Copy;
  from: ReactNode;
}) {
  const turnId = item.source.turnId ?? "inbox";
  return (
    <>
      {item.kind === "approval" && (
        <ApprovalCard turnId={turnId} approval={item.approval} viewer={viewer} copy={copy} />
      )}
      {item.kind === "clarification" && (
        <ClarificationCard turnId={turnId} clarification={item.clarification} copy={copy} />
      )}
      {item.kind === "failure" && (
        <FailureAlert turnId={turnId} failure={item.failure} copy={copy} />
      )}
      {from}
      {itemActions(item, copy)}
    </>
  );
}
