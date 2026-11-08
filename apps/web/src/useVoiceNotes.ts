import { useEffect, useState } from "react";
import { api } from "./api.ts";
import type { VoiceNotes } from "./voice.ts";

// Read once a page load, and again after an admin changes the workspace's
// voice provider (forgetVoiceSettings).
let settings: Promise<VoiceNotes | null> | undefined;

/** Reads the voice settings again on the next use: the voice provider changed. */
export function forgetVoiceSettings() {
  settings = undefined;
}

/** Voice notes, once the server says they are on; undefined until then, or when they are off. */
export function useVoiceNotes(): VoiceNotes | undefined {
  const [voice, setVoice] = useState<VoiceNotes>();
  useEffect(() => {
    let live = true;
    settings ??= api
      .voice()
      .then((s) =>
        s.transcribe ? { maxBytes: s.max_bytes, transcribe: api.transcribeVoice } : null,
      )
      .catch(() => null);
    void settings.then((v) => {
      if (live && v) setVoice(v);
    });
    return () => {
      live = false;
    };
  }, []);
  return voice;
}
