/**
 * A small, line-at-a-time highlighter for code excerpts: comments, strings,
 * numbers and keywords, chosen by the file's extension. It never runs or
 * parses the code, and a line it cannot read is plain text. A block comment or
 * string that spans lines is read from the excerpt's first line on, so lines
 * cut from the middle of one may be coloured as code; the text stays exact.
 */

export type TokenKind = "plain" | "comment" | "string" | "number" | "keyword";
export interface Token {
  kind: TokenKind;
  text: string;
}

interface Grammar {
  line: string[];
  block?: [string, string];
  quotes: string[];
  keywords: Set<string>;
}

const words = (s: string) => new Set(s.split(/\s+/));
const C_LIKE = { line: ["//"], block: ["/*", "*/"] as [string, string] };

const GRAMMARS: Record<string, Grammar> = {
  go: {
    ...C_LIKE,
    quotes: ['"', "'", "`"],
    keywords: words(
      "break case chan const continue default defer else fallthrough for func go goto if import interface map package range return select struct switch type var nil true false",
    ),
  },
  ts: {
    ...C_LIKE,
    quotes: ['"', "'", "`"],
    keywords: words(
      "as async await break case catch class const continue default delete do else enum export extends false finally for from function if import in instanceof interface let new null of return static super switch this throw true try type typeof undefined var void while yield",
    ),
  },
  rust: {
    ...C_LIKE,
    quotes: ['"'],
    keywords: words(
      "as async await break const continue crate dyn else enum extern false fn for if impl in let loop match mod move mut pub ref return self Self static struct super trait true type unsafe use where while",
    ),
  },
  py: {
    line: ["#"],
    quotes: ['"', "'"],
    keywords: words(
      "and as assert async await break class continue def del elif else except False finally for from global if import in is lambda None nonlocal not or pass raise return True try while with yield",
    ),
  },
  sh: {
    line: ["#"],
    quotes: ['"', "'"],
    keywords: words(
      "if then else elif fi for while do done case esac function in return local export",
    ),
  },
  yaml: { line: ["#"], quotes: ['"', "'"], keywords: words("true false null") },
};
const EXT: Record<string, string> = {
  go: "go",
  ts: "ts",
  tsx: "ts",
  js: "ts",
  jsx: "ts",
  mjs: "ts",
  java: "ts",
  kt: "ts",
  c: "ts",
  h: "ts",
  cc: "ts",
  cpp: "ts",
  cs: "ts",
  swift: "ts",
  rs: "rust",
  py: "py",
  rb: "py",
  sh: "sh",
  bash: "sh",
  zsh: "sh",
  yaml: "yaml",
  yml: "yaml",
  toml: "yaml",
};

/** The grammar for a path, or undefined when the excerpt is shown plain. */
function grammarOf(path: string): Grammar | undefined {
  const name = path.split("/").pop() ?? "";
  if (name === "Makefile" || name === "Dockerfile") return GRAMMARS.sh;
  const ext = name.includes(".") ? (name.split(".").pop() ?? "").toLowerCase() : "";
  const key = EXT[ext];
  return key ? GRAMMARS[key] : undefined;
}

/** Tokens per line, in order; joined, each line's tokens are exactly the line. */
export function highlight(path: string, lines: string[]): Token[][] {
  const g = grammarOf(path);
  if (!g) return lines.map((text) => [{ kind: "plain", text }]);
  let inBlock = false;
  return lines.map((line) => {
    const out: Token[] = [];
    const push = (kind: TokenKind, text: string) => {
      if (!text) return;
      const last = out[out.length - 1];
      if (last && last.kind === kind) last.text += text;
      else out.push({ kind, text });
    };
    let i = 0;
    while (i < line.length) {
      if (inBlock && g.block) {
        const close = line.indexOf(g.block[1], i);
        if (close === -1) {
          push("comment", line.slice(i));
          i = line.length;
        } else {
          push("comment", line.slice(i, close + g.block[1].length));
          i = close + g.block[1].length;
          inBlock = false;
        }
        continue;
      }
      const rest = line.slice(i);
      const lineComment = g.line.find((m) => rest.startsWith(m));
      if (lineComment) {
        push("comment", rest);
        break;
      }
      if (g.block && rest.startsWith(g.block[0])) {
        inBlock = true;
        push("comment", g.block[0]);
        i += g.block[0].length;
        continue;
      }
      const ch = line[i] as string;
      if (g.quotes.includes(ch)) {
        let j = i + 1;
        while (j < line.length && line[j] !== ch) j += line[j] === "\\" ? 2 : 1;
        push("string", line.slice(i, Math.min(j + 1, line.length)));
        i = Math.min(j + 1, line.length);
        continue;
      }
      const num = /^(?:0x[0-9a-fA-F_]+|\d[\d_]*(?:\.\d+)?)/.exec(rest);
      if (num && !/[\w$]/.test(line[i - 1] ?? "")) {
        push("number", num[0]);
        i += num[0].length;
        continue;
      }
      const word = /^[A-Za-z_$][\w$]*/.exec(rest);
      if (word) {
        push(g.keywords.has(word[0]) ? "keyword" : "plain", word[0]);
        i += word[0].length;
        continue;
      }
      push("plain", ch);
      i++;
    }
    return out;
  });
}
