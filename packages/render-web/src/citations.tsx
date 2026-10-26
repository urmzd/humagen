import type { Citation } from "@humagen/core";
import type { CitationLabels } from "@humagen/primitives";
import {
  createContext,
  type ReactNode,
  useContext,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";
import { ExternalLinkIcon } from "./icons";
import { useTurnMeta } from "./meta";

/**
 * Shared components emit citations as numbered markdown links (`[[1]](url)`)
 * and list the sources separately. The web joins them back up: Sources
 * registers its items here, and each inline marker looks its source up by URL
 * to show a hover card. No contract change, no app hooks.
 */
interface RegisteredCitation {
  citation: Citation;
  n: number;
  labels?: CitationLabels;
}

export class CitationRegistry {
  private scopes = new Map<
    string,
    { items: Citation[]; labels?: CitationLabels; byUrl: Map<string, RegisteredCitation[]> }
  >();
  private listeners = new Set<() => void>();
  private ambiguous = new Set<string>();
  private version = 0;
  register(items: Citation[], labels?: CitationLabels, scope = "") {
    const previous = this.scopes.get(scope);
    if (previous?.items === items && previous.labels === labels) return;
    const byUrl = new Map<string, RegisteredCitation[]>();
    items.forEach((citation, index) => {
      if (citation.url)
        byUrl.set(citation.url, [
          ...(byUrl.get(citation.url) ?? []),
          { citation, n: index + 1, labels },
        ]);
    });
    for (const url of byUrl.keys())
      if (
        [...this.scopes.entries()].some(([other, group]) => other !== scope && group.byUrl.has(url))
      )
        this.ambiguous.add(url);
    this.scopes.set(scope, { items, labels, byUrl });
    this.changed();
  }
  unregister(scope: string, items: Citation[]) {
    if (this.scopes.get(scope)?.items !== items) return;
    this.scopes.delete(scope);
    this.changed();
  }
  get(url: string, scope?: string, index?: number) {
    const select = (records: RegisteredCitation[]) =>
      index === undefined
        ? records.length === 1
          ? records[0]
          : undefined
        : records.find((record) => record.n === index);
    if (scope !== undefined) return select(this.scopes.get(scope)?.byUrl.get(url) ?? []);
    if (this.ambiguous.has(url)) return undefined;
    const matches = [...this.scopes.values()].flatMap((group) => group.byUrl.get(url) ?? []);
    return select(matches);
  }
  private changed() {
    this.version++;
    for (const listener of this.listeners) listener();
  }
  subscribe = (listener: () => void) => {
    this.listeners.add(listener);
    return () => {
      this.listeners.delete(listener);
    };
  };
  snapshot = () => this.version;
}

export const CitationContext = createContext<CitationRegistry | null>(null);

export function useRegisterCitations(items: Citation[], labels: CitationLabels, scope?: string) {
  const registry = useContext(CitationContext);
  const turn = useTurnMeta();
  const generated = useId();
  const key = scope ?? turn?.id ?? generated;
  useLayoutEffect(() => {
    registry?.register(items, labels, key);
    return () => registry?.unregister(key, items);
  }, [registry, items, labels, key]);
}

function useCitation(url: string | undefined, label: string) {
  const registry = useContext(CitationContext);
  const turn = useTurnMeta();
  useSyncExternalStore(registry?.subscribe ?? noopSubscribe, registry?.snapshot ?? zero, zero);
  return url ? registry?.get(url, turn?.id, Number(label)) : undefined;
}

/** Only recorded facts are rendered. Historic citations never borrow current configuration. */
export function CitationProvenance({
  citation,
  labels,
}: {
  citation: Citation;
  labels?: CitationLabels;
}) {
  if (!labels) return null;
  const known = citation.mode === "indexed" || citation.mode === "live";
  const copy =
    known &&
    citation.mode === "indexed" &&
    citation.copyAt &&
    /^\d{4}-\d{2}-\d{2}T/.test(citation.copyAt) &&
    Number.isFinite(Date.parse(citation.copyAt))
      ? citation.copyAt
      : undefined;
  return (
    <span
      className="flex max-w-full flex-wrap gap-x-2 gap-y-0.5 break-words text-aui-2xs text-aui-muted"
      data-aui="citation-provenance"
    >
      <span>
        {citation.mode === "indexed"
          ? labels.indexed
          : citation.mode === "live"
            ? labels.live
            : labels.unknown}
      </span>
      {citation.mode === "live" ? (
        <span>{labels.queriedLive}</span>
      ) : copy ? (
        <span>
          {citation.refreshFailed ? labels.lastGoodCopy : labels.copyFrom}:{" "}
          <time dateTime={copy}>
            {new Date(copy).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" })}
          </time>
        </span>
      ) : (
        <span>
          {known && citation.refreshFailed ? `${labels.lastGoodCopy} · ` : ""}
          {labels.freshnessUnknown}
        </span>
      )}
    </span>
  );
}

const noopSubscribe = () => () => {};
const zero = () => 0;

export function hostOf(url?: string, fallback?: string): string {
  if (!url) return fallback ?? "";
  try {
    return new URL(url).hostname.replace(/^www\./, "");
  } catch {
    return fallback ?? url;
  }
}

/** Letter mark for a source; no favicon fetches, so nothing leaks to third parties. */
export function SourceMark({ label, className }: { label: string; className?: string }) {
  return (
    <span
      aria-hidden
      className={`inline-flex shrink-0 items-center justify-center rounded-[0.3rem] bg-aui-subtle font-semibold text-aui-muted uppercase ${className ?? "size-4 text-[0.6rem]"}`}
    >
      {label.charAt(0) || "·"}
    </span>
  );
}

/**
 * The source's own favicon, fetched from the cited site itself (no third-party
 * favicon service), falling back to the letter mark.
 */
export function Favicon({
  url,
  label,
  className = "size-3.5",
}: {
  url?: string;
  label: string;
  className?: string;
}) {
  const [failed, setFailed] = useState(false);
  let src: string | null = null;
  try {
    if (url) src = `${new URL(url).origin}/favicon.ico`;
  } catch {
    src = null;
  }
  if (!src || failed) return <SourceMark label={label} className={`${className} text-[0.55rem]`} />;
  return (
    <img
      src={src}
      alt=""
      loading="lazy"
      referrerPolicy="no-referrer"
      onError={() => setFailed(true)}
      className={`${className} shrink-0 rounded-[0.2rem] object-contain`}
    />
  );
}

/**
 * Inline citation chip: a superscript-height pill with the source number.
 * Hover or focus shows a card with the title, origin, and cited snippet.
 */
export function CitationChip({ href, label }: { href?: string; label: string }) {
  const found = useCitation(href, label);
  const [open, setOpen] = useState(false);
  const [alignEnd, setAlignEnd] = useState(false);
  const wrapper = useRef<HTMLSpanElement>(null);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const cardId = useId();
  const n = found?.n ?? label;
  const citation = found?.citation;
  const origin = citation?.source ?? hostOf(href);

  const show = (delay: number) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => {
      const rect = wrapper.current?.getBoundingClientRect();
      setAlignEnd(rect ? rect.left + 320 > window.innerWidth - 16 : false);
      setOpen(true);
    }, delay);
  };
  const hide = (delay: number) => {
    if (timer.current) clearTimeout(timer.current);
    timer.current = setTimeout(() => setOpen(false), delay);
  };
  useEffect(
    () => () => {
      if (timer.current) clearTimeout(timer.current);
    },
    [],
  );

  const chip =
    "relative -top-[0.3em] mx-0.5 inline-flex h-[1.35em] min-w-[1.35em] items-center justify-center gap-1 rounded-full border border-aui-border bg-aui-subtle px-1.5 align-baseline font-medium text-[0.68em] text-aui-muted leading-none tabular-nums no-underline transition-colors hover:border-aui-accent hover:bg-aui-accent-soft hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-offset-1 focus-visible:outline-aui-accent";

  const body: ReactNode = <span>{n}</span>;
  const aria = citation ? `Source ${n}: ${citation.title}` : `Source ${n}`;

  return (
    // biome-ignore lint/a11y/noStaticElementInteractions: hover intent wrapper; the chip inside is the focusable control
    <span
      ref={wrapper}
      className="relative inline-block"
      onMouseEnter={() => show(120)}
      onMouseLeave={() => hide(120)}
      onFocus={() => show(0)}
      onBlur={() => hide(0)}
      onKeyDown={(e) => {
        if (e.key === "Escape") setOpen(false);
      }}
    >
      {href ? (
        <a
          data-aui-cite=""
          href={href}
          target="_blank"
          rel="noreferrer"
          className={chip}
          aria-label={aria}
          aria-describedby={open && citation ? cardId : undefined}
        >
          {body}
        </a>
      ) : (
        <span data-aui-cite="" className={`${chip} cursor-default`}>
          {body}
        </span>
      )}
      {open && citation && (
        <span
          id={cardId}
          role="tooltip"
          className={`aui-pop-in aui-glass absolute top-full z-50 mt-1.5 flex w-80 max-w-[80vw] flex-col gap-1.5 rounded-aui p-3 text-left font-normal text-aui-sm text-aui-fg not-italic shadow-aui-pop ${alignEnd ? "right-0" : "left-0"}`}
        >
          <span className="flex items-center gap-2 text-aui-muted text-aui-xs">
            <Favicon url={href} label={origin} />
            <span className="truncate">{origin}</span>
            <ExternalLinkIcon size={12} className="ml-auto" />
          </span>
          <span className="font-medium leading-snug">{citation.title}</span>
          <CitationProvenance citation={citation} labels={found?.labels} />
          {citation.snippet && (
            <span className="line-clamp-3 border-aui-border border-l-2 pl-2 text-aui-muted text-aui-xs">
              {citation.snippet}
            </span>
          )}
        </span>
      )}
    </span>
  );
}
