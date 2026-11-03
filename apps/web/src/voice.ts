import { defaultCopy } from "@humagen/components/copy";
import type { VoiceTranscript } from "../../../packages/client/src/generated/types.ts";

/**
 * Voice notes from the composer: a recording the deployment transcribes
 * (POST /api/voice/transcriptions), its words put in the draft for the
 * person to read and send. Present only when the deployment has a speech
 * kind and the person holds speech:write.
 */
export interface VoiceNotes {
  /** The largest recording the server takes. */
  maxBytes: number;
  transcribe: (audio: Blob) => Promise<VoiceTranscript>;
}

/** The longest a recording runs before it stops by itself. */
export const MAX_RECORDING_MS = 5 * 60 * 1000;

/**
 * The draft with a transcript after it, marked as a voice note the way the
 * server marks one that arrives from Slack, so an agent reads it the same
 * from anywhere.
 */
export function withVoiceNote(draft: string, t: VoiceTranscript): string {
  const note = `${defaultCopy.voice.mark(t.language || undefined)} ${t.text.trim()}`;
  const base = draft.trimEnd();
  return base ? `${base}\n\n${note}` : note;
}

/** The first type the browser's recorder can make: Opus first, then what Safari makes. */
export function recordingType(supported: (type: string) => boolean): string | undefined {
  return ["audio/webm;codecs=opus", "audio/ogg;codecs=opus", "audio/webm", "audio/mp4"].find(
    (t) => {
      try {
        return supported(t);
      } catch {
        return false;
      }
    },
  );
}

/** Whether this browser can record at all. */
export function canRecord(): boolean {
  return (
    typeof MediaRecorder !== "undefined" &&
    typeof navigator !== "undefined" &&
    !!navigator.mediaDevices?.getUserMedia
  );
}
