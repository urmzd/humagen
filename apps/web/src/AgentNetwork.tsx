import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type HandoffContract, type AgentNetwork as Network } from "./api.ts";
import { OperationPersonField } from "./OperationPersonField.tsx";
import { AgentMark, field, Label, Page, Problem, primary } from "./ui.tsx";

const words = defaultCopy.operations;

export function AgentNetwork({
  selectedId,
  canWrite = false,
  onAgent,
  onRun,
}: {
  selectedId?: string;
  canWrite?: boolean;
  onAgent: (name: string) => void;
  onRun: (id: string) => void;
}) {
  const [network, setNetwork] = useState<Network | null>(null);
  const [contracts, setContracts] = useState<HandoffContract[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    setNetwork(null);
    setContracts([]);
    setProblem(null);
    const read = () =>
      Promise.all([
        api.agentNetwork(),
        selectedId ? api.handoff(selectedId).then((h) => [h]) : api.handoffs(),
      ])
        .then(([n, h]) => {
          if (live) {
            setNetwork(n);
            setProblem(null);
            setContracts(h);
          }
        })
        .catch(() => {
          if (live) setProblem(words.loadFailed);
        });
    void read();
    const timer = setInterval(() => void read(), 5000);
    return () => {
      live = false;
      clearInterval(timer);
    };
  }, [selectedId]);
  return (
    <Page title={words.network} lede={words.networkHint}>
      <Problem message={problem} />
      {!network ? (
        <output>{words.loading}</output>
      ) : network.edges.length === 0 ? (
        <p className="text-aui-sm text-aui-muted">{words.networkEmpty}</p>
      ) : (
        <>
          {network.truncated && (
            <output className="text-aui-xs text-aui-muted">{words.truncated}</output>
          )}
          <ul className="divide-y divide-aui-line">
            {network.edges.map((e) => (
              <li
                key={`${e.from}:${e.to}`}
                className="flex flex-wrap items-center gap-x-5 gap-y-3 py-4"
              >
                <button
                  type="button"
                  className="flex items-center gap-2"
                  onClick={() => onAgent(e.from)}
                >
                  <AgentMark name={e.from} />
                  {e.from}
                </button>
                <span aria-hidden="true">→</span>
                <button
                  type="button"
                  className="flex items-center gap-2"
                  onClick={() => onAgent(e.to)}
                >
                  <AgentMark name={e.to} />
                  {e.to}
                </button>
                <span className="text-aui-xs text-aui-muted">
                  {words.volume}: {e.handoffs}
                </span>
                <span className="text-aui-xs text-aui-muted">
                  {words.waiting}: {e.waiting}
                </span>
                <span className="text-aui-xs text-aui-muted">
                  {words.blocked}: {e.blocked}
                </span>
                <span className="text-aui-xs text-aui-muted">
                  {words.working}: {e.working ?? 0}
                </span>
                <span className="text-aui-xs text-aui-muted">
                  {words.waitTime}: {words.minutes(Math.floor((e.waiting_seconds ?? 0) / 60))}
                </span>
              </li>
            ))}
          </ul>
          <section className="mt-8">
            <h2 className="text-aui-base font-medium">{words.contracts}</h2>
            <ul className="divide-y divide-aui-line">
              {contracts.map((h) => (
                <li key={h.id} className="flex flex-col gap-2 py-4">
                  <p className="text-aui-sm font-medium">{h.goal}</p>
                  <p className="text-aui-xs text-aui-muted">
                    {h.from} → {h.to}: {words.states[h.state]}
                  </p>
                  <p className="text-aui-sm">
                    {words.doneWhen}: {h.done_when}
                  </p>
                  <p className="text-aui-xs text-aui-muted">
                    {words.owner}: {h.owner_name || words.unassigned}
                  </p>
                  {h.due_at && (
                    <p className="text-aui-xs text-aui-muted">
                      {words.due}:{" "}
                      <time dateTime={h.due_at}>{new Date(h.due_at).toLocaleString()}</time>
                    </p>
                  )}
                  <HandoffInputs contract={h} onRun={onRun} />
                  {h.blocker && (
                    <output className="text-aui-sm">
                      {words.blocker}: {h.blocker}
                    </output>
                  )}
                  {canWrite && (
                    <HandoffDecision
                      contract={h}
                      onChanged={(updated) =>
                        setContracts((old) =>
                          old.map((item) => (item.id === updated.id ? updated : item)),
                        )
                      }
                    />
                  )}
                  {h.run_id && (
                    <button
                      type="button"
                      className="self-start text-aui-sm"
                      onClick={() => onRun(h.run_id as string)}
                    >
                      {words.openRun}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          </section>
        </>
      )}
    </Page>
  );
}

function HandoffDecision({
  contract,
  onChanged,
}: {
  contract: HandoffContract;
  onChanged: (h: HandoffContract) => void;
}) {
  const [owner, setOwner] = useState(contract.owner ?? "");
  const [ownerName, setOwnerName] = useState(contract.owner_name);
  const [version, setVersion] = useState(contract.version);
  const [blocker, setBlocker] = useState(contract.blocker ?? "");
  const [state, setState] = useState<"accepted" | "blocked" | "needs_person" | "result">(
    "accepted",
  );
  const [due, setDue] = useState(() => {
    const d = new Date(contract.due_at ?? Date.now());
    return new Date(d.getTime() - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
  });
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  return (
    <form
      className="flex max-w-xl flex-col gap-3"
      onSubmit={async (e) => {
        e.preventDefault();
        if (busy) return;
        setBusy(true);
        setProblem(null);
        try {
          onChanged(
            await api.saveHandoff(contract.id, {
              version,
              owner,
              blocker,
              state,
              due_at: new Date(due).toISOString(),
            }),
          );
        } catch (e) {
          setProblem(e instanceof Error ? e.message : words.failed);
        } finally {
          setBusy(false);
        }
      }}
    >
      <Problem message={problem} />
      <OperationPersonField
        label={words.owner}
        value={owner}
        displayName={ownerName}
        disabled={busy}
        onChange={setOwner}
      />
      {version !== contract.version && (
        <button
          type="button"
          onClick={() => {
            setOwner(contract.owner ?? "");
            setOwnerName(contract.owner_name);
            setBlocker(contract.blocker ?? "");
            setVersion(contract.version);
            const current = new Date(contract.due_at ?? Date.now());
            setDue(
              new Date(current.getTime() - current.getTimezoneOffset() * 60000)
                .toISOString()
                .slice(0, 16),
            );
          }}
        >
          {words.reviewLatest}
        </button>
      )}
      <Label text={words.rule}>
        <select
          className={field}
          value={state}
          onChange={(e) => setState(e.target.value as typeof state)}
        >
          <option value="accepted">{words.states.accepted}</option>
          <option value="blocked">{words.states.blocked}</option>
          <option value="needs_person">{words.states.needs_person}</option>
          <option value="result">{words.states.result}</option>
        </select>
      </Label>
      <Label text={words.blocker}>
        <textarea
          className={field}
          value={blocker}
          maxLength={4000}
          required={state === "blocked" || state === "needs_person"}
          onChange={(e) => setBlocker(e.target.value)}
        />
      </Label>
      <Label text={words.due}>
        <input
          className={field}
          type="datetime-local"
          required
          value={due}
          onChange={(e) => setDue(e.target.value)}
        />
      </Label>
      <button type="submit" className={primary} disabled={busy || version !== contract.version}>
        {busy ? words.saving : words.save}
      </button>
    </form>
  );
}

function HandoffInputs({
  contract,
  onRun,
}: {
  contract: HandoffContract;
  onRun: (id: string) => void;
}) {
  return (
    <section className="min-w-0 space-y-2">
      <h3 className="text-aui-sm font-medium">{words.inputs}</h3>
      {(contract.inputs ?? []).length === 0 ? (
        <p className="text-aui-xs text-aui-muted">{words.noInputs}</p>
      ) : (
        <ul className="space-y-2">
          {contract.inputs.map((input, index) => (
            <li key={`${input.type}:${index}`} className="min-w-0 text-aui-sm">
              {input.type === "text" ? (
                <pre className="whitespace-pre-wrap break-words font-sans">{input.reference}</pre>
              ) : input.type === "run" ? (
                <button type="button" onClick={() => onRun(input.reference)}>
                  {words.openInputRun}
                </button>
              ) : input.type === "thread" ? (
                <a
                  className="text-aui-accent"
                  href={`/conversations/${encodeURIComponent(input.reference)}`}
                >
                  {words.openInputConversation}
                </a>
              ) : (
                <a
                  className="text-aui-accent"
                  href={`/artifacts/${encodeURIComponent(input.reference.split("@")[0] ?? "")}`}
                >
                  {words.openInputArtifact} (
                  {words.artifactVersion(input.reference.split("@")[1] ?? "")})
                </a>
              )}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
