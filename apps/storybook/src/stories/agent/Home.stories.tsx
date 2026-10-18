import type { Meta, StoryObj } from "@storybook/react-vite";
import { Home } from "@warp/components";
import { ActionId, HOME_ID, type HomeModel } from "@warp/core";
import * as fx from "@warp/core/fixtures";
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
          "Where a person lands: a greeting (the page's one title), ways to start, notifications, who answers them, connections and access. Notifications are inbox items waiting on the person and open alerts: critical alerts first, then the inbox, then other alerts, newest first within each, eight at most with the rest counted and linked. Each links to its own page: the web links the title, Slack writes the address out under the row, and only when the host gave an absolute one. One `Home` component: the web gets the ask box and a grid of starts; Slack publishes the same model as the App Home tab with `homeView`, with three buttons, a select for who answers direct messages, and access folded into one line.",
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
    await expect(canvas.getByRole("heading", { level: 1 })).toHaveTextContent("Hi Ada, I'm Warp.");
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
