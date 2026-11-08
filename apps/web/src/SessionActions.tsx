import { EllipsisIcon } from "@humagen/render-web";
import { Modal } from "@humagen/web";
import { useRef, useState } from "react";
import { coworking, type Thread } from "./api.ts";
import { publishMarks } from "./marks.ts";
import { button, field, Popover, Problem, primary } from "./ui.tsx";

/** The header uses the same ownership and governed deletion as the session list. */
export function SessionActions({
  thread,
  canManage,
  onChanged,
  onUpdated,
  onShare,
}: {
  thread: Thread;
  canManage: boolean;
  onChanged: () => void;
  onUpdated: (thread: Thread) => void;
  onShare?: () => void;
}) {
  const [mode, setMode] = useState<"rename" | "delete">();
  const [title, setTitle] = useState(thread.title ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string>();
  const [archived, setArchived] = useState(false);
  const [deleted, setDeleted] = useState(false);
  const marksRequest = useRef(0);
  const act = async (operation: () => Promise<unknown>) => {
    if (busy) return;
    marksRequest.current++;
    setBusy(true);
    setProblem(undefined);
    try {
      await operation();
      onChanged();
    } catch (error) {
      setProblem(error instanceof Error ? error.message : "The session could not be updated.");
    } finally {
      setBusy(false);
    }
  };
  const archive = () =>
    act(async () => {
      const marks = await coworking.markThread(thread.id, { archived: !archived });
      publishMarks(marks);
      setArchived(!archived);
    });
  return (
    <>
      <Popover
        label="Session actions"
        className="inline-flex size-9 items-center justify-center rounded-aui hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
        button={<EllipsisIcon size={18} />}
        narrow
        onOpen={() => {
          const generation = ++marksRequest.current;
          coworking.marks().then(
            (marks) => {
              if (generation === marksRequest.current)
                setArchived(marks.archived.includes(thread.id));
            },
            (error) => {
              if (generation === marksRequest.current)
                setProblem(
                  error instanceof Error ? error.message : "Archive status could not be loaded.",
                );
            },
          );
        }}
      >
        {(close) => (
          <div role="menu" aria-label="Session actions" className="flex flex-col">
            {onShare && (
              <button
                type="button"
                role="menuitem"
                className={button}
                onClick={() => {
                  close();
                  onShare();
                }}
              >
                Share
              </button>
            )}
            {canManage && (
              <button
                type="button"
                role="menuitem"
                className={button}
                disabled={busy || deleted}
                onClick={() => {
                  close();
                  setTitle(thread.title ?? "");
                  setMode("rename");
                }}
              >
                Rename
              </button>
            )}
            <button
              type="button"
              role="menuitem"
              className={button}
              disabled={busy || deleted}
              onClick={() => {
                close();
                void archive();
              }}
            >
              {archived ? "Unarchive" : "Archive"}
            </button>
            {canManage && (
              <button
                type="button"
                role="menuitem"
                className={`${button} text-aui-bad`}
                disabled={busy || deleted}
                onClick={() => {
                  close();
                  setMode("delete");
                }}
              >
                Delete
              </button>
            )}
          </div>
        )}
      </Popover>
      {(problem || archived || deleted) && (
        <div className="basis-full text-aui-xs" role="status">
          <Problem message={problem ?? null} />
          {archived && !deleted && (
            <span>
              Archived for you.{" "}
              <button
                type="button"
                disabled={busy}
                className="underline"
                onClick={() => void archive()}
              >
                Undo
              </button>
            </span>
          )}
          {deleted && <span>Deletion requested. An administrator must approve removal.</span>}
        </div>
      )}
      {mode && (
        <Modal
          title={mode === "rename" ? "Rename session" : "Delete session"}
          onClose={() => {
            if (!busy) setMode(undefined);
          }}
        >
          <form
            className="flex flex-col gap-4 p-5"
            onSubmit={(event) => {
              event.preventDefault();
              void act(async () => {
                if (mode === "rename") onUpdated(await coworking.rename(thread.id, title.trim()));
                else {
                  await coworking.archive(thread.id, true);
                  setDeleted(true);
                }
                setMode(undefined);
              });
            }}
          >
            <h3 className="text-aui-lg font-medium">
              {mode === "rename" ? "Rename session" : "Delete session?"}
            </h3>
            {mode === "rename" ? (
              <label className="flex flex-col gap-2 text-aui-sm">
                Name
                <input
                  autoFocus
                  className={field}
                  value={title}
                  maxLength={200}
                  onChange={(e) => setTitle(e.target.value)}
                />
              </label>
            ) : (
              <p className="text-aui-sm">
                This session is archived now. Nothing is deleted until an administrator approves
                removal.
              </p>
            )}
            <Problem message={problem ?? null} />
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className={button}
                disabled={busy}
                onClick={() => setMode(undefined)}
              >
                Cancel
              </button>
              <button
                type="submit"
                className={primary}
                disabled={busy || (mode === "rename" && !title.trim())}
              >
                {busy ? "Saving…" : mode === "rename" ? "Save name" : "Request deletion"}
              </button>
            </div>
          </form>
        </Modal>
      )}
    </>
  );
}
