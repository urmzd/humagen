import { defaultCopy } from "@humagen/components";
import { MicIcon } from "@humagen/render-web";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { type FormEvent, useCallback, useEffect, useRef, useState } from "react";
import {
  ApiError,
  api,
  type VoiceCheck,
  type VoiceKind,
  type VoiceKindName,
  type VoicePrice,
  type VoiceProvider,
  type VoiceProviderInput,
  type VoiceProviderView,
} from "./api.ts";
import { Pill } from "./cards.tsx";
import { button, card, field, Label, Problem, primary } from "./ui.tsx";
import { forgetVoiceSettings } from "./useVoiceNotes.ts";
import { dollarsToMicros, microsToDollars } from "./voicePrice.ts";

const words = defaultCopy.voiceSettings;
const kindName = (kind: string) => words.kinds[kind] ?? kind;
const message = (error: unknown, fallback: string) =>
  error instanceof ApiError ? error.message : fallback;

/** How a provider is set up, never its key. */
const summary = (p: VoiceProvider) =>
  words.summary(
    p.model,
    p.voice,
    p.key_stored ? words.keyKept : p.key_ref ? p.key_ref : words.noKey,
  );

/**
 * The workspace's voice provider: Groq or OpenAI, one at a time. Off until an admin sets
 * one up; Try Voice speaks a sample and hears it back, so the admin hears the voice and
 * knows both ways work. Changing it makes the console read the voice settings again.
 */
export function VoiceSettings({ canEdit }: { canEdit: boolean }) {
  const [view, setView] = useState<VoiceProviderView | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [editing, setEditing] = useState<VoiceKindName | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [check, setCheck] = useState<VoiceCheck | "checking" | null>(null);
  const [checkProblem, setCheckProblem] = useState<string | null>(null);
  const player = useRef<HTMLAudioElement | null>(null);

  const load = useCallback(() => {
    api
      .voiceProvider()
      .then(setView)
      .catch((e) =>
        setProblem(
          e instanceof ApiError && e.status === 403 ? words.readOnly : message(e, words.loadFailed),
        ),
      );
  }, []);
  useEffect(load, []);
  // Leaving the page stops a sample still playing.
  useEffect(() => () => player.current?.pause(), []);

  const tryVoice = () => {
    setCheck("checking");
    setCheckProblem(null);
    api
      .checkVoiceProvider()
      .then((c) => {
        setCheck(c);
        player.current?.pause();
        player.current = new Audio(`data:${c.media_type};base64,${c.audio}`);
        player.current.play().catch(() => undefined);
      })
      .catch((e) => {
        setCheck(null);
        setCheckProblem(words.checkFailed(message(e, words.loadFailed)));
      });
  };
  const changed = () => {
    forgetVoiceSettings();
    load();
  };
  const turnOff = () => {
    setProblem(null);
    api
      .deleteVoiceProvider()
      .then(() => {
        setConfirming(false);
        setCheck(null);
        changed();
      })
      .catch((e) => {
        setConfirming(false);
        setProblem(message(e, words.offFailed));
      });
  };

  if (view === null) return <Problem message={problem} />;
  const current = view.provider;
  const row = "flex min-w-0 flex-wrap items-center gap-3 px-3 py-2.5";
  return (
    <div className="flex min-w-0 flex-col gap-3">
      <Problem message={problem} />
      <ul
        aria-label={words.title}
        className="flex min-w-0 flex-col divide-y divide-aui-border rounded-aui-lg border border-aui-border"
      >
        {view.kinds.map((k) => {
          const on = current?.kind === k.kind;
          return (
            <li key={k.kind} className={row}>
              <MicIcon size={18} />
              <div className="flex min-w-0 flex-1 flex-col">
                <span className="truncate text-aui-sm font-medium text-aui-fg">
                  {kindName(k.kind)}
                </span>
                <span
                  className="truncate text-aui-xs text-aui-muted"
                  title={on && current ? summary(current) : undefined}
                >
                  {on && current ? summary(current) : words.off}
                </span>
              </div>
              {on ? (
                <>
                  <Pill tone="ok">{words.on}</Pill>
                  {canEdit &&
                    (confirming ? (
                      <span className="flex items-center gap-2">
                        <button
                          type="button"
                          className={button}
                          onClick={() => setConfirming(false)}
                        >
                          {words.keep}
                        </button>
                        <button type="button" className={primary} onClick={turnOff}>
                          {words.confirmOff}
                        </button>
                      </span>
                    ) : (
                      <span className="flex flex-wrap items-center gap-1">
                        <button
                          type="button"
                          className={button}
                          disabled={check === "checking"}
                          onClick={tryVoice}
                        >
                          {check === "checking" ? words.trying : words.tryVoice}
                        </button>
                        <button type="button" className={button} onClick={() => setEditing(k.kind)}>
                          {words.edit}
                        </button>
                        <button
                          type="button"
                          className={button}
                          onClick={() => setConfirming(true)}
                        >
                          {words.turnOff}
                        </button>
                      </span>
                    ))}
                </>
              ) : (
                canEdit &&
                editing !== k.kind && (
                  <span className="flex items-center gap-2">
                    {current && (
                      <span className="text-aui-xs text-aui-muted">
                        {words.replaces(kindName(current.kind))}
                      </span>
                    )}
                    <button type="button" className={primary} onClick={() => setEditing(k.kind)}>
                      {words.setUp}
                    </button>
                  </span>
                )
              )}
            </li>
          );
        })}
      </ul>
      {/* What the sample was heard as: a settled result, announced once. */}
      <output className="block text-aui-sm text-aui-muted empty:hidden">
        {check && check !== "checking" ? words.heard(check.heard) : ""}
      </output>
      <Problem message={checkProblem} />
      {editing !== null && (
        <VoiceForm
          key={editing}
          info={view.kinds.find((k) => k.kind === editing) as VoiceKind}
          existing={current?.kind === editing ? current : undefined}
          onCancel={() => setEditing(null)}
          onSaved={() => {
            setEditing(null);
            setCheck(null);
            changed();
          }}
        />
      )}
    </div>
  );
}

