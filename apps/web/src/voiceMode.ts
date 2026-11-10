// Voice mode's state, as plain data. The screen shows one phase, derived from
// a few facts (is the person speaking, is the agent working, is it speaking,
// does it wait on an approval), so no sequence of events leaves it showing
// the wrong one. Nothing here touches the microphone, the network or audio.

/** What the screen says is happening. */
export type VoicePhase =
  | "starting"
  | "listening"
  | "hearing"
  | "sending"
  | "thinking"
  | "tool"
  | "speaking"
  | "approval"
  | "paused"
  | "ended"
  | "failed";

export interface VoiceState {
  /** The microphone: being asked for, on, or refused. */
  mic: "starting" | "on" | "refused";
  paused: boolean;
  ended: boolean;
  /** The person is speaking now. */
  hearing: boolean;
  /** Their words are being transcribed and sent. */
  sending: boolean;
  /** A run this voice mode started is working. */
  working: boolean;
  /** The tool the agent is using now, if any. */
  tool?: string;
  /** The agent's answer is being spoken. */
  speaking: boolean;
  /** The person spoke over the agent: its speech is held until their words are heard. */
  bargedIn: boolean;
  /** Approvals the agent waits on, by question id, oldest first. */
  asks: string[];
  /** The last thing the person said, and the last sentence the agent spoke. */
  heard?: string;
  said?: string;
  problem?: string;
}

export type VoiceEvent =
  | { type: "ready" }
  | { type: "refused"; problem: string }
  | { type: "voiceStarted" }
  | { type: "voiceEnded" }
  | { type: "heard"; text: string }
  | { type: "heardNothing" }
  | { type: "problem"; problem: string }
  | { type: "runStarted" }
  | { type: "toolStarted"; name: string }
  | { type: "toolDone" }
  | { type: "speakStarted"; text: string }
  | { type: "speakDone" }
  | { type: "asked"; id: string }
  | { type: "answered"; id: string }
  | { type: "runEnded" }
  | { type: "pause" }
  | { type: "resume" }
  | { type: "end" };

export const initialVoice: VoiceState = {
  mic: "starting",
  paused: false,
  ended: false,
  hearing: false,
  sending: false,
  working: false,
  speaking: false,
  bargedIn: false,
  asks: [],
};

export function voiceReduce(s: VoiceState, e: VoiceEvent): VoiceState {
  if (s.ended) return s;
  switch (e.type) {
    case "ready":
      return { ...s, mic: "on", problem: undefined };
    case "refused":
      return { ...s, mic: "refused", problem: e.problem, hearing: false };
    case "voiceStarted":
      // Speaking over the agent holds its speech until the words are heard.
      return { ...s, hearing: true, bargedIn: s.bargedIn || s.speaking, problem: undefined };
    case "voiceEnded":
      return { ...s, hearing: false, sending: true };
    case "heard":
      // Words over the agent's speech end it: what is left is not said.
      return {
        ...s,
        sending: false,
        heard: e.text,
        speaking: s.bargedIn ? false : s.speaking,
        bargedIn: false,
      };
    case "heardNothing":
      return { ...s, hearing: false, sending: false, bargedIn: false };
    case "problem":
      return { ...s, hearing: false, sending: false, bargedIn: false, problem: e.problem };
    case "runStarted":
      return { ...s, working: true, tool: undefined };
    case "toolStarted":
      return { ...s, tool: e.name };
    case "toolDone":
      return { ...s, tool: undefined };
    case "speakStarted":
      return { ...s, speaking: true, said: e.text };
    case "speakDone":
      return { ...s, speaking: false };
    case "asked":
      return s.asks.includes(e.id) ? s : { ...s, asks: [...s.asks, e.id] };
    case "answered":
      return { ...s, asks: s.asks.filter((id) => id !== e.id) };
    case "runEnded":
      return { ...s, working: false, tool: undefined, asks: [] };
    case "pause":
      // Nothing is heard or said while paused; the session and its runs go on.
      return {
        ...s,
        paused: true,
        hearing: false,
        sending: false,
        speaking: false,
        bargedIn: false,
      };
    case "resume":
      return { ...s, paused: false, problem: undefined };
    case "end":
      return {
        ...s,
        ended: true,
        hearing: false,
        sending: false,
        speaking: false,
        bargedIn: false,
      };
  }
}

/** The one phase the screen shows. */
export function voicePhase(s: VoiceState): VoicePhase {
  if (s.ended) return "ended";
  if (s.mic === "refused") return "failed";
  if (s.paused) return "paused";
  if (s.mic === "starting") return "starting";
  if (s.hearing) return "hearing";
  if (s.sending) return "sending";
  if (s.speaking && !s.bargedIn) return "speaking";
  if (s.asks.length > 0) return "approval";
  if (s.working) return s.tool ? "tool" : "thinking";
  return "listening";
}

/**
 * Whether the phase waits on the person. Nothing that waits on a person moves: the
 * visual is still while listening, paused, waiting for an approval, or stopped.
 */
export function waitsOnPerson(phase: VoicePhase): boolean {
  return (
    phase === "listening" ||
    phase === "paused" ||
    phase === "approval" ||
    phase === "ended" ||
    phase === "failed" ||
    phase === "starting"
  );
}

/**
 * Whether a phase is announced. Hearing and sending pass in a moment; announcing them
 * would talk over the person, so a screen reader hears only the settled phases.
 */
export function announced(phase: VoicePhase): boolean {
  return phase !== "hearing" && phase !== "sending";
}

const APPROVE = new Set([
  "yes",
  "yeah",
  "yep",
  "approve",
  "approved",
  "allow",
  "ok",
  "okay",
  "sure",
  "go",
  "do",
]);
const DENY = new Set(["no", "nope", "deny", "denied", "dont", "don't", "reject", "cancel", "stop"]);

/**
 * A spoken answer to one open approval: approve or deny when the words are short and
 * start with an answer word ("yes", "go ahead", "no, don't"), else nothing, and the
 * words are sent as a message instead. A long sentence is never an answer, so the
 * agent's own ask, heard back through the speakers, cannot approve itself.
 */
export function spokenAnswer(text: string): "approve" | "deny" | undefined {
  const words = text
    .toLowerCase()
    .replace(/[^a-z' ]+/g, " ")
    .split(/\s+/)
    .filter(Boolean);
  const first = words[0];
  if (!first || words.length > 4) return undefined;
  if (APPROVE.has(first)) return "approve";
  if (DENY.has(first)) return "deny";
  return undefined;
}
