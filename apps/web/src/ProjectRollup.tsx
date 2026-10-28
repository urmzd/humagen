import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type ProjectRollup as Rollup } from "./api.ts";
import { href } from "./route.ts";
import { Problem } from "./ui.tsx";

const words = defaultCopy.projectRollup;
const link =
  "inline-flex min-h-11 items-center text-aui-sm text-aui-accent underline-offset-4 hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent";

/** Read-only rollups always link to the original project for review and changes. */
export function ProjectRollup({ project, signal = 0 }: { project: string; signal?: number }) {
  const [data, setData] = useState<Rollup | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let current = true;
    setData(null);
    setProblem(null);
    api
      .projectRollup(project)
      .then((next) => {
        if (current) setData(next);
      })
      .catch(() => {
        if (current) setProblem(words.failed);
      });
    return () => {
      current = false;
    };
  }, [project, signal]);
  return (
    <section className="flex min-w-0 flex-col gap-6" aria-label={words.title}>
      <Problem message={problem} />
      {!data && !problem && (
        <p role="status" className="text-aui-sm text-aui-muted">
          {words.loading}
        </p>
      )}
      {data && (
        <>
          <p className="max-w-prose text-aui-sm text-aui-muted">{words.inherited}</p>
          <nav className="flex flex-wrap gap-4" aria-label={words.manage}>
            <a className={link} href={href({ page: "project", name: project, tab: "context" })}>
              {words.context}
            </a>
            <a className={link} href={href({ page: "project", name: project, tab: "people" })}>
              {words.manage}
            </a>
          </nav>
          <div>
            <h2 className="mb-3 text-aui-sm font-semibold">{words.children}</h2>
            {data.projects.length === 0 && (
              <p className="text-aui-sm text-aui-muted">{words.emptyProjects}</p>
            )}
            <ul className="divide-y divide-aui-border">
              {data.projects.map((row) => (
                <li key={row.name} className="flex min-w-0 flex-col gap-3 py-4">
                  <a
                    className={`${link} font-semibold break-words`}
                    href={href({ page: "project", name: row.name, tab: "overview" })}
                  >
                    {row.title}
                  </a>
                  <dl className="flex flex-wrap gap-x-5 gap-y-2 text-aui-xs">
                    {[
                      [words.active, row.active],
                      [words.conversations, row.conversations],
                      [words.unread, row.unread],
                      [words.attention, row.needs],
                      [words.runs, row.runs_30_days],
                    ].map(([label, value]) => (
                      <div key={label}>
                        <dt className="text-aui-muted">{label}</dt>
                        <dd className="tabular-nums">{value}</dd>
                      </div>
                    ))}
                  </dl>
                  <a
                    className={link}
                    href={href({ page: "project", name: row.name, tab: "inbox" })}
                  >
                    {words.review}
                  </a>
                </li>
              ))}
            </ul>
          </div>
          <div>
            <h2 className="mb-3 text-aui-sm font-semibold">{words.notes}</h2>
            {data.notes.length === 0 && (
              <p className="text-aui-sm text-aui-muted">{words.emptyNotes}</p>
            )}
            <ul className="divide-y divide-aui-border">
              {data.notes.map((note) => (
                <li key={`${note.project}:${note.id}`} className="flex min-w-0 flex-col gap-2 py-3">
                  <span className="text-aui-xs text-aui-muted">
                    {note.kind === "risk" ? words.risk : words.decision}
                  </span>
                  <p className="whitespace-pre-wrap break-words text-aui-sm">{note.text}</p>
                  <a
                    className={link}
                    href={href({ page: "project", name: note.project, tab: "overview" })}
                  >
                    {note.project}
                  </a>
                </li>
              ))}
            </ul>
          </div>
          <div>
            <h2 className="mb-3 text-aui-sm font-semibold">{words.needs}</h2>
            {data.needs.length === 0 && (
              <p className="text-aui-sm text-aui-muted">{words.emptyNeeds}</p>
            )}
            <ul className="divide-y divide-aui-border">
              {data.needs.map((need) => (
                <li key={need.id} className="flex min-w-0 flex-col gap-2 py-3">
                  <p className="break-words text-aui-sm">{need.subject}</p>
                  <a
                    className={link}
                    href={href({
                      page: "project",
                      name: need.project,
                      tab: "inbox",
                      message: need.id,
                    })}
                  >
                    {words.review}: {need.project}
                  </a>
                </li>
              ))}
            </ul>
          </div>
          {(data.more_projects || data.more_notes || data.more_needs) && (
            <p className="text-aui-sm text-aui-muted">{words.more}</p>
          )}
        </>
      )}
    </section>
  );
}
