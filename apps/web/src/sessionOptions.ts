import { useEffect, useState } from "react";
import { call } from "./api.ts";

// What a new session may be set up with, read once per page and again while
// one of its images is still being checked. The pickers that show it under
// Home's composer are SessionPickers.tsx.

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

/** How often options are read again while one of their images is checking. */
const SETTLE_MS = 3000;

/** The one read every picker on the page shares. */
let optionsRead: Promise<SessionOptions> | undefined;
function readOnce(): Promise<SessionOptions> {
  const mine = call<SessionOptions>("GET", "/api/sessions/options").then(
    (o) => {
      // An image still checking will be ready or failed soon: this answer
      // is not kept, so the next read asks again.
      if (optionsRead === mine && o?.images?.some?.((i) => i.status === "checking"))
        optionsRead = undefined;
      return o;
    },
    (e) => {
      if (optionsRead === mine) optionsRead = undefined;
      throw e;
    },
  );
  return mine;
}

/**
 * The images and repositories a new session may have, read once per page.
 * While one of the images is still checking they are read again every
 * settleMs, so an image that has just become ready can be chosen without
 * loading the page again.
 */
export function useSessionOptions(
  enabled = true,
  read: () => Promise<SessionOptions> = () => (optionsRead ??= readOnce()),
  settleMs = SETTLE_MS,
): SessionOptions | null | undefined {
  const [options, setOptions] = useState<SessionOptions | null>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: read once, and again only while an image is checking.
  useEffect(() => {
    if (!enabled) return;
    let on = true;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const go = () =>
      read().then(
        (o) => {
          if (!on) return;
          // An answer not in this shape, from an older server, offers nothing.
          const ok = Array.isArray(o?.images) && Array.isArray(o.repos);
          setOptions(ok ? o : null);
          if (ok && o.images.some((i) => i.status === "checking")) timer = setTimeout(go, settleMs);
        },
        () => on && setOptions(null),
      );
    go();
    return () => {
      on = false;
      clearTimeout(timer);
    };
  }, []);
  return options;
}

/** Forget the options read, so the next picker reads them again: after an image is added. */
export function forgetSessionOptions(): void {
  optionsRead = undefined;
}
