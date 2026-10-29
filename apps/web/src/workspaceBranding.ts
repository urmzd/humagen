import { useEffect, useState } from "react";
import { api, type Branding } from "./api.ts";
import { servedBranding } from "./branding.ts";

const STYLE_ID = "humagen-workspace-branding";

/**
 * The workspace's branding, null until it is read. Its stylesheet is linked
 * after the deployment's, which the server already wrote into the page, so a
 * color the workspace sets wins.
 */
export function useBranding(): Branding | null {
  const [branding, setBranding] = useState<Branding | null>(null);
  useEffect(() => {
    let live = true;
    api
      .branding()
      .then((b) => live && setBranding(b))
      .catch(() => live && setBranding(servedBranding()));
    return () => {
      live = false;
    };
  }, []);
  useEffect(() => {
    const was = document.getElementById(STYLE_ID);
    if (!branding?.style) {
      was?.remove();
      return;
    }
    if (was instanceof HTMLLinkElement && was.getAttribute("href") === branding.style) return;
    was?.remove();
    const link = document.createElement("link");
    link.id = STYLE_ID;
    link.rel = "stylesheet";
    link.href = branding.style;
    document.head.appendChild(link);
  }, [branding?.style]);
  return branding;
}
