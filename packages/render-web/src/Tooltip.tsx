import {
  cloneElement,
  type HTMLAttributes,
  type ReactElement,
  useEffect,
  useId,
  useState,
} from "react";

/**
 * A label for an icon control, visible on hover and keyboard focus: beside it
 * (`right`, the default, for a rail) or above it (`top`, for a control at the
 * page's edge), kept inside the viewport.
 */
export function Tooltip({
  label,
  side = "right",
  children,
}: {
  label: string;
  side?: "right" | "top";
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
    if (side === "top") {
      // Centred over the control; half the widest tooltip (max-w-56) plus a gutter keeps it on screen.
      const half = 112 + 12;
      const viewport = document.documentElement.clientWidth || window.innerWidth;
      setPosition({
        left: Math.min(Math.max(rect.left + rect.width / 2, half), viewport - half),
        top: Math.max(rect.top - 8, 40),
      });
      return;
    }
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
          data-side={side}
          className={`pointer-events-none fixed z-[100] max-w-56 ${side === "top" ? "w-max -translate-x-1/2 -translate-y-full text-center" : "-translate-y-1/2"} rounded-aui border border-aui-border bg-aui-pop px-2.5 py-1.5 text-aui-xs font-medium text-aui-fg shadow-lg`}
          style={position}
        >
          {label}
        </span>
      )}
    </span>
  );
}
