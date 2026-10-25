import { Home } from "@humagen/components";
import { ActionId, HOME_ID, type HomeModel } from "@humagen/core";
import * as fx from "@humagen/core/fixtures";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { expect, fn, userEvent, within } from "storybook/test";
import { type ActionArgs, Canvas, slack } from "../../support";

type Args = ActionArgs & { home: HomeModel };

const meta = {
  title: "Agent/Home",
  tags: ["slack"],
  parameters: {
    docs: {
      description: {
        component:
          "Where a person lands: a greeting (the page's one title), ways to start, notifications, what's new, their projects, who answers them, connections and access. Notifications are inbox items waiting on the person and open alerts: critical alerts first, then the inbox, then other alerts, newest first within each, eight at most with the rest counted and linked. Each links to its own page: the web links the title, Slack writes the address out under the row, and only when the host gave an absolute one. What's new is up to five announcements from the person's projects, unread first, and Your projects up to eight of the projects they have a role in, where something happened most recently first, with the rest counted and linked: the web links each title, Slack gives each an Open button. A section the host leaves out is not shown. One `Home` component: the web gets the ask box and a grid of starts; Slack publishes the same model as the App Home tab with `homeView`, with its start buttons, a select for who answers direct messages, and access folded into one line.",
      },
    },
  },
  args: { onAction: fn(), home: fx.home },
  render: ({ onAction, home }) => (
    <Canvas onAction={onAction}>
      <Home home={home} />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const Default: Story = {
  args: { home: fx.homes.default },
  parameters: slack("homes.default@home", "Published to App Home"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("heading", { level: 1 })).toHaveTextContent(
      "Hi Ada, I'm Humagen.",
    );
    // Each notification's title is a link to that item, and stays in the tab.
    const alert = canvas.getByRole("link", { name: /deploy-failures fired/ });
    await expect(alert).toHaveAttribute(
      "href",
      "https://example.com/console/?view=alerts&alert=al-deploy-failed",
    );
    await expect(canvas.getByRole("link", { name: /docs-bot wants/ })).toHaveAttribute(
      "href",
      "https://example.com/console/?view=inbox&message=msg-docs-reply",
    );
    await userEvent.type(canvas.getByLabelText("What should we work on?"), "Why did it fail?");
    await userEvent.click(canvas.getByRole("button", { name: "Ask" }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.homeAsk,
      payload: { turnId: HOME_ID, target: "ask", text: "Why did it fail?" },
    });
    await userEvent.selectOptions(canvas.getByLabelText("Answered by"), "triage");
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.homeAnsweredBy,
      payload: { turnId: HOME_ID, target: "answered_by", option: "triage" },
    });
  },
};

/** The Slack app's Home for a person its client maps: read for them, with no access or connections it cannot know. */
export const ReadForAPerson: Story = {
  args: { home: fx.homes.readFor },
  parameters: slack("homes.readFor@home", "Published to App Home"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(
      canvas.getByRole("link", { name: "The Field offsite moves to Lisbon, in May" }),
    ).toHaveAttribute("href", "https://example.com/console/projects/Field?message=m-offsite");
    await expect(canvas.getByRole("link", { name: "Field engineering" })).toHaveAttribute(
      "href",
      "https://example.com/console/projects/Field",
    );
    await expect(canvas.queryByText("Your access")).toBeNull();
  },
};

export const AlertsOnly: Story = {
  args: { home: fx.homes.alertsOnly },
  parameters: slack("homes.alertsOnly@home", "Published to App Home"),
};

export const MoreThanShown: Story = {
  args: { home: fx.homes.overflow },
  parameters: slack("homes.overflow@home", "Published to App Home"),
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("link", { name: "8 more in the inbox" })).toHaveAttribute(
      "href",
      "https://example.com/console/?view=inbox",
    );
    await expect(canvas.getByRole("link", { name: "4 more open alerts" })).toBeInTheDocument();
  },
};

/** No console address: nothing to link, so inbox items keep Review. */
export const WithoutLinks: Story = {
  args: { home: fx.homes.unlinked },
  parameters: slack("homes.unlinked@home", "Published to App Home"),
  play: async ({ args, canvasElement }) => {
    const canvas = within(canvasElement);
    await userEvent.click(canvas.getByRole("button", { name: /Review: docs-bot/ }));
    await expect(args.onAction).toHaveBeenCalledWith({
      id: ActionId.open,
      payload: { turnId: "msg-docs-reply", target: "msg-docs-reply" },
    });
  },
};

/** What went wrong with the last question, and the files going with the next, under the field. */
export const AskNotices: Story = {
  args: {
    home: {
      ...fx.home,
      ask: {
        ...fx.home.ask,
        notices: [
          { tone: "bad", text: "The question could not be sent. The platform is not reachable." },
          { tone: "info", text: "Sent with your question: deploy.log, plan.md." },
        ],
      },
    },
  },
  play: async ({ canvasElement }) => {
    const canvas = within(canvasElement);
    await expect(canvas.getByRole("alert")).toHaveTextContent("could not be sent");
  },
};

export const NothingWaiting: Story = {
  args: { home: fx.homes.empty },
  parameters: slack("homes.empty@home", "Published to App Home"),
};

export const CannotConnect: Story = {
  args: { home: fx.homes.noConnect },
  parameters: slack("homes.noConnect@home", "Published to App Home"),
};

export const NoGrants: Story = {
  args: { home: fx.homes.noGrants },
  parameters: slack("homes.noGrants@home", "Published to App Home"),
  play: async ({ canvasElement }) => {
    // No ask in the model, so no ask box.
    await expect(within(canvasElement).queryByLabelText("What should we work on?")).toBeNull();
  },
};
