import { Mascot, type MascotPersona, mascotPersonas, type OrbState } from "@humagen/web";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@humagen/render-web/fonts.css";
import "./styles.css";

/**
 * Humagen's mascots: each persona in each pose, moving as it would at work.
 * Open /mascot-gallery.html on the dev server; add `?theme=light` for the
 * light theme. It calls nothing on the platform and is not part of the built
 * console.
 */
const states: [OrbState, string][] = [
  ["idle", "Idle"],
  ["searching", "Investigating"],
  ["thinking", "Thinking"],
  ["calling", "Using a tool"],
  ["composing", "Writing"],
  ["delegating", "Handing off"],
  ["waiting", "Waiting for you"],
];
const looks: Record<MascotPersona, [string, string]> = {
  detective: ["Researcher", "oklch(0.62 0.14 40)"],
  writer: ["Writer", "oklch(0.7 0.13 190)"],
  engineer: ["Engineer", "oklch(0.68 0.15 135)"],
  scientist: ["Analyst", "oklch(0.66 0.15 300)"],
  inspector: ["QA Reviewer", "oklch(0.66 0.14 250)"],
  conductor: ["Humagen", "oklch(0.64 0.19 25)"],
  plain: ["Helper", "oklch(0.72 0.13 85)"],
};
if (new URLSearchParams(location.search).get("theme") === "light")
  document.documentElement.dataset.theme = "light";

function Gallery() {
  return (
    <main className="min-h-dvh bg-aui-bg p-8 text-aui-fg">
      <h1 className="mb-6 font-medium text-aui-lg">Humagen mascots</h1>
      <div
        className="grid items-end gap-x-4 gap-y-8"
        style={{ gridTemplateColumns: `8rem repeat(${states.length}, 9rem)` }}
      >
        <span />
        {states.map(([s, label]) => (
          <span key={s} className="text-aui-muted text-aui-xs">
            {label}
          </span>
        ))}
        {mascotPersonas.map((persona) => (
          <div key={persona} className="contents">
            <span className="self-center text-aui-sm">{looks[persona][0]}</span>
            {states.map(([state]) => (
              <Mascot
                key={state}
                persona={persona}
                state={state}
                size={110}
                color={looks[persona][1]}
              />
            ))}
          </div>
        ))}
      </div>
      <h2 className="mt-10 mb-4 text-aui-muted text-aui-sm">In a thread, at 32px</h2>
      <div className="flex items-center gap-6">
        {mascotPersonas.map((persona, i) => (
          <span key={persona} className="inline-flex items-center gap-2 text-aui-sm">
            <Mascot
              persona={persona}
              state={states[(i % 6) + 1]?.[0] ?? "idle"}
              size={32}
              color={looks[persona][1]}
            />
            {looks[persona][0]}
          </span>
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
