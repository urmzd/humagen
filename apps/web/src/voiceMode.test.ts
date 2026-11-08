import assert from "node:assert/strict";
import { test } from "node:test";
import { DETECTOR, levelOf, VoiceDetector } from "./vad.ts";
import {
  announced,
  initialVoice,
  spokenAnswer,
  type VoiceEvent,
  type VoicePhase,
  type VoiceState,
  voicePhase,
  voiceReduce,
  waitsOnPerson,
} from "./voiceMode.ts";

const run = (...events: VoiceEvent[]): VoiceState => events.reduce(voiceReduce, initialVoice);
const phases = (...events: VoiceEvent[]): VoicePhase[] => {
  let s = initialVoice;
  return events.map((e) => {
    s = voiceReduce(s, e);
    return voicePhase(s);
  });
};

test("a turn goes listening, hearing, sending, thinking, using a tool, speaking, listening", () => {
  assert.deepEqual(
    phases(
      { type: "ready" },
      { type: "voiceStarted" },
      { type: "voiceEnded" },
      { type: "heard", text: "What's on my calendar?" },
      { type: "runStarted" },
      { type: "toolStarted", name: "calendar_list" },
      { type: "toolDone" },
      { type: "speakStarted", text: "You have two meetings." },
      { type: "runEnded" },
      { type: "speakDone" },
    ),
    [
      "listening",
      "hearing",
      "sending",
      "listening",
      "thinking",
      "tool",
      "thinking",
      "speaking",
      "speaking",
      "listening",
    ],
  );
});

test("speaking over the agent holds its speech; words end it, silence lets it go on", () => {
  const over = run(
    { type: "ready" },
    { type: "runStarted" },
    { type: "speakStarted", text: "First," },
    { type: "voiceStarted" },
  );
  assert.equal(voicePhase(over), "hearing");
  assert.equal(over.bargedIn, true);
  const words = voiceReduce(voiceReduce(over, { type: "voiceEnded" }), {
    type: "heard",
    text: "Stop, do the other one",
  });
  assert.equal(words.speaking, false);
  assert.equal(words.bargedIn, false);
  const cough = voiceReduce(voiceReduce(over, { type: "voiceEnded" }), { type: "heardNothing" });
  assert.equal(cough.speaking, true);
  assert.equal(voicePhase(cough), "speaking");
});

test("an approval waits on the person until answered, and is never shown while speaking it", () => {
  let s = run({ type: "ready" }, { type: "runStarted" }, { type: "asked", id: "q1" });
  assert.equal(voicePhase(s), "approval");
  s = voiceReduce(s, { type: "speakStarted", text: "Builder wants to run git push." });
  assert.equal(voicePhase(s), "speaking");
  s = voiceReduce(s, { type: "speakDone" });
  assert.equal(voicePhase(s), "approval");
  s = voiceReduce(s, { type: "asked", id: "q1" });
  assert.deepEqual(s.asks, ["q1"]);
  s = voiceReduce(s, { type: "answered", id: "q1" });
  assert.equal(voicePhase(s), "thinking");
});

test("pause stops hearing and speaking and keeps the run; resume listens; end is final", () => {
  let s = run(
    { type: "ready" },
    { type: "runStarted" },
    { type: "speakStarted", text: "Hi" },
    { type: "pause" },
  );
  assert.equal(voicePhase(s), "paused");
  assert.equal(s.speaking, false);
  assert.equal(s.working, true);
  s = voiceReduce(s, { type: "resume" });
  assert.equal(voicePhase(s), "thinking");
  s = voiceReduce(s, { type: "end" });
  assert.equal(voicePhase(s), "ended");
  assert.equal(voiceReduce(s, { type: "resume" }), s);
});

test("a refused microphone fails; a passing problem goes back to listening with the reason", () => {
  assert.equal(voicePhase(run({ type: "refused", problem: "blocked" })), "failed");
  const s = run(
    { type: "ready" },
    { type: "voiceStarted" },
    { type: "voiceEnded" },
    { type: "problem", problem: "too long" },
  );
  assert.equal(voicePhase(s), "listening");
  assert.equal(s.problem, "too long");
});

test("nothing waiting on the person moves, and passing phases are not announced", () => {
  for (const p of ["listening", "paused", "approval", "ended", "failed", "starting"] as const) {
    assert.equal(waitsOnPerson(p), true, p);
  }
  for (const p of ["hearing", "thinking", "tool", "speaking"] as const) {
    assert.equal(waitsOnPerson(p), false, p);
  }
  assert.equal(announced("hearing"), false);
  assert.equal(announced("sending"), false);
  assert.equal(announced("approval"), true);
});

test("only a short answer that starts with an answer word answers an approval", () => {
  assert.equal(spokenAnswer("Yes."), "approve");
  assert.equal(spokenAnswer("go ahead"), "approve");
  assert.equal(spokenAnswer("OK, do it"), "approve");
  assert.equal(spokenAnswer("No, don't."), "deny");
  assert.equal(spokenAnswer("deny"), "deny");
  assert.equal(spokenAnswer("Builder wants to push to main, approve or deny?"), undefined);
  assert.equal(spokenAnswer("maybe later"), undefined);
  assert.equal(spokenAnswer(""), undefined);
});

test("speech starts after the level stays up, ends after it stays down, and the room's noise is learned", () => {
  const d = new VoiceDetector();
  const feed = (level: number, frames: number, agent = false) => {
    const out: string[] = [];
    for (let i = 0; i < frames; i++) {
      const e = d.push(level, 20, agent);
      if (e) out.push(e);
    }
    return out;
  };
  assert.deepEqual(feed(0.003, 50), []);
  // A click shorter than the start time is not speech.
  assert.deepEqual(feed(0.2, 3), []);
  assert.deepEqual(feed(0.003, 5), []);
  assert.deepEqual(feed(0.2, 10), ["start"]);
  // A short pause inside a sentence does not end it.
  assert.deepEqual(feed(0.003, 20), []);
  assert.deepEqual(feed(0.2, 5), []);
  assert.deepEqual(feed(0.003, DETECTOR.endMs / 20), ["end"]);
  // While the agent speaks, the bar is higher.
  const t = d.threshold();
  assert.ok(d.threshold(true) > t);
  assert.deepEqual(feed(t * 1.5, 30, true), []);
});

test("one utterance is cut at the longest it may run", () => {
  const d = new VoiceDetector({ ...DETECTOR, maxMs: 1000 });
  const events: string[] = [];
  for (let i = 0; i < 100; i++) {
    const e = d.push(0.3, 20);
    if (e) events.push(e);
  }
  assert.deepEqual(events.slice(0, 2), ["start", "end"]);
  assert.equal(levelOf(new Float32Array([0.5, -0.5, 0.5, -0.5])), 0.5);
  assert.equal(levelOf(new Float32Array([])), 0);
});
