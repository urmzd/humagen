import { TurnView } from "@vultus/components";
import { replay } from "@vultus/core";
import { renderToSlack } from "@vultus/render-slack";
import { WebSurface } from "@vultus/render-web";

// One stream of events from any runtime...
const turn = replay([
  { type: "turn.started", turnId: "t1", agent: { name: "Assistant" } },
  { type: "task.upserted", task: { id: "s", title: "Searched runbooks", status: "complete" } },
  { type: "text.delta", text: "Roll back deploy `#4812`, then watch the 5xx rate." },
  { type: "turn.completed" },
]);

// ...one component, rendered to the web...
export const Web = () => (
  <WebSurface onAction={(action) => console.log(action.id, action.payload)}>
    <TurnView turn={turn} />
  </WebSurface>
);

// ...and to Slack Block Kit, ready for chat.postMessage.
export const slack = renderToSlack(<TurnView turn={turn} />);
