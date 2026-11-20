import { type ReactNode, useEffect, useId, useState } from "react";
import { ChevronDownIcon } from "./icons";

/** Muted text with a highlight sweep. Reduced motion shows it static. */
export function Shimmer({ children, className }: { children: string; className?: string }) {
  return <span className={`aui-shimmer ${className ?? ""}`}>{children}</span>;
}

/**
 * The words that turn while an agent thinks. Each says only that it is
 * thinking: none claims a tool, a search or a draft that may not be happening.
 */
export const THINKING_VERBS = [
  "Thinking…",
  "Pondering…",
  "Mulling it over…",
  "Working it out…",
  "Connecting the dots…",
  "Weighing options…",
  "Sketching a plan…",
  "Untangling…",
  "Considering…",
  "Piecing it together…",
  "Noodling…",
  "Chewing on it…",
  "Lining things up…",
  "Figuring…",
  "Brewing…",
  "Turning it over…",
] as const;

/**
 * Turn through short progress verbs while work is in flight, so a wait reads
 * as progress. The first verb always shows first; the rest follow in an order
 * drawn afresh each time, and the turn never stops while the work goes on.
 */
export function useVerbSequence(
  active: boolean,
  verbs: readonly string[] = THINKING_VERBS,
  intervalMs = 2400,
): string {
  const [verb, setVerb] = useState(verbs[0] ?? "");
  useEffect(() => {
    setVerb(verbs[0] ?? "");
    if (!active || verbs.length <= 1) return;
    let last = 0;
    const timer = setInterval(() => {
      // Any verb but the one showing, so every tick is a change.
      const next = (last + 1 + Math.floor(Math.random() * (verbs.length - 1))) % verbs.length;
      last = next;
      setVerb(verbs[next] ?? "");
    }, intervalMs);
    return () => clearInterval(timer);
  }, [active, verbs, intervalMs]);
  return verb;
}

/**
 * A shimmering verb. A new verb rises in (keyed remount), so changing the
 * label reads as forward progress rather than a flicker.
 */
export function ShimmerVerb({ children, className }: { children: string; className?: string }) {
  return (
    <span
      key={children}
      className={`aui-verb-in inline-block whitespace-nowrap ${className ?? ""}`}
    >
      <Shimmer>{children}</Shimmer>
    </span>
  );
}

/**
 * Open state with the sample's precedence, highest first: the viewer's own
 * toggle, then `auto` (latched: once opened by state it stays open), then the
 * default as it was at mount. A card the viewer closed on purpose never pops back open.
 */
export function useDisclosure({
  defaultOpen = false,
  auto = false,
}: {
  defaultOpen?: boolean;
  auto?: boolean;
}) {
  const [viewer, setViewer] = useState<boolean | null>(null);
  const [initial] = useState(defaultOpen);
  const [latched, setLatched] = useState(auto);
  if (auto && !latched) setLatched(true);
  const open = viewer ?? (latched || initial);
  return { open, setOpen: setViewer, toggle: () => setViewer(!open) };
}

/** Animated height for disclosure content. Content stays mounted for a smooth close. */
export function Collapse({
  open,
  id,
  children,
}: {
  open: boolean;
  id?: string;
  children: ReactNode;
}) {
  return (
    <div className="aui-collapse" data-open={open}>
      <div id={id} inert={!open}>
        {children}
      </div>
    </div>
  );
}

/**
 * A disclosure row: button header with a rotating chevron, collapsible body.
 * The header owns aria-expanded/aria-controls; the chevron is decorative.
 */
export function Disclosure({
  header,
  open,
  onToggle,
  children,
  className,
  headerClassName,
}: {
  header: ReactNode;
  open: boolean;
  onToggle: () => void;
  children: ReactNode;
  className?: string;
  headerClassName?: string;
}) {
  const id = useId();
  return (
    <div className={className}>
      <button
        type="button"
        aria-expanded={open}
        aria-controls={id}
        onClick={onToggle}
        className={`group flex w-full min-w-0 items-center gap-2 text-left outline-none focus-visible:ring-2 focus-visible:ring-aui-accent ${headerClassName ?? ""}`}
      >
        {header}
        <ChevronDownIcon
          size={14}
          className={`ml-auto text-aui-muted transition-[color,rotate,transform] duration-(--aui-dur-row) group-hover:text-aui-fg motion-reduce:transition-none ${open ? "rotate-180" : ""}`}
        />
      </button>
      <Collapse open={open} id={id}>
        {children}
      </Collapse>
    </div>
  );
}
