import { useEffect, useState } from "react";
import { call } from "./api.ts";

// What a new session may be set up with, read once per page. Kept apart from
// SessionPickers.tsx, which Home reads only when it draws the pickers, so the
// composer can hold a session's choices without loading the pickers.

/** One image a new session may run in (`GET /api/sessions/options`). */
export interface SessionImage {
  /** "default" is the deployment's own. */
  id: string;
  name: string;
  ref: string;
  /** The reference pinned, repo@sha256:..., once its check resolved it. */
  digest?: string;
  status: "checking" | "ready" | "failed";
  problem?: string;
  /** An instance is kept started, so a session does not wait for one. */
  warm: boolean;
  default: boolean;
}

/** What a new session may be set up with. */
export interface SessionOptions {
  images: SessionImage[];
  /** Where the workspace may add images from; empty when it may add none. */
  imageRegistries: string[];
  sandbox: string;
  isolated: boolean;
  terminal: boolean;
  repos: string[];
}

/** A new session's choices, sent with its first message and kept on it. */
export interface SessionSetup {
  image: string;
  repos: string[];
}

export const DEFAULT_SETUP: SessionSetup = { image: "default", repos: [] };

/** The images and repositories a new session may have, read once per page. */
let optionsRead: Promise<SessionOptions> | undefined;
export function useSessionOptions(
  enabled = true,
  read: () => Promise<SessionOptions> = () =>
    (optionsRead ??= call<SessionOptions>("GET", "/api/sessions/options").catch((e) => {
      optionsRead = undefined;
      throw e;
    })),
): SessionOptions | null | undefined {
  const [options, setOptions] = useState<SessionOptions | null>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: read once.
  useEffect(() => {
    if (!enabled) return;
    let on = true;
    read().then(
      (o) => on && setOptions(o),
      () => on && setOptions(null),
    );
    return () => {
      on = false;
    };
  }, []);
  return options;
}

/** Forget the options read, so the next picker reads them again: after an image is added. */
export function forgetSessionOptions(): void {
  optionsRead = undefined;
}
