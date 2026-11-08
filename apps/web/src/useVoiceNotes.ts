import { useEffect, useState } from "react";
import { api } from "./api.ts";
import type { VoiceNotes } from "./voice.ts";

// Read once a page load, and again after an admin changes the workspace's
// voice provider (forgetVoiceSettings).
type Read = { notes: VoiceNotes | null; speak: boolean };
let settings: Promise<Read> | undefined;

/** Reads the voice settings again on the next use: the voice provider changed. */
export function forgetVoiceSettings() {
  settings = undefined;
}

function read(): Promise<Read> {
  settings ??= api
    .voice()
    .then((s) => ({
      notes: s.transcribe ? { maxBytes: s.max_bytes, transcribe: api.transcribeVoice } : null,
      speak: s.speak === true,
    }))
    .catch(() => ({ notes: null, speak: false }));
  return settings;
}

/** Whether voice mode is on: the workspace has a voice provider. */
export function useVoiceMode(): boolean {
  const [speak, setSpeak] = useState(false);
  useEffect(() => {
    let live = true;
    void read().then((r) => {
      if (live) setSpeak(r.speak);
    });
    return () => {
      live = false;
    };
  }, []);
  return speak;
}

/** Voice notes, once the server says they are on; undefined until then, or when they are off. */
export function useVoiceNotes(): VoiceNotes | undefined {
  const [voice, setVoice] = useState<VoiceNotes>();
  useEffect(() => {
    let live = true;
    void read().then((r) => {
      if (live && r.notes) setVoice(r.notes);
    });
    return () => {
      live = false;
    };
  }, []);
  return voice;
}
