import {
  ANY_AGENT,
  firstName,
  HOME_NOTIFICATIONS,
  HOME_PROJECTS_MAX,
  type HomeAccess,
  type HomeModel,
  type HomeNotification,
  homeAction,
  notificationsFromWaiting,
  orderNotifications,
  orderProjects,
  orderUpdates,
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
 * items waiting on them and open alerts, each linking to its own page), what's
 * new in their projects, their projects, who answers their messages, what is
 * connected, and what they may do. A section the host leaves out is not shown.
 *
 * One definition for both surfaces. The web gets the ask box and a grid of
 * starts; Slack, which has its own composer, gets its start buttons, a select
 * for who answers direct messages, and access folded into one line with a
 * link to the console. An announcement or a project links its title on the
 * web; on Slack an Open button beside it does. Publish it on Slack with
 * `renderToSlack(..., { placement: "home" })` or `homeView` from
 * `@warp/render-slack`.
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
  const updates = home.updates ? orderUpdates(home.updates) : null;
  const projects = home.projects ? orderProjects(home.projects) : null;
  const shownProjects = projects?.slice(0, HOME_PROJECTS_MAX) ?? [];
  const moreProjects = (projects?.length ?? 0) - shownProjects.length;
  /** A title that opens where it lives: a link on the web. Slack shows an Open button instead. */
  const titled = (text: string, url: string | undefined, bold: boolean) => {
    const shown = bold ? `**${text}**` : text;
    if (!url || slack) return shown;
    const label = escapeLabel(text);
    return `[${bold ? `**${label}**` : label}](<${url}>)`;
  };
  const open = (url: string | undefined, what: string) =>
    url && slack ? (
      <Button label={c.open} url={url} accessibilityLabel={`${c.open} ${what}`} />
    ) : null;

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
                // A start the host gives an address for opens it; Slack opens only an absolute one.
                {...(s.id !== "talk" && linkable(s.url)
                  ? { url: s.url }
                  : { action: s.id === "talk" ? homeAction.ask() : homeAction.start(s.id) })}
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

      {updates && (
        <>
          <Divider />
          <Header text={c.whatsNew} level={2} />
          {updates.length === 0 && <Context items={[c.nothingNew]} />}
          {updates.map((u) => {
            const url = linkable(u.url);
            return (
              <Row
                key={`update:${u.id}`}
                text={titled(u.subject, url, u.unread)}
                detail={[
                  u.unread ? copy.mailbox.unread : null,
                  copy.announcements.postedIn(u.projectTitle),
                  u.at ? timeToken(u.at) : null,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              >
                {open(url, u.subject)}
              </Row>
            );
          })}
        </>
      )}

      {projects && (
        <>
          <Divider />
          <Header text={c.yourProjects} level={2} />
          {projects.length === 0 && <Context items={[c.noProjects]} />}
          {shownProjects.map((p) => {
            const url = linkable(p.url);
            return (
              <Row
                key={`project:${p.name}`}
                text={titled(p.title, url, true)}
                detail={[
                  copy.projects.kind[p.kind] ?? p.kind,
                  p.within.length > 0 ? copy.projects.within(p.within) : null,
                  p.active > 0 ? copy.projects.count.running(p.active) : null,
                  p.activeAt ? copy.projects.count.active(timeToken(p.activeAt)) : null,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              >
                {open(url, p.title)}
              </Row>
            );
          })}
          {moreProjects > 0 && (
            <Context
              items={[
                link(c.moreProjects(moreProjects), linkable(home.projectsUrl)) ??
                  c.moreProjects(moreProjects),
              ]}
            />
          )}
        </>
      )}

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

      {(connections.length > 0 || home.personal) && (
        <>
          <Divider />
          <Header text={c.connections} level={2} />
          {!slack && home.connectionsUrl && (
            <Context items={[link(c.manageAll, home.connectionsUrl) as string]} />
          )}
        </>
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

      <Only surface="slack">
        {(home.access || home.consoleUrl) && (
          <>
            <Divider />
            <Context
              items={[
                [
                  home.access ? c.signedInAs(home.person.name, home.access.role) : null,
                  home.consoleUrl ? c.openConsole(home.consoleUrl) : null,
                ]
                  .filter(Boolean)
                  .join(" "),
              ]}
            />
          </>
        )}
      </Only>
      <Only surface="web">
        {home.access && (
          <>
            <Divider />
            <Access access={home.access} copy={copy} />
          </>
        )}
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

/** What the person may do, and why: the web's access section. */
function Access({ access, copy }: { access: HomeAccess; copy: Copy }) {
  const c = copy.home;
  const items = [
    link(access.role ? c.why : c.whyNot, access.explainUrl),
    link(c.policy, access.policyUrl),
  ].filter((x): x is string => x !== null);
  return (
    <>
      <Header text={c.access} level={2} />
      <Text
        text={
          access.role
            ? [
                c.signedIn(access.via, access.role, access.group),
                access.allows ? c.allows(access.allows) : null,
                access.denies ? c.denies(access.denies) : null,
              ]
                .filter(Boolean)
                .join(" ")
            : c.noRole(access.via)
        }
      />
      {items.length > 0 && <Context items={items} />}
    </>
  );
}
