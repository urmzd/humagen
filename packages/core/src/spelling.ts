/**
 * Words a name writes in one way: short words in capitals ("qa" is "QA") and
 * brands as their owners spell them ("github" is "GitHub"). Keys are lower
 * case.
 */
const SPELLED: ReadonlyMap<string, string> = new Map(
  [
    ...["AI", "API", "AWS", "CI", "GCP", "HR", "ID", "LLM", "MCP", "PR", "QA", "SQL", "SRE"],
    ...["UI", "UX", "BigQuery", "ChatGPT", "DevOps", "GitHub", "GitLab", "LinkedIn", "OAuth"],
    ...["OpenAI", "PagerDuty", "PostgreSQL", "Slack", "YouTube"],
  ].map((word) => [word.toLowerCase(), word]),
);

/** A word's own spelling, whatever its case, or nothing for an ordinary word. */
export function spelled(word: string): string | undefined {
  return SPELLED.get(word.toLowerCase());
}
