import type { BigQuerySetupInput } from "../../../packages/client/src/generated/types.ts";

/** Bounded connection setup. Identity and ownership come from the server. */
export interface BigQueryDraft {
  name: string;
  cloudProject: string;
  datasets: string;
  location: string;
  credentialMode: "oauth" | "adc" | "reference";
  credentialRef: string;
  maxGiB: string;
  maxRows: string;
  timeoutSeconds: string;
  refreshHours: string;
}

export const initialBigQueryDraft: BigQueryDraft = {
  name: "bigquery",
  cloudProject: "",
  datasets: "",
  location: "",
  credentialMode: "oauth",
  credentialRef: "",
  maxGiB: "1",
  maxRows: "200",
  timeoutSeconds: "60",
  refreshHours: "24",
};

export type BigQueryConfig = BigQuerySetupInput;

export type BigQueryProblem =
  | "name"
  | "project"
  | "datasets"
  | "location"
  | "credential"
  | "limits";

function boundedInteger(text: string, min: number, max: number): number | undefined {
  if (!/^\d+$/.test(text)) return;
  const value = Number(text);
  if (Number.isSafeInteger(value) && value >= min && value <= max) return value;
}

function validReference(ref: string): boolean {
  if (ref.length > 512) return false;
  if (/^env:[A-Za-z_][A-Za-z0-9_]*$/.test(ref)) return true;
  if (
    /^keychain:[A-Za-z0-9][A-Za-z0-9 ._@-]{0,127}(\/[A-Za-z0-9][A-Za-z0-9 ._@-]{0,127})?$/.test(ref)
  )
    return true;
  if (!ref.startsWith("op://")) return false;
  const parts = ref.slice(5).split("/");
  return (
    (parts.length === 3 || parts.length === 4) &&
    parts.every((part) => /^[A-Za-z0-9][A-Za-z0-9 ._@-]{0,127}$/.test(part))
  );
}

export function prepareBigQuery(
  draft: BigQueryDraft,
): { config: BigQueryConfig; problem?: never } | { config?: never; problem: BigQueryProblem } {
  const name = draft.name.trim();
  if (!/^[a-z0-9][a-z0-9-]{0,62}$/.test(name)) return { problem: "name" };
  const project = draft.cloudProject.trim();
  if (!/^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/.test(project)) return { problem: "project" };
  const datasets = [
    ...new Set(
      draft.datasets
        .split(/[,\n]/)
        .map((v) => v.trim())
        .filter(Boolean),
    ),
  ];
  if (
    draft.datasets.length > 6000 ||
    datasets.length < 1 ||
    datasets.length > 50 ||
    datasets.some((v) => !/^[A-Za-z0-9_]{1,1024}$/.test(v))
  )
    return { problem: "datasets" };
  const location = draft.location.trim();
  if (location && !/^[A-Za-z0-9-]{1,64}$/.test(location)) return { problem: "location" };
  const credential = draft.credentialRef.trim();
  if (draft.credentialMode === "reference" && !validReference(credential))
    return { problem: "credential" };
  const maxGiB = boundedInteger(draft.maxGiB, 1, 1024);
  const maxRows = boundedInteger(draft.maxRows, 1, 10000);
  const timeout = boundedInteger(draft.timeoutSeconds, 1, 600);
  const refresh = boundedInteger(draft.refreshHours, 1, 720);
  if (
    maxGiB === undefined ||
    maxRows === undefined ||
    timeout === undefined ||
    refresh === undefined
  )
    return { problem: "limits" };
  return {
    config: {
      name,
      project,
      datasets,
      ...(location ? { location } : {}),
      credential: {
        kind: draft.credentialMode,
        ...(draft.credentialMode === "reference" ? { ref: credential } : {}),
      },
      max_bytes_billed: maxGiB * 1073741824,
      max_rows: maxRows,
      timeout_seconds: timeout,
      refresh_hours: refresh,
    },
  };
}
