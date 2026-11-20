import { defaultCopy } from "@humagen/components";
import {
  type FoldedEntry,
  foldHumanActivity,
  type HumanActivityInput,
  type HumanActivityRecord,
  type HumanActivitySource,
  type HumanActivityStore,
  humanActivity,
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
import { type ReactNode, useCallback, useMemo, useSyncExternalStore } from "react";

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

/**
 * What goes with the person's next message, above the composer: one line for
 * each thing they did, each with a way to take it out. Nothing draws when
 * there is nothing recorded. `draft` is what is typed so far, which decides
 * how much room the message has left.
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
  if (!records.length) return null;
  const { omitted } = foldHumanActivity(draft, records);
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
      <ul className="aui-scroll flex max-h-40 flex-col overflow-y-auto">
        {records.map((r) => (
          <li
            key={r.id}
            data-source={r.source}
            className="flex min-w-0 items-center gap-2 text-aui-base text-aui-fg"
          >
            <span className="flex size-5 shrink-0 items-center justify-center text-aui-muted">
              {sourceMark[r.source]}
            </span>
            <span className="sr-only">{words.source[r.source]}: </span>
            <span className="min-w-0 flex-1 truncate" title={r.detail ?? r.summary}>
              {r.summary}
            </span>
            <button
              type="button"
              aria-label={words.remove(r.summary)}
              title={words.remove(r.summary)}
              onClick={() => remove(r.id)}
              className={`${quiet} size-9 pointer-coarse:size-11`}
            >
              <XIcon size={16} />
            </button>
          </li>
        ))}
      </ul>
      {omitted > 0 && (
        <p role="status" className="text-aui-base text-aui-muted">
          {words.overflow(omitted)}
        </p>
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
