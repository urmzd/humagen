import { defaultCopy, Home, RequestView, SystemAlertView, TurnView } from "@vultus/components";
import {
  ActionId,
  type Approval,
  feedbackEvent,
  type HomeModel,
  reduce,
  type Turn,
  type TurnEvent,
} from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { Context } from "@vultus/primitives";
import {
  AMOUNT_MODAL,
  amountModal,
  askForm,
  decodeAmountModal,
  decodeAskForm,
  decodeFeedbackModal,
  decodeSlackAction,
  ENTRY,
  FEEDBACK_MODAL,
  feedbackModal,
  homeView,
  reactionChanges,
  renderToSlack,
  suggestedPrompts,
} from "@vultus/render-slack";
import { streamTurn } from "@vultus/slack-host";
import { eventsFor, SCENARIOS, scenarioFor } from "./fixtures";
import { type SandboxEnv, type SlackClient, SlackError, slackClient } from "./slack";

/**
 * Socket Mode handler: talk to the sandbox bot in Slack and click the real
 * buttons. DM it or @mention it; keywords pick a scenario (see SCENARIOS, or
 * say "help"). "status" posts a system alert and resolves it in place.
 *
 * The same turns also start from the shortcuts menu, a message's menu, /agent
 * and a Workflow Builder step (see ENTRY), and the Home tab publishes the
 * `Home` fixture for whoever opens it.
 */

interface Live {
  turn: Turn;
  channel: string;
  ts: string;
  threadTs: string;
  triggerTs: string;
  user: string;
  team: string;
  /** Set when a workflow is waiting on this turn: it is told how the turn ended. */
  step?: { id: string; token?: string };
}

type FormMeta = { kind: "ask" | "question" | "other" | "edit"; turnId?: string; target?: string };

/** The shared ask form, titled for this app, with what the words are for in `meta`. */
function form(title: string, label: string, meta: FormMeta, initial?: string) {
  return askForm({ title, label, meta, ...(initial ? { initial } : {}) });
}

/** Slack shows four: one per kind of thing worth trying first. */
const PROMPTS = {
  suggestions: ["add a skill", "update your prompt", "export with customer emails", "long report"]
    .map((say) => SCENARIOS.find((s) => s.say === say))
    .filter((s) => s !== undefined)
    .map((s) => ({ title: s.shows.charAt(0).toUpperCase() + s.shows.slice(1), message: s.say })),
};

const HELP = [
  "Say any of these to see it in Slack:",
  ...SCENARIOS.map((s) => `• \`${s.say}\`: ${s.shows}`),
  "• `status`: a system alert that resolves in place",
  "Anything else gets a plain answer.",
].join("\n");

/** Slack's encoded message text back to what the person typed: `&lt;` `&gt;` `&amp;` decoded. */
function fromSlack(text: string): string {
  return text.replaceAll("&lt;", "<").replaceAll("&gt;", ">").replaceAll("&amp;", "&");
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));

