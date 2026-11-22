import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useState } from "react";
import { api, coworking, type Thread } from "./api.ts";
import { button, Confirm, field, Label, Problem, primary } from "./ui.tsx";

const words = defaultCopy.conversationAccess;

/**
 * Who is in a conversation: its people, and a way to invite another. Agents
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
  useEffect(() => setCurrent(thread), [thread]);
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
  const add = (event: FormEvent) => {
    event.preventDefault();
    change(
      coworking
        .participants(current.id, {
          version: current.version,
          add: [{ kind: "person", name: handle.trim().replace(/^@/, "") }],
        })
        .then((next) => {
          setHandle("");
          return next;
        }),
    );
  };
  return (
    <div className="flex min-w-0 flex-col gap-5">
      <p className="text-aui-sm text-aui-muted">
        {current.visibility === "private" ? words.privateHint : words.sharedHint}
      </p>
      {current.fork && <p className="text-aui-xs text-aui-muted">{words.forkHint}</p>}
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
      <ul aria-label={words.people} className="divide-aui-border divide-y">
        {current.participants
          .filter((person) => person.kind === "person")
          .map((person) => (
            <li
              key={`${person.kind}:${person.name}`}
              className="flex min-w-0 flex-wrap items-center gap-2 py-3 text-aui-sm"
            >
              <span className="min-w-0 flex-1 break-words">{person.name}</span>
              {person.owner && <span className="text-aui-xs text-aui-muted">{words.owner}</span>}
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
                    className={button}
                    disabled={saving}
                    onClick={() => setRemove(person.name)}
                  >
                    {words.remove}
                  </button>
                ))}
            </li>
          ))}
      </ul>
      {write && (
        // One field and its button on one line: a person is invited by their handle.
        <form className="flex min-w-0 items-end gap-2" onSubmit={add}>
          <Label text={words.handle}>
            <input
              className={field}
              value={handle}
              onChange={(event) => setHandle(event.target.value)}
              placeholder="@"
              maxLength={200}
              required
            />
          </Label>
          <button type="submit" className={primary} disabled={saving || !handle.trim()}>
            {words.add}
          </button>
        </form>
      )}
      <p className="text-aui-xs text-aui-muted">{words.agentsNote}</p>
      <Problem message={problem} />
    </div>
  );
}
