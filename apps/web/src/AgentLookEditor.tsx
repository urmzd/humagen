import { defaultCopy } from "@humagen/components";
import { AgentInitials, AgentStill, agentBot, type BotLook, botShapes, toRgb } from "@humagen/web";
import { type CSSProperties, useEffect, useRef, useState } from "react";
import { setAgentHues, setAgentLooks } from "./agentHues.ts";
import { botLook } from "./agentLooks.ts";
import { type AgentLook, type AgentLookView, ApiError, api } from "./api.ts";
import { brandShown } from "./branding.ts";
import { button, field, Label, Problem, primary, Segmented, SkeletonRows, Switch } from "./ui.tsx";

const words = defaultCopy.agentLook;

/** The hues a picker offers: the twelve the console has always drawn. */
export const SWATCHES = Array.from({ length: 12 }, (_, i) => i * 30);

type Face = NonNullable<AgentLook["face"]>;
type Finish = NonNullable<AgentLook["finish"]>;
type Hat = NonNullable<AgentLook["hat"]>;
type Glasses = NonNullable<AgentLook["glasses"]>;

/** A look with every default left out, as the server keeps it, so two ways of writing one look compare equal. */
export function trimLook(look: AgentLook): AgentLook {
  const out: AgentLook = {};
  const name = (look.display_name ?? "").trim().replace(/\s+/g, " ");
  if (name) out.display_name = name;
  if (look.shape) out.shape = look.shape;
  if (look.face && look.face !== "eyes") out.face = look.face;
  if (look.finish && look.finish !== "fabric") out.finish = look.finish;
  if (look.hat && look.hat !== "none") out.hat = look.hat;
  if (look.glasses && look.glasses !== "none") out.glasses = look.glasses;
  if (look.headphones) out.headphones = true;
  if (look.bow_tie) out.bow_tie = true;
  const greeting = (look.greeting ?? "").trim();
  if (greeting) out.greeting = greeting;
  const starters = (look.starters ?? [])
    .map((st) => ({ title: st.title.trim().replace(/\s+/g, " "), prompt: st.prompt.trim() }))
    .filter((st) => st.title || st.prompt);
  if (starters.length) out.starters = starters;
  return out;
}

export const sameLook = (a: AgentLook, b: AgentLook) =>
  JSON.stringify(trimLook(a)) === JSON.stringify(trimLook(b));

/** A random look and free hue, for Shuffle. `random` is injectable so a test can pin it. */
export function shuffled(
  taken: readonly number[],
  random: () => number = Math.random,
  keep?: AgentLook,
) {
  const pick = <T,>(xs: readonly T[]) => xs[Math.floor(random() * xs.length)] as T;
  const free = SWATCHES.filter((h) => !taken.includes(h));
  const look: AgentLook = {
    display_name: keep?.display_name,
    shape: pick(botShapes) as AgentLook["shape"],
    face: pick(["eyes", "mouth"] as const),
    finish: pick(["fabric", "fabric", "plastic", "smooth"] as const),
    hat: random() < 0.4 ? pick(["beret", "beanie", "party", "crown"] as const) : "none",
    glasses: random() < 0.35 ? pick(["round", "square", "shades"] as const) : "none",
    headphones: random() < 0.2,
    bow_tie: random() < 0.2,
  };
  return { look: trimLook(look), hue: free.length ? pick(free) : undefined };
}

/**
 * How an agent looks, and what people call it: a live preview beside the
 * choices. Every choice starts at the agent's default, so an agent nobody
 * styled keeps the bot it always had until someone picks another.
 */
