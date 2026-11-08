import type { AnswerRequest } from "@humagen/adapter/question";
import { defaultCopy } from "@humagen/components";
import type { RunEvent } from "./adapter.ts";
import { ApiError, api, type VoiceSpeech } from "./api.ts";
import { followSession } from "./sessionStream.ts";
import { DETECTOR, levelOf, VoiceDetector } from "./vad.ts";
import { recordingType } from "./voice.ts";
import {
  initialVoice,
  spokenAnswer,
  type VoiceEvent,
  type VoiceState,
  voiceReduce,
} from "./voiceMode.ts";

const words = defaultCopy.voiceMode;

/** An approval the agent waits on, as voice mode shows and answers it. */
export interface VoiceAsk {
  run: string;
  question: string;
  agent?: string;
  tool?: string;
  prompt: string;
}

/** What voice mode reaches outside itself. Tests replace each one. */
export interface VoiceDeps {
  media: () => Promise<MediaStream>;
  audioContext: () => AudioContext;
  recorder: (stream: MediaStream) => MediaRecorder;
  player: (src: string) => HTMLAudioElement;
  hear: (session: string, audio: Blob) => Promise<{ text: string }>;
  speak: (session: string, input: { run: string; from: number }) => Promise<VoiceSpeech>;
  /** Posts the words as a spoken message; answers the runs it started. */
  post: (session: string, text: string) => Promise<{ runs: { id: string; status: string }[] }>;
  answer: (r: AnswerRequest) => Promise<unknown>;
  stop: (run: string) => Promise<unknown>;
  follow: (session: string, onRun: (e: RunEvent) => void) => () => void;
  /** Says a short line in the browser's own voice, for an approval's ask. */
  say?: (text: string) => Promise<void>;
  /** How often the microphone's level is read. */
  frameMs: number;
}

export const browserVoice: VoiceDeps = {
  media: () =>
    navigator.mediaDevices.getUserMedia({
      audio: { echoCancellation: true, noiseSuppression: true, autoGainControl: true },
    }),
  audioContext: () => new AudioContext(),
  recorder: (stream) => {
    const mimeType = recordingType((t) => MediaRecorder.isTypeSupported(t));
    return new MediaRecorder(stream, mimeType ? { mimeType } : undefined);
  },
  player: (src) => new Audio(src),
  hear: (session, audio) => api.hearInSession(session, audio),
  speak: (session, input) => api.speakInSession(session, input),
  post: async (session, text) => {
    const { coworking } = await import("./api.ts");
    return coworking.post(session, { text, spoken: true } as Parameters<typeof coworking.post>[1]);
  },
  answer: (r) => api.answer(r),
  stop: (run) => api.stopRun(run),
  follow: (session, onRun) => followSession(session, { run: onRun }),
  say:
    typeof window !== "undefined" && "speechSynthesis" in window
      ? (text) =>
          new Promise<void>((resolve) => {
            const u = new SpeechSynthesisUtterance(text);
            u.onend = () => resolve();
            u.onerror = () => resolve();
            window.speechSynthesis.speak(u);
          })
      : undefined,
  frameMs: 30,
};

const WORKING = new Set(["queued", "accepted", "running", "parked", "waiting"]);

interface Tracked {
  /** Where speaking its answer goes on from, as the server last said. */
  from: number;
  /** A speech call is out. */
  asking: boolean;
  /** Its answer has been said in full, or is no longer to be said. */
  said: boolean;
  /** The run has ended. */
  ended: boolean;
  /** Text arrived while a call was out: ask again after it. */
  more: boolean;
}

/**
 * Runs voice mode in one session: listens, sends what the person says as a spoken
 * message, follows the runs it starts, speaks their answers as they are written, and
 * answers approvals by voice or by click. Its state goes to onState after every change.
 */
export class VoiceController {
  state: VoiceState = initialVoice;
  /** The level the visual follows, 0 to 1: the person's while hearing, the agent's while speaking. */
  level = 0;
  /** The open approvals, by question id. */
  asks = new Map<string, VoiceAsk>();
  /** Words heard while the agent worked, sent when it finishes. */
  queued: string[] = [];

