import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { ApiError, type AgentSlackApp as App, api } from "./api.ts";
import { button, field, Label, Problem, primary, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.agentSlackApp;

/**
 * An agent's own Slack app (RFC 0012): create it in Slack from a manifest
 * made from the agent's look, install it, and connect it with its bot token
 * and signing secret. Slack says who the bot is, so nobody copies an id.
 * Once connected, mentioning the agent's own bot in any of the team's Slack
 * apps names the agent, and its own app answers.
 */
export function AgentSlackApp({
  name,
  canLink,
  seed,
}: {
  name: string;
  /** clients:write: may connect an app. Anyone who reads the agent sees whether it has one. */
  canLink: boolean;
  seed?: App;
}) {
  const [app, setApp] = useState<App | null>(seed ?? null);
  const [token, setToken] = useState("");
  const [signing, setSigning] = useState("");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);

  useEffect(() => {
    if (seed) return;
    let live = true;
    api
      .agentSlackApp(name)
      .then((a) => live && setApp(a))
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.failed));
    return () => {
      live = false;
    };
  }, [name, seed]);

  if (!app) return problem ? <Problem message={problem} /> : <SkeletonRows count={2} />;

  const connect = () => {
    setBusy(true);
    setProblem(null);
    api
      .linkAgentSlackApp(name, token.trim(), signing.trim())
      .then((a) => {
        setApp(a);
        setToken("");
        setSigning("");
      })
      .catch((e) => setProblem(e instanceof ApiError ? e.message : words.failed))
      .finally(() => setBusy(false));
  };

  return (
    <section
      className="flex flex-col gap-4 rounded-aui border border-aui-border p-4"
      data-testid="agent-slack-app"
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h3 className="font-semibold text-aui-sm">{words.title}</h3>
        <span
          className={`rounded-full px-2 py-0.5 text-aui-2xs ${app.client ? "bg-aui-ok/15 text-aui-ok" : "bg-aui-hover text-aui-muted"}`}
        >
          {app.client ? words.connected : words.notConnected}
        </span>
      </div>
      <p className="text-aui-xs text-aui-muted">
        {app.client ? words.connectedAs(app.client) : words.what}
      </p>
      {!app.client && canLink && (
        <ol className="flex flex-col gap-3 text-aui-sm">
          <li className="flex flex-col gap-1">
            <span>{words.step1}</span>
            <a
              className={`${button} self-start`}
              href={app.create_url}
              target="_blank"
              rel="noreferrer"
            >
              {words.create}
            </a>
          </li>
          <li>{words.step2}</li>
          <li className="flex flex-col gap-2">
            <span>{words.step3}</span>
            <div className="grid min-w-0 gap-2 sm:grid-cols-2">
              <Label text={words.token}>
                <input
                  id="agent-slack-token"
                  className={field}
                  type="password"
                  autoComplete="off"
                  value={token}
                  placeholder="xoxb-"
                  onChange={(e) => setToken(e.target.value)}
                />
              </Label>
              <Label text={words.signing}>
                <input
                  id="agent-slack-signing"
                  className={field}
                  type="password"
                  autoComplete="off"
                  value={signing}
                  onChange={(e) => setSigning(e.target.value)}
                />
              </Label>
            </div>
            <button
              type="button"
              className={`${primary} self-start`}
              disabled={busy || !token.trim().startsWith("xoxb-")}
              onClick={connect}
            >
              {words.connect}
            </button>
          </li>
        </ol>
      )}
      {!app.client && !canLink && <p className="text-aui-xs text-aui-muted">{words.askAdmin}</p>}
      <Problem message={problem} />
    </section>
  );
}
