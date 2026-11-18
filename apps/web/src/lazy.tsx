import { defaultCopy } from "@humagen/components";
import {
  Component,
  type ComponentType,
  createContext,
  createElement,
  type ReactNode,
  useContext,
  useEffect,
  useSyncExternalStore,
} from "react";
import { track } from "./transition.ts";
import { button, HubTabLabel, Page, Problem, SkeletonRows } from "./ui.tsx";

/*
 * Pages and heavy views the first screen does not show are loaded when they
 * are opened (pages.ts lists them). Each is read once: a view already read
 * draws at once, with no loading state, so a page whose code was read ahead
 * (on hover, on focus, or while Home sat idle) opens as it always did. A read
 * that fails, as after a deploy or without a network, shows a failure with a
 * way to try again, never an empty page.
 */

/** How long what can wait is held back, so it never competes with the first screen's own reads. */
export const IDLE_HOLD_MS = 1000;

/**
 * Run something that can wait: after a beat, so the screen being drawn has
 * its reads and its first frames to itself, and then once the browser is
 * idle (at once where it cannot say). Returns a way to call it off.
 */
export function whenIdle(run: () => void): () => void {
  let idle: number | undefined;
  const timer = window.setTimeout(() => {
    if (typeof window.requestIdleCallback !== "function") return run();
    idle = window.requestIdleCallback(run, { timeout: 3000 });
  }, IDLE_HOLD_MS);
  return () => {
    window.clearTimeout(timer);
    if (idle !== undefined) window.cancelIdleCallback(idle);
  };
}

/** The code of a page could not be read: a deploy replaced it, or the network is gone. */
export class ViewLoadError extends Error {
  constructor(cause: unknown) {
    super("A part of the console could not be loaded.", { cause });
    this.name = "ViewLoadError";
  }
}

/** A view whose code is read on first use. */
export type LazyView<P> = ((props: P) => ReactNode) & {
  /** Read the code ahead of use. Never rejects: a failure is met when the view is drawn. */
  preload: () => Promise<void>;
};

// Every lazy view's way to forget a failed read, so Retry reads it again.
const forget = new Set<() => void>();

/** Forget every failed read, so the next draw of a lazy view reads its code again. */
export function retryViews(): void {
  for (const reset of forget) reset();
}

// What a lazy view draws while its code is read: its frame's loading state.
const Fallback = createContext<ReactNode>(null);

/**
 * A view read by `load` on first use, drawn inside a `LazyFrame` (or made
 * with `lazyPage`, which brings one): the frame's loading state stands in
 * while the code is read, and a read that fails is the frame's to answer.
 *
 * The read is a dynamic import held here, not `React.lazy`: React holds
 * back what follows a loading state for a third of a second so loading
 * states do not flicker, which made every first visit that slow. A read made
 * for a view on screen counts as a read in flight (transition.ts), so a
 * change of screen keeps the old one on view until the next has its code
 * and its data, and its loading state shows only when that takes long.
 */
export function lazyView<P extends object>(load: () => Promise<ComponentType<P>>): LazyView<P> {
  let view: ComponentType<P> | undefined;
  let failure: ViewLoadError | undefined;
  let reading: Promise<void> | undefined;
  let counted = false;
  const drawn = new Set<() => void>();
  const tell = () => {
    for (const redraw of drawn) redraw();
  };
  /** Read the code once. `shown` says a view on screen waits on it. */
  const read = (shown: boolean): Promise<void> => {
    if (!reading) {
      counted = false;
      reading = load().then(
        (loaded) => {
          view = loaded;
          tell();
        },
        (cause) => {
          reading = undefined;
          failure = new ViewLoadError(cause);
          tell();
        },
      );
    }
    if (shown && !counted) {
      counted = true;
      void track(reading);
    }
    return reading;
  };
  forget.add(() => {
    if (!failure) return;
    failure = undefined;
    tell();
  });
  const subscribe = (redraw: () => void) => {
    drawn.add(redraw);
    return () => {
      drawn.delete(redraw);
    };
  };
  const state = () => view ?? failure ?? null;
  const View = ((props: P) => {
    const now = useSyncExternalStore(subscribe, state, state);
    const fallback = useContext(Fallback);
    // Asked for when first drawn, and again once on screen: a read ahead that
    // failed in between is not held against the view.
    if (now === null) void read(true);
    useEffect(() => {
      if (state() === null) void read(true);
    });
    if (now instanceof ViewLoadError) throw now;
    return now === null ? fallback : createElement(now, props);
  }) as LazyView<P>;
  View.preload = () =>
    read(false).then(() => {
      // Nothing on screen waited on it, so a failure is forgotten and opening the view reads again.
      if (drawn.size === 0) failure = undefined;
    });
  return View;
}

