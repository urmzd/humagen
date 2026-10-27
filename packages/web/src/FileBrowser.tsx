import { ChevronDownIcon, FileIcon, FolderIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@humagen/render-web/ui/collapsible";
import { type ReactNode, useState } from "react";
import { Inspector } from "./Inspector.tsx";

interface Folder {
  folders: Map<string, Folder>;
  files: string[];
}

/** Files are an explicit manifest, never a host filesystem path. */
export function FileBrowser({
  paths,
  selected,
  onSelect,
  label,
  children,
  labels,
}: {
  paths: string[];
  selected: string;
  onSelect: (path: string) => void;
  label: string;
  children: ReactNode;
  labels?: Record<string, string>;
}) {
  const [open, setOpen] = useState(false);
  const root: Folder = { folders: new Map(), files: [] };
  for (const path of paths) {
    const parts = path.split("/");
    let at = root;
    for (const name of parts.slice(0, -1)) {
      let child = at.folders.get(name);
      if (!child) {
        child = { folders: new Map(), files: [] };
        at.folders.set(name, child);
      }
      at = child;
    }
    at.files.push(path);
  }
  const branch = (folder: Folder): ReactNode => (
    <ul className="flex min-w-0 flex-col gap-0.5">
      {folder.files.sort().map((path) => (
        <li key={path}>
          <button
            type="button"
            aria-current={selected === path ? "true" : undefined}
            title={path}
            onClick={() => {
              setOpen(false);
              onSelect(path);
            }}
            className="flex min-h-9 w-full items-center gap-2 rounded-md px-2 text-left text-sm text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring aria-current:bg-accent aria-current:font-medium aria-current:text-foreground pointer-coarse:min-h-11"
          >
            <FileIcon size={16} />
            <span className="truncate">{labels?.[path] ?? path.split("/").at(-1)}</span>
          </button>
        </li>
      ))}
      {[...folder.folders]
        .sort(([a], [b]) => a.localeCompare(b))
        .map(([name, child]) => (
          <li key={name}>
            <Collapsible defaultOpen>
              <CollapsibleTrigger className="group flex min-h-9 w-full items-center gap-2 rounded-md px-2 text-left text-sm hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring pointer-coarse:min-h-11">
                <FolderIcon size={16} />
                <span className="min-w-0 flex-1 truncate">{name}</span>
                <ChevronDownIcon
                  className="transition-transform group-data-[state=closed]:-rotate-90"
                  size={14}
                />
              </CollapsibleTrigger>
              <CollapsibleContent className="ml-3 border-l border-border pl-2">
                {branch(child)}
              </CollapsibleContent>
            </Collapsible>
          </li>
        ))}
    </ul>
  );
  return (
    <div
      className="@container/files min-w-0 overflow-hidden rounded-lg border border-border"
      data-slot="file-browser"
    >
      <div className="grid min-w-0 @2xl/files:grid-cols-[12rem_minmax(0,1fr)]">
        <nav
          aria-label={label}
          className="min-w-0 border-b border-border bg-card/40 p-2 @2xl/files:border-b-0 @2xl/files:border-r"
        >
          <Button
            variant="ghost"
            className="w-full justify-start @2xl/files:hidden"
            aria-label={label}
            aria-haspopup="dialog"
            aria-expanded={open}
            onClick={() => setOpen(true)}
          >
            <FileIcon size={16} />
            <span className="min-w-0 truncate">{labels?.[selected] ?? selected}</span>
          </Button>
          {!open && <div className="hidden @2xl/files:block">{branch(root)}</div>}
        </nav>
        <div className="min-w-0">{children}</div>
      </div>
      <Inspector open={open} title={label} onClose={() => setOpen(false)}>
        {open && branch(root)}
      </Inspector>
    </div>
  );
}
