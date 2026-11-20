import "./react.ts";
import { runDetails } from "@humagen/adapter";
import { type Copy, defaultCopy, RunDetailsView } from "@humagen/components";
import { type Block, renderToSlack } from "@humagen/render-slack";
import type { ActAs, ClientRecord, Humagen } from "./humagen.ts";

/**
 * Run details: what a finished run used, on request.
 *
 * A turn in Slack never shows tokens or cost, since a whole channel reads it.
 * A finished turn offers Run Details instead, and a click answers the person
 * who clicked, alone, in an ephemeral message: the run's agent, model,
 * tokens, cost and elapsed time, read from the run's record
 * (`GET /api/runs/{id}`).
 *
 * Humagen decides who may read. The run is read as the person the app's
 * client maps the Slack user to (`act_as_client`, `act_as_user`), never as
 * the app's own machine, so a person sees a run's usage in Slack only where
 * they may read the run in the console. A Slack user the client maps to
 * nobody is told so, and Humagen is not asked.
 */

/** Whether this app can answer Run Details at all: its client names it and maps at least one person. */
export function offersRunDetails(
  client: ClientRecord | null | undefined,
  subject: string,
): boolean {
  return (
    !!client?.enabled &&
    client.slack?.machine === subject &&
    Object.keys(client.slack?.people ?? {}).length > 0
  );
}

export interface DetailsReply {
  /** Why it was refused, for a log line. Absent when the details were read. */
  refused?: "not-connected" | "not-readable" | "unavailable";
  /** The message's text, and its fallback when it has blocks. */
  text: string;
  blocks?: Block[];
}

/** The ephemeral message that answers one person's click on Run Details. */
export async function runDetailsReply(
  deps: {
    humagen: Pick<Humagen, "getRunFor">;
    /** Who the Slack user is to the app's client, or null (see `App.actAs`). */
    actAs: (user: string) => Promise<ActAs | null>;
    copy?: Copy;
  },
  runId: string,
  user: string,
): Promise<DetailsReply> {
  const copy = deps.copy ?? defaultCopy;
  const as = await deps.actAs(user).catch(() => null);
  if (!as) return { refused: "not-connected", text: copy.runDetails.notConnected };
  let run: Awaited<ReturnType<Humagen["getRunFor"]>>;
  try {
    run = await deps.humagen.getRunFor(runId, as);
  } catch {
    return { refused: "unavailable", text: copy.runDetails.unavailable };
  }
  // Humagen answers 404 for a run the person may not read, as for one that does not exist.
  if (!run) return { refused: "not-readable", text: copy.runDetails.notReadable };
  const view = renderToSlack(<RunDetailsView details={runDetails(run)} copy={copy} />);
  return { text: view.text, blocks: view.pages[0] ?? [] };
}
