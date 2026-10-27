import { useEffect, useState } from "react";
import { coworking, type Deletion } from "./api.ts";
import { card, pageTitle, pageLede, Problem, SkeletonRows } from "./ui.tsx";

const day = (iso: string) => new Date(iso).toLocaleDateString(undefined, { month: "short", day: "numeric", year: "numeric" });

/**
 * Archived conversations waiting on an admin. One is due when someone asked
 * to delete it, or 30 days after it was archived. Approving removes its
 * messages for good; rejecting brings it back.
 */
export function DeletionQueue() {
  const [rows, setRows] = useState<Deletion[] | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState<string | null>(null);
  const load = () =>
    coworking.deletions().then(setRows, () => setProblem("The deletion queue could not be loaded."));
  useEffect(() => {
    load();
  }, []);
  const act = (d: Deletion, approve: boolean) => {
    setBusy(d.id);
    setProblem("");
    (approve ? coworking.approveDeletion(d.id) : coworking.rejectDeletion(d.id))
      .then(() => setRows((r) => r?.filter((x) => x.id !== d.id) ?? r))
      .catch(() => setProblem(approve ? "Could not delete. Try again." : "Could not restore. Try again."))
      .finally(() => setBusy(null));
  };
  const due = rows?.filter((r) => r.due) ?? [];
  const waiting = rows?.filter((r) => !r.due) ?? [];
  const list = (items: Deletion[]) => (
    <ul className="flex flex-col gap-2">
      {items.map((d) => (
        <li key={d.id} className={`${card} flex flex-wrap items-center gap-3 p-4`}>
          <div className="min-w-0 flex-1">
            <p className="truncate text-aui-sm text-aui-fg">{d.title || "Untitled conversation"}</p>
            <p className="text-aui-xs text-aui-muted">
              {d.requested ? "Delete asked for" : "Archived"} by {d.archived_by} on {day(d.archived_at)}
              {!d.due && ` · due ${day(d.delete_after)}`}
              {d.messages > 0 && ` · ${d.messages} message${d.messages === 1 ? "" : "s"}`}
            </p>
          </div>
          <div className="flex gap-1.5">
            <button
              type="button"
              disabled={busy === d.id}
              onClick={() => act(d, false)}
              className="h-8 rounded-aui border border-aui-border px-3 text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50"
            >
              Restore
            </button>
            <button
              type="button"
              disabled={busy === d.id || !d.due}
              title={d.due ? undefined : `Can be approved from ${day(d.delete_after)}`}
              onClick={() => act(d, true)}
              className="h-8 rounded-aui bg-aui-bad px-3 text-aui-sm text-white hover:opacity-90 focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40"
            >
              Approve deletion
            </button>
          </div>
        </li>
      ))}
    </ul>
  );
  return (
    <>
      <header className="pb-2">
        <h1 className={pageTitle}>Deletion queue</h1>
        <p className={pageLede}>Archived conversations are deleted only when you approve. Each is due 30 days after it is archived, or at once when someone asks to delete it.</p>
      </header>
      <Problem message={problem} />
      {rows === null ? (
        !problem && <SkeletonRows count={3} />
      ) : rows.length === 0 ? (
        <p className="text-aui-muted">Nothing is archived.</p>
      ) : (
        <>
          <section className="flex flex-col gap-2">
            <h2 className="text-aui-sm font-medium text-aui-fg">Waiting on you ({due.length})</h2>
            {due.length ? list(due) : <p className="text-aui-sm text-aui-muted">Nothing is due.</p>}
          </section>
          {waiting.length > 0 && (
            <section className="flex flex-col gap-2 pt-4">
              <h2 className="text-aui-sm font-medium text-aui-fg">Archived, not yet due ({waiting.length})</h2>
              {list(waiting)}
            </section>
          )}
        </>
      )}
    </>
  );
}
