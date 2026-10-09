import { defaultCopy } from "@agent-ui/components";
import type { Turn, TurnStatus } from "@agent-ui/core";
import * as fx from "@agent-ui/core/fixtures";
import { lifecycle } from "@agent-ui/render-slack";
import type { Meta, StoryObj } from "@storybook/react-vite";

const meta = {
  title: "Slack/Lifecycle signals",
  parameters: {
    docs: {
      description: {
        component:
          "Status that lives outside the message. Slack: one reaction on the user's message (each emoji has one meaning) plus the agent session status. Web: a status line on the turn.",
      },
    },
  },
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

const EMOJI: Record<string, string> = {
  eyes: "👀",
  raised_hand: "✋",
  white_check_mark: "✅",
  x: "❌",
  black_square_for_stop: "⏹️",
};

const rows: { status: TurnStatus; turn: Turn; when: string }[] = [
  {
    status: "queued",
    turn: { ...fx.streamingTurn, status: "queued", tasks: [] },
    when: "Message received, before any work",
  },
  { status: "working", turn: fx.streamingTurn, when: "Tools running or tokens streaming" },
  { status: "waiting", turn: fx.waitingTurn, when: "An approval or question needs a person" },
  { status: "done", turn: fx.doneTurn, when: "Answer delivered" },
  { status: "failed", turn: fx.failedTurn, when: "Run ended on a failure alert" },
  { status: "stopped", turn: fx.stoppedTurn, when: "User pressed Stop" },
];

export const Signals: Story = {
  render: () => (
    <div className="overflow-x-auto">
      <table className="w-full min-w-[720px] text-left text-sm">
        <thead className="text-xs uppercase tracking-wide text-aui-muted">
          <tr className="border-b border-aui-border">
            <th className="py-2 pr-4">Turn status</th>
            <th className="py-2 pr-4">When</th>
            <th className="py-2 pr-4">Slack reaction</th>
            <th className="py-2 pr-4">Slack session</th>
            <th className="py-2 pr-4">Web status line</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(({ status, turn, when }) => {
            const s = lifecycle(turn);
            return (
              <tr key={status} className="border-b border-aui-border last:border-0">
                <td className="py-2 pr-4 font-mono">{status}</td>
                <td className="py-2 pr-4 text-aui-muted">{when}</td>
                <td className="py-2 pr-4">
                  {s.reaction ? (
                    <>
                      <span className="mr-2 text-lg">{EMOJI[s.reaction]}</span>
                      <code className="text-xs text-aui-muted">:{s.reaction}:</code>
                    </>
                  ) : (
                    "none"
                  )}
                </td>
                <td className="py-2 pr-4">
                  <code className="rounded bg-aui-code px-1.5 py-0.5 text-xs">
                    {s.sessionStatus}
                  </code>
                  {s.statusText && <span className="ml-2 text-aui-muted">“{s.statusText}”</span>}
                </td>
                <td className="py-2 pr-4">{defaultCopy.status[status]}</td>
              </tr>
            );
          })}
        </tbody>
      </table>
      <p className="mt-4 max-w-2xl text-sm text-aui-muted">
        👀 goes on as soon as the message arrives and comes off when the reply lands. Only one
        reaction shows at a time; <code>reactionChanges(prev, next)</code> returns the add/remove
        pair. Session status is never left on <code>processing</code> after a run ends, even on
        failure.
      </p>
    </div>
  ),
};
