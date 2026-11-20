/**
 * What a Slack host needs that humagen does not export yet. The entry ids and
 * the wording below are copied from humagen's Slack sandbox
 * (`apps/slack-sandbox/src/manifest.ts` and `serve.tsx`), where they are
 * private to that app. They belong in a humagen package: until they move, this
 * file is the copy, and it holds nothing else.
 *
 * humagen: export ENTRY (with the manifest), the ask form as askForm() in
 * @humagen/render-slack (like feedbackModal), and these words in copy.ts as
 * copy.ask.{title,label,send,cancel} and copy.notInChannel.
 */

/** Ways into the agent besides a direct message or a mention. They match the sandbox manifest. */
export const ENTRY = {
  /** Global shortcut: ask from anywhere, answered in the app's messages with that person. */
  ask: "aui_ask",
  /** Message shortcut: ask about one message, answered in its thread. */
  askAbout: "aui_ask_about",
  command: "/agent",
  /** The one text form behind the global shortcut. */
  form: "aui_form",
  /** The form behind Ask a Question on a call that waits for approval. */
  askCall: "aui_ask_call",
} as const;

export const words = {
  askTitle: "Ask the agent",
  askLabel: "What do you need?",
  // The form Build an Agent opens on Home. A modal title holds 24 characters.
  buildTitle: "Build an agent",
  buildLabel: "What should the agent do?",
  send: "Send",
  cancel: "Cancel",
  /** What the Home tab says the app does, before how to reach it. */
  homeIntro: "I hand your questions to this workspace's agents.",
  notInChannel: "I'm not in this channel yet. Invite me with `/invite`, then try again.",
  /** A Reply with nothing to open a form from, or on a part a form cannot answer. */
  replyInConsole: "Replies to this question are typed in the console for now.",
  /** A press on a question that was settled already: the card is brought up to date beside it. */
  alreadySettled: "This is no longer waiting for an answer. The card now shows what was recorded.",
  // Ask a Question on a call that waits for approval: a form, then the question in the thread.
  // A modal title holds 24 characters.
  askCallTitle: "Ask about this action",
  askCallLabel: "What do you want to know before you decide?",
  /** In the thread, where everyone reads it: who asked about which action, and what. */
  askCallSaid: (who: string, action: string, question: string) =>
    `${who} asked about "${action}": ${question}`,
  /** What the agent is asked: the person's words, with the action they are about. */
  askCallAsked: (action: string, question: string) =>
    `About the action waiting for approval, "${action}": ${question}`,
  /** The call's run is in no Slack thread of this app's, so there is nowhere to answer. */
  askCallNowhere: "This conversation is not in a Slack thread I answer in. Ask in the console.",
  /** The thread's approval mode could not be changed for a reason the platform did not give. */
  modeFailed: "Approvals could not be changed. Try again.",
  /** Under a mention or a question sent to a person's direct messages: where it was said. */
  openThread: "Open Thread",
  /** Under a background result replied in the Slack thread that asked: its session in the console. */
  openSession: "Open Session",
  // humagen: belongs in copy.ts, as copy.interrupted.
  interrupted: "This answer was cut off when the app restarted. Please ask again.",
  // What the agent is told about a thread's messages that were not addressed to it.
  historyOpen: "Messages in this Slack thread since you were last asked, oldest first:",
  historyClose: "End of thread messages. The message for you follows.",
};

/** What a form asks for: a question, or an agent to build. It comes back on submit. */
export type FormKind = "ask" | "build";

/** The form the global shortcut and Ask a Question open. Its text comes back on submit. */
export function askForm(w: typeof words = words) {
  return form("ask", w.askTitle, w.askLabel, w);
}

/** The form Build an Agent opens on Home: what the agent should do, for the builder. */
export function buildForm(w: typeof words = words) {
  return form("build", w.buildTitle, w.buildLabel, w);
}

function form(kind: FormKind, title: string, label: string, w: typeof words) {
  return {
    type: "modal",
    callback_id: ENTRY.form,
    private_metadata: JSON.stringify({ kind }),
    title: { type: "plain_text", text: title },
    submit: { type: "plain_text", text: w.send },
    close: { type: "plain_text", text: w.cancel },
    blocks: [
      {
        type: "input",
        block_id: "text",
        label: { type: "plain_text", text: label },
        element: { type: "plain_text_input", action_id: "value", multiline: true },
      },
    ],
  };
}

/**
 * What a question about a call is about: the run and its question, by id,
 * the action as the card says it, and where the card was pressed, for a
 * refusal said to the person alone.
 */
export interface AskCall {
  run: string;
  question: string;
  action: string;
  channel: string;
  threadTs?: string;
}

/** Slack keeps at most 3000 characters of a modal's private metadata; the action is cut to fit. */
const ACTION_KEPT = 500;

/** The form Ask a Question opens on a call that waits for approval: one question, asked in the thread. */
export function askCallForm(at: AskCall, w: typeof words = words) {
  const meta: AskCall = { ...at, action: at.action.slice(0, ACTION_KEPT) };
  return {
    type: "modal",
    callback_id: ENTRY.askCall,
    private_metadata: JSON.stringify(meta),
    title: { type: "plain_text", text: w.askCallTitle },
    submit: { type: "plain_text", text: w.send },
    close: { type: "plain_text", text: w.cancel },
    blocks: [
      ...(meta.action
        ? [
            {
              type: "context",
              // Plain text: an agent's words never become a mention or a link here.
              elements: [{ type: "plain_text", text: meta.action.slice(0, 2000) }],
            },
          ]
        : []),
      {
        type: "input",
        block_id: "text",
        label: { type: "plain_text", text: w.askCallLabel },
        element: { type: "plain_text_input", action_id: "value", multiline: true },
      },
    ],
  };
}

/** What a submitted Ask a Question form is about, or null when it is not one. */
export function askCallOf(view: {
  callback_id?: string;
  private_metadata?: string;
}): AskCall | null {
  if (view.callback_id !== ENTRY.askCall) return null;
  try {
    const m = JSON.parse(view.private_metadata ?? "{}") as Partial<AskCall>;
    if (typeof m.run !== "string" || !m.run || typeof m.question !== "string" || !m.question)
      return null;
    return {
      run: m.run,
      question: m.question,
      action: typeof m.action === "string" ? m.action : "",
      channel: typeof m.channel === "string" ? m.channel : "",
      ...(typeof m.threadTs === "string" && m.threadTs ? { threadTs: m.threadTs } : {}),
    };
  } catch {
    return null;
  }
}

/** The kind of form a submission came from. Anything else is a question. */
export function formKind(privateMetadata?: string): FormKind {
  try {
    return (JSON.parse(privateMetadata ?? "{}") as { kind?: string }).kind === "build"
      ? "build"
      : "ask";
  } catch {
    return "ask";
  }
}
