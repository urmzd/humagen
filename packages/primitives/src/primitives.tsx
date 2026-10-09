import { type ComponentType, createContext, type ReactNode, useContext } from "react";
import type { PrimitiveName, PrimitiveProps, Renderer, SurfaceKind } from "./contract";

const RendererContext = createContext<Renderer | null>(null);

export function RendererProvider({
  renderer,
  children,
}: {
  renderer: Renderer;
  children: ReactNode;
}) {
  return <RendererContext.Provider value={renderer}>{children}</RendererContext.Provider>;
}

export function useRenderer(): Renderer {
  const renderer = useContext(RendererContext);
  if (!renderer) {
    throw new Error(
      "agent-ui primitives need a renderer: wrap in <WebSurface> or use renderToSlack",
    );
  }
  return renderer;
}

/** Which surface is drawing. Use for small per-surface variations inside a shared component. */
export function useSurface(): { surface: SurfaceKind; placement?: string } {
  const { surface, placement } = useRenderer();
  return { surface, placement };
}

/** Escape hatch: render children only on the given surface(s). */
export function Only({
  surface,
  children,
}: {
  surface: SurfaceKind | SurfaceKind[];
  children: ReactNode;
}) {
  const current = useRenderer().surface;
  const allowed = Array.isArray(surface) ? surface : [surface];
  return allowed.includes(current) ? children : null;
}

function primitive<K extends PrimitiveName>(name: K) {
  function Primitive(props: PrimitiveProps[K]) {
    const Impl = useRenderer().impls[name] as ComponentType<PrimitiveProps[K]>;
    return <Impl {...props} />;
  }
  Primitive.displayName = name;
  return Primitive;
}

export const Header = primitive("Header");
export const Text = primitive("Text");
export const Markdown = primitive("Markdown");
export const Fields = primitive("Fields");
export const Context = primitive("Context");
export const Divider = primitive("Divider");
export const Code = primitive("Code");
export const Callout = primitive("Callout");
export const Button = primitive("Button");
export const Actions = primitive("Actions");
export const Task = primitive("Task");
export const Plan = primitive("Plan");
export const Sources = primitive("Sources");
export const Feedback = primitive("Feedback");
export const Table = primitive("Table");
export const Summary = primitive("Summary");
export const Terminal = primitive("Terminal");
export const Checkboxes = primitive("Checkboxes");
export const File = primitive("File");
export const Menu = primitive("Menu");
