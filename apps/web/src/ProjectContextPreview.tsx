import { defaultCopy } from "@humagen/components";
import { type FormEvent, useEffect, useRef, useState } from "react";
import { ApiError, api, type ProjectContext } from "./api.ts";
import { AgentMark, field, Label, primary, Problem } from "./ui.tsx";

const words = defaultCopy.projects.context.preview;

/** A preview comes from the live composer, with the reader's confinement. */
export function ProjectContextPreview({ project, version, initialAgent = "", initialChannel = "" }: { project: string; version: number; initialAgent?: string; initialChannel?: string }) {
  const [agent, setAgent] = useState(initialAgent);
  const [channel, setChannel] = useState(initialChannel);
  const [preview, setPreview] = useState<ProjectContext | null>(null);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const request = useRef(0);
  // The request belongs to these saved settings and these selections.
  // biome-ignore lint/correctness/useExhaustiveDependencies: changes invalidate the result
  useEffect(() => {
    request.current++;
    setPreview(null);
    setProblem(null);
    setBusy(false);
    return () => {
      request.current++;
    };
  }, [project, version, agent, channel]);
  const read = (e: FormEvent) => {
    e.preventDefault();
    if (busy) return;
    const id = ++request.current;
    setBusy(true);
    setProblem(null);
    setPreview(null);
    api
      .projectContext(project, agent.trim(), channel.trim())
      .then((value) => {
        if (request.current === id) setPreview(value);
      })
      .catch((e: unknown) => {
        if (request.current === id) setProblem(e instanceof ApiError ? e.message : words.failed);
      })
      .finally(() => {
        if (request.current === id) setBusy(false);
      });
  };
  return (
    <section className="flex flex-col gap-3" aria-label={words.title}>
      <div>
        <h2 className="text-aui-base font-medium text-aui-fg">{words.title}</h2>
        <p className="text-aui-xs text-aui-muted">{words.hint}</p>
      </div>
      <form onSubmit={read} className="flex flex-wrap items-end gap-2">
        <Label text={words.agent}>
          <input
            className={field}
            aria-label={words.agent}
            value={agent}
            maxLength={63}
            required
            onChange={(e) => {
              setAgent(e.target.value);
              setPreview(null);
            }}
          />
        </Label>
        <Label text={words.channel}>
          <input
            className={field}
            aria-label={words.channel}
            value={channel}
            maxLength={63}
            onChange={(e) => {
              setChannel(e.target.value);
              setPreview(null);
            }}
          />
        </Label>
        <button type="submit" className={primary} disabled={busy || !agent.trim()}>
          {busy ? words.loading : words.submit}
        </button>
      </form>
      <Problem message={problem} />
      {preview && (
        <ol className="flex flex-col gap-3">
          {preview.layers.map((layer, index) => (
            <li key={`${layer.kind}:${layer.from}:${index}`} className="min-w-0 border-aui-border border-t pt-3">
              <h3 className="flex flex-wrap items-center gap-1 text-aui-sm font-medium text-aui-fg">
                {words.kinds[layer.kind as keyof typeof words.kinds] ?? layer.kind}
                {layer.from && (
                  <span className="inline-flex items-center gap-1">
                    <span aria-hidden>·</span>
                    {layer.kind.startsWith("agent_") && <AgentMark name={layer.from} size={12} />}
                    {layer.from}
                  </span>
                )}
              </h3>
              <p className="text-aui-xs text-aui-muted">
                {words.usage(layer.used, layer.budget, layer.unit)}
                {layer.omitted > 0 ? ` · ${words.omitted(layer.omitted)}` : ""}
                {layer.preview_omitted > 0 ? ` · ${words.previewOmitted(layer.preview_omitted)}` : ""}
                {layer.kind === "skills" ? ` · ${words.available}` : !layer.sent ? ` · ${words.notSent}` : ""}
              </p>
              {layer.text && <p className="whitespace-pre-wrap break-words text-aui-sm text-aui-fg">{layer.text}</p>}
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}
