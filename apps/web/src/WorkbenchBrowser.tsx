import { defaultCopy } from "@humagen/components";
import type { HumanActivityInput } from "@humagen/core";
import { ChevronRightIcon, ExternalLinkIcon } from "@humagen/render-web";
import { type FormEvent, useEffect, useId, useMemo, useRef, useState } from "react";
import { ApiError } from "./api.ts";
import {
  type BrowserAct,
  type BrowserAction,
  type BrowserElement,
  type BrowserView,
  sessionBrowser,
} from "./browserApi.ts";
import {
  type BrowserShot,
  type BrowserShots,
  browserShots,
  useBrowserShots,
} from "./browserShots.ts";
import { type BrowserStep, browserNow, browserSteps, callsOfResponses } from "./browserSteps.ts";
import { ago } from "./format.ts";
import { button, field, primary, SkeletonRows } from "./ui.tsx";
import type { WorkbenchTabProps } from "./workbench.ts";

const words = defaultCopy.workbenchBrowser;

// Everything in the tab is read at 14px or more, and what is secondary is
// the muted ink, which holds 7:1 on the pane in light and dark.
const quiet = "text-aui-base text-aui-muted";
const heading = "font-medium text-aui-base text-aui-fg";
const FIELDS = new Set(["textbox", "searchbox", "combobox", "spinbutton"]);

const named = (el: BrowserElement) => words.element(el.role, el.name ?? "");
const pageName = (page: { title?: string; url?: string }) =>
  page.title || page.url || words.untitled;

/**
 * The Browser tab: what the session's agent did in its browser, step by
 * step, and the page as it is now. A person allowed to post may act on that
 * same browser by hand: go to an address, click or type on the page. Each
 * thing they do is recorded and goes with their next message; nothing is
 * sent to the agent before they send one.
 *
 * The steps are read from the answers' tool calls, and say where each left
 * the browser. A screenshot is taken when the person asks for one or acts,
 * through the session's own permission-checked route; the platform keeps
 * none of the agent's, so there is no picture of an earlier step to show.
 */
