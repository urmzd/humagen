import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { type Place, placeOf } from "./trace.ts";

const words = defaultCopy.trace;

type ThreadPlace = Parameters<typeof placeOf>[0];

/**
 * The Slack team a client answers in, so a thread opens in Slack's web
 * client. Read once per page; a reader who may not list clients gets the
 * channel link instead.
 */
const teams = new Map<string, Promise<string>>();
function teamOf(client: string): Promise<string> {
  let p = teams.get(client);
  if (!p) {
    p = api
      .clients()
      .then(
        (r) => r.clients.find((c) => c.name === client || c.id === client)?.identity?.team ?? "",
      )
      .catch(() => "");
    teams.set(client, p);
  }
  return p;
}

/** A session's place and audience, worked out from its thread and, for Slack, its client's team. */
export function usePlace(thread: ThreadPlace | undefined): Place | undefined {
  const client = thread?.surface?.client ?? "";
  const [team, setTeam] = useState("");
  useEffect(() => {
    if (!client) return;
    let live = true;
    teamOf(client).then((t) => live && setTeam(t));
    return () => {
      live = false;
    };
  }, [client]);
  return thread ? placeOf(thread, team) : undefined;
}

const link =
  "min-h-6 rounded-aui underline decoration-aui-border underline-offset-2 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * Where a session started, linked back for the full conversation, and who
 * can read it there: "Slack thread · Slack channel", "urmzd/humagen PR #5 ·
 * Shared", "Console · Private". It sits in the session header's meta line.
 */
export function SessionPlace({ place }: { place: Place }) {
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2" data-testid="session-place">
      <span className="sr-only">{words.place.label}</span>
      {place.url ? (
        <a
          href={place.url}
          target="_blank"
          rel="noreferrer"
          className={link}
          title={words.place.openIn(place.label)}
        >
          {place.label}
        </a>
      ) : (
        <span>{place.label}</span>
      )}
      <span aria-hidden>·</span>
      <span>
        <span className="sr-only">{words.audience.label}: </span>
        {words.audience[place.audience]}
      </span>
    </span>
  );
}
