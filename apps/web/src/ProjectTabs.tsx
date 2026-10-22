import { ActionId, isAnnouncement, messageState, type UiAction } from "@warp/core";
import { defaultCopy } from "@warp/components";
import { BanIcon, buttonClass } from "@warp/render-web";
import { type MailboxShow } from "@warp/web";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import {
  ApiError,
  api,
  type PolicyScope,
  type ProjectChange,
  type ProjectEntry,
  type ProjectMember,
  type ProjectMembers,
  type ProjectRole,
  type ProjectView,
} from "./api.ts";
import { ScopeLimits } from "./Billing.tsx";
import { useHeld, useInbox } from "./inbox.ts";
import { KindChips, MailboxView } from "./InboxPage.tsx";
import { type PlatformMessage, projectInbox, projectOf, type Source } from "./mail.ts";
import { ago, button, Empty, field, Label, Menu, Problem, primary, SkeletonRows, Switch } from "./ui.tsx";
import { titleOf, updateOf } from "./projects.ts";

const copy = defaultCopy;
const section = "flex flex-col gap-2 border-aui-border border-t pt-5 first:border-t-0 first:pt-0";
const heading = "text-aui-base font-medium text-aui-fg";
const sorry = (fallback: string) => (e: unknown) => (e instanceof ApiError ? e.message : fallback);

const roleOptions = (["viewer", "member", "owner"] as const).map((r) => ({ value: r, label: copy.roles[r] }));

/** A control in the reading pane's header, beside Snooze and Hide for Me. */
const paneButton = `${buttonClass("ghost")} h-7 px-2`;

/**
 * A project's inbox: what is filed in it, and its announcements, which reach
 * the members of the project and of every project below it. Those allowed post
 * here, and the author or an owner withdraws what was posted, from the
 * announcement itself. The inbox and its Open, Resolved and All switch stay
 * whatever is open, so what the reader hid is always found under Resolved.
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
  // A linked message is read even when Open does not list it, as one the reader hid.
  const { messages, keep } = useHeld(inbox.messages, message, inbox.loading === "first", via);
  const [draft, setDraft] = useState({ subject: "", text: "", warning: false });
  const [writing, setWriting] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const may = project.actions.includes("announce");
  const owner = project.access.role === "owner";
  // Its author or an owner withdraws an announcement still standing for
  // everyone: one this reader only hid for themselves still is. An agent's
  // draft was never announced; its owners publish or discard it. An owner
  // here owns every project below, so one filed below is theirs to withdraw
  // too, in the project it was filed in.
  const withdrawable = (m: PlatformMessage) =>
    m.kind === "announcement" &&
    isAnnouncement(m) &&
    project.actions.length > 0 &&
    (messageState(m) === "open" || Boolean(m.hidden)) &&
    (owner || (m.from.kind === "person" && m.sender === me));
  const withdraw = (m: PlatformMessage) =>
    api
      .withdrawAnnouncement(projectOf(m), m.id)
      .then(() => {
        onMessage(null);
        setPosted((n) => n + 1);
      })
      .catch((err) => setProblem(sorry("It could not be withdrawn.")(err)));

  const post = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    api
      .postAnnouncement(project.name, {
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

  // Leaving out the projects below closes a message filed in one of them.
  const showChildren = (next: boolean) => {
    setChildren(next);
    const reading = messages.find((m) => m.id === message);
    if (!next && reading && projectOf(reading) !== project.name) onMessage(null);
  };

  const onAction = (action: UiAction) => {
    const m = messages.find((x) => x.id === action.payload.turnId);
    if (!m) return;
    inbox.act(m, action).then(keep);
    if (action.id === ActionId.dismiss && message === m.id) onMessage(null);
  };

  return (
    <div className="flex min-h-[28rem] flex-col gap-3">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <Switch label={copy.announcements.includeSubProjects} checked={children} onChange={showChildren} />
        {may && !writing && (
          <button type="button" className={primary} onClick={() => setWriting(true)}>
            {copy.announcements.post}
          </button>
        )}
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
      ) : inbox.loading === "first" && messages.length === 0 ? (
        <SkeletonRows count={4} height="3rem" className="max-w-md" />
      ) : (
        <MailboxView
          messages={messages}
          selectedId={message ?? null}
          onSelect={onMessage}
          onAction={onAction}
          onOpen={onOpen}
          viewer={me}
          show={show}
          onShow={setShow}
          // Announcements land here, and what the reader hid of them is under Resolved.
          // A chosen kind of anything else keeps the mailbox's own words.
          emptyOpen={
            kind === "" || kind === "announcement" ? { title: copy.mailbox.emptyOpen, hint: copy.announcements.emptyOpenHint } : undefined
          }
          messageActions={(m) =>
            withdrawable(m) && (
              <button type="button" className={paneButton} onClick={() => withdraw(m)}>
                <BanIcon size={14} />
                {copy.announcements.withdraw}
              </button>
            )
          }
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

/**
 * Who a member entry is, in words: a person by what the platform calls them
 * (`name`), never by the subject the entry keeps them by. An entry typed in
 * here and not saved yet has no name, and reads as it was typed.
 */
function whoOf(entry: { who: string; name?: string }): string {
  const { who } = entry;
  if (who === "*") return copy.roles.everyone;
  if (who.startsWith("role:")) return `Everyone who holds ${who.slice(5)}`;
  return entry.name || who;
}

/** A project above this one by its title, as its breadcrumb names it. */
export function aboveTitle(project: Pick<ProjectView, "path">, name: string): string {
  if (name === "") return copy.project.theWorkspace;
  const step = project.path.find((s) => s.name === name);
  return step ? titleOf(step) : name;
}

