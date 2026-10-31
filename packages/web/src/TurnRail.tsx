import type { Turn, TurnStatus } from "@humagen/core";
import {
  type CSSProperties,
  type KeyboardEvent,
  type PointerEvent,
  type RefObject,
  useEffect,
  useRef,
  useState,
} from "react";

export interface TurnRailEntry {
  /** Matches `data-turn-id` on the turn's element (AgentMessage stamps it). */
  id: string;
  agent: string;
  /** What the preview card says: the prompt that started the turn, or the answer's first line. */
  preview: string;
  status: TurnStatus;
  /** How much happened in the turn; sets the tick's length. Any positive scale. */
  weight: number;
  /** When the turn started, ISO. */
  at?: string;
}

/** A rail entry from a turn: weight grows with answer length and work done. */
export function railEntry(turn: Turn, at?: string): TurnRailEntry {
  const text = turn.text.replace(/\[\[[^\]]+\]\]/g, "").trim();
  return {
    id: turn.id,
    agent: turn.agent.name,
    preview: text.split("\n").find((l) => l.trim()) ?? "",
    status: turn.status,
    weight: text.length + turn.tasks.length * 120 + (turn.attachments?.length ?? 0) * 200 + 40,
    at,
  };
}

const REVEAL_MS = 1400;
/** The height of one turn's slot on the rail, and of the whole rail at most. */
const SLOT_PX = 12;
const MAX_RAIL = "min(60vh, 32rem)";
const statusWord: Record<TurnStatus, string> = {
  queued: "Queued",
  working: "Working",
  waiting: "Needs input",
  done: "Done",
  failed: "Failed",
  stopped: "Stopped",
};

/** The slot a pointer at `y` is over, on a rail of `n` slots from `top` to `top + height`. */
export function slotAt(y: number, top: number, height: number, n: number): number {
  if (n <= 0 || height <= 0) return 0;
  return Math.min(n - 1, Math.max(0, Math.floor(((y - top) / height) * n)));
}

/**
 * The conversation's wave, down its left edge: one tick per turn, longer when
 * the turn did more, so a long conversation reads like a sound wave. Ticks
 * near the one in view or under the pointer swell. It rests faint and
 * brightens while the reader scrolls or reaches for it.
 *
 * It is one vertical slider. Hover shows the turn's prompt; press, or on a
 * touch screen press and drag, to scrub through the conversation, which
 * follows; arrows, Home and End step through it from the keyboard.
 */
