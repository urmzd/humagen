import { createContext, type ReactNode, useContext } from "react";
import { GitHubIcon } from "./brand-icons";

/**
 * GitHub references in an answer, drawn as chips: a pull request, an issue
 * or a commit by its address, and `owner/repo#123` written out. A chip is
 * the reference's mark and its short name, still a link to GitHub. A bare
 * `owner/repo#123` does not say whether it is a pull request or an issue,
 * so it carries GitHub's mark rather than guess.
 */
export type GitHubRefKind = "pull" | "issue" | "commit" | "ref";

export interface GitHubRef {
  kind: GitHubRefKind;
  /** "urmzd/humagen#461", or "urmzd/humagen@9f2c4e1" for a commit. */
  label: string;
}

/** The words a chip is announced with; the host passes its copy. */
export interface GitHubRefWords {
  pull: (ref: string) => string;
  issue: (ref: string) => string;
  commit: (ref: string) => string;
  ref: (ref: string) => string;
}

export const GitHubRefWordsContext = createContext<GitHubRefWords>({
  pull: (r) => `Pull request ${r}`,
  issue: (r) => `Issue ${r}`,
  commit: (r) => `Commit ${r}`,
  ref: (r) => `GitHub ${r}`,
});

const ADDRESS =
  /^https:\/\/github\.com\/([\w.-]+\/[\w.-]+)\/(pull|issues|commit)\/([1-9]\d{0,9}|[0-9a-f]{7,40})\/?(?:[?#].*)?$/i;
const BARE = /(^|[^\w/.#@-])([A-Za-z\d][\w.-]*\/[\w.-]*\w)#([1-9]\d{0,9})(?![\w#])/g;

/** The reference a link's address names, or undefined for any other address. */
export function githubRefOf(href: string | undefined): GitHubRef | undefined {
  const m = href ? ADDRESS.exec(href) : null;
  if (!m) return undefined;
  const [, repo, kind, id] = m as unknown as [string, string, string, string];
  if (kind === "commit")
    return /^[0-9a-f]{7,40}$/i.test(id)
      ? { kind: "commit", label: `${repo}@${id.slice(0, 7)}` }
      : undefined;
  if (!/^\d+$/.test(id)) return undefined;
  return { kind: kind === "pull" ? "pull" : "issue", label: `${repo}#${id}` };
}

type Node = { type: string; value?: string; url?: string; children?: Node[] };

/** `owner/repo#123` in a text node as links to it, the rest of the text as it was. */
function splitText(value: string): Node[] | undefined {
  const out: Node[] = [];
  let last = 0;
  BARE.lastIndex = 0;
  for (let m = BARE.exec(value); m; m = BARE.exec(value)) {
    const [, lead, repo, num] = m as unknown as [string, string, string, string];
    const start = m.index + lead.length;
    if (start > last) out.push({ type: "text", value: value.slice(last, start) });
    const label = `${repo}#${num}`;
    out.push({
      type: "link",
      // GitHub sends an issue's address on to the pull request of that number.
      url: `https://github.com/${repo}/issues/${num}`,
      children: [{ type: "text", value: label }],
    });
    last = start + label.length;
  }
  if (!out.length) return undefined;
  if (last < value.length) out.push({ type: "text", value: value.slice(last) });
  return out;
}

function walk(node: Node) {
  if (!node.children || node.type === "link" || node.type === "linkReference") return;
  const next: Node[] = [];
  for (const child of node.children) {
    if (child.type === "text" && child.value) next.push(...(splitText(child.value) ?? [child]));
    else {
      walk(child);
      next.push(child);
    }
  }
  node.children = next;
}

/** A remark plugin: written-out `owner/repo#123` outside code and links becomes a link. */
export function remarkGitHubRefs() {
  return (tree: Node) => walk(tree);
}

const pullIcon = (
  <>
    <circle cx="6" cy="6" r="2.5" />
    <circle cx="6" cy="18" r="2.5" />
    <circle cx="18" cy="18" r="2.5" />
    <path d="M6 8.5v7M18 15.5V9a3 3 0 0 0-3-3h-4M13 3.5 10.5 6 13 8.5" />
  </>
);
const issueIcon = (
  <>
    <circle cx="12" cy="12" r="8.5" />
    <circle cx="12" cy="12" r="1.5" fill="currentColor" />
  </>
);
const commitIcon = (
  <>
    <circle cx="12" cy="12" r="3.5" />
    <path d="M3 12h5.5M15.5 12H21" />
  </>
);

function RefMark({ kind }: { kind: GitHubRefKind }) {
  if (kind === "ref") return <GitHubIcon size={13} className="shrink-0 self-center" />;
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={14}
      height={14}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.75}
      strokeLinecap="round"
      strokeLinejoin="round"
      className="shrink-0 self-center"
      data-ref-kind={kind}
    >
      {kind === "pull" ? pullIcon : kind === "issue" ? issueIcon : commitIcon}
    </svg>
  );
}

/**
 * A link to a GitHub reference as a chip: its mark, then its short name.
 * Link text that only repeats the address (or the bare reference) reads as
 * the short name; any other text is kept.
 */
export function GitHubRefLink({
  href,
  label,
  children,
  bare,
}: {
  href: string;
  label: string;
  children: ReactNode;
  /** Written as `owner/repo#123`, so of unknown kind. */
  bare?: boolean;
}) {
  const words = useContext(GitHubRefWordsContext);
  const ref = githubRefOf(href);
  if (!ref) return null;
  const kind: GitHubRefKind = bare ? "ref" : ref.kind;
  const own = label.trim() === href || label.trim() === ref.label;
  const name = words[kind](ref.label);
  return (
    <a
      href={href}
      target="_blank"
      rel="noopener noreferrer"
      data-github-ref={kind}
      title={name}
      aria-label={own ? name : undefined}
      className="aui-ref inline-flex max-w-full items-baseline gap-1 align-baseline"
    >
      <RefMark kind={kind} />
      <span className="min-w-0 [overflow-wrap:anywhere]">{own ? ref.label : children}</span>
    </a>
  );
}
