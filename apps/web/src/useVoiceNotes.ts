import { useEffect, useState } from "react";
import { api } from "./api.ts";
import type { VoiceNotes } from "./voice.ts";

// Read once a page load: whether voice notes are on does not change while
// the server runs.
let settings: Promise<VoiceNotes | null> | undefined;

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
