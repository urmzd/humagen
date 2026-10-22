import { defaultCopy } from "@warp/components";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import { ApiError, type Channel, coworking, type DirectoryPerson, type ProjectView, type Thread, type ThreadPage } from "./api.ts";
import { button, field, Label, Problem, primary } from "./ui.tsx";

const words = defaultCopy.threads;

/**
 * People to add, completed as their name or handle is typed: arrow keys move,
 * Enter or Tab picks, Escape closes the list. The reader and anyone already
 * in the conversation are never offered.
 */
function PeopleToAdd({
  value,
  leaveOut,
  onChange,
}: {
  value: DirectoryPerson[];
  /** Subjects and handles never offered: the reader and the people already in it. */
  leaveOut: string[];
  onChange: (next: DirectoryPerson[]) => void;
}) {
  const [query, setQuery] = useState("");
  const [found, setFound] = useState<DirectoryPerson[]>([]);
  const [active, setActive] = useState(0);
  const [open, setOpen] = useState(false);
  const timer = useRef<number | undefined>(undefined);
  const asked = useRef(0);
  const list = useId();

  useEffect(() => () => window.clearTimeout(timer.current), []);

  const look = (q: string) => {
    setQuery(q);
    setActive(0);
    setOpen(q.trim().length > 0);
    window.clearTimeout(timer.current);
    if (!q.trim()) return setFound([]);
    const n = ++asked.current;
    timer.current = window.setTimeout(() => {
      coworking
        .people(q.trim(), 9)
        .then((p) => n === asked.current && setFound(p))
        .catch(() => n === asked.current && setFound([]));
    }, 150);
  };

  const shown = found
    .filter((p) => !leaveOut.includes(p.subject) && !leaveOut.includes(p.handle) && !value.some((v) => v.subject === p.subject))
    .slice(0, 8);
  const listed = open && shown.length > 0;

  const pick = (p: DirectoryPerson) => {
    onChange([...value, p]);
    setQuery("");
    setFound([]);
    setOpen(false);
  };

  const onKeyDown = (e: KeyboardEvent<HTMLInputElement>) => {
    if (!listed || e.nativeEvent.isComposing) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => (i + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
    } else if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault();
      const p = shown[Math.min(active, shown.length - 1)];
      if (p) pick(p);
    } else if (e.key === "Escape") {
      // Closes the list, not the dialog.
      e.preventDefault();
      e.stopPropagation();
      setOpen(false);
    }
  };

  return (
    <div className="flex flex-col gap-2">
      {value.length > 0 && (
        <ul aria-label={words.shareAdding} className="flex flex-wrap gap-1.5">
          {value.map((p) => (
            <li key={p.subject} className="inline-flex h-7 max-w-full items-center gap-1 rounded-full border border-aui-border pr-1 pl-2.5 text-aui-xs text-aui-fg">
              <span className="truncate">@{p.handle}</span>
              <button
                type="button"
                aria-label={words.shareRemove(p.handle)}
                onClick={() => onChange(value.filter((v) => v.subject !== p.subject))}
                className="inline-flex size-5 items-center justify-center rounded-full text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <span aria-hidden>×</span>
              </button>
            </li>
          ))}
        </ul>
      )}
      <div className="relative">
        <input
          className={field}
          value={query}
          placeholder={words.shareAddHint}
          aria-label={words.shareAdd}
          role="combobox"
          aria-expanded={listed}
          aria-controls={listed ? list : undefined}
          aria-autocomplete="list"
          aria-activedescendant={listed ? `${list}-${active}` : undefined}
          onChange={(e) => look(e.target.value)}
          onKeyDown={onKeyDown}
          onBlur={() => setOpen(false)}
        />
        {listed && (
          // Still: a list waiting on a person does not animate.
          <ul
            id={list}
            role="listbox"
            aria-label={words.shareAdd}
            className="absolute right-0 left-0 top-full z-20 mt-1 max-h-56 overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1 shadow-lg"
          >
            {shown.map((p, i) => (
              <li
                key={p.subject}
                id={`${list}-${i}`}
                role="option"
                aria-selected={i === active}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pick(p);
                }}
                className="flex cursor-pointer items-baseline gap-2 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg aria-selected:bg-aui-hover"
              >
                <span className="shrink-0 font-medium">@{p.handle}</span>
                {p.name && <span className="min-w-0 truncate text-aui-xs text-aui-muted">{p.name}</span>}
              </li>
            ))}
          </ul>
        )}
      </div>
    </div>
  );
}

