import { App } from "./app.tsx";
import { NEEDED, readConfig, USEFUL } from "./config.ts";
import { httpHumagen } from "./humagen.ts";
import { fileJournal, journaled, recoverStreams } from "./journal.ts";
import { slackClient, socketMode } from "./slack.ts";

const log = (line: string) => console.log(`${new Date().toISOString()} ${line}`);

async function main() {
  const config = readConfig();
  for (const note of config.notes) log(`note: ${note}`);
  const humagen = httpHumagen({ baseUrl: config.humagen.url, token: config.humagen.token });

  // The credential decides the workspace. The configured name only has to agree with it.
  const { principal: me, permissions } = await humagen.getMe();
  if (me.tenant !== config.humagen.tenant) {
    throw new Error(
      "The humagen credential is for another workspace than HUMAGEN_TENANT. Refusing to start.",
    );
  }
  const lacking = [...NEEDED, ...(config.humagen.client ? ["clients:read"] : [])].filter(
    (p) => !permissions.includes(p),
  );
  if (lacking.length > 0) {
    throw new Error(`The humagen credential lacks ${lacking.join(", ")}.`);
  }
  // More than the app can use is allowed, but every button in Slack acts with it.
  const extra = permissions.filter((p) => !USEFUL.includes(p));
  if (extra.length > 0) {
    log(
      `warning: the humagen credential also grants ${extra.join(", ")}, which this app never uses`,
    );
  }

  // The bot token decides the team in the same way.
  const bot = slackClient(config.slack.botToken);
  const who = await bot.call<{ team_id: string; user_id: string }>("auth.test");
  if (who.team_id !== config.slack.team) {
    throw new Error(
      "The Slack bot token is for another team than SLACK_TEAM_ID. Refusing to start.",
    );
  }

  // Streams the last process left open are closed before any new one opens.
  const journal = fileJournal(config.slack.streamJournal, log);
  const recovered = await recoverStreams(bot, journal, log);
  if (recovered > 0) log(`closed ${recovered} stream(s) the last run of the app left open`);
  const slack = journaled(bot, journal);

  const app = new App(slack, humagen, {
    team: config.slack.team,
    botUser: who.user_id,
    subject: me.subject,
    tenant: me.tenant,
    operators: config.slack.operators,
    clientId: config.humagen.client,
    defaultAgent: config.humagen.defaultAgent,
    publicUrl: config.humagen.publicUrl,
    branding: config.slack.branding,
    log,
  });

  const stop = new AbortController();
  for (const signal of ["SIGINT", "SIGTERM"] as const) process.once(signal, () => stop.abort());
  stop.signal.addEventListener("abort", () => {
    log("stopping");
    app.close();
    // Turns in flight finish their Slack writes, then the process ends.
    const patience = new Promise((r) => setTimeout(r, 10_000));
    Promise.race([app.idle(), patience]).finally(() => process.exit(0));
  });

  // The inbox is followed beside the socket. If humagen goes away it is followed
  // again, and each time the stream opens the app catches up on the
  // announcements of its bound channels, which the stream does not replay.
  (async () => {
    while (!stop.signal.aborted) {
      await app.watchInbox(stop.signal).catch((e: Error) => log(`inbox watch ended: ${e.message}`));
      await new Promise((r) => setTimeout(r, 5_000));
    }
  })();

  // Resolves on shutdown. Rejects when Slack cannot be reached again, and the
  // process exits non-zero so its supervisor restarts it.
  log(`serving workspace ${me.tenant} as ${me.subject}`);
  await socketMode(slackClient(config.slack.appToken), (envelope) => app.handle(envelope), {
    log,
    signal: stop.signal,
    ...config.slack.socket,
  });
}

main().catch((e: Error) => {
  console.error(e.message);
  process.exit(1);
});
