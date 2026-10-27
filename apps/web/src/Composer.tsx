import { defaultCopy } from "@humagen/components";
import { ArrowUpIcon, FileIcon, XIcon } from "@humagen/render-web";
import { type QueueControls, QueueStrip } from "@humagen/web";
import { type KeyboardEvent, type ReactNode, useEffect, useLayoutEffect, useRef, useState } from "react";
import { insertTag, matching, sigilOf, type TagOption, type Typing, tagAt } from "./tags.ts";
import { AgentMark, Presence } from "./ui.tsx";

const MAX_HEIGHT = 200;
const MAX_FILES = 5;
const MAX_FILE_BYTES = 200 * 1024;

// Files are read in the browser and sent as part of the message, so only text
// can be attached. These are the extensions treated as text when the browser
// does not say.
const TEXT_EXT = /\.(txt|md|markdown|csv|tsv|json|ya?ml|toml|xml|html?|css|log|sql|sh|py|go|rs|ts|tsx|js|jsx|java|rb|c|h|cpp)$/i;

export interface Attached {
  id: string;
  name: string;
  text: string;
}

/** Read picked files as text to send with a message, and say why any were left out. */
export async function readAttached(picked: FileList | File[], already: Attached[]): Promise<{ files: Attached[]; skipped: string[] }> {
  const skipped: string[] = [];
  const next = [...already];
  for (const file of Array.from(picked)) {
    const textual = file.type.startsWith("text/") || /json|xml|yaml|javascript/.test(file.type) || TEXT_EXT.test(file.name);
    if (next.length >= MAX_FILES) skipped.push(`${file.name}: at most ${MAX_FILES} files`);
    else if (!textual) skipped.push(`${file.name}: only text files can be attached`);
    else if (file.size > MAX_FILE_BYTES) skipped.push(`${file.name}: larger than ${MAX_FILE_BYTES / 1024} KB`);
    else {
      const text = await file.text();
      // A NUL byte means the file is not text, whatever its name says.
      if (text.includes("\u0000")) skipped.push(`${file.name}: only text files can be attached`);
      else next.push({ id: `${file.name}:${file.size}:${file.lastModified}`, name: file.name, text });
    }
  }
  return { files: next, skipped };
}

/** A message with its attached files after it. */
export function withAttached(text: string, files: Attached[]): string {
  return [text.trim(), ...files.map((f) => `Attached file: ${f.name}\n\`\`\`\n${f.text}\n\`\`\``)].filter(Boolean).join("\n\n");
}

// The browser's speech recognizer, where it exists. It has no standard type yet.
interface Recognizer {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  onresult: ((e: { resultIndex: number; results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }> }) => void) | null;
  onend: (() => void) | null;
  onerror: (() => void) | null;
  start: () => void;
  stop: () => void;
}
function recognizer(): (new () => Recognizer) | undefined {
  const w = window as unknown as { SpeechRecognition?: new () => Recognizer; webkitSpeechRecognition?: new () => Recognizer };
  return w.SpeechRecognition ?? w.webkitSpeechRecognition;
}

function Glyph({ d, size = 18 }: { d: string; size?: number }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0"
    >
      <path d={d} />
    </svg>
  );
}
const PLUS = "M5 12h14M12 5v14";
const MIC = "M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3zM19 10v2a7 7 0 0 1-14 0v-2M12 19v3";

// The send and speak buttons share one size and one place, so nothing moves
// when one takes over from the other.
const action =
  "inline-flex size-9 shrink-0 items-center justify-center rounded-full transition-opacity focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-aui-accent";

const tool =
  "inline-flex size-9 items-center justify-center rounded-full text-aui-muted transition-colors hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-40";

export type ComposerSeed = { text: string; mode?: "prepend" };

/**
 * The message box. Type, attach text files, or dictate. Enter sends and
 * Shift+Enter adds a line. While a reply is in flight, sending queues the
 * message behind it.
 */
