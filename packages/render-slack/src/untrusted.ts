/**
 * Text no person wrote, such as an agent's announcement or recap, made safe to
 * post: every way Slack pings people is turned into plain words. `<!here>`,
 * `<!channel>` and `<!everyone>` become `@here`, `@channel` and `@everyone`,
 * a group mention `<!subteam^S123|@oncall>` becomes `@oncall`, and a user
 * mention `<@U123|dana>` becomes `@dana` (or `@U123`). The result holds no
 * mention token at all, so it is safe in mrkdwn, plain_text and a `markdown`
 * block alike. Channel links and dates ping nobody and are kept.
 */
export function escapeMentions(text: string): string {
  return text
    .replace(/<!(here|channel|everyone)(?:\|[^<>]*)?>/g, (_, which: string) => `@${which}`)
    .replace(/<!subteam\^([A-Za-z0-9]+)(?:\|([^<>]*))?>/g, (_, id: string, label?: string) =>
      label ? `@${label.replace(/^@/, "")}` : `@${id}`,
    )
    .replace(/<@([UW][A-Za-z0-9]+)(?:\|([^<>]*))?>/g, (_, id: string, label?: string) =>
      label ? `@${label.replace(/^@/, "")}` : `@${id}`,
    );
}
