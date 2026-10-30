import { type ArtifactKind, type ArtifactPhase, formatBytes } from "@humagen/core";
import type { FileProps } from "@humagen/primitives";
import { type CSSProperties, useContext, useId } from "react";
import { DownloadIcon, EyeIcon } from "./icons";
import { ArtifactOpenContext } from "./meta";

type Mark =
  | { k: "line"; x1: number; y1: number; x2: number; y2: number; flat?: boolean }
  | { k: "rect"; x: number; y: number; w: number; h: number; rx?: number }
  | { k: "circle"; cx: number; cy: number; r: number }
  | { k: "path"; d: string };

const PAGE = "M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8Z";
const FOLD = "M14 2v4a2 2 0 0 0 2 2h4";

const lines = (ys: number[], end = 16.5): Mark[] =>
  ys.map((y, i) => ({ k: "line", x1: 7.5, y1: y, x2: i === ys.length - 1 ? 13 : end, y2: y }));

/** What the page fills with, in reading order: cells for a sheet, lines for a doc. */
const MARKS: Record<ArtifactKind, Mark[]> = {
  spreadsheet: [12, 15, 18].flatMap((y): Mark[] => [
    { k: "line", x1: 7.6, y1: y, x2: 11.2, y2: y, flat: true },
    { k: "line", x1: 12.8, y1: y, x2: 16.4, y2: y, flat: true },
  ]),
  presentation: [
    { k: "rect", x: 7.4, y: 11.2, w: 9.2, h: 6.4, rx: 1 },
    { k: "line", x1: 9.4, y1: 14, x2: 13.6, y2: 14 },
    { k: "line", x1: 9.4, y1: 16, x2: 12.2, y2: 16 },
  ],
  document: lines([12, 15, 18]),
  pdf: [...lines([12, 15]), { k: "rect", x: 7.5, y: 17, w: 5.6, h: 2.6, rx: 0.7 }],
  image: [
    { k: "rect", x: 7.4, y: 11.2, w: 9.2, h: 7.6, rx: 1.2 },
    { k: "circle", cx: 10.2, cy: 13.8, r: 1 },
    { k: "path", d: "M7.9 18.4l2.9-3 2 1.9 1.7-1.5 1.9 2.6" },
  ],
  html: [
    { k: "path", d: "M10 12.5 8 15l2 2.5" },
    { k: "path", d: "M14 12.5l2 2.5-2 2.5" },
  ],
  code: [
    { k: "path", d: "M10 12.5 8 15l2 2.5" },
    { k: "line", x1: 11, y1: 18, x2: 16, y2: 18 },
  ],
  data: [
    {
      k: "path",
      d: "M10.9 11.9c-1.05 0-1.05 1.15-1.05 1.95 0 .8-.35 1.25-1.05 1.25.7 0 1.05.45 1.05 1.25 0 .8 0 1.95 1.05 1.95",
    },
    { k: "circle", cx: 12, cy: 15.1, r: 0.45 },
    {
      k: "path",
      d: "M13.1 11.9c1.05 0 1.05 1.15 1.05 1.95 0 .8.35 1.25 1.05 1.25-.7 0-1.05.45-1.05 1.25 0 .8 0 1.95-1.05 1.95",
    },
  ],
  archive: [
    { k: "line", x1: 12, y1: 11, x2: 12, y2: 12 },
    { k: "line", x1: 12, y1: 14, x2: 12, y2: 15 },
    { k: "rect", x: 10.8, y: 16.5, w: 2.4, h: 2.4, rx: 0.6 },
  ],
};

function MarkEl({ m, i }: { m: Mark; i: number }) {
  const style = { "--i": i } as CSSProperties;
  const cls = `aui-art-mark ${m.k === "circle" ? "aui-art-pop" : ""}`;
  switch (m.k) {
    case "line":
      return (
        <line
          className={cls}
          style={style}
          x1={m.x1}
          y1={m.y1}
          x2={m.x2}
          y2={m.y2}
          strokeLinecap={m.flat ? "butt" : undefined}
        />
      );
    case "rect":
      return (
        <rect className={cls} style={style} x={m.x} y={m.y} width={m.w} height={m.h} rx={m.rx} />
      );
    case "circle":
      return <circle className={cls} style={style} cx={m.cx} cy={m.cy} r={m.r} />;
    case "path":
      return <path className={cls} style={style} d={m.d} />;
  }
}

function Glyph({ kind }: { kind: ArtifactKind }) {
  return (
    <>
      <path className="aui-art-page" d={PAGE} pathLength={100} />
      <path className="aui-art-page aui-art-fold" d={FOLD} pathLength={100} />
      {MARKS[kind].map((m, i) => (
        // biome-ignore lint/suspicious/noArrayIndexKey: static marks, order is identity
        <MarkEl key={i} m={m} i={i} />
      ))}
    </>
  );
}

const PIP: Partial<Record<ArtifactPhase, "pause" | "alert" | "slash">> = {
  waiting: "pause",
  failed: "alert",
  denied: "slash",
};

/**
 * A file drawn as it is written: the page draws itself, breathes while queued,
 * fills mark by mark with a sweep while generating, and holds still while
 * waiting on a person. Reduced motion shows a partial fill instead.
 */
