// Voice activity detection on the microphone's level, as plain arithmetic, so
// it is tested without a microphone. The page feeds it one level a frame (the
// RMS of the samples, 0 to 1) and how long the frame was.

export interface DetectorOptions {
  /** How long the level stays up before speech starts. */
  startMs: number;
  /** How long it stays down before speech ends. */
  endMs: number;
  /** The quietest level that counts as speech, whatever the room. */
  minLevel: number;
  /** How far over the room's noise a level must be. */
  ratio: number;
  /** The longest one utterance runs before it is cut and sent. */
  maxMs: number;
}

export const DETECTOR: DetectorOptions = {
  startMs: 180,
  endMs: 900,
  minLevel: 0.015,
  ratio: 3,
  maxMs: 60_000,
};

/**
 * Says when speech starts and ends. The room's noise is learned while nobody speaks.
 * While the agent speaks, the bar is higher and longer, so its own voice, mostly removed
 * by the browser's echo cancellation, does not interrupt it.
 */
export class VoiceDetector {
  private floor = 0.005;
  private above = 0;
  private below = 0;
  private length = 0;
  private on = false;
  private readonly o: DetectorOptions;

  constructor(o: DetectorOptions = DETECTOR) {
    this.o = o;
  }

  get speaking(): boolean {
    return this.on;
  }

  /** The level is up but not yet for long enough to be speech: record, in case it is. */
  get rising(): boolean {
    return !this.on && this.above > 0;
  }

  /** The level a frame must pass to count as speech. */
  threshold(agentSpeaking = false): number {
    const t = Math.max(this.o.minLevel, this.floor * this.o.ratio);
    return agentSpeaking ? t * 2 : t;
  }

  /** Feeds one frame; answers "start" or "end" when speech starts or ends there. */
  push(level: number, ms: number, agentSpeaking = false): "start" | "end" | undefined {
    const loud = level > this.threshold(agentSpeaking);
    if (!this.on) {
      if (!loud) {
        // Quiet frames teach the room's noise, slowly.
        this.floor = this.floor * 0.95 + Math.max(level, 0.001) * 0.05;
        this.above = 0;
        return undefined;
      }
      this.above += ms;
      if (this.above >= (agentSpeaking ? this.o.startMs * 1.5 : this.o.startMs)) {
        this.on = true;
        this.below = 0;
        this.length = this.above;
        return "start";
      }
      return undefined;
    }
    this.length += ms;
    this.below = loud ? 0 : this.below + ms;
    if (this.below >= this.o.endMs || this.length >= this.o.maxMs) {
      this.on = false;
      this.above = 0;
      return "end";
    }
    return undefined;
  }

  /** Forgets an utterance in progress, as when voice mode pauses. */
  reset() {
    this.on = false;
    this.above = 0;
    this.below = 0;
    this.length = 0;
  }
}

/** The RMS of one frame of samples. */
export function levelOf(samples: Float32Array): number {
  let sum = 0;
  for (const v of samples) sum += v * v;
  return samples.length ? Math.sqrt(sum / samples.length) : 0;
}
