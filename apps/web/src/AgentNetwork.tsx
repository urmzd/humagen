import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type HandoffContract, type AgentNetwork as Network } from "./api.ts";
import { AgentMark, Page, Problem } from "./ui.tsx";

const words = defaultCopy.operations;

export function AgentNetwork({
  onAgent,
  onRun,
}: {
  onAgent: (name: string) => void;
  onRun: (id: string) => void;
}) {
  const [network, setNetwork] = useState<Network | null>(null);
  const [contracts, setContracts] = useState<HandoffContract[]>([]);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let live = true;
    const read = () =>
      Promise.all([api.agentNetwork(), api.handoffs()])
        .then(([n, h]) => {
          if (live) {
            setNetwork(n);
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
  }, []);
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
                  {h.blocker && (
                    <output className="text-aui-sm">
                      {words.blocker}: {h.blocker}
                    </output>
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
