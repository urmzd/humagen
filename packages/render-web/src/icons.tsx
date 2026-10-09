import type { ReactNode, SVGProps } from "react";

/**
 * A small inline icon set (lucide geometry, 24px grid, 2px stroke). Inline so
 * the renderer has no icon dependency and every glyph sits in a square box:
 * a rotating icon rotates around its true center.
 */
type IconProps = SVGProps<SVGSVGElement> & { size?: number };

function icon(paths: ReactNode, name: string) {
  function Icon({ size = 16, className, ...rest }: IconProps) {
    return (
      <svg
        aria-hidden="true"
        focusable="false"
        width={size}
        height={size}
        viewBox="0 0 24 24"
        fill="none"
        stroke="currentColor"
        strokeWidth={2}
        strokeLinecap="round"
        strokeLinejoin="round"
        className={`shrink-0 ${className ?? ""}`}
        {...rest}
      >
        {paths}
      </svg>
    );
  }
  Icon.displayName = name;
  return Icon;
}

export const CheckIcon = icon(<path d="M20 6 9 17l-5-5" />, "CheckIcon");
export const CheckCircleIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="m8.5 12 2.5 2.5 4.5-5" />
  </>,
  "CheckCircleIcon",
);
export const XCircleIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="m15 9-6 6M9 9l6 6" />
  </>,
  "XCircleIcon",
);
export const CircleIcon = icon(<circle cx="12" cy="12" r="8" />, "CircleIcon");
export const XIcon = icon(<path d="M18 6 6 18M6 6l12 12" />, "XIcon");
export const ChevronDownIcon = icon(<path d="m6 9 6 6 6-6" />, "ChevronDownIcon");
export const ChevronRightIcon = icon(<path d="m9 18 6-6-6-6" />, "ChevronRightIcon");
export const CopyIcon = icon(
  <>
    <rect x="9" y="9" width="12" height="12" rx="2" />
    <path d="M5 15H4a1 1 0 0 1-1-1V4a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v1" />
  </>,
  "CopyIcon",
);
export const InfoIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 16v-4M12 8h.01" />
  </>,
  "InfoIcon",
);
export const AlertTriangleIcon = icon(
  <>
    <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3" />
    <path d="M12 9v4M12 17h.01" />
  </>,
  "AlertTriangleIcon",
);
export const AlertOctagonIcon = icon(
  <>
    <path d="M7.86 2h8.28L22 7.86v8.28L16.14 22H7.86L2 16.14V7.86z" />
    <path d="M12 8v4M12 16h.01" />
  </>,
  "AlertOctagonIcon",
);
export const ThumbsUpIcon = icon(
  <>
    <path d="M7 10v12" />
    <path d="M15 5.88 14 10h5.83a2 2 0 0 1 1.92 2.56l-2.33 8A2 2 0 0 1 17.5 22H4a2 2 0 0 1-2-2v-8a2 2 0 0 1 2-2h2.76a2 2 0 0 0 1.79-1.11L12 2a3.13 3.13 0 0 1 3 3.88" />
  </>,
  "ThumbsUpIcon",
);
export const ThumbsDownIcon = icon(
  <>
    <path d="M17 14V2" />
    <path d="M9 18.12 10 14H4.17a2 2 0 0 1-1.92-2.56l2.33-8A2 2 0 0 1 6.5 2H20a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2h-2.76a2 2 0 0 0-1.79 1.11L12 22a3.13 3.13 0 0 1-3-3.88" />
  </>,
  "ThumbsDownIcon",
);
export const FileIcon = icon(
  <>
    <path d="M15 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V7z" />
    <path d="M14 2v5h6" />
  </>,
  "FileIcon",
);
export const TerminalIcon = icon(<path d="m4 17 6-6-6-6M12 19h8" />, "TerminalIcon");
export const ExternalLinkIcon = icon(
  <path d="M15 3h6v6M10 14 21 3M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6" />,
  "ExternalLinkIcon",
);
export const ListChecksIcon = icon(
  <path d="m3 17 2 2 4-4M3 7l2 2 4-4M13 6h8M13 12h8M13 18h8" />,
  "ListChecksIcon",
);
export const BrainIcon = icon(
  <>
    <path d="M12 5a3 3 0 1 0-5.997.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z" />
    <path d="M12 5a3 3 0 1 1 5.997.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z" />
    <path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4" />
  </>,
  "BrainIcon",
);
export const ArrowUpIcon = icon(<path d="m5 12 7-7 7 7M12 19V5" />, "ArrowUpIcon");
export const ArrowDownIcon = icon(<path d="M12 5v14M19 12l-7 7-7-7" />, "ArrowDownIcon");
export const PaperclipIcon = icon(
  <path d="m21.44 11.05-9.19 9.19a6 6 0 0 1-8.49-8.49l8.57-8.57A4 4 0 1 1 18 8.84l-8.59 8.57a2 2 0 0 1-2.83-2.83l8.49-8.48" />,
  "PaperclipIcon",
);
export const SparkleIcon = icon(
  <path d="M12 3l1.9 5.8L19.7 10.7 13.9 12.6 12 18.4 10.1 12.6 4.3 10.7 10.1 8.8z" />,
  "SparkleIcon",
);

/**
 * In-progress ring. The SVG box itself rotates, so the ring spins in place
 * around the center of a square box instead of orbiting a text baseline.
 */
export function Spinner({ size = 16, className }: { size?: number; className?: string }) {
  return (
    <svg
      aria-hidden="true"
      focusable="false"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      className={`aui-spinner block shrink-0 ${className ?? ""}`}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.2" strokeWidth="2.5" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="2.5"
        strokeLinecap="round"
      />
    </svg>
  );
}
