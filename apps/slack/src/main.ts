import { App } from "./app.tsx";
import { readConfig } from "./config.ts";
import { fileJournal, journaled, recoverStreams } from "./journal.ts";
import { slackClient, socketMode } from "./slack.ts";
import { httpWarp } from "./warp.ts";

/** The permissions the app cannot work without. Others unlock more of the inbox, see the README. */
const NEEDED = ["runs:read", "runs:write", "agents:read"];
/** What some buttons can use when granted. Anything beyond is more than the app needs. */
const USEFUL = [...NEEDED, "clients:read", "agents:write", "alerts:read", "alerts:write"];

const log = (line: string) => console.log(`${new Date().toISOString()} ${line}`);

async function main() {
  const config = readConfig();
  const warp = httpWarp({ baseUrl: config.warp.url, token: config.warp.token });

  // The credential decides the workspace. The configured name only has to agree with it.
  const { principal: me, permissions } = await warp.getMe();
  if (me.tenant !== config.warp.tenant) {
    throw new Error(
      "The warp credential is for another workspace than WARP_TENANT. Refusing to start.",
    );
  }
  const lacking = [...NEEDED, ...(config.warp.client ? ["clients:read"] : [])].filter(
    (p) => !permissions.includes(p),
  );
  if (lacking.length > 0) {
    throw new Error(`The warp credential lacks ${lacking.join(", ")}.`);
  }
  // More than the app can use is allowed, but every button in Slack acts with it.
  const extra = permissions.filter((p) => !USEFUL.includes(p));
  if (extra.length > 0) {
    log(`warning: the warp credential also grants ${extra.join(", ")}, which this app never uses`);
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

  const app = new App(slack, warp, {
    team: config.slack.team,
    botUser: who.user_id,
    subject: me.subject,
    tenant: me.tenant,
    operators: config.slack.operators,
    clientId: config.warp.client,
    defaultAgent: config.warp.defaultAgent,
    publicUrl: config.warp.publicUrl,
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

  // The inbox is followed beside the socket. If warp goes away it is followed again.
  (async () => {
    while (!stop.signal.aborted) {
      await app.watchInbox(stop.signal).catch((e: Error) => log(`inbox watch ended: ${e.message}`));
      await new Promise((r) => setTimeout(r, 5_000));
    }
  })();

  log(`serving workspace ${me.tenant} as ${me.subject}`);
  await socketMode(slackClient(config.slack.appToken), (envelope) => app.handle(envelope), {
    log,
    signal: stop.signal,
  });
}

main().catch((e: Error) => {
  console.error(e.message);
  process.exit(1);
});
