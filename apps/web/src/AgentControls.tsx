import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { type AgentControl, api } from "./api.ts";
import { AgentMark, field, Label, Page, Problem, primary } from "./ui.tsx";

const words = defaultCopy.operations;

export function AgentControls({
  agent = "",
  canWrite,
  onChanged,
}: {
  agent?: string;
  canWrite: boolean;
  onChanged?: () => void;
}) {
  const [control, setControl] = useState<AgentControl | null>(null);
  const [manager, setManager] = useState("");
  const [stopRules, setStopRules] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    setControl(null);
    setProblem(null);
    api
      .agentControl(agent)
      .then((c) => {
        if (live) {
          setControl(c);
          setManager(c.manager ?? "");
          setStopRules(c.stop_rules ?? "");
        }
      })
      .catch(() => {
        if (live) setProblem(words.loadFailed);
      });
    return () => {
      live = false;
    };
  }, [agent]);
  const save = async (paused: boolean) => {
    if (!control || busy || !canWrite) return;
    setBusy(true);
    setProblem(null);
    try {
      const c = await api.saveAgentControl(agent, {
        paused,
        manager,
        stop_rules: stopRules,
        version: control.version,
      });
      setControl(c);
      onChanged?.();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Page
      title={agent ? words.controls : words.workspaceControls}
      icon={agent ? <AgentMark name={agent} /> : undefined}
      lede={words.controlsHint}
    >
      <Problem message={problem} />
      {!control ? (
        <output>{words.loading}</output>
      ) : (
        <div className="flex max-w-xl flex-col gap-5">
          <output className="text-aui-sm text-aui-fg">
            {control.paused ? words.paused : words.active}
          </output>
          <button
            type="button"
            className={primary}
            disabled={!canWrite || busy}
            onClick={() => void save(!control.paused)}
          >
            {agent
              ? control.paused
                ? words.resume
                : words.pause
              : control.paused
                ? words.resumeAll
                : words.pauseAll}
          </button>
          {agent && (
            <>
              <Label text={words.manager}>
                <input
                  className={field}
                  value={manager}
                  maxLength={256}
                  disabled={!canWrite || busy}
                  onChange={(e) => setManager(e.target.value)}
                />
              </Label>
              <Label text={words.stopRules}>
                <textarea
                  className={field}
                  value={stopRules}
                  maxLength={4000}
                  disabled={!canWrite || busy}
                  onChange={(e) => setStopRules(e.target.value)}
                />
              </Label>
              <p className="text-aui-xs text-aui-muted">{words.stopRulesHint}</p>
              <button
                type="button"
                className={primary}
                disabled={!canWrite || busy}
                onClick={() => void save(control.paused)}
              >
                {busy ? words.saving : words.save}
              </button>
            </>
          )}
        </div>
      )}
    </Page>
  );
}