export function WorkbenchBrowser({
  thread,
  responses,
  canPost,
  selection,
  onSelection,
  openTab,
  record,
  ask,
  act = sessionBrowser,
  shots: store = browserShots,
}: WorkbenchTabProps & {
  /** The route, for a test to stand in for. */
  act?: BrowserAct;
  shots?: BrowserShots;
}) {
  const steps = useMemo(() => browserSteps(callsOfResponses(responses ?? [])), [responses]);
  const now = browserNow(steps);
  const taken = useBrowserShots(thread, store);
  const [shownShot, setShownShot] = useState<string>();
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  // Whether the session's browser can be reached from here: no once the server says it has none.
  const [reachable, setReachable] = useState(true);
  const [picked, setPicked] = useState<string>();
  const [address, setAddress] = useState("");
  const live = useRef(true);
  useEffect(() => {
    live.current = true;
    return () => {
      live.current = false;
    };
  }, []);
  // biome-ignore lint/correctness/useExhaustiveDependencies: another session starts clean
  useEffect(() => {
    setShownShot(undefined);
    setProblem(null);
    setReachable(true);
    setPicked(undefined);
    setAddress("");
  }, [thread]);

  const controls = canPost && reachable;
  const shot = taken.find((s) => s.id === shownShot) ?? taken[taken.length - 1];
  const isLatest = !!shot && shot === taken[taken.length - 1];

  /** Does one thing in the browser, keeps the page it answers with, and records what was done. */
  const run = async (
    action: BrowserAction,
    what: string,
    did?: (view: BrowserView) => HumanActivityInput,
  ) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    try {
      const view = await act(thread, now.provider ? { ...action, provider: now.provider } : action);
      store.add(thread, view, what);
      if (did) record(did(view));
      if (!live.current) return;
      setShownShot(undefined);
      setPicked(undefined);
      setAddress(view.url ?? "");
    } catch (e) {
      const why = e instanceof Error ? e.message : defaultCopy.failure.internal();
      // No browser to act on is not a failure of what was asked.
      const none = e instanceof ApiError && e.status === 404;
      if (did && !none) record({ source: "browser", summary: words.did.failed(what, why) });
      if (!live.current) return;
      if (none) setReachable(false);
      else
        setProblem(
          e instanceof ApiError && e.status === 403 ? why || defaultCopy.noPermission : why,
        );
    } finally {
      if (live.current) setBusy(false);
    }
  };

  const look = () => run({ action: "look" }, words.did.look);
  const goTo = (url: string) =>
    run({ action: "navigate", url }, words.did.navigate(url), (view) => ({
      source: "browser",
      summary: words.did.navigate(url),
      detail: words.did.page(view.title ?? "", view.url ?? ""),
    }));
  const goBack = () =>
    run({ action: "back" }, words.did.back(""), (view) => ({
      source: "browser",
      summary: words.did.back(view.url ?? ""),
      detail: words.did.page(view.title ?? "", view.url ?? ""),
    }));
  const click = (el: BrowserElement, on: BrowserShot) => {
    const summary = words.did.click(named(el), pageName(on));
    return run({ action: "click", target: el.ref, element: named(el) }, summary, (view) => ({
      source: "browser",
      summary,
      detail: words.did.page(view.title ?? "", view.url ?? ""),
    }));
  };
  // What was typed stays out of the record: it may be a password.
  const type = (el: BrowserElement, text: string, submit: boolean) => {
    const summary = words.did.type([...text].length, named(el), submit);
    return run(
      { action: "type", target: el.ref, element: named(el), text, submit },
      summary,
      (view) => ({
        source: "browser",
        summary,
        detail: words.did.page(view.title ?? "", view.url ?? ""),
      }),
    );
  };

  if (!responses) return <SkeletonRows count={4} className="p-5" />;

  if (!steps.length && !taken.length)
    return (
      <div className="flex flex-col items-start gap-3 p-5" data-testid="browser-empty">
        <p className="humagen-display font-medium text-aui-fg text-aui-lg">{words.none}</p>
        {controls ? (
          <button type="button" className={primary} disabled={busy} onClick={look}>
            {words.look}
          </button>
        ) : ask ? (
          <button type="button" className={primary} onClick={() => ask(words.askSeed)}>
            {words.ask}
          </button>
        ) : (
          <button type="button" className={button} onClick={() => openTab("details")}>
            {words.toDetails}
          </button>
        )}
        <Told busy={busy} problem={problem} unreachable={canPost && !reachable} />
      </div>
    );

  const submitAddress = (event: FormEvent) => {
    event.preventDefault();
    const url = address.trim();
    if (url) goTo(/^https?:\/\//i.test(url) ? url : `https://${url}`);
  };

  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col" data-testid="browser-tab">
      {controls && (
        <form
          onSubmit={submitAddress}
          className="flex shrink-0 flex-wrap items-center gap-2 border-aui-border border-b px-5 py-3"
        >
          <button type="button" className={button} disabled={busy || !shot} onClick={goBack}>
            {words.back}
          </button>
          <button type="button" className={button} disabled={busy} onClick={look}>
            {shot ? words.refresh : words.look}
          </button>
          {/* The address and its button wrap together, so Go is never alone on a line. */}
          <div className="flex min-w-0 flex-1 basis-64 items-center gap-2">
            <input
              type="text"
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              aria-label={words.address}
              placeholder={words.addressHint}
              value={address}
              onChange={(e) => setAddress(e.target.value)}
              className={`${field} min-w-0 flex-1 text-aui-base`}
            />
            <button type="submit" className={primary} disabled={busy || !address.trim()}>
              {words.go}
            </button>
          </div>
        </form>
      )}
      <div className="aui-scroll flex min-h-0 flex-1 flex-col gap-6 overflow-y-auto p-5">
        <Told busy={busy} problem={problem} unreachable={canPost && !reachable} />
        <section aria-label={words.page} className="flex min-w-0 flex-col gap-3">
          <PageLine
            title={shot ? shot.title : now.title}
            url={shot ? shot.url : now.url}
            provider={shot?.provider ?? now.provider}
          />
          {shot ? (
            <Stage
              shot={shot}
              // Only the page as it is now can be acted on: an earlier screenshot's elements are gone.
              act={controls && isLatest && !busy}
              picked={picked}
              onPick={setPicked}
              onClick={(el) => click(el, shot)}
              onType={(el, text, submit) => type(el, text, submit)}
            />
          ) : (
            <p className={quiet} data-testid="browser-no-shot">
              {controls ? words.noShot : words.notKept}
            </p>
          )}
          {taken.length > 1 && (
            <Shots
              taken={taken}
              shown={shot?.id}
              onShow={(id) => {
                setShownShot(id);
                setPicked(undefined);
              }}
            />
          )}
        </section>
        {steps.length > 0 && (
          <Steps
            steps={steps}
            selection={selection}
            onSelection={onSelection}
            onGo={controls && !busy ? goTo : undefined}
          />
        )}
      </div>
    </div>
  );
}

