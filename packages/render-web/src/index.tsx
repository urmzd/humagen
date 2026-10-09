import type { ActionHandler } from "@agent-ui/core";
import { RendererProvider } from "@agent-ui/primitives";
import { type ReactNode, useState } from "react";
import { ActionContext } from "./buttons";
import { CitationContext, CitationRegistry } from "./citations";
import { webImpls } from "./impls";

export { ActionContext, buttonClass } from "./buttons";
export { CitationChip, CitationRegistry, hostOf, SourceMark } from "./citations";
export { CodeBlock, CopyButton, Terminal } from "./code";
export * from "./icons";
export { localizeTimes, SuppressContext, webImpls } from "./impls";
export {
  Collapse,
  Disclosure,
  Shimmer,
  ShimmerVerb,
  THINKING_VERBS,
  useDisclosure,
  useVerbSequence,
} from "./motion";
export { StatusIcon, statusLabel } from "./task";

/**
 * Render agent-ui components to the DOM. Clicks surface as the same
 * `UiAction` a Slack interaction decodes to, so one handler serves both.
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
  return (
    <ActionContext.Provider value={onAction}>
      <CitationContext.Provider value={citations}>
        <RendererProvider renderer={{ surface: "web", impls: webImpls }}>
          <div className={`aui-stack font-aui-sans text-aui-fg ${className ?? ""}`}>{children}</div>
        </RendererProvider>
      </CitationContext.Provider>
    </ActionContext.Provider>
  );
}
