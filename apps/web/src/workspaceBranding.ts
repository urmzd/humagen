import { useEffect } from "react";

const STYLE_ID = "humagen-workspace-branding";

/**
 * Link the workspace's stylesheet after the deployment's, which the server
 * already wrote into the page, so a color the workspace sets wins.
 */
export function useBrandingStyle(style: string | null | undefined): void {
  useEffect(() => {
    const was = document.getElementById(STYLE_ID);
    if (!style) {
      was?.remove();
      return;
    }
    if (was instanceof HTMLLinkElement && was.getAttribute("href") === style) return;
    was?.remove();
    const link = document.createElement("link");
    link.id = STYLE_ID;
    link.rel = "stylesheet";
    link.href = style;
    document.head.appendChild(link);
  }, [style]);
}