/** What is happening, and what went wrong, said once where the eye is. */
function Told({
  busy,
  problem,
  unreachable,
}: {
  busy: boolean;
  problem: string | null;
  unreachable: boolean;
}) {
  if (problem)
    return (
      <p
        role="alert"
        className="rounded-aui border border-aui-bad/40 bg-aui-bad/10 px-3 py-2 text-aui-base text-aui-fg"
      >
        {problem}
      </p>
    );
  if (busy)
    return (
      <output className={quiet} data-testid="browser-busy">
        {words.working}
      </output>
    );
  if (unreachable)
    return (
      <output className={quiet} data-testid="browser-unreachable">
        {words.noControl}
      </output>
    );
  return null;
}

/** The page's title and address, and where its browser runs. */
function PageLine({ title, url, provider }: { title?: string; url?: string; provider?: string }) {
  if (!title && !url && !provider) return null;
  return (
    <div className="flex min-w-0 flex-col">
      <h3 className={`${heading} truncate`}>{title || url || words.untitled}</h3>
      <p className={`${quiet} flex min-w-0 flex-wrap items-center gap-x-2`}>
        {url && /^https?:\/\//.test(url) ? (
          <a
            href={url}
            target="_blank"
            rel="noreferrer"
            className="inline-flex min-w-0 items-center gap-1 underline-offset-2 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
          >
            <span className="min-w-0 truncate">{url}</span>
            <ExternalLinkIcon size={14} />
          </a>
        ) : (
          url && <span className="min-w-0 truncate">{url}</span>
        )}
        {provider && <span className="shrink-0">{words.card.on(provider)}</span>}
      </p>
    </div>
  );
}

/**
 * A screenshot, with what can be acted on laid over it where it is on the
 * page. Picking one selects it; the row under the picture then clicks it or
 * types into it, so nothing on a shared browser is pressed by a stray click.
 * The same choices are a list under the picture, for a keyboard.
 */
