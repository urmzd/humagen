import { defaultCopy } from "@humagen/components/copy";
import {
  CodeIcon,
  MessageSquareIcon,
  SaveIcon,
  SearchIcon,
  WrapIcon,
  XIcon,
} from "@humagen/render-web";
import { type KeyboardEvent, useId, useMemo, useRef, useState } from "react";

export interface CodeEditorProps {
  path: string;
  value: string;
  onChange: (value: string) => void;
  onSave?: () => void;
  onAskSelection?: (selection: { text: string; startLine: number; endLine: number }) => void;
  readOnly?: boolean;
  saving?: boolean;
  maxBytes?: number;
}

const words = defaultCopy.editor;
const control =
  "inline-flex min-h-9 items-center justify-center gap-1.5 rounded-aui-sm px-2 text-aui-xs hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50 pointer-coarse:min-h-11";
const languages: Record<string, string> = {
  ts: "TypeScript",
  tsx: "TypeScript JSX",
  js: "JavaScript",
  jsx: "JavaScript JSX",
  py: "Python",
  go: "Go",
  rs: "Rust",
  md: "Markdown",
  mdx: "Markdown JSX",
  json: "JSON",
  yaml: "YAML",
  yml: "YAML",
  html: "HTML",
  css: "CSS",
  sh: "Shell",
  sql: "SQL",
  toml: "TOML",
  diff: words.diffLanguage,
  patch: words.diffLanguage,
  txt: words.plainText,
};

