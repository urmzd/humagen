import {
  ActionId,
  canAct,
  formatAmount,
  type InboxMessage,
  type MessagePart,
  messageState,
  messageTemplate,
  openParts,
  partAction,
  partResolution,
  partTarget,
  sortMessages,
  timeToken,
} from "@warp/core";
import {
  Actions,
  Button,
  Callout,
  Checkboxes,
  Context,
  Divider,
  Header,
  Markdown,
  Menu,
  Sources,
  Summary,
  Text,
} from "@warp/primitives";
import { Fragment } from "react";
import {
  ApprovalCard,
  AttachmentView,
  ClarificationCard,
  FailureAlert,
  RaiseLimit,
  SystemAlertView,
} from "./components";
import { type Copy, defaultCopy } from "./copy";

interface WithCopy {
  copy?: Copy;
}

/**
 * One part of a message: what it says, the controls that resolve it, and its
 * record once resolved. Built from primitives, so the web reading pane and a
 * Slack message show the same thing and emit the same actions.
 */
export function MessagePartView({
  message,
  part,
  viewer,
  copy = defaultCopy,
}: { message: InboxMessage; part: MessagePart; viewer?: string } & WithCopy) {
  const done = partResolution(part);
  const record = done ? (
    <Context items={[copy.outcome(done.outcome, done.by, timeToken(done.at), done.detail)]} />
  ) : null;
  const act = (id: ActionId, extra?: { option?: string }) => partAction(message, part, id, extra);
  const may = (id: ActionId) => canAct(part, id);
  // A reader who can't decide still sees what is being asked, without buttons that would fail.
  const locked = (line: string) => (
    <>
      <Text text={`**${line}**`} />
      <Context items={[copy.noPermission]} />
    </>
  );
  switch (part.type) {
    case "text":
      return <Markdown text={part.text} />;
    case "approval":
      if (!done && !may(ActionId.approve) && !may(ActionId.deny)) {
        return locked(part.approval.summary);
      }
      // The card keeps its own record ("Approved by …"), so none is added here.
      return (
        <ApprovalCard turnId={message.id} approval={part.approval} viewer={viewer} copy={copy} />
      );
    case "choice":
      if (!done && !may(ActionId.clarify)) return locked(part.clarification.question);
      return (
        <ClarificationCard turnId={message.id} clarification={part.clarification} copy={copy} />
      );
    case "reply":
      return (
        <>
          <Text text={`**${part.prompt}**`} />
          {record ??
            (may(ActionId.reply) ? (
              <Actions blockId={`reply:${part.id}`}>
                <Button label={copy.reply} style="primary" action={act(ActionId.reply)} />
              </Actions>
            ) : (
              <Context items={[copy.noPermission]} />
            ))}
        </>
      );
    case "memory": {
      if (done) {
        const kept = new Set(part.kept ?? []);
        return (
          <>
            <Text
              text={part.proposals
                .map((p) => (kept.has(p.id) ? `• ${p.text}` : `• ~~${p.text}~~`))
                .join("\n")}
            />
            {record}
          </>
        );
      }
      if (!may(ActionId.keepMemories)) {
        return (
          <>
            <Text text={part.proposals.map((p) => `• ${p.text}`).join("\n")} />
            <Context items={[copy.noPermission]} />
          </>
        );
      }
      // The group is named after the part, so the buttons' target finds the ticks on Slack too.
      const name = partTarget(part);
      return (
        <>
          <Text text={`**${copy.memoryTitle(part.proposals.length)}**`} />
          <Checkboxes
            name={name}
            options={part.proposals.map((p) => ({
              id: p.id,
              label: p.text,
              description: p.scope,
            }))}
            selected={part.proposals.map((p) => p.id)}
          />
          <Actions blockId={`memory:${part.id}`}>
            <Button
              label={copy.keepSelected}
              style="primary"
              collects={name}
              action={act(ActionId.keepMemories)}
            />
            <Button label={copy.discardAll} action={act(ActionId.discardMemories)} />
          </Actions>
        </>
      );
    }
    case "budget": {
      const { meter } = part;
      const amount = (n: number) => formatAmount(n, meter.unit);
      // Whole dollars read better on a button than "$75.00".
      const short = (n: number) => (meter.unit === "usd" ? `$${n}` : amount(n));
      const once = may(ActionId.allowOnce);
      const raise = may(ActionId.raiseBudget);
      // Acknowledging a limit is offered only when the host lists it.
      const ack = part.allowed?.includes(ActionId.acknowledge) ?? false;
      return (
        <>
          <Callout
            tone={done ? "neutral" : "warn"}
            title={copy.budgetTitle}
            text={copy.budgetBody(
              amount(meter.used),
              amount(meter.limit),
              meter.period,
              meter.needs === undefined ? undefined : amount(meter.needs),
            )}
          />
          {record ??
            (once || raise || ack ? (
              <>
                <Actions blockId={`budget:${part.id}`}>
                  {once && (
                    <Button
                      label={copy.allowOnce}
                      style="primary"
                      action={act(ActionId.allowOnce)}
                    />
                  )}
                  {(raise ? (part.raiseTo ?? []) : []).map((limit) => (
                    <Button
                      key={limit}
                      label={copy.raiseTo(short(limit))}
                      action={act(ActionId.raiseBudget, { option: String(limit) })}
                    />
                  ))}
                  {ack && <Button label={copy.acknowledge} action={act(ActionId.acknowledge)} />}
                </Actions>
                {/* Offered amounts are the only ones the platform takes: no field beside them. */}
                {raise && !part.raiseTo?.length && (
                  <RaiseLimit
                    unit={meter.unit}
                    limit={meter.limit}
                    {...(part.raiseMax !== undefined ? { max: part.raiseMax } : {})}
                    action={act(ActionId.raiseBudget)}
                    copy={copy}
                  />
                )}
              </>
            ) : (
              <Context items={[copy.noPermission]} />
            ))}
        </>
      );
    }
    case "alert": {
      const over = part.alert.status === "resolved" || part.alert.status === "operational";
      return (
        <>
          <SystemAlertView alert={part.alert} copy={copy} />
          {record ??
            (over ? null : (
              <Actions blockId={`alert:${part.id}`}>
                {may(ActionId.acknowledge) && (
                  <Button label={copy.acknowledge} action={act(ActionId.acknowledge)} />
                )}
                {may(ActionId.snooze) && (
                  <Button label={copy.snooze} action={act(ActionId.snooze)} />
                )}
                {part.ruleUrl && <Button label={copy.openRule} url={part.ruleUrl} />}
              </Actions>
            ))}
        </>
      );
    }
    case "failure":
      return done ? (
        <>
          <Context items={[part.failure.message]} />
          {record}
        </>
      ) : part.allowed !== undefined && part.allowed.length === 0 ? (
        locked(part.failure.message)
      ) : (
        <FailureAlert turnId={message.id} failure={part.failure} copy={copy} />
      );
    case "reference":
      return <Sources items={part.citations} />;
    case "artifact":
      return <AttachmentView attachment={part.artifact} />;
    case "custom":
      return (
        <>
          <Context items={[part.fallback]} />
          {record}
        </>
      );
  }
}

