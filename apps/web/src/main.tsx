import { defaultCopy } from "@humagen/components/copy";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import type { Branding } from "./api.ts";
import { brandCopy } from "./branding.ts";
import "@humagen/render-web/fonts.css";
import "./styles.css";
import { trackAppHeight } from "./viewport.ts";

/**
 * The workspace's branding, read before the console's modules load, so its
 * words are in place for text a module reads once as it loads, as well as
 * for every page. Signed out, or unreachable, it is null and App reads it
 * after sign-in.
 */
async function preload(): Promise<Branding | null> {
  try {
    const res = await fetch("/api/branding", { credentials: "same-origin" });
    return res.ok ? ((await res.json()) as Branding) : null;
  } catch {
    return null;
  }
}

async function start() {
  trackAppHeight();
  const root = document.getElementById("root");
  if (!root) throw new Error("missing #root");
  const branding = await preload();
  if (branding) brandCopy(defaultCopy, branding);
  const { App } = await import("./App.tsx");
  createRoot(root).render(
    <StrictMode>
      <App preloaded={branding} />
    </StrictMode>,
  );
}

void start();
