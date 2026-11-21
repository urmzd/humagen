import { defaultCopy } from "@humagen/components";
import { LayersIcon, PlusIcon } from "@humagen/render-web";
import { type FormEvent, useCallback, useEffect, useState } from "react";
import { ApiError, call } from "./api.ts";
import { Pill, type Tone } from "./cards.tsx";
import { forgetSessionOptions } from "./sessionOptions.ts";
import { button, field, Label, Problem, primary } from "./ui.tsx";

const words = defaultCopy.images;

/** An image a workspace added for its sessions to run in (`GET /api/images`). */
export interface WorkspaceImage {
  id: string;
  name: string;
  ref: string;
  digest?: string;
  status: "checking" | "ready" | "failed";
  problem?: string;
  warm?: boolean;
  credential?: boolean;
  createdBy: string;
  createdAt: string;
  checkedAt?: string;
}

interface WorkspaceImages {
  images: WorkspaceImage[];
  registries: string[];
}

/** The calls this page makes, so a test can stand in for the server. */
export interface ImagesApi {
  list: () => Promise<WorkspaceImages>;
  add: (input: {
    name: string;
    ref: string;
    credential?: string;
    warm?: boolean;
  }) => Promise<WorkspaceImage>;
  change: (id: string, change: { warm?: boolean; check?: boolean }) => Promise<WorkspaceImage>;
  remove: (id: string) => Promise<void>;
  /** The deployment's own image, as the session options name it. */
  defaultRef: () => Promise<string>;
}

export const imagesApi: ImagesApi = {
  list: () => call<WorkspaceImages>("GET", "/api/images"),
  add: (input) => call<WorkspaceImage>("POST", "/api/images", input),
  change: (id, change) =>
    call<WorkspaceImage>("PATCH", `/api/images/${encodeURIComponent(id)}`, change),
  remove: (id) => call<void>("DELETE", `/api/images/${encodeURIComponent(id)}`),
  defaultRef: () =>
    call<{ images: { default: boolean; ref: string }[] }>("GET", "/api/sessions/options").then(
      (o) => o.images.find((i) => i.default)?.ref ?? "",
    ),
};

const tone: Record<WorkspaceImage["status"], Tone> = {
  checking: "neutral",
  ready: "ok",
  failed: "bad",
};

const said = (e: unknown, fallback: string) => (e instanceof ApiError ? e.message : fallback);

/** A digest in a few characters: sha256:0f3a9c1d2b4e. */
export function shortDigest(digest: string | undefined): string {
  const hex = digest?.match(/@sha256:([0-9a-f]{12})/)?.[1];
  return hex ? `sha256:${hex}` : "";
}

/** How often a checking image is read again. */
const POLL_MS = 2000;

/**
 * Settings → Images: the deployment's default, which everyone has, then the
 * workspace's own, each checked when added. Someone who may change clients
 * adds, keeps warm, checks again and removes; anyone else reads.
 */