export async function serve(slack: SlackClient, env: SandboxEnv) {
  if (!env.appToken) {
    throw new Error("SLACK_APP_TOKEN (xapp-…, connections:write) is required for Socket Mode.");
  }
  const app = slackClient(env.appToken);
  const live = new Map<string, Live>(); // by turn id
  const running = new Map<string, AbortController>(); // by thread ts

  /** Tell a waiting workflow how its turn ended. A second answer for the same step is ignored. */
  const completeStep = async (
    step: Live["step"],
    status: string,
    answer: string,
    error?: string,
  ) => {
    if (!step) return;
    const as = step.token ? slackClient(step.token) : slack;
    await (error
      ? as.call("functions.completeError", { function_execution_id: step.id, error })
      : as.call("functions.completeSuccess", {
          function_execution_id: step.id,
          outputs: { status, answer },
        })
    ).catch((e: Error) => console.warn(`  step not completed: ${e.message}`));
  };

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
        { signal: abort.signal, log: (line) => console.warn(`  ${line}`) },
      );
      live.set(turn.id, { ...target, turn, ts });
      console.log(`  turn ${turn.id} → ${turn.status}`);
      // A turn that is waiting on a person keeps the workflow waiting too.
      if (turn.status !== "waiting") {
        const answer = turn.text.replace(/\s*\[\[[^\]]+\]\]/g, "");
        await completeStep(target.step, turn.status, answer, turn.failure?.message);
      }
    } finally {
      running.delete(target.threadTs);
    }
  };

  /**
   * Start a turn somewhere that has no message to reply to yet (a shortcut, a
   * command, a workflow): say who asked what, then answer in that thread.
   */
  const start = async (
    where: { channel: string; user: string; team: string; step?: Live["step"] },
    text: string,
    via: string,
  ) => {
    // The thread hangs from what was asked, in the same component the web draws as a bubble.
    // Said under it: where the question came from ("Asked with `/agent`").
    const asked = renderToSlack(
      <>
        <RequestView by={`<@${where.user}>`} text={text} />
        <Context items={[`Asked ${via}`]} />
      </>,
    );
    const root = await slack.call<{ ts: string; channel: string }>("chat.postMessage", {
      channel: where.channel,
      text: asked.text,
      blocks: asked.blocks,
    });
    const scenario = scenarioFor(text);
    await run(
      { ...where, channel: root.channel, threadTs: root.ts, triggerTs: root.ts },
      eventsFor({ ...scenario, id: `${scenario.id}-${Date.now()}` }),
    );
  };
  /** The app can only post where it is a member; say so instead of failing silently. */
  const orExplain = async (work: Promise<void>, responseUrl: string | undefined) => {
    try {
      await work;
    } catch (e) {
      const away = e instanceof SlackError && /not_in_channel|channel_not_found/.test(e.code);
      if (!away || !responseUrl) throw e;
      await slack.respond(responseUrl, {
        response_type: "ephemeral",
        text: defaultCopy.notInChannel,
      });
    }
  };

  /** Who answers each person's DMs, as picked on their Home tab. In memory, like everything here. */
  const answeredBy = new Map<string, string>();

  /** The Home fixture, addressed to whoever opened the tab. */
  const publishHome = async (user: string) => {
    const model: HomeModel = {
      ...fx.home,
      person: { ...fx.home.person, id: user },
      ...(fx.home.answeredBy
        ? { answeredBy: { ...fx.home.answeredBy, selected: answeredBy.get(user) } }
        : {}),
    };
    const { view, warnings } = homeView(<Home home={model} />);
    if (warnings.length > 0) console.warn(`  home: ${warnings.join("; ")}`);
    await slack.call("views.publish", { user_id: user, view });
  };

  /** A Home click the sandbox has no real flow for: say what a host would do, in a modal. */
  const explain = (triggerId: string, title: string, text: string) =>
    slack.call("views.open", {
      trigger_id: triggerId,
      view: {
        type: "modal",
        title: { type: "plain_text", text: title },
        close: { type: "plain_text", text: "Close" },
        blocks: [{ type: "section", text: { type: "mrkdwn", text } }],
      },
    });

  /** Home actions arrive from the Home tab, which has no channel to answer in. */
  const onHomeAction = async (
    ui: { id: ActionId; payload: { target: string; option?: string; turnId: string } },
    payload: { user: { id: string }; trigger_id: string },
  ): Promise<boolean> => {
    const { id, payload: p } = ui;
    console.log(`  home ${id} → ${p.target}${p.option ? ` (${p.option})` : ""}`);
    if (id === ActionId.homeAsk) {
      await slack.call("views.open", {
        trigger_id: payload.trigger_id,
        view: form("Ask Collegium", "What do you need?", { kind: "ask" }),
      });
      return true;
    }
    if (id === ActionId.homeAnsweredBy) {
      if (p.option) answeredBy.set(payload.user.id, p.option);
      await publishHome(payload.user.id);
      return true;
    }
    if (id === ActionId.homeStart) {
      await explain(
        payload.trigger_id,
        "Start",
        `A host opens the \`${p.target}\` flow here. The sandbox only shows that the click arrived.`,
      );
      return true;
    }
    if (id === ActionId.homeConnect) {
      await explain(
        payload.trigger_id,
        "Connect",
        `A host starts the OAuth flow for \`${p.target}\` here. The sandbox doesn't connect anything.`,
      );
      return true;
    }
    if (id === ActionId.open && !live.has(p.turnId)) {
      await explain(
        payload.trigger_id,
        "Review",
        `A host opens \`${p.target}\` from message \`${p.turnId}\` here, to approve or answer it.`,
      );
      return true;
    }
    return false;
  };

  const onEvent = async (event: Record<string, string>, teamId: string) => {
    if (event.type === "app_home_opened" && event.tab === "home") {
      await publishHome(event.user as string);
      return;
    }
    if (event.type === "function_executed") {
      const fn = event as unknown as {
        function: { callback_id: string };
        inputs: { prompt: string; channel_id: string; user_id: string };
        function_execution_id: string;
        bot_access_token?: string;
      };
      if (fn.function.callback_id !== ENTRY.step) return;
      const step = { id: fn.function_execution_id, token: fn.bot_access_token };
      try {
        await start(
          { channel: fn.inputs.channel_id, user: fn.inputs.user_id, team: teamId, step },
          fn.inputs.prompt,
          "from a workflow",
        );
      } catch (e) {
        // The workflow must hear about it either way, or it hangs until it times out.
        await completeStep(step, "failed", "", (e as Error).message);
        throw e;
      }
      return;
    }
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
    const said = (event.text ?? "").toLowerCase();
    const thread = { channel: event.channel, thread_ts: event.thread_ts ?? event.ts };
    if (/\b(help|what can you)\b/.test(said)) {
      await slack.call("chat.postMessage", { ...thread, text: HELP });
      return;
    }
    if (/\b(status|outage|incident)\b/.test(said)) {
      // An alert is one message that changes as the incident does, not a new post per update.
      const down = renderToSlack(<SystemAlertView alert={fx.systemAlerts.down} />);
      const posted = await slack.call<{ ts: string }>("chat.postMessage", {
        ...thread,
        text: down.text,
        blocks: down.blocks,
      });
      await sleep(8000);
      const resolved = renderToSlack(
        <SystemAlertView alert={{ ...fx.systemAlerts.resolved, id: fx.systemAlerts.down.id }} />,
      );
      await slack.call("chat.update", {
        channel: event.channel,
        ts: posted.ts,
        text: resolved.text || "Resolved",
        blocks: resolved.blocks,
      });
      return;
    }
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
    /** Absent for clicks on the Home tab. */
    channel: { id: string };
    trigger_id: string;
    message?: { ts: string; thread_ts?: string };
    actions: { action_id: string; value?: string; selected_option?: { value: string } }[];
    state?: Parameters<typeof decodeSlackAction>[1];
  }) => {
    const action = payload.actions[0];
    const ui = action ? decodeSlackAction(action, payload.state) : null;
    // Ticking a checkbox is not a decision; the batch buttons read the ticks.
    if (!ui) return;
    if (await onHomeAction(ui, payload)) return;
    const { id } = ui;
    const { turnId, target, option } = ui.payload;
    const state = live.get(turnId);
    const whisper = (text: string) =>
      slack.call("chat.postEphemeral", {
        channel: payload.channel.id,
        user: payload.user.id,
        // Reply where the button was clicked, not in the channel above the thread.
        ...(payload.message?.thread_ts ? { thread_ts: payload.message.thread_ts } : {}),
        text,
      });

    if (id === ActionId.feedback && (target === "up" || target === "down")) {
      // The rating is kept first, in memory, so nothing slow sits before views.open:
      // the trigger lasts about three seconds. Skipping the modal leaves the rating as it is.
      if (state) live.set(turnId, { ...state, turn: { ...state.turn, feedback: target } });
      await slack.call("views.open", {
        trigger_id: payload.trigger_id,
        view: feedbackModal({ turnId, rating: target }),
      });
      console.log(`  feedback ${turnId} → ${target}`);
      return;
    }
    if (id === ActionId.raiseBudget && !option) {
      // Raise Limit carries no amount: collect it, bounded by the limit in force.
      const part = fx.mailMessages.find((m) => m.id === turnId)?.parts.find((p) => p.id === target);
      if (part?.type !== "budget") return whisper("That limit isn't in the sandbox's fixtures.");
      await slack.call("views.open", {
        trigger_id: payload.trigger_id,
        // Its words default to vultus copy (raiseLimitTitle, newLimit, raiseLimit, cancel).
        view: amountModal({
          action: ui,
          unit: part.meter.unit,
          above: part.meter.limit,
          ...(part.raiseMax !== undefined ? { max: part.raiseMax } : {}),
        }),
      });
      return;
    }
    if (id === ActionId.reconnect) return whisper("In a real app this opens the reconnect flow.");
    if (id === ActionId.report) return whisper("Reported. Reference copied to the incident log.");
    if (!state) return whisper("This sandbox restarted, so that turn is gone. Ask again.");

    const at = new Date().toISOString();
    const by = `<@${payload.user.id}>`;
    const settle = (turn: Turn) => settleTurn(state, turn);

    // These three need words, not a click: collect them in a form on this click's trigger.
    const asked = state.turn.interrupts.find((i) => i.id === target);
    const draft = state.turn.attachments?.find((a) => a.type === "code");
    const forms: Partial<Record<ActionId, object>> = {
      [ActionId.ask]: form("Ask first", "Your question", { kind: "question", turnId, target }),
      [ActionId.clarifyOther]: form("Something else", "What should it do instead?", {
        kind: "other",
        turnId,
        target,
      }),
      [ActionId.edit]: form(
        "Edit the proposal",
        "Change it, then send it back for approval",
        { kind: "edit", turnId, target },
        draft?.type === "code" ? draft.code : asked?.type === "approval" ? asked.summary : "",
      ),
    };
    if (forms[id]) {
      await slack.call("views.open", { trigger_id: payload.trigger_id, view: forms[id] });
      return;
    }

    if (id === ActionId.clarify || id === ActionId.unsure) {
      const turn: Turn = {
        ...state.turn,
        interrupts: state.turn.interrupts.map((i) =>
          i.type === "clarification" && i.id === target
            ? {
                ...i,
                answer:
                  id === ActionId.clarify
                    ? { kind: "answered", by, at, optionIds: option ? [option] : [] }
                    : { kind: "unsure", by, at },
              }
            : i,
        ),
        status: "done",
      };
      await settle(turn);
      const picked =
        asked?.type === "clarification" ? asked.options.find((o) => o.id === option) : undefined;
      await run(state, answered(turnId, picked?.label));
      return;
    }
    if (id === ActionId.approveSelected || id === ActionId.approveAll || id === ActionId.denyAll) {
      const chosen = id === ActionId.approveSelected ? (ui.payload.selection ?? []) : null;
      const approved: Approval[] = [];
      const turn: Turn = {
        ...state.turn,
        interrupts: state.turn.interrupts.map((i) => {
          if (i.type !== "approval" || i.decision.kind !== "pending") return i;
          const yes = id !== ActionId.denyAll && (chosen === null || chosen.includes(i.id));
          if (yes) approved.push(i);
          return {
            ...i,
            decision: yes ? { kind: "approved", by, at } : { kind: "denied", by, at },
          };
        }),
        status: "done",
      };
      await settle(turn);
      if (approved.length === 0) await completeStep(state.step, "denied", "");
      for (const approval of approved) await run(state, followUp(turnId, approval));
      return;
    }
    if (id === ActionId.approve || id === ActionId.deny) {
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
      // Close the loop in place: buttons become a record of the decision. An approval's
      // follow-up run adds its own reaction; a denial ends here, so it gets the final one now.
      const add = await settle(turn);
      for (const name of id === ActionId.deny ? add : []) {
        await slack
          .call("reactions.add", { channel: state.channel, timestamp: state.triggerTs, name })
          .catch(() => {});
      }
      if (id === ActionId.approve && asked?.type === "approval") {
        await run(state, followUp(turnId, asked));
      } else {
        await completeStep(state.step, "denied", "");
      }
      return;
    }
    if (id === ActionId.retry) {
      await run(state, eventsFor({ ...scenarioFor("answer"), id: `${turnId}-retry` }));
      return;
    }
    return whisper(`The sandbox doesn't act on ${id} yet.`);
  };

  /** Record the outcome on the card itself, so the thread keeps who decided what. */
  const settleTurn = async (state: Live, turn: Turn) => {
    const view = renderToSlack(<TurnView turn={turn} streamed />);
    await slack.call("chat.update", {
      channel: state.channel,
      ts: state.ts,
      text: view.text,
      blocks: view.blocks,
    });
    live.set(turn.id, { ...state, turn });
    const { add, remove } = reactionChanges("waiting", turn.status);
    for (const name of remove) {
      await slack
        .call("reactions.remove", { channel: state.channel, timestamp: state.triggerTs, name })
        .catch(() => {});
    }
    return add;
  };

  /** Shortcuts and form submissions: everything interactive that isn't a block click. */
  const onInteractive = async (payload: {
    type: string;
    callback_id?: string;
    trigger_id: string;
    response_url?: string;
    user: { id: string };
    team: { id: string };
    channel?: { id: string };
    message?: { ts: string; thread_ts?: string; text?: string };
    view?: {
      callback_id: string;
      private_metadata: string;
      state: {
        values: Record<
          string,
          Record<string, { value?: string; selected_option?: { value: string } | null }>
        >;
      };
    };
  }) => {
    if (payload.type === "shortcut" && payload.callback_id === ENTRY.ask) {
      await slack.call("views.open", {
        trigger_id: payload.trigger_id,
        view: form("Ask Collegium", "What do you need?", { kind: "ask" }),
      });
      return;
    }
    if (payload.type === "message_action" && payload.callback_id === ENTRY.askAbout) {
      const { channel, message } = payload;
      if (!channel || !message) return;
      const scenario = scenarioFor(message.text ?? "");
      // The message is the question, so the answer belongs in its thread.
      await orExplain(
        run(
          {
            channel: channel.id,
            threadTs: message.thread_ts ?? message.ts,
            triggerTs: message.ts,
            user: payload.user.id,
            team: payload.team.id,
          },
          eventsFor({ ...scenario, id: `${scenario.id}-${Date.now()}` }),
        ),
        payload.response_url,
      );
      return;
    }
    if (payload.type === "view_submission" && payload.view?.callback_id === FEEDBACK_MODAL) {
      // Only adds to a rating that already exists; an empty submit decodes to nothing.
      const comment = decodeFeedbackModal(payload.view);
      const event = comment ? feedbackEvent(comment) : null;
      const rated = comment ? live.get(comment.payload.turnId) : undefined;
      if (!comment || !event) return;
      console.log(`  feedback ${comment.payload.turnId} → ${JSON.stringify(comment.payload)}`);
      if (!rated) return;
      live.set(rated.turn.id, { ...rated, turn: reduce(rated.turn, event) });
      await slack.call("chat.postEphemeral", {
        channel: rated.channel,
        user: payload.user.id,
        thread_ts: rated.threadTs,
        text: "Thanks, your comment was added to your rating.",
      });
      return;
    }
    if (payload.type === "view_submission" && payload.view?.callback_id === AMOUNT_MODAL) {
      const raised = decodeAmountModal(payload.view);
      console.log(`  ${raised ? JSON.stringify(raised) : "amount not taken"}`);
      return;
    }
    if (payload.type !== "view_submission" || !payload.view) return;
    const submitted = decodeAskForm<FormMeta>(payload.view);
    if (!submitted) return;
    const { text, meta } = submitted;
    if (meta.kind === "ask") {
      // A global shortcut has no channel: answer in the app's own messages with this person.
      await start(
        { channel: payload.user.id, user: payload.user.id, team: payload.team.id },
        text,
        "from the shortcuts menu",
      );
      return;
    }
    const state = meta.turnId ? live.get(meta.turnId) : undefined;
    if (!state) return;
    const at = new Date().toISOString();
    const by = `<@${payload.user.id}>`;
    const { turn } = state;
    if (meta.kind === "question") {
      // The call stays halted: the card now shows the question next to the same buttons.
      await settleTurn(state, {
        ...turn,
        interrupts: turn.interrupts.map((i) =>
          i.type === "approval" && i.id === meta.target
            ? { ...i, decision: { kind: "asked", by, at, question: text } }
            : i,
        ),
      });
    } else if (meta.kind === "edit") {
      // A changed proposal is a new revision; the reviewer still has to approve it.
      const draft = turn.attachments?.find((a) => a.type === "code");
      await settleTurn(state, {
        ...turn,
        attachments: turn.attachments?.map((a) => (a === draft ? { ...a, code: text } : a)),
        interrupts: turn.interrupts.map((i) =>
          i.type === "approval" && i.id === meta.target
            ? { ...i, ...(draft ? {} : { summary: text }), revision: (i.revision ?? 1) + 1 }
            : i,
        ),
      });
    } else {
      await settleTurn(state, {
        ...turn,
        interrupts: turn.interrupts.map((i) =>
          i.type === "clarification" && i.id === meta.target
            ? { ...i, answer: { kind: "answered", by, at, optionIds: [], other: text } }
            : i,
        ),
        status: "done",
      });
      await run(state, answered(turn.id, text));
    }
  };

  const onCommand = async (command: {
    text: string;
    user_id: string;
    channel_id: string;
    team_id: string;
    response_url: string;
  }) => {
    // A command's text arrives entity-encoded; RequestView takes the words as typed.
    const text = fromSlack(command.text).trim();
    if (!text || text === "help") {
      await slack.respond(command.response_url, { response_type: "ephemeral", text: HELP });
      return;
    }
    await orExplain(
      start(
        { channel: command.channel_id, user: command.user_id, team: command.team_id },
        text,
        `with \`${ENTRY.command}\``,
      ),
      command.response_url,
    );
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
            : envelope.type === "interactive"
              ? onInteractive(p as never)
              : envelope.type === "slash_commands"
                ? onCommand(p as never)
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

const reply = (id: string, turn: Partial<Turn>): TurnEvent[] =>
  eventsFor({
    id,
    agent: { name: "Assistant" },
    status: "done",
    text: "",
    tasks: [],
    citations: [],
    interrupts: [],
    notices: [],
    sessionUrl: fx.doneTurn.sessionUrl,
    source: fx.doneTurn.source,
    ...turn,
  });

/** After an approval, the agent does what was approved and reports the result. */
function followUp(turnId: string, approval: Approval): TurnEvent[] {
  const id = `${turnId}-${approval.id}-run`;
  switch (approval.tool) {
    case "skills.install":
      return reply(id, {
        durationMs: 4_000,
        tasks: [{ id: "s1", title: "Added incident-postmortem 1.2.0", status: "complete" }],
        text: "Added the **Incident postmortem** skill. I'll draft a postmortem the next time an incident closes, and ask before I post it anywhere.",
      });
    case "agent.update_prompt":
      return reply(id, {
        durationMs: 3_000,
        tasks: [{ id: "s1", title: "Saved the new instructions", status: "complete" }],
        text: "Updated. From the next conversation on, incident replies name the deploy owner and their team.",
      });
    case "files.create":
      return reply(id, {
        durationMs: 11_000,
        tasks: [{ id: "s1", title: "Created refunds-q3.xlsx", status: "complete" }],
        text: "Here's the export, shared with this thread only.",
        attachments: [fx.artifacts.ready],
      });
    case "slack.post":
      return reply(id, {
        durationMs: 2_000,
        tasks: [{ id: "s1", title: "Posted to #payments-oncall", status: "complete" }],
        text: "Posted the incident summary to **#payments-oncall**.",
      });
    default:
      return reply(id, {
        durationMs: 9_000,
        planTitle: "Rolling back",
        text: "Rolled back **payments-api** to `#4811`. The 5xx rate is back under 0.1%.",
        tasks: [
          {
            id: "r1",
            title: "Rolled back payments-api",
            status: "complete",
            output: "#4812 → #4811",
          },
          {
            id: "r2",
            title: "Watched the error rate",
            status: "complete",
            output: "0.08% after 2 min",
          },
        ],
      });
  }
}

/** After an answer, the agent carries on with it. */
function answered(turnId: string, choice: string | undefined): TurnEvent[] {
  return reply(`${turnId}-answered`, {
    durationMs: 6_000,
    tasks: [{ id: "c1", title: "Checked the error rate", status: "complete", output: "4.2% 5xx" }],
    text: choice
      ? `Going with **${choice}**. The 5xx rate there is 4.2% since 14:52.`
      : "No problem. I checked production first, since that's where alerts usually fire: the 5xx rate is 4.2% since 14:52.",
  });
}
