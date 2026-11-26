import { ChevronDownIcon } from "@humagen/render-web";
import { Button } from "@humagen/render-web/ui/button";
import { ButtonGroup } from "@humagen/render-web/ui/button-group";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@humagen/render-web/ui/dropdown-menu";
import type { ReactNode } from "react";

/** One choice under a split button's arrow. */
export interface SplitChoice {
  label: string;
  onSelect: () => void;
  /** Said apart, after a rule, in the failure color: Remove, Delete. */
  destructive?: boolean;
  disabled?: boolean;
}

/**
 * One action and the choices that go with it, as one control: the button does
 * the usual thing, the arrow beside it opens the rest. A secondary action is
 * never a second row of buttons. With no choices it is a plain button.
 */
export function SplitButton({
  label,
  onClick,
  choices = [],
  menuLabel,
  primary = false,
  disabled = false,
  busy,
  icon,
  name,
}: {
  label: string;
  /** The button's full name where its label alone is not enough, such as "Install Review". */
  name?: string;
  onClick: () => void;
  choices?: SplitChoice[];
  /** The arrow's name for assistive technology, such as "More for Review". */
  menuLabel: string;
  /** The page's one filled button; others are outlined. */
  primary?: boolean;
  disabled?: boolean;
  /** Said in place of the label while the action runs. */
  busy?: string;
  icon?: ReactNode;
}) {
  const variant = primary ? "default" : "outline";
  const safe = choices.filter((c) => !c.destructive);
  const risky = choices.filter((c) => c.destructive);
  const main = (
    <Button
      size="sm"
      variant={variant}
      aria-label={busy ? undefined : name}
      disabled={disabled || !!busy}
      onClick={onClick}
    >
      {icon}
      {busy ?? label}
    </Button>
  );
  if (choices.length === 0) return main;
  return (
    <ButtonGroup>
      {main}
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button
            size="sm"
            variant={variant}
            aria-label={menuLabel}
            disabled={!!busy}
            className="px-2"
          >
            <ChevronDownIcon size={14} aria-hidden />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {safe.map((c) => (
            <DropdownMenuItem key={c.label} disabled={c.disabled} onSelect={c.onSelect}>
              {c.label}
            </DropdownMenuItem>
          ))}
          {safe.length > 0 && risky.length > 0 && <DropdownMenuSeparator />}
          {risky.map((c) => (
            <DropdownMenuItem
              key={c.label}
              disabled={c.disabled}
              onSelect={c.onSelect}
              className="text-aui-bad focus:text-aui-bad"
            >
              {c.label}
            </DropdownMenuItem>
          ))}
        </DropdownMenuContent>
      </DropdownMenu>
    </ButtonGroup>
  );
}
