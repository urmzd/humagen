/**
 * A person acting on a session's browser by hand: POST
 * /api/sessions/{id}/browser. The server holds the call to what the agent's
 * own is held to (internal/platform/browser_view.go) and answers with the
 * page afterwards. Nothing here is sent to the agent.
 */
import type {
  BrowserAction,
  BrowserView as WireView,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";

export type {
  BrowserAction,
  BrowserElement,
} from "../../../packages/client/src/generated/types.ts";

/** The page after an action. The screenshot's bytes arrive as base64 text. */
export type BrowserView = Omit<WireView, "screenshot"> & {
  screenshot?: { media_type: string; data: string };
};

export type BrowserAct = (session: string, action: BrowserAction) => Promise<BrowserView>;

export const sessionBrowser: BrowserAct = (session, action) =>
  call<BrowserView>("POST", `/api/sessions/${encodeURIComponent(session)}/browser`, action);
