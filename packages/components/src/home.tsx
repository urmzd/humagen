import {
  ANY_AGENT,
  firstName,
  HOME_NOTIFICATIONS,
  type HomeModel,
  type HomeNotification,
  homeAction,
  notificationsFromWaiting,
  orderNotifications,
  PERSONAL_ACCOUNT,
  timeToken,
} from "@warp/core";
import {
  Actions,
  Button,
  Context,
  Divider,
  Header,
  Only,
  Prompt,
  Row,
  Select,
  Summary,
  Text,
  Tiles,
  useSurface,
} from "@warp/primitives";
import { type Copy, defaultCopy } from "./copy";

function link(label: string, url?: string) {
  return url ? `[${label}](${url})` : null;
}

/** Slack opens only absolute web addresses; anything else would be a broken link there. */
const ABSOLUTE = /^https?:\/\/[^\s]+$/i;

/** Square brackets would end a link's text early. */
const escapeLabel = (text: string) => text.replace(/[[\]\\]/g, (ch) => `\\${ch}`);

/**
 * Where a person lands: a greeting, ways to start, their notifications (inbox
 * items waiting on them and open alerts, each linking to its own page), who
 * answers their messages, what is connected, and what they may do.
 *
 * One definition for both surfaces. The web gets the ask box and a grid of
 * starts; Slack, which has its own composer, gets three buttons, a select for
 * who answers direct messages, and access folded into one line with a link
 * to the console. Publish it on Slack with `renderToSlack(..., { placement: "home" })`
 * or `homeView` from `@warp/render-slack`.
 */
