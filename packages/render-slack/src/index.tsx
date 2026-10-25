import { decodeAction, type UiAction } from "@humagen/core";
import { RendererProvider } from "@humagen/primitives";
import type { ReactNode } from "react";
import {
  type Block,
  type Placement,
  type SerializeOptions,
  type SerializeResult,
  serialize,
} from "./blocks";
import { slackImpls } from "./impls";
import { renderToHostTree } from "./reconciler";

export {
  type Block,
  LIMITS,
  type Placement,
  type SerializeOptions,
  type SerializeResult,
} from "./blocks";
export * from "./connections";
export * from "./feedback";
export * from "./forms";
export * from "./lifecycle";
export { escapeAnnouncement, escapeSlackText, plainDate, toMrkdwn, toPlain } from "./mrkdwn";
export * from "./reply";
export { breakMarkdown, nextBreak } from "./split";
export * from "./stream";
export { escapeMentions } from "./untrusted";

export interface RenderOptions extends SerializeOptions {
  /** Where the blocks will be posted. Changes which blocks are legal. Default "message". */
  placement?: Placement;
}

/**
 * Render humagen components to Block Kit. Synchronous and side-effect free:
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
  return serialize(tree, placement, { amountForm: options.amountForm });
}

/** A Home tab view, ready for `views.publish` as `{ user_id, view }`. */
export interface HomeView {
  view: { type: "home"; blocks: Block[]; private_metadata?: string; callback_id?: string };
  /** Limits hit and degradations applied. Empty when the output is lossless. */
  warnings: string[];
}

/**
 * Render a layout (usually `<Home home={model} />`) as an App Home tab view.
 * Home allows 100 blocks and has one page: anything past the limit is
 * replaced by a note and reported in `warnings`.
 */
export function homeView(
  element: ReactNode,
  options: { privateMetadata?: string; callbackId?: string } & SerializeOptions = {},
): HomeView {
  const { blocks, warnings } = renderToSlack(element, {
    placement: "home",
    amountForm: options.amountForm,
  });
  if (options.privateMetadata && options.privateMetadata.length > 3000) {
    throw new RangeError("private_metadata is limited to 3000 characters");
  }
  return {
    view: {
      type: "home",
      blocks,
      ...(options.privateMetadata ? { private_metadata: options.privateMetadata } : {}),
      ...(options.callbackId ? { callback_id: options.callbackId } : {}),
    },
    warnings,
  };
}

/** Slack sends action_id back as "<base>#<index>"; recover the shared humagen ActionId. */
export function actionIdBase(actionId: string): string {
  const i = actionId.lastIndexOf("#");
  return i === -1 ? actionId : actionId.slice(0, i);
}

/**
 * Decode any humagen interaction from a Slack block_actions payload: buttons
 * carry the payload in `value`; overflow and select menus in `selected_option.value`;
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
