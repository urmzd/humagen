import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { type ActionPolicy, api } from "./api.ts";
import { Page, Problem, SkeletonRows } from "./ui.tsx";
import { Button } from "@humagen/render-web/ui/button";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Badge } from "@humagen/render-web/ui/badge";

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
  const [refresh, setRefresh] = useState(0);
  const [saved, setSaved] = useState(false);
  const dirty = !!policy && JSON.stringify(rules) !== JSON.stringify(policy.rules);
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
  }, [scope, name, refresh]);
  const save = async () => {
    if (!policy || busy || !canWrite || !dirty) return;
    setBusy(true);
    setProblem(null);
    setSaved(false);
    try {
      const saved = await api.saveActionPolicy(scope, name, { rules, version: policy.version });
      setPolicy(saved);
      setRules(saved.rules);
      setSaved(true);
    } catch (error) {
      setProblem(error instanceof Error ? error.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <Page title={words.policies} lede={words.policiesHint} narrow>
      <div className="flex flex-wrap items-center gap-3 text-sm">
        <span className="text-muted-foreground">{words.policyScope}</span>
        <Badge variant="secondary">
          {scope === "workspace" ? defaultCopy.memoryLibrary.workspace : words.scopeKinds[scope]}
        </Badge>
        {name && <span className="font-medium">{name}</span>}
      </div>
      <Problem message={problem} />
      {!policy ? (
        problem ? (
          <Button variant="outline" className="self-start" onClick={() => setRefresh((n) => n + 1)}>
            {defaultCopy.retry}
          </Button>
        ) : (
          <SkeletonRows count={4} />
        )
      ) : (
        <form
          className="flex min-w-0 flex-col gap-5"
          onSubmit={(event) => {
            event.preventDefault();
            void save();
          }}
        >
          <p className="max-w-[70ch] text-sm leading-6 text-muted-foreground">
            {words.policyContext}
          </p>
          <div className="divide-y divide-border border-y border-border">
            {Object.entries(words.categories).map(([category, label]) => {
              const effective = policy.effective?.[category];
              const inherited = effective?.inherited;
              const selected =
                rules[category] && rank(rules[category]) < rank(inherited)
                  ? inherited
                  : rules[category];
              const id = `policy-${category}`;
              return (
                <div
                  key={category}
                  className="grid min-w-0 gap-3 py-4 sm:grid-cols-[minmax(0,1fr)_12rem] sm:gap-6"
                >
                  <div className="min-w-0">
                    <label htmlFor={id} className="text-sm font-medium">
                      {label}
                    </label>
                    <p
                      id={`${id}-hint`}
                      className="mt-1 max-w-[52ch] text-sm leading-5 text-muted-foreground"
                    >
                      {words.categoryHints[category as keyof typeof words.categoryHints]}
                    </p>
                  </div>
                  <div className="flex min-w-0 flex-col gap-2">
                    <NativeSelect
                      id={id}
                      aria-describedby={`${id}-hint`}
                      value={selected ?? ""}
                      disabled={!canWrite || busy || inherited === "never"}
                      onChange={(event) => {
                        const next = { ...rules };
                        if (event.target.value) next[category] = event.target.value as Level;
                        else delete next[category];
                        setRules(next);
                        setSaved(false);
                      }}
                    >
                      <option value="">
                        {words.inherited}
                        {inherited ? `: ${words[inherited]}` : ""}
                      </option>
                      <option value="allow" disabled={rank(inherited) > rank("allow")}>
                        {words.allow}
                      </option>
                      <option value="ask" disabled={rank(inherited) > rank("ask")}>
                        {words.ask}
                      </option>
                      <option value="never">{words.never}</option>
                    </NativeSelect>
                    {effective?.level && (
                      <p className="text-xs text-muted-foreground">
                        {words.effective}:{" "}
                        <span className="font-medium text-foreground">
                          {words[effective.level]}
                        </span>
                      </p>
                    )}
                    {effective?.locked && (
                      <p className="text-xs text-muted-foreground">
                        {words.lockedBy}:{" "}
                        {effective.sources
                          .map((source) => source.label || words.scopeKinds[source.kind])
                          .join(", ")}
                      </p>
                    )}
                  </div>
                </div>
              );
            })}
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={!canWrite || busy || !dirty}>
              {busy ? words.saving : words.save}
            </Button>
            {dirty && (
              <>
                <Button variant="ghost" disabled={busy} onClick={() => setRules(policy.rules)}>
                  {words.policyReset}
                </Button>
                <span className="text-xs text-muted-foreground">{words.policyDraft}</span>
              </>
            )}
            {saved && (
              <p role="status" className="text-sm text-muted-foreground">
                {words.policySaved}
              </p>
            )}
          </div>
        </form>
      )}
    </Page>
  );
}
