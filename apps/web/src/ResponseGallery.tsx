import { answerOf, foldAll } from "@humagen/core";
import { responseStreams } from "@humagen/core/fixtures/responses";
import { useState } from "react";
import { ActivityLanes, costOf, WorkedButton } from "./ResponseActivity.tsx";
import { SessionPanel } from "./SessionPanel.tsx";
import { summarize } from "./sessionSummary.ts";
import { referenceOf } from "./trace.ts";
import { AgentMark } from "./ui.tsx";

const streams = [
  ["One agent", responseStreams.singleAgent],
  ["Two agents in parallel", responseStreams.parallelAgents],
  ["Two deep, waiting on an approval", responseStreams.nestedApproval],
  ["Stopped", responseStreams.stopped],
] as const;

/**
 * Each recorded stream as it reads in a session: the answer, Worked …, and,
 * once opened, its lanes as the Details panel shows them.
 */
export function ResponseGallery() {
  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-2">
      {streams.map(([name, events]) => (
        <Example key={name} name={name} events={events} />
      ))}
    </div>
  );
}

function Example({ name, events }: { name: string; events: (typeof streams)[number][1] }) {
  const state = foldAll(events);
  const [open, setOpen] = useState(name !== "One agent");
  const answer = answerOf(state);
  return (
    <article
      aria-label={name}
      data-testid="gallery-response"
      className="flex min-w-0 flex-col gap-3 rounded-aui-lg border border-[var(--humagen-rule)] p-4"
    >
      <p className="text-aui-muted text-aui-xs">{name}</p>
      <p className="flex items-center gap-2 font-medium text-aui-fg text-aui-sm">
        <AgentMark name={state.agent ?? ""} size={20} />
        {state.agent}
      </p>
      {answer.text && <p className="whitespace-pre-wrap text-aui-fg text-aui-sm">{answer.text}</p>}
      <WorkedButton state={state} open={open} onToggle={() => setOpen((v) => !v)} />
      {open && (
        <div className="flex flex-col gap-3 border-[var(--humagen-rule)] border-t pt-3">
          <p className="text-aui-muted text-aui-xs">{costOf(state)}</p>
          <ActivityLanes state={state} messageId={state.id} />
        </div>
      )}
    </article>
  );
}

const files = [
  {
    id: "a1",
    title: "Austin apartment options.xlsx",
    kind: "spreadsheet",
    mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  },
  { id: "a2", title: "Lease summary.pdf", kind: "pdf", mime: "application/pdf" },
];

const panels = [
  ["Running", [foldAll(responseStreams.parallelAgents.slice(0, 12))]],
  ["Waiting for you", [foldAll(responseStreams.nestedApproval)]],
  [
    "Done",
    [
      foldAll(responseStreams.parallelAgents),
      foldAll(responseStreams.singleAgent),
      foldAll(responseStreams.usedTools),
    ],
  ],
] as const;

/** The session's Details panel while a response runs, while it waits on a person, and once done. */
export function SessionPanelGallery() {
  return (
    <div className="grid grid-cols-1 gap-6 lg:grid-cols-3">
      {panels.map(([name, responses]) => (
        <div
          key={name}
          aria-label={name}
          data-testid="gallery-session-panel"
          className="relative flex h-[44rem] overflow-hidden rounded-aui-lg border border-[var(--humagen-rule)] [transform:translateZ(0)] [&>aside]:w-full"
        >
          <SessionPanel
            responses={[...responses]}
            summary={summarize([...responses], {
              files: name === "Done" ? files : [],
              uploads: name === "Done" ? ["leases.pdf", "budget.csv", "notes.txt"] : [],
            })}
            origin={referenceOf("slack://T1/C1/1700000000.000100")}
            onClose={() => {}}
            onOpenFile={() => {}}
          />
        </div>
      ))}
    </div>
  );
}
