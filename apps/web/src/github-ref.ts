export interface GitHubRef {
  repo: string;
  number: number;
  kind: "pull" | "issue";
}
/** Only public GitHub issue/pull addresses, or a number with an explicit repository. */
export function githubRef(input: string, repo?: string): GitHubRef | undefined {
  const text = input.trim();
  const short = /^#([1-9]\d*)$/.exec(text);
  if (short && Number(short[1]) <= 2147483647 && repo && /^[\w.-]+\/[\w.-]+$/.test(repo))
    return { repo, number: Number(short[1]), kind: "issue" };
  const match =
    /^(?:https:\/\/github\.com\/)?([\w.-]+\/[\w.-]+)\/(pull|issues)\/([1-9]\d*)(?:[?#].*)?$/.exec(
      text,
    );
  const internal = /^([\w.-]+\/[\w.-]+)#([1-9]\d*)$/.exec(text);
  if (internal && Number(internal[2]) <= 2147483647)
    return { repo: internal[1]!, number: Number(internal[2]), kind: "pull" };
  if (!match || Number(match[3]) > 2147483647) return undefined;
  return {
    repo: match[1]!,
    number: Number(match[3]),
    kind: match[2] === "pull" ? "pull" : "issue",
  };
}
