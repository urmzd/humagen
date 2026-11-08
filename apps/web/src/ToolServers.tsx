import { Inspector } from "@humagen/web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import { ApiError, api, type PersonContext, type PersonToolServer } from "./api.ts";
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
  allowedHosts,
}: {
  /** How references you may give start, from your connections. */
  references: string[];
  allowedHosts?: string[];
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

  const [reviewing, setReviewing] = useState<PersonToolServer | null>(null);
  const review = async (server: PersonToolServer) => {
    setBusy(true);
    setProblem(null);
    try {
      setReviewing(await api.reviewPersonTools({ ...server, tools: undefined }));
    } catch (e) {
      fail("The tool list could not be loaded.")(e);
    } finally {
      setBusy(false);
    }
  };
  const approve = async () => {
    if (!context || !reviewing) return;
    setBusy(true);
    try {
      setContext(
        await api.setPersonContext({
          run: context.run,
          version: context.version,
          tool_servers: context.tool_servers.map((s) =>
            s.name === reviewing.name ? reviewing : s,
          ),
        }),
      );
      setReviewing(null);
    } catch (e) {
      fail("The tool list could not be approved.")(e);
    } finally {
      setBusy(false);
    }
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
        workspace allows, and with nobody else's. Only agents you permit can use them. Review each
        tool list before connecting. A changed list needs approval again.
      </p>
      {allowedHosts && (
        <p className="text-aui-sm text-aui-muted">
          {allowedHosts.length
            ? `Allowed hosts: ${allowedHosts.join(", ")}.`
            : "No tool server hosts are allowed yet. Ask a workspace administrator to allow a service in Limits."}
        </p>
      )}
      <Problem message={problem} />
      {reviewing && (
        <section aria-label="Review tool list" className={card}>
          <ToolList server={reviewing} />
          <button type="button" className={button} onClick={() => setReviewing(null)}>
            Cancel
          </button>
          <button type="button" className={primary} disabled={busy} onClick={approve}>
            Approve Tool List
          </button>
        </section>
      )}
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
                <button type="button" className={button} disabled={busy} onClick={() => review(s)}>
                  Review Tools
                </button>
              )}
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
            allowedHosts={allowedHosts}
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
  allowedHosts,
}: {
  context: PersonContext;
  references: string[];
  onCancel: () => void;
  onAdded: (next: PersonContext) => void;
  allowedHosts?: string[];
}) {
  const [name, setName] = useState("");
  const [url, setUrl] = useState("");
  const [credential, setCredential] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [preview, setPreview] = useState<PersonToolServer | null>(null);
  const server = { name: name.trim(), url: url.trim(), credential };
  const submit = (e: FormEvent) => {
    e.preventDefault();
    const why = toolServerProblem(server, context.tool_servers, references);
    if (why) {
      setProblem(why);
      return;
    }
    if (allowedHosts && !allowedHosts.includes(hostOf(server.url))) {
      setProblem(
        allowedHosts.length
          ? `This host is not allowed. Allowed hosts: ${allowedHosts.join(", ")}.`
          : "No tool server hosts are allowed yet. Ask an administrator to allow this service in Limits.",
      );
      return;
    }
    setBusy(true);
    setProblem(null);
    const action = preview
      ? api.setPersonContext(withToolServer(context, preview)).then(onAdded)
      : api.reviewPersonTools(server).then(setPreview);
    action
      .catch((err) =>
        setProblem(err instanceof ApiError ? err.message : "The tool server could not be added."),
      )
      .finally(() => setBusy(false));
  };
  return (
    <form aria-label="New tool server" className="flex flex-col gap-3" onSubmit={submit} noValidate>
      <Problem message={problem} />
      {preview && <ToolList server={preview} />}
      <fieldset disabled={busy || preview !== null} className="contents">
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
      </fieldset>
      <div className="flex flex-wrap justify-end gap-2">
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
        <button type="submit" className={primary} disabled={busy}>
          {preview ? "Approve Tool List" : "Review Tools"}
        </button>
      </div>
    </form>
  );
}

export function ToolList({ server }: { server: PersonToolServer }) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="font-medium">Tools from {server.name}</h3>
      <p className="text-aui-sm text-aui-muted">
        Read tools run with your permission. Every write tool asks for approval.
      </p>
      {server.tools?.length === 0 && <p>No tools offered.</p>}
      <ul>
        {server.tools?.map((tool) => (
          <li key={tool.name} className="py-2">
            <strong>{tool.name}</strong>{" "}
            <span>{tool.class === "read" ? "Read" : "Write, approval required"}</span>
            <p className="text-aui-sm text-aui-muted">{tool.description}</p>
            <details>
              <summary>Input Schema</summary>
              <pre className="overflow-x-auto text-aui-xs">
                {JSON.stringify(tool.input_schema, null, 2)}
              </pre>
            </details>
          </li>
        ))}
      </ul>
    </div>
  );
}
