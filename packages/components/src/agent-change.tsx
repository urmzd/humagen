import { changeToolLabel, readAgentChange, type Task } from "@humagen/core";
import { Callout, Code, Context, Fields, Header, Text } from "@humagen/primitives";
import { Fragment } from "react";

/** Native surfaces review the same recorded proposal as the web, before tool approval. */
export function AgentChangeCards({ tasks }: { tasks: Task[] }) {
  const previews = tasks.flatMap((task) => {
    const preview = readAgentChange(task);
    return preview ? [preview] : [];
  });
  const unique = [...new Map(previews.map((preview) => [preview.change.id, preview])).values()];
  return (
    <>
      {unique.map(({ change, before_prompt, after_prompt, tools, connections, listens }) => (
        <Fragment key={change.id}>
          <Header
            text={`Proposed change to ${change.kind === "workspace_prompt" ? "workspace instructions" : change.agent}`}
          />
          <Callout
            tone={change.evaluated && change.passed ? "info" : "warn"}
            title={
              !change.evaluated
                ? "Evaluation incomplete"
                : change.passed
                  ? "Evaluations passed"
                  : "Evaluation regressed"
            }
            text={
              !change.evaluated
                ? "This change cannot be applied until evaluation completes."
                : change.passed
                  ? "Approval is required to apply this change."
                  : "An authorized reviewer must give an override reason before this change can be applied."
            }
          />
          {change.error && <Callout tone="bad" title="Evaluation error" text={change.error} />}
          <Code title="Prompt before" text={before_prompt || "(empty)"} language="text" />
          <Code title="Prompt after" text={after_prompt || "(empty)"} language="text" />
          <Fields
            items={[
              {
                label: "Tools",
                value: tools.length ? tools.map(changeToolLabel).join(", ") : "No tools",
              },
              ...change.scores.map((score) => ({
                label: score.suite,
                value: `Before ${score.before}% → After ${score.after}%. ${score.passed ? "Passed" : "Regression"}`,
              })),
            ]}
          />
          <Header text="Current connections" />
          {connections?.length ? (
            <Fields
              items={connections.map((connection) => ({
                label: connection.name,
                value: `${connection.service} · ${connection.scope} · ${connection.status}`,
              }))}
            />
          ) : (
            <Text
              text={
                connections === undefined
                  ? "Not included in this preview."
                  : "No current connections."
              }
            />
          )}
          <Header text="Current listening locations" />
          {listens?.length ? (
            <Fields
              items={listens.map((location) => ({
                label: `${location.connection} · ${location.place}`,
                value: `${location.project ?? ""}${location.enabled === undefined ? "" : location.enabled ? " · Enabled" : " · Disabled"}`,
              }))}
            />
          ) : (
            <Text
              text={
                listens === undefined
                  ? "Not included in this preview."
                  : "No current listening locations."
              }
            />
          )}
          {change.override_reason && <Text text={`Override reason: ${change.override_reason}`} />}
          <Context items={[`Proposal ${change.id}. This preview does not apply changes.`]} />
        </Fragment>
      ))}
    </>
  );
}
