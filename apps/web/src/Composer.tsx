import { defaultCopy } from "@humagen/components";
import { FileIcon, XIcon } from "@humagen/render-web";
import { Modal, type QueueControls, QueueStrip } from "@humagen/web";
import {
  type KeyboardEvent,
  type ReactNode,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { termsUrl } from "./branding.ts";
import {
  type Command,
  commandsFor,
  commandTokens,
  type Hit,
  nextEnabled,
  recentCommands,
  rememberCommands,
  segments,
  skillsIn,
} from "./commands.ts";
import { LazyFrame, sparing, whenIdle } from "./lazy.tsx";
import { VoiceLevel } from "./pages.ts";
import {
  insertTag,
  matching,
  mentionsIn,
  mentionTokens,
  sigilOf,
  type TagOption,
  type Typing,
  tagAt,
  withMention,
  withoutMention,
} from "./tags.ts";
import { AgentMark, button, composerIconControl, Presence, primary, textLink } from "./ui.tsx";
import {
  canRecord,
  MAX_RECORDING_MS,
  recordingType,
  type VoiceNotes,
  withVoiceNote,
} from "./voice.ts";

const MAX_HEIGHT = 200;

export { readAttached, withAttached } from "./attachments.ts";

import { type Attached, readAttached, uploadsOf, withAttached } from "./attachments.ts";

// The browser's speech recognizer, where it exists. It has no standard type yet.
interface Recognizer {
  continuous: boolean;
  interimResults: boolean;
  lang: string;
  onresult:
    | ((e: {
        resultIndex: number;
        results: ArrayLike<ArrayLike<{ transcript: string }> & { isFinal: boolean }>;
      }) => void)
    | null;
  onend: (() => void) | null;
  onerror: ((event: { error?: string }) => void) | null;
  start: () => void;
  stop: () => void;
}
function recognizer(): (new () => Recognizer) | undefined {
  const w = window as unknown as {
    SpeechRecognition?: new () => Recognizer;
    webkitSpeechRecognition?: new () => Recognizer;
  };
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
const PLUS = "M3 12h18M12 3v18";
const RETURN = "M9 10l-5 5 5 5M20 4v7a4 4 0 0 1-4 4H4";
const MIC =
  "M12 2a3 3 0 0 0-3 3v7a3 3 0 0 0 6 0V5a3 3 0 0 0-3-3zM19 10v2a7 7 0 0 1-14 0v-2M12 19v3";

const tool = `${composerIconControl} mb-0.5 pointer-coarse:mb-0 text-aui-muted hover:text-aui-fg disabled:opacity-40`;

/**
 * What to put in the box: text that replaces the draft or goes ahead of it,
 * or an agent's mention, written at the caret (the start of an empty draft)
 * as the `@` completion writes it, or taken out.
 */
export type ComposerSeed =
  | { text: string; mode?: "prepend" | "append"; files?: Attached[] }
  | { quote: { label: string; url: string; text?: string; agent?: string } }
  | { mention: string; remove?: boolean };

/**
 * The message box. Type, attach files, record a voice note (where the
 * deployment transcribes them) or dictate. Enter sends and
 * Shift+Enter adds a line. While anything in the session runs, the box says
 * so: Enter sends now (the work in flight stops for it), Cmd+Enter
 * (Ctrl+Enter) queues the message behind it, and Stop, or Esc with nothing
 * typed, stops everything running in the session.
 */
/**
 * A line the bar says while a person types (no match, no skills yet, a refusal).
 * It floats where the completion list would, so the bar keeps its size and
 * nothing around it moves.
 */
const floatNote =
  // Over the bar where it sits at the foot of a page; under it on Home's wide layout, where the greeting is above.
  "absolute right-2 bottom-full left-2 z-(--aui-z-popover) mb-1 rounded-aui border border-aui-border bg-aui-surface px-3 py-2 text-aui-xs text-aui-muted shadow-lg md:[.humagen-home_&]:top-full md:[.humagen-home_&]:bottom-auto md:[.humagen-home_&]:mt-1 md:[.humagen-home_&]:mb-0";

export function Composer({
  queue,
  placeholder,
  emptyText,
  autoFocus,
  onDraft,
  leading,
  trailing,
  seed,
  tags,
  sessionAgents,
  plus,
  voice,
  held = false,
  onCancelHeld,
  stopAll,
  footer,
}: {
  /**
   * Everything running in the session, whoever started it. While anything
   * runs the bar shows Stop, which (as Esc in an empty box does) stops all of
   * it; without this, only the reply to this person's last message counts.
   */
  stopAll?: { busy: boolean; pending: boolean; onStop: () => void };
  /** A quiet row under the box: who answers, the approval mode, usage. */
  footer?: ReactNode;
  /**
   * The message in the box is sent and waits to go: it stays on screen,
   * read-only and dimmed, until it goes. Escape gives it back to edit.
   */
  held?: boolean;
  onCancelHeld?: () => void;
  /**
   * Voice notes, when the deployment transcribes them: the microphone
   * records, and the words come back into the draft, marked, to read and
   * send. Without it the microphone dictates with the browser's recognizer.
   */
  voice?: VoiceNotes;
  /** Text to start from, such as a question picked from a list. Each new object replaces what is typed. */
  seed?: ComposerSeed;
  placeholder: string;
  /** What the empty box says when nobody is mentioned. Left out, Home's words. */
  emptyText?: string;
  queue: QueueControls;
  autoFocus?: boolean;
  /** Called as the text changes, for anything that previews it. */
  onDraft?: (text: string) => void;
  /** Controls beside the plus, such as where the conversation is filed. */
  leading?: ReactNode;
  /** Controls before the send or speak button. */
  trailing?: ReactNode;
  /**
   * Completion: what a message here can tag with `@`, the skills `/` offers as commands, and a call
   * each time the tag being typed changes, for options looked up as it is
   * typed.
   */
  tags?: {
    label: string;
    defaultAgent?: string;
    options: TagOption[];
    onQuery?: (typing: Typing) => void;
    /** The agents are still being read: the @ menu says so in a row of its own. */
    agentsLoading?: boolean;
  };
  /** Agents with an existing session that can be continued or started afresh. */
  sessionAgents?: string[];
  /**
   * The + menu, in place of + attaching files at once: what it shows, given
   * a way to close it and to attach files, and where the conversation is
   * filed, shown as a text control in the bar. Ctrl+/ (Cmd+/) opens it. The
   * menu is given the draft as it stands, so a host that marks the draft's
   * mentions need not keep the draft itself and render again on every key.
   */
  plus?: {
    menu: (close: () => void, attach: () => void, leave: () => void, draft: string) => ReactNode;
    chips?: ReactNode;
  };
}) {
  const [value, setValue] = useState("");
  const [files, setFiles] = useState<Attached[]>([]);
  const [quote, setQuote] = useState<Extract<ComposerSeed, { quote: unknown }>["quote"]>();
  const [chooseSession, setChooseSession] = useState<string>();
  const [sessionPick, setSessionPick] = useState<{ agent: string; fresh: boolean }>();
  const [notice, setNotice] = useState<string | null>(null);
  const [listening, setListening] = useState(false);
  const input = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
  const speech = useRef<Recognizer | null>(null);
  const [recording, setRecording] = useState(false);
  // The microphone while a voice note records, for the level along the bar.
  const [mic, setMic] = useState<MediaStream | null>(null);
  const [transcribing, setTranscribing] = useState(false);
  const recorder = useRef<MediaRecorder | null>(null);
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
  // Anything at work in the session: the bar shows Stop, and Enter sends now.
  const running = queue.running || Boolean(stopAll?.busy);
  const stopping = Boolean(stopAll?.pending) || queue.state.status === "stopping";
  const stopEverything = () => {
    if (queue.state.status === "running") queue.stop();
    else stopAll?.onStop();
  };
  const mac = typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform);
  // A press outside the menu closes it, as Escape does.
  useEffect(() => {
    if (!menu) return;
    const away = (e: MouseEvent) => {
      const panel = form.current?.querySelector("[data-plus-panel]");
      if (
        panel &&
        !panel.contains(e.target as Node) &&
        !(e.target as HTMLElement).closest?.("[data-plus-button]")
      )
        setMenu(false);
    };
    document.addEventListener("mousedown", away);
    return () => document.removeEventListener("mousedown", away);
  }, [menu]);
  const records = voice !== undefined && canRecord();
  const canDictate = records || recognizer() !== undefined;

  const [typing, setTyping] = useState<Typing | null>(null);
  const [active, setActive] = useState(0);
  // `/` opens the command menu (commands.ts); `@` the tag list.
  const commands: Command[] =
    tags && typing?.sigil === "/"
      ? commandsFor(
          tags.options,
          value,
          typing,
          {
            named: defaultCopy.threads.commandNamed,
            limit: defaultCopy.threads.commandLimit,
          },
          undefined,
          recentCommands(),
        )
      : [];
  const shown =
    tags && typing && typing.sigil !== "/"
      ? matching(tags.options, typing.query, 8, typing.sigil)
      : [];
  const count = commands.length || shown.length;
  // The highlighted option, kept inside the list as it shrinks under the
  // caret, and never on a command that cannot be picked.
  const clamped = Math.max(0, Math.min(active, count - 1));
  const current = commands[clamped]?.disabled ? nextEnabled(commands, clamped, 1) : clamped;
  // The list opens toward the side of the bar with more room, no taller than that room.
  const open = count > 0;
  // An @ typed before the agents are read opens the list with a row saying they are on their way.
  const agentsPending = !!tags?.agentsLoading && typing?.sigil === "@";
  // The skills a message here can name, drawn as tokens where it names them.
  const skillNames = tags?.options.filter((o) => o.kind === "skill").map((o) => o.token) ?? [];
  const tokens = [
    ...commandTokens(value, skillNames),
    ...mentionTokens(value, tags?.options ?? []),
  ].sort((a, b) => a.start - b.start);
  const mentioned = mentionsIn(
    value,
    (tags?.options ?? []).filter((option) => option.kind === "agent").map((option) => option.token),
  );
  const backdrop = useRef<HTMLDivElement>(null);
  const [listAt, setListAt] = useState<{ up: boolean; max: number }>({ up: true, max: 256 });
  const listed = open || agentsPending;
  useLayoutEffect(() => {
    if (!listed) return;
    const r = form.current?.getBoundingClientRect();
    if (!r) return;
    const above = r.top - 8;
    const below = window.innerHeight - r.bottom - 8;
    const up = above >= Math.min(256, below);
    setListAt({ up, max: Math.max(120, Math.min(256, up ? above : below)) });
  }, [listed]);

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
    // The caret is placed as the box shows the new text, before another key
    // can land where it was.
    input.current?.focus();
    caretAfter.current = next.caret;
    if (option.kind === "agent" && sessionAgents?.includes(option.token))
      setChooseSession(option.token);
  };

  const change = (next: string) => {
    setValue(next);
    onDraft?.(next);
  };

  const caretAfter = useRef<number | null>(null);
  // A picked starter fills the box and puts the cursor at its end, ready to
  // send or edit. A picked agent is mentioned where the cursor was.
  // biome-ignore lint/correctness/useExhaustiveDependencies: once per seed
  useEffect(() => {
    if (!seed) return;
    if ("files" in seed && seed.files) setFiles(seed.files);
    const el = input.current;
    let next: string;
    let caret: number;
    if ("quote" in seed) {
      setQuote(seed.quote);
      el?.focus();
      return;
    }
    if ("mention" in seed) {
      if (seed.remove) {
        next = withoutMention(value, seed.mention);
        caret = next.length;
        if (sessionPick?.agent === seed.mention) setSessionPick(undefined);
      } else {
        const at = value.trim() ? (el?.selectionStart ?? value.length) : 0;
        ({ text: next, caret } = withMention(value.trim() ? value : "", at, seed.mention));
        if (sessionAgents?.includes(seed.mention)) {
          setMenu(false);
          el?.focus();
          setChooseSession(seed.mention);
        }
      }
    } else {
      next =
        seed.mode === "prepend"
          ? seed.text + value
          : seed.mode === "append" && value.trim()
            ? `${value}\n\n${seed.text}`
            : seed.text;
      caret = next.length;
    }
    // A mention picked in the + menu leaves focus there, for the next pick;
    // the box takes it back as the menu closes, with the caret after the
    // mention.
    if (!("mention" in seed)) el?.focus();
    // The caret is placed once the box shows the new text.
    if (next === value) el?.setSelectionRange(caret, caret);
    else caretAfter.current = caret;
    change(next);
  }, [seed]);
  // biome-ignore lint/correctness/useExhaustiveDependencies: placed after each change of the text
  useLayoutEffect(() => {
    if (caretAfter.current === null) return;
    input.current?.setSelectionRange(caretAfter.current, caretAfter.current);
    caretAfter.current = null;
  }, [value]);

  // Grow with the text up to a cap, then scroll.
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-measure whenever the text changes
  useLayoutEffect(() => {
    const el = input.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, MAX_HEIGHT)}px`;
    el.style.overflowY = el.scrollHeight > MAX_HEIGHT ? "auto" : "hidden";
  }, [value]);

  // The tokens' backdrop sits exactly under the text box, with its box and
  // its type, so each token lies under its words.
  // biome-ignore lint/correctness/useExhaustiveDependencies: placed after each change of the text
  useLayoutEffect(() => {
    const el = input.current;
    const under = backdrop.current;
    if (!el || !under) return;
    const place = () => {
      const css = getComputedStyle(el);
      Object.assign(under.style, {
        left: `${el.offsetLeft}px`,
        top: `${el.offsetTop}px`,
        width: `${el.clientWidth}px`,
        height: `${el.clientHeight}px`,
        padding: css.padding,
        font: css.font,
        letterSpacing: css.letterSpacing,
        lineHeight: css.lineHeight,
      });
      under.scrollTop = el.scrollTop;
    };
    place();
    if (typeof ResizeObserver === "undefined") return;
    const watch = new ResizeObserver(place);
    watch.observe(el);
    return () => watch.disconnect();
  }, [value, tokens.length > 0]);

  useEffect(() => {
    if (autoFocus) input.current?.focus();
  }, [autoFocus]);

  useEffect(
    () => () => {
      speech.current?.stop();
      // Leaving the page drops a recording in progress, and frees the microphone.
      const r = recorder.current;
      if (r) {
        r.ondataavailable = null;
        r.onstop = null;
        if (r.state !== "inactive") r.stop();
        for (const track of r.stream.getTracks()) track.stop();
      }
    },
    [],
  );

  const ready = value.trim().length > 0 || files.length > 0;
  // The bar's light (ComposerLight.tsx) shows only on a bar in use, so it is
  // read once the bar sits idle on screen, or the moment it is due.
  const [light, setLight] = useState(false);
  const due = ready || !!mic;
  // Each time the light comes due it is asked for afresh: a read that failed
  // is not held against the bar, which works as plainly without it.
  const [asked, setAsked] = useState(0);
  useEffect(() => {
    if (due) {
      setLight(true);
      setAsked((n) => n + 1);
      return;
    }
    // Nothing is read ahead for a person saving data: the light waits until it is due.
    if (light || sparing()) return;
    return whenIdle(() => setLight(true));
  }, [light, due]);
  const lit = String(asked);

  const send = (now: boolean) => {
    if (!ready || held) return;
    const text =
      withAttached(
        quote
          ? `${quote.agent ? `@${quote.agent} ` : ""}Replying to [${quote.label}](${quote.url}):\n\n${value}`
          : value,
        files,
      ) || "Please inspect the attached files.";
    const pick =
      sessionPick && mentionsIn(text, [sessionPick.agent]).length ? sessionPick : undefined;
    // The commands this message names lead the / menu next time.
    if (tags)
      rememberCommands(
        skillsIn(
          value,
          tags.options.filter((o) => o.kind === "skill").map((o) => o.token),
        ),
      );
    if (now) queue.sendNow(text, pick, uploadsOf(files));
    else queue.send(text, pick, uploadsOf(files));
    change("");
    setFiles([]);
    setQuote(undefined);
    setSessionPick(undefined);
    setNotice(null);
    input.current?.focus();
  };

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (held) {
      if (e.key === "Escape") {
        e.preventDefault();
        onCancelHeld?.();
      }
      return;
    }
    if (plus && e.key === "/" && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      setTyping(null);
      showMenu();
      return;
    }
    // While the typeahead is open, the keys move through it and pick from it.
    if (open && !e.nativeEvent.isComposing) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp") {
        e.preventDefault();
        if (commands.length > 0) {
          const next = nextEnabled(commands, current, e.key === "ArrowDown" ? 1 : -1);
          if (next >= 0) setActive(next);
        } else setActive((i) => (i + (e.key === "ArrowDown" ? 1 : count - 1)) % count);
        return;
      }
      // Enter with a modifier sends or adds a line, as it does with the list
      // closed. With every command greyed, Enter sends the text as it is.
      const option = commands.length > 0 ? commands[current]?.option : shown[current];
      if (
        option &&
        ((e.key === "Enter" && !e.shiftKey && !e.metaKey && !e.ctrlKey) || e.key === "Tab")
      ) {
        e.preventDefault();
        pickTag(option);
        return;
      }
      if (e.key === "Escape") {
        e.preventDefault();
        setTyping(null);
        return;
      }
    }
    // Esc with nothing typed stops everything at work in the session.
    if (e.key === "Escape" && !value && running && !stopping) {
      e.preventDefault();
      stopEverything();
      return;
    }
    // Enter sends now; Cmd+Enter (Ctrl+Enter) queues behind what is running.
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send(!(e.metaKey || e.ctrlKey));
    }
  };

  const attach = async (picked: FileList | null) => {
    if (!picked) return;
    const { files: next, skipped } = await readAttached(picked, files);
    setFiles(next);
    setNotice(skipped.length > 0 ? skipped.join(". ") : null);
  };

  // A voice note: record until stopped (or MAX_RECORDING_MS), then send the
  // recording to be transcribed and put its words, marked, after the draft.
  const record = async () => {
    if (!voice || transcribing) return;
    if (recording) return recorder.current?.stop();
    let stream: MediaStream;
    try {
      stream = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch {
      setNotice(defaultCopy.voice.denied);
      return;
    }
    const type = recordingType((t) => MediaRecorder.isTypeSupported(t));
    const r = new MediaRecorder(stream, type ? { mimeType: type } : undefined);
    const chunks: Blob[] = [];
    let size = 0;
    r.ondataavailable = (e) => {
      chunks.push(e.data);
      size += e.data.size;
      // Past what the server takes, stop: nothing more would be heard.
      if (size > voice.maxBytes && r.state === "recording") r.stop();
    };
    const limit = setTimeout(() => r.state === "recording" && r.stop(), MAX_RECORDING_MS);
    r.onstop = () => {
      clearTimeout(limit);
      for (const track of stream.getTracks()) track.stop();
      recorder.current = null;
      setRecording(false);
      setMic(null);
      const audio = new Blob(chunks, { type: r.mimeType || type || "audio/webm" });
      if (audio.size === 0) return;
      if (audio.size > voice.maxBytes) {
        setNotice(defaultCopy.voice.tooLong);
        return;
      }
      setTranscribing(true);
      setNotice(defaultCopy.voice.transcribing);
      voice
        .transcribe(audio)
        .then((t) => {
          change(withVoiceNote(input.current?.value ?? "", t));
          setNotice(null);
          input.current?.focus();
        })
        .catch((e: unknown) =>
          setNotice(defaultCopy.voice.failed(e instanceof Error ? e.message : String(e))),
        )
        .finally(() => setTranscribing(false));
    };
    recorder.current = r;
    setNotice(defaultCopy.voice.recording);
    setRecording(true);
    setMic(stream);
    // A chunk a second, so the size is known while it records.
    r.start(1000);
  };

  const dictate = () => {
    if (records) return void record();
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
    r.onerror = (event) => {
      setListening(false);
      setNotice(
        event.error === "not-allowed" || event.error === "service-not-allowed"
          ? "Dictation is not allowed. Check microphone permissions for this page."
          : event.error === "no-speech"
            ? "No speech was detected. Try again."
            : "The speech service is unavailable. You can keep typing your message.",
      );
    };
    speech.current = r;
    setListening(true);
    r.start();
  };

  const micLabel = records
    ? recording
      ? defaultCopy.voice.stop
      : defaultCopy.voice.record
    : listening
      ? "Stop dictating"
      : "Dictate";

  return (
    <div className="humagen-composer-container relative z-(--aui-z-composer) flex min-w-0 flex-col gap-1.5">
      {chooseSession && (
        <Modal title={`Session for @${chooseSession}`} onClose={() => setChooseSession(undefined)}>
          <div className="flex flex-col gap-4 p-5">
            <h3 className="text-aui-lg font-medium">Session for @{chooseSession}</h3>
            <p className="text-aui-sm text-aui-muted">
              Continue with this agent’s earlier context, or start a separate session. Your message
              stays in the draft until you send it.
            </p>
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className={primary}
                onClick={() => {
                  setSessionPick({ agent: chooseSession, fresh: false });
                  setChooseSession(undefined);
                }}
              >
                Continue
              </button>
              <button
                type="button"
                className={button}
                onClick={() => {
                  setSessionPick({ agent: chooseSession, fresh: true });
                  setChooseSession(undefined);
                }}
              >
                Start fresh
              </button>
            </div>
          </div>
        </Modal>
      )}
      <form
        ref={form}
        aria-label="Message"
        onSubmit={(e) => {
          e.preventDefault();
          send(true);
        }}
        className="humagen-composer relative flex flex-col gap-1 rounded-[1.25rem] border border-aui-border bg-aui-surface p-1.5 transition-colors focus-within:border-aui-fg motion-reduce:transition-none"
      >
        {mic && (
          <LazyFrame fallback={null} instead={null} resetKey={lit}>
            <VoiceLevel stream={mic} host={form.current} />
          </LazyFrame>
        )}
        {tags && commands.length > 0 && (
          // Still: a list waiting on a person does not animate.
          <ul
            id="composer-tags"
            data-layer="completion"
            data-commands
            role="listbox"
            aria-label={defaultCopy.threads.commands}
            style={{ maxHeight: listAt.max }}
            className={`absolute right-2 left-2 z-(--aui-z-popover) overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1 shadow-lg ${listAt.up ? "bottom-full mb-1 md:[.humagen-home_&]:top-full md:[.humagen-home_&]:bottom-auto md:[.humagen-home_&]:mt-1 md:[.humagen-home_&]:mb-0" : "top-full mt-1"}`}
          >
            {commands.map((c, i) => (
              <li
                key={c.option.token}
                id={`composer-tag-${i}`}
                role="option"
                aria-selected={i === current}
                aria-disabled={c.disabled ? true : undefined}
                onMouseDown={(e) => {
                  // Keep the text box's focus and caret while picking.
                  e.preventDefault();
                  if (!c.disabled) pickTag(c.option);
                }}
                className="flex pointer-coarse:min-h-11 min-w-0 cursor-pointer items-center gap-3 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg aria-disabled:cursor-default aria-disabled:text-aui-muted aria-selected:bg-aui-hover"
              >
                <span className="shrink-0 font-medium">
                  /<Marked text={c.option.token} hits={c.nameHits} />
                </span>
                <span className="min-w-0 flex-1 truncate text-aui-xs text-aui-muted">
                  {c.disabled ??
                    (c.option.hint && <Marked text={c.option.hint} hits={c.hintHits} />)}
                </span>
                <span className="shrink-0 text-aui-2xs text-aui-muted">
                  {defaultCopy.threads.commandKind}
                </span>
              </li>
            ))}
          </ul>
        )}
        {tags && (shown.length > 0 || agentsPending) && (
          // Still: a list waiting on a person does not animate.
          <ul
            id="composer-tags"
            data-layer="completion"
            role="listbox"
            aria-label={tags.label}
            style={{ maxHeight: listAt.max }}
            className={`absolute right-2 left-2 z-(--aui-z-popover) overflow-y-auto rounded-aui border border-aui-border bg-aui-surface p-1 shadow-lg ${listAt.up ? "bottom-full mb-1 md:[.humagen-home_&]:top-full md:[.humagen-home_&]:bottom-auto md:[.humagen-home_&]:mt-1 md:[.humagen-home_&]:mb-0" : "top-full mt-1"}`}
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
                className="flex cursor-pointer pointer-coarse:min-h-11 items-center gap-2.5 rounded-aui px-2.5 py-1.5 text-aui-sm text-aui-fg aria-selected:bg-aui-hover"
              >
                {o.kind === "agent" ? (
                  <AgentMark name={o.token} size={16} />
                ) : (
                  <span
                    aria-hidden
                    className="inline-flex size-4 shrink-0 items-center justify-center rounded-full bg-aui-hover text-aui-2xs leading-none font-medium"
                  >
                    {o.kind === "person" ? (o.hint ?? o.label).trim().charAt(0).toUpperCase() : "@"}
                  </span>
                )}
                <span className="shrink-0 font-medium">
                  {sigilOf(o)}
                  {o.token}
                </span>
                {o.hint && (
                  <span className="min-w-0 truncate text-aui-xs text-aui-muted">{o.hint}</span>
                )}
              </li>
            ))}
            {agentsPending && (
              // Not an option to pick: the keys pass over it, and Enter still sends.
              <li
                role="option"
                aria-disabled
                aria-selected={false}
                className="flex pointer-coarse:min-h-11 items-center gap-2.5 rounded-aui px-2.5 py-1.5 text-aui-xs text-aui-muted"
              >
                <span aria-hidden className="aui-skeleton size-4 shrink-0 rounded-full" />
                {defaultCopy.chat.loadingAgents}
              </li>
            )}
          </ul>
        )}
        {plus && (
          <Presence when={menu} ms={130}>
            {(leaving) => (
              <div
                key={menuEpoch}
                data-plus-panel
                inert={leaving}
                aria-hidden={leaving || undefined}
                data-leaving={leaving}
                className="humagen-composer-menu contents"
              >
                {plus.menu(
                  closeMenu,
                  () => {
                    setMenu(false);
                    picker.current?.click();
                  },
                  () => setMenu(false),
                  value,
                )}
              </div>
            )}
          </Presence>
        )}
        <QueueStrip queue={queue} />
        {sessionPick && mentionsIn(value, [sessionPick.agent]).length > 0 && (
          <div className="flex items-center gap-2 px-2 text-aui-xs text-aui-muted">
            <span>
              {sessionPick.fresh ? "Start fresh" : "Continue"} @{sessionPick.agent}
            </span>
            <button
              type="button"
              className={tool}
              aria-label="Clear session choice"
              onClick={() => {
                setSessionPick(undefined);
                input.current?.focus();
              }}
            >
              <XIcon size={14} />
            </button>
          </div>
        )}
        {quote && (
          <div
            className="flex min-w-0 items-center gap-2 rounded-aui bg-aui-hover px-3 py-2 text-aui-xs"
            data-reply-quote
          >
            <a href={quote.url} className="min-w-0 flex-1 truncate" title={quote.text}>
              Replying to {quote.label}
            </a>
            <button
              type="button"
              aria-label="Remove reply"
              className={tool}
              onClick={() => {
                setQuote(undefined);
                input.current?.focus();
              }}
            >
              <XIcon size={14} />
            </button>
          </div>
        )}
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
                  onClick={() => {
                    setFiles(files.filter((x) => x.id !== f.id));
                    setNotice(null);
                    input.current?.focus();
                  }}
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
        {tokens.length > 0 && (
          // The skills the message names, drawn behind the text as tokens.
          <div
            ref={backdrop}
            aria-hidden
            data-tokens
            className={`pointer-events-none absolute overflow-hidden break-words whitespace-pre-wrap text-transparent${held ? " opacity-60" : ""}`}
          >
            {tokenRuns(value, tokens).map((run, i) =>
              run.token ? (
                // biome-ignore lint/suspicious/noArrayIndexKey: runs are positional
                <mark key={i} className="rounded-sm bg-aui-accent-soft text-transparent">
                  {run.text}
                </mark>
              ) : (
                // biome-ignore lint/suspicious/noArrayIndexKey: runs are positional
                <span key={i}>{run.text}</span>
              ),
            )}
            {"\u200b"}
          </div>
        )}
        {tags && typing?.query && typing.sigil === "@" && !shown.length && !agentsPending && (
          <p role="status" className={floatNote}>
            No matching people or agents for @{typing.query}
          </p>
        )}
        {tags && typing?.sigil === "/" && !commands.length && (
          <p role="status" className={floatNote}>
            {tags.options.some((o) => o.kind === "skill") ? (
              <>No skill matches /{typing.query}.</>
            ) : (
              <>
                No skills yet. Skills become / commands here.{" "}
                <a href="/library/skills" className="underline hover:text-aui-fg">
                  Create a skill
                </a>
              </>
            )}
          </p>
        )}
        {notice && (
          <p role="status" className={floatNote}>
            {notice}
          </p>
        )}
        {/*
          One row: + on the left, the words, and on the right two plain icon
          buttons of equal weight, Send (or Stop while anything runs) and the
          microphone. The row grows upward with the words; the buttons stay
          on its last line. Both are always there: talk or type at any time.
        */}
        <div className="humagen-composer-row flex items-end gap-1">
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
              className={`${tool} humagen-composer-plus`}
              aria-label={defaultCopy.chat.plus}
              aria-haspopup="dialog"
              aria-expanded={menu}
              title={`${defaultCopy.chat.plus} (${defaultCopy.chat.plusShortcut(/Mac|iPhone|iPad/.test(navigator.platform))})`}
              onClick={() => (menu ? closeMenu() : showMenu())}
            >
              <Glyph d={PLUS} />
            </button>
          ) : (
            <button
              type="button"
              className={`${tool} humagen-composer-plus`}
              aria-label="Attach files"
              title="Attach text files"
              onClick={() => picker.current?.click()}
            >
              <Glyph d={PLUS} />
            </button>
          )}
          <textarea
            id="composer-input"
            ref={input}
            rows={1}
            value={value}
            readOnly={held}
            aria-readonly={held || undefined}
            // Who a mention asks, else the caller's own words: never the default agent's token.
            placeholder={
              mentioned.length > 0
                ? `Message ${mentioned.map((name) => `@${name}`).join(", ")}`
                : running
                  ? defaultCopy.dock.hint(mac)
                  : emptyText || placeholder || defaultCopy.chat.placeholder
            }
            onChange={(e) => {
              change(e.target.value);
              follow(e.target.value);
            }}
            onKeyDown={onKeyDown}
            onKeyUp={(e) => {
              if (
                e.key === "ArrowLeft" ||
                e.key === "ArrowRight" ||
                e.key === "Home" ||
                e.key === "End"
              )
                follow(value);
            }}
            onClick={() => follow(value)}
            onBlur={() => setTyping(null)}
            onScroll={(e) => {
              if (backdrop.current) backdrop.current.scrollTop = e.currentTarget.scrollTop;
            }}
            // A multiline text box that completes from a list, not a combobox:
            // a textarea may not take that role, and a combobox is one line.
            // The open list is said by aria-controls and the active option.
            aria-haspopup={tags ? "listbox" : undefined}
            aria-controls={tags && listed ? "composer-tags" : undefined}
            aria-autocomplete={tags ? "list" : undefined}
            aria-activedescendant={
              tags && open && current >= 0 ? `composer-tag-${current}` : undefined
            }
            className={`humagen-composer-input relative max-h-[200px] min-h-9 pointer-coarse:min-h-11 min-w-0 flex-1 resize-none self-center bg-transparent px-1 py-1.5 text-aui-base text-aui-fg outline-none placeholder:text-aui-muted${held ? " opacity-60" : ""}`}
          />
          {running ? (
            // While anything in the session runs, Send is Stop, for all of it. Esc does too.
            <button
              type="button"
              data-layer="stop-all"
              className={`${tool} humagen-composer-stop text-aui-fg disabled:cursor-progress`}
              aria-label={stopping ? defaultCopy.dock.stoppingAll : defaultCopy.dock.stopAllLabel}
              title={defaultCopy.dock.stopAllHint}
              disabled={stopping}
              onClick={stopEverything}
            >
              <span aria-hidden className="size-3 rounded-[2px] bg-current" />
            </button>
          ) : (
            <button
              type="submit"
              data-layer="send"
              disabled={!ready || held}
              aria-label="Send message"
              title={defaultCopy.dock.sendHint(mac)}
              className={`${tool} humagen-composer-send text-aui-fg disabled:text-aui-muted`}
            >
              <Glyph d={RETURN} />
            </button>
          )}
          {canDictate && (
            // Records a voice note (or dictates, where the deployment does not
            // transcribe); while it records or listens it is the stop, in the same place.
            <button
              type="button"
              data-layer="mic"
              className={`${tool} humagen-composer-mic ${listening || recording ? "bg-aui-accent-soft text-aui-accent hover:bg-aui-accent-soft hover:text-aui-accent" : ""}`}
              aria-label={micLabel}
              aria-pressed={listening || recording}
              title={micLabel}
              disabled={transcribing}
              onClick={dictate}
            >
              {listening || recording ? (
                <span
                  aria-hidden
                  className="size-3 animate-pulse rounded-[2px] bg-current motion-reduce:animate-none"
                />
              ) : (
                <Glyph d={MIC} />
              )}
            </button>
          )}
        </div>
        {(leading || plus?.chips || trailing) && (
          <div className="humagen-composer-bar flex items-center gap-1">
            {leading}
            {/* What the + menu picked sits under the words, so the row keeps its size. */}
            <div className="humagen-composer-chips min-w-0 flex-1">{plus?.chips}</div>
            {trailing}
          </div>
        )}
      </form>
      {footer}
      <AiNotice />
    </div>
  );
}

/**
 * The line under every composer: the product is AI and can make mistakes,
 * with a link to the workspace's page of terms when its branding names one
 * (branding.terms_url). A line of its own, so it reads the same at every
 * width and nothing in the footer row moves for it.
 */
function AiNotice() {
  const terms = termsUrl();
  return (
    <p data-layer="ai-notice" className="px-2 text-center text-aui-xs leading-6 text-aui-muted">
      {defaultCopy.dock.aiNotice(defaultCopy.product)}
      {terms && (
        <>
          {" "}
          <a className={textLink} href={terms} target="_blank" rel="noreferrer">
            {defaultCopy.dock.terms}
          </a>
        </>
      )}
    </p>
  );
}

/** Text with the runs a query matched highlighted. */
function Marked({ text, hits }: { text: string; hits: Hit[] }) {
  return (
    <>
      {segments(text, hits).map((seg, i) =>
        seg.hit ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: segments are positional
          <mark key={i} className="rounded-sm bg-aui-accent-soft text-aui-accent">
            {seg.text}
          </mark>
        ) : (
          seg.text
        ),
      )}
    </>
  );
}

/** The draft in runs, each a token or the text between. */
function tokenRuns(
  text: string,
  tokens: { start: number; end: number }[],
): { text: string; token: boolean }[] {
  const out: { text: string; token: boolean }[] = [];
  let at = 0;
  for (const t of tokens) {
    if (t.start > at) out.push({ text: text.slice(at, t.start), token: false });
    out.push({ text: text.slice(t.start, t.end), token: true });
    at = t.end;
  }
  if (at < text.length) out.push({ text: text.slice(at), token: false });
  return out;
}
