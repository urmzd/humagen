import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api } from "./api.ts";
import { OriginMark } from "./MessageRefs.tsx";
import { type Place, placeOf, type Reference } from "./trace.ts";

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
  return thread ? withScheduleLink(placeOf(thread, team)) : undefined;
}

/** A schedule opens on its own page here; Slack and GitHub open where they live. */
function withScheduleLink(place: Place): Place {
  if (place.kind !== "schedule" || !place.origin) return place;
  const name = place.origin.slice("schedule://".length);
  return { ...place, url: `/schedules/${encodeURIComponent(name)}` };
}

/** A place as a reference, for the origin chip under the root message. */
export function referenceOfPlace(place: Place | undefined): Reference | undefined {
  if (!place || place.kind === "console") return undefined;
  return { kind: place.kind, label: place.label, url: place.url };
}

function openWords(place: Place): string {
  return place.kind === "slack"
    ? words.place.openIn("Slack")
    : place.kind === "github"
      ? words.place.openIn("GitHub")
      : place.kind === "schedule"
        ? words.place.openSchedule
        : words.place.openIn(place.label);
}

/**
 * The header's first icon: the origin's own mark (Slack, GitHub, a calendar
 * for a schedule) linking to where the session started. A session started
 * on the web has none.
 */
export function OriginButton({ place }: { place: Place | undefined }) {
  if (!place || place.kind === "console" || !place.url) return null;
  const label = openWords(place);
  const external = place.kind !== "schedule";
  return (
    <a
      href={place.url}
      aria-label={label}
      title={label}
      data-testid="origin-button"
      {...(external ? { target: "_blank", rel: "noreferrer" } : {})}
      className="inline-flex size-9 shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
    >
      <OriginMark kind={place.kind} size={18} />
    </a>
  );
}

/**
 * The root message in the header's meta line: where it came from and who
 * sent it, and who can read it there. "Slack thread · from Ana · Slack
 * channel". A session started on the web needs no line.
 */
export function SessionPlace({ place, sender }: { place: Place; sender?: string }) {
  if (place.kind === "console") return null;
  return (
    <span className="inline-flex flex-wrap items-center gap-x-2" data-testid="session-place">
      <span className="sr-only">{words.place.label}</span>
      <span>{place.label}</span>
      {sender && (
        <>
          <span aria-hidden>·</span>
          <span>{words.place.from(sender)}</span>
        </>
      )}
      <span aria-hidden>·</span>
      <span>
        <span className="sr-only">{words.audience.label}: </span>
        {words.audience[place.audience]}
      </span>
    </span>
  );
}