export function ArtifactIcon({
  kind,
  phase,
  size = 20,
  className,
}: {
  kind: ArtifactKind;
  phase: ArtifactPhase;
  size?: number;
  className?: string;
}) {
  const uid = useId().replace(/[^a-zA-Z0-9]/g, "");
  const pip = PIP[phase];
  return (
    <svg
      aria-hidden="true"
      viewBox="0 0 24 24"
      width={size}
      height={size}
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className={`aui-art shrink-0 ${className ?? ""}`}
      data-phase={phase}
    >
      <defs>
        <linearGradient id={`g${uid}`} x1="0" y1="0" x2="1" y2="0">
          <stop offset="0" stopColor="#fff" stopOpacity="0" />
          <stop offset="0.5" stopColor="#fff" stopOpacity="1" />
          <stop offset="1" stopColor="#fff" stopOpacity="0" />
        </linearGradient>
        <mask id={`m${uid}`}>
          <rect
            className="aui-art-band"
            x={-14}
            y={0}
            width={14}
            height={24}
            fill={`url(#g${uid})`}
          />
        </mask>
      </defs>
      <g className="aui-art-base">
        <Glyph kind={kind} />
      </g>
      <g className="aui-art-sweep" mask={`url(#m${uid})`}>
        <Glyph kind={kind} />
      </g>
      {pip && (
        <g>
          <circle cx={18.4} cy={18.4} r={4.6} fill="var(--aui-bg)" stroke="none" />
          <circle cx={18.4} cy={18.4} r={3.9} />
          {pip === "pause" && <path d="M17.3 16.9v3M19.5 16.9v3" />}
          {pip === "alert" && <path d="M18.4 16.6v2M18.4 20.1v.1" />}
          {pip === "slash" && <path d="M16.8 20 20 16.8" />}
        </g>
      )}
    </svg>
  );
}

export const phaseLabel: Record<ArtifactPhase, string> = {
  queued: "Queued",
  generating: "Generating",
  waiting: "Waiting for approval",
  finalizing: "Finishing up",
  ready: "Ready",
  failed: "Failed",
  denied: "Not created",
};

const kindLabel: Record<ArtifactKind, string> = {
  document: "Document",
  spreadsheet: "Spreadsheet",
  presentation: "Slides",
  image: "Image",
  pdf: "PDF",
  html: "Web page",
  code: "Code",
  data: "Data",
  archive: "Archive",
};

const iconTone: Record<ArtifactPhase, string> = {
  queued: "text-aui-muted",
  generating: "text-aui-accent",
  waiting: "text-aui-accent/70",
  finalizing: "text-aui-accent",
  ready: "text-aui-fg",
  failed: "text-aui-bad",
  denied: "text-aui-muted",
};

/** The File primitive: one compact row per file, with Open (preview) and Download once ready. */
export function FileTile(props: FileProps) {
  const { title, kind, phase, sizeBytes, version, url, downloadUrl, description, error } = props;
  const open = useContext(ArtifactOpenContext);
  const ready = phase === "ready";
  const busy = phase === "queued" || phase === "generating" || phase === "finalizing";
  const meta = [
    ready ? kindLabel[kind] : phaseLabel[phase],
    sizeBytes !== undefined ? formatBytes(sizeBytes) : null,
    version && version > 1 ? `v${version}` : null,
  ].filter(Boolean);
  const canOpen = ready && (open || url);
  const action =
    "inline-flex h-7 pointer-coarse:h-11 items-center gap-1 rounded-aui-sm px-2 text-aui-xs text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

  return (
    <div
      data-aui="file"
      data-phase={phase}
      aria-busy={busy || undefined}
      className="group flex min-w-0 items-center gap-2.5 rounded-aui border border-aui-border px-2.5 py-1.5"
    >
      <span
        className={`flex size-8 shrink-0 items-center justify-center rounded-aui-sm bg-aui-subtle/60 ${iconTone[phase]}`}
      >
        <ArtifactIcon kind={kind} phase={phase} />
      </span>
      <span className="flex min-w-0 flex-1 flex-col">
        {canOpen && open ? (
          <button
            type="button"
            onClick={() => open(props)}
            className="min-h-6 pointer-coarse:min-h-11 truncate text-left font-medium text-aui-fg text-aui-sm hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            {title}
          </button>
        ) : (
          <span
            className={`truncate font-medium text-aui-sm ${phase === "denied" ? "text-aui-muted line-through" : "text-aui-fg"}`}
          >
            {title}
          </span>
        )}
        <span className="flex min-w-0 items-center gap-1.5 text-aui-2xs text-aui-muted">
          <span className={`shrink-0 tabular-nums ${busy ? "aui-shimmer" : ""}`}>
            {meta.join(" ")}
          </span>
          {(error || description) && (
            <span
              className={`truncate ${error ? "text-aui-bad" : ""}`}
              title={error ?? description}
            >
              {error ?? description}
            </span>
          )}
        </span>
      </span>
      {canOpen &&
        (open ? (
          <button
            type="button"
            className={action}
            onClick={() => open(props)}
            aria-label={`Open ${title}`}
          >
            <EyeIcon size={13} />
            <span className="hidden sm:inline">Open</span>
          </button>
        ) : (
          <a
            className={action}
            href={url}
            target="_blank"
            rel="noreferrer"
            aria-label={`Open ${title}`}
          >
            <EyeIcon size={13} />
            <span className="hidden sm:inline">Open</span>
          </a>
        ))}
      {ready && downloadUrl && (
        <a className={action} href={downloadUrl} download={title} aria-label={`Download ${title}`}>
          <DownloadIcon size={13} />
          <span className="hidden sm:inline">Download</span>
        </a>
      )}
    </div>
  );
}
