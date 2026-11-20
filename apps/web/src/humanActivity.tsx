import { defaultCopy } from "@humagen/components";
import {
  type FoldedEntry,
  foldHumanActivity,
  type HumanActivityInput,
  type HumanActivityRecord,
  type HumanActivitySource,
  type HumanActivityStore,
  humanActivity,
  MAX_HUMAN_ACTIVITY,
  type SplitHumanActivity,
} from "@humagen/core";
import {
  ChevronRightIcon,
  DiffIcon,
  FileIcon,
  HandIcon,
  MonitorIcon,
  TerminalIcon,
  XIcon,
} from "@humagen/render-web";
import {
  type ReactNode,
  useCallback,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  useSyncExternalStore,
} from "react";

/*
 * What a person did by hand in a session's workbench, on the page: the hook a
 * tab records with, the list above the composer, and the collapsed line a
 * thread draws on a message that carried some. The records, the cap and the
 * fold into a message are core's (packages/core/src/human-activity.ts).
 */

const words = defaultCopy.workbench.byHand;

const sourceMark: Record<HumanActivitySource, ReactNode> = {
  terminal: <TerminalIcon size={16} aria-hidden />,
  browser: <MonitorIcon size={16} aria-hidden />,
  files: <FileIcon size={16} aria-hidden />,
  changes: <DiffIcon size={16} aria-hidden />,
  other: <HandIcon size={16} aria-hidden />,
};

export interface HumanActivity {
  /** What is recorded and not yet sent, oldest first. */
  records: readonly HumanActivityRecord[];
  /** Records something the person did. Nothing is sent until they submit. */
  record: (input: HumanActivityInput) => HumanActivityRecord | undefined;
  remove: (id: string) => void;
  clear: () => void;
}

const none = () => () => {};

/**
 * A conversation's record of what its person did by hand since their last
 * message. A workbench tab calls `record`; the composer shows `records` and
 * folds them into the next message.
 */
export function useHumanActivity(
  conversation: string | undefined,
  store: HumanActivityStore = humanActivity,
): HumanActivity {
  const subscribe = useCallback(
    (changed: () => void) => (conversation ? store.subscribe(conversation, changed) : none()),
    [conversation, store],
  );
  const read = useCallback(() => store.list(conversation ?? ""), [conversation, store]);
  const records = useSyncExternalStore(subscribe, read, read);
  return useMemo(
    () => ({
      records,
      record: (input) => (conversation ? store.add(conversation, input) : undefined),
      remove: (id) => {
        if (conversation) store.remove(conversation, id);
      },
      clear: () => {
        if (conversation) store.clear(conversation);
      },
    }),
    [records, conversation, store],
  );
}

const quiet =
  "inline-flex shrink-0 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent";

/** How many lines the list shows before the rest are asked for. */
const SHOWN = 4;

const detailBox =
  "ml-7 whitespace-pre-wrap break-words rounded-aui bg-aui-hover px-2.5 py-2 font-aui-mono text-aui-base text-aui-fg";

/**
 * One thing that goes with the next message. A line with a detail opens in
 * place to show it whole, since the detail is sent under the person's name
 * and they did not write it. `bare` says the message has no room for the
 * detail, and `later` that it has none for the line.
 */
function Row({
  record: r,
  bare,
  later,
  onRemove,
  onResize,
}: {
  record: HumanActivityRecord;
  bare: boolean;
  /** The next message has no room for the line at all. */
  later: boolean;
  onRemove: () => void;
  /** The line opened or closed, so the list is a different height. */
  onResize: () => void;
}) {
  const line = (
    <>
      <span className="flex h-6 w-5 shrink-0 items-center justify-center text-aui-muted">
        {sourceMark[r.source]}
      </span>
      <span className="sr-only">{words.source[r.source]}: </span>
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="break-words">{r.summary}</span>
        {bare && <span className="text-aui-muted">{words.rowBare}</span>}
        {later && <span className="text-aui-muted">{words.rowLater}</span>}
      </span>
    </>
  );
  return (
    <li
      data-source={r.source}
      data-bare={bare || undefined}
      data-later={later || undefined}
      className="flex min-w-0 items-start gap-1 text-aui-base text-aui-fg"
    >
      {r.detail ? (
        <details
          className="group/row min-w-0 flex-1"
          // An opened detail is brought into the list's view, in one step.
          onToggle={(e) => {
            if (e.currentTarget.open) e.currentTarget.scrollIntoView?.({ block: "nearest" });
            onResize();
          }}
        >
          <summary className="flex min-h-9 cursor-pointer list-none items-start gap-2 rounded-aui py-1.5 hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11 pointer-coarse:py-2.5 [&::-webkit-details-marker]:hidden">
            {line}
            <span className="flex h-6 shrink-0 items-center text-aui-muted">
              <ChevronRightIcon size={14} aria-hidden className="group-open/row:rotate-90" />
            </span>
          </summary>
          <pre data-testid="human-activity-detail" className={`${detailBox} mb-1.5`}>
            {r.detail}
          </pre>
        </details>
      ) : (
        <div className="flex min-h-9 min-w-0 flex-1 items-start gap-2 py-1.5 pointer-coarse:min-h-11 pointer-coarse:py-2.5">
          {line}
        </div>
      )}
      <button
        type="button"
        aria-label={words.remove(r.summary)}
        title={words.remove(r.summary)}
        onClick={onRemove}
        className={`${quiet} size-9 pointer-coarse:size-11`}
      >
        <XIcon size={16} />
      </button>
    </li>
  );
}