/**
 * Who holds a role in a project: its members, the owners of the projects
 * above it, and, for an owner, a way to change them. Without an entry for
 * everyone it is members only; a company-wide project keeps everyone as at
 * least a member. An owner of an account everyone can see is asked whether
 * to make it members only, and nothing is closed for them. Under a project
 * everyone owns, ownership flows down, so everyone sees it: nothing offers to
 * close it, and the tab says where to name owners first.
 */
export function ProjectPeople({ project, onChanged }: { project: ProjectView; onChanged: () => void }) {
  const [members, setMembers] = useState<ProjectMembers | null>(null);
  const [draft, setDraft] = useState<ProjectMember[] | null>(null);
  const [who, setWho] = useState("");
  const [role, setRole] = useState<ProjectRole>("member");
  const [problem, setProblem] = useState<string | null>(null);
  const editable = project.actions.includes("edit") && project.managed !== "file";
  const name = project.name;

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
  // Everyone owning a project above owns this one too, so it stays open.
  const ownedAbove = members.inherited.find((o) => o.who === "*");
  const open = shown.some((m) => m.who === "*");
  const company = project.kind === "company" || project.name === "";
  const keepsEveryone = company || ownedAbove !== undefined;
  const nudge = editable && open && !everyoneOwns && !ownedAbove && project.kind === "account" && !draft;
  const change = (next: ProjectMember[]) => setDraft(next);
  // A change names who holds a role by the entry alone: what a person is
  // called is the platform's to say.
  const save = () =>
    api
      .setProjectMembers(
        name,
        members.version,
        shown.map(({ who, role }): ProjectEntry => ({ who, role })),
      )
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
      {!open && <p className="max-w-xl text-aui-sm text-aui-muted">{copy.project.membersOnly}</p>}
      {ownedAbove && !everyoneOwns && (
        <p className="max-w-xl text-aui-sm text-aui-muted">
          {copy.project.everyoneOwnsAbove(aboveTitle(project, ownedAbove.from))}
        </p>
      )}
      {nudge && (
        <p className="flex max-w-xl flex-wrap items-center gap-3 text-aui-sm text-aui-muted">
          {copy.project.visibleToEveryone(titleOf(project))}
          <button type="button" className={button} onClick={() => change(shown.filter((m) => m.who !== "*"))}>
            {copy.project.makeMembersOnly}
          </button>
        </p>
      )}
      <section className={section}>
        <h2 className={heading}>{copy.project.members}</h2>
        <ul>
          {shown.map((m) => (
            <li key={m.who} className="flex flex-wrap items-center justify-between gap-3 border-aui-border border-t py-2 first:border-t-0">
              <span className="min-w-0 text-aui-sm text-aui-fg">{whoOf(m)}</span>
              <span className="flex items-center gap-2">
                {editable ? (
                  <Menu
                    label={`${copy.project.role}: ${whoOf(m)}`}
                    value={m.role}
                    options={roleOptions}
                    onChange={(r) => change(shown.map((x) => (x.who === m.who ? { ...x, role: r } : x)))}
                  />
                ) : (
                  <span className="text-aui-xs text-aui-muted">{copy.roles[m.role]}</span>
                )}
                {editable && (m.who !== "*" || !keepsEveryone) && (
                  <button type="button" className={button} onClick={() => change(shown.filter((x) => x.who !== m.who))}>
                    {copy.project.remove}
                  </button>
                )}
              </span>
            </li>
          ))}
        </ul>
        {editable && company && <p className="text-aui-xs text-aui-muted">{copy.project.everyoneNeeded}</p>}
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
                  .updateProject(name, { ...updateOf(project, {}), announce })
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
                <span className="min-w-0 text-aui-sm text-aui-fg">{whoOf(o)}</span>
                <span className="text-aui-xs text-aui-muted">{copy.roles.ownerFrom(aboveTitle(project, o.from))}</span>
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
  const name = project.name;
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
              {c.by_name || c.by}, {ago(c.at)}
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
 * above it ("Field's limit also applies here"), and, for an owner of a
 * project above, a tighter limit of their own. A reader who may do neither
 * is told who may.
 */
export function ProjectLimits({
  project,
  scope,
  chain,
  canBilling,
  above,
  tick,
}: {
  project: ProjectView;
  /** The project's own scope: the tenant's for the workspace. */
  scope: PolicyScope;
  /** The limits above it, in words. */
  chain: { scope: PolicyScope; words: string }[];
  canBilling: boolean;
  /** The project just above, by title, whose owners may set a tighter limit. None for the workspace. */
  above?: string;
  tick?: number;
}) {
  const [runs, setRuns] = useState("");
  const [period, setPeriod] = useState<"day" | "month">("day");
  const [problem, setProblem] = useState<string | null>(null);
  const [note, setNote] = useState<string | null>(null);
  const [reads, setReads] = useState(0);
  return (
    <div className="flex flex-col gap-6">
      <section className={section} aria-label={copy.project.tabs.limits}>
        <ScopeLimits key={`${project.name}:${reads}`} scope={scope} canWrite={canBilling} tick={tick} chain={chain} />
        {!canBilling && !project.actions.includes("limits") && (
          <p className="max-w-xl text-aui-xs text-aui-muted">{copy.project.limitsReadOnly(above)}</p>
        )}
      </section>
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
                .setProjectLimit(project.name, { period, max_runs: Number(runs), action: "block" })
                .then(() => {
                  setNote(`At most ${runs} runs each ${period}.`);
                  setReads((n) => n + 1);
                })
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
                  .deleteProjectLimit(project.name)
                  .then(() => {
                    setNote(null);
                    setReads((n) => n + 1);
                  })
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
