import { createContext, type ReactNode } from "react";

/**
 * How the host draws a name an agent wrote as `@name` in its answer: a link
 * to the agent it names, or nothing when the name is no agent's, which leaves
 * the text as it was written. Without a host, every `@name` stays text.
 */
export const MentionContext = createContext<((name: string) => ReactNode | undefined) | null>(null);

type Node = {
  type: string;
  value?: string;
  url?: string;
  children?: Node[];
  data?: { hProperties?: Record<string, string> };
};

/** The property a mention's link carries its name on. */
export const MENTION = "data-mention";

// `@name` at the start of the text or after something that is not part of a
// word, an address or a path: `a@b.co`, `repo@sha` and `/@scope` stay text.
// The name ends on a letter or a digit, and nothing after it makes it part
// of something longer: `@scope/pkg`, `@name.md`, `@host.com` and `@a@b` stay
// text too, though the name in them may be an agent's. A name is taken whole
// or not at all: `@humagen-writer/core` is not a mention of `humagen`.
const AT =
  /(^|[^\w@/.-])@([a-z0-9](?:[a-z0-9_-]*[a-z0-9])?)(?![a-z0-9_]|-[a-z0-9_]|[/@]|\.[a-z0-9])/gi;

/** `@name` in a text node as mentions, the rest of the text as it was. */
export function splitMentions(value: string): Node[] | undefined {
  const out: Node[] = [];
  let last = 0;
  AT.lastIndex = 0;
  for (let m = AT.exec(value); m; m = AT.exec(value)) {
    const [, lead, name] = m as unknown as [string, string, string];
    const start = m.index + lead.length;
    if (start > last) out.push({ type: "text", value: value.slice(last, start) });
    out.push({
      type: "link",
      url: "",
      data: { hProperties: { [MENTION]: name } },
      children: [{ type: "text", value: `@${name}` }],
    });
    last = start + name.length + 1;
  }
  if (!out.length) return undefined;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

const KEPT = new Set(["link", "linkReference", "code", "inlineCode", "html", "definition"]);

function walk(node: Node) {
  if (!node.children || KEPT.has(node.type)) return;
  const next: Node[] = [];
  for (const child of node.children) {
    if (child.type === "text" && child.value) next.push(...(splitMentions(child.value) ?? [child]));
    else {
      walk(child);
      next.push(child);
    }
  }
  node.children = next;
}

/**
 * A remark plugin: `@name` outside code and links becomes a mention, which
 * the host draws as a link when the name is an agent's. It runs on every
 * parse, so a name is linked while the answer that holds it is still arriving.
 */
export function remarkMentions() {
  return (tree: Node) => walk(tree);
}
