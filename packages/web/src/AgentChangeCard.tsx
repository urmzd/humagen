import { changeToolLabel, readAgentChange, type Task } from "@humagen/core";
import { ChangeReview } from "./ChangeReview.tsx";

export { readAgentChange } from "@humagen/core";

export function AgentChangeCards({ tasks }: { tasks: Task[] }) {
  const previews = tasks.flatMap((task) => {
    const preview = readAgentChange(task);
    return preview ? [preview] : [];
  });
  const unique = [...new Map(previews.map((preview) => [preview.change.id, preview])).values()];
  if (!unique.length) return null;
  return (
    <div className="flex min-w-0 flex-col gap-4">
      {unique.map(({ change, before_prompt, after_prompt, tools, connections, listens }) => (
        <section
          key={change.id}
          aria-label={`Proposed change to ${change.kind === "workspace_prompt" ? "workspace instructions" : change.agent}`}
          className="flex min-w-0 flex-col gap-3 rounded-aui border border-aui-border p-4"
        >
          <h3 className="text-aui-base font-medium">
            Proposed change to{" "}
            {change.kind === "workspace_prompt" ? "workspace instructions" : change.agent}
          </h3>
          <p
            className={`text-aui-sm ${change.evaluated && change.passed ? "text-aui-fg" : "text-aui-warn"}`}
          >
            {!change.evaluated
              ? "Evaluation incomplete. This change cannot be applied."
              : change.passed
                ? "Evaluations passed. Approval is required to apply this change."
                : "An evaluation regressed. An authorized reviewer must give an override reason before this change can be applied."}
          </p>
          {change.error && <p className="text-aui-sm text-aui-bad">{change.error}</p>}
          <div className="min-w-0">
            <h4 className="mb-2 text-aui-sm font-medium">Prompt changes</h4>
            <ChangeReview
              before={before_prompt}
              after={after_prompt}
              path="Agent prompt"
              layout="auto"
            />
          </div>
          <div className="text-aui-sm">
            <h4 className="font-medium">Tools</h4>
            <p className="break-words text-aui-muted">
              {tools.length ? tools.map(changeToolLabel).join(", ") : "No tools"}
            </p>
          </div>
          {change.scores.length > 0 && (
            <div className="overflow-x-auto">
              <table className="w-full text-left text-aui-sm">
                <caption className="sr-only">
                  Evaluation scores before and after the proposed change
                </caption>
                <thead>
                  <tr>
                    <th scope="col">Suite</th>
                    <th scope="col">Before</th>
                    <th scope="col">After</th>
                    <th scope="col">Result</th>
                  </tr>
                </thead>
                <tbody>
                  {change.scores.map((score) => (
                    <tr key={score.suite}>
                      <th scope="row" className="py-2 pr-3 font-normal">
                        {score.suite}
                      </th>
                      <td>{score.before}%</td>
                      <td>{score.after}%</td>
                      <td>{score.passed ? "Passed" : "Regression"}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="text-aui-sm">
            <h4 className="font-medium">Current connections</h4>
            {connections === undefined ? (
              <p className="text-aui-muted">Not included in this preview.</p>
            ) : connections.length ? (
              <ul className="flex flex-col gap-1">
                {connections.map((connection) => (
                  <li key={`${connection.scope}:${connection.name}`} className="break-words">
                    {connection.name} | {connection.service} | {connection.scope} |{" "}
                    {connection.status}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-aui-muted">No current connections.</p>
            )}
          </div>
          <div className="text-aui-sm">
            <h4 className="font-medium">Current listening locations</h4>
            {listens === undefined ? (
              <p className="text-aui-muted">Not included in this preview.</p>
            ) : listens.length ? (
              <ul className="flex flex-col gap-1">
                {listens.map((location) => (
                  <li
                    key={`${location.connection}:${location.place}:${location.project ?? ""}`}
                    className="break-words"
                  >
                    {location.connection} | {location.place}
                    {location.project ? ` | ${location.project}` : ""}
                    {location.enabled === undefined
                      ? ""
                      : location.enabled
                        ? " | Enabled"
                        : " | Disabled"}
                  </li>
                ))}
              </ul>
            ) : (
              <p className="text-aui-muted">No current listening locations.</p>
            )}
          </div>
          {change.override_reason && (
            <p className="text-aui-sm">Override reason: {change.override_reason}</p>
          )}
        </section>
      ))}
    </div>
  );
}
