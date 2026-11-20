import { formatElapsed, formatMicros, type RunDetails } from "@humagen/core";
import { Context, Fields, Summary, Text } from "@humagen/primitives";
import { type Copy, defaultCopy } from "./copy";

/** The rows of a run's details, in reading order. A figure the run did not record has no row. */
export function runDetailRows(
  details: RunDetails,
  copy: Copy = defaultCopy,
): { label: string; value: string }[] {
  const w = copy.runDetails;
  const count = (n: number) => n.toLocaleString("en-US");
  const rows: { label: string; value: string }[] = [];
  if (details.agent) rows.push({ label: w.agent, value: details.agent });
  if (details.model) rows.push({ label: w.model, value: details.model });
  if (details.inputTokens !== undefined)
    rows.push({
      label: w.input,
      value: details.cachedTokens
        ? w.withCached(count(details.inputTokens), count(details.cachedTokens))
        : count(details.inputTokens),
    });
  if (details.outputTokens !== undefined)
    rows.push({ label: w.output, value: count(details.outputTokens) });
  const tokens = details.inputTokens !== undefined || details.outputTokens !== undefined;
  if (details.cost) {
    const amount = formatMicros(details.cost.micros, details.cost.currency);
    rows.push({ label: w.cost, value: details.cost.atLeast ? w.atLeast(amount) : amount });
  } else if (tokens) {
    // Tokens with no price are never shown as a cost of zero.
    rows.push({ label: w.cost, value: w.notPriced });
  }
  if (details.elapsedMs !== undefined)
    rows.push({ label: w.elapsed, value: formatElapsed(details.elapsedMs) });
  return rows;
}

/**
 * What one run used: its agent, model, tokens, cost and elapsed time. A
 * surface shows it only to the person who asked for it (Slack: an ephemeral
 * message), never on the turn, where a whole channel would read it.
 */
export function RunDetailsView({
  details,
  copy = defaultCopy,
}: {
  details: RunDetails;
  copy?: Copy;
}) {
  const w = copy.runDetails;
  const rows = runDetailRows(details, copy);
  const usage = rows.some((r) => r.label !== w.agent && r.label !== w.elapsed);
  return (
    <>
      <Summary text={w.title} />
      <Text text={`**${w.title}**`} />
      {rows.length > 0 && <Fields items={rows} />}
      {!usage && <Context items={[w.none]} />}
      <Context items={[w.onlyYou]} />
    </>
  );
}
