import { RendererProvider } from "@agent-ui/primitives";
import type { ReactNode } from "react";
import { type Placement, type SerializeResult, serialize } from "./blocks";
import { slackImpls } from "./impls";
import { renderToHostTree } from "./reconciler";

export { type Block, LIMITS, type Placement, type SerializeResult } from "./blocks";
export * from "./lifecycle";
export { plainDate, toMrkdwn, toPlain } from "./mrkdwn";
export * from "./stream";

export interface RenderOptions {
  /** Where the blocks will be posted. Changes which blocks are legal. Default "message". */
  placement?: Placement;
}

/**
 * Render agent-ui components to Block Kit. Synchronous and side-effect free:
 * no hooks with effects, no async. Post the result with chat.postMessage,
 * chat.update, chat.stopStream (blocks), views.open, or views.publish.
 */
export function renderToSlack(element: ReactNode, options: RenderOptions = {}): SerializeResult {
  const placement = options.placement ?? "message";
  const tree = renderToHostTree(
    <RendererProvider renderer={{ surface: "slack", placement, impls: slackImpls }}>
      {element}
    </RendererProvider>,
  );
  return serialize(tree, placement);
}

/** Slack sends action_id back as "<base>#<index>"; recover the shared agent-ui ActionId. */
export function actionIdBase(actionId: string): string {
  const i = actionId.lastIndexOf("#");
  return i === -1 ? actionId : actionId.slice(0, i);
}

/** Open the payload in Slack's Block Kit Builder. */
export function blockKitBuilderUrl(blocks: unknown[], placement: Placement = "message"): string {
  const mode = placement === "message" ? "" : `"type":"${placement}",`;
  const json = `{${mode}"blocks":${JSON.stringify(blocks)}}`;
  return `https://app.slack.com/block-kit-builder/#${encodeURIComponent(json)}`;
}
