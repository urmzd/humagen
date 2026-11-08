import { useEffect, useState } from "react";
import { connectionApi, type ConnectionDetail as Detail } from "./connectionApi.ts";
import { card, Problem } from "./ui.tsx";

export function ConnectionDetail({ provider }: { provider: string }) {
  const [detail, setDetail] = useState<Detail | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  useEffect(() => {
    let active = true;
    connectionApi
      .detail(provider)
      .then((value) => {
        if (active) setDetail(value);
      })
      .catch((error: unknown) => {
        if (active)
          setProblem(
            error instanceof Error ? error.message : "Connection details could not be loaded.",
          );
      });
    return () => {
      active = false;
    };
  }, [provider]);
  return (
    <section className={`${card} flex flex-col gap-3`} aria-label="Connection details">
      <h2 className="font-medium">{detail?.connection.name ?? "Connection"} details</h2>
      <Problem message={problem} />
      {detail && (
        <>
          <p className="text-aui-sm">
            Granted permissions:{" "}
            {detail.connection.granted_scopes === undefined
              ? "The provider did not report granted scopes."
              : detail.connection.granted_scopes.length
                ? detail.connection.granted_scopes.join(", ")
                : "The provider reported no scopes."}
          </p>
          <p className="text-aui-xs text-aui-muted">{detail.note}</p>
          {detail.mappings.length === 0 ? (
            <p className="text-aui-sm">No configured uses are visible to you.</p>
          ) : (
            <ul className="flex flex-col gap-3">
              {detail.mappings.map((use) => (
                <li key={`${use.kind}:${use.id}`} className="text-aui-sm">
                  <p className="font-medium">
                    {use.name}{" "}
                    <span className="font-normal text-aui-muted">
                      {use.kind.replaceAll("_", " ")}
                    </span>
                  </p>
                  <p>Agents: {use.agents.length ? use.agents.join(", ") : "None visible"}</p>
                  <p>
                    Projects:{" "}
                    {use.projects.length
                      ? use.projects.map((p) => p || "Workspace").join(", ")
                      : "None visible"}
                  </p>
                  {use.places.length > 0 && <p>Reply destinations: {use.places.join(", ")}</p>}
                  {use.tools.length > 0 && <p>Approved tools: {use.tools.join(", ")}</p>}
                </li>
              ))}
            </ul>
          )}
          {detail.truncated && (
            <p className="text-aui-xs text-aui-muted">
              This bounded view may omit additional configured uses.
            </p>
          )}
        </>
      )}
    </section>
  );
}
