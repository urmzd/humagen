import { Markdown } from "@vultus/primitives";
import {
  BrainIcon,
  Collapse,
  ShimmerVerb,
  Spinner,
  useDisclosure,
  useVerbSequence,
} from "@vultus/render-web";
import { useEffect, useId, useRef, useState } from "react";

const REASONING_VERBS = ["Thinking…", "Weighing options…", "Planning next steps…"] as const;

function formatSeconds(s: number) {
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  const rest = s % 60;
  return rest === 0 ? `${m}m` : `${m}m ${rest}s`;
}

/**
 * The model's reasoning trace, folded under one line. Open and shimmering
 * while it streams; folds itself a beat after it settles (once, and never
 * against the viewer's own toggle), then reads "Thought for 8s".
 *
 * Web only: Slack has no reasoning surface, so TurnView leaves it out.
 * Render inside a WebSurface.
 */
export function Reasoning({
  text,
  streaming = false,
  durationMs,
}: {
  text: string;
  streaming?: boolean;
  /** Known duration; otherwise it is measured from when streaming was seen. */
  durationMs?: number;
}) {
  const id = useId();
  const started = useRef<number | null>(streaming ? Date.now() : null);
  const [measured, setMeasured] = useState<number | undefined>(undefined);
  const { open, setOpen, toggle } = useDisclosure({ defaultOpen: streaming });
  const wasStreaming = useRef(streaming);
  const verb = useVerbSequence(streaming, REASONING_VERBS, 2200);

  useEffect(() => {
    if (streaming && started.current === null) started.current = Date.now();
    if (wasStreaming.current && !streaming) {
      if (started.current !== null) setMeasured(Date.now() - started.current);
      const t = setTimeout(() => {
        wasStreaming.current = false;
        setOpen(false);
      }, 1000);
      return () => clearTimeout(t);
    }
    wasStreaming.current = streaming;
  }, [streaming, setOpen]);

  const ms = durationMs ?? measured;
  const label = streaming
    ? null
    : ms !== undefined
      ? `Thought for ${formatSeconds(Math.max(1, Math.round(ms / 1000)))}`
      : "Thought for a few seconds";

  return (
    <div data-aui="reasoning" className="flex flex-col">
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={toggle}
        className="group -mx-1.5 inline-flex w-fit max-w-full items-center gap-2 rounded-aui-sm px-1.5 py-1 text-aui-muted text-aui-sm transition-colors hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
      >
        {streaming ? <Spinner size={14} className="text-aui-accent" /> : <BrainIcon size={14} />}
        {streaming ? <ShimmerVerb>{verb}</ShimmerVerb> : <span>{label}</span>}
        <svg
          aria-hidden="true"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
          className={`transition-transform duration-200 motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>
      <Collapse open={open} id={id}>
        <div
          className="mt-1 ml-[0.4375rem] border-aui-border border-l-2 py-0.5 pl-4 text-aui-muted"
          style={{
            ["--aui-prose-size" as string]: "0.8125rem",
            ["--aui-prose-leading" as string]: "1.35rem",
          }}
        >
          <Markdown text={text} streaming={streaming} />
        </div>
      </Collapse>
    </div>
  );
}
