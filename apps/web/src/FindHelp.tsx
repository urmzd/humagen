import { defaultCopy } from "@humagen/components";
import { useRef, useState } from "react";
import type { HelpSuggestion } from "../../../packages/client/src/generated/types.ts";
import { AgentMark, field, textAction } from "./ui.tsx";

export type { HelpSuggestion } from "../../../packages/client/src/generated/types.ts";

/** Discovery prepares the current composer; searching never starts work. */
export function FindHelp({ onSearch, onChoose, initialDescription = "", canChoose = true }: {
  onSearch: (description: string) => Promise<HelpSuggestion[]>;
  onChoose: (suggestion: HelpSuggestion, description: string) => void;
  initialDescription?: string;
  canChoose?: boolean;
}) {
  const c = defaultCopy.home.findHelp;
  const [description, setDescription] = useState(initialDescription.slice(0, 200));
  const [results, setResults] = useState<HelpSuggestion[] | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const serial = useRef(0);
  const search = async () => {
    const query = description.trim();
    if (!query || pending) return;
    const request = ++serial.current;
    setPending(true);
    setError(false);
    try {
      const found = await onSearch(query);
      if (request === serial.current) setResults(found);
    } catch {
      if (request === serial.current) setError(true);
    } finally {
      if (request === serial.current) setPending(false);
    }
  };
  return (
    <section aria-label={c.title} className="min-w-0 px-2.5 pb-2">
      <h2 className="text-aui-sm font-medium text-aui-fg">{c.title}</h2>
      <p className="mt-1 mb-3 text-aui-xs text-aui-muted">{c.description}</p>
      <div role="search" className="flex min-w-0 flex-col gap-2">
        <label className="flex min-w-0 flex-col gap-1 text-aui-sm">
          {c.label}
          <input
            className={`${field} min-w-0`}
            value={description}
            maxLength={200}
            placeholder={c.placeholder}
            onChange={(event) => {
              serial.current++;
              setPending(false);
              setError(false);
              setResults(null);
              setDescription(event.target.value);
            }}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.nativeEvent.isComposing) {
                event.preventDefault();
                event.stopPropagation();
                void search();
              }
            }}
          />
        </label>
        <button type="button" disabled={pending || !description.trim()} className={`${textAction} min-h-10 self-start`} onClick={() => void search()}>
          {pending ? c.searching : c.search}
        </button>
      </div>
      {pending && <p role="status" className="mt-3 text-aui-sm text-aui-muted">{c.searching}</p>}
      {error && <p role="alert" className="mt-3 text-aui-sm text-aui-muted">{c.failed}</p>}
      {!pending && !error && results?.length === 0 && <p className="mt-3 text-aui-sm text-aui-muted">{c.empty}</p>}
      {!pending && !error && results && results.length > 0 && (
        <ul className="mt-3 divide-y divide-aui-border">
          {results.map((result) => (
            <li key={`${result.project}:${result.agent}`} className="flex min-w-0 flex-wrap items-start gap-2 py-3">
              <AgentMark name={result.agent} />
              <div className="min-w-0 flex-1">
                <p className="break-words text-aui-sm text-aui-fg">{result.title} <span className="text-aui-muted">{result.agent}</span></p>
                <p className="mt-1 text-aui-xs text-aui-muted">{result.reason}</p>
                {canChoose && <button type="button" className={`${textAction} mt-2 min-h-10`} onClick={() => onChoose(result, description.trim())}>{c.choose}</button>}
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