function Stage({
  shot,
  act,
  picked,
  onPick,
  onClick,
  onType,
}: {
  shot: BrowserShot;
  act: boolean;
  picked?: string;
  onPick: (ref?: string) => void;
  onClick: (el: BrowserElement) => void;
  onType: (el: BrowserElement, text: string, submit: boolean) => void;
}) {
  // The picture's own size, which the elements' boxes are measured in.
  const [size, setSize] = useState<{ w: number; h: number }>();
  const [text, setText] = useState("");
  const [submit, setSubmit] = useState(false);
  const id = useId();
  const chosen = act ? shot.elements.find((el) => el.ref === picked) : undefined;
  // biome-ignore lint/correctness/useExhaustiveDependencies: another element starts with nothing typed
  useEffect(() => {
    setText("");
    setSubmit(false);
  }, [picked, shot.id]);
  const placed = act && size ? shot.elements.filter((el) => el.box?.length === 4) : [];
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <div className="relative w-full overflow-hidden rounded-aui border border-aui-border bg-aui-surface">
        {/* biome-ignore lint/a11y/noNoninteractiveElementInteractions: the picture's size is read when it has loaded, nothing is pressed */}
        <img
          src={shot.src}
          alt={words.screenshot(pageName(shot))}
          data-testid="browser-shot"
          className="block h-auto w-full"
          onLoad={(e) => {
            const img = e.currentTarget;
            if (img.naturalWidth && img.naturalHeight)
              setSize({ w: img.naturalWidth, h: img.naturalHeight });
          }}
        />
        {size &&
          placed.map((el) => {
            const [x = 0, y = 0, w = 0, h = 0] = el.box ?? [];
            if (x >= size.w || y >= size.h || x + w <= 0 || y + h <= 0) return null;
            return (
              <button
                key={el.ref}
                type="button"
                // The list under the picture is the keyboard's way in.
                tabIndex={-1}
                title={named(el)}
                aria-label={named(el)}
                aria-pressed={el.ref === picked}
                onClick={() => onPick(el.ref === picked ? undefined : el.ref)}
                style={{
                  left: `${(x / size.w) * 100}%`,
                  top: `${(y / size.h) * 100}%`,
                  width: `${(w / size.w) * 100}%`,
                  height: `${(h / size.h) * 100}%`,
                }}
                className="absolute cursor-pointer rounded-sm outline-2 outline-transparent hover:outline-aui-accent aria-pressed:bg-aui-accent/20 aria-pressed:outline-aui-accent"
              />
            );
          })}
      </div>
      {act && chosen && (
        <form
          data-testid="browser-chosen"
          onSubmit={(e) => {
            e.preventDefault();
            if (FIELDS.has(chosen.role) && text) onType(chosen, text, submit);
            else onClick(chosen);
          }}
          className="flex min-w-0 flex-col gap-2 rounded-aui-lg border border-aui-border bg-aui-surface p-3"
        >
          <p className="min-w-0 text-aui-base text-aui-fg">
            <span className="text-aui-muted">{words.selected}: </span>
            {named(chosen)}
          </p>
          {FIELDS.has(chosen.role) && (
            <>
              <input
                type="text"
                autoComplete="off"
                aria-label={words.typeInto(named(chosen))}
                value={text}
                onChange={(e) => setText(e.target.value)}
                className={`${field} text-aui-base`}
              />
              <label
                htmlFor={`${id}-submit`}
                className="flex items-center gap-2 text-aui-base text-aui-fg"
              >
                <input
                  id={`${id}-submit`}
                  type="checkbox"
                  checked={submit}
                  onChange={(e) => setSubmit(e.target.checked)}
                  className="size-4"
                />
                {words.submit}
              </label>
            </>
          )}
          <div className="flex flex-wrap items-center gap-2">
            {FIELDS.has(chosen.role) && (
              <button type="submit" className={primary} disabled={!text}>
                {words.type}
              </button>
            )}
            <button
              type="button"
              className={FIELDS.has(chosen.role) ? button : primary}
              onClick={() => onClick(chosen)}
            >
              {words.click}
            </button>
            <button type="button" className={button} onClick={() => onPick(undefined)}>
              {words.cancel}
            </button>
          </div>
        </form>
      )}
      {act && shot.elements.length > 0 && (
        <details className="min-w-0" data-testid="browser-elements">
          <summary className="cursor-pointer text-aui-base text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent">
            {words.elements} · {shot.elements.length}
          </summary>
          <ul className="aui-scroll mt-2 flex max-h-60 flex-col overflow-y-auto rounded-aui border border-aui-border">
            {shot.elements.map((el) => (
              <li key={el.ref}>
                <button
                  type="button"
                  aria-pressed={el.ref === picked}
                  onClick={() => onPick(el.ref === picked ? undefined : el.ref)}
                  className="flex min-h-9 w-full min-w-0 items-center gap-2 px-3 py-1.5 text-left text-aui-base text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:bg-aui-hover pointer-coarse:min-h-11"
                >
                  <span className="w-20 shrink-0 text-aui-muted">{el.role}</span>
                  <span className="min-w-0 truncate">{el.name || words.untitled}</span>
                </button>
              </li>
            ))}
          </ul>
          {shot.more > 0 && <p className={`${quiet} mt-2`}>{words.moreElements(shot.more)}</p>}
        </details>
      )}
      {act && shot.elements.length > 0 && !chosen && <p className={quiet}>{words.pick}</p>}
    </div>
  );
}

/** The screenshots taken here, newest first, to look back through. */
function Shots({
  taken,
  shown,
  onShow,
}: {
  taken: readonly BrowserShot[];
  shown?: string;
  onShow: (id?: string) => void;
}) {
  const latest = taken[taken.length - 1];
  return (
    <div className="flex min-w-0 flex-col gap-2" data-testid="browser-shots">
      <h3 className={heading}>{words.shots}</h3>
      <ul className="aui-scroll flex gap-2 overflow-x-auto pb-1">
        {[...taken].reverse().map((s) => (
          <li key={s.id} className="shrink-0">
            <button
              type="button"
              aria-pressed={s.id === shown}
              aria-label={words.shotOf(ago(new Date(s.at).toISOString()), s.what)}
              title={s.what}
              onClick={() => onShow(s === latest ? undefined : s.id)}
              className="flex w-36 flex-col gap-1 rounded-aui border border-aui-border p-1 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:border-aui-accent"
            >
              <img src={s.src} alt="" className="h-20 w-full rounded-sm object-cover object-top" />
              <span className="truncate px-1 text-aui-base text-aui-fg">
                {s === latest ? words.latest : ago(new Date(s.at).toISOString())}
              </span>
              <span className="truncate px-1 text-aui-base text-aui-muted">{s.what}</span>
            </button>
          </li>
        ))}
      </ul>
    </div>
  );
}

