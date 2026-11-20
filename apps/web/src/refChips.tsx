import { defaultCopy } from "@humagen/components";
import { CalendarIcon, GitHubIcon, SlackIcon } from "@humagen/render-web";
import type { MouseEvent as ReactMouseEvent } from "react";
import { Pill, type ReferenceKind, Reference as SharedReference } from "./cards.tsx";
import type { Chip, Reference } from "./trace.ts";

const words = defaultCopy.trace;

function chipText(c: Chip): string {
  if (c.kind === "approval")
    return [words.decided[c.decision ?? "ask"], c.label, c.by ? words.by(c.by) : ""]
      .filter(Boolean)
      .join(" ");
  if (c.kind === "failure") return words.failedTool(c.label);
  return c.label;
}

/** What a chip's link points at, for its mark. */
function kindOf(c: Chip, origin?: Reference): ReferenceKind {
  if (c.kind === "origin" && origin)
    return origin.kind === "slack" || origin.kind === "github" || origin.kind === "schedule"
      ? origin.kind
      : "url";
  const host = c.url ? (/^https?:\/\/([^/]+)/.exec(c.url)?.[1] ?? "") : "";
  if (host.endsWith("slack.com")) return "slack";
  if (host === "github.com") return "github";
  return c.kind === "source" ? "file" : "url";
}

/**
 * Follow a link to another console page without reloading: a plain click
 * moves the address and tells the router, which reads it as Back would.
 */
export function inApp(event: ReactMouseEvent<HTMLAnchorElement>) {
  if (event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
    return;
  event.preventDefault();
  window.history.pushState(null, "", event.currentTarget.getAttribute("href") ?? "/");
  window.dispatchEvent(new PopStateEvent("popstate"));
}

/** Slack's or GitHub's mark, or a calendar for a schedule. */
export function OriginMark({ kind, size = 16 }: { kind: Reference["kind"]; size?: number }) {
  if (kind === "slack") return <SlackIcon size={size} aria-hidden />;
  if (kind === "github") return <GitHubIcon size={size} aria-hidden />;
  if (kind === "schedule") return <CalendarIcon size={size} aria-hidden />;
  return null;
}

/**
 * A message's references as quiet chips, each the shared Reference: its
 * origin, what it pulled in and the sources it cited, linked. An approval and
 * who decided it, or a tool that failed, is a state, so it is a Pill.
 */
export function Chips({ chips, origin }: { chips: Chip[]; origin?: Reference }) {
  if (!chips.length) return null;
  return (
    <ul className="flex flex-wrap gap-1" aria-label={words.references}>
      {chips.map((c) => {
        const text = chipText(c);
        const hint = c.detail ? `${text}: ${c.detail}` : text;
        return (
          <li
            key={`${c.kind}:${c.url ?? c.label}`}
            className="min-w-0"
            data-chip={c.kind}
            title={hint}
          >
            {c.kind === "approval" || c.kind === "failure" ? (
              <Pill
                tone={
                  c.kind === "failure" || c.decision === "deny"
                    ? "bad"
                    : c.decision === "ask"
                      ? "warn"
                      : "ok"
                }
              >
                {text}
              </Pill>
            ) : (
              <SharedReference kind={kindOf(c, origin)} label={text} href={c.url} title={hint} />
            )}
          </li>
        );
      })}
    </ul>
  );
}
