import { defaultCopy } from "@humagen/components";
import { SearchIcon, UserPlusIcon, XIcon } from "@humagen/render-web";
import { buttonVariants } from "@humagen/render-web/ui/button";
import { type FormEvent, useEffect, useId, useState } from "react";
import { api, coworking, type DirectoryPerson, type Thread } from "./api.ts";
import { PersonMark } from "./PersonMark.tsx";
import { button, Confirm, field, Problem, primary } from "./ui.tsx";

const words = defaultCopy.conversationAccess;

/**
 * Who is in a conversation: a search to add people from the workspace's
 * directory, then the people already in, each with their mark. Agents
 * are not invited: a conversation's agents are the ones its project turns on,
 * so they are not listed here. Participation is narrower than project roles;
 * the server owns both checks.
 */
export function ConversationAccess({
  thread,
  onChanged,
}: {
  thread: Thread;
  onChanged?: (thread: Thread) => void;
}) {
  const [current, setCurrent] = useState(thread);
  const [handle, setHandle] = useState("");
  const [myHandle, setMyHandle] = useState("");
  const [write, setWrite] = useState(false);
  const [manage, setManage] = useState(false);
  const [saving, setSaving] = useState(false);
  const [remove, setRemove] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [found, setFound] = useState<DirectoryPerson[] | null>(null);
  const searchId = useId();
  useEffect(() => setCurrent(thread), [thread]);
  // The directory, searched as the person types; nothing is read for an empty field.
  const typed = handle.trim().replace(/^@/, "");
  useEffect(() => {
    if (!typed) {
      setFound(null);
      return;
    }
    let live = true;
    const wait = setTimeout(() => {
      coworking.people(typed, 6).then(
        (people) => live && setFound(people),
        () => live && setFound([]),
      );
    }, 150);
    return () => {
      live = false;
      clearTimeout(wait);
    };
  }, [typed]);
  useEffect(() => {
    let active = true;
    Promise.all([api.me(), api.project(thread.project)])
      .then(([me, project]) => {
        if (!active) return;
        const mine = me.handle || me.principal.subject;
        setMyHandle(mine);
        const mayWrite =
          me.permissions.includes("runs:write") &&
          (project.project.access.role === "owner" || project.project.access.role === "member") &&
          (thread.visibility === "shared" ||
            thread.participants.some(
              (participant) => participant.kind === "person" && participant.name === mine,
            ));
        setWrite(mayWrite);
        setManage(
          mayWrite && (thread.created_by === mine || project.project.access.role === "owner"),
        );
      })
      .catch(() => {
        if (active) {
          setWrite(false);
          setManage(false);
        }
      });
    return () => {
      active = false;
    };
  }, [thread]);
  const keep = (next: Thread) => {
    setCurrent(next);
    onChanged?.(next);
    setProblem(null);
  };
  const change = (operation: Promise<Thread>) => {
    setSaving(true);
    setProblem(null);
    operation
      .then(keep)
      .catch((error: unknown) => setProblem(error instanceof Error ? error.message : words.failed))
      .finally(() => setSaving(false));
  };
  const people = current.participants.filter((person) => person.kind === "person");
  const isIn = (name: string) => people.some((person) => person.name === name);
  const invite = (name: string) =>
    change(
      coworking
        .participants(current.id, {
          version: current.version,
          add: [{ kind: "person", name }],
        })
        .then((next) => {
          setHandle("");
          return next;
        }),
    );
  const add = (event: FormEvent) => {
    event.preventDefault();
    if (typed) invite(typed);
  };
  return (
    <div className="flex min-w-0 flex-col gap-5">
      {write && (
        // A search of the workspace's people; Invite adds the handle as typed.
        <form className="flex min-w-0 flex-col gap-2" onSubmit={add}>
          <label htmlFor={searchId} className="font-medium text-aui-fg text-aui-sm">
            {words.handle}
          </label>
          <div className="flex min-w-0 items-center gap-2">
            <div className="relative min-w-0 flex-1">
              <SearchIcon
                size={16}
                aria-hidden
                className="-translate-y-1/2 pointer-events-none absolute top-1/2 left-2.5 text-aui-muted"
              />
              <input
                id={searchId}
                type="search"
                className={`${field} w-full pl-8`}
                value={handle}
                onChange={(event) => setHandle(event.target.value)}
                placeholder={words.search}
                autoComplete="off"
                maxLength={200}
                required
              />
            </div>
            <button type="submit" className={primary} disabled={saving || !typed}>
              <UserPlusIcon size={16} aria-hidden />
              {words.add}
            </button>
          </div>
          {found && (
            <ul aria-label={words.search} className="flex flex-col">
              {found.length === 0 ? (
                <li className="px-2 py-2 text-aui-muted text-aui-xs">{words.noMatch}</li>
              ) : (
                found.map((person) => {
                  const added = isIn(person.handle);
                  return (
                    <li key={person.subject}>
                      <button
                        type="button"
                        disabled={saving || added}
                        onClick={() => invite(person.handle)}
                        className="flex w-full min-w-0 items-center gap-3 rounded-aui px-2 py-2 text-left hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:hover:bg-transparent"
                      >
                        <PersonMark name={person.name || person.handle} size={28} />
                        <span className="flex min-w-0 flex-1 flex-col">
                          <span className="truncate text-aui-fg text-aui-sm">
                            {person.name || person.handle}
                          </span>
                          <span className="truncate text-aui-muted text-aui-xs">
                            @{person.handle}
                          </span>
                        </span>
                        {added ? (
                          <span className="text-aui-muted text-aui-xs">{words.added}</span>
                        ) : (
                          <UserPlusIcon size={16} aria-hidden className="text-aui-muted" />
                        )}
                      </button>
                    </li>
                  );
                })
              )}
            </ul>
          )}
        </form>
      )}
      <section className="flex flex-col gap-1">
        <h3 className="font-medium text-aui-muted text-aui-xs">{words.inHere(people.length)}</h3>
        <ul aria-label={words.people} className="flex flex-col">
          {people.map((person) => (
            <li
              key={`${person.kind}:${person.name}`}
              className="flex min-w-0 flex-wrap items-center gap-3 py-2 text-aui-sm"
            >
              <PersonMark name={person.name} size={28} />
              <span className="min-w-0 flex-1 break-words">
                {person.name}
                {person.name === myHandle && <span className="text-aui-muted"> {words.you}</span>}
              </span>
              {person.owner && <span className="text-aui-muted text-aui-xs">{words.owner}</span>}
              {write &&
                !person.owner &&
                (manage || person.name === myHandle) &&
                (remove === person.name ? (
                  <Confirm
                    question={`${words.remove} ${person.name}?`}
                    action={words.remove}
                    onKeep={() => setRemove(null)}
                    onConfirm={() => {
                      setRemove(null);
                      change(
                        coworking.participants(current.id, {
                          version: current.version,
                          remove: [person.name],
                        }),
                      );
                    }}
                  />
                ) : (
                  <button
                    type="button"
                    aria-label={words.removeOne(person.name)}
                    title={words.removeOne(person.name)}
                    className={buttonVariants({ variant: "ghost", size: "icon-sm" })}
                    disabled={saving}
                    onClick={() => setRemove(person.name)}
                  >
                    <XIcon size={16} aria-hidden />
                  </button>
                ))}
            </li>
          ))}
        </ul>
      </section>
      <p className="text-aui-muted text-aui-xs">
        <span>{current.visibility === "private" ? words.privateHint : words.sharedHint}</span>{" "}
        <span>{words.agentsNote}</span>
      </p>
      {current.fork && <p className="text-aui-muted text-aui-xs">{words.forkHint}</p>}
      {manage && current.visibility !== "private" && (
        <button
          type="button"
          className={`${button} self-start`}
          disabled={saving}
          onClick={() =>
            change(
              coworking.update(current.id, {
                version: current.version,
                title: current.title || "",
                visibility: "private",
                default: current.default || "",
                agents: current.agents,
                mode: current.mode,
              }),
            )
          }
        >
          {words.makePrivate}
        </button>
      )}
      <Problem message={problem} />
    </div>
  );
}
