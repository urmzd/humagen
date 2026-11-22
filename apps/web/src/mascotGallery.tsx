import type { OrbState } from "@humagen/web";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@humagen/render-web/fonts.css";
import "./styles.css";
import { AgentMark, setAgentHues } from "./ui.tsx";

/**
 * Every working state of an agent's mark, with the prop it holds, at the
 * sizes the console draws. Open /mascot-gallery.html on the dev server; add
 * `?theme=light` for the light theme. It calls nothing on the platform and is
 * not part of the built console.
 */
const states: [OrbState, string][] = [
  ["searching", "Investigating"],
  ["thinking", "Thinking"],
  ["calling", "Using a tool"],
  ["composing", "Writing"],
  ["delegating", "Handing off"],
  ["waiting", "Waiting for you"],
  ["idle", "Idle"],
];
const agents = ["researcher", "writer", "humagen", "qa-reviewer", "planner", "analyst", "lead"];
setAgentHues(Object.fromEntries(agents.map((a, i) => [a, (20 + i * 51) % 360])));
if (new URLSearchParams(location.search).get("theme") === "light")
  document.documentElement.dataset.theme = "light";

function Gallery() {
  return (
    <main className="min-h-dvh bg-aui-bg p-10 text-aui-fg">
      <h1 className="mb-8 font-medium text-aui-lg">Agent marks and their props</h1>
      <div className="grid grid-cols-[10rem_repeat(3,8rem)] items-center gap-y-10">
        <span />
        {[28, 48, 96].map((size) => (
          <span key={size} className="text-aui-muted text-aui-xs">
            {size}px
          </span>
        ))}
        {states.map(([state, label], i) => (
          <div key={state} className="contents">
            <span className="text-aui-sm">{label}</span>
            {[28, 48, 96].map((size) => (
              <span key={size} className="inline-flex">
                <AgentMark name={agents[i] ?? "researcher"} state={state} size={size} />
              </span>
            ))}
          </div>
        ))}
      </div>
    </main>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <Gallery />
    </StrictMode>,
  );
