import * as fx from "@agent-ui/core/fixtures";
import { Fields, Table } from "@agent-ui/primitives";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { Canvas, slackPrimitive } from "../../support";

const meta = {
  title: "Primitives/Fields and Table",
  tags: ["slack"],
  parameters: slackPrimitive("Fields is a section with up to 10 fields; Table is a table block"),
} satisfies Meta;

export default meta;
type Story = StoryObj<typeof meta>;

export const FieldList: Story = {
  name: "Fields",
  render: () => (
    <Canvas>
      <Fields items={fx.pendingApproval.preview ?? []} />
    </Canvas>
  ),
};

export const DataTable: Story = {
  name: "Table",
  render: () => (
    <Canvas>
      <Table
        columns={[
          { key: "deploy", label: "Deploy" },
          { key: "time", label: "Time" },
          { key: "errors", label: "5xx rate", align: "right" },
        ]}
        rows={[
          { deploy: "#4810", time: "09:12", errors: "0.1%" },
          { deploy: "#4811", time: "Yesterday 17:20", errors: "0.1%" },
          { deploy: "#4812", time: "14:50", errors: "4.2%" },
        ]}
      />
    </Canvas>
  ),
};
