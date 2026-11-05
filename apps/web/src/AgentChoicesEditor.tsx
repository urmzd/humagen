import { defaultCopy } from "@humagen/components";
import { useId } from "react";
import type { AgentChoices } from "./api.ts";
import { field, Label, Section } from "./ui.tsx";

const words = defaultCopy.agentChoices;

/** Keep the draft as typed; canonicalize only when saving the revision. */
export function cleanChoices(choices?: AgentChoices): AgentChoices | undefined {
  const models = [...new Set((choices?.models ?? []).map((s) => s.trim()).filter(Boolean))].sort();
  const efforts = [
    ...new Set((choices?.efforts ?? []).map((s) => s.trim().toLowerCase()).filter(Boolean)),
  ].sort();
  return models.length || efforts.length
    ? { ...(models.length ? { models } : {}), ...(efforts.length ? { efforts } : {}) }
    : undefined;
}

export function choicesProblem(choices?: AgentChoices): string | null {
  const next = cleanChoices(choices);
  if (next?.models?.some((model) => !/^[^/\s]+\/\S+$/.test(model))) return words.modelProblem;
  if (next?.efforts?.some((effort) => !/^[a-z]{1,32}$/.test(effort))) return words.effortProblem;
  return null;
}

/** The exact model and effort allowlists offered by the message composer. */
export function AgentChoicesEditor({
  choices,
  editable,
  onChange,
}: {
  choices?: AgentChoices;
  editable: boolean;
  onChange: (choices: AgentChoices) => void;
}) {
  const id = useId();
  const problem = choicesProblem(choices);
  return (
    <Section title={words.title} hint={words.hint}>
      <div className="grid min-w-0 grid-cols-1 gap-4 sm:grid-cols-2">
        <Label text={words.models}>
          <textarea
            className={`${field} min-h-28 font-aui-mono`}
            aria-label={words.models}
            aria-describedby={`${id}-models`}
            aria-invalid={problem === words.modelProblem || undefined}
            disabled={!editable}
            spellCheck={false}
            value={(choices?.models ?? []).join("\n")}
            onChange={(event) => onChange({ ...choices, models: event.target.value.split("\n") })}
          />
          <span id={`${id}-models`} className="text-aui-xs text-aui-muted">
            {words.modelsHint}
          </span>
        </Label>
        <Label text={words.efforts}>
          <textarea
            className={`${field} min-h-28 font-aui-mono`}
            aria-label={words.efforts}
            aria-describedby={`${id}-efforts`}
            aria-invalid={problem === words.effortProblem || undefined}
            disabled={!editable}
            spellCheck={false}
            value={(choices?.efforts ?? []).join("\n")}
            onChange={(event) => onChange({ ...choices, efforts: event.target.value.split("\n") })}
          />
          <span id={`${id}-efforts`} className="text-aui-xs text-aui-muted">
            {words.effortsHint}
          </span>
        </Label>
      </div>
      {problem && (
        <p role="alert" className="text-aui-xs text-aui-bad">
          {problem}
        </p>
      )}
    </Section>
  );
}
