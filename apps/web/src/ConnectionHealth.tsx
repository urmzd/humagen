import { useEffect, useState } from "react";
import { connectionApi, type ConnectionHealth as Health } from "./connectionApi.ts";
import { button, Problem } from "./ui.tsx";

export function ConnectionHealth({
  provider,
  client,
  canCheck,
  initial,
}: {
  provider?: string;
  client?: string;
  canCheck: boolean;
  initial?: Health | null;
}) {
  const [result, setResult] = useState<Health | null>(initial ?? null);
  useEffect(() => {
    if (initial || !provider) return;
    let active = true;
    connectionApi
      .detail(provider)
      .then((detail) => {
        if (active)
          setResult(
            client
              ? (detail.mappings.find((m) => m.kind === "client" && m.id === client)?.health ??
                  null)
              : (detail.connection.health ?? null),
          );
      })
      .catch(() => {});
    return () => {
      active = false;
    };
  }, [provider, client, initial]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  return (
    <div className="flex flex-col items-start gap-2">
      <output aria-live="polite" className="text-aui-xs text-aui-muted">
        {result
          ? `${result.healthy ? "Access verified" : "Access not verified"} · ${new Date(result.checked_at).toLocaleString()}. ${result.message}`
          : "Access has not been checked. Saving credentials does not verify them."}
      </output>
      <Problem message={problem} />
      {canCheck && (
        <button
          type="button"
          className={button}
          disabled={busy}
          onClick={async () => {
            setBusy(true);
            setProblem(null);
            setResult(null);
            try {
              setResult(
                client
                  ? await connectionApi.checkClient(client)
                  : await connectionApi.check(provider ?? ""),
              );
            } catch (error) {
              setProblem(error instanceof Error ? error.message : "Access could not be checked.");
            } finally {
              setBusy(false);
            }
          }}
        >
          {busy ? "Checking…" : "Test access"}
        </button>
      )}
    </div>
  );
}
