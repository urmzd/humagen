import type { Meta, StoryObj } from "@storybook/react-vite";
import { type InboxMessage, resolveMessage } from "@vultus/core";
import * as fx from "@vultus/core/fixtures";
import { NowContext } from "@vultus/render-web";
import { Mailbox, type PartRenderer } from "@vultus/web";
import { useState } from "react";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, FIXTURE_NOW, slack } from "../../support";

/** A host-defined part: the eval gate draws its own before and after. */
const evalDiff: PartRenderer = ({ message, part, onAction }) => {
  if (part.type !== "custom") return null;
  const data = part.data as { metric: string; before: number; after: number; cases: number };
  return (
    <div className="flex flex-col gap-2 rounded-aui border border-aui-border p-3">
      <p className="text-aui-sm">
        <span className="font-semibold capitalize">{data.metric}</span> fell from{" "}
        <span className="tabular-nums">{data.before}</span> to{" "}
        <span className="font-semibold tabular-nums">{data.after}</span> on {data.cases} cases.
      </p>
      {part.resolution ? (
        <p className="text-aui-muted text-aui-xs">
          {part.resolution.outcome} by {part.resolution.by}
        </p>
      ) : (
        <div className="flex gap-2">
          {["accepted", "blocked"].map((option) => (
            <button
              key={option}
              type="button"
              className="rounded-aui border border-aui-border px-2 py-1 text-aui-sm capitalize hover:bg-aui-hover"
              onClick={() =>
                onAction({
                  id: "aui.inbox.reply",
                  payload: { turnId: message.id, target: part.id, option },
                })
              }
            >
              {option === "accepted" ? "Accept the Regression" : "Block the Release"}
            </button>
          ))}
        </div>
      )}
    </div>
  );
};

function Demo({
  onAction,
  messages: initial = fx.mailMessages,
  width = "max-w-5xl",
  open = null,
}: ActionArgs & { messages?: InboxMessage[]; width?: string; open?: string | null }) {
  const [messages, setMessages] = useState(initial);
  const [selected, setSelected] = useState(open);
  return (
    <NowContext.Provider value={FIXTURE_NOW}>
      <div className={`mx-auto ${width}`}>
        <Mailbox
          className="h-[40rem]"
          messages={messages}
          selectedId={selected}
          onSelect={setSelected}
          renderers={{ "eval-diff": evalDiff }}
          onAction={(a) => {
            onAction(a);
            const who = { by: "You", at: new Date(FIXTURE_NOW).toISOString() };
            setMessages((list) => list.map((m) => resolveMessage(m, a, who)));
          }}
        />
      </div>
    </NowContext.Provider>
  );
}

const meta = {
  title: "Agent/Mailbox",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "The inbox as a mailbox. Alerts decide what to watch for; this is where what they raise gets resolved. Each message is an envelope around parts, and each part resolves its own way: approve, pick, reply, keep memories, raise a budget, acknowledge. A message is resolved when its parts are; read-only messages can only be dismissed. j/k move, Enter opens, e dismisses. Slack publishes the same parts as the App Home tab with `MailboxView`.",
      },
    },
  },
  args: { onAction: fn() },
  render: (args) => <Demo {...args} />,
} satisfies Meta<ActionArgs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const AllParts: Story = {
  name: "Every kind of part",
  parameters: slack("mailbox@home", "Published to App Home"),
};

export const Reading: Story = {
  name: "Reading a message with several parts",
  render: (args) => <Demo {...args} open="m-approval" />,
  parameters: slack("mail.m-approval"),
};

export const ResolveInPlace: Story = {
  name: "Resolve in place",
  parameters: slack("mail.m-budget"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /Nightly eval is over its budget/ }));
    await userEvent.click(canvas.getByRole("button", { name: "Raise to $75" }));
    await expect(await canvas.findByText(/Limit raised by You/)).toBeInTheDocument();
  },
};

export const Narrow: Story = {
  name: "Narrow: list and pane are separate views",
  render: (args) => <Demo {...args} width="max-w-sm" />,
  parameters: slack("mailbox@home"),
};

export const FromItems: Story = {
  name: "From existing inbox items",
  render: (args) => <Demo {...args} messages={fx.mailFromItems} />,
  parameters: slack("inboxItems", "messageFromItem turns each InboxItem into a one-part message"),
};

export const Empty: Story = {
  render: (args) => <Demo {...args} messages={[]} />,
  parameters: slack("mailbox@home", "With nothing open, Home says so"),
};
