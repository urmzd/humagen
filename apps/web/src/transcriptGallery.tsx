import { WebSurface } from "@humagen/render-web";
import { AgentMessage, UserMessage } from "@humagen/web";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import "@humagen/render-web/fonts.css";
import "./styles.css";
import { type TranscriptScene, transcriptScenes } from "./transcript.fixtures.ts";
import { AgentTint, setAgentHues } from "./ui.tsx";

/**
 * A conversation transcript, one scene at a time, drawn from fixtures alone:
 * `?scene=failed|connect|refs|tools|running` and `?theme=light|dark`. Open
 * /transcript-gallery.html on the dev server. It calls nothing on the
 * platform and is not part of the built console; the screen captures of
 * this change are taken from it.
 */
setAgentHues({ humagen: 12, "github-reviewer": 48 });

const params = new URLSearchParams(window.location.search);
const theme = params.get("theme") === "light" ? "light" : "dark";
document.documentElement.dataset.theme = theme;
const scene = (params.get("scene") ?? "failed") as TranscriptScene;
const shown = transcriptScenes[scene] ?? transcriptScenes.failed;

function Transcript() {
  const turn = shown.turn();
  return (
    <main className="h-full overflow-y-auto bg-aui-bg">
      <div className="mx-auto flex w-full max-w-3xl flex-col gap-6 px-4 py-6">
        <div className="self-end">
          <UserMessage text={shown.ask} />
        </div>
        <WebSurface>
          <AgentTint name={turn.agent.name}>
            <AgentMessage turn={turn} disclaimer={false} activity={false} />
          </AgentTint>
        </WebSurface>
      </div>
    </main>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <Transcript />
    </StrictMode>,
  );
