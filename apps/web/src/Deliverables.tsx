import { defaultCopy } from "@humagen/components";
import { Markdown } from "@humagen/primitives";
import { ArtifactIcon, WebSurface } from "@humagen/render-web";
import { HumagenMark, Inspector } from "@humagen/web";
import { useEffect, useRef, useState } from "react";
import { ArtifactPromotion, type ArtifactPromotionAccess } from "./ArtifactPromotion.tsx";
import type { FilePreview } from "./artifactPreview.ts";
import { appendPage, contentUrl, type Stored } from "./storage.ts";
import { AgentMark, ago, button, primary, Problem, SkeletonRows } from "./ui.tsx";
import { href, type ArtifactTab, type ConversationView } from "./route.ts";

const words = defaultCopy.conversation;
const at = (id: string) => `/api/artifacts/${encodeURIComponent(id)}`;
export function revisionPrompt(file: Stored, slide?: number): string {
  const target = href({
    page: "conversation",
    id: file.thread,
    panel: "deliverables",
    artifact: file.id,
  });
  return `@${file.agent} please revise [this artifact, v${file.version}${slide ? `, slide ${slide}` : ""}](${target}).${words.artifactReadSource(file.id,file.version)} Requested changes: `;
}
function ExportPDF({file}:{file:Stored}) {
  const [busy,setBusy]=useState(false);
  const [problem,setProblem]=useState("");
  if (!file.mime.includes("openxmlformats")) return null;
  const download=async()=>{
    setBusy(true);setProblem("");
    try {
      const response=await fetch(`/api/artifacts/${encodeURIComponent(file.id)}/pdf`,{credentials:"same-origin"});
      if (!response.ok) {const body=await response.json().catch(()=>null);throw new Error(body?.error || words.artifactPDFFailed)}
      const url=URL.createObjectURL(await response.blob());
      const link=document.createElement("a");link.href=url;link.download=file.title.replace(/\.[^.]+$/,"")+".pdf";document.body.append(link);link.click();link.remove();setTimeout(()=>URL.revokeObjectURL(url),1000);
    } catch(error) {setProblem(error instanceof Error ? error.message : words.artifactPDFFailed)}
    finally {setBusy(false)}
  };
  return <div className="flex flex-col gap-2"><button type="button" className={`${button} self-start`} disabled={busy} onClick={download}>{busy?words.artifactPDFBusy:words.artifactPDF}</button>{problem && <p role="alert" className="text-aui-xs text-aui-bad">{problem}</p>}</div>;
}

async function get<T>(path: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal, credentials: "same-origin" });
  if (!response.ok) throw new Error(words.artifactFailed);
  return response.json() as Promise<T>;
}

function SlideView({
  preview,
  slide,
}: {
  preview: Extract<FilePreview, { kind: "presentation" }>;
  slide: number;
}) {
  const value = preview.slides[slide];
  if (!value) return null;
  return (
    <div className="humagen-slide-frame">
      <section className="humagen-native-slide" aria-label={`Slide ${slide + 1}: ${value.title}`}>
        <div className="flex items-center gap-1.5 text-aui-accent">
          <HumagenMark size={16} />
          <span>Humagen Slide Preview</span>
        </div>
        <h3>{value.title}</h3>
        <div className="humagen-slide-copy">
          {value.paragraphs.map((paragraph, i) => (
            <p key={`${i}:${paragraph.slice(0, 30)}`}>{paragraph}</p>
          ))}
        </div>
        <footer>
          {slide + 1} / {preview.slides.length}
        </footer>
      </section>
    </div>
  );
}

