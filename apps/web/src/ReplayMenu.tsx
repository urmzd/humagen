import { defaultCopy } from "@humagen/components";
import { PlayIcon } from "@humagen/render-web";
import { Modal, messageActionButton } from "@humagen/web";
import { useState } from "react";
import { replayText, replayWith } from "./replay.ts";
import { AgentMark, Popover } from "./ui.tsx";

/**
 * Replay With on a finished turn: pick an agent, and the same prompt goes to
 * it in a fork of the conversation at that turn (DES-058).
 */
export function ReplayMenu({
  agents,
  answered,
  prompt,
  thread,
  point,
  onOpen,
}: {
  /** The agents a message here may name. */
  agents: string[];
  /** Who answered the turn: listed last, since replaying with it is a retry. */
  answered: string;
  prompt: string;
  thread: string;
  /** The finished turn saved as the branch point. */
  point: string;
  onOpen: (thread: string) => void;
}) {
  const words = defaultCopy.forking;
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const [selected, setSelected] = useState<string>();
  const order = [...agents.filter((a) => a !== answered), ...agents.filter((a) => a === answered)];
  if (order.length === 0 || !prompt.trim()) return null;
  const run = (agent: string) => {
    if (busy) return;
    setSelected(undefined);
    setBusy(true);
    setProblem("");
    replayWith({ thread, point, text: replayText(agent, prompt) })
      .then(onOpen, () => setProblem(words.replayFailed))
      .finally(() => setBusy(false));
  };
  return (
    <div className="inline-flex items-center">
      <Popover
        narrow
        up
        label={words.replay}
        tooltip={words.replay}
        className={messageActionButton}
        button={<PlayIcon size={16} className={busy ? "opacity-50" : ""} />}
      >
        {(close) => (
          <div
            role="menu"
            aria-label={words.replay}
            className="flex max-h-72 flex-col overflow-y-auto"
          >
            <p className="px-2.5 pt-1.5 pb-1 text-aui-xs text-aui-muted">{words.replayHint}</p>
            {order.map((agent) => (
              <button
                key={agent}
                type="button"
                role="menuitem"
                disabled={busy}
                onClick={() => {
                  close();
                  setSelected(agent);
                }}
                className="flex h-11 shrink-0 items-center gap-2 rounded-aui px-2.5 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent disabled:opacity-50"
              >
                <AgentMark name={agent} size={18} />
                <span className="min-w-0 flex-1 truncate">{words.replayWith(agent)}</span>
              </button>
            ))}
          </div>
        )}
      </Popover>
      {selected && (
        <Modal title={`Replay with @${selected}`} onClose={() => setSelected(undefined)}>
          <div className="flex flex-col gap-4 p-5">
            <h2 className="font-medium">Replay with @{selected}</h2>
            <p className="text-aui-sm">
              This starts a new session with the selected agent and the original session as context.
            </p>
            <blockquote className="whitespace-pre-wrap rounded-aui bg-aui-hover p-3 text-aui-sm">
              {replayText(selected, prompt)}
            </blockquote>
            <div className="flex justify-end gap-2">
              <button
                type="button"
                className="rounded-aui px-3 py-2"
                onClick={() => setSelected(undefined)}
              >
                Cancel
              </button>
              <button
                type="button"
                className="rounded-aui bg-aui-primary px-3 py-2 text-aui-primary-fg"
                onClick={() => run(selected)}
              >
                Start replay
              </button>
            </div>
          </div>
        </Modal>
      )}
      {problem && (
        <span role="alert" className="ml-1 text-aui-2xs text-aui-bad">
          {problem}
        </span>
      )}
    </div>
  );
}
