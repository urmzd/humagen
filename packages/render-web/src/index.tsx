import type { ActionHandler } from "@agent-ui/core";
import { RendererProvider } from "@agent-ui/primitives";
import type { ReactNode } from "react";
import { ActionContext, webImpls } from "./impls";

export { webImpls } from "./impls";

/**
 * Render agent-ui components to the DOM. Clicks surface as the same
 * `UiAction` a Slack interaction decodes to, so one handler serves both.
 */
export function WebSurface({
  onAction = () => {},
  children,
}: {
  onAction?: ActionHandler;
  children: ReactNode;
}) {
  return (
    <ActionContext.Provider value={onAction}>
      <RendererProvider renderer={{ surface: "web", impls: webImpls }}>
        <div className="flex flex-col gap-3 text-aui-fg">{children}</div>
      </RendererProvider>
    </ActionContext.Provider>
  );
}
