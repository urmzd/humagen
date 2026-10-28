import { defaultCopy } from "@humagen/components";
import { Inspector } from "@humagen/web";
import { type KeyboardEvent, useEffect, useId, useRef, useState } from "react";
import {
  ApiError,
  type Channel,
  coworking,
  type DirectoryPerson,
  type ProjectView,
  type Thread,
  type ThreadShared,
} from "./api.ts";
import { conflictOf } from "./conflicts.ts";
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
    .filter(
      (p) =>
        !leaveOut.includes(p.subject) &&
        !leaveOut.includes(p.handle) &&
        !value.some((v) => v.subject === p.subject),
    )
    .slice(0, 8);
  const listed = open && shown.length > 0;
  const current = Math.min(active, shown.length - 1);

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
      const p = shown[current];
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
            <li
              key={p.subject}
              className="inline-flex h-7 max-w-full items-center gap-1 rounded-full border border-aui-border pr-1 pl-2.5 text-aui-xs text-aui-fg"
            >
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
          aria-activedescendant={listed ? `${list}-${current}` : undefined}
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
                aria-selected={i === current}
                onMouseDown={(e) => {
                  e.preventDefault();
                  pick(p);
                }}
                className="flex cursor-pointer items-baseline gap-2 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg aria-selected:bg-aui-hover"
              >
                <span className="shrink-0 font-medium">@{p.handle}</span>
                {p.name && (
                  <span className="min-w-0 truncate text-aui-xs text-aui-muted">{p.name}</span>
                )}
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
  open = true,
  thread,
  hasRuns,
  projects,
  workspaceName,
  me,
  handle,
  onShared,
  onPublished,
  onClose,
}: {
  open?: boolean;
  thread: Thread;
  /** Completed work requires an explicit audience review. */
  hasRuns: boolean;
  /** The projects the reader may share into. */
  projects: ProjectView[];
  workspaceName: string;
  me: string;
  /** How the platform names the reader, whom the people to add leave out. */
  handle?: string;
  /** The thread as shared, and the people asked to be added, of whom the page's `not_added` were not. */
  onShared: (page: ThreadShared, asked: string[]) => void;
  onPublished?: (thread: Thread) => void;
  onClose: () => void;
}) {
  const [project, setProject] = useState(thread.project);
  // The server refused a move (409): the project stays where it is.
  const [fixed, setFixed] = useState(false);
  const [preview, setPreview] = useState<Awaited<ReturnType<typeof coworking.placement>> | null>(
    null,
  );
  const [reviewed, setReviewed] = useState(false);
  const [summary, setSummary] = useState("");
  const [previewFailed, setPreviewFailed] = useState(false);
  const [reviewVersion, setReviewVersion] = useState(0);
  const [channels, setChannels] = useState<Channel[] | null>(null);
  // The channel it is filed in, until another project is picked, where general is.
  const [channel, setChannel] = useState(thread.channel || "general");
  const [people, setPeople] = useState<DirectoryPerson[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const _heading = useId();
  const form = useRef<HTMLFormElement>(null);

  // The first control that can change takes focus, so Escape closes from it.
  useEffect(() => {
    form.current
      ?.querySelector<HTMLElement>("select:not(:disabled), input:not(:disabled)")
      ?.focus();
  }, []);

  useEffect(() => {
    let live = true;
    setChannel(project === thread.project ? thread.channel || "general" : "general");
    setChannels(null);
    coworking
      .channels(project)
      .then((cs) => live && setChannels(cs.filter((c) => !c.archived)))
      .catch(() => live && setChannels(null));
    return () => {
      live = false;
    };
  }, [project]);

  useEffect(() => {
    if (!hasRuns) return;
    let live = true;
    setPreview(null);
    setReviewed(false);
    setPreviewFailed(false);
    coworking
      .placement(thread.id, project, channel)
      .then((next) => {
        if (live) setPreview(next);
      })
      .catch(() => {
        if (live) setPreviewFailed(true);
      });
    return () => {
      live = false;
    };
  }, [hasRuns, thread.id, thread.version, project, channel, reviewVersion]);

  const share = () => {
    if (
      hasRuns &&
      (!preview || !reviewed || (preview.summary_required && (!summary.trim() || !onPublished)))
    )
      return;
    setProblem(null);
    setBusy(true);
    if (preview?.summary_required) {
      coworking
        .publishSummary(thread.id, {
          project: project || "~",
          channel,
          summary: summary.trim(),
          placement_token: preview.token,
        })
        .then((next) => {
          setBusy(false);
          onPublished?.(next);
        })
        .catch((error) => {
          setBusy(false);
          if (error instanceof ApiError && error.status === 409) {
            setReviewed(false);
            setReviewVersion((n) => n + 1);
            setProblem(words.placementChanged);
          } else
            setProblem(
              error instanceof ApiError && error.status === 403
                ? defaultCopy.noPermission
                : words.shareFailed,
            );
        });
      return;
    }
    coworking
      .share(thread.id, {
        ...(preview ? { placement_token: preview.token } : {}),
        ...(project !== thread.project ? { project: project === "" ? "~" : project } : {}),
        channel,
        ...(people.length > 0 ? { add: people.map((p) => p.handle) } : {}),
      })
      .then((p) => {
        setBusy(false);
        onShared(
          p,
          people.map((x) => x.handle),
        );
      })
      .catch((e) => {
        setBusy(false);
        // Each refusal in the console's own words: a run fixes the project
        // and the channel, and someone else may have changed it meanwhile.
        if (hasRuns && e instanceof ApiError && e.status === 409) {
          setReviewed(false);
          setReviewVersion((n) => n + 1);
          setProblem(words.placementChanged);
          return;
        }
        const why = conflictOf(e);
        if (why === "project" || why === "channel") {
          setProblem(why === "project" ? words.shareConflict : words.shareChannelFixed);
          setProject(thread.project);
          setChannel(thread.channel || "general");
          setFixed(true);
        } else if (why === "version") setProblem(words.changedMeanwhile);
        else if (e instanceof ApiError && e.status === 403) setProblem(defaultCopy.noPermission);
        else setProblem(words.shareFailed);
      });
  };

  const inThread = thread.participants.filter((p) => p.kind === "person").map((p) => p.name);
  const names = channels?.map((c) => c.name) ?? [];
  if (!names.includes("general")) names.unshift("general");
  if (project === thread.project && thread.channel && !names.includes(thread.channel))
    names.push(thread.channel);

  return (
    <Inspector
      open={open}
      title={words.shareTitle}
      onClose={onClose}
      footer={
        <>
          <button type="button" className={button} onClick={onClose}>
            {defaultCopy.projects.cancel}
          </button>
          <button
            type="button"
            className={primary}
            disabled={
              busy ||
              (hasRuns &&
                (!preview ||
                  !reviewed ||
                  (preview.summary_required && (!summary.trim() || !onPublished))))
            }
            onClick={() => form.current?.requestSubmit()}
          >
            {preview?.summary_required ? words.publishSummary : words.share}
          </button>
        </>
      }
    >
      <p className="text-aui-xs text-aui-muted">{words.shareLede}</p>
      <form
        ref={form}
        className="flex flex-col gap-3"
        onSubmit={(e) => {
          e.preventDefault();
          share();
        }}
      >
        <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
          <Label text={words.shareProject} hint={fixed ? words.shareProjectFixed : undefined}>
            <select
              className={field}
              value={project}
              disabled={fixed}
              onChange={(e) => setProject(e.target.value)}
            >
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
            <select
              className={field}
              value={channel}
              disabled={fixed}
              onChange={(e) => setChannel(e.target.value)}
            >
              {names.map((n) => (
                <option key={n} value={n}>
                  {words.channel(n)}
                </option>
              ))}
            </select>
          </Label>
        </div>
        {hasRuns && (
          <section
            aria-live="polite"
            className="flex flex-col gap-3 rounded-aui border border-aui-border p-3"
          >
            {preview ? (
              <>
                <p className="text-aui-sm text-aui-fg">
                  {words.placementContents(preview.audience, preview.runs)}
                </p>
                {preview.exclusions.length > 0 && (
                  <ul className="list-disc pl-5 text-aui-xs text-aui-muted">
                    {preview.exclusions.map((item) => (
                      <li key={item}>{item}</li>
                    ))}
                  </ul>
                )}
                {preview.summary_required && (
                  <Label text={words.publicationSummary} hint={words.publicationSummaryHint}>
                    <textarea
                      className={field}
                      rows={5}
                      maxLength={16000}
                      value={summary}
                      onChange={(event) => {
                        setSummary(event.target.value);
                        setReviewed(false);
                      }}
                    />
                  </Label>
                )}
                <label className="flex items-start gap-2 text-aui-sm text-aui-fg">
                  <input
                    type="checkbox"
                    checked={reviewed}
                    onChange={(event) => setReviewed(event.target.checked)}
                    className="mt-1 size-4 shrink-0"
                  />
                  {words.placementReview}
                </label>
              </>
            ) : previewFailed ? (
              <>
                <Problem message={words.placementFailed} />
                <button
                  type="button"
                  className={button}
                  onClick={() => setReviewVersion((n) => n + 1)}
                >
                  {words.placementRetry}
                </button>
              </>
            ) : (
              <p className="text-aui-xs text-aui-muted">{words.placementLoading}</p>
            )}
          </section>
        )}
        {!preview?.summary_required && (
          <div className="flex flex-col gap-1">
            <span className="text-aui-xs font-medium text-aui-muted">{words.shareAdd}</span>
            <PeopleToAdd
              value={people}
              leaveOut={[me, ...(handle ? [handle] : []), ...inThread]}
              onChange={setPeople}
            />
          </div>
        )}
        <Problem message={problem} />
      </form>
    </Inspector>
  );
}
