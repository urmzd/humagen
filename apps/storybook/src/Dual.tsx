import type { UiAction } from "@agent-ui/core";
import { blockKitBuilderUrl, type Placement, renderToSlack } from "@agent-ui/render-slack";
import { BlockKitPreview } from "@agent-ui/render-slack/preview";
import { WebSurface } from "@agent-ui/render-web";
import { type ReactNode, useState } from "react";

/** Where a component exists. Web-only components name what Slack uses natively instead. */
export type Availability =
  | { kind: "both" }
  | { kind: "web-only"; slackNative: string; slackPayload?: unknown };

/**
 * Renders one element on every surface, side by side. The same React tree goes
 * to the DOM renderer and to the Block Kit renderer.
 */
export function Dual({
  children,
  web,
  placement = "message",
  availability = { kind: "both" },
}: {
  children?: ReactNode;
  /** Web-only content rendered outside the shared primitive tree (prompt bar, live hooks). */
  web?: ReactNode;
  placement?: Placement;
  availability?: Availability;
}) {
  const [last, setLast] = useState<UiAction | null>(null);
  let slack: ReturnType<typeof renderToSlack> | null = null;
  let error: string | null = null;
  if (availability.kind === "both") {
    try {
      slack = renderToSlack(children, { placement });
    } catch (e) {
      error = e instanceof Error ? e.message : String(e);
    }
  }

  return (
    <div className="flex flex-col gap-4">
      <Badge availability={availability} />
      <div className="grid gap-6 lg:grid-cols-2">
        <section className="flex min-w-0 flex-col gap-2">
          <Label>Web</Label>
          <div className="rounded-aui border border-aui-border bg-aui-surface p-4">
            {web ?? <WebSurface onAction={setLast}>{children}</WebSurface>}
          </div>
          {last && (
            <p className="font-mono text-xs text-aui-muted">
              action: {last.id} {JSON.stringify(last.payload)}
            </p>
          )}
        </section>
        <section className="flex min-w-0 flex-col gap-2">
          <Label>
            Slack · {availability.kind === "both" ? placement : "native"}
            {slack && (
              <a
                className="ml-3 font-normal normal-case text-aui-accent underline"
                href={blockKitBuilderUrl(slack.blocks, placement)}
                target="_blank"
                rel="noreferrer"
              >
                Open in Block Kit Builder
              </a>
            )}
          </Label>
          {availability.kind === "web-only" && (
            <div className="flex flex-col gap-2 rounded-aui border border-dashed border-aui-border p-4 text-sm">
              <p className="text-aui-fg">{availability.slackNative}</p>
              {availability.slackPayload !== undefined && (
                <pre className="overflow-auto rounded-md bg-aui-code p-3 font-mono text-xs">
                  {JSON.stringify(availability.slackPayload, null, 2)}
                </pre>
              )}
            </div>
          )}
          {error && <p className="text-sm text-aui-bad">{error}</p>}
          {slack && (
            <>
              <BlockKitPreview blocks={slack.blocks} />
              <p className="text-xs text-aui-muted">
                <span className="font-semibold">Fallback text:</span> {slack.text}
              </p>
              {slack.warnings.length > 0 && (
                <ul className="list-disc pl-5 text-xs text-aui-warn">
                  {slack.warnings.map((w) => (
                    <li key={w}>{w}</li>
                  ))}
                </ul>
              )}
              <details className="text-xs">
                <summary className="cursor-pointer text-aui-muted">
                  Block Kit JSON ({slack.blocks.length} blocks)
                </summary>
                <pre className="mt-2 max-h-96 overflow-auto rounded-md bg-aui-code p-3 font-mono">
                  {JSON.stringify({ text: slack.text, blocks: slack.blocks }, null, 2)}
                </pre>
              </details>
            </>
          )}
        </section>
      </div>
    </div>
  );
}

function Badge({ availability }: { availability: Availability }) {
  const both = availability.kind === "both";
  return (
    <p className="flex gap-2 text-xs">
      <span className="rounded-full bg-aui-primary px-2 py-0.5 font-medium text-aui-primary-fg">
        Web
      </span>
      <span
        className={`rounded-full px-2 py-0.5 font-medium ${
          both
            ? "bg-aui-accent text-aui-accent-fg"
            : "border border-aui-border text-aui-muted line-through"
        }`}
      >
        Slack
      </span>
      {!both && <span className="text-aui-muted">Web only. Slack covers this natively.</span>}
    </p>
  );
}

function Label({ children }: { children: ReactNode }) {
  return (
    <h2 className="text-xs font-semibold uppercase tracking-wide text-aui-muted">{children}</h2>
  );
}
