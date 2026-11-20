import { defaultCopy } from "@humagen/components";
import { useEffect, useState } from "react";
import { api, type StorePackage } from "./api.ts";
import { StoreCatalog, StorePackageDetail } from "./StoreCatalog.tsx";
import { Page, Problem, SkeletonRows } from "./ui.tsx";

/**
 * The Store tab under Agents: the platform's optional agents as packages,
 * each a versioned base definition. The catalog is at `/agent-store` and a
 * package's page at `/agent-store/<name>`. Install adds a package's agents to
 * the workspace, and again restores any that were retired; Remove retires
 * them after a confirm that says their history stays. Whether a package is
 * installed is what the server reads from the workspace's agents.
 */
export function AgentStore({
  canWrite,
  selected,
  onOpen,
  onChanged,
}: {
  /** agents:write: Install and Remove are offered. */
  canWrite: boolean;
  /** The package whose page is open, by name. None is the catalog. */
  selected?: string;
  /** Opens a package's page, or the catalog with no name. */
  onOpen: (name?: string) => void;
  /** Called after an install or removal, so the agent list is read again. */
  onChanged?: () => void;
}) {
  const words = defaultCopy.agentStore;
  const [packages, setPackages] = useState<StorePackage[] | null>(null);
  const [problem, setProblem] = useState("");
  const [busy, setBusy] = useState("");
  const [note, setNote] = useState<Record<string, string>>({});
  useEffect(() => {
    let live = true;
    api
      .storePackages()
      .then((all) => live && setPackages(all))
      .catch(() => live && setProblem(words.failed));
    return () => {
      live = false;
    };
  }, [words.failed]);
  const put = (next: StorePackage) =>
    setPackages((all) => (all ?? []).map((p) => (p.name === next.name ? next : p)));
  const install = async (pkg: StorePackage) => {
    setBusy(pkg.name);
    setProblem("");
    try {
      const out = await api.installStorePackage(pkg.name);
      put(out.package);
      setNote((n) => ({
        ...n,
        [pkg.name]: out.added.length ? words.installedNote(out.added) : "",
      }));
      onChanged?.();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e));
    } finally {
      setBusy("");
    }
  };
  const remove = async (pkg: StorePackage, force: boolean) => {
    setBusy(pkg.name);
    setProblem("");
    try {
      const out = await api.removeStorePackage(pkg.name, force);
      put(out.package);
      setNote((n) => ({
        ...n,
        [pkg.name]: out.retired.length ? words.removedNote(out.retired) : "",
      }));
      onChanged?.();
    } catch (e) {
      setProblem(e instanceof Error ? e.message : String(e));
      // Someone may have changed an agent meanwhile: read the store again.
      api
        .storePackages()
        .then(setPackages)
        .catch(() => {});
    } finally {
      setBusy("");
    }
  };
  const actions = { canWrite, busy, onInstall: install, onRemove: remove };
  const open = selected ? packages?.find((p) => p.name === selected) : undefined;
  return (
    <Page title={words.title}>
      <Problem message={problem} />
      {packages === null ? (
        !problem && <SkeletonRows count={3} height="7rem" />
      ) : open ? (
        <StorePackageDetail
          pkg={open}
          actions={actions}
          note={note[open.name]}
          onBack={() => onOpen()}
        />
      ) : (
        <>
          {selected && <Problem message={words.notFound(selected)} />}
          <StoreCatalog packages={packages} actions={actions} onOpen={(pkg) => onOpen(pkg.name)} />
        </>
      )}
    </Page>
  );
}