export function AgentLookEditor({
  name,
  canWrite,
  seed,
}: {
  /** The agent's handle. */
  name: string;
  canWrite: boolean;
  /** The view to start from, for tests and the gallery; read from the API otherwise. */
  seed?: AgentLookView;
}) {
  const [view, setView] = useState<AgentLookView | null>(seed ?? null);
  const [look, setLook] = useState<AgentLook>(seed?.look ?? {});
  const [hue, setHue] = useState<number | undefined>(seed?.hue);
  const [problem, setProblem] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);

  useEffect(() => {
    if (seed) return;
    let live = true;
    api
      .agentLook(name)
      .then((v) => {
        if (!live) return;
        setView(v);
        setLook(v.look);
        setHue(v.hue);
      })
      .catch((e) => live && setProblem(e instanceof ApiError ? e.message : words.failed));
    return () => {
      live = false;
    };
  }, [name, seed]);

  // A still of the bot, drawn off screen in the look being edited, is what a
  // save sends as the agent's picture. Its color is read from the page, as a
  // mark's is.
  const tinted = useRef<HTMLSpanElement>(null);
  const still = useRef<HTMLCanvasElement>(null);
  const [ink, setInk] = useState<string>();
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again when the color changes
  useEffect(() => {
    if (tinted.current) setInk(toRgb(getComputedStyle(tinted.current).color));
  }, [hue, view]);

  if (!view) return problem ? <Problem message={problem} /> : <SkeletonRows count={4} />;

  const base = brandShown(name);
  const shape = (look.shape ?? agentBot(base)) as NonNullable<BotLook["shape"]>;
  const dirty = !sameLook(look, view.look) || hue !== view.hue;
  const isDefault = Object.keys(trimLook(look)).length === 0;
  const set = (patch: Partial<AgentLook>) => {
    setNotice(null);
    setLook((l) => ({ ...l, ...patch }));
  };
  const drawn = (l: AgentLook): BotLook => ({
    ...botLook(trimLook(l)),
    shape: (l.shape ?? shape) as BotLook["shape"],
  });
  const tint = (h: number | undefined) => ({ "--aui-accent-h": h ?? view.hue }) as CSSProperties;
  const swatches = SWATCHES.includes(view.hue)
    ? SWATCHES
    : [...SWATCHES, view.hue].sort((a, b) => a - b);

  const save = () => {
    setSaving(true);
    setProblem(null);
    api
      .setAgentLook(name, trimLook(look), hue !== view.hue ? hue : undefined)
      .then((v) => {
        setView(v);
        setLook(v.look);
        setHue(v.hue);
        setAgentLooks(Object.keys(v.look).length ? { [name]: v.look } : {}, [name]);
        setAgentHues({ [name]: v.hue });
        setNotice(words.saved);
        return drawPicture();
      })
      .catch((e) => {
        if (e instanceof ApiError && e.status === 409)
          setProblem(/color/i.test(e.message) ? words.colorTakenNow : words.nameTaken);
        else setProblem(e instanceof ApiError ? e.message : words.failed);
      })
      .finally(() => setSaving(false));
  };

  /**
   * Send the still as the agent's picture. It is drawn after the saved look
   * is on it, and a page that cannot draw one (no canvas) sends nothing: the
   * look is saved either way, and Slack falls back to the app's own icon.
   */
  const drawPicture = async () => {
    await new Promise((done) => requestAnimationFrame(() => requestAnimationFrame(done)));
    const canvas = still.current;
    if (!canvas || typeof canvas.toBlob !== "function") return;
    const png = await new Promise<Blob | null>((done) => {
      try {
        canvas.toBlob(done, "image/png");
      } catch {
        done(null);
      }
    });
    if (!png || png.size === 0) return;
    try {
      const v = await api.setAgentMark(name, png);
      setAgentLooks(Object.keys(v.look).length ? { [name]: v.look } : {}, [name]);
    } catch {
      // The look is saved; only its picture for other surfaces is not.
    }
  };
  const starters = look.starters ?? [];
  const setStarter = (i: number, patch: Partial<{ title: string; prompt: string }>) => {
    const next = [0, 1, 2].map((j) => ({
      title: starters[j]?.title ?? "",
      prompt: starters[j]?.prompt ?? "",
    }));
    next[i] = { ...next[i], ...patch } as (typeof next)[number];
    set({ starters: next });
  };

  const shown = (look.display_name ?? "").trim() || base;

  return (
    <div className="flex flex-col gap-6" data-testid="agent-look">
      {!canWrite && <p className="text-aui-sm text-aui-muted">{words.readOnly}</p>}
      <div className="grid min-w-0 gap-6 md:grid-cols-[minmax(0,15rem)_minmax(0,1fr)]">
        <div className="flex flex-col items-center gap-3 self-start rounded-aui border border-aui-border bg-aui-surface p-6 md:sticky md:top-4">
          {/* A bot reads its color once, so a new color draws a new one. */}
          <span key={hue} data-accent="agent" style={tint(hue)} className="inline-flex">
            <AgentInitials name={base} lookOf={name} look={drawn(look)} size={120} />
          </span>
          <div className="flex min-w-0 flex-col items-center">
            <span className="max-w-full truncate font-semibold text-aui-lg" data-testid="look-name">
              {shown}
            </span>
            <span className="text-aui-xs text-aui-muted">@{name}</span>
          </div>
          {look.greeting?.trim() && (
            <p className="text-center text-aui-xs text-aui-muted" data-testid="look-greeting">
              {look.greeting.trim()}
            </p>
          )}
          <span
            ref={tinted}
            aria-hidden
            data-accent="agent"
            style={{
              ...tint(hue),
              color: "var(--aui-accent)",
              position: "absolute",
              left: -10000,
              top: 0,
            }}
          >
            {ink && (
              <AgentStill ref={still} key={`${hue}`} name={base} look={drawn(look)} color={ink} />
            )}
          </span>
          {isDefault && hue === view.hue && (
            <span className="rounded-full bg-aui-hover px-2 py-0.5 text-aui-2xs text-aui-muted">
              {words.defaultBadge}
            </span>
          )}
        </div>

        <fieldset disabled={!canWrite} className="flex min-w-0 flex-col gap-5">
          <Label text={words.name} hint={words.nameHint}>
            <input
              id="agent-look-name"
              className={field}
              value={look.display_name ?? ""}
              placeholder={base}
              maxLength={40}
              onChange={(e) => set({ display_name: e.target.value })}
            />
          </Label>

          <Label text={words.greeting} hint={words.greetingHint}>
            <textarea
              id="agent-look-greeting"
              className={`${field} min-h-16`}
              value={look.greeting ?? ""}
              maxLength={280}
              rows={2}
              onChange={(e) => set({ greeting: e.target.value })}
            />
          </Label>

          <fieldset className="flex min-w-0 flex-col gap-2">
            <legend className="mb-2 text-aui-xs font-medium text-aui-muted">
              {words.starters}
            </legend>
            {[0, 1, 2].map((i) => (
              <div
                key={i}
                className="grid min-w-0 gap-2 sm:grid-cols-[minmax(0,12rem)_minmax(0,1fr)]"
              >
                <input
                  id={`agent-look-starter-${i}-title`}
                  aria-label={words.starterTitle(i + 1)}
                  className={field}
                  placeholder={words.starterTitlePlaceholder}
                  maxLength={60}
                  value={starters[i]?.title ?? ""}
                  onChange={(e) => setStarter(i, { title: e.target.value })}
                />
                <input
                  id={`agent-look-starter-${i}-prompt`}
                  aria-label={words.starterPrompt(i + 1)}
                  className={field}
                  placeholder={words.starterPromptPlaceholder}
                  maxLength={500}
                  value={starters[i]?.prompt ?? ""}
                  onChange={(e) => setStarter(i, { prompt: e.target.value })}
                />
              </div>
            ))}
          </fieldset>

          <fieldset className="flex min-w-0 flex-col gap-2">
            <legend className="mb-2 text-aui-xs font-medium text-aui-muted">{words.shape}</legend>
            <div className="grid grid-cols-6 gap-2 sm:grid-cols-9">
              {botShapes.map((s) => (
                <button
                  key={s}
                  type="button"
                  aria-pressed={shape === s}
                  aria-label={s}
                  title={s}
                  data-shape={s}
                  onClick={() => set({ shape: s as AgentLook["shape"] })}
                  className={`flex aspect-square items-center justify-center rounded-aui border ${
                    shape === s
                      ? "border-aui-accent bg-aui-accent-soft"
                      : "border-aui-border hover:bg-aui-hover"
                  }`}
                >
                  <span key={hue} data-accent="agent" style={tint(hue)} className="inline-flex">
                    <AgentInitials
                      name={base}
                      look={{
                        ...drawn(look),
                        shape: s,
                        hat: "none",
                        glasses: "none",
                        headphones: false,
                        bowTie: false,
                      }}
                      size={30}
                    />
                  </span>
                </button>
              ))}
            </div>
          </fieldset>

          <fieldset className="flex min-w-0 flex-col gap-2">
            <legend className="mb-2 text-aui-xs font-medium text-aui-muted">{words.color}</legend>
            <div className="flex flex-wrap gap-2">
              {swatches.map((h) => {
                const taken = view.taken.includes(h);
                return (
                  <button
                    key={h}
                    type="button"
                    aria-pressed={hue === h}
                    aria-label={`Hue ${h}`}
                    title={taken ? words.colorTaken : `Hue ${h}`}
                    disabled={taken}
                    data-hue={h}
                    onClick={() => {
                      setNotice(null);
                      setHue(h);
                    }}
                    data-accent="agent"
                    className={`size-8 rounded-full border-2 bg-[var(--aui-accent)] disabled:cursor-not-allowed disabled:opacity-40 disabled:[background-image:linear-gradient(135deg,transparent_45%,var(--aui-fg)_45%,var(--aui-fg)_55%,transparent_55%)] ${
                      hue === h ? "border-aui-fg" : "border-transparent"
                    }`}
                    style={tint(h)}
                  />
                );
              })}
            </div>
          </fieldset>

          <div className="grid min-w-0 gap-4 sm:grid-cols-2">
            <div className="flex min-w-0 flex-col gap-1">
              <span aria-hidden className="text-aui-xs font-medium text-aui-muted">
                {words.face}
              </span>
              <Segmented<Face>
                label={words.face}
                value={(look.face ?? "eyes") as Face}
                options={(["eyes", "mouth"] as Face[]).map((id) => ({
                  id,
                  label: words.faces[id],
                }))}
                onChange={(face) => set({ face })}
              />
            </div>
            <div className="flex min-w-0 flex-col gap-1">
              <span aria-hidden className="text-aui-xs font-medium text-aui-muted">
                {words.finish}
              </span>
              <Segmented<Finish>
                label={words.finish}
                value={(look.finish ?? "fabric") as Finish}
                options={(["fabric", "plastic", "smooth", "flat"] as Finish[]).map((id) => ({
                  id,
                  label: words.finishes[id],
                }))}
                onChange={(finish) => set({ finish })}
              />
            </div>
            <div className="flex min-w-0 flex-col gap-1">
              <span aria-hidden className="text-aui-xs font-medium text-aui-muted">
                {words.hat}
              </span>
              <Segmented<Hat>
                label={words.hat}
                value={(look.hat ?? "none") as Hat}
                options={(["none", "beret", "beanie", "party", "crown"] as Hat[]).map((id) => ({
                  id,
                  label: words.hats[id],
                }))}
                onChange={(hat) => set({ hat })}
              />
            </div>
            <div className="flex min-w-0 flex-col gap-1">
              <span aria-hidden className="text-aui-xs font-medium text-aui-muted">
                {words.glasses}
              </span>
              <Segmented<Glasses>
                label={words.glasses}
                value={(look.glasses ?? "none") as Glasses}
                options={(["none", "round", "square", "shades"] as Glasses[]).map((id) => ({
                  id,
                  label: words.glassesKinds[id],
                }))}
                onChange={(glasses) => set({ glasses })}
              />
            </div>
          </div>
          <div className="flex flex-col gap-3">
            <Switch
              label={words.headphones}
              checked={!!look.headphones}
              onChange={(headphones) => set({ headphones })}
            />
            <Switch
              label={words.bowTie}
              checked={!!look.bow_tie}
              onChange={(bow_tie) => set({ bow_tie })}
            />
          </div>

          <Problem message={problem} />
          {canWrite && (
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                className={button}
                onClick={() => {
                  const next = shuffled(view.taken, Math.random, look);
                  setNotice(null);
                  setLook(next.look);
                  if (next.hue !== undefined) setHue(next.hue);
                }}
              >
                {words.shuffle}
              </button>
              <button
                type="button"
                className={button}
                disabled={isDefault}
                onClick={() => {
                  setNotice(null);
                  setLook({});
                }}
              >
                {words.reset}
              </button>
              <span className="flex-1" />
              {notice && !dirty && <output className="text-aui-xs text-aui-muted">{notice}</output>}
              {dirty && (
                <button
                  type="button"
                  className={button}
                  onClick={() => {
                    setLook(view.look);
                    setHue(view.hue);
                  }}
                >
                  {words.discard}
                </button>
              )}
              <button type="button" className={primary} disabled={!dirty || saving} onClick={save}>
                {words.save}
              </button>
            </div>
          )}
        </fieldset>
      </div>
    </div>
  );
}