/** A native text editor with explicit keyboard escape and no code execution. */
export function CodeEditor({
  path,
  value,
  onChange,
  onSave,
  onAskSelection,
  readOnly = false,
  saving = false,
  maxBytes = 2 * 1024 * 1024,
}: CodeEditorProps) {
  const input = useRef<HTMLTextAreaElement>(null);
  const gutter = useRef<HTMLPreElement>(null);
  const find = useRef<HTMLInputElement>(null);
  const help = useId();
  const [wrap, setWrap] = useState(false);
  const [searching, setSearching] = useState(false);
  const [query, setQuery] = useState("");
  const [notice, setNotice] = useState("");
  const [position, setPosition] = useState({ line: 1, column: 1 });
  const [escapeTab, setEscapeTab] = useState(false);
  const [selection, setSelection] = useState({ start: 0, end: 0 });
  const bytes = useMemo(() => new TextEncoder().encode(value).length, [value]);
  const lines = useMemo(() => value.split("\n"), [value]);
  const language = languages[path.split(".").at(-1)?.toLowerCase() ?? ""] ?? words.plainText;
  const tooLarge = bytes > maxBytes || lines.length > 20000;
  const updatePosition = () => {
    const caret = input.current?.selectionStart ?? 0;
    setSelection({ start: caret, end: input.current?.selectionEnd ?? caret });
    const preceding = value.slice(0, caret);
    setPosition({
      line: preceding.split("\n").length,
      column: caret - preceding.lastIndexOf("\n"),
    });
  };
  const change = (next: string) => {
    if (new TextEncoder().encode(next).length > maxBytes || next.split("\n").length > 20000) {
      setNotice(words.editTooLarge);
      return false;
    }
    setNotice("");
    onChange(next);
    return true;
  };
  const replace = (next: string, start: number, end = start) => {
    if (!change(next)) return;
    requestAnimationFrame(() => {
      input.current?.focus();
      input.current?.setSelectionRange(start, end);
      const prefix = next.slice(0, start);
      setPosition({ line: prefix.split("\n").length, column: start - prefix.lastIndexOf("\n") });
    });
  };
  const indent = (back = false) => {
    const textarea = input.current;
    if (!textarea || readOnly || saving) return;
    const start = textarea.selectionStart;
    const end = textarea.selectionEnd;
    if (start === end && !back) {
      replace(`${value.slice(0, start)}  ${value.slice(end)}`, start + 2);
      return;
    }
    const blockStart = start === 0 ? 0 : value.lastIndexOf("\n", start - 1) + 1;
    // A selection ending at the next line's start does not select that line.
    const lastSelected = end > start && value[end - 1] === "\n" ? end - 1 : end;
    const nextBreak = value.indexOf("\n", lastSelected);
    const blockEnd = nextBreak < 0 ? value.length : nextBreak;
    const block = value.slice(blockStart, blockEnd);
    let first = 0;
    let delta = 0;
    const indented = block
      .split("\n")
      .map((line, index) => {
        const count = back ? (line.match(/^(?: {1,2}|\t)/)?.[0].length ?? 0) : -2;
        const offset = -count;
        if (index === 0) first = offset;
        delta += offset;
        return back ? line.slice(count) : `  ${line}`;
      })
      .join("\n");
    replace(
      `${value.slice(0, blockStart)}${indented}${value.slice(blockEnd)}`,
      Math.max(blockStart, start + first),
      Math.max(blockStart, end + delta),
    );
  };
  const openFind = () => {
    setSearching(true);
    requestAnimationFrame(() => find.current?.focus());
  };
  const findNext = () => {
    if (!query || !input.current) return;
    const start = input.current.selectionEnd;
    const at = value.indexOf(query, start);
    const match = at >= 0 ? at : value.indexOf(query);
    if (match < 0) {
      setNotice(words.noMatches);
      return;
    }
    input.current.focus();
    input.current.setSelectionRange(match, match + query.length);
    if (!wrap)
      input.current.scrollTop = Math.max(0, value.slice(0, match).split("\n").length - 3) * 24;
    if (gutter.current) gutter.current.scrollTop = input.current.scrollTop;
    setNotice(at >= 0 ? words.matchSelected : words.searchWrapped);
    const prefix = value.slice(0, match);
    setPosition({ line: prefix.split("\n").length, column: match - prefix.lastIndexOf("\n") });
  };
  const keyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.nativeEvent.isComposing) return;
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "f") {
      event.preventDefault();
      openFind();
      return;
    }
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === "s" && onSave) {
      event.preventDefault();
      if (!readOnly && !saving && !tooLarge) onSave();
      return;
    }
    if (event.key === "Escape" && !readOnly && !saving) {
      event.preventDefault();
      event.stopPropagation();
      setEscapeTab(true);
      return;
    }
    if (event.key === "Tab" && !readOnly && !saving) {
      if (escapeTab) {
        setEscapeTab(false);
        return;
      }
      event.preventDefault();
      indent(event.shiftKey);
      return;
    }
    setEscapeTab(false);
    if (
      event.key === "Enter" &&
      !readOnly &&
      !saving &&
      !event.ctrlKey &&
      !event.metaKey &&
      !event.altKey
    ) {
      event.preventDefault();
      const start = event.currentTarget.selectionStart;
      const end = event.currentTarget.selectionEnd;
      const lineStart = value.lastIndexOf("\n", start - 1) + 1;
      const leading = value.slice(lineStart, start).match(/^[\t ]*/)?.[0] ?? "";
      replace(
        `${value.slice(0, start)}\n${leading}${value.slice(end)}`,
        start + 1 + leading.length,
      );
    }
  };
  return (
    <section
      data-slot="code-editor"
      aria-label={words.region(path)}
      className="flex min-w-0 flex-col overflow-hidden rounded-aui border border-aui-border bg-aui-code text-aui-fg"
    >
      <header className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-1 border-b border-aui-border px-2 py-1">
        <CodeIcon size={16} className="text-aui-muted" />
        <span title={path} className="min-w-0 flex-1 truncate font-aui-mono text-aui-xs">
          {path}
        </span>
        <span className="text-aui-2xs text-aui-muted">{language}</span>
        <button type="button" className={control} onClick={openFind} aria-expanded={searching}>
          <SearchIcon size={14} />
          {words.find}
        </button>
        <button
          type="button"
          className={control}
          onClick={() => setWrap(!wrap)}
          aria-pressed={wrap}
        >
          <WrapIcon size={14} />
          {words.wrap}
        </button>
        {onSave && (
          <button
            type="button"
            className={control}
            disabled={readOnly || saving || tooLarge}
            onClick={onSave}
            title={words.saveShortcut}
          >
            <SaveIcon size={14} />
            {saving ? words.saving : words.save}
          </button>
        )}
        {onAskSelection && (
          <button
            type="button"
            className={control}
            disabled={selection.start === selection.end || saving || tooLarge}
            onClick={() => {
              const textarea = input.current;
              if (!textarea || textarea.selectionStart === textarea.selectionEnd) return;
              const start = textarea.selectionStart;
              const end = textarea.selectionEnd;
              if (end - start > 16000) {
                setNotice(words.selectionTooLarge);
                return;
              }
              onAskSelection({
                text: value.slice(start, end),
                startLine: value.slice(0, start).split("\n").length,
                endLine: value.slice(0, value[end - 1] === "\n" ? end - 1 : end).split("\n").length,
              });
            }}
          >
            <MessageSquareIcon size={14} />
            {words.askSelection}
          </button>
        )}
      </header>
      {searching && (
        <form
          className="flex flex-wrap items-center gap-1 border-b border-aui-border px-2 py-1"
          onSubmit={(event) => {
            event.preventDefault();
            findNext();
          }}
        >
          <input
            ref={find}
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            aria-label={words.findLabel}
            className="min-h-9 min-w-0 flex-1 rounded-aui-sm border border-aui-border bg-aui-bg px-2 text-base outline-aui-accent sm:text-aui-xs"
            onKeyDown={(event) => {
              if (event.key === "Escape") {
                event.preventDefault();
                event.stopPropagation();
                setSearching(false);
                input.current?.focus();
              }
            }}
          />
          <button type="submit" className={control} disabled={!query}>
            {words.findNext}
          </button>
          <button
            type="button"
            className={control}
            aria-label={words.closeFind}
            onClick={() => {
              setSearching(false);
              input.current?.focus();
            }}
          >
            <XIcon size={14} />
          </button>
        </form>
      )}
      {tooLarge ? (
        <p role="alert" className="p-4 text-aui-sm">
          {words.fileTooLarge(Math.floor(maxBytes / 1024))}
        </p>
      ) : (
        <div className="flex min-h-64 min-w-0">
          {!wrap && (
            <pre
              ref={gutter}
              aria-hidden
              className="m-0 max-h-[55dvh] min-h-64 shrink-0 select-none overflow-hidden border-r border-aui-border px-2 py-3 text-right font-aui-mono text-base leading-6 text-aui-muted sm:text-aui-xs sm:leading-6"
            >
              {lines.map((_, index) => index + 1).join("\n")}
            </pre>
          )}
          <textarea
            ref={input}
            value={value}
            onChange={(event) => change(event.target.value)}
            readOnly={readOnly || saving}
            aria-label={words.edit(path)}
            aria-describedby={help}
            spellCheck={false}
            autoCapitalize="off"
            autoCorrect="off"
            wrap={wrap ? "soft" : "off"}
            onSelect={updatePosition}
            onKeyDown={keyDown}
            onScroll={(event) => {
              if (gutter.current) gutter.current.scrollTop = event.currentTarget.scrollTop;
            }}
            className="m-0 min-h-64 min-w-0 flex-1 resize-y border-0 bg-transparent px-3 py-3 font-aui-mono text-base leading-6 text-aui-fg outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-aui-accent sm:text-aui-xs sm:leading-6"
            style={{ height: "min(55dvh, 36rem)", tabSize: 2 }}
          />
        </div>
      )}
      <footer className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-aui-border px-2 py-1 text-aui-2xs text-aui-muted">
        <output className="tabular-nums">{words.position(position.line, position.column)}</output>
        <span>{words.lines(lines.length)}</span>
        <span>{words.bytes(bytes)}</span>
        {readOnly ? (
          <span>{words.readOnly}</span>
        ) : (
          <>
            <button
              type="button"
              className={control}
              disabled={saving || tooLarge}
              onClick={() => indent()}
            >
              {words.indent}
            </button>
            <button
              type="button"
              className={control}
              disabled={saving || tooLarge}
              onClick={() => indent(true)}
            >
              {words.unindent}
            </button>
          </>
        )}
        <span id={help}>{readOnly ? "" : words.keyboardHelp}</span>
      </footer>
      <output className={notice ? "px-3 py-2 text-aui-xs text-aui-muted" : "sr-only"}>
        {notice}
      </output>
    </section>
  );
}
