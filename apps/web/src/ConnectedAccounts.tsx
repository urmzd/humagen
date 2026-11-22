import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { ConnectionLogo } from "./ConnectionLogo.tsx";
import { type ConnectionPreset, connectionApi, type LinkedAccount } from "./connectionApi.ts";
import { SplitButton } from "./SplitButton.tsx";
import { Problem } from "./ui.tsx";

const words = defaultCopy.plugins.accounts;

/** The surface an account preset links on, for its logo. */
const surfaceOf = (id: string) => id.replace(/-account$/, "");

/**
 * Who you are on GitHub and on Slack: one row per surface, with its one
 * control. Not linked, the control is Connect; linked, the row shows the
 * account's login and picture, and the control is Disconnect. A surface the
 * deployment has not set up for linking, and that you have not linked, is
 * not shown.
 */
export function ConnectedAccounts({
  presets,
  busy = false,
  canConnect = true,
  onConnect,
  onDisconnect,
}: {
  presets: ConnectionPreset[];
  busy?: boolean;
  canConnect?: boolean;
  onConnect: (preset: ConnectionPreset) => void;
  onDisconnect: (preset: ConnectionPreset) => void;
}) {
  const shown = presets.filter((p) => p.kind === "account_oauth" && (p.available || p.connected));
  if (shown.length === 0) return null;
  return (
    <section aria-labelledby="connected-accounts" className="flex min-w-0 flex-col gap-1">
      <h2 id="connected-accounts" className="text-aui-base font-medium text-aui-fg">
        {words.title}
      </h2>
      <span className="pb-1 text-aui-muted text-aui-xs">{words.lede}</span>
      <ul aria-label={words.title} className="grid min-w-0 grid-cols-1">
        {shown.map((p) => {
          const account = p.connected ? p.account : undefined;
          const login = account
            ? account.surface === "github"
              ? `@${account.login}`
              : account.login
            : undefined;
          return (
            <li
              key={p.id}
              aria-label={p.name}
              className="flex min-h-16 min-w-0 flex-wrap items-center gap-x-3 gap-y-1 border-aui-border border-b py-3"
            >
              <span className="flex size-9 shrink-0 items-center justify-center rounded-aui-lg border border-aui-border bg-aui-surface">
                <ConnectionLogo id={surfaceOf(p.id)} name={p.name} />
              </span>
              <span className="flex min-w-28 flex-1 flex-col justify-center">
                <span className="w-full min-w-0 truncate font-medium text-aui-sm text-aui-fg">
                  {p.name}
                </span>
                <span className="flex w-full min-w-0 items-center gap-1.5 text-aui-muted text-aui-xs">
                  {account?.avatar_url && (
                    <img
                      src={account.avatar_url}
                      alt=""
                      width={16}
                      height={16}
                      className="size-4 shrink-0 rounded-full"
                      referrerPolicy="no-referrer"
                    />
                  )}
                  <span className="min-w-0 truncate">{login ?? words.notConnected}</span>
                </span>
              </span>
              {canConnect && (
                <span className="ml-auto flex shrink-0 items-center">
                  {account ? (
                    <SplitButton
                      label={words.disconnect}
                      name={`${words.disconnect} ${p.name}`}
                      menuLabel={`More for ${p.name}`}
                      disabled={busy}
                      onClick={() => onDisconnect(p)}
                    />
                  ) : (
                    <SplitButton
                      label={words.connect}
                      name={`${words.connect} ${p.name}`}
                      menuLabel={`More for ${p.name}`}
                      disabled={busy || !p.available}
                      onClick={() => onConnect(p)}
                    />
                  )}
                </span>
              )}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/**
 * The people who linked an account on a client's surface, for its
 * administrators: the account, the person it acts for, and Disconnect for
 * someone who left or whose account was taken. Nothing is shown until
 * someone has linked one.
 */
export function LinkedPeople({
  client,
  canUnlink = false,
}: {
  client: string;
  /** May unlink, which the server holds to clients:write. */
  canUnlink?: boolean;
}) {
  const [linked, setLinked] = useState<LinkedAccount[]>([]);
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const [revision, setRevision] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: revision reloads after an unlink
  useEffect(() => {
    let live = true;
    connectionApi
      .linkedAccounts(client)
      .then((a) => live && setLinked(a))
      .catch(() => live && setLinked([]));
    return () => {
      live = false;
    };
  }, [client, revision]);
  const unlink = (a: LinkedAccount) => {
    if (busy) return;
    setBusy(true);
    setProblem(null);
    connectionApi
      .unlinkAccount(client, a.id)
      .then(() => setRevision((v) => v + 1))
      .catch((e: unknown) => setProblem(e instanceof Error ? e.message : words.unlinkFailed))
      .finally(() => setBusy(false));
  };
  if (linked.length === 0) return null;
  const shown = (a: LinkedAccount) => (a.surface === "github" ? `@${a.login}` : a.login);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <h4 className="font-medium text-aui-sm text-aui-fg">{words.linkedPeople}</h4>
      <Problem message={problem} />
      <ul aria-label={words.linkedPeople} className="flex min-w-0 flex-col gap-1">
        {linked.map((a) => (
          <li
            key={`${a.team ?? ""}/${a.id}`}
            aria-label={shown(a)}
            className="flex min-h-9 min-w-0 items-center gap-2 text-aui-sm"
          >
            {a.avatar_url && (
              <img
                src={a.avatar_url}
                alt=""
                width={16}
                height={16}
                className="size-4 shrink-0 rounded-full"
                referrerPolicy="no-referrer"
              />
            )}
            <span className="min-w-0 truncate text-aui-fg">{shown(a)}</span>
            <span className="min-w-0 flex-1 truncate text-aui-muted text-aui-xs">{a.subject}</span>
            {canUnlink && (
              <SplitButton
                label={words.disconnect}
                name={`${words.disconnect} ${shown(a)}`}
                menuLabel={`More for ${shown(a)}`}
                disabled={busy}
                onClick={() => unlink(a)}
              />
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}
