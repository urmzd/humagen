import type { ReactNode } from "react";
import { Label } from "@humagen/render-web/ui/label";

/** Shared reading order for settings: what it changes, then the control. */
export function SettingsFormRow({
  id,
  label,
  description,
  icon,
  compact,
  children,
}: {
  id: string;
  label: string;
  description?: string;
  icon?: ReactNode;
  compact?: boolean;
  children: ReactNode;
}) {
  return (
    <div
      className={`flex gap-4 border-b border-aui-border py-4 last:border-b-0 ${compact ? "items-start" : "flex-col sm:flex-row sm:items-start"}`}
    >
      <div className="flex min-w-0 flex-1 items-start gap-3">
        {icon && <span className="mt-1 shrink-0 text-aui-muted">{icon}</span>}
        <div className="min-w-0">
          <Label htmlFor={id} className="min-h-7 text-sm font-medium">
            {label}
          </Label>
          {description && (
            <p id={`${id}-hint`} className="mt-1 max-w-[60ch] text-sm leading-6 text-aui-muted">
              {description}
            </p>
          )}
        </div>
      </div>
      <div
        className={compact ? "shrink-0 pt-1" : "w-full min-w-0 sm:w-[min(48%,22rem)] sm:shrink-0"}
      >
        {children}
      </div>
    </div>
  );
}
