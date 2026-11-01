import { Inspector } from "@humagen/web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import { ApiError, api, type PersonContext } from "./api.ts";
import { whereReferences } from "./connections.ts";
import {
  credentialWords,
  hostOf,
  MAX_TOOL_SERVERS,
  toolServerProblem,
  withoutToolServer,
  withToolServer,
} from "./toolServers.ts";
import { button, card, Empty, field, Label, Problem, primary, rows } from "./ui.tsx";

/**
 * Your tool servers: MCP servers you add for yourself. They belong to the
 * runs that act for you and to nobody else's, and they hold a credential
 * reference, never the secret.
 */
export function YourToolServers({
  references,
  canConnect = true,
}: {
  /** How references you may give start, from your connections. */
  references: string[];
  /** May add or remove a server, which the server holds to runs:write. */
  canConnect?: boolean;
}) {
  const [context, setContext] = useState<PersonContext | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [busy, setBusy] = useState(false);

  const fail = (fallback: string) => (e: unknown) =>
    setProblem(e instanceof ApiError ? e.message : fallback);
  const reload = useCallback(() => {
    api.personContext().then(setContext).catch(fail("Your tool servers could not be loaded."));
  }, []);
  useEffect(reload, [reload]);

  const remove = (name: string) => {
    if (!context) return;
    setProblem(null);
    setBusy(true);
    api
      .setPersonContext(withoutToolServer(context, name))
      .then(setContext)
      .catch((e) => {
        fail("The tool server could not be removed.")(e);
        reload();
      })
      .finally(() => setBusy(false));
  };

  const servers = context?.tool_servers ?? [];
  const full = servers.length >= MAX_TOOL_SERVERS;
  return (
    <section aria-label="Your tool servers" className="flex flex-col gap-3">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-aui-base font-medium text-aui-fg">Your tool servers</h2>
        {canConnect && context && (
          <button
            type="button"
            className={primary}
            aria-haspopup="dialog"
            aria-expanded={adding}
            disabled={full}
            onClick={() => setAdding(true)}
          >
            Add Tool Server
          </button>
        )}
      </div>
      <p className="text-aui-sm text-aui-muted">
        MCP servers you add for yourself. They go with the runs that act for you, on hosts the
        workspace allows, and with nobody else's. They are not connected to runs yet: that waits for
        the connectors compartment of a run's sandbox.
      </p>
      <Problem message={problem} />
      {context && servers.length === 0 && (
        <div className={card}>
          <Empty title="No tool servers">
            {canConnect ? "Add one by its https address." : "You have not added any."}
          </Empty>
        </div>
      )}
      {servers.length > 0 && (
        <ul className={rows}>
          {servers.map((s) => (
            <li
              key={s.name}
              aria-label={s.name}
              className="flex flex-wrap items-center justify-between gap-2 py-3"
            >
              <div className="min-w-0">
                <p className="text-aui-sm font-medium text-aui-fg">
                  {s.name} <span className="font-normal text-aui-muted">{hostOf(s.url)}</span>
                </p>
                <p className="break-all text-aui-xs text-aui-muted">{credentialWords(s)}</p>
              </div>
              {canConnect && (
                <button
                  type="button"
                  className={button}
                  disabled={busy}
                  aria-label={`Remove ${s.name}`}
                  onClick={() => remove(s.name)}
                >
                  Remove
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
      <Inspector open={adding} title="Add a tool server" onClose={() => setAdding(false)}>
        {context && (
          <AddToolServer
            context={context}
            references={references}
            onCancel={() => setAdding(false)}
            onAdded={(next) => {
              setContext(next);
              setAdding(false);
            }}
          />
        )}
      </Inspector>
    </section>
  );
}

/** The form that adds one server: a name, its https address and, optionally, a reference. */
export function AddToolServer({
  context,
  references,
  onCancel,
  onAdded,
}: {
  context: PersonContext;
  references: string[];
  onCancel: () => void;
  onAdded: (next: PersonContext) => void;
}) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [credential, setCredential] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const server = { name: name.trim(), url: url.trim(), credential };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const why = toolServerProblem(server, context.tool_servers, references);
    if (why) {
      setProblem(why);
      return;
    }
    setBusy(true);
    setProblem(null);
    api
      .setPersonContext(withToolServer(context, server))
      .then(onAdded)
      .catch((err) =>
        setProblem(err instanceof ApiError ? err.message : "The tool server could not be added."),
      )
      .finally(() => setBusy(false));
  };
  return (
    <form aria-label="New tool server" className="flex flex-col gap-3" onSubmit={submit}>
      <Problem message={problem} />
      <Label text="Name" hint="Lower case letters, digits and dashes.">
        <input
          className={field}
          required
          disabled={busy}
          maxLength={63}
          value={name}
          onChange={(e) => setName(e.target.value)}
          autoComplete="off"
          spellCheck={false}
        />
      </Label>
      <Label text="Address" hint="The server's https URL, with no user or password in it.">
        <input
          className={field}
          required
          disabled={busy}
          type="url"
          maxLength={500}
          value={url}
          onChange={(e) => setUrl(e.target.value)}
          placeholder="https://mcp.example.com/mcp"
          autoComplete="off"
          spellCheck={false}
        />
      </Label>
      <Label text="Credential reference" hint={`Optional. ${whereReferences(references)}`}>
        <input
          className={field}
          disabled={busy || references.length === 0}
          value={credential}
          onChange={(e) => setCredential(e.target.value)}
          placeholder={references[0] ? `${references[0]}…` : "op://VAULT/ITEM/FIELD"}
          autoComplete="off"
          spellCheck={false}
        />
      </Label>
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className={primary} disabled={busy}>
          Add Tool Server
        </button>
      </div>
    </form>
  );
}