/** What kind of message it is, and how urgent, in words: "Alert · Urgent". */
export function messageTag(message: InboxMessage, copy: Copy = defaultCopy): string {
  const severity = message.severity ? copy.severity[message.severity] : undefined;
  return [message.tag ?? copy.template[messageTemplate(message)], severity]
    .filter(Boolean)
    .join(" · ");
}

/** The envelope line: its tag, who sent it, when, and where it came from. */
export function messageByline(message: InboxMessage, copy: Copy = defaultCopy): string[] {
  const sources = [message.source, ...(message.sources ?? [])]
    .filter((s) => s !== undefined)
    .map((s) => (s.url ? `[${s.label}](${s.url})` : s.label));
  return [
    messageTag(message, copy),
    `**${message.from.name}**`,
    message.at ? timeToken(message.at) : null,
    sources.length > 0 ? copy.from(sources.join(", ")) : null,
  ].filter((x): x is string => Boolean(x));
}

/** A whole message: subject, envelope, its parts in order, then what the reader can do with it. */
export function MessageView({
  message,
  viewer,
  copy = defaultCopy,
}: { message: InboxMessage; viewer?: string } & WithCopy) {
  const open = messageState(message) === "open";
  const payload = { turnId: message.id, target: message.id };
  return (
    <>
      <Summary text={message.subject} />
      <Text text={`**${message.subject}**`} />
      <Context items={messageByline(message, copy)} />
      {message.parts.map((part) => (
        <MessagePartView key={part.id} message={message} part={part} viewer={viewer} copy={copy} />
      ))}
      {open && (
        <Actions blockId={`message:${message.id}`}>
          {message.source?.url && (
            <Button label={copy.openSource(message.source.label)} url={message.source.url} />
          )}
          <Menu
            label={copy.moreOptions}
            items={[
              { label: copy.snooze, action: { id: ActionId.snooze, payload } },
              { label: copy.dismiss, action: { id: ActionId.dismiss, payload } },
            ]}
          />
        </Actions>
      )}
    </>
  );
}

/** How many open messages one Slack Home tab shows in full; the rest are counted. */
const HOME_MESSAGES = 8;

/**
 * The mailbox as one scroll: open messages in full, most urgent first, then a
 * record of what was resolved. Slack publishes it as the App Home tab; the web
 * uses the two-pane `Mailbox` from `@warp/web` instead.
 */
export function MailboxView({
  messages,
  viewer,
  copy = defaultCopy,
}: { messages: InboxMessage[]; viewer?: string } & WithCopy) {
  const sorted = sortMessages(messages);
  const open = sorted.filter((m) => messageState(m) === "open");
  const waiting = open.filter((m) => openParts(m).length > 0).length;
  const resolved = sorted.filter((m) => messageState(m) === "resolved").slice(0, 10);
  return (
    <>
      <Summary text={`${copy.inbox}: ${copy.inboxCount(waiting)}`} />
      <Header text={copy.inbox} />
      <Context items={[copy.inboxCount(waiting)]} />
      {open.slice(0, HOME_MESSAGES).map((m) => (
        <Fragment key={m.id}>
          <Divider />
          <MessageView message={m} viewer={viewer} copy={copy} />
        </Fragment>
      ))}
      {open.length > HOME_MESSAGES && (
        <Context items={[`${open.length - HOME_MESSAGES} more in the full inbox`]} />
      )}
      {resolved.length > 0 && (
        <>
          <Divider />
          <Context items={[`**${copy.resolved}**`, ...resolved.map((m) => m.subject)]} />
        </>
      )}
    </>
  );
}
