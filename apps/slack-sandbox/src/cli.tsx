import { writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { AttachmentView } from "@warp/components";
import type { Turn } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { renderToSlack } from "@warp/render-slack";
import { streamTurn } from "@warp/slack-host";
import { eventsFor, registry, views } from "./fixtures";
import { buildManifest, CAPABILITIES, capabilitiesExcept, createAppUrl } from "./manifest";
import { serve } from "./serve";
import { readEnv, slackClient, uploadFile, validateBlocks } from "./slack";

const USAGE = `warp Slack sandbox

  pnpm sandbox manifest [out.json]   Generate the app manifest and a "create app" link
  pnpm sandbox list                  List postable fixtures
  pnpm sandbox validate              blocks.validate every fixture (no token needed)
  pnpm sandbox post <name|all>       Post fixtures to $SLACK_CHANNEL
  pnpm sandbox stream [turnFixture]  Stream a turn for real (startStream/appendStream/stopStream)
  pnpm sandbox upload                Upload a real file and post its artifact card
  pnpm sandbox serve                 Socket Mode: chat with the bot and click real buttons
`;

async function main() {
  const [command, arg] = process.argv.slice(2);
  switch (command) {
    case "manifest": {
      // A free workspace refuses custom functions: SLACK_SANDBOX_SKIP=workflowStep leaves them out.
      // A free workspace refuses custom functions: SLACK_SANDBOX_SKIP=workflowStep leaves them out.
      const capabilities = capabilitiesExcept(process.env.SLACK_SANDBOX_SKIP);
      const manifest = buildManifest({ capabilities });
      // pnpm --filter runs in the package dir; resolve paths from where the user ran the command.
      const out = resolve(process.env.INIT_CWD ?? process.cwd(), arg ?? "slack-manifest.json");
      writeFileSync(out, `${JSON.stringify(manifest, null, 2)}\n`);
      console.log(`Wrote ${out}\n`);
      for (const [name, cap] of Object.entries(CAPABILITIES)) {
        const mark = capabilities.includes(name) ? "" : "(skipped) ";
        console.log(
          `  ${name.padEnd(17)} ${mark}${cap.scopes.join(", ") || "(no scope)"}  ${cap.why}`,
        );
      }
      console.log(`\nCreate the app from this manifest:\n${createAppUrl(manifest)}\n`);
      console.log(
        "Then: install to the workspace, copy the Bot token (xoxb-…), create an App-level token with connections:write (xapp-…), and put both in .envrc.local.",
      );
      return;
    }
    case "list":
      for (const p of registry())
        console.log(`${p.name}${p.placement ? `  (${p.placement})` : ""}`);
      return;
    case "validate": {
      let failed = 0;
      for (const p of registry()) {
        const { pages, warnings } = renderToSlack(p.element, {
          placement: p.placement,
          // The sandbox opens amountModal for Raise Limit (see serve.tsx).
          amountForm: true,
        });
        // A fixture that continues across messages is only valid if every message is.
        for (const [i, blocks] of pages.entries()) {
          const view =
            p.placement === "modal"
              ? { type: "modal", title: { type: "plain_text", text: "warp" }, blocks }
              : p.placement === "home"
                ? { type: "home", blocks }
                : undefined;
          const res = await validateBlocks(blocks, view);
          if (!res.ok) failed++;
          const detail = res.ok ? "" : ` ${JSON.stringify(res.errors ?? res)}`;
          const part = pages.length > 1 ? ` [${i + 1}/${pages.length}]` : "";
          console.log(
            `${res.ok ? "ok  " : "FAIL"} ${p.name}${part}${warnings.length ? ` (${warnings.length} warnings)` : ""}${detail}`,
          );
        }
      }
      for (const v of views()) {
        const res = await validateBlocks([], v.view);
        if (!res.ok) failed++;
        console.log(
          `${res.ok ? "ok  " : "FAIL"} ${v.name}${res.ok ? "" : ` ${JSON.stringify(res.errors ?? res)}`}`,
        );
      }
      if (failed) process.exitCode = 1;
      return;
    }
    case "post": {
      const env = readEnv();
      const slack = slackClient(env.botToken);
      const wanted = registry().filter((p) => arg === "all" || p.name === arg);
      if (wanted.length === 0) throw new Error(`No fixture "${arg}". Try: pnpm sandbox list`);
      const header = await slack.call<{ ts: string }>("chat.postMessage", {
        channel: env.channel,
        text: `warp sandbox · ${wanted.length} fixture${wanted.length > 1 ? "s" : ""} · ${new Date().toISOString()}`,
      });
      for (const p of wanted) {
        if (p.placement === "modal") {
          console.log(`skip ${p.name}: modals need a trigger_id (use serve)`);
          continue;
        }
        const { blocks, pages, text, warnings } = renderToSlack(p.element, {
          placement: p.placement,
          amountForm: true,
        });
        if (blocks.length === 0) {
          // Nothing to show is a valid render (a fresh session has no notices), but Slack rejects it.
          console.log(`skip ${p.name}: renders no blocks`);
          continue;
        }
        try {
          for (const [i, page] of pages.entries()) {
            await slack.call("chat.postMessage", {
              channel: env.channel,
              thread_ts: header.ts,
              text: pages.length > 1 ? `(${i + 1}/${pages.length}) ${text}`.slice(0, 4000) : text,
              blocks: page,
              // Label each post so the thread reads as a catalog.
              metadata: { event_type: "warp_fixture", event_payload: { name: p.name } },
            });
          }
        } catch (e) {
          // One bad fixture shouldn't hide the rest of the catalog.
          console.log(`FAIL ${p.name}: ${(e as Error).message}`);
          process.exitCode = 1;
          continue;
        }
        console.log(
          `posted ${p.name}${warnings.length ? ` (warnings: ${warnings.join("; ")})` : ""}`,
        );
      }
      return;
    }
    case "stream": {
      const env = readEnv();
      if (!env.testUser)
        throw new Error("SLACK_TEST_USER (your member id, U…) is required to stream in a channel.");
      const slack = slackClient(env.botToken);
      const auth = await slack.call<{ team_id: string }>("auth.test");
      const turn = (fx as Record<string, unknown>)[arg ?? "doneTurn"] as Turn | undefined;
      if (!turn?.tasks) throw new Error(`"${arg}" is not a turn fixture`);
      const root = await slack.call<{ ts: string }>("chat.postMessage", {
        channel: env.channel,
        text: `Streaming \`${arg ?? "doneTurn"}\` for <@${env.testUser}>`,
      });
      const { turn: final } = await streamTurn(
        slack,
        {
          channel: env.channel,
          threadTs: root.ts,
          triggerTs: root.ts,
          recipientUser: env.testUser,
          recipientTeam: auth.team_id,
        },
        eventsFor(turn),
        { log: (line) => console.warn(`  ${line}`) },
      );
      console.log(`streamed → ${final.status}`);
      return;
    }
    case "upload": {
      // A real file, so the artifact card's Download/Open point at Slack's own copy.
      const env = readEnv();
      const slack = slackClient(env.botToken);
      const csv = [
        "reason,count,amount_usd",
        "duplicate,412,18240.10",
        "fraud,97,9120.00",
        "not_received,775,31008.55",
      ].join("\n");
      const root = await slack.call<{ ts: string }>("chat.postMessage", {
        channel: env.channel,
        text: "warp sandbox · artifact upload",
      });
      const uploaded = await uploadFile(
        slack,
        {
          name: "refunds-q3.csv",
          bytes: new TextEncoder().encode(csv),
          title: "Q3 refunds by reason",
        },
        { channel: env.channel, threadTs: root.ts },
      );
      const card = renderToSlack(
        <AttachmentView
          attachment={{
            ...fx.artifacts.ready,
            title: "refunds-q3.csv",
            kind: "data",
            sizeBytes: csv.length,
            url: uploaded.permalink,
            downloadUrl: uploaded.permalink,
            description: "3 refund reasons, Q3 totals",
          }}
        />,
      );
      await slack.call("chat.postMessage", { channel: env.channel, thread_ts: root.ts, ...card });
      console.log(`uploaded ${uploaded.id} ${uploaded.permalink ?? ""}`);
      return;
    }
    case "serve": {
      const env = readEnv();
      await serve(slackClient(env.botToken), env);
      return;
    }
    default:
      console.log(USAGE);
  }
}

main().catch((e: Error) => {
  console.error(e.message);
  process.exit(1);
});
