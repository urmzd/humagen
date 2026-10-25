import { type FormEvent, useState } from "react";
import { ApiError, api, type ClientCreateInput, type ToolCredentialInput } from "./api.ts";
import { button, card, field, Label, primary, Problem } from "./ui.tsx";

/** Connect an installed Slack app or GitHub installation using the existing client contract. */
export function ClientConnect({ onCreated, onCancel }: { onCreated: () => void; onCancel: () => void }) {
  const [surface, setSurface] = useState<ClientCreateInput["surface"]>("slack");
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [team, setTeam] = useState("");
  const [owners, setOwners] = useState("");
  const [app, setApp] = useState("");
  const [installation, setInstallation] = useState("");
  const [login, setLogin] = useState("");
  const [agent, setAgent] = useState("");
  const [allowed, setAllowed] = useState("");
  const [token, setToken] = useState("");
  const [signing, setSigning] = useState("");
  const [tokenWay, setTokenWay] = useState("value");
  const [signingWay, setSigningWay] = useState("value");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const list = (text: string) => text.split(",").map((s) => s.trim()).filter(Boolean);
  const credential = (way: string, text: string): ToolCredentialInput => way === "reference" ? { ref: text.trim() } : { value: text };
  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (busy) return;
    setProblem(null);
    const agents = list(allowed);
    if (agent.trim() !== "" && !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(agent.trim())) {
      setProblem("The default must name one agent. Use letters, digits, underscores or dashes, starting with a letter or digit.");
      return;
    }
    if (agents.includes("*") || agents.some((a) => !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(a))) {
      setProblem("List individual agent names, separated by commas. This form does not grant access to every agent.");
      return;
    }
    if (surface === "github" && ((agent.trim() !== "" && login.trim() === "") || (login.trim() !== "" && !/^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$/.test(login.trim())))) {
      setProblem("Give the GitHub App's own login, such as your-app[bot], so mentioning it can reach the default agent.");
      return;
    }
    if (token.trim() === "" || signing.trim() === "") {
      setProblem("Give both a token and a signing secret.");
      return;
    }
    const input: ClientCreateInput = {
      id: id.trim(), name: name.trim(), surface,
      identity: surface === "slack"
        ? { team: team.trim(), ...(app.trim() && { app_id: app.trim() }) }
        : { owners: list(owners), ...(app.trim() && { app_id: app.trim() }), ...(installation.trim() && { installation: installation.trim() }) },
      default_agent: agent.trim(), agents: [...new Set(agents)], enabled: true,
      ...(surface === "github" && login.trim() !== "" ? { github: { login: login.trim() } } : {}),
      credentials: { token: credential(tokenWay, token), signing_secret: credential(signingWay, signing) },
    };
    setBusy(true);
    // Values leave the form as soon as they are submitted, also on a refusal.
    setToken("");
    setSigning("");
    try {
      await api.createClient(input);
      onCreated();
    } catch (error) {
      setProblem(error instanceof ApiError ? error.message : "The connection could not be created. Try again.");
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className={`${card} flex flex-col gap-3`} aria-label="Connect a system" onSubmit={submit}>
      <h2 className="text-aui-sm font-medium text-aui-fg">Connect a system</h2>
      <p className="text-aui-sm text-aui-muted">Install your Slack app or GitHub App first, then provide its credentials here. Saving configures the connection; a real reply verifies it works.</p>
      <Problem message={problem} />
      <fieldset disabled={busy} className="flex min-w-0 flex-col gap-3">
        <Label text="System">
          <select className={field} value={surface} onChange={(e) => {
            setSurface(e.target.value as ClientCreateInput["surface"]);
            setApp(""); setLogin(""); setToken(""); setSigning(""); setProblem(null);
          }}><option value="slack">Slack</option><option value="github">GitHub</option></select>
        </Label>
        <Label text="Connection ID" hint="A unique name of up to 40 lowercase letters, digits and dashes.">
          <input className={field} required maxLength={40} pattern={"[a-z0-9][a-z0-9\\-]{0,39}"} value={id} onChange={(e) => setId(e.target.value)} />
        </Label>
        <Label text="Name"><input className={field} required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} /></Label>
        {surface === "slack" ? (
          <Label text="Slack team ID"><input className={field} required pattern="[A-Z][A-Z0-9]{2,31}" placeholder="T0123ABC" value={team} onChange={(e) => setTeam(e.target.value)} /></Label>
        ) : (
          <>
            <Label text="GitHub accounts" hint="Account or organization names, separated by commas."><input className={field} required value={owners} onChange={(e) => setOwners(e.target.value)} /></Label>
            <Label text="Installation ID (optional)"><input className={field} inputMode="numeric" pattern="[0-9]+" value={installation} onChange={(e) => setInstallation(e.target.value)} /></Label>
            <Label text="GitHub bot login" hint="The app's own GitHub login, such as your-app[bot]. Required with a default agent so native @mentions can reach it.">
              <input className={field} required={agent.trim() !== ""} value={login} onChange={(e) => setLogin(e.target.value)} placeholder="your-app[bot]" />
            </Label>
          </>
        )}
        <Label text="App ID (optional)"><input className={field} value={app} onChange={(e) => setApp(e.target.value)} /></Label>
        <Label text="Default agent (optional)" hint="Answers messages that name no agent. With no default and no allowed agents, nothing can run.">
          <input className={field} pattern={"[A-Za-z0-9][A-Za-z0-9_\\-]{0,63}"} value={agent} onChange={(e) => setAgent(e.target.value)} />
        </Label>
        <Label text="Other allowed agents (optional)" hint="Individual agent names, separated by commas. No access is granted by default.">
          <input className={field} value={allowed} onChange={(e) => setAllowed(e.target.value)} />
        </Label>
        <p className="text-aui-xs text-aui-muted">Credential values are stored by the server and never shown again. An environment reference must be permitted by your workspace operator. GitHub needs a current installation token; it is not refreshed automatically.</p>
        <Label text="Token source"><select className={field} value={tokenWay} onChange={(e) => { setTokenWay(e.target.value); setToken(""); }}><option value="value">Value</option><option value="reference">Environment reference</option></select></Label>
        <Label text={tokenWay === "value" ? "Token" : "Token reference"}>
          <input className={field} required type={tokenWay === "value" ? "password" : "text"} autoComplete="off" spellCheck={false} pattern={tokenWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined} placeholder={tokenWay === "reference" ? "env:HUMAGEN_SLACK_TOKEN" : undefined} value={token} onChange={(e) => setToken(e.target.value)} />
        </Label>
        <Label text="Signing secret source"><select className={field} value={signingWay} onChange={(e) => { setSigningWay(e.target.value); setSigning(""); }}><option value="value">Value</option><option value="reference">Environment reference</option></select></Label>
        <Label text={signingWay === "value" ? "Signing secret" : "Signing secret reference"}>
          <input className={field} required type={signingWay === "value" ? "password" : "text"} autoComplete="off" spellCheck={false} pattern={signingWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined} placeholder={signingWay === "reference" ? "env:HUMAGEN_SLACK_SIGNING_SECRET" : undefined} value={signing} onChange={(e) => setSigning(e.target.value)} />
        </Label>
        <p className="text-aui-xs text-aui-muted">Your app must send signed events to the server's integration listener at {surface === "slack" ? "/connectors/slack" : "/connectors/github"}. Your workspace operator provides its public address.</p>
        <div className="flex flex-wrap gap-2"><button type="submit" className={primary}>{busy ? "Connecting…" : "Connect"}</button><button type="button" className={button} onClick={onCancel}>Cancel</button></div>
      </fieldset>
    </form>
  );
}
