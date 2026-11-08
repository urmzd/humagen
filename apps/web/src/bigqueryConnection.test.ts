import assert from "node:assert/strict";
import { test } from "node:test";
import { initialBigQueryDraft, prepareBigQuery } from "./bigqueryConnection.ts";

const valid = {
  ...initialBigQueryDraft,
  cloudProject: "cloud-project",
  datasets: "sales, support\nsales",
};

test("BigQuery setup sends explicit datasets and query limits without client-supplied identity", () => {
  const result = prepareBigQuery(valid);
  assert.deepEqual(result.config, {
    name: "bigquery",
    credential: { kind: "oauth" },
    refresh_hours: 24,
    project: "cloud-project",
    datasets: ["sales", "support"],
    max_bytes_billed: 1073741824,
    max_rows: 200,
    timeout_seconds: 60,
  });
  assert.equal(prepareBigQuery({ ...valid, datasets: "" }).problem, "datasets");
  assert.equal(prepareBigQuery({ ...valid, datasets: "sales.*" }).problem, "datasets");
  assert.equal(
    prepareBigQuery({ ...valid, datasets: Array.from({ length: 51 }, (_, i) => `d${i}`).join(",") })
      .problem,
    "datasets",
  );
});

test("BigQuery config accepts deployment references and never an inline secret", () => {
  for (const credentialRef of [
    "env:BIGQUERY_KEY",
    "keychain:humagen/bigquery",
    "op://Private/BigQuery/credential",
  ] as const) {
    const result = prepareBigQuery({ ...valid, credentialMode: "reference", credentialRef });
    assert.deepEqual(result.config?.credential, { kind: "reference", ref: credentialRef });
  }
  for (const credentialRef of [
    '{"private_key":"secret"}',
    "token",
    "env:",
    "env:NAME\nOTHER",
    "file:/key.json",
    "op://Private/BigQuery",
  ]) {
    assert.equal(
      prepareBigQuery({ ...valid, credentialMode: "reference", credentialRef }).problem,
      "credential",
    );
  }
  const adc = prepareBigQuery({ ...valid, credentialMode: "adc", credentialRef: "unused-secret" });
  assert.deepEqual(adc.config?.credential, { kind: "adc" });
});

test("BigQuery config rejects unlimited, fractional and unsafe numeric limits", () => {
  for (const [key, value] of [
    ["maxGiB", "0"],
    ["maxGiB", "1025"],
    ["maxRows", "10001"],
    ["maxRows", "1.5"],
    ["timeoutSeconds", "601"],
    ["refreshHours", "0"],
    ["refreshHours", "1e3"],
    ["maxGiB", "Infinity"],
  ] as const) {
    assert.equal(prepareBigQuery({ ...valid, [key]: value }).problem, "limits", `${key}=${value}`);
  }
});
