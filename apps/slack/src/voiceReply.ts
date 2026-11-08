// A spoken answer in Slack: when a person's message was a voice clip
// (humagen stores it `spoken`), the app answers in text as always and also
// uploads the answer as an audio clip in the same thread. humagen speaks it
// (POST /api/runs/{id}/speech, with the workspace's voice provider); this
// module only fetches the clip and uploads it. It needs `speech:write` on the
// app's humagen token and `files:write` on its Slack token.

import type { TokenSource } from "./humagen.ts";
import type { SlackApi } from "./slack.ts";

/** A finished run's answer as one clip, as humagen sends it. */
export interface SpokenAnswer {
  /** The clip, base64. */
  audio: string;
  media_type: string;
  /** What the clip says. */
  text: string;
  /** Said in full, not cut at humagen's bound. */
  whole: boolean;
  cost_micros: number;
}

/** Why no clip was posted, when that is expected and nothing went wrong. */
export type Skipped =
  | "no-voice" // the workspace has no voice provider, or the run is still working
  | "not-allowed" // the app's token lacks speech:write
  | "no-words" // the answer has nothing to say
  | "spend-limit"; // a spend limit over the run is reached

export type SpokenReply = { posted: true; file: string } | { posted: false; skipped: Skipped };

/** A refusal from humagen's speech route, with its status. */
export class SpeechError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

/** POST /api/runs/{id}/speech with the app's own credential. */
export function speechCall(cfg: {
  baseUrl: string;
  token: TokenSource;
  fetch?: typeof fetch;
}): (run: string) => Promise<SpokenAnswer> {
  const base = cfg.baseUrl.replace(/\/+$/, "");
  const call = cfg.fetch ?? fetch;
  return async (run) => {
    const once = async (fresh: boolean) => {
      const token = await cfg.token({ fresh });
      return call(`${base}/api/runs/${encodeURIComponent(run)}/speech`, {
        method: "POST",
        headers: token ? { authorization: `Bearer ${token}` } : {},
      });
    };
    let res = await once(false);
    if (res.status === 401) res = await once(true);
    const body = (await res.json().catch(() => ({}))) as Partial<SpokenAnswer> & { error?: string };
    if (!res.ok) throw new SpeechError(res.status, body.error ?? `speech failed (${res.status})`);
    return body as SpokenAnswer;
  };
}

const EXTENSIONS: Record<string, string> = {
  "audio/mpeg": "mp3",
  "audio/mp3": "mp3",
  "audio/wav": "wav",
  "audio/x-wav": "wav",
  "audio/ogg": "ogg",
};

/**
 * Uploads a finished run's answer as an audio clip into a Slack thread. Call it when
 * the run that answered a spoken message has completed, after its text is posted.
 * Expected refusals (no voice provider, no speech:write, no words, a spend limit)
 * answer `posted: false` with the reason; anything else throws, for the caller to log.
 */
export async function speakAnswer(
  deps: {
    speech: (run: string) => Promise<SpokenAnswer>;
    slack: SlackApi;
    /** Puts the bytes at Slack's upload address. */
    fetch?: typeof fetch;
  },
  at: { run: string; channel: string; threadTs: string; title?: string },
): Promise<SpokenReply> {
  let answer: SpokenAnswer;
  try {
    answer = await deps.speech(at.run);
  } catch (e) {
    const skipped = e instanceof SpeechError ? skippedBy(e.status) : undefined;
    if (skipped) return { posted: false, skipped };
    throw e;
  }
  const bytes = Buffer.from(answer.audio, "base64");
  if (!bytes.length) return { posted: false, skipped: "no-words" };
  const ext = EXTENSIONS[answer.media_type.split(";")[0]?.trim().toLowerCase() ?? ""] ?? "audio";
  const upload = await deps.slack.call<{ upload_url: string; file_id: string }>(
    "files.getUploadURLExternal",
    { filename: `answer.${ext}`, length: bytes.length },
  );
  const put = await (deps.fetch ?? fetch)(upload.upload_url, {
    method: "POST",
    headers: { "content-type": "application/octet-stream" },
    body: bytes,
  });
  if (!put.ok) throw new Error(`Slack refused the clip's upload (${put.status})`);
  await deps.slack.call("files.completeUploadExternal", {
    files: [{ id: upload.file_id, title: at.title ?? "Spoken answer" }],
    channel_id: at.channel,
    thread_ts: at.threadTs,
  });
  return { posted: true, file: upload.file_id };
}

function skippedBy(status: number): Skipped | undefined {
  switch (status) {
    case 409:
      return "no-voice";
    case 403:
      return "not-allowed";
    case 422:
      return "no-words";
    case 402:
      return "spend-limit";
  }
  return undefined;
}