export function Composer({
  placeholder,
  queue,
  autoFocus,
  onDraft,
  leading,
  trailing,
  seed,
  tags,
  plus,
}: {
  /** Text to start from, such as a question picked from a list. Each new object replaces what is typed. */
  seed?: ComposerSeed;
  placeholder: string;
  queue: QueueControls;
  autoFocus?: boolean;
  /** Called as the text changes, for anything that previews it. */
  onDraft?: (text: string) => void;
  /** Controls beside the plus, such as where the conversation is filed. */
  leading?: ReactNode;
  /** Controls before the send or speak button. */
  trailing?: ReactNode;
  /**
   * Completion: what a message here can tag with `@` and `$`, and a call
   * each time the tag being typed changes, for options looked up as it is
   * typed.
   */
  tags?: { label: string; options: TagOption[]; onQuery?: (typing: Typing) => void };
  /**
   * The + menu, in place of + attaching files at once: what it shows, given
   * a way to close it and to attach files, and the chips of what it
   * selected, shown as text controls below the composer. Ctrl+/ (Cmd+/) opens it.
   */
  plus?: { menu: (close: () => void, attach: () => void, leave: () => void) => ReactNode; chips?: ReactNode };
}) {
  const [value, setValue] = useState("");
  const [files, setFiles] = useState<Attached[]>([]);
  const [notice, setNotice] = useState<string | null>(null);
  const [listening, setListening] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const speech = useRef<Recognizer | null>(null);
  const form = useRef<HTMLFormElement>(null);
  const [menu, setMenu] = useState(false);
  const [menuEpoch, setMenuEpoch] = useState(0);
  const showMenu = () => {
    if (!menu) setMenuEpoch((epoch) => epoch + 1);
    setMenu(true);
  };
  const closeMenu = () => {
    setMenu(false);
    input.current?.focus();
  };
  // A press outside the menu closes it, as Escape does.
  useEffect(() => {
    if (!menu) return;
    const away = (e: MouseEvent) => {
      const panel = form.current?.querySelector('[data-plus-panel]');
      if (panel && !panel.contains(e.target as Node) && !(e.target as HTMLElement).closest?.("[data-plus-button]")) setMenu(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [menu]);
  const canDictate = recognizer() !== undefined;

  const [typing, setTyping] = useState<Typing | null>(null);
  const [active, setActive] = useState(0);
  const shown = tags && typing ? matching(tags.options, typing.query, 8, typing.sigil) : [];
  // The highlighted option, kept inside the list as it shrinks under the caret.
  const current = Math.max(0, Math.min(active, shown.length - 1));
  // The list opens toward the side of the bar with more room, no taller than that room.
  const open = shown.length > 0;
  const [listAt, setListAt] = useState<{ up: boolean; max: number }>({ up: true, max: 256 });
  useLayoutEffect(() => {
    if (!open) return;
    const r = form.current?.getBoundingClientRect();
    if (!r) return;
    const above = r.top - 8;
    const below = window.innerHeight - r.bottom - 8;
    const up = above >= Math.min(256, below);
    setListAt({ up, max: Math.max(120, Math.min(256, up ? above : below)) });
  }, [open]);

  // What is being tagged at the caret, read after every change and every
  // move of the caret.
  const follow = (text: string, caret?: number) => {
    if (!tags) return;
    const el = input.current;
    const at = tagAt(text, caret ?? el?.selectionStart ?? text.length);
    setTyping(at);
    setActive(0);
    if (at && (at.query !== typing?.query || at.sigil !== typing?.sigil)) tags.onQuery?.(at);
  };

  const pickTag = (option: TagOption) => {
    if (!typing) return;
    const next = insertTag(value, typing, option);
    change(next.text);
    // A mention kind is completed further: `@sme:` opens its items.
    if (option.kind === "kind") follow(next.text, next.caret);
    else setTyping(null);
    requestAnimationFrame(() => {
      const el = input.current;
      el?.focus();
      el?.setSelectionRange(next.caret, next.caret);
    });
  };

  const change = (next: string) => {
    setValue(next);
    onDraft?.(next);
  };

  // A picked starter fills the box and puts the cursor at its end, ready to send or edit.
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per seed
  useEffect(() => {
    if (!seed) return;
    const next = seed.mode === "prepend" ? seed.text + value : seed.text;
    change(next);
    const el = input.current;
    if (el) {
      el.focus();
      el.setSelectionRange(next.length, next.length);
    }
  }, [seed]);

  // Grow with the text up to a cap, then scroll.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-measure whenever the text changes
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
    el.style.overflowY = el.scrollHeight > MAX_HEIGHT ? "auto" : "hidden";
  }, [value]);

  useEffect(() => {
    if (autoFocus) input.current?.focus();
  }, [autoFocus]);

  useEffect(() => () => speech.current?.stop(), []);

  const ready = value.trim().length > 0 || files.length > 0;

  const send = (now: boolean) => {
    if (!ready) return;
    const text = withAttached(value, files);
    if (now) queue.sendNow(text);
    else queue.send(text);
    change("");
    setFiles([]);
    setNotice(null);
    input.current?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (plus && e.key === "/" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      setTyping(null);
      showMenu();
      return;
    }
    // While the typeahead is open, the keys move through it and pick from it.
    if (shown.length > 0 && !e.nativeEvent.isComposing) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        setActive((i) => (i + (e.key === "ArrowDown" ? 1 : shown.length - 1)) % shown.length);
        return;
      }
      // Enter with a modifier sends or adds a line, as it does with the list closed.
      if ((e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) || e.key === "Tab") {
        e.preventDefault();
        const option = shown[current];
        if (option) pickTag(option);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setTyping(null);
        return;
      }
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send(e.metaKey || e.ctrlKey);
    }
  };

  const attach = async (picked: FileList | null) => {
    if (!picked) return;
    const { files: next, skipped } = await readAttached(picked, files);
    setFiles(next);
    setNotice(skipped.length > 0 ? skipped.join(". ") : null);
  };

  const dictate = () => {
    if (listening) return speech.current?.stop();
    const Speech = recognizer();
    if (!Speech) return;
    const r = new Speech();
    r.continuous = true;
    r.interimResults = false;
    r.lang = navigator.language;
    r.onresult = (e) => {
      let heard = "";
      for (let i = e.resultIndex; i < e.results.length; i++) {
        const result = e.results[i];
        if (result?.isFinal) heard += result[0]?.transcript ?? "";
      }
      if (heard) {
        const el = input.current;
        const base = el?.value ?? "";
        change(base + (base && !base.endsWith(" ") ? " " : "") + heard.trim());
      }
    };
    r.onend = () => setListening(false);
    r.onerror = () => {
      setListening(false);
      setNotice("Dictation stopped. Check that the microphone is allowed for this page.");
    };
    speech.current = r;
    setListening(true);
    r.start();
  };

  return (
    <div className="flex min-w-0 flex-col gap-1.5">
    <form
      ref={form}
      aria-label="Message"
      onSubmit={(e) => {
        e.preventDefault();
        send(false);
      }}
      className="relative flex flex-col gap-1 rounded-[1.25rem] border border-aui-border bg-aui-surface p-2 transition-colors focus-within:border-aui-muted"
    >
      {tags && shown.length > 0 && (
        // Still: a list waiting on a person does not animate.
        <ul
          id="composer-tags"
          data-layer="completion"
          role="listbox"
          aria-label={tags.label}
          style={{ maxHeight: listAt.max }}
          className={`absolute right-2 left-2 z-20 overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1 shadow-lg ${listAt.up ? "bottom-full mb-1" : "top-full mt-1"}`}
        >
          {shown.map((o, i) => (
            <li
              key={`${o.kind}:${o.token}`}
              id={`composer-tag-${i}`}
              role="option"
              aria-selected={i === current}
              onMouseDown={(e) => {
                // Keep the text box's focus and caret while picking.
                e.preventDefault();
                pickTag(o);
              }}
              className="flex cursor-pointer items-center gap-2.5 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg aria-selected:bg-aui-hover"
            >
              {o.kind === "agent" ? (
                <AgentMark name={o.token} size={16} />
              ) : (
                <span aria-hidden className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-aui-hover text-[0.6rem] font-medium">
                  {o.kind === "person" ? (o.hint ?? o.label).trim().charAt(0).toUpperCase() : o.kind === "skill" ? "$" : "@"}
                </span>
              )}
              <span className="shrink-0 font-medium">
                {sigilOf(o)}
                {o.token}
              </span>
              {o.hint && <span className="min-w-0 truncate text-aui-xs text-aui-muted">{o.hint}</span>}
            </li>
          ))}
        </ul>
      )}
      {plus && <Presence when={menu} ms={130}>{(leaving) => (
        <div key={menuEpoch} data-plus-panel inert={leaving} aria-hidden={leaving || undefined} data-leaving={leaving} className="humagen-composer-menu contents">
          {plus.menu(
            closeMenu,
            () => {
              setMenu(false);
              picker.current?.click();
            },
            () => setMenu(false),
          )}
        </div>
      )}</Presence>}
      <QueueStrip queue={queue} />
      {files.length > 0 && (
        <ul className="flex flex-wrap gap-1.5 px-2 pt-1" aria-label="Attached files">
          {files.map((f) => (
            <li
              key={f.id}
              className="inline-flex min-h-8 pointer-coarse:min-h-11 max-w-60 items-center gap-1.5 rounded-full border border-aui-border pr-1 pl-2.5 text-aui-xs text-aui-fg"
            >
              <FileIcon size={12} className="text-aui-muted" />
              <span className="truncate">{f.name}</span>
              <button
                type="button"
                aria-label={`Remove ${f.name}`}
                onClick={() => setFiles(files.filter((x) => x.id !== f.id))}
                className="inline-flex size-8 pointer-coarse:size-11 shrink-0 items-center justify-center rounded-full text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent"
              >
                <XIcon size={12} />
              </button>
            </li>
          ))}
        </ul>
      )}
      <label className="sr-only" htmlFor="composer-input">
        Message
      </label>
      <textarea
        id="composer-input"
        ref={input}
        rows={1}
        value={value}
        placeholder={placeholder}
        onChange={(e) => {
          change(e.target.value);
          follow(e.target.value);
        }}
        onKeyDown={onKeyDown}
        onKeyUp={(e) => {
          if (e.key === "ArrowLeft" || e.key === "ArrowRight" || e.key === "Home" || e.key === "End") follow(value);
        }}
        onClick={() => follow(value)}
        onBlur={() => setTyping(null)}
        role={tags ? "combobox" : undefined}
        aria-expanded={tags ? shown.length > 0 : undefined}
        aria-controls={tags && shown.length > 0 ? "composer-tags" : undefined}
        aria-autocomplete={tags ? "list" : undefined}
        aria-activedescendant={tags && shown.length > 0 ? `composer-tag-${current}` : undefined}
        className="max-h-[200px] min-h-10 w-full resize-none bg-transparent px-3 pt-2 text-aui-base text-aui-fg outline-none placeholder:text-aui-muted"
      />
      {notice && (
        <p role="status" className="px-3 text-aui-xs text-aui-muted">
          {notice}
        </p>
      )}
      <div className="flex items-center gap-1">
        <input
          ref={picker}
          type="file"
          multiple
          className="sr-only"
          tabIndex={-1}
          aria-hidden
          onChange={(e) => {
            void attach(e.target.files);
            e.target.value = "";
          }}
        />
        {plus ? (
          <button
            type="button"
            data-plus-button
            className={`${tool} self-start`}
            aria-label={defaultCopy.chat.plus}
            aria-haspopup="dialog"
            aria-expanded={menu}
            title={`${defaultCopy.chat.plus} (${defaultCopy.chat.plusShortcut(/Mac|iPhone|iPad/.test(navigator.platform))})`}
            onClick={() => (menu ? closeMenu() : showMenu())}
          >
            <Glyph d={PLUS} />
          </button>
        ) : (
          <button type="button" className={tool} aria-label="Attach files" title="Attach text files" onClick={() => picker.current?.click()}>
            <Glyph d={PLUS} />
          </button>
        )}
        {leading}
        <div className="min-w-0 flex-1" />
        {trailing}
        {/*
          One button in one place. With nothing to send it listens, and once
          there is something to send it sends. While it is listening it stays
          the stop button, even as words arrive, so dictation can always be
          ended from where it was started.
        */}
        {listening || (!ready && canDictate) ? (
          <button
            type="button"
            className={`${action} ${listening ? "bg-aui-accent text-aui-accent-fg" : "bg-aui-hover text-aui-fg"}`}
            aria-label={listening ? "Stop dictating" : "Dictate"}
            aria-pressed={listening}
            title={listening ? "Stop dictating" : "Dictate"}
            onClick={dictate}
          >
            {listening ? <span aria-hidden className="size-2.5 rounded-[2px] bg-current" /> : <Glyph d={MIC} />}
          </button>
        ) : (
          <button
            type="submit"
            disabled={!ready}
            aria-label={queue.running ? "Queue message" : "Send message"}
            title={queue.running ? "Queue behind the reply in flight" : "Send"}
            className={`${action} bg-aui-primary text-aui-primary-fg hover:opacity-90 disabled:opacity-40`}
          >
            <ArrowUpIcon size={16} strokeWidth={2.5} />
          </button>
        )}
      </div>
    </form>
    {plus?.chips && <div className="min-w-0 px-1">{plus.chips}</div>}
    </div>
  );
}
