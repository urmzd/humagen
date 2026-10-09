import {
  ActionId,
  type Approval,
  type Attachment,
  type Citation,
  type Clarification,
  type Failure,
  type Field,
  type Interrupt,
  type Notice,
  type Turn,
  timeToken,
} from "@agent-ui/core";
import {
  Actions,
  Button,
  Callout,
  Code,
  Context,
  Feedback,
  Fields,
  Markdown,
  Only,
  Plan,
  Sources,
  Summary,
  Task,
  Terminal,
  Text,
} from "@agent-ui/primitives";
import { type Copy, defaultCopy } from "./copy";

interface WithCopy {
  copy?: Copy;
}

/** Fallback preview when a tool supplies none: scalar args as fields, never a JSON dump. */
function previewFields(approval: Approval): Field[] {
  if (approval.preview) return approval.preview;
  return Object.entries(approval.args ?? {})
    .filter(([, v]) => ["string", "number", "boolean"].includes(typeof v))
    .map(([label, value]) => ({ label, value: String(value) }));
}

function withReason(line: string, reason?: string) {
  return reason ? `${line} · "${reason}"` : line;
}

/**
 * Approval gate. Point first, a preview of exactly what will happen, specific
 * verbs, one emphasized button, and a record in place of the controls once decided.
 */
export function ApprovalCard({
  turnId,
  approval,
  copy = defaultCopy,
}: { turnId: string; approval: Approval } & WithCopy) {
  const { decision } = approval;
  const fields = previewFields(approval);
  const payload = { turnId, target: approval.id };

  if (decision.kind === "pending") {
    const meta = [
      approval.actingAs ? copy.actingAs(approval.actingAs) : null,
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
          {approval.editable && (
            <Button label={copy.edit} action={{ id: ActionId.edit, payload }} />
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

/** Clarifying choice: two to four unstyled options, each with a line of context, plus a way out. */
export function ClarificationCard({
  turnId,
  clarification,
  copy = defaultCopy,
}: { turnId: string; clarification: Clarification } & WithCopy) {
  const { answer, options } = clarification;
  if (answer.kind === "answered") {
    const choice = options
      .filter((o) => answer.optionIds.includes(o.id))
      .map((o) => o.label)
      .concat(answer.other ? [`"${answer.other}"`] : [])
      .join(", ");
    return (
      <>
        <Text text={clarification.question} />
        <Context items={[copy.answeredBy(answer.by, choice, timeToken(answer.at))]} />
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
  const described = options.filter((o) => o.description);
  const body = [
    `**${clarification.question}**`,
    ...described.map((o) => `• **${o.label}**: ${o.description}`),
  ].join("\n");
  return (
    <>
      <Summary text={copy.inputNeeded(clarification.question)} />
      <Text text={body} />
      <Actions blockId={`clarification:${clarification.id}`}>
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
      </Actions>
    </>
  );
}

export function InterruptCard({
  turnId,
  interrupt,
  copy,
}: { turnId: string; interrupt: Interrupt } & WithCopy) {
  return interrupt.type === "approval" ? (
    <ApprovalCard turnId={turnId} approval={interrupt} copy={copy} />
  ) : (
    <ClarificationCard turnId={turnId} clarification={interrupt} copy={copy} />
  );
}

/** Severity stated in words, with recovery actions instead of a dead end. */
export function NoticeView({ notice, copy = defaultCopy }: { notice: Notice } & WithCopy) {
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

/**
 * A failed run, by who can fix it: the user (reconnect), time (retry later),
 * or us (report). Severity is in the words, recovery is a button, never a dead end.
 */
export function FailureAlert({
  turnId,
  failure,
  copy = defaultCopy,
}: { turnId: string; failure: Failure } & WithCopy) {
  const lines = [failure.message];
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
  return (
    <>
      <Callout
        tone={failure.kind === "auth_required" || failure.kind === "forbidden" ? "warn" : "bad"}
        title={failureTitle(failure, copy)}
        text={lines.join(" ")}
      />
      {actions.length > 0 && (
        <Actions blockId={`failure:${turnId}`}>
          {actions.map((a) => (
            <Button key={a.id} label={a.label} action={{ id: a.id, payload }} />
          ))}
        </Actions>
      )}
      {failure.reference && <Context items={[copy.reference(failure.reference)]} />}
    </>
  );
}

/** Commands and code the agent ran or produced. */
export function AttachmentView({ attachment }: { attachment: Attachment }) {
  if (attachment.type === "code") {
    return <Code text={attachment.code} language={attachment.language} title={attachment.path} />;
  }
  const { type: _type, id: _id, ...run } = attachment;
  return <Terminal {...run} />;
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
export function TaskList({ turn, copy = defaultCopy }: { turn: Turn } & WithCopy) {
  if (turn.tasks.length === 0) return null;
  const tasks = turn.tasks.map((t) => <Task key={t.id} {...t} />);
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
    return `${failureTitle(turn.failure, copy)}: ${turn.failure.message}`;
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
}: {
  turn: Turn;
  /**
   * The progress and answer were already streamed (Slack chat.appendStream), so
   * render only what belongs in the final chat.stopStream call.
   */
  streamed?: boolean;
} & WithCopy) {
  const final = turn.status === "done";
  const footer = [
    turn.sessionUrl ? `[${copy.openSession}](${turn.sessionUrl})` : null,
    final ? turn.disclaimer : null,
  ].filter((x): x is string => Boolean(x));

  return (
    <>
      <Summary text={summarizeTurn(turn, copy)} />
      {/* Slack shows status as a reaction and the session status (see render-slack lifecycle). */}
      <Only surface="web">
        <Context items={[`**${turn.agent.name}** · ${copy.status[turn.status]}`]} />
      </Only>
      {!streamed && <TaskList turn={turn} copy={copy} />}
      {!streamed && turn.text && (
        <Markdown
          text={linkCitations(turn.text, turn.citations)}
          streaming={turn.status === "working"}
        />
      )}
      {turn.attachments?.map((a) => (
        <AttachmentView key={a.id} attachment={a} />
      ))}
      {turn.citations.length > 0 && <Sources items={turn.citations} />}
      {turn.interrupts.map((i) => (
        <InterruptCard key={i.id} turnId={turn.id} interrupt={i} copy={copy} />
      ))}
      {turn.notices.map((n) => (
        <NoticeView key={n.id} notice={n} copy={copy} />
      ))}
      {turn.status === "failed" && turn.failure && (
        <FailureAlert turnId={turn.id} failure={turn.failure} copy={copy} />
      )}
      {turn.status === "stopped" && <Text text={copy.stopped} />}
      {final && (
        <Feedback
          up={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "up" } }}
          down={{ id: ActionId.feedback, payload: { turnId: turn.id, target: "down" } }}
          selected={turn.feedback}
          labels={{ up: copy.helpful, down: copy.notHelpful }}
          blockId={`feedback:${turn.id}`}
        />
      )}
      {footer.length > 0 && <Context items={footer} />}
    </>
  );
}
