import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { ApiError, call } from "./api.ts";
import { itemGrid, Reference } from "./cards.tsx";
import { MessageTime } from "./MessageTime.tsx";
import { href, type Route } from "./route.ts";
import { SessionRow } from "./SessionCenter.tsx";
import { type AgentSessionSummary, type Alert, sessionCenter } from "./sessionCenter.ts";
import { referenceOf } from "./trace.ts";
import { Page, Problem, SkeletonRows } from "./ui.tsx";

const failure = (error: unknown) =>
  error instanceof Error ? error.message : "This could not be loaded. Try again.";

const words = defaultCopy.items;

/** A place that mentioned an item: a pasted link, a permalink, "#243". */
export type Backlink = { place: string; url?: string; at: string; session?: string };

/** An item, as `GET /api/items/{address}` returns it. */
export type Item = {
  address: string;
  kind: string;
  title: string;
  url?: string;
  sessions: AgentSessionSummary[];
  backlinks: Backlink[];
};

/**
 * An item and everything about it. Until the platform serves items, the
 * sessions rooted at its address stand for it (`GET /api/sessions?parent=`),
 * named by the address alone, with no backlinks.
 */
export async function loadItem(address: string): Promise<Item> {
  try {
    return await call<Item>("GET", `/api/items/${encodeURIComponent(address)}`);
  } catch (e) {
    if (!(e instanceof ApiError) || (e.status !== 404 && e.status !== 405)) throw e;
    const ref = referenceOf(address);
    const r = await sessionCenter.sessions({ parent: address, limit: "50" });
    return {
      address,
      kind: ref.kind,
      title: ref.label,
      url: ref.url,
      sessions: r.sessions,
      backlinks: [],
    };
  }
}

const link =
  "rounded-aui underline decoration-aui-border underline-offset-2 hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/**
 * An item's page (`/items/<address>`): what it is, with a link to it, every
 * session that worked on it and every place that mentioned it, each leading
 * to its trace.
 */
export function ItemPage({
  address,
  go,
  alerts = [],
  tick,
  load = loadItem,
}: {
  address: string;
  go: (route: Route) => void;
  alerts?: Alert[];
  tick?: unknown;
  load?: (address: string) => Promise<Item>;
}) {
  const [item, setItem] = useState<Item>();
  const [problem, setProblem] = useState<string>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: tick is the event invalidation signal.
  useEffect(() => {
    let live = true;
    setProblem(undefined);
    load(address).then(
      (r) => live && setItem(r),
      (e) => live && setProblem(failure(e)),
    );
    return () => {
      live = false;
    };
  }, [address, load, tick]);
  const ref = referenceOf(address);
  const url = item?.url ?? ref.url;
  return (
    <Page
      title={item?.title || ref.label}
      meta={words.kind(item?.kind ?? ref.kind)}
      actions={
        url ? (
          <a href={url} target="_blank" rel="noreferrer" className={link}>
            {defaultCopy.trace.place.openIn(placeName(ref.kind))}
          </a>
        ) : undefined
      }
    >
      <Problem message={problem ?? null} />
      {!item && !problem ? (
        <SkeletonRows count={3} />
      ) : item ? (
        <div className="flex flex-col gap-6">
          <section aria-labelledby="item-sessions" className="flex flex-col gap-3">
            <h2 id="item-sessions" className="font-medium text-aui-fg text-aui-sm">
              {words.sessions}
            </h2>
            {item.sessions.length ? (
              <ul className={itemGrid}>
                {item.sessions.map((s) => (
                  <SessionRow key={s.id} session={s} go={go} alerts={alerts} />
                ))}
              </ul>
            ) : (
              <p className="text-aui-muted text-aui-sm">{words.noSessions}</p>
            )}
          </section>
          {item.backlinks.length > 0 && (
            <section aria-labelledby="item-backlinks" className="flex flex-col gap-2">
              <h2 id="item-backlinks" className="font-medium text-aui-fg text-aui-sm">
                {words.backlinks}
              </h2>
              <ul className="flex flex-col gap-1.5 text-aui-sm">
                {item.backlinks.map((b) => {
                  const where = referenceOf(b.place);
                  const to = b.session
                    ? href({ page: "conversation", id: b.session })
                    : (b.url ?? where.url);
                  return (
                    <li
                      key={`${b.place}:${b.at}`}
                      className="flex flex-wrap items-baseline gap-x-2"
                    >
                      <Reference
                        kind={
                          where.kind === "slack" ||
                          where.kind === "github" ||
                          where.kind === "schedule"
                            ? where.kind
                            : "url"
                        }
                        label={where.label}
                        href={to}
                      />
                      <span className="text-aui-muted text-aui-xs">
                        <MessageTime at={b.at} />
                      </span>
                    </li>
                  );
                })}
              </ul>
            </section>
          )}
        </div>
      ) : null}
    </Page>
  );
}

function placeName(kind: string): string {
  return kind === "slack" ? "Slack" : kind === "github" ? "GitHub" : "a new tab";
}