  private stream?: MediaStream;
  private ctx?: AudioContext;
  private analyser?: AnalyserNode;
  private outAnalyser?: AnalyserNode;
  private frame?: ReturnType<typeof setInterval>;
  private detector = new VoiceDetector(DETECTOR);
  private recorder?: MediaRecorder;
  private chunks: Blob[] = [];
  private confirmed = false;
  private held = false;
  private quiet = false;
  private runs = new Map<string, Tracked>();
  private playing?: HTMLAudioElement;
  private queue: { src: string; text: string }[] = [];
  private unfollow?: () => void;
  private readonly session: string;
  private readonly deps: VoiceDeps;
  private readonly onState: (s: VoiceState) => void;

  constructor(session: string, deps: VoiceDeps, onState: (s: VoiceState) => void) {
    this.session = session;
    this.deps = deps;
    this.onState = onState;
  }

  private dispatch(e: VoiceEvent) {
    this.state = voiceReduce(this.state, e);
    this.onState(this.state);
  }

  /** Asks for the microphone and starts listening. */
  async start() {
    this.unfollow ??= this.deps.follow(this.session, (e) => this.onRun(e));
    await this.listen();
  }

  private async listen() {
    try {
      this.stream = await this.deps.media();
    } catch {
      this.dispatch({ type: "refused", problem: words.denied });
      return;
    }
    if (this.state.ended || this.state.paused) {
      this.release();
      return;
    }
    try {
      this.ctx ??= this.deps.audioContext();
      const source = this.ctx.createMediaStreamSource(this.stream);
      this.analyser = this.ctx.createAnalyser();
      this.analyser.fftSize = 1024;
      source.connect(this.analyser);
    } catch {
      // Without a level there is no detection; holding Space still talks.
      this.analyser = undefined;
    }
    const samples = new Float32Array(new ArrayBuffer(1024 * 4));
    this.frame = setInterval(() => this.tick(samples), this.deps.frameMs);
    this.dispatch({ type: "ready" });
  }

  private tick(samples: Float32Array<ArrayBuffer>) {
    if (this.state.paused || this.state.ended) return;
    const speaking = !!this.playing && !this.playing.paused;
    if (speaking && this.outAnalyser) {
      this.outAnalyser.getFloatTimeDomainData(samples);
      this.level = Math.min(1, levelOf(samples) * 4);
    }
    if (!this.analyser) return;
    this.analyser.getFloatTimeDomainData(samples);
    const mine = levelOf(samples);
    if (!speaking) this.level = Math.min(1, mine * 6);
    // Space held, or the browser saying an approval's ask: no detection.
    if (this.held || this.quiet) return;
    const e = this.detector.push(mine, this.deps.frameMs, speaking);
    if (this.detector.rising && !this.recorder) this.record();
    if (!this.detector.rising && !this.detector.speaking && this.recorder && !this.confirmed) {
      this.discard();
    }
    if (e === "start") this.voiceStarted();
    if (e === "end") this.finish();
  }

  private voiceStarted() {
    this.confirmed = true;
    if (!this.recorder) this.record();
    // Speaking over the agent holds its speech until the words are heard.
    if (this.playing && !this.playing.paused) this.playing.pause();
    this.dispatch({ type: "voiceStarted" });
  }

  private record() {
    if (!this.stream) return;
    this.chunks = [];
    this.confirmed = false;
    try {
      const r = this.deps.recorder(this.stream);
      r.ondataavailable = (e) => {
        if (e.data.size) this.chunks.push(e.data);
      };
      r.start(250);
      this.recorder = r;
    } catch {
      this.recorder = undefined;
    }
  }

  private discard() {
    const r = this.recorder;
    this.recorder = undefined;
    this.chunks = [];
    if (r && r.state !== "inactive") {
      r.onstop = null;
      r.stop();
    }
  }

  private finish() {
    const r = this.recorder;
    this.recorder = undefined;
    this.confirmed = false;
    if (!r) return;
    this.dispatch({ type: "voiceEnded" });
    r.onstop = () => {
      const audio = new Blob(this.chunks, { type: r.mimeType || "audio/webm" });
      this.chunks = [];
      void this.heard(audio);
    };
    if (r.state !== "inactive") r.stop();
  }

  /** Holding Space talks, whatever the detector says. */
  hold(down: boolean) {
    if (this.state.paused || this.state.ended || this.state.mic !== "on") return;
    if (down && !this.held) {
      this.held = true;
      this.detector.reset();
      this.discard();
      this.record();
      this.voiceStarted();
    } else if (!down && this.held) {
      this.held = false;
      this.finish();
    }
  }

