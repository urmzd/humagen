// The console's own pieces, rendered to static markup at build time: its
// icons (lucide geometry, 24px grid, 2px stroke), the Humagen mark, agent
// initials and button classes. Nothing here ships JavaScript to the page.
import {
  BotIcon,
  ChevronRightIcon,
  DatabaseIcon,
  ExternalLinkIcon,
  HistoryIcon,
  LayersIcon,
  MessageIcon,
  ScrollTextIcon,
  ServerIcon,
  ShieldIcon,
  UsersIcon,
  WrenchIcon,
} from "@humagen/render-web/icons";
import { cn } from "@humagen/render-web/lib/utils";
import { buttonVariants } from "@humagen/render-web/ui/button";
import { agentInitials } from "@humagen/web/AgentOrb";
import { HumagenMark } from "@humagen/web/HumagenMark";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const icons = {
  bot: BotIcon,
  chevron: ChevronRightIcon,
  database: DatabaseIcon,
  external: ExternalLinkIcon,
  history: HistoryIcon,
  layers: LayersIcon,
  message: MessageIcon,
  scroll: ScrollTextIcon,
  server: ServerIcon,
  shield: ShieldIcon,
  users: UsersIcon,
  wrench: WrenchIcon,
};

export type IconName = keyof typeof icons;

export function icon(name: IconName, size = 16, className = ""): string {
  return renderToStaticMarkup(createElement(icons[name], { size, className }));
}

export function mark(size = 23): string {
  return renderToStaticMarkup(createElement(HumagenMark, { size }));
}

export { agentInitials };

/** The console's primary action, in the accent (red by default). */
export const primaryButton = cn(
  buttonVariants({ size: "lg" }),
  "site-btn bg-aui-accent text-aui-accent-fg hover:bg-aui-accent/90",
);

/** The console's everyday button (`button` in apps/web/src/ui.tsx). */
export const outlineButton = cn(buttonVariants({ variant: "outline", size: "lg" }), "site-btn");