/**
 * Share a private conversation: the project (only while no agent has run in
 * it), the channel (general unless another is picked) and people to add.
 * It calls `POST /api/threads/{id}/share` and hands back the thread as it
 * then stands. A project the server will not change to says why.
 */
export function ShareDialog({
  thread,
  hasRuns,
  projects,
  workspaceName,
  me,
  onShared,
  onClose,
}: {
  thread: Thread;
  /** Whether an agent has run in it: the project is fixed then. */
  hasRuns: boolean;
  /** The projects the reader may share into. */
  projects: ProjectView[];
  workspaceName: string;
  me: string;
  onShared: (page: ThreadPage) => void;
  onClose: () => void;
}) {
  const [project, setProject] = useState(thread.project);
  const [channels, setChannels] = useState<Channel[] | null>(null);
  const [channel, setChannel] = useState("general");
  const [people, setPeople] = useState<DirectoryPerson[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const heading = useId();
  const first = useRef<HTMLSelectElement>(null);

  useEffect(() => {
    first.current?.focus();
  }, []);

  useEffect(() => {
    let live = true;
    setChannel("general");
    coworking
      .channels(project)
      .then((cs) => live && setChannels(cs.filter((c) => !c.archived)))
      .catch(() => live && setChannels(null));
    return () => {
      live = false;
    };
  }, [project]);

  const share = () => {
    setProblem(null);
    setBusy(true);
    coworking
      .share(thread.id, {
        ...(project !== thread.project ? { project: project === "" ? "~" : project } : {}),
        channel,
        ...(people.length > 0 ? { add: people.map((p) => p.handle) } : {}),
      })
      .then((p) => {
        setBusy(false);
        onShared(p);
      })
      .catch((e) => {
        setBusy(false);
        if (e instanceof ApiError && e.status === 409) setProblem(words.shareConflict);
        else if (e instanceof ApiError && e.status === 403) setProblem(defaultCopy.noPermission);
        else setProblem(e instanceof ApiError ? e.message : words.shareFailed);
      });
  };

  const inThread = thread.participants.filter((p) => p.kind === "person").map((p) => p.name);
  const names = channels?.map((c) => c.name) ?? [];
  if (!names.includes("general")) names.unshift("general");

  return (
    <section
      role="dialog"
      aria-labelledby={heading}
      onKeyDown={(e) => {
        if (e.key === "Escape") {
          e.preventDefault();
          onClose();
        }
      }}
      className="warp-glass mx-auto flex w-full max-w-4xl flex-col gap-3 rounded-aui-lg p-4"
    >
      <h2 id={heading} className="text-aui-base font-semibold text-aui-fg">
        {words.shareTitle}
      </h2>
      <p className="text-aui-xs text-aui-muted">{words.shareLede}</p>
      <form
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          share();
        }}
      >
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Label text={words.shareProject} hint={hasRuns ? words.shareProjectFixed : undefined}>
            <select ref={first} className={field} value={project} disabled={hasRuns} onChange={(e) => setProject(e.target.value)}>
              <option value="">{workspaceName || defaultCopy.projects.workspace}</option>
              {projects
                .filter((p) => p.name !== "")
                .map((p) => (
                  <option key={p.name} value={p.name}>
                    {p.title || p.name}
                  </option>
                ))}
            </select>
          </Label>
          <Label text={words.shareChannel}>
            <select className={field} value={channel} onChange={(e) => setChannel(e.target.value)}>
              {names.map((n) => (
                <option key={n} value={n}>
                  {words.channel(n)}
                </option>
              ))}
            </select>
          </Label>
        </div>
        <div className="flex flex-col gap-1">
          <span className="text-aui-xs font-medium text-aui-muted">{words.shareAdd}</span>
          <PeopleToAdd value={people} leaveOut={[me, ...inThread]} onChange={setPeople} />
        </div>
        <Problem message={problem} />
        <div className="flex flex-wrap gap-2">
          <button type="submit" className={primary} disabled={busy}>
            {words.share}
          </button>
          <button type="button" className={button} onClick={onClose}>
            {defaultCopy.projects.cancel}
          </button>
        </div>
      </form>
    </section>
  );
}