  private async heard(audio: Blob) {
    let text = "";
    try {
      text = (await this.deps.hear(this.session, audio)).text.trim();
    } catch (e) {
      if (e instanceof ApiError && e.status === 422) {
        this.nothingHeard();
        return;
      }
      this.resumeSpeech();
      this.dispatch({ type: "problem", problem: words.failed(message(e)) });
      return;
    }
    if (!text) {
      this.nothingHeard();
      return;
    }
    if (this.state.ended || this.state.paused) return;
    // An approval waiting: a short yes or no answers it; anything else is asked again.
    if (this.asks.size === 1) {
      const answer = spokenAnswer(text);
      const ask = [...this.asks.values()][0];
      if (answer && ask) {
        this.dispatch({ type: "heard", text });
        await this.decide(ask.question, answer);
        return;
      }
    }
    if (this.asks.size > 0) {
      this.resumeSpeech();
      this.dispatch({ type: "heard", text });
      this.dispatch({ type: "problem", problem: words.answerHow });
      return;
    }
    const barged = this.state.bargedIn;
    this.dispatch({ type: "heard", text });
    if (barged) {
      // Words over the agent: stop its speech and its run, then send them.
      this.silence();
      const working = [...this.runs].filter(([, t]) => !t.ended).map(([id]) => id);
      await Promise.all(working.map((id) => this.deps.stop(id).catch(() => undefined)));
      await this.send(text);
      return;
    }
    if (this.state.working) {
      // The agent is working, not speaking: the words wait their turn, as a typed message does.
      this.queued.push(text);
      this.onState(this.state);
      return;
    }
    await this.send(text);
  }

  private nothingHeard() {
    this.dispatch({ type: "heardNothing" });
    this.resumeSpeech();
  }

  private async send(text: string) {
    try {
      const posted = await this.deps.post(this.session, text);
      const working = posted.runs.filter((r) => WORKING.has(r.status));
      for (const r of working) {
        this.runs.set(r.id, { from: 0, asking: false, said: false, ended: false, more: false });
      }
      if (working.length) this.dispatch({ type: "runStarted" });
    } catch (e) {
      this.dispatch({ type: "problem", problem: words.failed(message(e)) });
    }
  }

  /** Answers an approval, by voice or by click. */
  async decide(question: string, action: "approve" | "deny") {
    const ask = this.asks.get(question);
    if (!ask) return;
    try {
      await this.deps.answer({ run: ask.run, question, body: { action } });
      this.asks.delete(question);
      this.dispatch({ type: "answered", id: question });
    } catch (e) {
      this.dispatch({ type: "problem", problem: words.failed(message(e)) });
    }
  }

  private onRun(e: RunEvent) {
    const run = e.run;
    const t = run ? this.runs.get(run) : undefined;
    if (!run || !t || this.state.ended) return;
    const data = (e.data ?? {}) as Record<string, unknown>;
    switch (e.type) {
      case "run.delta.text.v1":
        this.pump(run);
        break;
      case "run.delta.tool.call.v1":
        if (typeof data.name === "string") this.dispatch({ type: "toolStarted", name: data.name });
        break;
      case "run.delta.tool.done.v1":
        this.dispatch({ type: "toolDone" });
        break;
      case "run.parked.v1": {
        const q = data.question as
          | {
              id?: string;
              kind?: string;
              agent?: string;
              prompt?: string;
              tool?: { name?: string };
            }
          | undefined;
        if (!q?.id || q.kind !== "approval") break;
        const ask: VoiceAsk = {
          run,
          question: q.id,
          agent: q.agent ?? e.agent,
          tool: q.tool?.name,
          prompt: q.prompt ?? "",
        };
        this.asks.set(q.id, ask);
        this.dispatch({ type: "asked", id: q.id });
        void this.sayAsk(ask);
        break;
      }
      case "run.resumed.v1": {
        const a = data.answer as { question_id?: string } | undefined;
        if (a?.question_id && this.asks.delete(a.question_id)) {
          this.dispatch({ type: "answered", id: a.question_id });
        }
        break;
      }
      case "run.completed.v1":
      case "run.stopped.v1":
      case "run.failed.v1":
        if (e.type === "run.failed.v1" && data.terminal === false) break;
        t.ended = true;
        for (const [id, ask] of this.asks) if (ask.run === run) this.asks.delete(id);
        this.pump(run);
        if ([...this.runs.values()].every((r) => r.ended)) {
          this.dispatch({ type: "runEnded" });
          const next = this.queued.shift();
          if (next) void this.send(next);
        }
        break;
    }
  }

