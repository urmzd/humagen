import { defaultCopy } from "@humagen/components";
import { type FormEvent, useState } from "react";
import { ApiError, api, type ClientCreateInput, type ToolCredentialInput } from "./api.ts";
import { button, card, field, Label, primary, Problem } from "./ui.tsx";

const words = defaultCopy.clientConnect;

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
      setProblem(words.invalidDefaultAgent);
      return;
    }
    if (agents.includes("*") || agents.some((a) => !/^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$/.test(a))) {
      setProblem(words.invalidAgents);
      return;
    }
    if (surface === "github" && ((agent.trim() !== "" && login.trim() === "") || (login.trim() !== "" && !/^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$/.test(login.trim())))) {
      setProblem(words.invalidGithubLogin);
      return;
    }
    if (token.trim() === "" || signing.trim() === "") {
      setProblem(words.credentialsRequired);
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
      setProblem(error instanceof ApiError ? error.message : words.failed);
    } finally {
      setBusy(false);
    }
  };
  return (
    <form className={`${card} flex flex-col gap-3`} aria-label={words.heading} onSubmit={submit}>
      <h2 className="text-aui-sm font-medium text-aui-fg">{words.heading}</h2>
      <p className="text-aui-sm text-aui-muted">{words.intro}</p>
      <Problem message={problem} />
      <fieldset disabled={busy} className="flex min-w-0 flex-col gap-3">
        <Label text={words.system}>
          <select className={field} value={surface} onChange={(e) => {
            setSurface(e.target.value as ClientCreateInput["surface"]);
            setApp(""); setLogin(""); setToken(""); setSigning(""); setProblem(null);
          }}><option value="slack">{words.slack}</option><option value="github">{words.github}</option></select>
        </Label>
        <Label text={words.id} hint={words.idHint}>
          <input className={field} required maxLength={40} pattern={"[a-z0-9][a-z0-9\\-]{0,39}"} value={id} onChange={(e) => setId(e.target.value)} />
        </Label>
        <Label text={words.name}><input className={field} required maxLength={80} value={name} onChange={(e) => setName(e.target.value)} /></Label>
        {surface === "slack" ? (
          <Label text={words.slackTeam}><input className={field} required pattern="[A-Z][A-Z0-9]{2,31}" placeholder={words.slackTeamPlaceholder} value={team} onChange={(e) => setTeam(e.target.value)} /></Label>
        ) : (
          <>
            <Label text={words.githubAccounts} hint={words.githubAccountsHint}><input className={field} required value={owners} onChange={(e) => setOwners(e.target.value)} /></Label>
            <Label text={words.installation}><input className={field} inputMode="numeric" pattern="[0-9]+" value={installation} onChange={(e) => setInstallation(e.target.value)} /></Label>
            <Label text={words.botLogin} hint={words.botLoginHint}>
              <input className={field} required={agent.trim() !== ""} value={login} onChange={(e) => setLogin(e.target.value)} placeholder={words.botLoginPlaceholder} />
            </Label>
          </>
        )}
        <Label text={words.app}><input className={field} value={app} onChange={(e) => setApp(e.target.value)} /></Label>
        <Label text={words.defaultAgent} hint={words.defaultAgentHint}>
          <input className={field} pattern={"[A-Za-z0-9][A-Za-z0-9_\\-]{0,63}"} value={agent} onChange={(e) => setAgent(e.target.value)} />
        </Label>
        <Label text={words.allowedAgents} hint={words.allowedAgentsHint}>
          <input className={field} value={allowed} onChange={(e) => setAllowed(e.target.value)} />
        </Label>
        <p className="text-aui-xs text-aui-muted">{words.credentialHint}</p>
        <Label text={words.tokenSource}><select className={field} value={tokenWay} onChange={(e) => { setTokenWay(e.target.value); setToken(""); }}><option value="value">{words.value}</option><option value="reference">{words.reference}</option></select></Label>
        <Label text={tokenWay === "value" ? words.token : words.tokenReference}>
          <input className={field} required type={tokenWay === "value" ? "password" : "text"} autoComplete="off" spellCheck={false} pattern={tokenWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined} placeholder={tokenWay === "reference" ? words.tokenPlaceholder : undefined} value={token} onChange={(e) => setToken(e.target.value)} />
        </Label>
        <Label text={words.signingSource}><select className={field} value={signingWay} onChange={(e) => { setSigningWay(e.target.value); setSigning(""); }}><option value="value">{words.value}</option><option value="reference">{words.reference}</option></select></Label>
        <Label text={signingWay === "value" ? words.signing : words.signingReference}>
          <input className={field} required type={signingWay === "value" ? "password" : "text"} autoComplete="off" spellCheck={false} pattern={signingWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined} placeholder={signingWay === "reference" ? words.signingPlaceholder : undefined} value={signing} onChange={(e) => setSigning(e.target.value)} />
        </Label>
        <p className="text-aui-xs text-aui-muted">{words.webhook(surface)}</p>
        <div className="flex flex-wrap gap-2"><button type="submit" className={primary}>{busy ? words.connecting : words.connect}</button><button type="button" className={button} onClick={onCancel}>{defaultCopy.cancel}</button></div>
      </fieldset>
    </form>
  );
}
