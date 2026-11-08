import assert from "node:assert/strict";
import { afterEach, test } from "node:test";
import type { AnswerRequest } from "@humagen/adapter/question";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { RunEvent } from "./adapter.ts";
import type { VoiceSpeech } from "./api.ts";
import { VoiceMode } from "./VoiceMode.tsx";
import type { VoiceDeps } from "./voiceController.ts";

// Voice mode: a focused view over a session, driven here with a fake
// microphone, recorder, player and API. Holding Space talks.

class FakeRecorder {
  state = "inactive";
  mimeType = "audio/webm";
  ondataavailable: ((e: { data: Blob }) => void) | null = null;
  onstop: (() => void) | null = null;
  start() {
    this.state = "recording";
  }
  stop() {
    this.state = "inactive";
    this.ondataavailable?.({ data: new Blob(["voice"]) });
    this.onstop?.();
  }
}

class FakeAudio {
  paused = true;
  onended: (() => void) | null = null;
  onerror: (() => void) | null = null;
  readonly src: string;
  constructor(src: string) {
    this.src = src;
  }
  play() {
    this.paused = false;
    return Promise.resolve();
  }
  pause() {
    this.paused = true;
  }
  finish() {
    this.paused = true;
    this.onended?.();
  }
}

function harness(heard: string[], speech: (run: string, from: number) => VoiceSpeech) {
  const log = {
    posted: [] as string[],
    answered: [] as AnswerRequest[],
    stopped: [] as string[],
    audios: [] as FakeAudio[],
    tracksStopped: 0,
    emit: (_e: RunEvent) => {},
  };
  const deps: VoiceDeps = {
    media: async () =>
      ({ getTracks: () => [{ stop: () => log.tracksStopped++ }] }) as unknown as MediaStream,
    audioContext: () => {
      throw new Error("no Web Audio here");
    },
    recorder: () => new FakeRecorder() as unknown as MediaRecorder,
    player: (src) => {
      const a = new FakeAudio(src);
      log.audios.push(a);
      return a as unknown as HTMLAudioElement;
    },
    hear: async () => ({ text: heard.shift() ?? "" }),
    speak: async (_s, { run, from }) => speech(run, from),
    post: async (_s, text) => {
      log.posted.push(text);
      return { runs: [{ id: `r${log.posted.length}`, status: "running" }] };
    },
    answer: async (r) => {
      log.answered.push(r);
    },
    stop: async (run) => {
      log.stopped.push(run);
    },
    follow: (_s, onRun) => {
      log.emit = onRun;
      return () => {};
    },
    frameMs: 1000,
  };
  return { deps, log };
}

const event = (run: string, type: string, data: unknown = {}) =>
  ({ run, type, data, seq: 1, time: "2026-11-07T00:00:00Z" }) as unknown as RunEvent;

async function talk(dialog: HTMLElement) {
  fireEvent.keyDown(dialog, { code: "Space", key: " " });
  fireEvent.keyUp(dialog, { code: "Space", key: " " });
}

const status = () => screen.getByRole("status").textContent;

afterEach(cleanup);

test("a spoken turn: heard, sent spoken, thinking, a tool, spoken back, then listening again", async () => {
  const { deps, log } = harness(["What's on today?"], (_run, from) =>
    from === 0
      ? {
          audio: "SUQz",
          media_type: "audio/mpeg",
          text: "You have two meetings.",
          next: 22,
          cost_micros: 1,
        }
      : { text: "", next: from, done: true, cost_micros: 0 },
  );
  render(<VoiceMode session="s_1" agent="Assistant" onEnd={() => {}} deps={deps} />);
  const dialog = screen.getByRole("dialog", { name: "Voice mode" });
  await waitFor(() => assert.equal(status(), "Listening"));
  await talk(dialog);
  await waitFor(() => assert.deepEqual(log.posted, ["What's on today?"]));
  await screen.findByText("What's on today?");
  await waitFor(() => assert.equal(status(), "Thinking"));
  act(() => log.emit(event("r1", "run.delta.tool.call.v1", { id: "t1", name: "calendar_list" })));
  await waitFor(() => assert.equal(status(), "Using calendar_list"));
  act(() => log.emit(event("r1", "run.delta.tool.done.v1", { id: "t1" })));
  act(() => log.emit(event("r1", "run.delta.text.v1", { text: "You have two meetings." })));
  await screen.findByText("You have two meetings.");
  await waitFor(() => assert.equal(status(), "Speaking"));
  assert.equal(log.audios[0]?.src, "data:audio/mpeg;base64,SUQz");
  act(() => log.emit(event("r1", "run.completed.v1", { text: "You have two meetings." })));
  act(() => log.audios[0]?.finish());
  await waitFor(() => assert.equal(status(), "Listening"));
});

