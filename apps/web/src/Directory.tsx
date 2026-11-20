import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { ApiError, api, type Person } from "./api.ts";
import { button, Page, Problem, rows, SearchField, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.directory;

/**
 * People: everyone in the workspace, by name and handle with their own picture
 * and their role. Agents and projects have pages of their own.
 */
export function DirectoryPage({ can }: { can: (permission: string) => boolean }) {
  return (
    <Page title={words.title} lede={words.lede}>
      {can("people:read") ? (
        <People />
      ) : (
        <p className="text-aui-sm text-aui-muted">{words.empty.people}</p>
      )}
    </Page>
  );
}

/** A person's picture: their own photo, or a still, neutral circle. Never a letter in a box. */
function Face({ person, size = 32 }: { person: Person; size?: number }) {
  if (person.picture) {
    return (
      <img
        src={person.picture}
        alt=""
        width={size}
        height={size}
        className="shrink-0 rounded-full object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      aria-hidden
      className="shrink-0 rounded-full bg-aui-hover"
      style={{ width: size, height: size }}
    />
  );
}

function People() {
  const [q, setQ] = useState("");
  const [people, setPeople] = useState<Person[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    let live = true;
    const timer = window.setTimeout(() => {
      api
        .people({ q: q.trim() || undefined })
        .then((page) => {
          if (!live) return;
          setPeople(page.people);
          setNext(page.next);
          setProblem(null);
        })
        .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.failed));
    }, 200);
    return () => {
      live = false;
      window.clearTimeout(timer);
    };
  }, [q]);

  const more = () => {
    if (!next) return;
    api
      .people({ q: q.trim() || undefined, before: next })
      .then((page) => {
        setPeople((was) => [...(was ?? []), ...page.people]);
        setNext(page.next);
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.failed));
  };

  return (
    <section className="flex flex-col gap-4" aria-label={words.tabs.people}>
      <SearchField label={words.search} value={q} onChange={setQ} />
      <Problem message={problem} />
      {people === null ? (
        <SkeletonRows count={5} />
      ) : people.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">
          {q.trim() ? words.noMatch : words.empty.people}
        </p>
      ) : (
        <ul className={rows}>
          {people.map((p) => (
            <li key={p.handle} className="flex items-center gap-3 py-2.5">
              <Face person={p} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-aui-sm text-aui-fg">
                  {p.name || words.handle(p.handle)}
                </span>
                <span className="block truncate text-aui-xs text-aui-muted">
                  {words.handle(p.handle)} | {words.roles(p.roles)}
                  {p.projects && p.projects.length > 0 && (
                    <span className="ml-3">
                      {words.personProjects(
                        p.projects.map((proj) => proj.title || proj.name),
                        p.projects_more ?? 0,
                      )}
                    </span>
                  )}
                </span>
              </span>
            </li>
          ))}
        </ul>
      )}
      {next && (
        <button type="button" className={`${button} self-start`} onClick={more}>
          {words.more}
        </button>
      )}
    </section>
  );
}