const samePrice = (a: VoicePrice, b: VoicePrice) =>
  a.hear_per_hour === b.hear_per_hour &&
  a.min_seconds === b.min_seconds &&
  a.speak_per_million_chars === b.speak_per_million_chars;

/**
 * Sets a kind up, or changes the one set up. A change keeps its key unless a new one is
 * given; models and prices start at what it has, else the kind's defaults.
 */
function VoiceForm({
  info,
  existing,
  onCancel,
  onSaved,
}: {
  info: VoiceKind;
  existing?: VoiceProvider;
  onCancel: () => void;
  onSaved: (saved: VoiceProvider) => void;
}) {
  const base = existing ?? { ...info, key_stored: false };
  const [keyWay, setKeyWay] = useState<"value" | "env">(existing?.key_ref ? "env" : "value");
  const [key, setKey] = useState("");
  const [keyRef, setKeyRef] = useState(existing?.key_ref ?? "");
  const [voice, setVoice] = useState(base.voice);
  const [hearModel, setHearModel] = useState(base.model);
  const [speakModel, setSpeakModel] = useState(base.speech_model);
  const [hearPrice, setHearPrice] = useState(microsToDollars(base.price.hear_per_hour));
  const [minSeconds, setMinSeconds] = useState(String(base.price.min_seconds));
  const [speakPrice, setSpeakPrice] = useState(microsToDollars(base.price.speak_per_million_chars));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState<string | null>(null);
  const keeps = !!existing && (existing.key_stored || !!existing.key_ref);
  const voices = info.voices.includes(voice) ? info.voices : [voice, ...info.voices];

  const submit = (e: FormEvent) => {
    e.preventDefault();
    setProblem(null);
    const hear = dollarsToMicros(hearPrice);
    const speak = dollarsToMicros(speakPrice);
    const least = /^\d{1,4}$/.test(minSeconds.trim()) ? Number(minSeconds.trim()) : null;
    if (hear === null || speak === null || least === null || least > 3600) {
      setProblem(words.badPrice);
      return;
    }
    const price = { hear_per_hour: hear, min_seconds: least, speak_per_million_chars: speak };
    const input: VoiceProviderInput = {
      kind: info.kind,
      voice,
      model: hearModel.trim(),
      speech_model: speakModel.trim(),
    };
    // A price left as shown stays the provider's: it follows the published one.
    if (!samePrice(price, base.price)) input.price = price;
    if (keyWay === "value" && key.trim()) input.key = key.trim();
    if (keyWay === "env" && keyRef.trim()) input.key_ref = keyRef.trim();
    setBusy(true);
    api
      .setVoiceProvider(input)
      .then(onSaved)
      .catch((err) => setProblem(message(err, words.saveFailed)))
      .finally(() => setBusy(false));
  };

  const name = kindName(info.kind);
  return (
    <form
      className={`${card} flex min-w-0 flex-col gap-3`}
      onSubmit={submit}
      aria-label={existing ? words.changeTitle(name) : words.formTitle(name)}
    >
      <h2 className="flex items-center gap-2 text-aui-sm font-medium text-aui-fg">
        <MicIcon size={16} />
        {existing ? words.changeTitle(name) : words.formTitle(name)}
      </h2>
      <div className="grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-[10rem_minmax(0,1fr)]">
        <Label text={words.key}>
          <NativeSelect
            aria-label={words.keyWay}
            value={keyWay}
            onChange={(e) => setKeyWay(e.target.value as "value" | "env")}
          >
            <option value="value">{words.pasteIt}</option>
            <option value="env">{words.envVar}</option>
          </NativeSelect>
        </Label>
        {keyWay === "value" ? (
          <Label text={words.value} hint={keeps ? words.keepsKey : words.keptHint}>
            <input
              className={field}
              type="password"
              autoComplete="off"
              required={!keeps}
              value={key}
              onChange={(e) => setKey(e.target.value)}
            />
          </Label>
        ) : (
          <Label text={words.reference} hint={words.referenceHint}>
            <input
              className={field}
              required={!keeps}
              placeholder="env:HUMAGEN_GROQ_API_KEY"
              value={keyRef}
              onChange={(e) => setKeyRef(e.target.value)}
            />
          </Label>
        )}
      </div>
      <Label text={words.voice}>
        <NativeSelect value={voice} onChange={(e) => setVoice(e.target.value)}>
          {voices.map((v) => (
            <option key={v} value={v}>
              {v}
            </option>
          ))}
        </NativeSelect>
      </Label>
      <details className="flex min-w-0 flex-col gap-3">
        <summary className="cursor-pointer text-aui-sm text-aui-muted">{words.more}</summary>
        <div className="mt-3 grid min-w-0 grid-cols-1 gap-3 sm:grid-cols-2">
          <Label text={words.hearModel}>
            <input
              className={field}
              value={hearModel}
              onChange={(e) => setHearModel(e.target.value)}
            />
          </Label>
          <Label text={words.speakModel}>
            <input
              className={field}
              value={speakModel}
              onChange={(e) => setSpeakModel(e.target.value)}
            />
          </Label>
          <Label text={words.hearPrice} hint={words.priceHint}>
            <input
              className={field}
              inputMode="decimal"
              value={hearPrice}
              onChange={(e) => setHearPrice(e.target.value)}
            />
          </Label>
          <Label text={words.minSeconds}>
            <input
              className={field}
              inputMode="numeric"
              value={minSeconds}
              onChange={(e) => setMinSeconds(e.target.value)}
            />
          </Label>
          <Label text={words.speakPrice}>
            <input
              className={field}
              inputMode="decimal"
              value={speakPrice}
              onChange={(e) => setSpeakPrice(e.target.value)}
            />
          </Label>
        </div>
      </details>
      <Problem message={problem} />
      <div className="flex items-center gap-2">
        <button type="button" className={button} onClick={onCancel}>
          {words.cancel}
        </button>
        <button type="submit" className={primary} disabled={busy}>
          {busy ? words.saving : words.save}
        </button>
      </div>
    </form>
  );
}
