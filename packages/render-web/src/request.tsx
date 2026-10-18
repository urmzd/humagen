import type { PrimitiveProps } from "@warp/primitives";

/**
 * Slack's mention syntax as a reader sees it: `<@U123|dana>` → "@dana",
 * `<@U123>` → "@U123", `<#C123|general>` → "#general". Anything else stays
 * as typed, and React escapes it.
 */
export function readableMentions(text: string): string {
  return text
    .replace(
      /<@([UW][A-Z0-9]+)(?:\|([^<>]*))?>/g,
      (_m, id: string, name?: string) => `@${name || id}`,
    )
    .replace(
      /<#(C[A-Z0-9]+)(?:\|([^<>]*))?>/g,
      (_m, id: string, name?: string) => `#${name || id}`,
    );
}

/**
 * What a person asked: who asked, then their words in a right-aligned bubble,
 * as Slack shows "Dana: …". It needs no surface around it, so a chat can draw
 * it directly; the `Request` primitive draws the same. A chat's own speaker
 * can leave the name out (`showBy={false}`), as `UserMessage` does for "You".
 */
export function RequestBubble({
  by,
  text,
  showBy = true,
}: PrimitiveProps["Request"] & { showBy?: boolean }) {
  const who = readableMentions(by);
  return (
    <article aria-label={`${who} message`} className="aui-fade-up flex flex-col items-end gap-1">
      {showBy && <span className="font-medium text-aui-muted text-aui-xs">{who}</span>}
      <p className="max-w-[min(80%,36rem)] whitespace-pre-wrap break-words rounded-aui-lg rounded-br-aui-sm bg-aui-subtle px-3.5 py-2 text-aui-base text-aui-fg">
        {readableMentions(text)}
      </p>
    </article>
  );
}