export function ImageSettings({
  canEdit,
  api = imagesApi,
  pollMs = POLL_MS,
}: {
  canEdit: boolean;
  api?: ImagesApi;
  pollMs?: number;
}) {
  const [data, setData] = useState<WorkspaceImages | null>(null);
  const [defaultRef, setDefaultRef] = useState("");
  const [problem, setProblem] = useState<string | null>(null);
  const [adding, setAdding] = useState(false);
  const [confirming, setConfirming] = useState<string | null>(null);

  const load = useCallback(() => {
    api
      .list()
      .then((d) => {
        setData(d);
        setProblem(null);
      })
      .catch((e) => setProblem(said(e, "The images could not be read.")));
  }, [api]);
  useEffect(load, [load]);
  useEffect(() => {
    api.defaultRef().then(setDefaultRef, () => setDefaultRef(""));
  }, [api]);

  const checking = data?.images.some((i) => i.status === "checking") ?? false;
  useEffect(() => {
    if (!checking) return;
    const t = setInterval(load, pollMs);
    return () => clearInterval(t);
  }, [checking, load, pollMs]);

  const changed = () => {
    forgetSessionOptions();
    load();
  };
  const act = (p: Promise<unknown>, fallback: string) =>
    p.then(changed).catch((e) => setProblem(said(e, fallback)));

  if (data === null) return <Problem message={problem} />;
  const registries = data.registries;
  const row = "flex min-w-0 flex-wrap items-center gap-3 px-3 py-2.5";

  return (
    <div className="flex min-w-0 flex-col gap-3" data-testid="image-settings">
      <Problem message={problem} />
      <ul
        aria-label={words.title}
        className="flex min-w-0 flex-col divide-y divide-aui-border rounded-aui-lg border border-aui-border"
      >
        <li className={row} data-image="default">
          <LayersIcon size={16} className="shrink-0 text-aui-muted" />
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="truncate font-medium text-aui-fg text-aui-sm">{words.defaultRow}</span>
            <span className="truncate text-aui-muted text-aui-xs">
              {words.defaultSub(defaultRef || "…")}
            </span>
          </div>
          <Pill tone="ok">{words.status.ready}</Pill>
        </li>
        {data.images.map((img) => (
          <li key={img.id} className={row} data-image={img.id} data-status={img.status}>
            <LayersIcon size={16} className="shrink-0 text-aui-muted" />
            <div className="flex min-w-0 flex-1 flex-col">
              <span className="truncate font-medium text-aui-fg text-aui-sm">{img.name}</span>
              <span className="truncate text-aui-muted text-aui-xs">{img.ref}</span>
              {shortDigest(img.digest) && (
                <span className="truncate font-mono text-aui-2xs text-aui-muted">
                  {words.pinned(shortDigest(img.digest))}
                </span>
              )}
              {img.status === "failed" && img.problem && (
                <span className="break-words text-aui-bad text-aui-xs" data-testid="image-problem">
                  {img.problem}
                </span>
              )}
            </div>
            <Pill tone={tone[img.status]}>{words.status[img.status]}</Pill>
            {canEdit &&
              (confirming === img.id ? (
                <span className="flex items-center gap-2">
                  <button type="button" className={button} onClick={() => setConfirming(null)}>
                    Keep
                  </button>
                  <button
                    type="button"
                    className={primary}
                    onClick={() => {
                      setConfirming(null);
                      act(api.remove(img.id), "The image could not be removed.");
                    }}
                  >
                    {words.remove} {img.name}
                  </button>
                </span>
              ) : (
                <span className="flex flex-wrap items-center gap-1">
                  <label className="inline-flex min-h-9 items-center gap-1.5 px-2 text-aui-xs text-aui-muted">
                    <input
                      type="checkbox"
                      checked={!!img.warm}
                      onChange={(e) =>
                        act(
                          api.change(img.id, { warm: e.currentTarget.checked }),
                          "The image could not be changed.",
                        )
                      }
                    />
                    {words.warm}
                  </label>
                  <button
                    type="button"
                    className={button}
                    disabled={img.status === "checking"}
                    onClick={() =>
                      act(api.change(img.id, { check: true }), "The image could not be checked.")
                    }
                  >
                    {words.checkAgain}
                  </button>
                  <button type="button" className={button} onClick={() => setConfirming(img.id)}>
                    {words.remove}
                  </button>
                </span>
              ))}
          </li>
        ))}
      </ul>
      {registries.length === 0 ? (
        <p className="text-aui-muted text-aui-sm" data-testid="images-off">
          {words.off}
        </p>
      ) : (
        <>
          {data.images.length === 0 && !adding && (
            <p className="text-aui-muted text-aui-sm">{words.empty}</p>
          )}
          {canEdit &&
            (adding ? (
              <AddImage
                registries={registries}
                onCancel={() => setAdding(false)}
                onAdd={(input) =>
                  api.add(input).then(() => {
                    setAdding(false);
                    changed();
                  })
                }
              />
            ) : (
              <div>
                <button type="button" className={button} onClick={() => setAdding(true)}>
                  <PlusIcon size={16} />
                  {words.add}
                </button>
              </div>
            ))}
        </>
      )}
    </div>
  );
}

function AddImage({
  registries,
  onAdd,
  onCancel,
}: {
  registries: string[];
  onAdd: (input: {
    name: string;
    ref: string;
    credential?: string;
    warm?: boolean;
  }) => Promise<void>;
  onCancel: () => void;
}) {
  const [name, setName] = useState("");
  const [ref, setRef] = useState("");
  const [credential, setCredential] = useState("");
  const [warm, setWarm] = useState(false);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const submit = (e: FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setProblem(null);
    onAdd({
      name: name.trim(),
      ref: ref.trim(),
      ...(credential ? { credential } : {}),
      ...(warm ? { warm } : {}),
    })
      .catch((err) => setProblem(said(err, "The image could not be added.")))
      .finally(() => setBusy(false));
  };
  return (
    <form
      onSubmit={submit}
      aria-label={words.add}
      className="flex min-w-0 flex-col gap-3 rounded-aui-lg border border-aui-border p-3"
    >
      <Label text={words.name}>
        <input
          className={field}
          value={name}
          required
          maxLength={60}
          placeholder={words.namePlaceholder}
          onChange={(e) => setName(e.currentTarget.value)}
        />
      </Label>
      <Label text={words.ref} hint={words.refHint(registries.join(", "))}>
        <input
          className={field}
          value={ref}
          required
          maxLength={512}
          spellCheck={false}
          autoCapitalize="off"
          placeholder={`${registries[0] ?? "ghcr.io"}/acme/agent:1.4`}
          onChange={(e) => setRef(e.currentTarget.value)}
        />
      </Label>
      <Label text={words.credential} hint={words.credentialHint}>
        <input
          className={field}
          type="password"
          autoComplete="off"
          value={credential}
          onChange={(e) => setCredential(e.currentTarget.value)}
        />
      </Label>
      <label className="flex items-start gap-2 text-aui-sm">
        <input
          type="checkbox"
          className="mt-1"
          checked={warm}
          onChange={(e) => setWarm(e.currentTarget.checked)}
        />
        <span className="flex flex-col">
          {words.warm}
          <span className="text-aui-2xs text-aui-muted">{words.warmHint}</span>
        </span>
      </label>
      <Problem message={problem} />
      <div className="flex gap-2">
        <button type="submit" className={primary} disabled={busy || !name.trim() || !ref.trim()}>
          {words.add}
        </button>
        <button type="button" className={button} onClick={onCancel}>
          Cancel
        </button>
      </div>
    </form>
  );
}
