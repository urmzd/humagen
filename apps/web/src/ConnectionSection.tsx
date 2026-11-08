import type { ComponentProps } from "react";
import { Page } from "./ui.tsx";

/** Reuse connection flows in their canonical page without nesting page titles. */
export function ConnectionSection({
  embedded,
  ...props
}: ComponentProps<typeof Page> & { embedded?: boolean }) {
  return embedded ? (
    <section
      className="flex min-w-0 flex-col gap-4"
      aria-label={typeof props.title === "string" ? props.title : undefined}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-aui-base font-medium">{props.title}</h2>
        {props.actions}
      </div>
      {props.children}
    </section>
  ) : (
    <Page {...props} />
  );
}
