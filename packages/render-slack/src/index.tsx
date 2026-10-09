import { decodeAction, type UiAction } from "@agent-ui/core";
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

/**
 * Decode any agent-ui interaction from a Slack block_actions payload: buttons
 * carry the payload in `value`; overflow menus in `selected_option.value`;
 * checkbox groups contribute `selection` from state.values.
 */
export function decodeSlackAction(
  action: { action_id: string; value?: string; selected_option?: { value: string } },
  state?: { values?: Record<string, Record<string, { selected_options?: { value: string }[] }>> },
): UiAction | null {
  let ui: UiAction | null = null;
  if (action.selected_option) {
    const { id, p } = JSON.parse(action.selected_option.value) as {
      id: UiAction["id"];
      p: UiAction["payload"];
    };
    ui = { id, payload: p };
  } else if (action.value) {
    ui = {
      id: actionIdBase(action.action_id) as UiAction["id"],
      payload: decodeAction(action.value),
    };
  }
  if (!ui) return null;
  // Batch buttons collect the checkbox group named in their target.
  const group = state?.values?.[`checkboxes:${ui.payload.target}`];
  const checked = group && Object.values(group)[0]?.selected_options;
  if (checked) ui.payload.selection = checked.map((o) => o.value);
  return ui;
}

/** Open the payload in Slack's Block Kit Builder. */
export function blockKitBuilderUrl(blocks: unknown[], placement: Placement = "message"): string {
  const mode = placement === "message" ? "" : `"type":"${placement}",`;
  const json = `{${mode}"blocks":${JSON.stringify(blocks)}}`;
  return `https://app.slack.com/block-kit-builder/#${encodeURIComponent(json)}`;
}
