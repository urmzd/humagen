import { defaultCopy } from "@humagen/components";
import { type ReactNode, StrictMode, useState } from "react";
import { createRoot } from "react-dom/client";
import "@humagen/render-web/fonts.css";
import "./styles.css";
import type { StorePackage } from "./api.ts";
import { type StoreActions, StoreCatalog, StorePackageDetail } from "./StoreCatalog.tsx";
import {
  corePackage,
  generatedPackages,
  installed,
  reviewPackage,
  storeAgentNames,
} from "./store.fixtures.ts";
import { Problem, Segmented, SkeletonRows, setAgentHues } from "./ui.tsx";

/**
 * Every state of the store's catalog and package page, drawn from props
 * alone. Open /store-gallery.html on the dev server; add `?story=<id>` for one
 * story on its own. It calls nothing on the platform and is not part of the
 * built console.
 */
const words = defaultCopy.agentStore;
// The page calls nothing, so the hues the server would give are given here.
setAgentHues(Object.fromEntries(storeAgentNames.map((a, i) => [a, (25 + i * 58) % 360])));

/** A store kept in the page: Install and Remove change it, as the server would. */
function useStore(start: StorePackage[], canWrite = true) {
  const [packages, setPackages] = useState(start);
  const [note, setNote] = useState<Record<string, string>>({});
  const put = (next: StorePackage, said: string) => {
    setPackages((all) => all.map((p) => (p.name === next.name ? next : p)));
    setNote((n) => ({ ...n, [next.name]: said }));
  };
  const actions: StoreActions = {
    canWrite,
    onInstall: (pkg) => put(installed(pkg), words.installedNote(pkg.agents.map((a) => a.name))),
    onRemove: (pkg) =>
      put(
        {
          ...pkg,
          installed: false,
          missing: pkg.agents.map((a) => a.name),
          agents: pkg.agents.map((a) => ({ ...a, installed: false, customized: false })),
        },
        words.removedNote(pkg.agents.map((a) => a.name)),
      ),
  };
  return { packages, actions, note };
}

/** The Store tab's page: the catalog, and a package's page once a card is opened. */
function StoreTab({ start, canWrite }: { start: StorePackage[]; canWrite?: boolean }) {
  const { packages, actions, note } = useStore(start, canWrite);
  const [open, setOpen] = useState("");
  const pkg = packages.find((p) => p.name === open);
  return pkg ? (
    <StorePackageDetail
      pkg={pkg}
      actions={actions}
      note={note[pkg.name]}
      onBack={() => setOpen("")}
    />
  ) : (
    <StoreCatalog packages={packages} actions={actions} onOpen={(p) => setOpen(p.name)} />
  );
}

/** One package's page, with a store of its own so its buttons work. */
function PackagePage({ pkg, canWrite }: { pkg: StorePackage; canWrite?: boolean }) {
  const { packages, actions, note } = useStore([pkg], canWrite);
  const now = packages[0] ?? pkg;
  return <StorePackageDetail pkg={now} actions={actions} note={note[now.name]} onBack={() => {}} />;
}

const stories: { id: string; title: string; body: () => ReactNode }[] = [
  {
    id: "tab",
    title: "Store tab: the catalog (open a card for its page)",
    body: () => <StoreTab start={[corePackage(), reviewPackage()]} />,
  },
  {
    id: "catalog-states",
    title: "Catalog: installed, incomplete, a taken name, and more packages (generated)",
    body: () => {
      const review = installed(reviewPackage());
      const core = corePackage();
      const [support, releases, research] = generatedPackages();
      return (
        <StoreTab
          start={[
            installed(corePackage()),
            {
              ...review,
              missing: ["qa-reviewer"],
              agents: review.agents.map((a) =>
                a.name === "qa-reviewer" ? { ...a, installed: false } : a,
              ),
            },
            ...(support ? [support] : []),
            ...(releases
              ? [
                  {
                    ...releases,
                    agents: releases.agents.map((a) => ({ ...a, taken: true })),
                    missing: [],
                  },
                ]
              : []),
            ...(research ? [research] : []),
            { ...core, name: "humagen/core-2", title: "Core (not installed)" },
          ]}
        />
      );
    },
  },
  {
    id: "package",
    title: "Package page: not installed, agents that share a model and tools",
    body: () => <PackagePage pkg={reviewPackage()} />,
  },
  {
    id: "package-differs",
    title: "Package page: agents whose tools differ",
    body: () => <PackagePage pkg={corePackage()} />,
  },
  {
    id: "package-installed",
    title: "Package page: installed, one agent changed in the workspace",
    body: () => (
      <PackagePage
        pkg={installed(reviewPackage(), { "security-reviewer": { customized: true } })}
      />
    ),
  },
  {
    id: "package-models-differ",
    title: "Package page: agents on different models (generated)",
    body: () => {
      const research = generatedPackages()[2];
      return research ? <PackagePage pkg={research} /> : null;
    },
  },
  {
    id: "read-only",
    title: "Without agents:write: no Install or Remove",
    body: () => <StoreTab start={[installed(corePackage()), reviewPackage()]} canWrite={false} />,
  },
  {
    id: "loading",
    title: "Loading",
    body: () => <SkeletonRows count={3} height="7rem" />,
  },
  {
    id: "empty",
    title: "Empty",
    body: () => (
      <StoreCatalog
        packages={[]}
        actions={{ canWrite: true, onInstall() {}, onRemove() {} }}
        onOpen={() => {}}
      />
    ),
  },
  {
    id: "failed",
    title: "Failed",
    body: () => <Problem message={words.failed} />,
  },
];

function Gallery() {
  const query = new URLSearchParams(window.location.search);
  const only = query.get("story");
  const [theme, setTheme] = useState<"dark" | "light">(
    query.get("theme") === "light" ? "light" : "dark",
  );
  document.documentElement.dataset.theme = theme;
  const shown = only ? stories.filter((s) => s.id === only) : stories;
  return (
    <main className="flex max-w-[68rem] flex-col gap-12 px-4 py-8 md:px-10">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <h1 className="humagen-display font-semibold text-[1.875rem] text-aui-fg leading-[1.15]">
          Agent store
        </h1>
        <Segmented
          label="Theme"
          value={theme}
          options={[
            { id: "dark", label: "Dark" },
            { id: "light", label: "Light" },
          ]}
          onChange={setTheme}
        />
      </header>
      {shown.map((story) => (
        <section key={story.id} id={story.id} data-story={story.id} className="flex flex-col gap-4">
          <h2 className="text-aui-muted text-aui-xs uppercase tracking-wide">{story.title}</h2>
          {story.body()}
        </section>
      ))}
    </main>
  );
}

const root = document.getElementById("root");
if (root)
  createRoot(root).render(
    <StrictMode>
      <Gallery />
    </StrictMode>,
  );