test("an approval waits for the person: Approve answers it, and so does a spoken yes", async () => {
  const { deps, log } = harness(["Push it", "Yes, go ahead."], (_r, from) => ({
    text: "",
    next: from,
    pending: true,
    cost_micros: 0,
  }));
  render(<VoiceMode session="s_1" onEnd={() => {}} deps={deps} />);
  const dialog = screen.getByRole("dialog");
  await waitFor(() => assert.equal(status(), "Listening"));
  await talk(dialog);
  await waitFor(() => assert.equal(log.posted.length, 1));
  const ask = (id: string) =>
    event("r1", "run.parked.v1", {
      question: {
        id,
        kind: "approval",
        agent: "builder",
        prompt: "Push to main?",
        tool: { name: "git_push" },
      },
    });
  act(() => log.emit(ask("q1")));
  await waitFor(() => assert.equal(status(), "Waiting for your approval"));
  assert.ok(screen.getByText("builder wants to use git_push."));
  fireEvent.click(screen.getByRole("button", { name: "Approve" }));
  await waitFor(() =>
    assert.deepEqual(log.answered[0], { run: "r1", question: "q1", body: { action: "approve" } }),
  );
  await waitFor(() => assert.equal(status(), "Thinking"));
  act(() => log.emit(ask("q2")));
  await waitFor(() => assert.equal(status(), "Waiting for your approval"));
  await talk(dialog);
  await waitFor(() => assert.deepEqual(log.answered[1]?.body, { action: "approve" }));
  // The yes answered the approval; it was not sent as a message.
  assert.equal(log.posted.length, 1);
});

test("speaking over the agent stops its speech and its run, then sends the words", async () => {
  const { deps, log } = harness(["Tell me about Friday", "No, the other one"], (_r, from) =>
    from === 0
      ? {
          audio: "SUQz",
          media_type: "audio/mpeg",
          text: "Monday has three meetings.",
          next: 26,
          cost_micros: 1,
        }
      : { text: "", next: from, pending: true, cost_micros: 0 },
  );
  render(<VoiceMode session="s_1" onEnd={() => {}} deps={deps} />);
  const dialog = screen.getByRole("dialog");
  await waitFor(() => assert.equal(status(), "Listening"));
  await talk(dialog);
  await waitFor(() => assert.equal(log.posted.length, 1));
  act(() => log.emit(event("r1", "run.delta.text.v1", { text: "Monday has three meetings." })));
  await waitFor(() => assert.equal(status(), "Speaking"));
  const playing = log.audios[0];
  fireEvent.keyDown(dialog, { code: "Space", key: " " });
  assert.equal(playing?.paused, true);
  fireEvent.keyUp(dialog, { code: "Space", key: " " });
  await waitFor(() => assert.deepEqual(log.posted, ["Tell me about Friday", "No, the other one"]));
  assert.deepEqual(log.stopped, ["r1"]);
});

test("pause releases the microphone and stops speaking; resume listens; End leaves", async () => {
  const { deps, log } = harness([], () => ({ text: "", next: 0, done: true, cost_micros: 0 }));
  let ended = false;
  render(<VoiceMode session="s_1" onEnd={() => (ended = true)} deps={deps} />);
  await waitFor(() => assert.equal(status(), "Listening"));
  fireEvent.click(screen.getByRole("button", { name: "Pause" }));
  await waitFor(() => assert.equal(status(), "Paused"));
  assert.equal(log.tracksStopped, 1);
  fireEvent.click(screen.getByRole("button", { name: "Resume" }));
  await waitFor(() => assert.equal(status(), "Listening"));
  fireEvent.click(screen.getByRole("button", { name: "End" }));
  assert.equal(ended, true);
});

test("a refused microphone says so, and nothing is sent", async () => {
  const { deps, log } = harness([], () => ({ text: "", next: 0, cost_micros: 0 }));
  deps.media = async () => {
    throw new Error("NotAllowedError");
  };
  render(<VoiceMode session="s_1" onEnd={() => {}} deps={deps} />);
  await waitFor(() => assert.equal(status(), "The microphone is off"));
  assert.ok(screen.getByRole("alert").textContent?.includes("microphone"));
  assert.equal(log.posted.length, 0);
});
