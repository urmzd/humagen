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
import { slackOverrides, TEXT_BLOCK } from "./native";
import { renderToHostTree } from "./reconciler";

export {
  type Block,
  fallbackOf,
  LIMITS,
  type Placement,
  type SerializeOptions,
  type SerializeResult,
} from "./blocks";
export * from "./connections";
export * from "./feedback";
export * from "./forms";
export * from "./lifecycle";
export {
  escapeAnnouncement,
  escapeSlackText,
  inertName,
  plainDate,
  toMrkdwn,
  toPlain,
} from "./mrkdwn";
export * from "./native";
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
    <RendererProvider
      renderer={{ surface: "slack", placement, impls: slackImpls, overrides: slackOverrides }}
    >
      {element}
    </RendererProvider>,
  );
  return serialize(tree, placement, {
    amountForm: options.amountForm,
    ...(options.native === false ? { native: false } : {}),
  });
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
  action: {
    action_id: string;
    value?: string;
    selected_option?: { value: string };
    /** Sent by Slack; an input's words come as `value`, its action in `block_id`. */
    type?: string;
    block_id?: string;
  },
  state?: { values?: Record<string, Record<string, { selected_options?: { value: string }[] }>> },
): UiAction | null {
  let ui: UiAction | null = null;
  if (action.type === "plain_text_input") {
    // An input that sends on Enter (questionContainer): the words are the value, the action is kept in the block.
    const text = action.value?.trim();
    if (!text || !action.block_id?.startsWith(TEXT_BLOCK)) return null;
    return {
      id: actionIdBase(action.action_id) as UiAction["id"],
      payload: { ...decodeAction(action.block_id.slice(TEXT_BLOCK.length)), text },
    };
  }
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
