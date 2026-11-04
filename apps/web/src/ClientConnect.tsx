import { defaultCopy } from "@humagen/components";
import { type FormEvent, useState } from "react";
import { ApiError, api, type ClientCreateInput, type ToolCredentialInput } from "./api.ts";
import { button, card, field, Label, Problem, primary } from "./ui.tsx";

const words = defaultCopy.clientConnect;

/** Connect an installed Slack app, GitHub installation, WhatsApp number or iMessage server using the existing client contract. */
export function ClientConnect({
  onCreated,
  onCancel,
}: {
  onCreated: () => void;
  onCancel: () => void;
}) {
  const [surface, setSurface] = useState<ClientCreateInput["surface"]>("slack");
  const [id, setId] = useState("");
  const [name, setName] = useState("");
  const [team, setTeam] = useState("");
  const [owners, setOwners] = useState("");
  const [app, setApp] = useState("");
  const [installation, setInstallation] = useState("");
  const [phoneNumberId, setPhoneNumberId] = useState("");
  const [server, setServer] = useState("");
  const [serverUrl, setServerUrl] = useState("");
  const [login, setLogin] = useState("");
  const [agent, setAgent] = useState("");
  const [allowed, setAllowed] = useState("");
  const [people, setPeople] = useState("");
  const [token, setToken] = useState("");
  const [signing, setSigning] = useState("");
  const [verify, setVerify] = useState("");
  const [tokenWay, setTokenWay] = useState("value");
  const [signingWay, setSigningWay] = useState("value");
  const [verifyWay, setVerifyWay] = useState("value");
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const list = (text: string) =>
    text
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean);
  const parsePeople = (text: string): Record<string, string> => {
    const out: Record<string, string> = {};
    for (const entry of text.split(/[\n,]+/)) {
      const trimmed = entry.trim();
      if (!trimmed) continue;
      const eq = trimmed.indexOf("=");
      if (eq <= 0) continue;
      const ext = trimmed.slice(0, eq).trim();
      const subj = trimmed.slice(eq + 1).trim();
      if (ext && subj) out[ext] = subj;
    }
    return out;
  };
  // What the two secrets are called on the surface being connected.
  const secretWords =
    surface === "whatsapp"
      ? {
          token: words.whatsappToken,
          tokenPlaceholder: words.whatsappTokenPlaceholder,
          signing: words.whatsappSigning,
          signingPlaceholder: words.whatsappSigningPlaceholder,
        }
      : surface === "imessage"
        ? {
            token: words.imessageToken,
            tokenPlaceholder: words.imessageTokenPlaceholder,
            signing: words.imessageSigning,
            signingPlaceholder: words.imessageSigningPlaceholder,
          }
        : words;
  const credential = (way: string, text: string): ToolCredentialInput =>
    way === "reference" ? { ref: text.trim() } : { value: text };
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
    if (
      surface === "github" &&
      ((agent.trim() !== "" && login.trim() === "") ||
        (login.trim() !== "" && !/^[A-Za-z0-9][A-Za-z0-9-]{0,38}(\[bot\])?$/.test(login.trim())))
    ) {
      setProblem(words.invalidGithubLogin);
      return;
    }
    if (token.trim() === "" || signing.trim() === "") {
      setProblem(words.credentialsRequired);
      return;
    }
    if (surface === "whatsapp" && verify.trim() === "") {
      setProblem(words.verifyRequired);
      return;
    }
    const mappedPeople = parsePeople(people);
    const hasPeople = Object.keys(mappedPeople).length > 0;
    const identity: ClientCreateInput["identity"] =
      surface === "slack"
        ? { team: team.trim(), ...(app.trim() && { app_id: app.trim() }) }
        : surface === "whatsapp"
          ? { phone_number_id: phoneNumberId.trim() }
          : surface === "imessage"
            ? { server: server.trim() }
            : {
                owners: list(owners),
                ...(app.trim() && { app_id: app.trim() }),
                ...(installation.trim() && { installation: installation.trim() }),
              };
    const input: ClientCreateInput = {
      id: id.trim(),
      name: name.trim(),
      surface,
      identity,
      default_agent: agent.trim(),
      agents: [...new Set(agents)],
      enabled: true,
      ...(surface === "slack" && hasPeople ? { slack: { people: mappedPeople } } : {}),
      ...(surface === "github" && (login.trim() !== "" || hasPeople)
        ? {
            github: {
              ...(login.trim() !== "" ? { login: login.trim() } : {}),
              ...(hasPeople ? { people: mappedPeople } : {}),
            },
          }
        : {}),
      ...(surface === "whatsapp" && hasPeople ? { whatsapp: { people: mappedPeople } } : {}),
      ...(surface === "imessage"
        ? { imessage: { url: serverUrl.trim(), ...(hasPeople ? { people: mappedPeople } : {}) } }
        : {}),
      credentials: {
        token: credential(tokenWay, token),
        signing_secret: credential(signingWay, signing),
        ...(surface === "whatsapp" ? { verify_token: credential(verifyWay, verify) } : {}),
      },
    };
    setBusy(true);
    // Values leave the form as soon as they are submitted, also on a refusal.
    setToken("");
    setSigning("");
    setVerify("");
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
          <select
            className={field}
            value={surface}
            onChange={(e) => {
              setSurface(e.target.value as ClientCreateInput["surface"]);
              setApp("");
              setLogin("");
              setToken("");
              setSigning("");
              setVerify("");
              setProblem(null);
            }}
          >
            <option value="slack">{words.slack}</option>
            <option value="github">{words.github}</option>
            <option value="whatsapp">{words.whatsapp}</option>
            <option value="imessage">{words.imessage}</option>
          </select>
        </Label>
        <Label text={words.id} hint={words.idHint}>
          <input
            className={field}
            required
            maxLength={40}
            pattern={"[a-z0-9][a-z0-9\\-]{0,39}"}
            value={id}
            onChange={(e) => setId(e.target.value)}
          />
        </Label>
        <Label text={words.name}>
          <input
            className={field}
            required
            maxLength={80}
            value={name}
            onChange={(e) => setName(e.target.value)}
          />
        </Label>
        {surface === "whatsapp" ? (
          <Label text={words.phoneNumberId} hint={words.phoneNumberIdHint}>
            <input
              className={field}
              required
              inputMode="numeric"
              pattern="[0-9]{1,20}"
              value={phoneNumberId}
              onChange={(e) => setPhoneNumberId(e.target.value)}
            />
          </Label>
        ) : surface === "imessage" ? (
          <>
            <Label text={words.server} hint={words.serverHint}>
              <input
                className={field}
                required
                maxLength={40}
                pattern={"[a-z0-9][a-z0-9\\-]{0,39}"}
                placeholder="home"
                value={server}
                onChange={(e) => setServer(e.target.value)}
              />
            </Label>
            <Label text={words.serverUrl} hint={words.serverUrlHint}>
              <input
                className={field}
                required
                type="url"
                placeholder="https://bb.example.com"
                value={serverUrl}
                onChange={(e) => setServerUrl(e.target.value)}
              />
            </Label>
          </>
        ) : surface === "slack" ? (
          <Label text={words.slackTeam}>
            <input
              className={field}
              required
              pattern="[A-Z][A-Z0-9]{2,31}"
              placeholder={words.slackTeamPlaceholder}
              value={team}
              onChange={(e) => setTeam(e.target.value)}
            />
          </Label>
        ) : (
          <>
            <Label text={words.githubAccounts} hint={words.githubAccountsHint}>
              <input
                className={field}
                required
                value={owners}
                onChange={(e) => setOwners(e.target.value)}
              />
            </Label>
            <Label text={words.installation}>
              <input
                className={field}
                inputMode="numeric"
                pattern="[0-9]+"
                value={installation}
                onChange={(e) => setInstallation(e.target.value)}
              />
            </Label>
            <Label text={words.botLogin} hint={words.botLoginHint}>
              <input
                className={field}
                required={agent.trim() !== ""}
                value={login}
                onChange={(e) => setLogin(e.target.value)}
                placeholder={words.botLoginPlaceholder}
              />
            </Label>
          </>
        )}
        {(surface === "slack" || surface === "github") && (
          <Label text={words.app}>
            <input className={field} value={app} onChange={(e) => setApp(e.target.value)} />
          </Label>
        )}
        <Label text={words.defaultAgent} hint={words.defaultAgentHint}>
          <input
            className={field}
            pattern={"[A-Za-z0-9][A-Za-z0-9_\\-]{0,63}"}
            value={agent}
            onChange={(e) => setAgent(e.target.value)}
          />
        </Label>
        <Label text={words.allowedAgents} hint={words.allowedAgentsHint}>
          <input className={field} value={allowed} onChange={(e) => setAllowed(e.target.value)} />
        </Label>
        <Label text={words.people} hint={words.peopleHint(surface)}>
          <textarea
            className={field}
            rows={2}
            placeholder={words.peoplePlaceholder(surface)}
            value={people}
            onChange={(e) => setPeople(e.target.value)}
          />
        </Label>
        <p className="text-aui-xs text-aui-muted">{words.credentialHint}</p>
        <Label text={words.tokenSource}>
          <select
            className={field}
            value={tokenWay}
            onChange={(e) => {
              setTokenWay(e.target.value);
              setToken("");
            }}
          >
            <option value="value">{words.value}</option>
            <option value="reference">{words.reference}</option>
          </select>
        </Label>
        <Label text={tokenWay === "value" ? secretWords.token : words.tokenReference}>
          <input
            className={field}
            required
            type={tokenWay === "value" ? "password" : "text"}
            autoComplete="off"
            spellCheck={false}
            pattern={tokenWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined}
            placeholder={tokenWay === "reference" ? secretWords.tokenPlaceholder : undefined}
            value={token}
            onChange={(e) => setToken(e.target.value)}
          />
        </Label>
        <Label text={words.signingSource}>
          <select
            className={field}
            value={signingWay}
            onChange={(e) => {
              setSigningWay(e.target.value);
              setSigning("");
            }}
          >
            <option value="value">{words.value}</option>
            <option value="reference">{words.reference}</option>
          </select>
        </Label>
        <Label text={signingWay === "value" ? secretWords.signing : words.signingReference}>
          <input
            className={field}
            required
            type={signingWay === "value" ? "password" : "text"}
            autoComplete="off"
            spellCheck={false}
            pattern={signingWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined}
            placeholder={signingWay === "reference" ? secretWords.signingPlaceholder : undefined}
            value={signing}
            onChange={(e) => setSigning(e.target.value)}
          />
        </Label>
        {surface === "whatsapp" && (
          <>
            <Label text={words.verifySource}>
              <select
                className={field}
                value={verifyWay}
                onChange={(e) => {
                  setVerifyWay(e.target.value);
                  setVerify("");
                }}
              >
                <option value="value">{words.value}</option>
                <option value="reference">{words.reference}</option>
              </select>
            </Label>
            <Label text={verifyWay === "value" ? words.verify : words.verifyReference}>
              <input
                className={field}
                required
                type={verifyWay === "value" ? "password" : "text"}
                autoComplete="off"
                spellCheck={false}
                pattern={verifyWay === "reference" ? "env:HUMAGEN_[A-Z0-9_]+" : undefined}
                placeholder={verifyWay === "reference" ? words.verifyPlaceholder : undefined}
                value={verify}
                onChange={(e) => setVerify(e.target.value)}
              />
            </Label>
          </>
        )}
        <p className="text-aui-xs text-aui-muted">{words.webhook(surface)}</p>
        <div className="flex flex-wrap gap-2">
          <button type="submit" className={primary}>
            {busy ? words.connecting : words.connect}
          </button>
          <button type="button" className={button} onClick={onCancel}>
            {defaultCopy.cancel}
          </button>
        </div>
      </fieldset>
    </form>
  );
}
