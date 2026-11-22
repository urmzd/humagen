import { defaultCopy } from "@humagen/components";
import { ChevronRightIcon, PlusIcon, RefreshIcon } from "@humagen/render-web";
import { AgentInitials, type BotLook } from "@humagen/web";
import { type ReactNode, useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { FallbackCondition } from "../../../packages/client/src/generated/types.ts";
import { HuePicker, ShapePicker, SWATCHES, shuffled, tintOf } from "./AgentLookEditor.tsx";
import { agentHue, setAgentHues, setAgentLooks, subscribeHues } from "./agentHues.ts";
import {
  type AgentLook,
  type AgentTemplate,
  type AgentTemplateCatalog,
  ApiError,
  api,
  type Revision,
} from "./api.ts";
import { href } from "./route.ts";
import { button, field, Page, Problem, primary, SearchField, SkeletonRows } from "./ui.tsx";

const words = defaultCopy.agentCreate;

/** A handle people can tag from a name: lower case, letters and digits joined by dashes. */
export function handleOf(name: string): string {
  return name
    .normalize("NFKD")
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
}

/** The templates whose title, outcome or description has every word of the query. Blank always stays. */
export function matching(templates: AgentTemplate[], query: string): AgentTemplate[] {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return templates;
  return templates.filter((t) => {
    if (t.id === "blank") return true;
    const text = `${t.title} ${t.outcome} ${t.description}`.toLowerCase();
    return words.every((w) => text.includes(w));
  });
}

/** What a new agent is called on screen: the name typed, else its handle. */
const agentShownOf = (handle: string, typed: string) => typed.trim() || handle;

/** The first color no agent holds, or none when every one is held and the server is left to pick. */
export function freeHue(taken: readonly number[]): number | undefined {
  return SWATCHES.find((h) => !taken.includes(h));
}

/**
 * A new agent, goal first: pick the outcome it is for from the templates
 * (blank first, searchable), then give it a name, a shape and a color with
 * its bot drawn as it will be. Create makes an ordinary agent from the
 * template and keeps its look; everything stays editable in its settings.
 * Each step has an address: /agents/new, then /agents/new/{template}.
 */
export function AgentCreate({
  template,
  agents,
  builder,
  onPick,
  onBack,
  onCreated,
  onDescribe,
  seed,
}: {
  /** The picked template's id: the Customize step. Absent, the goal step. */
  template?: string;
  /** The workspace's agents, whose colors are taken. */
  agents: Revision[];
  /** What the entry agent is called, for Describe It. */
  builder?: string;
  onPick: (template: string | undefined) => void;
  onBack: () => void;
  onCreated: (agent: Revision) => void;
  /** Describe the agent to the builder in a session instead, when the workspace has one. */
  onDescribe?: () => void;
  /** The catalog to start from, for tests and the gallery; read from the API otherwise. */
  seed?: AgentTemplateCatalog;
}) {
  const [catalog, setCatalog] = useState<AgentTemplateCatalog | undefined>(seed);
  const [problem, setProblem] = useState("");
  const [attempt, setAttempt] = useState(0);
  // biome-ignore lint/correctness/useExhaustiveDependencies: read again on Retry
  useEffect(() => {
    if (seed) return;
    let live = true;
    setProblem("");
    api
      .agentTemplates()
      .then((c) => live && setCatalog(c))
      .catch(() => live && setProblem(words.templatesFailed));
    return () => {
      live = false;
    };
  }, [seed, attempt]);

  const picked = catalog?.templates.find((t) => t.id === template);
  const body: ReactNode = !catalog ? (
    problem ? (
      <div
        className="flex flex-col items-start gap-3 p-6 lg:col-span-2"
        data-testid="agent-create-failed"
      >
        <Problem message={problem} />
        <button type="button" className={button} onClick={() => setAttempt((n) => n + 1)}>
          {words.retry}
        </button>
      </div>
    ) : (
      <div className="p-6 lg:col-span-2">
        <SkeletonRows count={6} />
      </div>
    )
  ) : template && picked ? (
    <Customize
      template={picked}
      catalog={catalog}
      agents={agents}
      onBack={() => onPick(undefined)}
      onCreated={onCreated}
    />
  ) : (
    <Goal
      templates={catalog.templates}
      stale={!!template && !picked}
      builder={builder}
      onContinue={(id) => onPick(id)}
      onDescribe={onDescribe}
    />
  );
  return (
    <Page title={words.title} back={{ label: defaultCopy.navigation.agents, onClick: onBack }}>
      <div
        className="grid min-w-0 overflow-hidden rounded-aui-lg border border-aui-border bg-aui-bg lg:min-h-[32rem] lg:grid-cols-[minmax(0,5fr)_minmax(0,6fr)]"
        data-testid="agent-create"
      >
        {body}
      </div>
    </Page>
  );
}

/** The goal step's stage: the bots a template can start as, gathered, and what making one means. */
function Gathered({ templates }: { templates: AgentTemplate[] }) {
  const crowd = templates.filter((t) => t.id !== "blank").slice(0, 5);
  return (
    <div className="flex flex-col items-center justify-center gap-5 bg-aui-hover/60 px-6 py-8 text-center max-md:py-6">
      <div aria-hidden className="flex items-end justify-center gap-1 max-md:hidden">
        {crowd.map((t, i) => (
          <span
            key={t.id}
            data-accent="agent"
            style={tintOf(SWATCHES[(i * 5 + 1) % SWATCHES.length] ?? 0)}
            className={`inline-flex ${i === 2 ? "-translate-y-3" : i % 2 ? "translate-y-1" : ""}`}
          >
            <AgentInitials
              name={t.title}
              look={{ shape: t.shape as BotLook["shape"] }}
              size={i === 2 ? 76 : 52}
            />
          </span>
        ))}
      </div>
      <div className="flex max-w-xs flex-col gap-1.5">
        <h2 className="humagen-display font-semibold text-aui-lg text-aui-fg">{words.artTitle}</h2>
        <p className="text-aui-sm text-aui-muted">{words.artLine}</p>
      </div>
    </div>
  );
}

function Goal({
  templates,
  stale,
  builder,
  onContinue,
  onDescribe,
}: {
  templates: AgentTemplate[];
  /** The address named a template the catalog no longer has. */
  stale: boolean;
  builder?: string;
  onContinue: (id: string) => void;
  onDescribe?: () => void;
}) {
  const [query, setQuery] = useState("");
  const [chosen, setChosen] = useState(templates[0]?.id ?? "blank");
  const shown = matching(templates, query);
  const others = shown.filter((t) => t.id !== "blank");
  return (
    <>
      <Gathered templates={templates} />
      <form
        className="flex min-w-0 flex-col gap-4 p-5 md:p-6"
        onSubmit={(e) => {
          e.preventDefault();
          onContinue(chosen);
        }}
      >
        <div className="flex flex-col gap-1">
          <h2 className="font-semibold text-aui-base text-aui-fg">{words.goal}</h2>
          <p className="text-aui-sm text-aui-muted">{words.goalHint}</p>
        </div>
        {stale && <Problem message={words.unknownTemplate} />}
        <SearchField
          label={words.search}
          placeholder={words.search}
          value={query}
          onChange={setQuery}
        />
        <fieldset className="flex min-h-0 min-w-0 flex-col">
          <legend className="sr-only">{words.templates}</legend>
          <div className="flex max-h-[22rem] min-w-0 flex-col gap-0.5 overflow-y-auto rounded-aui border border-aui-border p-1">
            {shown.map((t, i) => (
              <label
                key={t.id}
                data-template={t.id}
                className={`flex min-h-11 shrink-0 cursor-pointer items-center gap-3 rounded-aui px-2.5 py-2 has-[:focus-visible]:outline-2 has-[:focus-visible]:outline-aui-accent ${
                  chosen === t.id ? "bg-aui-accent-soft" : "hover:bg-aui-hover"
                }`}
              >
                <input
                  type="radio"
                  name="agent-template"
                  value={t.id}
                  className="sr-only"
                  checked={chosen === t.id}
                  onChange={() => setChosen(t.id)}
                />
                {t.id === "blank" ? (
                  <span
                    aria-hidden
                    className="flex size-7 shrink-0 items-center justify-center rounded-full border border-dashed border-aui-border text-aui-muted"
                  >
                    <PlusIcon className="size-3.5" />
                  </span>
                ) : (
                  <span
                    aria-hidden
                    data-accent="agent"
                    style={tintOf(SWATCHES[(i * 5 + 1) % SWATCHES.length] ?? 0)}
                    className="inline-flex shrink-0"
                  >
                    <AgentInitials
                      name={t.title}
                      look={{ shape: t.shape as BotLook["shape"] }}
                      size={28}
                    />
                  </span>
                )}
                <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-2">
                  <span className="font-medium text-aui-sm text-aui-fg">{t.title}</span>
                  <span className="min-w-0 text-aui-xs text-aui-muted">{t.outcome}</span>
                </span>
              </label>
            ))}
            {query && others.length === 0 && (
              <output className="px-2.5 py-3 text-aui-sm text-aui-muted">
                {words.noMatch(query)}
              </output>
            )}
          </div>
        </fieldset>
        <div className="mt-auto flex flex-wrap items-center justify-end gap-2 pt-2">
          {onDescribe && builder && (
            <button
              type="button"
              onClick={onDescribe}
              className="mr-auto min-h-9 rounded-aui text-aui-sm text-aui-muted underline-offset-4 pointer-coarse:min-h-11 hover:text-aui-fg hover:underline focus-visible:outline-2 focus-visible:outline-aui-accent"
            >
              {words.describe(builder)}
            </button>
          )}
          <button type="submit" className={primary}>
            {words.continue}
            <ChevronRightIcon aria-hidden className="size-4" />
          </button>
        </div>
      </form>
    </>
  );
}

function Customize({
  template,
  catalog,
  agents,
  onBack,
  onCreated,
}: {
  template: AgentTemplate;
  catalog: AgentTemplateCatalog;
  agents: Revision[];
  onBack: () => void;
  onCreated: (agent: Revision) => void;
}) {
  // The colors every other agent holds, as the hue store has them.
  const known = useSyncExternalStore(
    subscribeHues,
    () => agents.map((a) => agentHue(a.spec.name) ?? -1).join(","),
    () => "",
  );
  const taken = useMemo(
    () =>
      known
        .split(",")
        .map(Number)
        .filter((h) => Number.isFinite(h) && h >= 0),
    [known],
  );
  const [name, setName] = useState(template.id === "blank" ? "" : template.title);
  const [look, setLook] = useState<AgentLook>({ shape: template.shape as AgentLook["shape"] });
  const [hue, setHue] = useState<number | undefined>(() => freeHue(taken));
  const [busy, setBusy] = useState(false);
  const [problem, setProblem] = useState("");
  const handle = handleOf(name);
  const shown = name.trim() || template.title;
  const drawn: BotLook = {
    shape: (look.shape ?? template.shape) as BotLook["shape"],
    face: look.face as BotLook["face"],
    finish: look.finish as BotLook["finish"],
    hat: look.hat as BotLook["hat"],
    glasses: look.glasses as BotLook["glasses"],
    headphones: look.headphones,
    bowTie: look.bow_tie,
  };
  const model = catalog.model;
  // The agent once it is made: a failed look is retried for it, never made again.
  const [made, setMade] = useState<Revision>();
  const keepLook = async (agent: Revision) => {
    const kept: AgentLook = {
      ...look,
      ...(name.trim() && name.trim() !== handle ? { display_name: name.trim() } : {}),
      greeting: template.greeting,
      ...(template.starters.length ? { starters: template.starters } : {}),
    };
    try {
      const v = await api.setAgentLook(agent.spec.name, kept, hue);
      setAgentLooks({ [agent.spec.name]: v.look }, [agent.spec.name]);
      setAgentHues({ [agent.spec.name]: v.hue });
      return true;
    } catch (e) {
      setProblem(
        e instanceof ApiError && e.status === 409
          ? /color/i.test(e.message)
            ? defaultCopy.agentLook.colorTakenNow
            : defaultCopy.agentLook.nameTaken
          : words.lookNotKept(agentShownOf(agent.spec.name, name)),
      );
      return false;
    }
  };
  const create = async () => {
    if (!handle) return setProblem(words.nameNeeded);
    setBusy(true);
    setProblem("");
    let agent = made;
    if (!agent) {
      try {
        agent = await api.createAgent({
          name: handle,
          description: template.description,
          provider: model.provider,
          model: model.model,
          ...(model.fallbacks?.length ? { fallbacks: model.fallbacks } : {}),
          ...(model.fallback_on?.length
            ? { fallback_on: model.fallback_on as FallbackCondition[] }
            : {}),
          system_prompt: template.system_prompt,
          tools: [...template.tools],
          access: { spawn: [] },
        });
      } catch (e) {
        setBusy(false);
        return setProblem(
          e instanceof ApiError && e.status === 409
            ? e.message.includes("retired")
              ? words.retired
              : words.taken
            : e instanceof ApiError
              ? e.message
              : words.failed,
        );
      }
      setMade(agent);
    }
    const ok = await keepLook(agent);
    setBusy(false);
    if (ok) onCreated(agent);
  };
  return (
    <>
      <div className="flex flex-col items-center justify-center gap-4 bg-aui-hover/60 px-6 py-8 text-center max-md:py-6">
        <span
          key={hue}
          data-accent="agent"
          style={tintOf(hue ?? 0)}
          className="inline-flex"
          data-testid="agent-create-preview"
        >
          <AgentInitials name={shown} look={drawn} size={120} />
        </span>
        <div className="flex max-w-xs min-w-0 flex-col items-center gap-1.5">
          <span className="max-w-full truncate rounded-aui bg-aui-bg px-3 py-1 font-semibold text-aui-lg text-aui-fg">
            {shown}
          </span>
          {handle && <span className="text-aui-xs text-aui-muted">@{handle}</span>}
          <p className="text-aui-sm text-aui-muted">{template.description}</p>
          <button
            type="button"
            className={`${button} mt-1`}
            onClick={() => {
              const next = shuffled(taken);
              setLook(next.look);
              if (next.hue !== undefined) setHue(next.hue);
            }}
          >
            <RefreshIcon aria-hidden className="size-4" />
            {defaultCopy.agentLook.shuffle}
          </button>
        </div>
      </div>
      <form
        className="flex min-w-0 flex-col gap-5 p-5 md:p-6"
        onSubmit={(e) => {
          e.preventDefault();
          void create();
        }}
      >
        <div className="flex flex-col gap-1">
          <h2 className="font-semibold text-aui-base text-aui-fg">{words.customize}</h2>
          <p className="text-aui-sm text-aui-muted">{words.customizeHint}</p>
        </div>
        <div className="flex flex-col gap-1">
          <label htmlFor="agent-create-name" className="text-aui-xs font-medium text-aui-muted">
            {words.name}
          </label>
          <input
            id="agent-create-name"
            className={field}
            value={name}
            maxLength={40}
            placeholder={template.title}
            aria-describedby="agent-create-handle"
            onChange={(e) => setName(e.target.value)}
            readOnly={!!made}
          />
          <span id="agent-create-handle" className="min-h-4 text-aui-2xs text-aui-muted">
            {handle ? words.handle(handle) : ""}
          </span>
        </div>
        <ShapePicker
          base={shown}
          look={drawn}
          shape={drawn.shape ?? template.shape}
          hue={hue ?? 0}
          onPick={(s) => setLook((l) => ({ ...l, shape: s as AgentLook["shape"] }))}
        />
        <HuePicker hues={SWATCHES} hue={hue} taken={taken} onPick={setHue} />
        <p className="text-aui-xs text-aui-muted">
          {model.provider ? words.startsOn(model.provider, model.model) : words.noModel}
        </p>
        <Problem message={problem} />
        <div className="mt-auto flex flex-wrap items-center justify-end gap-2 pt-2">
          {made ? (
            // The agent exists: its look is tried again, or left to set later.
            <button type="button" className={button} onClick={() => onCreated(made)}>
              {words.openWithoutLook}
            </button>
          ) : (
            <a
              className={button}
              href={href({ page: "agent-create" })}
              onClick={(e) => {
                e.preventDefault();
                onBack();
              }}
            >
              {words.back}
            </a>
          )}
          <button type="submit" className={primary} disabled={busy || !model.provider}>
            {busy ? words.creating : made ? words.retryLook : words.create}
          </button>
        </div>
      </form>
    </>
  );
}
