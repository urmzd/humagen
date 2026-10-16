import type { ActionHandler } from "@vultus/core";
import { RendererProvider } from "@vultus/primitives";
import { type ReactNode, useState } from "react";
import { ActionContext } from "./buttons";
import { CitationContext, CitationRegistry } from "./citations";
import { FormContext, FormStore } from "./form";
import { webImpls } from "./impls";

export { Amount } from "./amount";
export { ActionContext, buttonClass, DenseContext, Menu } from "./buttons";
export { CitationChip, CitationRegistry, Favicon, hostOf, SourceMark } from "./citations";
export { CodeBlock, CopyButton, Terminal } from "./code";
export { Feedback, FeedbackPromptContext, Rating } from "./feedback";
export { ArtifactIcon, FileTile, phaseLabel } from "./file";
export { Checkboxes, FormContext, FormStore, useGroup } from "./form";
export * from "./icons";
export { localizeTimes, webImpls } from "./impls";
export {
  ArtifactOpenContext,
  type ItemActions,
  ItemActionsContext,
  NowContext,
  SuppressContext,
  type TurnItem,
  TurnMetaContext,
  timeLeft,
  useNow,
  useTurnMeta,
} from "./meta";
export {
  Collapse,
  Disclosure,
  Shimmer,
  ShimmerVerb,
  THINKING_VERBS,
  useDisclosure,
  useVerbSequence,
} from "./motion";
export { RequestBubble, readableMentions } from "./request";
export * from "./Tooltip";
export { StatusIcon, statusLabel } from "./task";

/**
 * Render vultus components to the DOM. Clicks surface as the same
 * `UiAction` a Slack interaction decodes to, so one handler serves both.
 *
 * Holds one form store, so a Button's `collects` reads the Checkboxes group
 * rendered in the same surface.
 *
 * Children are laid out as one `.aui-stack`: shared components emit flat runs
 * of primitives, and the stack's sibling rules group them into cards.
 */
export function WebSurface({
  onAction = () => {},
  children,
  className,
}: {
  onAction?: ActionHandler;
  children: ReactNode;
  className?: string;
}) {
  const [citations] = useState(() => new CitationRegistry());
  const [form] = useState(() => new FormStore());
  return (
    <ActionContext.Provider value={onAction}>
      <CitationContext.Provider value={citations}>
        <FormContext.Provider value={form}>
          <RendererProvider renderer={{ surface: "web", impls: webImpls }}>
            <div className={`aui-stack font-aui-sans text-aui-fg ${className ?? ""}`}>
              {children}
            </div>
          </RendererProvider>
        </FormContext.Provider>
      </CitationContext.Provider>
    </ActionContext.Provider>
  );
}
