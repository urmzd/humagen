import { type FormEvent, type MouseEvent, useCallback, useEffect, useState } from "react";
import { ApiError, api, type ToolCredential, type ToolCredentials } from "./api.ts";
import { CONNECT_PERMISSION, cannotConnect, destination, offers, standing, startsAllowed, type Way, ways, whereReferences } from "./connections.ts";
import { href } from "./route.ts";
import { button, card, Empty, field, Label, Page, primary, Problem, Segmented } from "./ui.tsx";

/** A plain click goes there in place; a modified one is the browser's. */
const follow = (go: () => void) => (e: MouseEvent<HTMLAnchorElement>) => {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey) return;
  e.preventDefault();
  go();
};

/**
 * Your connections: the tools in this workspace that act as you, reached with
 * your own credential, and whether you have given one. Everything here is
 * yours alone; nobody else's credential is shown or used for you.
 */
export function YourConnections({
  open,
  onOpen,
  canConnect = true,
  onWhy,
}: {
  open?: string;
  onOpen: (provider?: string) => void;
  /** May give or remove a credential, which the server holds to CONNECT_PERMISSION. */
  canConnect?: boolean;
  /** Opens why the person may not connect, in Settings > Access. */
  onWhy?: () => void;
}) {
  const [state, setState] = useState<ToolCredentials | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const fail = (fallback: string) => (e: unknown) => setProblem(e instanceof ApiError ? e.message : fallback);
  const reload = useCallback(() => {
    api
      .toolCredentials()
      .then(setState)
      .catch(fail("Your connections could not be loaded."));
  }, []);
  useEffect(reload, [reload]);

  const save = (provider: string, way: Way, text: string) => {
    setProblem(null);
    setBusy(true);
    api
      .connectTool(provider, way === "value" ? { value: text } : { ref: text.trim() })
      .then(() => {
        onOpen(undefined);
        reload();
      })
      .catch(fail("The credential could not be saved."))
      .finally(() => setBusy(false));
  };
  const remove = (provider: string) => {
    setProblem(null);
    api.disconnectTool(provider).then(reload).catch(fail("The credential could not be removed."));
  };

  return (
    <Page
      title="Your connections"
      lede="Some tools act as you: they reach their service with your own credential. Without one, an agent cannot use them for you, and nobody else's is used instead."
      narrow
    >
      <Problem message={problem} />
      {!canConnect && state && state.providers.length > 0 && (
        <p className="text-aui-sm text-aui-muted">
          {cannotConnect}
          {onWhy && (
            <>
              {" "}
              <a
                href={href({ page: "settings", section: "access", permission: CONNECT_PERMISSION })}
                onClick={follow(onWhy)}
                className="rounded-aui text-aui-fg underline focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                Why can't I?
              </a>
            </>
          )}
        </p>
      )}
      {state && state.providers.length === 0 && (
        <Empty title="No tools here act as you">A tool acts as each person when the workspace declares it with credential_scope: person.</Empty>
      )}
      {state?.providers.map((c) => (
        <ConnectionCard
          key={c.provider}
          connection={c}
          ways={ways(state)}
          references={state.references}
          canConnect={canConnect}
          open={canConnect && open === c.provider}
          busy={busy}
          onOpen={() => onOpen(c.provider)}
          onCancel={() => onOpen(undefined)}
          onSave={(way, text) => save(c.provider, way, text)}
          onRemove={() => remove(c.provider)}
        />
      ))}
    </Page>
  );
}

/** One tool that acts as you, and what you gave for it. */
export function ConnectionCard({
  connection: c,
  ways,
  references,
  canConnect = true,
  open,
  busy = false,
  onOpen,
  onCancel,
  onSave,
  onRemove,
}: {
  connection: ToolCredential;
  ways: Way[];
  references: string[];
  /** May give or remove a credential. One who may not is shown where they stand, and nothing to press. */
  canConnect?: boolean;
  open: boolean;
  busy?: boolean;
  onOpen: () => void;
  onCancel: () => void;
  onSave: (way: Way, text: string) => void;
  onRemove: () => void;
}) {
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label={c.provider}>
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h2 className="text-aui-sm font-medium text-aui-fg">
          {c.provider} <span className="font-normal text-aui-muted">{offers(c)}</span>
        </h2>
        <span className={`text-aui-xs ${c.connected ? "text-aui-fg" : "text-aui-muted"}`}>{c.connected ? "Connected" : "Not connected"}</span>
      </div>
      <p className="text-aui-sm text-aui-muted">{standing(c)}</p>
      <p className="text-aui-xs text-aui-muted">{destination(c)}</p>
      {!canConnect ? null : open ? (
        <ConnectForm ways={ways} references={references} replacing={c.connected} busy={busy} onSave={onSave} onCancel={onCancel} />
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          {ways.length > 0 && (
            <button type="button" className={c.connected ? button : primary} onClick={onOpen}>
              {c.connected ? "Replace" : "Connect"}
            </button>
          )}
          {c.connected && (
            <button type="button" className={button} onClick={onRemove}>
              Remove
            </button>
          )}
          {ways.length === 0 && !c.connected && (
            <p className="text-aui-xs text-aui-muted">
              This workspace lets nobody connect a credential yet: the operator has listed nowhere references may live, and stored values are off.
            </p>
          )}
        </div>
      )}
    </section>
  );
}

/**
 * Give a credential: a reference to where it is kept, or, where the workspace
 * keeps them, the value. A value is typed once and never shown again.
 */
export function ConnectForm({
  ways,
  references,
  replacing,
  busy = false,
  onSave,
  onCancel,
}: {
  ways: Way[];
  references: string[];
  replacing: boolean;
  busy?: boolean;
  onSave: (way: Way, text: string) => void;
  onCancel: () => void;
}) {
  const [way, setWay] = useState<Way>(ways[0] ?? "reference");
  const [text, setText] = useState("");
  const outside = way === "reference" && text.trim() !== "" && !startsAllowed(text, references);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    if (text.trim() === "" || outside) return;
    onSave(way, text);
    // A value is not kept on the page once it is sent.
    setText("");
  };
  return (
    <form className="flex flex-col gap-3" onSubmit={submit}>
      {ways.length > 1 && (
        <Segmented<Way>
          label="How to give it"
          value={way}
          options={[
            { id: "reference", label: "Reference" },
            { id: "value", label: "Value" },
          ]}
          onChange={(next) => {
            setWay(next);
            setText("");
          }}
        />
      )}
      {way === "reference" ? (
        <Label text="Reference" hint={outside ? "That is not somewhere this workspace lets references live." : whereReferences(references)}>
          <input
            className={field}
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={references[0] ? `${references[0]}…` : "op://VAULT/ITEM/FIELD"}
            autoComplete="off"
            spellCheck={false}
          />
        </Label>
      ) : (
        <Label text="Value" hint="Stored with warp, in the store client secrets are kept in. It is never shown again.">
          <input className={field} type="password" value={text} onChange={(e) => setText(e.target.value)} autoComplete="off" spellCheck={false} />
        </Label>
      )}
      <div className="flex flex-wrap items-center gap-2">
        <button type="submit" className={primary} disabled={busy || text.trim() === "" || outside}>
          {replacing ? "Replace" : "Connect"}
        </button>
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}
