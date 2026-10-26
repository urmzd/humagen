import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import type {
  AgentSkillRef,
  AgentSkillDefaults as Bindings,
  LibraryScope,
  LibrarySkill,
} from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { field, Problem,button,primary,textLink,Label } from "./ui.tsx";

// The picker only reads the library. Both an unsaved setup and an existing
// agent editor submit its exact references through their own durable APIs.
export function PublishedSkillPicker({
  home,
  refs,
  onChange,
  disabled = false,
}: {
  home: string;
  refs: AgentSkillRef[];
  onChange: (refs: AgentSkillRef[]) => void;
  disabled?: boolean;
}) {
  const copy = defaultCopy.agentSkillDefaults;
  const [scopes, setScopes] = useState<{ key: string; label: string; value: LibraryScope }[]>([
    { key: "workspace", label: copy.workspace, value: { kind: "workspace" } },
  ]);
  const [scope, setScope] = useState("workspace");
  const [choices, setChoices] = useState<LibrarySkill[]>([]);
  const [known, setKnown] = useState<LibrarySkill[]>([]);
  const [mayRead, setMayRead] = useState(false);
  const [loading, setLoading] = useState(true);
  const [problem, setProblem] = useState<string | null>(null);
  const failed = (e: unknown) => setProblem(e instanceof Error ? e.message : copy.failed);
  useEffect(() => {
    let live = true;
    setProblem(null);
    setLoading(true);
    api
      .me()
      .then(async (me) => {
        const mayRead = me.permissions.includes("projects:read");
        const projects = mayRead ? await api.projects() : [];
        if (!live) return;
        setMayRead(mayRead);
        const next = [
          { key: "workspace", label: copy.workspace, value: { kind: "workspace" } as LibraryScope },
        ];
        let at = home.replace(/^~/, "");
        const seen = new Set<string>();
        for (let depth = 0; at && depth < 32 && !seen.has(at); depth++) {
          seen.add(at);
          const project = projects.find((p) => p.name === at);
          if (!project) break;
          next.push({
            key: `project:${project.name}`,
            label: project.title || project.name,
            value: { kind: "project", project: project.name },
          });
          if (project.personal_owner === me.principal.subject)
            next.push({ key: "personal", label: copy.personal, value: { kind: "personal" } });
          at = project.parent ?? "";
        }
        setScopes(next);
        setScope("workspace");
        setLoading(false);
      })
      .catch((e) => {
        if (live) {
          failed(e);
          setLoading(false);
        }
      });
    return () => {
      live = false;
    };
  }, [home]);
  const refKey = JSON.stringify(refs);
  useEffect(() => {
    let live = true;
    const refs: AgentSkillRef[] = JSON.parse(refKey);
    Promise.all(refs.slice(0, 4).map((ref) => api.librarySkill(ref.id).catch(() => []))).then(
      (groups) => {
        if (live) setKnown(groups.flat());
      },
    );
    return () => {
      live = false;
    };
  }, [refKey]);
  useEffect(() => {
    let live = true;
    setChoices([]);
    const target = scopes.find((s) => s.key === scope);
    if (!mayRead || !target) return;
    setLoading(true);
    api
      .librarySkills(target.value)
      .then(async (rows) => {
        const groups = await Promise.all(rows.slice(0, 32).map((row) => api.librarySkill(row.id)));
        if (live) {
          setChoices(
            groups.flatMap((versions) => versions.filter((v) => !!v.published_at).slice(0, 32)),
          );
          setLoading(false);
        }
      })
      .catch((e) => {
        if (live) {
          failed(e);
          setLoading(false);
        }
      });
    return () => {
      live = false;
    };
  }, [scope, scopes, mayRead]);
  return (
    <div className="grid min-w-0 gap-3 text-aui-sm">
      {problem && <Problem message={problem} />}
      <div>
        <h4>{copy.selected}</h4>
        <ul className="grid gap-2">
          {refs.map((ref) => {
            const row = [...known, ...choices].find(
              (c) => c.id === ref.id && c.version === ref.version,
            );
            return (
              <li key={ref.id} className="flex flex-wrap items-center gap-2">
                <>{row?<a className={`${textLink} inline-flex min-h-10 items-center break-words`} href={skillHref(row, ref.id,ref.version)} target="_blank" rel="noreferrer" aria-label={copy.inspect(row.name,ref.version)}>{row.name}</a>:<span>{copy.savedRevision}</span>}</>
                <span>{copy.version(ref.version)}</span>
                {!disabled && (
                  <button
                    type="button"
                    className={button}
                    onClick={() => onChange(refs.filter((s) => s.id !== ref.id))}
                  >
                    {copy.remove}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      </div>
      {loading ? (
        <output>{copy.loading}</output>
      ) : !mayRead ? (
        <p>{copy.unavailable}</p>
      ) : (
        <>
          <Label text={copy.scope}>
            <select
              className={field}
              value={scope}
              disabled={disabled}
              onChange={(e) => setScope(e.target.value)}
            >
              {scopes.map((s) => (
                <option key={s.key} value={s.key}>
                  {s.label}
                </option>
              ))}
            </select>
          </Label>
          {!choices.length && <p>{copy.empty}</p>}
          <div className="grid gap-2">
            {choices.map((row) => {
              const checked = refs.some((s) => s.id === row.id && s.version === row.version);
              return (
                <label key={`${row.id}:${row.version}`} className="flex min-h-11 min-w-0 items-start gap-2 py-2">
                  <input
                    type="checkbox"
                    checked={checked}
                    disabled={
                      disabled ||
                      (!checked && refs.length >= 4 && !refs.some((s) => s.id === row.id))
                    }
                    onChange={() =>
                      onChange(
                        checked
                          ? refs.filter((s) => s.id !== row.id)
                          : [
                              ...refs.filter((s) => s.id !== row.id),
                              { id: row.id, version: row.version },
                            ],
                      )
                    }
                  />
                  <span>
                    {row.name} {copy.version(row.version)}
                    <span className="block text-aui-xs text-aui-muted">{row.description}</span><span className="block text-aui-xs text-aui-muted">{copy.tools}: {row.tools.length?row.tools.join(", "):copy.noTools}</span>
                  </span>
                </label>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
}

export function AgentDefaultSkills({
  agent,
  home = "",
  agentRevision,
  canWrite = false,
}: {
  agent: string;
  home?: string;
  agentRevision: number;
  canWrite?: boolean;
}) {
  const copy = defaultCopy.agentSkillDefaults;
  const [saved, setSaved] = useState<Bindings | null>(null);
  const [selected, setSelected] = useState<AgentSkillRef[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState("");
  const [refresh, setRefresh] = useState(0);
  const failed = (e: unknown) => setProblem(e instanceof Error ? e.message : copy.failed);
  useEffect(() => {
    let live = true;
    setSaved(null);
    setProblem(null);
    setNotice("");
    api
      .agentSkillDefaults(agent)
      .then((bindings) => {
        if (live) {
          setSaved(bindings);
          setSelected(bindings.skills);
        }
      })
      .catch((e) => {
        if (live) failed(e);
      });
    return () => {
      live = false;
    };
  }, [agent, refresh]);
  const save = async () => {
    if (!saved) return;
    setBusy(true);
    setNotice("");
    setProblem(null);
    try {
      const next = await api.setAgentSkillDefaults(agent, {
        agent_revision: agentRevision,
        version: saved.version,
        skills: selected,
      });
      setSaved(next);
      setSelected(next.skills);
      setNotice(copy.saved);
    } catch (e) {
      failed(e);
    } finally {
      setBusy(false);
    }
  };
  return (
    <section aria-label={copy.title} className="grid min-w-0 gap-3 rounded-aui border border-aui-border p-4 text-aui-sm">
      <h3 className="font-medium">{copy.title}</h3>
      <p className="text-aui-sm text-aui-muted">{copy.help}</p>
      {problem && <Problem message={problem} />}
      {notice && <output>{notice}</output>}
      {!saved ? (
        <output>{copy.loading}</output>
      ) : (
        <>
          {saved.agent_revision !== agentRevision && <output>{copy.stale}</output>}
          {!canWrite && <p>{copy.readOnly}</p>}
          <PublishedSkillPicker
            home={home}
            refs={selected}
            disabled={!canWrite || busy}
            onChange={(refs) => {
              setSelected(refs);
              setNotice("");
            }}
          />
          <div className="flex flex-wrap gap-3">
            {canWrite && (
              <button type="button" className={primary} disabled={busy} onClick={() => void save()}>
                {copy.save}
              </button>
            )}
            <button type="button" className={button} disabled={busy} onClick={() => setRefresh((v) => v + 1)}>
              {copy.reload}
            </button>
          </div>
        </>
      )}
    </section>
  );
}
function skillHref(row: LibrarySkill | undefined, id: string,version:number) {
  const query = new URLSearchParams({ scope: row?.scope.kind ?? "workspace", skill: id,version:String(version) });
  if (row?.scope.project) query.set("project", row.scope.project);
  return `/library/skills?${query}`;
}