export function Home({ home, copy = defaultCopy }: { home: HomeModel; copy?: Copy }) {
  const slack = useSurface().surface === "slack";
  const c = copy.home;
  const greeting = c.greeting(firstName(home.person), home.assistant.name);
  const handle = home.assistant.handle ?? home.assistant.name;
  // Hosts built before notifications still send `waiting`.
  const notes = home.notifications ?? notificationsFromWaiting(home.waiting ?? []);
  const linkable = (url?: string) => (url && (!slack || ABSOLUTE.test(url)) ? url : undefined);
  const ordered = orderNotifications(notes.items);
  const shown = ordered.slice(0, HOME_NOTIFICATIONS);
  const of = (list: HomeNotification[], kind: HomeNotification["kind"]) =>
    list.filter((n) => n.kind === kind).length;
  const inboxTotal = Math.max(notes.inbox?.total ?? 0, of(ordered, "inbox"));
  const alertTotal = Math.max(notes.alerts?.total ?? 0, of(ordered, "alert"));
  const moreInbox = inboxTotal - of(shown, "inbox");
  const moreAlerts = alertTotal - of(shown, "alert");
  const more = [
    moreInbox > 0
      ? (link(c.moreWaiting(moreInbox), linkable(notes.inbox?.url)) ?? c.moreWaiting(moreInbox))
      : null,
    moreAlerts > 0
      ? (link(c.moreAlerts(moreAlerts), linkable(notes.alerts?.url)) ?? c.moreAlerts(moreAlerts))
      : null,
  ].filter((x): x is string => x !== null);
  const agents = home.answeredBy
    ? [
        { id: ANY_AGENT, label: c.anyAgent },
        ...home.answeredBy.agents.map((a) => ({ id: a.id, label: a.name })),
      ]
    : null;
  const selected = home.answeredBy?.selected ?? ANY_AGENT;
  const connections = home.connections.filter((x) => !(slack && x.id === "slack"));

  return (
    <>
      <Summary text={`${greeting} ${c.notificationCount(inboxTotal + alertTotal)}.`} />
      <Header text={greeting} level={1} />
      <Text text={`${home.intro} ${slack ? c.reachSlack(handle) : c.reachWeb}`} />

      <Only surface="web">
        {home.ask && (
          <Prompt
            label={c.askLabel}
            placeholder={home.ask.placeholder ?? c.askPlaceholder}
            submit={{ label: c.ask, action: homeAction.ask() }}
            {...(home.ask.notices?.length ? { notices: home.ask.notices } : {})}
          >
            {agents && (
              <Select
                label={c.answeredBy}
                options={agents}
                selected={selected}
                action={homeAction.answeredBy()}
              />
            )}
            {home.ask.attach && <Button label={c.attach} action={homeAction.attach()} />}
          </Prompt>
        )}
        <Tiles
          label={c.startHere}
          items={home.starts.map((s) => ({
            id: s.id,
            label: c.starts[s.id]?.label ?? s.id,
            description: c.starts[s.id]?.description,
            ...(s.count ? { badge: s.count } : {}),
            ...(s.url ? { url: s.url } : { action: homeAction.start(s.id) }),
          }))}
        />
      </Only>
      <Only surface="slack">
        <Actions blockId="home:start">
          {home.starts
            .filter((s) => c.starts[s.id]?.slack)
            .map((s) => (
              <Button
                key={s.id}
                label={c.starts[s.id]?.slack ?? s.id}
                action={s.id === "talk" ? homeAction.ask() : homeAction.start(s.id)}
                {...(s.id === "talk" ? { style: "primary" as const } : {})}
              />
            ))}
        </Actions>
      </Only>

      <Divider />
      <Header text={c.needsYou} level={2} />
      {shown.length === 0 && <Context items={[c.nothingWaiting]} />}
      {shown.map((n) => {
        const url = linkable(n.url);
        const who = `**${n.from}** ${n.summary}`;
        const meta = [
          c.notificationTag(n.kind, n.severity),
          n.at ? timeToken(n.at) : null,
          n.detail,
        ]
          .filter(Boolean)
          .join(" · ");
        return (
          <Row
            key={`${n.kind}:${n.id}`}
            // The web links the title itself. Slack shows the address, so it is clear where it goes.
            text={
              url && !slack
                ? `[**${escapeLabel(n.from)}** ${escapeLabel(n.summary)}](<${url}>)`
                : who
            }
            detail={url && slack ? `${meta}\n${copy.runLink(url)}` : meta}
          >
            {n.kind === "inbox" && !url && (
              <Button
                label={c.review}
                action={homeAction.review(n)}
                accessibilityLabel={`${c.review}: ${n.from} ${n.summary}`}
              />
            )}
          </Row>
        );
      })}
      {more.length > 0 && <Context items={more} />}

      {agents && (
        <Only surface="slack">
          <Divider />
          <Header text={c.whoAnswers} level={2} />
          <Select
            label={c.dmRouting}
            options={agents}
            selected={selected}
            action={homeAction.answeredBy()}
          />
        </Only>
      )}

      <Divider />
      <Header text={c.connections} level={2} />
      {!slack && home.connectionsUrl && (
        <Context items={[link(c.manageAll, home.connectionsUrl) as string]} />
      )}
      {connections.map((x) => (
        <Row
          key={x.id}
          text={`**${x.name}**`}
          detail={
            x.connected
              ? slack
                ? c.connectedHere
                : x.description
              : x.canConnect
                ? c.notConnected
                : c.adminCanAdd
          }
          {...(x.connected && !slack
            ? {
                status: {
                  tone: "ok" as const,
                  text: x.account ? c.connectedTo(x.account) : c.connected,
                },
              }
            : {})}
        >
          {!x.connected && x.canConnect && (
            <Button
              label={c.connect}
              action={homeAction.connect(x.id)}
              accessibilityLabel={`${c.connect} ${x.name}`}
            />
          )}
        </Row>
      ))}
      {home.personal && (
        <Row
          text={`**${c.personal}**`}
          detail={[
            slack ? c.personalSlack : c.personalWeb,
            home.personal.connected > 0 ? c.personalCount(home.personal.connected) : null,
          ]
            .filter(Boolean)
            .join(" ")}
        >
          {home.personal.canConnect && (
            <Button label={c.connectAccount} action={homeAction.connect(PERSONAL_ACCOUNT)} />
          )}
        </Row>
      )}

      <Divider />
      <Only surface="slack">
        <Context
          items={[
            [
              c.signedInAs(home.person.name, home.access.role),
              home.consoleUrl ? c.openConsole(home.consoleUrl) : null,
            ]
              .filter(Boolean)
              .join(" "),
          ]}
        />
      </Only>
      <Only surface="web">
        <Header text={c.access} level={2} />
        <Text
          text={
            home.access.role
              ? [
                  c.signedIn(home.access.via, home.access.role, home.access.group),
                  home.access.allows ? c.allows(home.access.allows) : null,
                  home.access.denies ? c.denies(home.access.denies) : null,
                ]
                  .filter(Boolean)
                  .join(" ")
              : c.noRole(home.access.via)
          }
        />
        <AccessLinks home={home} copy={copy} />
        {home.links.length > 0 && (
          <>
            <Divider />
            <Context items={home.links.map((l) => `[${l.label}](${l.url})`)} />
          </>
        )}
      </Only>
    </>
  );
}

function AccessLinks({ home, copy }: { home: HomeModel; copy: Copy }) {
  const c = copy.home;
  const items = [
    link(home.access.role ? c.why : c.whyNot, home.access.explainUrl),
    link(c.policy, home.access.policyUrl),
  ].filter((x): x is string => x !== null);
  return items.length > 0 ? <Context items={items} /> : null;
}
