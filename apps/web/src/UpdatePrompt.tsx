import { defaultCopy } from "@humagen/components/copy";
import { useSyncExternalStore } from "react";
import type { Updates } from "./pwa.ts";
import { button, primary } from "./ui.tsx";

/**
 * "A new version is ready", with Reload and Later. It waits on a person, so
 * it never animates and never reloads on its own (pwa.ts). It sits above the
 * home indicator and clear of the composer's send button.
 */
export function UpdatePrompt({ updates }: { updates: Updates }) {
  const { waiting } = useSyncExternalStore(updates.subscribe, updates.get, updates.get);
  if (!waiting) return null;
  const copy = defaultCopy.app;
  return (
    <section
      aria-label={copy.updateReady}
      data-layer="update"
      className="humagen-update fixed z-[60] flex max-w-[calc(100vw-2rem)] flex-col gap-2 rounded-aui-lg p-3 text-aui-sm text-aui-fg sm:w-96"
    >
      <p role="status" className="font-medium">
        {copy.updateReady}
      </p>
      <p className="text-aui-xs text-aui-muted">{copy.updateHint}</p>
      <div className="flex justify-end gap-2">
        <button type="button" className={button} onClick={updates.dismiss}>
          {copy.later}
        </button>
        <button type="button" className={primary} onClick={updates.accept}>
          {copy.reload}
        </button>
      </div>
    </section>
  );
}
