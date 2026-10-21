import { ActionId, messageState, type UiAction } from "@warp/core";
import { defaultCopy } from "@warp/components";
import { type MailboxShow } from "@warp/web";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  api,
  type ProjectChange,
  type ProjectDetail,
  type ProjectEntry,
  type ProjectMembers,
  type ProjectRole,
  type ProjectView,
} from "./api.ts";
import { ScopeLimits } from "./Billing.tsx";
import { useInbox } from "./inbox.ts";
import { KindChips, MailboxView } from "./InboxPage.tsx";
import { type PlatformMessage, projectInbox, type Source } from "./mail.ts";
import { ago, button, Empty, field, Label, Menu, Problem, primary, SkeletonRows, Switch } from "./ui.tsx";

const copy = defaultCopy;
const section = "flex flex-col gap-2 border-aui-border border-t pt-5 first:border-t-0 first:pt-0";
const heading = "text-aui-base font-medium text-aui-fg";
const sorry = (fallback: string) => (e: unknown) => (e instanceof ApiError ? e.message : fallback);

/** How a project is named to people: its title, its name, or the workspace's name for the root. */
export function titleOf(p: Pick<ProjectView, "name" | "title">, workspace?: string): string {
  if (p.name === "") return workspace || "Workspace";
  return p.title || p.name;
}

/** A project's name in an address: `~` is the workspace. */
export const addressOf = (name: string) => (name === "" ? "~" : name);

const roleOptions = (["viewer", "member", "owner"] as const).map((r) => ({ value: r, label: copy.roles[r] }));

/**
 * A project's inbox: what is filed in it, and its announcements, which reach
 * the members of the project and of every project below it. Those allowed post
 * here, and the author or an owner withdraws what was posted.
 */
export function ProjectInbox({
  project,
  me,
  signal,
  message,
  onMessage,
  onOpen,
  onChanged,
}: {
  project: ProjectView;
  /** The reader's subject, to tell their own announcements apart. */
  me: string;
  signal: unknown;
  message?: string;
  onMessage: (id: string | null) => void;
  onOpen: (source: Source) => void;
  onChanged: () => void;
}) {
  const [children, setChildren] = useState(false);
  const [kind, setKind] = useState("");
  const [show, setShow] = useState<MailboxShow>("open");
  const [posted, setPosted] = useState(0);
  const via = useMemo(() => projectInbox(project.name, children), [project.name, children]);
  // One value that changes when the console's inbox does or something is posted here.
  // biome-ignore lint/correctness/useExhaustiveDependencies: a new object is the signal
  const changed = useMemo(() => ({}), [signal, posted]);
  const inbox = useInbox(kind, show, changed, via, me);
  const [draft, setDraft] = useState({ subject: "", text: "", warning: false });
  const [writing, setWriting] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const may = project.actions.includes("announce");
  const owner = project.access.role === "owner";
  const selected: PlatformMessage | undefined = inbox.messages.find((m) => m.id === message);
  const withdrawable =
    selected?.kind === "announcement" &&
    messageState(selected) !== "resolved" &&
    project.actions.length > 0 &&
    (owner || (selected.from.kind === "person" && selected.from.name === me));

  const post = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    api
      .postAnnouncement(project.name === "" ? "~" : project.name, {
        subject: draft.subject.trim(),
        text: draft.text.trim(),
        severity: draft.warning ? "warning" : "info",
      })
      .then(() => {
        setDraft({ subject: "", text: "", warning: false });
        setWriting(false);
        setPosted((n) => n + 1);
        onChanged();
      })
      .catch((err) =>
        setProblem(err instanceof ApiError && err.status === 429 ? copy.announcements.tooMany(20) : sorry("The announcement could not be posted.")(err)),
      );
  };

  const onAction = (action: UiAction) => {
    const m = inbox.messages.find((x) => x.id === action.payload.turnId);
    if (!m) return;
    inbox.act(m, action);
    if (action.id === ActionId.dismiss && message === m.id) onMessage(null);
  };

  return (
    <div className="flex min-h-[28rem] flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Switch label={copy.announcements.includeSubProjects} checked={children} onChange={setChildren} />
        <div className="flex flex-wrap gap-2">
          {withdrawable && (
            <button
              type="button"
              className={button}
              onClick={() =>
                api
                  .withdrawAnnouncement(addressOf(project.name), selected.id)
                  .then(() => {
                    onMessage(null);
                    setPosted((n) => n + 1);
                  })
                  .catch((err) => setProblem(sorry("It could not be withdrawn.")(err)))
              }
            >
              {copy.announcements.withdraw}
            </button>
          )}
          {may && !writing && (
            <button type="button" className={primary} onClick={() => setWriting(true)}>
              {copy.announcements.post}
            </button>
          )}
        </div>
      </div>
      <Problem message={problem ?? inbox.problem} />
      {writing && (
        <form onSubmit={post} className="flex flex-col gap-3 rounded-aui-lg border border-aui-border p-3">
          <Label text={copy.project.subject}>
            <input
              className={field}
              required
              maxLength={150}
              value={draft.subject}
              onChange={(e) => setDraft({ ...draft, subject: e.target.value })}
            />
          </Label>
          <Label text={copy.project.text}>
            <textarea
              className={`${field} min-h-20`}
              required
              maxLength={4000}
              value={draft.text}
              onChange={(e) => setDraft({ ...draft, text: e.target.value })}
            />
          </Label>
          <Switch label={copy.project.warning} checked={draft.warning} onChange={(warning) => setDraft({ ...draft, warning })} />
          <div className="flex justify-end gap-2">
            <button type="button" className={button} onClick={() => setWriting(false)}>
              Cancel
            </button>
            <button type="submit" className={primary} disabled={!draft.subject.trim() || !draft.text.trim()}>
              {copy.announcements.post}
            </button>
          </div>
        </form>
      )}
      {inbox.failed ? (
        <Empty title="The project's inbox could not be loaded">It will be tried again when something changes.</Empty>
      ) : inbox.loading === "first" && inbox.messages.length === 0 ? (
        <SkeletonRows count={4} height="3rem" className="max-w-md" />
      ) : inbox.messages.length === 0 && show === "open" && !kind ? (
        <p className="text-aui-sm text-aui-muted">{copy.announcements.empty}</p>
      ) : (
        <MailboxView
          messages={inbox.messages}
          selectedId={message ?? null}
          onSelect={onMessage}
          onAction={onAction}
          onOpen={onOpen}
          viewer={me}
          show={show}
          onShow={setShow}
          toolbar={<KindChips value={kind} onChange={setKind} />}
          listEnd={
            inbox.hasMore && (
              <div className="flex justify-center py-3">
                <button type="button" className={button} onClick={inbox.more} disabled={inbox.loading === "more"}>
                  {inbox.loading === "more" ? "Loading" : "See More"}
                </button>
              </div>
            )
          }
        />
      )}
    </div>
  );
}

