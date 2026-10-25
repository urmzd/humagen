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
  // humagen: needs a reply modal for questions; until then this says where to answer.
  replyInConsole: "Replies to this question are typed in the console for now.",
  /** Under a mention or a question sent to a person's direct messages: where it was said. */
  openThread: "Open Thread",
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