/**
 * What goes with the person's next message, above the composer: one line for
 * each thing they did, each with a way to take it out, and each detail a
 * press away, so all of what is sent can be read first. The newest few show;
 * a button above them shows the rest. Under the list it says what the message
 * will not carry: details with no room, lines with no room, and lines dropped
 * when the list was full. Nothing draws when there is nothing recorded.
 * `draft` is what is typed so far, which decides how much room the message
 * has left.
 */
export function HumanActivityList({
  conversation,
  draft = "",
  store = humanActivity,
}: {
  conversation: string;
  draft?: string;
  store?: HumanActivityStore;
}) {
  const { records, remove, clear } = useHumanActivity(conversation, store);
  const [all, setAll] = useState(false);
  // The list scrolls when it is tall. A rule on an edge says lines are out of
  // view past it, so a line cut by the edge is not taken for the last.
  const box = useRef<HTMLUListElement>(null);
  const [past, setPast] = useState({ above: false, below: false });
  const measure = useCallback(() => {
    const el = box.current;
    if (!el) return;
    const above = el.scrollTop > 1;
    const below = el.scrollTop + el.clientHeight < el.scrollHeight - 1;
    setPast((was) => (was.above === above && was.below === below ? was : { above, below }));
  }, []);
  useLayoutEffect(measure);
  if (!records.length) return null;
  const { omitted, bare, included } = foldHumanActivity(draft, records);
  // Read with the records: it changes only when they do.
  const dropped = store.dropped(conversation);
  const hidden = records.length > SHOWN ? records.length - SHOWN : 0;
  const shown = all || !hidden ? records : records.slice(-SHOWN);
  return (
    <section
      aria-label={words.pending(records.length)}
      data-testid="human-activity"
      className="flex flex-col gap-1 rounded-aui-lg border border-aui-border bg-aui-surface px-3 py-2"
    >
      <div className="flex items-center justify-between gap-3">
        <h3 className="min-w-0 text-aui-base text-aui-muted">{words.pending(records.length)}</h3>
        <button
          type="button"
          onClick={clear}
          className={`${quiet} min-h-9 px-2 text-aui-base pointer-coarse:min-h-11`}
        >
          {words.clear}
        </button>
      </div>
      {hidden > 0 && (
        <button
          type="button"
          aria-expanded={all}
          onClick={() => setAll(!all)}
          className={`${quiet} min-h-9 self-start px-2 text-aui-base pointer-coarse:min-h-11`}
        >
          {all ? words.fewer : words.earlier(hidden)}
        </button>
      )}
      <ul
        ref={box}
        onScroll={measure}
        data-more-above={past.above || undefined}
        data-more-below={past.below || undefined}
        className={`aui-scroll flex max-h-[40vh] flex-col overflow-y-auto border-y ${past.above ? "border-t-aui-muted" : "border-t-transparent"} ${past.below ? "border-b-aui-muted" : "border-b-transparent"}`}
      >
        {shown.map((r) => (
          <Row
            key={r.id}
            record={r}
            bare={bare.includes(r.id)}
            later={!included.includes(r.id)}
            onRemove={() => remove(r.id)}
            onResize={measure}
          />
        ))}
      </ul>
      {(bare.length > 0 || omitted > 0 || dropped > 0) && (
        <div role="status" className="flex flex-col gap-1 text-aui-base text-aui-muted">
          {bare.length > 0 && <p data-testid="human-activity-bare">{words.bare(bare.length)}</p>}
          {omitted > 0 && <p data-testid="human-activity-overflow">{words.overflow(omitted)}</p>}
          {dropped > 0 && (
            <p data-testid="human-activity-dropped">{words.dropped(dropped, MAX_HUMAN_ACTIVITY)}</p>
          )}
        </div>
      )}
    </section>
  );
}

function Entry({ entry }: { entry: FoldedEntry }) {
  return (
    <li data-source={entry.source} className="flex min-w-0 flex-col gap-1">
      <span className="flex min-w-0 items-start gap-2">
        <span className="flex h-6 w-5 shrink-0 items-center justify-center text-aui-muted">
          {sourceMark[entry.source]}
        </span>
        <span className="sr-only">{words.source[entry.source]}: </span>
        <span className="min-w-0 break-words">{entry.summary}</span>
      </span>
      {entry.detail && (
        <pre className="aui-scroll ml-7 max-h-48 overflow-auto whitespace-pre-wrap break-words rounded-aui bg-aui-hover px-2.5 py-2 font-aui-mono text-aui-base text-aui-fg">
          {entry.detail}
        </pre>
      )}
    </li>
  );
}

/**
 * On a message in a thread: what its person did by hand before sending it,
 * closed until it is opened, so a long message stays readable. `mine` says
 * the reader sent it.
 */
export function HumanActivityNote({
  activity,
  mine = false,
}: {
  activity: NonNullable<SplitHumanActivity["activity"]>;
  mine?: boolean;
}) {
  return (
    <details
      data-testid="human-activity-note"
      className={`group/byhand max-w-full text-aui-base text-aui-fg ${mine ? "sm:max-w-[70%]" : ""}`}
    >
      <summary className="inline-flex min-h-9 cursor-pointer list-none items-center gap-1.5 rounded-aui px-1.5 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent pointer-coarse:min-h-11 [&::-webkit-details-marker]:hidden">
        <ChevronRightIcon
          size={14}
          aria-hidden
          className="shrink-0 transition-transform group-open/byhand:rotate-90 motion-reduce:transition-none"
        />
        {words.did(activity.count, mine)}
      </summary>
      <ul className="flex flex-col gap-2 py-1 pl-1.5 text-left">
        {activity.entries.map((entry, index) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: A posted message's entries are fixed and may repeat.
          <Entry key={index} entry={entry} />
        ))}
      </ul>
      {activity.omitted > 0 && (
        <p className="pl-1.5 text-aui-muted">{words.omitted(activity.omitted)}</p>
      )}
    </details>
  );
}
