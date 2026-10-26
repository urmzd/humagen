import { defaultCopy } from "@humagen/components";
import { useEffect, useRef, useState } from "react";
import { type AgentControl, api } from "./api.ts";
import { OperationPersonField } from "./OperationPersonField.tsx";
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
  const [reminder, setReminder] = useState(240);
  const [blockedReminder, setBlockedReminder] = useState(120);
  const [reminderChanged, setReminderChanged] = useState(false);
  const [baselineVersion, setBaselineVersion] = useState(0);
  const [dirty, setDirty] = useState(false);
  const dirtyRef = useRef(false);
  const refreshEpoch = useRef(0);
  const changed = () => {
    dirtyRef.current = true;
    setDirty(true);
  };
  const adopt = (c: AgentControl) => {
    setManager(c.manager ?? "");
    setStopRules(c.stop_rules ?? "");
    setReminder((c.reminder_after_seconds ?? c.effective_reminder_after_seconds ?? 14400) / 60);
    setBlockedReminder(
      (c.blocked_reminder_after_seconds ?? c.effective_blocked_reminder_after_seconds ?? 7200) / 60,
    );
    setBaselineVersion(c.version);
    setReminderChanged(false);
    dirtyRef.current = false;
    setDirty(false);
  };
  const selected = useRef(agent);
  selected.current = agent;
  useEffect(() => {
    let live = true;
    setControl(null);
    setBusy(false);
    setReminderChanged(false);
    setProblem(null);
    dirtyRef.current = false;
    setDirty(false);
    const refresh = async () => {
      const epoch = refreshEpoch.current;
      try {
        const c = await api.agentControl(agent);
        if (!live || epoch !== refreshEpoch.current) return;
        setControl(c);
        if (!dirtyRef.current) adopt(c);
        setProblem(null);
      } catch {
        if (live) setProblem(words.loadFailed);
      }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 5000);
    return () => {
      live = false;
      window.clearInterval(timer);
    };
  }, [agent]);
  const save = async (paused: boolean) => {
    if (!control || busy || !canWrite || (dirty && control.version !== baselineVersion)) return;
    if (
      !Number.isInteger(reminder) ||
      !Number.isInteger(blockedReminder) ||
      reminder < 0 ||
      reminder > 10080 ||
      blockedReminder < 0 ||
      blockedReminder > 10080
    ) {
      setProblem(words.reminderInvalid);
      return;
    }
    refreshEpoch.current += 1;
    setBusy(true);
    setProblem(null);
    try {
      const c = await api.saveAgentControl(agent, {
        paused,
        manager,
        stop_rules: stopRules,
        version: baselineVersion,
        ...(reminderChanged
          ? {
              reminder_after_seconds: Math.round(reminder * 60),
              blocked_reminder_after_seconds: Math.round(blockedReminder * 60),
            }
          : {}),
      });
      if (selected.current !== agent) return;
      setControl(c);
      adopt(c);
      onChanged?.();
    } catch (e) {
      if (selected.current === agent) setProblem(e instanceof Error ? e.message : words.failed);
    } finally {
      if (selected.current === agent) setBusy(false);
    }
  };
  const stale = !!control && dirty && control.version !== baselineVersion;
  return (
    <Page
      title={agent ? words.controls : words.workspaceControls}
      icon={agent ? <AgentMark name={agent} /> : undefined}
      lede={words.controlsHint}
    >
      <Problem message={problem} />
      {stale && control && (
        <button type="button" className={primary} onClick={() => adopt(control)}>
          {words.reviewLatest}
        </button>
      )}
      {!control ? (
        <output>{words.loading}</output>
      ) : (
        <div className="flex max-w-xl flex-col gap-5">
          <output className="text-aui-sm text-aui-fg">
            {(control.effective_paused ?? control.paused) ? words.paused : words.active}
          </output>
          {agent && control.needs_skill_review && (
            <div className="grid gap-2">
              <p className="text-aui-sm">{defaultCopy.agentSkillDefaults.needsReview}</p>
              <a
                className="inline-flex min-h-11 items-center text-aui-accent"
                href={`/agents/${encodeURIComponent(agent)}/context`}
              >
                {defaultCopy.agentSkillDefaults.review}
              </a>
            </div>
          )}
          {control.pause_reason && (
            <p className="text-aui-sm text-aui-muted">{words.pauseReasons[control.pause_reason]}</p>
          )}
          <button
            type="button"
            className={primary}
            disabled={!canWrite || busy || stale}
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
          <Label text={words.unansweredReminder}>
            <input
              type="number"
              min={0}
              max={10080}
              step={1}
              className={field}
              value={reminder}
              disabled={!canWrite || busy}
              onChange={(event) => {
                setReminder(Number(event.target.value));
                setReminderChanged(true);
                changed();
              }}
            />
          </Label>
          <Label text={words.blockedReminder}>
            <input
              type="number"
              min={0}
              max={10080}
              step={1}
              className={field}
              value={blockedReminder}
              disabled={!canWrite || busy}
              onChange={(event) => {
                setBlockedReminder(Number(event.target.value));
                setReminderChanged(true);
                changed();
              }}
            />
          </Label>
          <p className="text-aui-xs text-aui-muted">{words.reminderHint}</p>
          {!agent && (
            <button
              type="button"
              className={primary}
              disabled={!canWrite || busy || stale}
              onClick={() => void save(control.paused)}
            >
              {busy ? words.saving : words.save}
            </button>
          )}
          {agent && (
            <>
              <OperationPersonField
                label={words.manager}
                value={manager}
                displayName={control.manager_name}
                disabled={!canWrite || busy}
                onChange={(value) => {
                  setManager(value);
                  changed();
                }}
              />
              <Label text={words.stopRules}>
                <textarea
                  className={field}
                  value={stopRules}
                  maxLength={4000}
                  disabled={!canWrite || busy}
                  onChange={(e) => {
                    setStopRules(e.target.value);
                    changed();
                  }}
                />
              </Label>
              <p className="text-aui-xs text-aui-muted">{words.stopRulesHint}</p>
              <button
                type="button"
                className={primary}
                disabled={!canWrite || busy || stale}
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
