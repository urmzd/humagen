import { TurnView } from "@agent-ui/components";
import { ActionId, decodeAction, type Turn, type TurnEvent } from "@agent-ui/core";
import {
  actionIdBase,
  reactionChanges,
  renderToSlack,
  suggestedPrompts,
} from "@agent-ui/render-slack";
import { eventsFor, scenarioFor } from "./fixtures";
import { streamTurn } from "./run";
import type { SandboxEnv, SlackClient } from "./slack";

/**
 * Socket Mode handler: talk to the sandbox bot in Slack and click the real
 * buttons. DM it or @mention it; keywords pick a scenario (deploy/rollback →
 * approval, fail → failure, code/test → coding agent, anything else → answer).
 */

interface Live {
  turn: Turn;
  channel: string;
  ts: string;
  threadTs: string;
  triggerTs: string;
  user: string;
  team: string;
}

const PROMPTS = {
  suggestions: [
    { title: "Why is payments-api erroring?", message: "Why is payments-api returning 5xx?" },
    { title: "Roll back the last deploy", message: "Roll back the last payments-api deploy" },
    { title: "Run the payments tests", message: "Run the payments tests and fix the migration" },
    { title: "Show a failure", message: "Simulate a failure" },
  ],
};

export async function serve(slack: SlackClient, env: SandboxEnv) {
  if (!env.appToken) {
    throw new Error("SLACK_APP_TOKEN (xapp-…, connections:write) is required for Socket Mode.");
  }
  const app = (await import("./slack")).slackClient(env.appToken);
  const live = new Map<string, Live>(); // by turn id
  const running = new Map<string, AbortController>(); // by thread ts

  const run = async (target: Omit<Live, "turn" | "ts">, events: TurnEvent[]) => {
    const abort = new AbortController();
    running.set(target.threadTs, abort);
    try {
      const { turn, ts } = await streamTurn(
        slack,
        {
          channel: target.channel,
          threadTs: target.threadTs,
          triggerTs: target.triggerTs,
          recipientUser: target.user,
          recipientTeam: target.team,
        },
        events,
        { signal: abort.signal },
      );
      live.set(turn.id, { ...target, turn, ts });
      console.log(`  turn ${turn.id} → ${turn.status}`);
    } finally {
      running.delete(target.threadTs);
    }
  };

  const onEvent = async (event: Record<string, string>, teamId: string) => {
    if (event.type === "app_home_opened" && event.tab === "messages") {
      await slack.call("assistant.threads.setSuggestedPrompts", {
        channel_id: event.channel,
        ...suggestedPrompts(PROMPTS, "Try the sandbox"),
      });
      return;
    }
    if (event.type === "agent_session_stopped") {
      running.get(event.thread_ts as string)?.abort();
      return;
    }
    const isTrigger =
      event.type === "app_mention" ||
      (event.type === "message" && event.channel_type === "im" && !event.subtype);
    if (!isTrigger || event.bot_id) return;
    const scenario = scenarioFor(event.text ?? "");
    const turnId = `${scenario.id}-${Date.now()}`;
    await run(
      {
        channel: event.channel as string,
        threadTs: event.thread_ts ?? (event.ts as string),
        triggerTs: event.ts as string,
        user: event.user as string,
        team: teamId,
      },
      eventsFor({ ...scenario, id: turnId }),
    );
  };

  const onAction = async (payload: {
    user: { id: string };
    channel: { id: string };
    message?: { ts: string; thread_ts?: string };
    actions: { action_id: string; value?: string }[];
  }) => {
    const action = payload.actions[0];
    if (!action?.value) return;
    const id = actionIdBase(action.action_id);
    const { turnId, target } = decodeAction(action.value);
    const state = live.get(turnId);
    const whisper = (text: string) =>
      slack.call("chat.postEphemeral", {
        channel: payload.channel.id,
        user: payload.user.id,
        // Reply where the button was clicked, not in the channel above the thread.
        ...(payload.message?.thread_ts ? { thread_ts: payload.message.thread_ts } : {}),
        text,
      });

    if (id === ActionId.feedback) return whisper("Thanks, feedback recorded.");
    if (id === ActionId.reconnect) return whisper("In a real app this opens the reconnect flow.");
    if (id === ActionId.report) return whisper("Reported. Reference copied to the incident log.");
    if (!state) return whisper("This sandbox restarted, so that turn is gone. Ask again.");

    if (id === ActionId.approve || id === ActionId.deny) {
      const at = new Date().toISOString();
      const by = `<@${payload.user.id}>`;
      const turn: Turn = {
        ...state.turn,
        interrupts: state.turn.interrupts.map((i) =>
          i.type === "approval" && i.id === target
            ? {
                ...i,
                decision:
                  id === ActionId.approve
                    ? { kind: "approved", by, at }
                    : { kind: "denied", by, at },
              }
            : i,
        ),
        status: "done",
      };
      // Close the loop in place: buttons become a record of the decision.
      const view = renderToSlack(<TurnView turn={turn} streamed />);
      await slack.call("chat.update", {
        channel: state.channel,
        ts: state.ts,
        text: view.text,
        blocks: view.blocks,
      });
      live.set(turnId, { ...state, turn });
      // The turn no longer needs a person: clear the waiting reaction. An approval's
      // follow-up run adds its own; a denial ends here, so it gets the final one now.
      const { add, remove } = reactionChanges("waiting", turn.status);
      for (const [method, names] of [
        ["reactions.remove", remove],
        ["reactions.add", id === ActionId.deny ? add : []],
      ] as const) {
        for (const name of names) {
          await slack
            .call(method, { channel: state.channel, timestamp: state.triggerTs, name })
            .catch(() => {});
        }
      }
      if (id === ActionId.approve) {
        await run({ ...state, triggerTs: state.triggerTs }, followUp(turnId));
      }
      return;
    }
    if (id === ActionId.retry) {
      await run(state, eventsFor({ ...scenarioFor("answer"), id: `${turnId}-retry` }));
    }
  };

  const connect = async () => {
    const { url } = await app.call<{ url: string }>("apps.connections.open");
    const ws = new WebSocket(url);
    ws.onmessage = (message) => {
      const envelope = JSON.parse(String(message.data)) as {
        envelope_id?: string;
        type: string;
        payload?: Record<string, unknown>;
      };
      // Acknowledge first: Slack retries anything not acked within 3 seconds.
      if (envelope.envelope_id) ws.send(JSON.stringify({ envelope_id: envelope.envelope_id }));
      const p = envelope.payload ?? {};
      const work =
        envelope.type === "events_api"
          ? onEvent(p.event as Record<string, string>, p.team_id as string)
          : envelope.type === "interactive" && p.type === "block_actions"
            ? onAction(p as never)
            : envelope.type === "disconnect"
              ? Promise.resolve(ws.close())
              : Promise.resolve();
      work.catch((e: Error) => console.error(`  handler failed: ${e.message}`));
    };
    ws.onclose = () => {
      console.log("socket closed, reconnecting");
      setTimeout(() => connect().catch((e) => console.error(e)), 1000);
    };
    ws.onopen = () =>
      console.log("connected. DM the sandbox bot or @mention it in the test channel.");
  };
  await connect();
}

/** After an approval, the agent runs the action and reports the result. */
function followUp(turnId: string): TurnEvent[] {
  const id = `${turnId}-run`;
  return eventsFor({
    id,
    agent: { name: "Assistant" },
    status: "done",
    durationMs: 9_000,
    planTitle: "Rolling back",
    text: "Rolled back **payments-api** to `#4811`. The 5xx rate is back under 0.1%.",
    tasks: [
      { id: "r1", title: "Rolled back payments-api", status: "complete", output: "#4812 → #4811" },
      {
        id: "r2",
        title: "Watched the error rate",
        status: "complete",
        output: "0.08% after 2 min",
      },
    ],
    citations: [],
    interrupts: [],
    notices: [],
  });
}