  /** Asks for the next part of a run's answer, until it is pending or said in full. */
  private pump(run: string) {
    const t = this.runs.get(run);
    if (!t || t.said || this.state.paused || this.state.ended) return;
    if (t.asking) {
      t.more = true;
      return;
    }
    t.asking = true;
    t.more = false;
    this.deps
      .speak(this.session, { run, from: t.from })
      .then((r) => {
        t.asking = false;
        t.from = r.next;
        if (r.audio && r.media_type) this.enqueue(`data:${r.media_type};base64,${r.audio}`, r.text);
        if (r.done) t.said = true;
        // More may be written already, or the run may have ended meanwhile.
        if (!r.done && (!r.pending || t.more || t.ended)) this.pump(run);
        this.settle();
      })
      .catch((e) => {
        t.asking = false;
        t.said = true;
        this.dispatch({ type: "problem", problem: words.failed(message(e)) });
        this.settle();
      });
  }

  private enqueue(src: string, text: string) {
    if (this.state.paused || this.state.ended) return;
    this.queue.push({ src, text });
    if (!this.playing) this.playNext();
  }

  private playNext() {
    const next = this.queue.shift();
    if (!next) {
      this.playing = undefined;
      this.settle();
      return;
    }
    const audio = this.deps.player(next.src);
    this.playing = audio;
    this.outAnalyser = undefined;
    if (this.ctx) {
      try {
        const source = this.ctx.createMediaElementSource(audio);
        this.outAnalyser = this.ctx.createAnalyser();
        source.connect(this.outAnalyser);
        this.outAnalyser.connect(this.ctx.destination);
      } catch {
        this.outAnalyser = undefined;
      }
    }
    audio.onended = () => this.playNext();
    audio.onerror = () => this.playNext();
    this.dispatch({ type: "speakStarted", text: next.text });
    audio.play().catch(() => this.playNext());
  }

  /** Speech has stopped when nothing plays, nothing waits to and no part is being fetched. */
  private settle() {
    if (this.playing || this.queue.length) return;
    if ([...this.runs.values()].some((t) => t.asking)) return;
    if (this.state.speaking) this.dispatch({ type: "speakDone" });
  }

  private resumeSpeech() {
    if (this.playing?.paused && !this.state.paused)
      this.playing.play().catch(() => this.playNext());
  }

  /** Stops speaking and drops what was left to say. */
  private silence() {
    this.queue = [];
    const p = this.playing;
    this.playing = undefined;
    if (p) {
      p.onended = null;
      p.pause();
    }
    for (const t of this.runs.values()) t.said = true;
    if (this.state.speaking) this.dispatch({ type: "speakDone" });
  }

  private async sayAsk(ask: VoiceAsk) {
    if (!this.deps.say || this.state.paused) return;
    // The microphone is closed while the ask is said, so it cannot answer itself.
    this.quiet = true;
    this.discard();
    this.detector.reset();
    try {
      await this.deps.say(words.asksAloud(ask.agent ?? "The agent", ask.tool ?? "a tool"));
    } finally {
      this.quiet = false;
    }
  }

  /** Stops listening and speaking; the session and its runs go on. */
  pause() {
    this.silence();
    this.discard();
    this.held = false;
    this.release();
    this.dispatch({ type: "pause" });
  }

  async resume() {
    this.dispatch({ type: "resume" });
    await this.listen();
  }

  /** Leaves voice mode: nothing more is heard or said. */
  end() {
    this.silence();
    this.discard();
    this.release();
    this.unfollow?.();
    this.unfollow = undefined;
    void this.ctx?.close().catch(() => undefined);
    this.ctx = undefined;
    this.dispatch({ type: "end" });
  }

  private release() {
    if (this.frame) clearInterval(this.frame);
    this.frame = undefined;
    for (const track of this.stream?.getTracks() ?? []) track.stop();
    this.stream = undefined;
    this.analyser = undefined;
    this.detector.reset();
    this.level = 0;
  }
}

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
