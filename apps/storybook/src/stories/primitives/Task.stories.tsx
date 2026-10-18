import type { Meta, StoryObj } from "@storybook/react-vite";
import type { TaskStatus } from "@warp/core";
import * as fx from "@warp/core/fixtures";
import { Plan, Task } from "@warp/primitives";
import { useEffect, useState } from "react";
import { Canvas, slackPrimitive } from "../../support";

type Args = { title: string; status: TaskStatus; details: string; output: string };

const meta = {
  title: "Primitives/Task and Plan",
  tags: ["slack"],
  parameters: slackPrimitive(
    "Task is a task_card block and Plan a plan block (or chat.appendStream task_update chunks while streaming)",
  ),
  args: {
    title: "Checking recent deploys",
    status: "in_progress",
    details: "payments-api, last 24 hours",
    output: "",
  },
  argTypes: {
    status: { control: "inline-radio", options: ["pending", "in_progress", "complete", "error"] },
  },
  render: ({ title, status, details, output }) => (
    <Canvas>
      <Task
        {...{ id: "t" }}
        title={title}
        status={status}
        details={details || undefined}
        output={output || undefined}
      />
    </Canvas>
  ),
} satisfies Meta<Args>;

export default meta;
type Story = StoryObj<typeof meta>;

export const TaskPlayground: Story = { name: "Task" };

export const Timeline: Story = {
  name: "Tasks without a plan",
  render: () => (
    <Canvas>
      {fx.codingTurn.tasks.map((t) => (
        <Task key={t.id} {...t} />
      ))}
      <Task {...{ id: "p", title: "Open a pull request", status: "pending" }} />
    </Canvas>
  ),
};

export const PlanRunning: Story = {
  name: "Plan, running",
  render: () => (
    <Canvas>
      <Plan title={fx.streamingTurn.planTitle ?? "Working"}>
        {fx.streamingTurn.tasks.map((t) => (
          <Task key={t.id} {...t} />
        ))}
        <Task {...{ id: "t4", title: "Summarize findings", status: "pending" }} />
      </Plan>
    </Canvas>
  ),
};

export const PlanDone: Story = {
  name: "Plan, done (folded)",
  render: () => (
    <Canvas>
      <Plan title="Worked for 12s">
        {fx.doneTurn.tasks.map((t) => (
          <Task key={t.id} {...t} />
        ))}
      </Plan>
    </Canvas>
  ),
};

export const PlanFailed: Story = {
  name: "Plan, failed",
  render: () => (
    <Canvas>
      <Plan title="Error">
        {fx.failedTurn.tasks.map((t) => (
          <Task key={t.id} {...t} />
        ))}
      </Plan>
    </Canvas>
  ),
};

const titles = ["Reading the alert", "Searching runbooks", "Checking recent deploys"];

function LivePlan() {
  const [step, setStep] = useState(0);
  useEffect(() => {
    const t = setInterval(() => setStep((s) => (s >= titles.length + 3 ? 0 : s + 1)), 1200);
    return () => clearInterval(t);
  }, []);
  const done = step >= titles.length;
  return (
    <Canvas>
      <Plan
        key={step === 0 ? "restart" : "run"}
        title={done ? "Worked for 4s" : (titles[step] ?? "")}
      >
        {titles.map((title, i) => (
          <Task
            key={title}
            id={title}
            title={title}
            status={i < step ? "complete" : i === step ? "in_progress" : "pending"}
          />
        ))}
      </Plan>
    </Canvas>
  );
}

export const PlanLive: Story = {
  name: "Plan, live (folds when done)",
  render: () => <LivePlan />,
};
