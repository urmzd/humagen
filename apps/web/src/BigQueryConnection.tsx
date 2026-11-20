import { defaultCopy } from "@humagen/components";
import { useCallback, useEffect, useRef, useState } from "react";
import type {
  BigQuerySetupView,
  CatalogConnectorView,
} from "../../../packages/client/src/generated/types.ts";
import { api } from "./api.ts";
import { bigqueryApi } from "./bigqueryApi.ts";
import { type BigQueryDraft, initialBigQueryDraft, prepareBigQuery } from "./bigqueryConnection.ts";
import { ConnectionSection } from "./ConnectionSection.tsx";
import { button, field, Label, Problem, primary } from "./ui.tsx";

const copy = defaultCopy.bigQueryConnection;

/** Save a private connection, verify real metadata access, then explicitly publish. */
export function BigQueryConnection({
  canWrite,
  onCancel,
  onVerified,
  onAuthorize,
  onSaved,
}: {
  canWrite: boolean;
  onCancel: () => void;
  onVerified?: () => void;
  onAuthorize?: () => void;
  /** A new connection was saved, by its connector's name: a corpus adds it as a member. */
  onSaved?: (name: string) => void;
}) {
  const [draft, setDraft] = useState(initialBigQueryDraft);
  const [setup, setSetup] = useState<BigQuerySetupView | null>(null);
  const [connectors, setConnectors] = useState<CatalogConnectorView[]>([]);
  const [selected, setSelected] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState<
    "create" | "reconnect" | "verify" | "publish" | "withdraw" | null
  >(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [formError, setFormError] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [failed, setFailed] = useState<Set<string>>(new Set());
  const epoch = useRef(0);

  const reload = useCallback(async () => {
    const request = ++epoch.current;
    setLoading(true);
    setLoadError(null);
    setNotice(null);
    try {
      const [catalog, capabilities] = await Promise.all([
        api.catalog(),
        canWrite ? bigqueryApi.setup() : null,
      ]);
      if (request !== epoch.current) return;
      const next = catalog.connectors.filter((connector) => connector.system === "bigquery");
      setConnectors(next);
      setSetup(capabilities);
      setSelected((current) =>
        next.some((c) => c.name === current)
          ? current
          : (next.find((c) => c.mine)?.name ?? next[0]?.name ?? ""),
      );
    } catch {
      if (request !== epoch.current) return;
      setConnectors([]);
      setSetup(null);
      setLoadError(copy.loadFailed);
    } finally {
      if (request === epoch.current) setLoading(false);
    }
  }, [canWrite]);

  useEffect(() => {
    void reload();
    return () => {
      epoch.current++;
    };
  }, [reload]);

  const connector = connectors.find((item) => item.name === selected);
  const owner = canWrite && connector?.mine;
  const checkFailed = connector && (failed.has(connector.name) || Boolean(connector.last_error));
  const editable = canWrite && setup !== null;
  const locked = loading || busy !== null;

  function change<K extends keyof BigQueryDraft>(key: K, value: BigQueryDraft[K]) {
    setDraft((current) => ({ ...current, [key]: value }));
    setFormError(null);
  }

  async function save() {
    if (locked || !setup || !canWrite) return;
    const result = prepareBigQuery(draft);
    if (result.problem) {
      setFormError(copy.problems[result.problem]);
      return;
    }
    if (draft.credentialMode === "oauth" && !setup.connected) {
      setFormError(copy.connectFirst);
      return;
    }
    const request = epoch.current;
    setBusy("create");
    setFormError(null);
    setNotice(null);
    try {
      const created = await bigqueryApi.create(result.config);
      if (request !== epoch.current) return;
      setConnectors((items) => [
        ...items.filter((item) => item.name !== created.name),
        {
          ...created,
          mine: true,
          capabilities:
            result.config.credential?.kind === "oauth" ? ["catalog"] : ["catalog", "live"],
        },
      ]);
      setSelected(created.name);
      setNotice(copy.saved);
      onSaved?.(created.name);
    } catch {
      if (request === epoch.current) setFormError(copy.saveFailed);
    } finally {
      if (request === epoch.current) setBusy(null);
    }
  }

  async function act(action: "verify" | "publish" | "withdraw" | "reconnect") {
    if (!connector || !owner || locked) return;
    const current = connector;
    const request = epoch.current;
    setBusy(action);
    setNotice(null);
    setActionError(null);
    try {
      const result = await (action === "reconnect"
        ? bigqueryApi.reconnect(current.name)
        : action === "verify"
          ? api.crawlCatalogConnector(current.name)
          : action === "publish"
            ? api.publishCatalogConnector(current.name)
            : api.withdrawCatalogConnector(current.name));
      if (request !== epoch.current) return;
      setConnectors((items) =>
        items.map((item) =>
          item.name === current.name
            ? {
                ...item,
                ...result,
                published: result.published,
                withdrawn: result.withdrawn,
                ...(action === "verify" ? { last_error: undefined } : {}),
              }
            : item,
        ),
      );
      if (action === "verify") {
        setFailed((previous) => {
          const next = new Set(previous);
          next.delete(current.name);
          return next;
        });
        setNotice(copy.verified);
        onVerified?.();
      } else {
        setNotice(
          action === "publish"
            ? copy.published
            : action === "reconnect"
              ? copy.reconnected
              : copy.withdrawn,
        );
      }
    } catch {
      if (request !== epoch.current) return;
      if (action === "verify") setFailed((previous) => new Set(previous).add(current.name));
      setActionError(
        action === "verify"
          ? copy.verifyFailed
          : action === "reconnect"
            ? copy.reconnectFailed
            : copy.actionFailed,
      );
    } finally {
      if (request === epoch.current) setBusy(null);
    }
  }

  return (
    <ConnectionSection
      embedded
      title={copy.title}
      actions={
        <button type="button" className={button} disabled={busy !== null} onClick={onCancel}>
          {copy.close}
        </button>
      }
    >
      <p className="max-w-2xl text-aui-sm text-aui-muted">{copy.intro}</p>
      {editable && (
        <details
          className="min-w-0 rounded-aui border border-aui-border p-4"
          open={connectors.length === 0}
        >
          <summary className="cursor-pointer text-aui-sm font-medium">{copy.configure}</summary>
          <form
            className="mt-4 flex min-w-0 flex-col gap-4"
            onSubmit={(event) => {
              event.preventDefault();
              void save();
            }}
          >
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              <Label text={copy.name}>
                <input
                  className={field}
                  value={draft.name}
                  maxLength={63}
                  required
                  onChange={(event) => change("name", event.target.value)}
                />
              </Label>
              <Label text={copy.cloudProject}>
                <input
                  className={field}
                  value={draft.cloudProject}
                  maxLength={128}
                  required
                  onChange={(event) => change("cloudProject", event.target.value)}
                />
              </Label>
              <Label text={copy.datasets} hint={copy.datasetsHint}>
                <textarea
                  className={field}
                  rows={2}
                  value={draft.datasets}
                  maxLength={6000}
                  required
                  onChange={(event) => change("datasets", event.target.value)}
                />
              </Label>
              <Label text={copy.location}>
                <input
                  className={field}
                  value={draft.location}
                  maxLength={64}
                  onChange={(event) => change("location", event.target.value)}
                />
              </Label>
            </div>
            <Label text={copy.authentication} hint={copy.authenticationHint}>
              <select
                className={field}
                value={draft.credentialMode}
                onChange={(event) =>
                  change("credentialMode", event.target.value as BigQueryDraft["credentialMode"])
                }
              >
                <option value="oauth" disabled={!setup.oauth_available}>
                  {copy.googleAccount}
                </option>
                {setup.adc.length > 0 && <option value="adc">{copy.defaultCredentials}</option>}
                {setup.credential_refs.length > 0 && (
                  <option value="reference">{copy.reference}</option>
                )}
              </select>
            </Label>
            {draft.credentialMode === "oauth" && (
              <div className="flex flex-wrap items-center gap-2 text-aui-sm">
                <p className="text-aui-muted">
                  {setup.connected
                    ? copy.accountReady
                    : setup.oauth_available
                      ? copy.connectFirst
                      : copy.oauthUnavailable}
                </p>
                {setup.oauth_available && onAuthorize && (
                  <button type="button" className={button} disabled={locked} onClick={onAuthorize}>
                    {setup.connected ? copy.reconnectGoogle : copy.connectGoogle}
                  </button>
                )}
              </div>
            )}
            {draft.credentialMode === "adc" && (
              <p className="text-aui-xs text-aui-muted">
                {copy.adcScopes(
                  setup.adc
                    .map(
                      (scope) =>
                        `${scope.project}: ${scope.datasets.length ? scope.datasets.join(", ") : copy.allDatasets}`,
                    )
                    .join("; "),
                )}
              </p>
            )}
            {draft.credentialMode === "reference" && (
              <Label
                text={copy.credentialRef}
                hint={copy.referenceHint(setup.credential_refs.join(", "))}
              >
                <input
                  className={field}
                  autoComplete="off"
                  value={draft.credentialRef}
                  maxLength={512}
                  required
                  onChange={(event) => change("credentialRef", event.target.value)}
                />
              </Label>
            )}
            <div className="grid min-w-0 gap-3 sm:grid-cols-2">
              {(
                [
                  ["maxGiB", copy.maxGiB, 1024],
                  ["maxRows", copy.maxRows, 10000],
                  ["timeoutSeconds", copy.timeout, 600],
                  ["refreshHours", copy.refresh, 720],
                ] as const
              ).map(([key, label, max]) => (
                <Label key={key} text={label}>
                  <input
                    className={field}
                    type="number"
                    min={1}
                    max={max}
                    step={1}
                    required
                    value={draft[key]}
                    onChange={(event) => change(key, event.target.value)}
                  />
                </Label>
              ))}
            </div>
            <p className="text-aui-xs text-aui-muted">{copy.limitsHint}</p>
            <Problem message={formError} />
            <div>
              <button
                className={primary}
                type="submit"
                disabled={locked || (draft.credentialMode === "oauth" && !setup.connected)}
              >
                {busy === "create" ? copy.saving : copy.save}
              </button>
            </div>
          </form>
        </details>
      )}
      <div className="flex min-w-0 flex-col gap-4 border-t border-aui-border pt-4">
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h3 className="text-aui-sm font-medium">{copy.configured}</h3>
          <button type="button" className={button} disabled={locked} onClick={() => void reload()}>
            {copy.reload}
          </button>
        </div>
        <Problem message={loadError} />
        {loading ? (
          <output className="text-aui-sm text-aui-muted">{copy.loading}</output>
        ) : !loadError && connectors.length === 0 ? (
          <p className="text-aui-sm text-aui-muted">{copy.empty}</p>
        ) : null}
        {!loading && connector && (
          <>
            <Label text={copy.select}>
              <select
                className={field}
                value={selected}
                disabled={busy !== null}
                onChange={(event) => {
                  setSelected(event.target.value);
                  setActionError(null);
                  setNotice(null);
                }}
              >
                {connectors.map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.name}
                  </option>
                ))}
              </select>
            </Label>
            <dl className="grid gap-3 text-aui-sm sm:grid-cols-3">
              <div>
                <dt className="text-aui-muted">{copy.metadata}</dt>
                <dd>
                  {checkFailed
                    ? copy.checkFailed
                    : connector.crawled_at
                      ? copy.checked
                      : copy.unchecked}
                </dd>
              </div>
              <div>
                <dt className="text-aui-muted">{copy.lastSuccess}</dt>
                <dd>
                  {connector.crawled_at ? (
                    <time dateTime={connector.crawled_at}>
                      {new Date(connector.crawled_at).toLocaleString()}
                    </time>
                  ) : (
                    copy.never
                  )}
                </dd>
              </div>
              <div>
                <dt className="text-aui-muted">{copy.entries}</dt>
                <dd>{connector.entries}</dd>
              </div>
            </dl>
            <p className="text-aui-xs text-aui-muted">{copy.verifyHint}</p>
            <p className="text-aui-sm">
              {connector.published ? copy.shared(connector.project) : copy.private}
            </p>
            {owner ? (
              <div className="flex flex-wrap gap-2">
                {setup?.connected && setup.oauth_connectors.includes(connector.name) && (
                  <button
                    type="button"
                    className={button}
                    disabled={locked}
                    onClick={() => void act("reconnect")}
                  >
                    {copy.useGoogleAccount}
                  </button>
                )}
                {connector.capabilities.includes("catalog") && (
                  <button
                    type="button"
                    className={primary}
                    disabled={locked}
                    onClick={() => void act("verify")}
                  >
                    {busy === "verify" ? copy.verifying : copy.verify}
                  </button>
                )}
                {connector.published ? (
                  <button
                    type="button"
                    className={button}
                    disabled={locked}
                    onClick={() => void act("withdraw")}
                  >
                    {copy.withdraw}
                  </button>
                ) : (
                  <button
                    type="button"
                    className={button}
                    disabled={locked || !connector.crawled_at || Boolean(checkFailed)}
                    onClick={() => void act("publish")}
                  >
                    {copy.publish}
                  </button>
                )}
              </div>
            ) : (
              <p className="text-aui-xs text-aui-muted">{copy.readOnly}</p>
            )}
            <p className="text-aui-xs text-aui-muted">{copy.publishHint}</p>
          </>
        )}
        <Problem message={actionError} />
        {notice && <output className="text-aui-sm">{notice}</output>}
      </div>
    </ConnectionSection>
  );
}
