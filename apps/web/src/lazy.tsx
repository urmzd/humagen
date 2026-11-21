import { defaultCopy } from "@humagen/components";
import { RefreshIcon } from "@humagen/render-web";
import { toast } from "@humagen/render-web/ui/sonner";
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
import { HubTabLabel, Page, Problem, SkeletonRows } from "./ui.tsx";

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

/**
 * The address a failed `import()` names, when the browser's error says it
 * (Chromium and Firefox do) and it is one of this build's own files.
 */
export function moduleAddress(error: unknown): string | undefined {
  const said = error instanceof Error ? error.message : "";
  const found = said.match(/https?:\/\/[^\s'"]+?\.js(?=[\s'")]|$)/)?.[0];
  if (!found) return undefined;
  try {
    const url = new URL(found);
    const here = typeof window === "undefined" ? url.origin : window.location.origin;
    return url.origin === here && url.pathname.startsWith("/assets/") ? url.href : undefined;
  } catch {
    return undefined;
  }
}

/** A failed read's address, to be asked for again: the same file under a query not asked before. */
export function again(address: string, attempt: number): string {
  const url = new URL(address);
  url.searchParams.set("again", String(attempt));
  return url.href;
}

/**
 * Whether the person or their connection asks for less to be sent: data
 * saving is on, or the connection is a slow one. Nothing is read ahead then;
 * a page is read when it is opened.
 */
export function sparing(): boolean {
  const connection = (
    navigator as Navigator & { connection?: { saveData?: boolean; effectiveType?: string } }
  ).connection;
  return !!connection?.saveData || /(^|-)2g$/.test(connection?.effectiveType ?? "");
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
export function lazyView<M, P extends object>(
  load: () => Promise<M>,
  pick: (module: M) => ComponentType<P>,
): LazyView<P> {
  let view: ComponentType<P> | undefined;
  let failure: ViewLoadError | undefined;
  let reading: Promise<void> | undefined;
  let counted = false;
  // Where the code was asked for when a read failed, and how many times it has been asked for again.
  let address: string | undefined;
  let tries = 0;
  const drawn = new Set<() => void>();
  const tell = () => {
    for (const redraw of drawn) redraw();
  };
  /** Read the code once. `shown` says a view on screen waits on it. */
  const read = (shown: boolean): Promise<void> => {
    if (!reading) {
      counted = false;
      // A browser holds a failed read against its address for as long as the
      // page lives, so a read made again asks at the same address with a
      // query no earlier read used. The file answers either way.
      const asked = address ? (lazyHost.again(again(address, ++tries)) as Promise<M>) : load();
      reading = asked.then(pick).then(
        (loaded) => {
          view = loaded;
          tell();
        },
        (cause) => {
          reading = undefined;
          address = moduleAddress(cause) ?? address;
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
 * What a failed read needs of the browser: loading the console again at the
 * same address when trying again in place fails too, reading a module at a
 * worked-out address, and telling the person. Held here so a test can stand
 * in for each.
 */
export const lazyHost = {
  reload: () => window.location.reload(),
  /** Read a module at an address worked out here, where the bundler's own `import()` cannot be used. */
  again: (address: string): Promise<unknown> => import(/* @vite-ignore */ address),
  /** Tell the person something that opens over the page could not be read. */
  tell: (message: string) => {
    toast.error(message);
  },
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
    /** Called when the read fails, with `instead`: to tell the person, or to put the control away. */
    onFailure?: () => void;
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
    if (this.props.instead !== undefined) {
      retryViews();
      this.props.onFailure?.();
      return;
    }
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
    return this.props.bare ? (
      <LoadFailure onRetry={this.retry} />
    ) : (
      <PageFailure onRetry={this.retry} />
    );
  }
}

/**
 * A read that failed, said once: the message, with the way to try again at
 * its end as an icon, named Retry for a screen reader and on hover. One line
 * and one control, as an empty state is one title and one action.
 */
function LoadFailure({ onRetry, message }: { onRetry: () => void; message?: string }) {
  return (
    <Problem
      message={message ?? defaultCopy.pageLoad.failed}
      beside
      action={
        <button
          type="button"
          aria-label={defaultCopy.retry}
          title={defaultCopy.retry}
          onClick={onRetry}
          className="inline-flex size-8 shrink-0 items-center justify-center rounded-aui text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:size-11"
        >
          <RefreshIcon size={16} aria-hidden />
        </button>
      }
    />
  );
}

/**
 * A page whose code could not be read. Under a hub's tabs the open tab heads
 * it, as it heads the page itself, so nothing is titled twice and the failure
 * says what happened. On its own the title says that, and the line under it
 * only what to do.
 */
function PageFailure({ onRetry }: { onRetry: () => void }) {
  const hub = useContext(HubTabLabel);
  const words = defaultCopy.pageLoad;
  return (
    <Page title={hub ?? words.title}>
      <LoadFailure onRetry={onRetry} message={hub ? words.failed : words.hint} />
    </Page>
  );
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

/** What a control that opens over the page takes besides its own props. */
export type Opened = {
  /**
   * Called when the control's code cannot be read, after the person is told:
   * put the control away, so the next press opens it, and reads it, again.
   */
  onUnavailable?: () => void;
};

/**
 * A page read on first use, in its own frame: the page's loading state while
 * it is read and its failure with Retry. `title` is what the page is headed
 * by, read when drawn so it carries the workspace's own words. `frame`:
 * "page" (the default) for a whole page, "rows" for a part of a page that is
 * already headed, "none" for something that opens over the page, which draws
 * nothing when it cannot be read, says so in a notice, and reads again the
 * next time it is opened (`onUnavailable`).
 */
export function lazyPage<M, P extends object>(
  load: () => Promise<M>,
  pick: (module: M) => ComponentType<P>,
  { title, frame = "page" }: { title?: () => string; frame?: "page" | "rows" | "none" } = {},
): LazyView<P & Opened> {
  const View = lazyView(load, pick);
  const Framed = (({ onUnavailable, ...props }: P & Opened) => (
    <LazyFrame
      bare={frame === "rows"}
      instead={frame === "none" ? null : undefined}
      onFailure={() => {
        lazyHost.tell(defaultCopy.pageLoad.partFailed);
        onUnavailable?.();
      }}
      fallback={
        frame === "page" ? (
          <PageLoading title={title?.()} />
        ) : frame === "rows" ? (
          <SkeletonRows count={3} />
        ) : null
      }
    >
      <View {...(props as unknown as P)} />
    </LazyFrame>
  )) as LazyView<P & Opened>;
  Framed.preload = View.preload;
  return Framed;
}
