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
  formatAmount,
  formatTokens,
  type InboxItem,
  type Interrupt,
  isRetryable,
  type Notice,
  SERVICE_TONE,
  type SessionState,
  type SystemAlert,
  type Turn,
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
import { type Copy, defaultCopy } from "./copy";

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
}

/** Fallback preview when a tool supplies none: scalar args as fields, never a JSON dump. */
function previewFields(approval: Approval): Field[] {
  if (approval.preview) return approval.preview;
  return Object.entries(approval.args ?? {})
    .filter(([, v]) => ["string", "number", "boolean"].includes(typeof v))
    .map(([label, value]) => ({ label, value: String(value) }));
}

function withReason(line: string, reason?: string) {
  return reason ? `${line}: "${reason}"` : line;
}

/**
 * Approval gate. Point first, a preview of exactly what will happen, specific
 * verbs, one emphasized button, and a record in place of the controls once decided.
 */
export function ApprovalCard({
  turnId,
  approval,
  viewer,
  copy = defaultCopy,
}: {
  turnId: string;
  approval: Approval;
  /** Who is looking. Non-initiators can ask but never approve. */
  viewer?: string;
} & WithCopy) {
  const { decision } = approval;
  const fields = previewFields(approval);
  const payload = { turnId, target: approval.id };
  const allowed = viewer === undefined || canDecide(approval, viewer);

  if (decision.kind === "asked") {
    // Asking halts the call; the agent answers, then the gate is raised again.
    return (
      <>
        <Text text={`**${approval.summary}**`} />
        {fields.length > 0 && <Fields items={fields} />}
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
        <Actions blockId={`approval:${approval.id}:r${approval.revision ?? 0}`}>
          <Button label={copy.ask} action={{ id: ActionId.ask, payload }} />
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
        <Actions blockId={`approval:${approval.id}:r${approval.revision ?? 0}`}>
          <Button
            label={approval.approveLabel ?? copy.approve}
            style={approval.destructive ? "danger" : "primary"}
            action={{ id: ActionId.approve, payload }}
            accessibilityLabel={`${approval.approveLabel ?? copy.approve}: ${approval.summary}`}
            confirm={approval.destructive ? copy.confirmDestructive : undefined}
          />
          {approval.editable && !approval.actsForPerson && (
            <Button label={copy.edit} action={{ id: ActionId.edit, payload }} />
          )}
          <Button label={copy.ask} action={{ id: ActionId.ask, payload }} />
          {approval.rememberable && (
            <Button
              label={copy.alwaysAllow(approval.tool)}
              action={{ id: ActionId.remember, payload }}
            />
          )}
          <Button
            label={copy.cancel}
            action={{ id: ActionId.deny, payload }}
            accessibilityLabel={`${copy.cancel}: ${approval.summary}`}
          />
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
        <Context
          items={[
            withReason(copy.approvedBy(decision.by, timeToken(decision.at)), decision.reason),
          ]}
        />
      </>
    );
  }

  // Declined, timed out, or cancelled: keep the preview struck through, then the outcome.
  const outcome =
    decision.kind === "denied"
      ? copy.rejected
      : decision.kind === "expired"
        ? copy.timedOut
        : copy.cancelled;
  return (
    <>
      <Text text={`~~${approval.summary}~~ ${outcome}`} />
      {decision.kind === "denied" && (
        <Context
          items={[
            withReason(copy.rejectedBy(decision.by, timeToken(decision.at)), decision.reason),
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
  copy = defaultCopy,
}: { turnId: string; approvals: ApprovalT[] } & WithCopy) {
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
  const { answer, options } = clarification;
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
  copy,
}: { turnId: string; interrupt: Interrupt; viewer?: string } & WithCopy) {
  return interrupt.type === "approval" ? (
    <ApprovalCard turnId={turnId} approval={interrupt} viewer={viewer} copy={copy} />
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
  return capitalize(copy.failure[failure.kind](failure.service));
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
              { label: copy.retry, id: ActionId.retry },
              { label: copy.report, id: ActionId.report },
            ]
          : [{ label: copy.retry, id: ActionId.retry }];
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
      {offered.length > 0 && (
        <Actions blockId={`failure:${turnId}`}>
          {offered.map((a) => (
            <Button key={a.id} label={a.label} action={{ id: a.id, payload }} />
          ))}
        </Actions>
      )}
      {!isRetryable(failure) && failure.kind !== "auth_required" && (
        <Context items={[copy.cantRetry]} />
      )}
      {notOffered && <Context items={[notOffered]} />}
      {failure.reference && <Context items={[copy.reference(failure.reference)]} />}
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
      label: approval.approveLabel ?? copy.approve,
      action: { id: ActionId.approve, payload },
      style: approval.destructive ? ("danger" as const) : ("primary" as const),
      confirm: approval.destructive ? copy.confirmDestructive : undefined,
    },
    decline: { label: copy.decline, action: { id: ActionId.deny, payload } },
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

/** Replace `[[citation-id]]` markers with numbered inline links into the source list. */
export function linkCitations(text: string, citations: Citation[]): string {
  return text.replace(/\[\[([^\]]+)\]\]/g, (_m, id: string) => {
    const i = citations.findIndex((c) => c.id === id);
    if (i === -1) return "";
    const c = citations[i] as Citation;
    return c.url ? `[[${i + 1}]](${c.url})` : `[${i + 1}]`;
  });
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
}: {
  turn: Turn;
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
  // One link: the session, where a person goes to see the run. The source's own page
  // stands in only when there is no session.
  const runUrl = turn.sessionUrl ?? turn.source?.url;
  const session = runUrl ? (
    <Context
      items={[turn.sessionUrl ? `[${copy.openConversation}](${runUrl})` : copy.runLink(runUrl)]}
    />
  ) : null;
  const disclaimer = turn.disclaimer ?? copy.disclaimer(turn.agent.name);

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
      {!streamed && !hideTasks && <TaskList turn={turn} copy={copy} />}
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
      {!streamed && turn.text && turn.text.trim() !== turn.outcome?.summary.trim() && (
        <Markdown
          text={linkCitations(turn.text, turn.citations)}
          streaming={turn.status === "working" && !turn.detached}
        />
      )}
      {turn.attachments?.map((a) => (
        <AttachmentView key={a.id} attachment={a} />
      ))}
      {/* Gated tool rows hold their own controls on the web; Slack gets the cards. */}
      {pendingApprovals.length > 1 && owned.size > 0 ? (
        <Only surface="slack">
          <ApprovalBatch turnId={turn.id} approvals={pendingApprovals} copy={copy} />
        </Only>
      ) : pendingApprovals.length > 1 ? (
        <>
          <ApprovalBatch turnId={turn.id} approvals={pendingApprovals} copy={copy} />
          {open
            .filter((i) => !pendingApprovals.includes(i as ApprovalT))
            .map((i) => (
              <InterruptCard
                key={i.id}
                turnId={turn.id}
                interrupt={i}
                viewer={viewer}
                copy={copy}
              />
            ))}
        </>
      ) : (
        open.map((i) =>
          owned.has(i.id) && i.type === "approval" && i.decision.kind === "pending" ? (
            <Only key={i.id} surface="slack">
              <InterruptCard turnId={turn.id} interrupt={i} viewer={viewer} copy={copy} />
            </Only>
          ) : (
            <InterruptCard key={i.id} turnId={turn.id} interrupt={i} viewer={viewer} copy={copy} />
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
      {/*
        Every message ends the same way, whatever happened in it: the references, then one
        line with the one link to the run, then the disclaimer. Nothing sits between them
        and nothing comes after, so cards, notices and feedback all go above.
      */}
      {turn.citations.length > 0 && (
        <Sources items={turn.citations} labels={copy.citation} scope={turn.id} />
      )}
      {session}
      {disclaimer && turn.status !== "queued" && <Context items={[disclaimer]} />}
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
 * Decisions resolve the item in place; the next refresh moves it to Resolved.
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