/**
 * What the agent did, oldest first, with a way to step back through it. The
 * step the address names is open: where it left the browser, why it failed,
 * and a way to take the browser back to that address.
 */
function Steps({
  steps,
  selection,
  onSelection,
  onGo,
}: {
  steps: BrowserStep[];
  selection?: string;
  onSelection: (ref?: string) => void;
  onGo?: (url: string) => void;
}) {
  const found = steps.findIndex((s) => s.id === selection);
  const at = found < 0 ? steps.length - 1 : found;
  const open = useRef<HTMLLIElement>(null);
  useEffect(() => {
    if (found >= 0) open.current?.scrollIntoView?.({ block: "nearest" });
  }, [found]);
  const move = (to: number) => onSelection(steps[to]?.id);
  const arrow =
    "inline-flex size-9 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40 pointer-coarse:size-11";
  return (
    <section
      aria-label={words.steps}
      className="flex min-w-0 flex-col gap-2"
      data-testid="browser-steps"
    >
      <div className="flex min-w-0 items-center justify-between gap-3">
        <h3 className={heading}>{words.steps}</h3>
        <div className="flex shrink-0 items-center gap-1">
          <span className={`${quiet} tabular-nums`}>{words.stepOf(at + 1, steps.length)}</span>
          <button
            type="button"
            aria-label={words.earlier}
            title={words.earlier}
            disabled={at <= 0}
            onClick={() => move(at - 1)}
            className={arrow}
          >
            <span className="inline-flex rotate-180">
              <ChevronRightIcon size={16} />
            </span>
          </button>
          <button
            type="button"
            aria-label={words.later}
            title={words.later}
            disabled={at >= steps.length - 1}
            onClick={() => move(at + 1)}
            className={arrow}
          >
            <ChevronRightIcon size={16} />
          </button>
        </div>
      </div>
      <ol className="flex min-w-0 flex-col divide-y divide-[var(--humagen-rule)] border-[var(--humagen-rule)] border-y">
        {steps.map((step, i) => {
          const current = i === at;
          return (
            <li
              key={step.id}
              ref={current ? open : undefined}
              data-step={step.id}
              className="min-w-0"
            >
              <button
                type="button"
                aria-current={current ? "step" : undefined}
                onClick={() => onSelection(step.id)}
                className="flex min-h-11 w-full min-w-0 items-start gap-3 px-1 py-2 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent aria-[current=step]:bg-aui-hover"
              >
                <span className={`${quiet} w-6 shrink-0 text-right tabular-nums`}>{i + 1}</span>
                <span className="flex min-w-0 flex-1 flex-col">
                  <span className="min-w-0 break-words text-aui-base text-aui-fg">
                    {step.label}
                  </span>
                  {(step.title || step.url) && (
                    <span className={`${quiet} min-w-0 truncate`}>{step.title || step.url}</span>
                  )}
                </span>
                {(step.failed || step.running) && (
                  <span className="flex shrink-0 items-center gap-1.5 text-aui-base text-aui-fg">
                    <span
                      aria-hidden
                      className={`size-2 rounded-full ${step.failed ? "bg-aui-bad" : "bg-aui-info"}`}
                    />
                    {step.failed ? words.failed : words.running}
                  </span>
                )}
              </button>
              {current && (step.url || step.error) && (
                <div
                  className="flex min-w-0 flex-col items-start gap-2 pb-3 pl-10"
                  data-testid="browser-step-open"
                >
                  {step.error && (
                    <p className="min-w-0 break-words text-aui-base text-aui-fg">{step.error}</p>
                  )}
                  {step.url && (
                    <p className={`${quiet} min-w-0 max-w-full break-all`}>{step.url}</p>
                  )}
                  {step.url && onGo && /^https?:\/\//.test(step.url) && (
                    <button type="button" className={button} onClick={() => onGo(step.url ?? "")}>
                      {words.goThere}
                    </button>
                  )}
                </div>
              )}
            </li>
          );
        })}
      </ol>
    </section>
  );
}
