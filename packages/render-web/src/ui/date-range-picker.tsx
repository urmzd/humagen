import { Popover } from "radix-ui";
import { type CSSProperties, useId } from "react";
import { DayPicker, getDefaultClassNames } from "react-day-picker";
import "react-day-picker/style.css";
import { CalendarIcon } from "../icons.tsx";
import { Button } from "./button.tsx";
import { Input } from "./input.tsx";
import { Label } from "./label.tsx";

// DayPicker's unlayered defaults win over Tailwind utility declarations.
// Set its public variables inline so calendar state follows the active theme.
const calendarStyle = {
  "--rdp-accent-color": "var(--primary)",
  "--rdp-accent-background-color": "var(--accent)",
  "--rdp-today-color": "var(--foreground)",
  "--rdp-day-width": "2.25rem",
  "--rdp-day-height": "2.25rem",
  "--rdp-day_button-width": "2.25rem",
  "--rdp-day_button-height": "2.25rem",
  "--rdp-range_start-color": "var(--primary-foreground)",
  "--rdp-range_end-color": "var(--primary-foreground)",
  "--rdp-range_middle-color": "var(--accent-foreground)",
  "--rdp-range_middle-background-color": "var(--accent)",
  width: "100%",
} as CSSProperties;
const calendarClasses = getDefaultClassNames();
const calendarFocus =
  "focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-ring";

/** Local wall-clock strings deliberately remain local until the host submits. */
export function DateRangePicker({
  from,
  to,
  onChange,
  labels,
}: {
  from: string;
  to: string;
  onChange: (from: string, to: string) => void;
  labels: { label: string; empty: string; from: string; to: string; time: string; clear: string };
}) {
  const id = useId();
  const local = (date: Date, time: string) =>
    `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}T${time}`;
  const selected = from ? { from: new Date(from), to: to ? new Date(to) : undefined } : undefined;
  const format = (value: string) =>
    new Date(value).toLocaleString(undefined, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
    });
  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <Button
          variant="outline"
          className="w-full justify-start font-normal"
          aria-label={labels.label}
        >
          <CalendarIcon aria-hidden="true" />
          <span className="truncate">
            {from ? `${format(from)}${to ? ` – ${format(to)}` : ""}` : labels.empty}
          </span>
        </Button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content
          align="start"
          sideOffset={8}
          collisionPadding={12}
          aria-label={labels.label}
          className="z-50 w-[22rem] max-h-[min(85dvh,38rem,var(--radix-popover-content-available-height))] max-w-[calc(100vw-1.5rem)] overflow-auto overscroll-contain rounded-xl border border-border bg-popover p-3 text-popover-foreground shadow-lg outline-none"
        >
          <DayPicker
            mode="range"
            selected={selected}
            defaultMonth={selected?.from}
            max={31}
            autoFocus
            onSelect={(range) =>
              onChange(
                range?.from ? local(range.from, from.split("T")[1] || "00:00") : "",
                range?.to ? local(range.to, to.split("T")[1] || "23:59") : "",
              )
            }
            style={calendarStyle}
            styles={{
              months: { maxWidth: "100%", justifyContent: "center" },
              month_caption: { fontSize: "0.875rem", fontWeight: 500 },
              selected: { fontSize: "inherit", fontWeight: 500 },
            }}
            classNames={{
              day_button: `${calendarClasses.day_button} ${calendarFocus}`,
              button_previous: `${calendarClasses.button_previous} ${calendarFocus}`,
              button_next: `${calendarClasses.button_next} ${calendarFocus}`,
            }}
            className="text-sm"
          />
          <div className="mt-3 grid grid-cols-1 gap-3 border-t border-border pt-3 min-[360px]:grid-cols-2">
            {(
              [
                ["from", labels.from, from],
                ["to", labels.to, to],
              ] as const
            ).map(([key, label, value]) => (
              <div key={key} className="min-w-0 space-y-2">
                <Label htmlFor={`${id}-${key}`}>{label}</Label>
                <Input
                  id={`${id}-${key}`}
                  type="date"
                  className="px-2 text-sm"
                  value={value.split("T")[0]}
                  onChange={(event) => {
                    const next = event.target.value
                      ? `${event.target.value}T${value.split("T")[1] || (key === "from" ? "00:00" : "23:59")}`
                      : "";
                    onChange(key === "from" ? next : from, key === "to" ? next : to);
                  }}
                />
                <Input
                  aria-label={`${label} ${labels.time}`}
                  type="time"
                  className="px-2 text-sm"
                  value={value.split("T")[1] || ""}
                  disabled={!value}
                  onChange={(event) => {
                    const next = `${value.split("T")[0]}T${event.target.value || "00:00"}`;
                    onChange(key === "from" ? next : from, key === "to" ? next : to);
                  }}
                />
              </div>
            ))}
          </div>
          <Button variant="ghost" size="sm" className="mt-2" onClick={() => onChange("", "")}>
            {labels.clear}
          </Button>
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}