/** Who a member entry is, in words. */
function whoOf(who: string): string {
  if (who === "*") return copy.roles.everyone;
  if (who.startsWith("role:")) return `Everyone who holds ${who.slice(5)}`;
  return who;
}

/**
 * Who holds a role in a project: its members, the owners of the projects
 * above it, and, for an owner, a way to change them. Everyone keeps at least
 * viewer, since every project stays visible.
 */
export function ProjectPeople({ project, onChanged }: { project: ProjectView; onChanged: () => void }) {
  const [members, setMembers] = useState<ProjectMembers | null>(null);
  const [draft, setDraft] = useState<ProjectEntry[] | null>(null);
  const [who, setWho] = useState("");
  const [role, setRole] = useState<ProjectRole>("member");
  const [problem, setProblem] = useState<string | null>(null);
  const editable = project.actions.includes("edit") && project.managed !== "file";
  const name = addressOf(project.name);

  useEffect(() => {
    api
      .projectMembers(name)
      .then((m) => {
        setMembers(m);
        setDraft(null);
      })
      .catch((err) => setProblem(sorry("Its members could not be loaded.")(err)));
  }, [name]);

  if (!members) return problem ? <Problem message={problem} /> : <SkeletonRows count={3} />;
  const shown = draft ?? members.members;
  const everyoneOwns = shown.some((m) => m.who === "*" && m.role === "owner");
  const change = (next: ProjectEntry[]) => setDraft(next);
  const save = () =>
    api
      .setProjectMembers(name, members.version, shown)
      .then((m) => {
        setMembers(m);
        setDraft(null);
        setProblem(null);
        onChanged();
      })
      .catch((err) => setProblem(sorry("The members could not be saved.")(err)));

  return (
    <div className="flex flex-col gap-6">
      <Problem message={problem} />
      {everyoneOwns && (
        <p className="max-w-xl text-aui-sm text-aui-muted">
          {copy.roles.everyoneOwns}
          {editable ? ` ${copy.roles.nameOwners} below.` : ""}
        </p>
      )}
      <section className={section}>
        <h2 className={heading}>{copy.project.members}</h2>
        <ul>
          {shown.map((m) => (
            <li key={m.who} className="flex flex-wrap items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
              <span className="min-w-0 text-aui-sm text-aui-fg">{whoOf(m.who)}</span>
              <span className="flex items-center gap-2">
                {editable ? (
                  <Menu
                    label={`${copy.project.role}: ${whoOf(m.who)}`}
                    value={m.role}
                    options={roleOptions}
                    onChange={(r) => change(shown.map((x) => (x.who === m.who ? { ...x, role: r } : x)))}
                  />
                ) : (
                  <span className="text-aui-xs text-aui-muted">{copy.roles[m.role]}</span>
                )}
                {editable && m.who !== "*" && (
                  <button type="button" className={button} onClick={() => change(shown.filter((x) => x.who !== m.who))}>
                    {copy.project.remove}
                  </button>
                )}
              </span>
            </li>
          ))}
        </ul>
        {editable && <p className="text-aui-xs text-aui-muted">{copy.project.everyoneNeeded}</p>}
        {editable && (
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              const w = who.trim();
              if (!w || shown.some((m) => m.who === w)) return;
              change([...shown, { who: w, role }]);
              setWho("");
            }}
          >
            <div className="min-w-48 flex-1">
              <Label text={copy.project.who} hint={copy.project.whoHint}>
                <input className={field} value={who} maxLength={255} onChange={(e) => setWho(e.target.value)} />
              </Label>
            </div>
            <Menu label={copy.project.role} value={role} options={roleOptions} onChange={setRole} />
            <button type="submit" className={button} disabled={!who.trim()}>
              {copy.roles.addPerson}
            </button>
          </form>
        )}
        {draft && (
          <div className="flex justify-end gap-2">
            <button type="button" className={button} onClick={() => setDraft(null)}>
              Cancel
            </button>
            <button type="button" className={primary} onClick={save}>
              {copy.project.saveMembers}
            </button>
          </div>
        )}
      </section>
      {editable && (
        <section className={section}>
          <div className="flex flex-wrap items-center justify-between gap-3">
            <h2 className={heading}>{copy.project.whoAnnounces}</h2>
            <Menu
              label={copy.project.whoAnnounces}
              value={project.announce ?? "owner"}
              options={roleOptions}
              onChange={(announce) =>
                api
                  .updateProject(name, {
                    version: project.version,
                    title: project.title,
                    description: project.description,
                    account: project.account,
                    instructions: project.instructions,
                    announce,
                  })
                  .then(onChanged)
                  .catch((err) => setProblem(sorry("That could not be saved.")(err)))
              }
            />
          </div>
        </section>
      )}
      {members.inherited.length > 0 && (
        <section className={section}>
          <h2 className={heading}>{copy.project.ownersAbove}</h2>
          <ul>
            {members.inherited.map((o) => (
              <li key={`${o.from}:${o.who}`} className="flex items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
                <span className="text-aui-sm text-aui-fg">{whoOf(o.who)}</span>
                <span className="text-aui-xs text-aui-muted">{copy.roles.ownerFrom(o.from === "" ? "the workspace" : o.from)}</span>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}

/** What changed on a project, newest first, a page at a time. */
export function ProjectHistory({ project, tick }: { project: ProjectView; tick?: number }) {
  const [changes, setChanges] = useState<ProjectChange[] | null>(null);
  const [next, setNext] = useState<string | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const name = addressOf(project.name);
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when the project changes
  useEffect(() => {
    api
      .projectHistory(name)
      .then((page) => {
        setChanges(page.changes);
        setNext(page.next);
      })
      .catch((err) => setProblem(sorry("Its history could not be loaded.")(err)));
  }, [name, project.version, tick]);
  if (!changes) return problem ? <Problem message={problem} /> : <SkeletonRows count={4} />;
  if (changes.length === 0) return <p className="text-aui-sm text-aui-muted">{copy.project.noHistory}</p>;
  return (
    <div className="flex flex-col gap-3">
      <Problem message={problem} />
      <ol>
        {changes.map((c) => (
          <li key={c.seq} className="flex flex-col gap-0.5 border-aui-border border-t py-2 first:border-t-0">
            <span className="text-aui-sm text-aui-fg">
              {copy.project.what[c.what] ?? c.what}
              {c.agent ? `: ${c.agent}` : ""}
              {c.note ? `, ${c.note}` : ""}
            </span>
            <span className="text-aui-xs text-aui-muted">
              {c.by}, {ago(c.at)}
            </span>
          </li>
        ))}
      </ol>
      {next && (
        <button
          type="button"
          className={`${button} self-center`}
          onClick={() =>
            api
              .projectHistory(name, next)
              .then((page) => {
                setChanges([...changes, ...page.changes]);
                setNext(page.next);
              })
              .catch((err) => setProblem(sorry("Older changes could not be loaded.")(err)))
          }
        >
          {copy.project.older}
        </button>
      )}
    </div>
  );
}

/**
 * The limits that hold a project: its own, which an admin changes, the chain
 * above it, and, for an owner of a project above, a tighter limit of their own.
 */
export function ProjectLimits({
  detail,
  canBilling,
  tick,
  workspace,
}: {
  detail: ProjectDetail;
  canBilling: boolean;
  tick?: number;
  workspace?: string;
}) {
  const project = detail.project;
  const [runs, setRuns] = useState("");
  const [period, setPeriod] = useState<"day" | "month">("day");
  const [problem, setProblem] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const titles = new Map([...detail.ancestors, project].map((p) => [p.name, titleOf(p, workspace)]));
  const above = detail.limits.filter((l) => l.from !== project.name);
  return (
    <div className="flex flex-col gap-6">
      {project.name !== "" && (
        <section className={section}>
          <ScopeLimits key={project.name} scope={{ kind: "project", name: project.name }} canWrite={canBilling} tick={tick} />
        </section>
      )}
      {above.length > 0 && (
        <section className={section}>
          <ul className="flex flex-col gap-1">
            {above.map((l) => (
              <li key={`${l.scope.kind}:${l.from}`} className="text-aui-sm text-aui-muted">
                {l.scope.kind === "tenant" ? copy.project.workspaceLimit : copy.project.limitFrom(titles.get(l.from) ?? l.from)}
              </li>
            ))}
          </ul>
        </section>
      )}
      {project.actions.includes("limits") && !canBilling && (
        <section className={section}>
          <h2 className={heading}>{copy.project.ownerLimit}</h2>
          <p className="max-w-xl text-aui-xs text-aui-muted">{copy.project.ownerLimitHint}</p>
          <Problem message={problem} />
          {note && <p className="text-aui-xs text-aui-muted">{note}</p>}
          <form
            className="flex flex-wrap items-end gap-2"
            onSubmit={(e) => {
              e.preventDefault();
              setProblem(null);
              api
                .setProjectLimit(addressOf(project.name), { period, max_runs: Number(runs) })
                .then(() => setNote(`At most ${runs} runs each ${period}.`))
                .catch((err) => setProblem(sorry("The limit could not be set.")(err)));
            }}
          >
            <Label text={copy.project.maxRuns}>
              <input className={`${field} w-28`} type="number" min={1} required value={runs} onChange={(e) => setRuns(e.target.value)} />
            </Label>
            <Menu
              label={copy.project.period}
              value={period}
              options={[
                { value: "day", label: "Day" },
                { value: "month", label: "Month" },
              ]}
              onChange={setPeriod}
            />
            <button type="submit" className={primary} disabled={!runs}>
              {copy.project.setLimit}
            </button>
            <button
              type="button"
              className={button}
              onClick={() =>
                api
                  .deleteProjectLimit(addressOf(project.name))
                  .then(() => setNote(null))
                  .catch((err) => setProblem(sorry("The limit could not be removed.")(err)))
              }
            >
              {copy.project.removeLimit}
            </button>
          </form>
        </section>
      )}
    </div>
  );
}

/**
 * What every agent answering in a project is sent from it: its instructions,
 * which an owner edits, after those of the projects above.
 */
export function ProjectInstructions({ project, ancestors, workspace, onSaved }: {
  project: ProjectView;
  ancestors: ProjectView[];
  workspace?: string;
  onSaved: () => void;
}) {
  const [text, setText] = useState(project.instructions ?? "");
  const [problem, setProblem] = useState<string | null>(null);
  const editable = project.actions.includes("edit") && project.managed !== "file";
  const inherited = ancestors.filter((a) => a.instructions);
  return (
    <section className={section}>
      <h2 className={heading}>{copy.project.instructions}</h2>
      <p className="max-w-xl text-aui-sm text-aui-muted">{copy.project.instructionsHint}</p>
      <Problem message={problem} />
      {inherited.map((a) => (
        <div key={a.name} className="flex flex-col gap-1 rounded-aui-lg border border-aui-border p-3">
          <span className="text-aui-xs text-aui-muted">From {titleOf(a, workspace)}</span>
          <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{a.instructions}</p>
        </div>
      ))}
      {editable ? (
        <form
          className="flex flex-col gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setProblem(null);
            api
              .updateProject(addressOf(project.name), {
                version: project.version,
                title: project.title,
                description: project.description,
                account: project.account,
                instructions: text,
              })
              .then(onSaved)
              .catch((err) => setProblem(sorry("The instructions could not be saved.")(err)));
          }}
        >
          <textarea
            aria-label={copy.project.instructions}
            className={`${field} min-h-28`}
            maxLength={4000}
            value={text}
            onChange={(e) => setText(e.target.value)}
          />
          <button type="submit" className={`${primary} self-end`} disabled={text === (project.instructions ?? "")}>
            {copy.project.saveInstructions}
          </button>
        </form>
      ) : project.instructions ? (
        <p className="whitespace-pre-wrap text-aui-sm text-aui-fg">{project.instructions}</p>
      ) : (
        <p className="text-aui-sm text-aui-muted">{copy.project.noInstructions}</p>
      )}
    </section>
  );
}
