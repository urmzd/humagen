import { defaultCopy } from "@humagen/components";
import { BookmarkIcon, WrenchIcon } from "@humagen/render-web";
import { Inspector, messageTextActionButton } from "@humagen/web";
import { type FormEvent, useRef, useState } from "react";
import type {
  LibraryScope,
  LibrarySkillDefinition,
  OutcomeLearningPreview,
} from "../../../packages/client/src/generated/types.ts";
import type { Run } from "./adapter.ts";
import { ApiError, api } from "./api.ts";
import { href } from "./route.ts";
import { button, field, Label, Problem, primary } from "./ui.tsx";

const copy = defaultCopy.outcomeLearning;
const blank: LibrarySkillDefinition = {
  name: "",
  description: "",
  instructions: "",
  tools: [],
  inputs: "",
  outputs: "",
};
const memoryText = (text: string) => {
  const line = text
    .trim()
    .replace(/\s+/g, " ")
    .replace(/^[-*• ]+/, "");
  return new TextEncoder().encode(line).length <= 300 ? line : "";
};

/** Explicit human learning from an authored outcome, never action approval. */
export function OutcomeActions({ run, canWrite }: { run: Run; canWrite: boolean }) {
  const [action, setAction] = useState<"skill" | "memory" | null>(null);
  const [scope, setScope] = useState<"personal" | "project">("personal");
  const [preview, setPreview] = useState<OutcomeLearningPreview>();
  const [definition, setDefinition] = useState<LibrarySkillDefinition>(blank);
  const [decision, setDecision] = useState(0);
  const [text, setText] = useState("");
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [saved, setSaved] = useState<{
    kind: "skill" | "memory";
    scope: LibraryScope;
    id: string;
  }>();
  const request = useRef(0);
  const fail = (error: unknown) =>
    setProblem(
      error instanceof ApiError && error.status === 409
        ? copy.stale
        : error instanceof Error
          ? error.message
          : copy.failed,
    );
  const load = (next: "personal" | "project", seed = false) => {
    const current = ++request.current;
    setLoading(true);
    setProblem(null);
    setPreview(undefined);
    api
      .outcomeLearning(run.id, next)
      .then((value) => {
        if (current !== request.current) return;
        setPreview(value);
        if (seed) {
          setDefinition({
            ...blank,
            description: Array.from(value.outcome.summary.replace(/\s+/g, " "))
              .slice(0, 1024)
              .join(""),
            instructions: [value.outcome.summary, value.outcome.next_action ?? ""]
              .filter(Boolean)
              .join("\n\n"),
          });
          const index = Math.max(
            (value.outcome.decisions ?? []).findIndex((item) => item.trim() !== ""),
            0,
          );
          setDecision(index);
          setText(memoryText(value.outcome.decisions?.[index] ?? ""));
        }
      })
      .catch((error) => {
        if (current === request.current) fail(error);
      })
      .finally(() => {
        if (current === request.current) setLoading(false);
      });
  };
  const open = (kind: "skill" | "memory") => {
    setAction(kind);
    setScope("personal");
    setSaved(undefined);
    setDefinition(blank);
    setText("");
    load("personal", true);
  };
  const close = () => {
    if (busy) return;
    request.current++;
    setAction(null);
    setPreview(undefined);
    setProblem(null);
  };
  const save = async (event: FormEvent) => {
    event.preventDefault();
    if (!preview || busy || loading || !action) return;
    setBusy(true);
    setProblem(null);
    try {
      const result =
        action === "skill"
          ? await api.draftOutcomeSkill(run.id, {
              snapshot: preview.snapshot,
              scope: preview.scope,
              definition: { ...definition, tools: [] },
            })
          : await api.rememberOutcomeDecision(run.id, {
              snapshot: preview.snapshot,
              scope: preview.scope,
              decision,
              text,
            });
      setSaved({ kind: action, scope: preview.scope, id: result.id });
    } catch (error) {
      fail(error);
    } finally {
      setBusy(false);
    }
  };
  if (!canWrite || run.status !== "completed" || !run.outcome) return null;
  return (
    <>
      <button
        type="button"
        className={messageTextActionButton}
        disabled={busy || action !== null}
        onClick={() => open("skill")}
      >
        <WrenchIcon size={16} />
        {copy.skill}
      </button>
      {run.outcome.decisions?.some((item) => item.trim() !== "") && (
        <button
          type="button"
          className={messageTextActionButton}
          disabled={busy || action !== null}
          onClick={() => open("memory")}
        >
          <BookmarkIcon size={16} />
          {copy.remember}
        </button>
      )}
      <Inspector
        open={action !== null}
        title={action === "skill" ? copy.skillTitle : copy.decisionTitle}
        onClose={close}
      >
        <div className="flex min-w-0 flex-col gap-4 p-4">
          <p className="text-aui-sm text-aui-muted">{copy.reviewHint}</p>
          {action === "memory" && (
            <p className="text-aui-sm text-aui-muted">{copy.noExternalApproval}</p>
          )}
          <Problem message={problem} />
          {problem && (
            <button
              type="button"
              className={`${button} self-start`}
              disabled={busy || loading}
              onClick={() => load(scope)}
            >
              {copy.retry}
            </button>
          )}
          {loading && (
            <p role="status" className="text-aui-sm">
              {copy.loading}
            </p>
          )}
          {saved ? (
            <div role="status" className="flex flex-col gap-3">
              <p>{saved.kind === "skill" ? copy.skillSaved : copy.memorySaved}</p>
              {saved.kind === "skill" && (
                <p className="text-aui-sm text-aui-muted">{copy.draftHint}</p>
              )}
              <a
                className="min-h-9 text-aui-accent underline"
                href={
                  saved.kind === "skill"
                    ? href({
                        page: "library",
                        section: "skills",
                        scope: saved.scope.kind as "personal" | "project",
                        project: saved.scope.project,
                        skill: saved.id,
                      })
                    : href({
                        page: "library",
                        section: "memory",
                        scope: saved.scope.kind as "personal" | "project",
                        owner: saved.scope.project,
                      })
                }
              >
                {saved.kind === "skill" ? copy.openSkill : copy.openMemory}
              </a>
              <button type="button" className={`${button} self-start`} onClick={close}>
                {copy.close}
              </button>
            </div>
          ) : (
            preview && (
              <form onSubmit={save} className="flex min-w-0 flex-col gap-4">
                <Label text={copy.scope}>
                  <select
                    aria-label={copy.scope}
                    className={field}
                    value={scope}
                    disabled={busy}
                    onChange={(event) => {
                      const next = event.target.value as "personal" | "project";
                      setScope(next);
                      load(next);
                    }}
                  >
                    <option value="personal">{copy.personal}</option>
                    {preview.may_project && (
                      <option value="project">{preview.project_title || copy.project}</option>
                    )}
                  </select>
                </Label>
                {!preview.may_project && (
                  <p className="text-aui-xs text-aui-muted">{copy.noProject}</p>
                )}
                {action === "skill" ? (
                  <>
                    {(
                      [
                        ["name", copy.name, 64],
                        ["description", copy.purpose, 1024],
                        ["instructions", copy.steps, 8000],
                        ["inputs", copy.inputs, 4000],
                        ["outputs", copy.outputs, 4000],
                      ] as const
                    ).map(([key, label, maxLength]) => (
                      <Label key={key} text={label}>
                        {key === "name" || key === "description" ? (
                          <input
                            aria-label={label}
                            className={field}
                            maxLength={maxLength}
                            value={definition[key] ?? ""}
                            required
                            disabled={busy}
                            onChange={(event) =>
                              setDefinition({ ...definition, [key]: event.target.value })
                            }
                          />
                        ) : (
                          <textarea
                            aria-label={label}
                            className={`${field} min-h-24`}
                            maxLength={maxLength}
                            value={definition[key] ?? ""}
                            required={key === "instructions"}
                            disabled={busy}
                            onChange={(event) =>
                              setDefinition({ ...definition, [key]: event.target.value })
                            }
                          />
                        )}
                      </Label>
                    ))}
                    <p className="text-aui-xs text-aui-muted">{copy.draftHint}</p>
                  </>
                ) : (
                  <>
                    <Label text={copy.decision}>
                      <select
                        aria-label={copy.decision}
                        className={field}
                        value={decision}
                        disabled={busy}
                        onChange={(event) => {
                          const next = Number(event.target.value);
                          setDecision(next);
                          setText(memoryText(preview.outcome.decisions?.[next] ?? ""));
                        }}
                      >
                        {preview.outcome.decisions?.map(
                          (item, index) =>
                            item.trim() !== "" && (
                              <option key={index} value={index}>
                                {item}
                              </option>
                            ),
                        )}
                      </select>
                    </Label>
                    <blockquote className="whitespace-pre-wrap break-words text-aui-sm">
                      {preview.outcome.decisions?.[decision]}
                    </blockquote>
                    <Label text={copy.text}>
                      <textarea
                        aria-label={copy.text}
                        className={`${field} min-h-28`}
                        maxLength={300}
                        value={text}
                        required
                        disabled={busy}
                        onChange={(event) => setText(event.target.value)}
                      />
                    </Label>
                  </>
                )}
                <div className="flex flex-wrap gap-2">
                  <button type="button" className={button} disabled={busy} onClick={close}>
                    {copy.cancel}
                  </button>
                  <button
                    type="submit"
                    className={primary}
                    disabled={
                      busy ||
                      loading ||
                      (action === "skill" ? !preview.can_draft : !preview.can_remember)
                    }
                  >
                    {busy
                      ? copy.saving
                      : action === "skill"
                        ? copy.createDraft
                        : copy.approveRemember}
                  </button>
                </div>
              </form>
            )
          )}
        </div>
      </Inspector>
    </>
  );
}