/**
 * How a failed page is read afresh when trying again in place fails too: the
 * console is loaded again at the same address. Held here so a test can stand
 * in for it.
 */
export const lazyHost = {
  reload: () => window.location.reload(),
};

type FrameState = { error: unknown; key?: string; retried: boolean };

/**
 * Around a lazy view: its loading state while its code is read, and a failure
 * with Retry when the read fails. Any other error is not this boundary's and
 * goes on up. `resetKey` clears a failure when it changes, as when the person
 * goes somewhere else.
 *
 * Retry reads the code again where it stands. A browser may hold a failed
 * read against its address for as long as the page lives, and after an update
 * the code that was asked for is gone, so when that read fails too the
 * console is loaded again at this address, which brings the current code.
 * Offline it is left alone: loading again would lose the console.
 */
export class LazyFrame extends Component<
  {
    /** What stands in while the code is read. */
    fallback: ReactNode;
    /** Inside a page that is already headed: the failure is its message and Retry alone. */
    bare?: boolean;
    /**
     * What to draw when the read fails, in place of the failure and Retry: for
     * something that opens over the page (null), or that has a plain form.
     * The failure is forgotten at once, so the next draw reads again.
     */
    instead?: ReactNode;
    resetKey?: string;
    children: ReactNode;
  },
  FrameState
> {
  override state: FrameState = { error: null, key: this.props.resetKey, retried: false };

  static getDerivedStateFromError(error: unknown) {
    return { error };
  }

  static getDerivedStateFromProps(props: { resetKey?: string }, state: FrameState) {
    return props.resetKey === state.key
      ? null
      : { error: null, key: props.resetKey, retried: false };
  }

  override componentDidCatch(error: unknown) {
    if (!(error instanceof ViewLoadError)) return;
    // Nothing offers Retry there, so the failure is forgotten at once.
    if (this.props.instead !== undefined) return retryViews();
    if (this.state.retried && navigator.onLine !== false) lazyHost.reload();
  }

  retry = () => {
    retryViews();
    this.setState({ error: null, retried: true });
  };

  override render() {
    const { error } = this.state;
    if (error === null)
      return (
        <Fallback.Provider value={this.props.fallback}>{this.props.children}</Fallback.Provider>
      );
    if (!(error instanceof ViewLoadError)) throw error;
    if (this.props.instead !== undefined) return this.props.instead;
    const words = defaultCopy.pageLoad;
    const failure = (
      <>
        <Problem message={words.failed} />
        <button type="button" className={`${button} self-start`} onClick={this.retry}>
          {defaultCopy.retry}
        </button>
      </>
    );
    return this.props.bare ? (
      <div className="flex flex-col gap-3">{failure}</div>
    ) : (
      <Page title={words.title}>{failure}</Page>
    );
  }
}

/**
 * A page while its code is read: its header, as the page itself will draw it,
 * over rows that stand in for what it lists, so nothing moves when it lands.
 * Under a hub's tabs the open tab names the page, as it does for the page.
 */
export function PageLoading({ title }: { title?: string }) {
  const hub = useContext(HubTabLabel);
  return (
    <Page
      title={
        hub ??
        title ?? (
          <span aria-hidden className="aui-skeleton aui-skeleton-delay inline-block h-6 w-48" />
        )
      }
    >
      <SkeletonRows count={4} />
    </Page>
  );
}

/**
 * A page read on first use, in its own frame: the page's loading state while
 * it is read and its failure with Retry. `title` is what the page is headed
 * by, read when drawn so it carries the workspace's own words. `frame`:
 * "page" (the default) for a whole page, "rows" for a part of a page that is
 * already headed, "none" for something that opens over the page.
 */
export function lazyPage<P extends object>(
  load: () => Promise<ComponentType<P>>,
  { title, frame = "page" }: { title?: () => string; frame?: "page" | "rows" | "none" } = {},
): LazyView<P> {
  const View = lazyView(load);
  const Framed = ((props: P) => (
    <LazyFrame
      bare={frame === "rows"}
      instead={frame === "none" ? null : undefined}
      fallback={
        frame === "page" ? (
          <PageLoading title={title?.()} />
        ) : frame === "rows" ? (
          <SkeletonRows count={3} />
        ) : null
      }
    >
      <View {...props} />
    </LazyFrame>
  )) as LazyView<P>;
  Framed.preload = View.preload;
  return Framed;
}
