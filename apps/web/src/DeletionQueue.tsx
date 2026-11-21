import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { coworking, type Deletion } from "./api.ts";
import { card, Problem, pageLede, pageTitle, SkeletonRows } from "./ui.tsx";

const day = (iso: string) =>
  new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });
const words = defaultCopy.deletions;

/**
 * Archived conversations waiting on an admin. One is due when someone asked
 * to delete it, or 30 days after it was archived. Approving removes its
 * messages for good; rejecting brings it back.
 */
export function DeletionQueue() {
  const [rows, setRows] = useState<Deletion[] | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const load = () => coworking.deletions().then(setRows, () => setProblem(words.loadFailed));
  useEffect(() => {
    load();
  }, []);
  const act = (d: Deletion, approve: boolean) => {
    setBusy(d.id);
    setProblem("");
    (approve ? coworking.approveDeletion(d.id) : coworking.rejectDeletion(d.id))
      .then(() => {
        setRows((r) => r?.filter((x) => x.id !== d.id) ?? r);
        return load();
      })
      .catch(() => setProblem(approve ? words.deleteFailed : words.restoreFailed))
      .finally(() => setBusy(null));
  };
  const due = rows?.filter((r) => r.due) ?? [];
  const waiting = rows?.filter((r) => !r.due) ?? [];
  const list = (items: Deletion[]) => (
    <ul className="flex flex-col gap-2">
      {items.map((d) => (
        <li key={d.id} className={`${card} flex flex-wrap items-center gap-3 p-4`}>
          <div className="min-w-0 flex-1">
            <p className="truncate text-aui-sm text-aui-fg">{d.title || words.untitled}</p>
            <p className="flex flex-wrap gap-x-2 gap-y-1 text-aui-xs text-aui-muted">
              <span>
                {d.requested ? words.requested : words.archived}{" "}
                {words.by(d.archived_by, day(d.archived_at))}
              </span>
              {!d.due && <span>{words.due(day(d.delete_after))}</span>}
              {d.messages > 0 && <span>{words.messages(d.messages)}</span>}
            </p>
          </div>
          <div className="flex gap-1.5">
            <button
              type="button"
              disabled={busy === d.id}
              onClick={() => act(d, false)}
              className="min-h-9 rounded-aui border border-aui-border px-3 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:min-h-11"
            >
              {words.restore}
            </button>
            <button
              type="button"
              disabled={busy === d.id || !d.due}
              title={d.due ? undefined : words.available(day(d.delete_after))}
              onClick={() => act(d, true)}
              className="min-h-9 rounded-aui bg-aui-bad px-3 text-aui-sm text-white hover:opacity-90 focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40 pointer-coarse:min-h-11"
            >
              {words.approve}
            </button>
          </div>
        </li>
      ))}
    </ul>
  );
  return (
    <>
      <header className="pb-2">
        <h1 className={pageTitle}>{words.title}</h1>
        <p className={pageLede}>{words.hint}</p>
      </header>
      <Problem message={problem} />
      {rows === null ? (
        !problem && <SkeletonRows count={3} />
      ) : rows.length === 0 ? (
        <p className="text-aui-muted">{words.empty}</p>
      ) : (
        <>
          <section className="flex flex-col gap-2">
            <h2 className="text-aui-sm font-medium text-aui-fg">{words.waiting(due.length)}</h2>
            {due.length ? list(due) : <p className="text-aui-sm text-aui-muted">{words.notDue}</p>}
          </section>
          {waiting.length > 0 && (
            <section className="flex flex-col gap-2 pt-4">
              <h2 className="text-aui-sm font-medium text-aui-fg">
                {words.archivedCount(waiting.length)}
              </h2>
              {list(waiting)}
            </section>
          )}
        </>
      )}
    </>
  );
}