export function NativePreview({
  file,
  preview,
  slide,
  onSlide,
}: {
  file: Stored;
  preview: FilePreview;
  slide: number;
  onSlide: (slide: number) => void;
}) {
  const presenting = useRef<HTMLDialogElement>(null);
  const notes = preview.kind === "presentation" ? (preview.slides[slide]?.notes ?? []) : [];
  if (preview.kind === "presentation")
    return (
      <div className="flex min-w-0 flex-col gap-3">
        <p className="text-aui-2xs text-aui-muted">
          Text and speaker notes from the saved deck. Download the original for its full layout and
          media.
        </p>
        <SlideView preview={preview} slide={slide} />
        <div className="flex flex-wrap items-center justify-between gap-2">
          <button
            type="button"
            className={button}
            disabled={slide === 0}
            onClick={() => onSlide(slide - 1)}
            aria-label="Previous slide"
          >
            ‹
          </button>
          <span className="text-aui-xs text-aui-muted">
            Slide {slide + 1} of {preview.slides.length}
          </span>
          <button
            type="button"
            className={button}
            disabled={slide >= preview.slides.length - 1}
            onClick={() => onSlide(slide + 1)}
            aria-label="Next slide"
          >
            ›
          </button>
        </div>
        <div className="flex max-w-full gap-1 overflow-x-auto" aria-label="Slides">
          {preview.slides.map((item, i) => (
            <button
              key={`${i}:${item.title}`}
              type="button"
              className={`${button} shrink-0 px-2`}
              aria-label={`Slide ${i + 1}: ${item.title}`}
              aria-current={slide === i ? "step" : undefined}
              onClick={() => onSlide(i)}
            >
              {i + 1}
            </button>
          ))}
        </div>
        {notes.length > 0 && (
          <details className="rounded-aui border border-aui-border p-3">
            <summary className="min-h-6 cursor-pointer text-aui-xs">Speaker notes</summary>
            <div className="mt-3 space-y-2 text-aui-xs">
              {notes.map((note, i) => (
                <p key={`${i}:${note.slice(0, 30)}`}>{note}</p>
              ))}
            </div>
          </details>
        )}
        {preview.truncated && (
          <p className="text-aui-xs text-aui-muted">
            Showing the first 80 slides. The download contains the whole presentation.
          </p>
        )}
        <button
          type="button"
          className={`${button} self-start`}
          onClick={() => presenting.current?.showModal()}
        >
          Present
        </button>
        <dialog
          ref={presenting}
          data-layer="presentation"
          aria-label={words.artifactPresentation(file.title)}
          className="fixed inset-0 m-0 h-dvh max-h-none w-screen max-w-none bg-[#11151b] p-4 text-white backdrop:bg-black lg:p-8"
          onCancel={(event) => {
            event.preventDefault();
            event.stopPropagation();
            presenting.current?.close();
          }}
          onKeyDown={(event) => {
            if (event.key === "Escape") event.stopPropagation();
            if (event.key === "ArrowRight") {
              event.preventDefault();
              onSlide(Math.min(preview.slides.length - 1, slide + 1));
            }
            if (event.key === "ArrowLeft") {
              event.preventDefault();
              onSlide(Math.max(0, slide - 1));
            }
          }}
        >
          <div className="flex h-full min-h-0 flex-col gap-4">
            <header className="flex shrink-0 items-center justify-between gap-3">
              <span className="truncate text-aui-xs">
                {file.title} v{file.version}
              </span>
              <button
                type="button"
                className="rounded-aui border border-white/30 px-3 py-1 text-aui-xs"
                onClick={() => presenting.current?.close()}
              >
                Exit Presentation
              </button>
            </header>
            <div className="flex min-h-0 flex-1 items-center justify-center">
              <div className="w-full max-w-6xl">
                <SlideView preview={preview} slide={slide} />
              </div>
            </div>
            <footer className="text-center text-aui-xs">
              Slide {slide + 1} of {preview.slides.length} ← →
            </footer>
          </div>
        </dialog>
      </div>
    );
  if (preview.kind === "document")
    return (
      <article className="rounded-aui border border-aui-border bg-aui-surface p-5 text-aui-sm leading-7">
        <div className="mb-5 text-aui-accent">
          <HumagenMark />
        </div>
        {(preview.blocks ?? preview.paragraphs.map((text) => ({ kind: "paragraph" as const, text }))).map((block, i) =>
          block.kind === "table" ? (
            <div key={i} className="aui-scroll mb-4 overflow-x-auto">
              <table className="w-full border-collapse text-left text-aui-xs">
                <tbody>{block.rows.map((row, r) => <tr key={r}>{row.map((cell, c) => r === 0
                  ? <th key={c} scope="col" className="border border-aui-border px-3 py-2 whitespace-pre-wrap break-words">{cell}</th>
                  : <td key={c} className="border border-aui-border px-3 py-2 whitespace-pre-wrap break-words">{cell}</td>)}</tr>)}</tbody>
              </table>
            </div>
          ) : i === 0 ? (
            <h3 key={i} className="mb-5 text-aui-lg font-semibold">{block.text}</h3>
          ) : (
            <p key={i} className="mb-3 whitespace-pre-wrap break-words">{block.text}</p>
          ),
        )}
        {preview.truncated && (
          <p className="text-aui-xs text-aui-muted">
            This preview is shortened. Download the original to read the whole document.
          </p>
        )}
      </article>
    );
  if (preview.kind === "table")
    return (
      <div className="min-w-0">
        <div className="aui-scroll max-h-[65dvh] overflow-auto rounded-aui border border-aui-border">
          <table className="w-full min-w-[28rem] border-collapse text-left text-aui-xs">
            <thead className="sticky top-0 bg-aui-surface">
              <tr>
                {preview.rows[0]?.map((cell, i) => (
                  <th
                    key={`${i}:${cell.slice(0, 20)}`}
                    className="border-aui-border border-b px-3 py-2 font-medium"
                  >
                    {cell}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {preview.rows.slice(1).map((row, i) => (
                <tr key={`${i}:${row.join().slice(0, 30)}`}>
                  {row.map((cell, j) => (
                    <td
                      key={`${j}:${cell.slice(0, 20)}`}
                      className="max-w-xs whitespace-pre-wrap break-words border-aui-border border-b px-3 py-2 align-top"
                    >
                      {cell}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
        {preview.truncated && (
          <p className="mt-2 text-aui-xs text-aui-muted">
            Showing up to 150 rows and 40 columns. Download the whole file for the rest.
          </p>
        )}
      </div>
    );
  if (preview.kind === "text")
    return preview.markdown ? (
      <WebSurface onAction={() => {}}>
        <Markdown text={preview.text} />
      </WebSurface>
    ) : (
      <pre className="whitespace-pre-wrap break-words rounded-aui border border-aui-border p-4 font-aui-mono text-aui-xs">
        {preview.text}
      </pre>
    );
  if (file.mime.startsWith("image/") && file.mime !== "image/svg+xml")
    return (
      <img
        src={contentUrl(file.id, true)}
        alt={file.description || file.title}
        className="max-h-[70dvh] max-w-full rounded-aui object-contain"
      />
    );
  if (file.mime === "application/pdf")
    return (
      <iframe
        title={file.title}
        src={contentUrl(file.id, true)}
        sandbox=""
        className="h-[65dvh] w-full rounded-aui border border-aui-border"
      />
    );
  return (
    <p className="text-aui-sm text-aui-muted">
      Download this file to open it in its native application.
    </p>
  );
}

function FileBody({
  file,
  slide,
  onSlide,
  onSlides,
}: {
  file: Stored;
  slide: number;
  onSlide: (slide: number) => void;
  onSlides: (count: number) => void;
}) {
  const [preview, setPreview] = useState<FilePreview>();
  const [problem, setProblem] = useState<string>();
  const [attempt,setAttempt]=useState(0);
  useEffect(() => {
    const abort = new AbortController();
    setProblem(undefined);setPreview(undefined);
    if (file.mime.startsWith("image/") || file.mime === "application/pdf") {
      setPreview({ kind: "none" });
      return () => abort.abort();
    }
    fetch(contentUrl(file.id), { signal: abort.signal, credentials: "same-origin" })
      .then(async (response) => {
        const { previewOf, readPreviewBytes } = await import("./artifactPreview.ts");
        return previewOf(await readPreviewBytes(response), file.title, file.mime);
      })
      .then((value) => {
        if (!abort.signal.aborted) {
          setPreview(value);
          onSlides(value.kind === "presentation" ? value.slides.length : 0);
        }
      })
      .catch((error) => {
        if (!abort.signal.aborted)
          setProblem(error instanceof Error ? error.message : words.artifactFailed);
      });
    return () => abort.abort();
  }, [file.id, file.mime, file.title, onSlides,attempt]);
  if (problem) return <div className="flex flex-col gap-3"><Problem message={problem}/><button type="button" className={`${button} self-start`} onClick={()=>setAttempt(value=>value+1)}>{defaultCopy.retry}</button></div>;
  if (!preview) return <SkeletonRows count={4} />;
  return <NativePreview file={file} preview={preview} slide={slide} onSlide={onSlide} />;
}

/** Saved artifacts in a conversation. A version is selected by its immutable artifact id. */
export function Deliverables({
  thread,
  view,
  onView,
  onClose,
  onRevise,
  promotion,
}: {
  thread: string;
  view: ConversationView;
  onView: (view: ConversationView) => void;
  onClose: () => void;
  onRevise?: (file: Stored, slide?: number) => void;
  promotion?: ArtifactPromotionAccess;
}) {
  const [items, setItems] = useState<Stored[]>();
  const [file, setFile] = useState<Stored>();
  const [versions, setVersions] = useState<Stored[]>();
  const [versionProblem, setVersionProblem] = useState<string>();
  const [problem, setProblem] = useState<string>();
  const [next, setNext] = useState<string | null>(null);
  const [moreBusy, setMoreBusy] = useState(false);
  const moreRequest = useRef<AbortController | null>(null);
  const selected = view.artifact;
  const tab = view.artifactTab ?? "preview";
  const setTab = (artifactTab: ArtifactTab) => onView({ ...view, artifactTab });
  const onSelect = (artifact?: string) => onView({ artifact });
  const [filter, setFilter] = useState("");
  const filtered = (items ?? []).filter(
    (item) =>
      !filter || item.kind === filter || (filter === "spreadsheet" && item.mime === "text/csv"),
  );
  const [slideCount, setSlideCount] = useState(0);
  const slide = Math.max(0, Math.min((view.slide ?? 1) - 1, Math.max(0, slideCount - 1)));
  const setSlide = (next: number) => onView({ ...view, slide: next + 1 });
  useEffect(() => {
    const abort = new AbortController();
    setItems(undefined);
    setNext(null);
    setMoreBusy(false);
    get<{ artifacts: Stored[]; next?: string | null }>(
      `/api/artifacts?${new URLSearchParams({ thread, limit: "100" })}`,
      abort.signal,
    )
      .then((response) => {
        if (!abort.signal.aborted) {
          setItems(response.artifacts ?? []);
          setNext(response.next ?? null);
        }
      })
      .catch(() => {
        if (!abort.signal.aborted) setProblem(words.artifactFailed);
      });
    return () => {
      abort.abort();
      moreRequest.current?.abort();
    };
  }, [thread]);
  useEffect(() => {
    const abort = new AbortController();
    setFile(undefined);
    setVersions(undefined);
    setVersionProblem(undefined);
    setProblem(undefined);
    setSlideCount(0);
    if (selected) {
      get<Stored>(at(selected), abort.signal)
        .then((value) => {
          if (abort.signal.aborted) return;
          if (value.thread !== thread) throw new Error(words.artifactFailed);
          setFile(value);
        })
        .catch(() => {
          if (!abort.signal.aborted) setProblem(words.artifactFailed);
        });
      get<{ versions: Stored[] }>(`${at(selected)}/versions`, abort.signal)
        .then((history) => {
          if (!abort.signal.aborted)
            setVersions(history.versions.filter((version) => version.thread === thread));
        })
        .catch(() => {
          if (!abort.signal.aborted) {
            setVersions([]);
            setVersionProblem(words.artifactFailed);
          }
        });
    }
    return () => abort.abort();
  }, [thread, selected]);
  const more = async () => {
    if (!next || moreBusy) return;
    const abort = new AbortController();
    moreRequest.current = abort;
    setMoreBusy(true);
    try {
      const page = await get<{ artifacts: Stored[]; next?: string | null }>(
        `/api/artifacts?${new URLSearchParams({ thread, limit: "100", before: next })}`,
        abort.signal,
      );
      if (!abort.signal.aborted) {
        setItems((old) => appendPage(old ?? [], page.artifacts ?? []));
        setNext(page.next ?? null);
      }
    } catch {
      if (!abort.signal.aborted) setProblem(words.artifactFailed);
    } finally {
      if (!abort.signal.aborted) setMoreBusy(false);
    }
  };
  return (
    <Inspector open wide={!!selected} title={file?.title || words.deliverables} onClose={onClose}>
      <Problem message={problem ?? null} />
      {selected ? (
        <div className="flex min-w-0 flex-col gap-4">
          <button
            type="button"
            className="min-h-6 self-start text-aui-xs text-aui-muted hover:text-aui-fg"
            onClick={() => onSelect()}
          >
            ‹ All Deliverables
          </button>
          {!file ? (
            !problem && <SkeletonRows count={4} />
          ) : (
            <>
              <div className="flex min-w-0 items-start gap-3">
                <ArtifactIcon kind={file.kind} phase="ready" size={24} />
                <div className="min-w-0">
                  <h3 className="break-words font-semibold text-aui-base">{file.title}</h3>
                  <p className="mt-1 flex flex-wrap items-center gap-2 text-aui-xs text-aui-muted">
                    <span>v{file.version}</span>
                    <AgentMark name={file.agent} size={12} />
                    <span>{file.agent}</span>
                    <time dateTime={file.created_at}>{ago(file.created_at)}</time>
                  </p>
                </div>
              </div>
              <p className="text-aui-xs text-aui-muted">{file.description || words.artifactHint}</p>
              <a
                href={contentUrl(file.id)}
                download={file.title}
                className={`${button} self-start`}
              >
                Download Original
              </a>
              <ExportPDF key={`pdf:${file.id}`} file={file} />
              <div className="flex gap-4 border-aui-border border-b" aria-label="Artifact views">
                {(["preview", "versions", "provenance"] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={tab === value}
                    onClick={() => setTab(value)}
                    className={`border-b-2 py-2 text-aui-xs ${tab === value ? "border-aui-fg text-aui-fg" : "border-transparent text-aui-muted"}`}
                  >
                    {value === "preview"
                      ? words.artifactPreview
                      : value === "versions"
                        ? words.artifactVersions
                        : words.artifactSources}
                  </button>
                ))}
              </div>
              {tab === "preview" ? (
                <FileBody
                  key={`preview:${file.id}`}
                  file={file}
                  slide={slide}
                  onSlide={setSlide}
                  onSlides={setSlideCount}
                />
              ) : tab === "versions" ? (
                <>
                  <Problem message={versionProblem ?? null} />
                  {versions === undefined && <SkeletonRows count={2} />}
                  <ul className="divide-aui-border divide-y">
                    {versions?.map((version) => (
                      <li key={version.id}>
                        <button
                          type="button"
                          className="flex min-h-8 w-full items-center justify-between gap-3 py-3 text-left text-aui-sm"
                          onClick={() => onSelect(version.id)}
                          aria-current={version.id === file.id ? "true" : undefined}
                        >
                          <span>Version {version.version}</span>
                          <span className="text-aui-xs text-aui-muted">
                            {version.id === file.id ? "Viewing" : ago(version.created_at)}
                          </span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </>
              ) : (
                <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-3 break-words text-aui-xs">
                  <dt>Agent</dt>
                  <dd>{file.agent}</dd>
                  <dt>Run</dt>
                  <dd>{file.run}</dd>
                  <dt>Conversation</dt>
                  <dd>{file.thread}</dd>
                  <dt>Created</dt>
                  <dd>{new Date(file.created_at).toLocaleString()}</dd>
                  <dt>SHA-256</dt>
                  <dd className="break-all font-aui-mono">{file.sha256}</dd>
                </dl>
              )}
              {promotion && <ArtifactPromotion key={`promote-${file.id}`} file={file} access={promotion} />}
              {onRevise && (
                <div className="flex flex-wrap items-center justify-between gap-3 border-aui-border border-t pt-4">
                  <p className="min-w-0 flex-1 text-aui-2xs text-aui-muted">
                    Feedback stays attached to this version
                    {slideCount > 0 ? ` and slide ${slide + 1}` : ""}.
                  </p>
                  <button
                    type="button"
                    className={button}
                    onClick={() => onRevise(file, slideCount > 0 ? slide + 1 : undefined)}
                  >
                    {words.artifactFeedback}
                  </button>
                </div>
              )}
            </>
          )}
        </div>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap gap-2" aria-label="Deliverable types">
            {(
              [
                ["", "All"],
                ["document", "Documents"],
                ["presentation", "Presentations"],
                ["spreadsheet", "Tables"],
              ] as const
            ).map(([value, label]) => (
              <button
                key={value}
                type="button"
                className={button}
                aria-pressed={filter === value}
                onClick={() => setFilter(value)}
              >
                {label}
              </button>
            ))}
          </div>
          {!items ? (
            !problem && <SkeletonRows count={3} />
          ) : !items.length ? (
            <p className="text-aui-sm text-aui-muted">{words.artifactEmpty}</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {filtered.map((item) => (
                <li key={item.id}>
                  <button
                    type="button"
                    className="flex w-full min-w-0 items-center gap-3 rounded-aui border border-aui-border p-3 text-left hover:bg-aui-hover"
                    onClick={() => onSelect(item.id)}
                  >
                    <ArtifactIcon kind={item.kind} phase="ready" size={22} />
                    <span className="min-w-0 flex-1">
                      <strong className="block truncate text-aui-sm font-medium">
                        {item.title}
                      </strong>
                      <span className="mt-1 block text-aui-2xs text-aui-muted">
                        v{item.version} {item.agent}
                      </span>
                    </span>
                    <span aria-hidden>›</span>
                  </button>
                </li>
              ))}
            </ul>
          )}
          {items && items.length > 0 && filtered?.length === 0 && (
            <p className="text-aui-sm text-aui-muted">{words.artifactNoMatch}</p>
          )}
          {next && (
            <button
              type="button"
              className={`${button} self-center`}
              disabled={moreBusy}
              onClick={() => void more()}
            >
              {moreBusy ? "Loading" : words.more}
            </button>
          )}
        </div>
      )}
    </Inspector>
  );
}
