import { useEffect, useState } from "react";
import { api, type Client } from "./api.ts";
import { button, field, Label, Problem } from "./ui.tsx";

/** Native consent establishes identity; the administrator grants routing next. */
export function InstalledClientSetup({
  client,
  onChanged,
}: {
  client: Client;
  onChanged: () => void;
}) {
  const [agent, setAgent] = useState(client.default_agent ?? "");
  const [allowed, setAllowed] = useState((client.agents ?? []).join(", "));
  const [project, setProject] = useState(client.home?.project ?? "");
  const [channel, setChannel] = useState(client.home?.channel ?? "");
  const [enabled, setEnabled] = useState(client.enabled);
  const [login, setLogin] = useState(client.github?.login ?? "");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [names, setNames] = useState<string[] | null>(null);
  const [projects, setProjects] = useState<Awaited<ReturnType<typeof api.projects>>>([]);
  useEffect(() => {
    let active = true;
    void Promise.allSettled([api.agents(), api.projects()]).then(([agents, places]) => {
      if (!active) return;
      if (agents.status === "fulfilled") setNames(agents.value.map((r) => r.spec.name));
      else setProblem("Agent choices could not be loaded. Reload before changing agent access.");
      if (places.status === "fulfilled") setProjects(places.value);
    });
    return () => {
      active = false;
    };
  }, []);
  const known = [
    ...new Set(
      [...(names ?? []), agent, ...allowed.split(",").map((a) => a.trim())].filter(Boolean),
    ),
  ];
  return (
    <form
      className="flex flex-col gap-3"
      aria-label="Configure installation"
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy) return;
        setBusy(true);
        setProblem(null);
        try {
          await api.saveClient(
            {
              ...client,
              default_agent: agent.trim(),
              agents: allowed
                .split(",")
                .map((a) => a.trim())
                .filter(Boolean),
              ...(client.surface === "github"
                ? { github: { ...client.github, login: login.trim() } }
                : {}),
              home: project.trim()
                ? { project: project.trim(), channel: channel.trim() || "general" }
                : undefined,
            },
            enabled,
          );
          onChanged();
        } catch (error) {
          setProblem(
            error instanceof Error ? error.message : "Installation could not be configured.",
          );
        } finally {
          setBusy(false);
        }
      }}
    >
      <p className="text-aui-sm text-aui-muted">
        Choose which agents this installation reaches and where its sessions belong. Use Events and
        Actions to grant its capabilities before enabling it.
      </p>
      <Label text="Default agent">
        <select
          className={field}
          value={agent}
          onChange={(e) => setAgent(e.target.value)}
          disabled={names === null}
        >
          <option value="">No default agent</option>
          {known.map((name) => (
            <option key={name} value={name}>
              {name}
            </option>
          ))}
        </select>
      </Label>
      {client.surface === "github" && (
        <Label text="GitHub App bot login" hint="For example, humagen[bot]">
          <input
            className={field}
            value={login}
            onChange={(e) => setLogin(e.target.value)}
            required={Boolean(agent.trim())}
            maxLength={80}
          />
        </Label>
      )}
      <fieldset className="flex flex-col gap-2" disabled={names === null}>
        <legend className="text-aui-sm">Allowed agents</legend>
        {known.map((name) => (
          <label key={name} className="flex items-center gap-2 text-aui-sm">
            <input
              type="checkbox"
              checked={allowed
                .split(",")
                .map((a) => a.trim())
                .includes(name)}
              onChange={(e) => {
                const before = allowed
                  .split(",")
                  .map((a) => a.trim())
                  .filter(Boolean);
                setAllowed(
                  (e.target.checked
                    ? [...new Set([...before, name])]
                    : before.filter((a) => a !== name)
                  ).join(", "),
                );
              }}
            />
            {name}
          </label>
        ))}
      </fieldset>
      <Label text="Project" hint="Leave empty for the workspace">
        <select className={field} value={project} onChange={(e) => setProject(e.target.value)}>
          <option value="">Workspace</option>
          {project && !projects.some((p) => p.name === project) && (
            <option value={project}>{project}</option>
          )}
          {projects
            .filter((p) => p.name !== "~")
            .map((p) => (
              <option key={p.name} value={p.name}>
                {p.title || p.name}
              </option>
            ))}
        </select>
      </Label>
      {project.trim() && (
        <Label text="Channel">
          <input
            className={field}
            value={channel}
            onChange={(e) => setChannel(e.target.value)}
            placeholder="general"
            maxLength={80}
          />
        </Label>
      )}
      <label className="flex items-center gap-2 text-aui-sm">
        <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} />
        Enabled
      </label>
      <Problem message={problem} />
      <button type="submit" disabled={busy} className={`${button} self-start`}>
        {busy ? "Saving…" : "Save installation"}
      </button>
    </form>
  );
}
