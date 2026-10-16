import {
  cloneElement,
  type HTMLAttributes,
  type ReactElement,
  useEffect,
  useId,
  useState,
} from "react";

/** A label for an icon control, visible on hover and keyboard focus. */
export function Tooltip({
  label,
  children,
}: {
  label: string;
  children: ReactElement<HTMLAttributes<HTMLElement>>;
}) {
  const id = useId();
  const [position, setPosition] = useState<{ left: number; top: number } | null>(null);
  useEffect(() => {
    if (!position) return;
    const hide = () => setPosition(null);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("resize", hide);
    return () => {
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("resize", hide);
    };
  }, [position]);
  const show = (element: HTMLElement) => {
    const rect = element.getBoundingClientRect();
    setPosition({
      left: rect.right + 10,
      top: Math.min(Math.max(rect.top + rect.height / 2, 24), window.innerHeight - 24),
    });
  };
  return (
    <span className="inline-flex min-w-0">
      {cloneElement(children, {
        "aria-describedby": position
          ? [children.props["aria-describedby"], id].filter(Boolean).join(" ")
          : children.props["aria-describedby"],
        onMouseEnter: (e) => {
          show(e.currentTarget);
          children.props.onMouseEnter?.(e);
        },
        onMouseLeave: (e) => {
          setPosition(null);
          children.props.onMouseLeave?.(e);
        },
        onFocus: (e) => {
          show(e.currentTarget);
          children.props.onFocus?.(e);
        },
        onBlur: (e) => {
          setPosition(null);
          children.props.onBlur?.(e);
        },
        onClick: (e) => {
          setPosition(null);
          children.props.onClick?.(e);
        },
        onKeyDown: (e) => {
          if (e.key === "Escape") setPosition(null);
          children.props.onKeyDown?.(e);
        },
      })}
      {position && (
        <span
          id={id}
          role="tooltip"
          className="pointer-events-none fixed z-[100] max-w-56 -translate-y-1/2 rounded-aui border border-aui-border bg-aui-pop px-2.5 py-1.5 text-aui-xs font-medium text-aui-fg shadow-lg"
          style={position}
        >
          {label}
        </span>
      )}
    </span>
  );
}
