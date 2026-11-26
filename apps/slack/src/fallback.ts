import { fallbackOf } from "@humagen/render-slack";
import { type SlackApi, SlackError } from "./slack.ts";

/** Slack refused the blocks themselves, not the call. */
const INVALID = /^invalid_blocks(_format)?$/;

const invalid = (e: unknown) => e instanceof SlackError && INVALID.test(e.code);

/**
 * Slack, with a call whose blocks Slack refuses as `invalid_blocks` sent
 * again when those blocks hold Slack's own card, carousel or container
 * (render-slack `native.tsx`): first with the same page drawn as sections
 * (`fallbackOf`), then, when there is no such page or Slack refuses it too,
 * with the message's text alone. A call whose blocks hold none fails as
 * before, since sending it again would fail the same way.
 */
export function withBlockFallback(
  slack: SlackApi,
  log: (line: string) => void = () => {},
): SlackApi {
  return {
    respond: (url, body) => slack.respond(url, body),
    async call<T>(method: string, args: Record<string, unknown> = {}): Promise<T> {
      try {
        return await slack.call<T>(method, args);
      } catch (e) {
        const { blocks, ...rest } = args;
        const sections = Array.isArray(blocks) && invalid(e) ? fallbackOf(blocks) : undefined;
        if (sections === undefined) throw e;
        log(`${method}: Slack refused its native blocks (${(e as SlackError).code})`);
        if (sections) {
          try {
            return await slack.call<T>(method, { ...rest, blocks: sections });
          } catch (again) {
            if (!invalid(again)) throw again;
            log(`${method}: Slack refused the sections too (${(again as SlackError).code})`);
          }
        }
        log(`${method}: sending the text alone`);
        return slack.call<T>(method, rest);
      }
    },
  };
}
