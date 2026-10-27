import { CheckIcon, FolderIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { type ReactNode, useState } from "react";
import { Inspector } from "./Inspector.tsx";

export interface ResourceScope {
  id: string;
  label: string;
  group?: string;
  description?: string;
  icon?: ReactNode;
  count?: number;
}

/** A controlled scope browser. The host supplies only resources this reader may see. */
export function ResourceBrowser({
  scopes,
  selectedScope,
  scopeLabel,
  onScopeChange,
  children,
  scopeSearch,
  disabled,
  selectedLabel,
}: {
  scopes: ResourceScope[];
  selectedScope: string;
  scopeLabel: string;
  onScopeChange: (id: string) => void;
  children: ReactNode;
  scopeSearch?: ReactNode;
  disabled?: boolean;
  selectedLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const selected = scopes.find((scope) => scope.id === selectedScope);
  const choices = (
    <>
      {scopeSearch && <div className="mb-3">{scopeSearch}</div>}
      <ul className="flex flex-col gap-1">
        {scopes.map((scope, index) => (
          <li key={scope.id} className="min-w-0">
            {scope.group && scope.group !== scopes[index - 1]?.group && (
              <p className="px-3 pb-2 pt-5 text-xs font-medium text-muted-foreground">
                {scope.group}
              </p>
            )}
            <button
              type="button"
              disabled={disabled}
              aria-current={scope.id === selectedScope ? "page" : undefined}
              onClick={() => {
                const opener = document.activeElement;
                setOpen(false);
                onScopeChange(scope.id);
                requestAnimationFrame(() => {
                  if (opener?.isConnected) return;
                  const heading = document.querySelector<HTMLElement>("main h1");
                  if (heading) {
                    heading.tabIndex = -1;
                    heading.focus({ preventScroll: true });
                  }
                });
              }}
              className="flex min-h-10 w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-sm text-muted-foreground hover:bg-accent hover:text-foreground focus-visible:outline-2 focus-visible:outline-ring aria-[current=page]:bg-accent aria-[current=page]:font-medium aria-[current=page]:text-foreground disabled:opacity-50 pointer-coarse:min-h-11"
            >
              {scope.icon}
              <span className="min-w-0 flex-1">
                <span className="block truncate">{scope.label}</span>
                {scope.description && (
                  <span className="block truncate text-xs font-normal text-muted-foreground">
                    {scope.description}
                  </span>
                )}
              </span>
              {scope.count !== undefined && (
                <span className="shrink-0 text-xs font-normal tabular-nums text-muted-foreground">
                  {scope.count}
                </span>
              )}
              {scope.id === selectedScope && <CheckIcon size={14} />}
            </button>
          </li>
        ))}
      </ul>
    </>
  );
  return (
    <div className="@container/resources min-w-0" data-slot="resource-browser">
      <div className="grid min-w-0 gap-5 @4xl/resources:grid-cols-[12rem_minmax(0,1fr)] @4xl/resources:gap-8">
        <nav aria-label={scopeLabel} className="min-w-0">
          <Button
            variant="outline"
            disabled={disabled}
            className="w-full justify-start @4xl/resources:hidden"
            aria-label={scopeLabel}
            aria-haspopup="dialog"
            aria-expanded={open}
            onClick={() => setOpen(true)}
          >
            {selected?.icon ?? <FolderIcon />}
            <span className="min-w-0 flex-1 truncate text-left">
              {selectedLabel ?? selected?.label ?? scopeLabel}
            </span>
          </Button>
          {!open && <div className="hidden @4xl/resources:block">{choices}</div>}
        </nav>
        <div className="min-w-0">{children}</div>
      </div>
      <Inspector open={open} title={scopeLabel} onClose={() => setOpen(false)}>
        {open && choices}
      </Inspector>
    </div>
  );
}
