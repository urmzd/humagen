import { defaultCopy } from "@humagen/components";
import { useRef, useState } from "react";
import type { HelpSuggestion } from "../../../packages/client/src/generated/types.ts";
import { AgentMark, textAction } from "./ui.tsx";

export type { HelpSuggestion } from "../../../packages/client/src/generated/types.ts";

/** Discovery only. Choosing a suggestion never publishes an existing chat. */
export function FindHelp({ onSearch, onChoose, onOpen }: {
  onSearch: (description: string) => Promise<HelpSuggestion[]>;
  onChoose: (suggestion: HelpSuggestion) => void;
  onOpen: (project: string) => void;
}) {
  const c = defaultCopy.home.findHelp;
  const [description, setDescription] = useState("");
  const [results, setResults] = useState<HelpSuggestion[] | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState(false);
  const serial = useRef(0);
  return <section aria-label={c.title} className="mx-auto w-full max-w-3xl px-4">
    <details>
      <summary className={`${textAction} cursor-pointer py-2`}>{c.title}</summary>
      <p className="mb-3 text-aui-sm text-aui-muted">{c.description}</p>
      <form className="flex flex-col gap-2 sm:flex-row sm:items-end" onSubmit={async (event) => {
        event.preventDefault();
        const query = description.trim();
        if (!query || pending) return;
        const request = ++serial.current;
        setPending(true); setError(false);
        try { const found = await onSearch(query); if (request === serial.current) setResults(found); }
        catch { if (request === serial.current) setError(true); }
        finally { if (request === serial.current) setPending(false); }
      }}>
        <label className="flex min-w-0 flex-1 flex-col gap-1 text-aui-sm">
          {c.label}
          <input className="min-w-0 rounded-aui border border-aui-line bg-aui-bg px-3 py-2 text-aui-fg" value={description} maxLength={200} placeholder={c.placeholder} onChange={(event) => { serial.current++; setPending(false); setError(false); setResults(null); setDescription(event.target.value); }} />
        </label>
        <button type="submit" disabled={pending || !description.trim()} className={`${textAction} min-h-10 px-2`}>{pending ? c.searching : c.search}</button>
      </form>
      {pending && <p role="status" className="mt-3 text-aui-sm text-aui-muted">{c.searching}</p>}
      {error && <p role="alert" className="mt-3 text-aui-sm text-aui-muted">{c.failed}</p>}
      {!pending && !error && results?.length === 0 && <p className="mt-3 text-aui-sm text-aui-muted">{c.empty}</p>}
      {!pending && !error && results && results.length > 0 && <ul className="mt-3 divide-y divide-aui-line">
        {results.map((result) => <li key={`${result.project}:${result.agent}`} className="flex flex-wrap items-start gap-3 py-3">
          <AgentMark name={result.agent} />
          <div className="min-w-0 flex-1"><p className="break-words text-aui-sm text-aui-fg">{result.title} <span className="text-aui-muted">{result.agent}</span></p><p className="mt-1 text-aui-sm text-aui-muted">{result.reason}</p></div>
          <div className="flex flex-wrap gap-4"><button type="button" className={textAction} onClick={() => onChoose(result)}>{c.choose}</button><button type="button" className={textAction} onClick={() => onOpen(result.project)}>{c.open}</button></div>
        </li>)}
      </ul>}
    </details>
  </section>;
}
