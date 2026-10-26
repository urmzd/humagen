import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { type ActionPolicy, api } from "./api.ts";
import { field, Label, Page, Problem, primary } from "./ui.tsx";

const words = defaultCopy.operations;
type Level = "allow" | "ask" | "never";
const rank = (level: string | undefined) =>
  level === "never" ? 3 : level === "ask" ? 2 : level === "allow" ? 1 : 0;

export function ActionPolicies({
  scope = "workspace",
  name = "",
  canWrite,
}: {
  scope?: "workspace" | "agent" | "project" | "personal";
  name?: string;
  canWrite: boolean;
}) {
  const [policy, setPolicy] = useState<ActionPolicy | null>(null);
  const [rules, setRules] = useState<Record<string, Level>>({});
  const [problem, setProblem] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    let live = true;
    setPolicy(null);
    setProblem(null);
    api
      .actionPolicy(scope, name)
      .then((p) => {
        if (live) {
          setPolicy(p);
          setRules(p.rules);
        }
      })
      .catch(() => {
        if (live) setProblem(words.loadFailed);
      });
    return () => {
      live = false;
    };
  }, [scope, name]);
  const save = async () => {
    if (!policy || busy || !canWrite) return;
    setBusy(true);
    setProblem(null);
    try {
      const p = await api.saveActionPolicy(scope, name, { rules, version: policy.version });
      setPolicy(p);
      setRules(p.rules);
    } catch (e) {
      setProblem(e instanceof Error ? e.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Page title={words.policies} lede={words.policiesHint}>
      <Problem message={problem} />
      {!policy ? (
        <output>{words.loading}</output>
      ) : (
        <form
          className="flex max-w-xl flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            void save();
          }}
        >
          <p className="text-aui-xs text-aui-muted">{words.policyContext}</p>
          {Object.entries(words.categories).map(([category, label]) => {
            const effective = policy.effective?.[category];
            const inherited = effective?.inherited;
            const selected =
              rules[category] && rank(rules[category]) < rank(inherited)
                ? inherited
                : rules[category];
            return (
              <div key={category} className="flex flex-col gap-2">
                <Label text={label}>
                  <select
                    className={field}
                    value={selected ?? ""}
                    disabled={!canWrite || busy || inherited === "never"}
                    onChange={(e) => {
                      const next = { ...rules };
                      if (e.target.value) next[category] = e.target.value as Level;
                      else delete next[category];
                      setRules(next);
                    }}
                  >
                    <option value="">{words.inherited}</option>
                    <option value="allow" disabled={rank(inherited) > rank("allow")}>
                      {words.allow}
                    </option>
                    <option value="ask" disabled={rank(inherited) > rank("ask")}>
                      {words.ask}
                    </option>
                    <option value="never">{words.never}</option>
                  </select>
                </Label>
                {effective?.level && (
                  <p className="text-aui-xs text-aui-muted">
                    {words.effective}: {words[effective.level]}
                  </p>
                )}
                {effective?.locked && (
                  <p className="text-aui-xs text-aui-muted">
                    {words.lockedBy}:{" "}
                    {effective.sources
                      .map((source) => source.label || words.scopeKinds[source.kind])
                      .join(", ")}
                  </p>
                )}
              </div>
            );
          })}
          <button type="submit" className={primary} disabled={!canWrite || busy}>
            {busy ? words.saving : words.save}
          </button>
        </form>
      )}
    </Page>
  );
}
