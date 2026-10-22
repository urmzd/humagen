import { ActionId, type InboxItem, openCount, type UiAction } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import { NowContext } from "@humagen/render-web";
import { Inbox, InboxButton } from "@humagen/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useState } from "react";
import { expect, fn, userEvent, waitFor, within } from "storybook/test";
import { type ActionArgs, FIXTURE_NOW, slack } from "../../support";

function resolve(items: InboxItem[], action: UiAction): InboxItem[] {
  const at = new Date().toISOString();
  return items.map((item) => {
    const match =
      item.id === action.payload.target ||
      (item.kind === "approval" && item.approval.id === action.payload.target) ||
      (item.kind === "clarification" && item.clarification.id === action.payload.target);
    if (!match) return item;
    if (action.id === ActionId.dismiss) return { ...item, state: "dismissed" };
    if (action.id === ActionId.snooze) return { ...item, state: "snoozed" };
    if (item.kind === "approval" && (action.id === ActionId.approve || action.id === ActionId.deny))
      return {
        ...item,
        state: "resolved",
        approval: {
          ...item.approval,
          decision:
            action.id === ActionId.approve
              ? { kind: "approved", by: "You", at }
              : { kind: "denied", by: "You", at },
        },
      };
    if (item.kind === "clarification" && action.id === ActionId.clarify)
      return {
        ...item,
        state: "resolved",
        clarification: {
          ...item.clarification,
          answer: { kind: "answered", by: "You", at, optionIds: [action.payload.option ?? ""] },
        },
      };
    return item;
  });
}

function Demo({ onAction }: ActionArgs) {
  const [items, setItems] = useState(fx.inboxItems);
  const [open, setOpen] = useState(true);
  return (
    <NowContext.Provider value={FIXTURE_NOW}>
      <div className="mx-auto flex max-w-2xl flex-col gap-3">
        <nav className="flex items-center justify-between border-aui-border border-b pb-2">
          <span className="font-semibold text-aui-sm">Assistant</span>
          <InboxButton count={openCount(items)} expanded={open} onClick={() => setOpen(!open)} />
        </nav>
        {open && (
          <Inbox
            className="max-h-[42rem]"
            items={items}
            onAction={(a) => {
              onAction(a);
              setItems((list) => resolve(list, a));
            }}
          />
        )}
      </div>
    </NowContext.Provider>
  );
}

const meta = {
  title: "Agent/Inbox",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Everything waiting on a person across conversations: outages first, then decisions, then failures. Filter tabs, j/k to move, items resolve in place with a short record and then move to Resolved. Slack publishes the same InboxView as the App Home tab.",
      },
    },
  },
  args: { onAction: fn() },
  render: (args) => <Demo {...args} />,
} satisfies Meta<ActionArgs>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Mixed: Story = {
  name: "Mixed items",
  parameters: slack("inboxItems", "Published to App Home"),
};

export const ResolveInPlace: Story = {
  name: "Resolve in place",
  parameters: slack("inboxItems"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /^Cancel: Post the incident/ }));
    await expect((await canvas.findAllByText(/Rejected by You/)).length).toBeGreaterThan(0);
    await waitFor(() =>
      expect(canvas.getByRole("button", { name: "Inbox, 5 need you" })).toBeInTheDocument(),
    );
  },
};

export const Empty: Story = {
  render: () => <Inbox items={[]} />,
  parameters: slack("inboxItems", "With no open items Home shows the empty state"),
};