export function TurnRail({
  turns,
  scroller,
}: {
  turns: TurnRailEntry[];
  scroller: RefObject<HTMLElement | null>;
}) {
  const [active, setActive] = useState<string | null>(turns.at(-1)?.id ?? null);
  const [hovered, setHovered] = useState<number | null>(null);
  const [scrubbing, setScrubbing] = useState(false);
  const [scrolling, setScrolling] = useState(false);
  const ids = useRef<string[]>([]);
  ids.current = turns.map((t) => t.id);
  const rail = useRef<HTMLDivElement>(null);
  // After a jump, the target stays current while the scroll settles.
  const lockUntil = useRef(0);
  const lastJump = useRef(-1);

  useEffect(() => {
    const root = scroller.current;
    if (!root) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    const onScroll = () => {
      setScrolling(true);
      if (timer) clearTimeout(timer);
      timer = setTimeout(() => setScrolling(false), REVEAL_MS);
    };
    root.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      root.removeEventListener("scroll", onScroll);
      if (timer) clearTimeout(timer);
    };
  }, [scroller]);

  useEffect(() => {
    const root = scroller.current;
    if (!root || typeof IntersectionObserver === "undefined") return;
    const visible = new Set<string>();
    const observer = new IntersectionObserver(
      (entries) => {
        for (const e of entries) {
          const id = e.target.getAttribute("data-turn-id");
          if (!id) continue;
          if (e.isIntersecting) visible.add(id);
          else visible.delete(id);
        }
        if (Date.now() < lockUntil.current) return;
        // Scrolled to the end: the last turn is current even if it starts lower down.
        const atEnd = root.scrollHeight - root.scrollTop - root.clientHeight < 8;
        const top = atEnd ? ids.current.at(-1) : ids.current.find((id) => visible.has(id));
        if (top) setActive(top);
      },
      { root, rootMargin: "0px 0px -55% 0px", threshold: 0 },
    );
    for (const t of turns) {
      const el = root.querySelector(`[data-turn-id="${CSS.escape(t.id)}"]`);
      if (el) observer.observe(el);
    }
    return () => observer.disconnect();
  }, [turns, scroller]);

  if (turns.length === 0) return null;

  const reduce = () => window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
  const jump = (i: number, smooth: boolean) => {
    const t = turns[i];
    if (!t) return;
    const el = scroller.current?.querySelector(`[data-turn-id="${CSS.escape(t.id)}"]`);
    lockUntil.current = Date.now() + 900;
    el?.scrollIntoView({ block: "start", behavior: smooth && !reduce() ? "smooth" : "auto" });
    setActive(t.id);
    lastJump.current = i;
  };

  const slotOf = (e: PointerEvent<HTMLDivElement>) => {
    const box = rail.current?.getBoundingClientRect();
    return box ? slotAt(e.clientY, box.top, box.height, turns.length) : 0;
  };

  const onPointerDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.button !== 0) return;
    e.currentTarget.setPointerCapture?.(e.pointerId);
    const i = slotOf(e);
    setScrubbing(true);
    setHovered(i);
    jump(i, e.pointerType === "mouse");
  };
  const onPointerMove = (e: PointerEvent<HTMLDivElement>) => {
    const i = slotOf(e);
    if (scrubbing) {
      if (i !== lastJump.current) {
        jump(i, false);
        // A light tick under the thumb for each turn passed, where the device has one.
        if (e.pointerType === "touch") navigator.vibrate?.(4);
      }
      setHovered(i);
    } else if (e.pointerType === "mouse") setHovered(i);
  };
  const onPointerEnd = (e: PointerEvent<HTMLDivElement>) => {
    setScrubbing(false);
    if (e.pointerType !== "mouse" || e.type === "pointerleave") setHovered(null);
  };

  const activeIndex = Math.max(
    0,
    turns.findIndex((t) => t.id === active),
  );
  const onKeyDown = (e: KeyboardEvent<HTMLDivElement>) => {
    const to =
      e.key === "ArrowDown" || e.key === "ArrowRight"
        ? Math.min(turns.length - 1, activeIndex + 1)
        : e.key === "ArrowUp" || e.key === "ArrowLeft"
          ? Math.max(0, activeIndex - 1)
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? turns.length - 1
              : null;
    if (to === null) return;
    e.preventDefault();
    jump(to, true);
    setHovered(to);
  };

  const logs = turns.map((t) => Math.log(t.weight + 1));
  const max = Math.max(...logs);
  const min = Math.min(...logs);
  const focus = hovered ?? activeIndex;
  const current = turns[activeIndex];
  const shown = hovered !== null ? turns[hovered] : undefined;
  const lit = scrolling || scrubbing || hovered !== null;

  return (
    <nav
      aria-label="Conversation wave"
      data-layer="wave"
      className={`aui-wave absolute top-1/2 left-0 z-20 -translate-y-1/2 transition-opacity duration-(--aui-dur-page) focus-within:opacity-100 hover:opacity-100 ${lit ? "opacity-100" : "opacity-45"}`}
    >
      <div
        ref={rail}
        role="slider"
        tabIndex={0}
        aria-label="Turns in this conversation"
        aria-orientation="vertical"
        aria-valuemin={1}
        aria-valuemax={turns.length}
        aria-valuenow={activeIndex + 1}
        aria-valuetext={
          current
            ? `${activeIndex + 1} of ${turns.length}, ${current.agent}: ${current.preview || "no answer yet"}`
            : undefined
        }
        onPointerDown={onPointerDown}
        onPointerMove={onPointerMove}
        onPointerUp={onPointerEnd}
        onPointerCancel={onPointerEnd}
        onPointerLeave={onPointerEnd}
        onKeyDown={onKeyDown}
        onBlur={() => setHovered(null)}
        className="aui-wave-rail flex cursor-pointer touch-none select-none flex-col rounded-full outline-none focus-visible:ring-2 focus-visible:ring-aui-accent"
        style={{ height: `min(${turns.length * SLOT_PX}px, ${MAX_RAIL})` }}
      >
        {turns.map((t, i) => {
          const norm = max > min ? ((logs[i] ?? min) - min) / (max - min) : 0.5;
          // Ticks near the one in view, or under the pointer, swell like a wave under the playhead.
          const near = Math.max(0, 1 - Math.abs(i - focus) / 3);
          const isActive = i === activeIndex;
          const tone =
            t.status === "failed"
              ? "bg-aui-bad"
              : isActive
                ? "aui-wave-now"
                : t.status === "waiting"
                  ? "bg-aui-accent/60"
                  : near > 0 && lit
                    ? "bg-aui-fg/45"
                    : "bg-aui-fg/25";
          return (
            <span key={t.id} aria-hidden className="flex min-h-0 flex-1 items-center">
              <span
                className={`aui-rail-tick aui-wave-tick block rounded-full ${tone}`}
                style={
                  {
                    "--aui-wave-w": `${6 + norm * 10 + near * 6}px`,
                    height: isActive ? 4 : 3,
                  } as CSSProperties
                }
              />
            </span>
          );
        })}
      </div>
      {shown && hovered !== null && (
        <div
          role="tooltip"
          className="aui-glass pointer-events-none absolute left-full z-30 ml-2 flex w-64 max-w-[70vw] -translate-y-1/2 flex-col gap-0.5 rounded-aui px-2.5 py-2 text-aui-xs shadow-md"
          style={{ top: `${((hovered + 0.5) / turns.length) * 100}%` }}
        >
          <span className="flex items-center gap-1.5 text-aui-2xs text-aui-muted">
            <span className="font-medium text-aui-fg">{shown.agent}</span>
            <span>· {statusWord[shown.status]}</span>
            <span className="ml-auto tabular-nums">
              {shown.at
                ? new Date(shown.at).toLocaleTimeString(undefined, {
                    hour: "numeric",
                    minute: "2-digit",
                  })
                : `${hovered + 1}/${turns.length}`}
            </span>
          </span>
          <span className="line-clamp-2 text-aui-fg">{shown.preview || "No answer yet"}</span>
        </div>
      )}
    </nav>
  );
}
