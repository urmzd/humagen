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

export const HourglassIcon = icon(
  <path d="M6 2h12M6 22h12M7 2v4.5a5 5 0 0 0 2.2 4.1L12 12l2.8-1.4A5 5 0 0 0 17 6.5V2M7 22v-4.5a5 5 0 0 1 2.2-4.1L12 12l2.8 1.4a5 5 0 0 1 2.2 4.1V22" />,
  "HourglassIcon",
);
export const PauseIcon = icon(<path d="M9 5v14M15 5v14" />, "PauseIcon");
export const PlayIcon = icon(<path d="M7 4.5v15l12-7.5z" />, "PlayIcon");
export const BanIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="m5.6 5.6 12.8 12.8" />
  </>,
  "BanIcon",
);
export const DownloadIcon = icon(<path d="M12 3v12M7 10l5 5 5-5M5 21h14" />, "DownloadIcon");
export const GripIcon = icon(
  <path d="M9 6h.01M9 12h.01M9 18h.01M15 6h.01M15 12h.01M15 18h.01" strokeWidth={3} />,
  "GripIcon",
);
export const BotIcon = icon(
  <>
    <rect x="4" y="8" width="16" height="12" rx="3" />
    <path d="M12 8V4M9 13v2M15 13v2" />
  </>,
  "BotIcon",
);
export const RefreshIcon = icon(
  <path d="M3 12a9 9 0 0 1 15.5-6.2L21 8M21 3v5h-5M21 12a9 9 0 0 1-15.5 6.2L3 16M3 21v-5h5" />,
  "RefreshIcon",
);
export const ClockIcon = icon(
  <>
    <circle cx="12" cy="12" r="9" />
    <path d="M12 7v5l3 2" />
  </>,
  "ClockIcon",
);
export const GaugeIcon = icon(<path d="M12 14l4-4M3.3 19a10 10 0 1 1 17.4 0" />, "GaugeIcon");
export const MegaphoneIcon = icon(
  <>
    <path d="m3 11 18-5v12L3 14v-3z" />
    <path d="M11.6 16.8a3 3 0 1 1-5.8-1.6" />
  </>,
  "MegaphoneIcon",
);
export const ShieldIcon = icon(
  <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10" />,
  "ShieldIcon",
);
export const ZapIcon = icon(<path d="M13 2 3 14h9l-1 8 10-12h-9z" />, "ZapIcon");
export const MessageIcon = icon(
  <path d="M21 15a2 2 0 0 1-2 2H7l-4 4V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2z" />,
  "MessageIcon",
);
export const EyeIcon = icon(
  <>
    <path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12" />
    <circle cx="12" cy="12" r="3" />
  </>,
  "EyeIcon",
);
export const ArrowUpDownIcon = icon(<path d="m7 15 5 5 5-5M7 9l5-5 5 5" />, "ArrowUpDownIcon");
export const PencilIcon = icon(
  <path d="M17 3a2.85 2.85 0 1 1 4 4L7.5 20.5 2 22l1.5-5.5Z" />,
  "PencilIcon",
);

export const FolderIcon = icon(
  <path d="M20 20H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2Z" />,
  "FolderIcon",
);
export const LayersIcon = icon(
  <path d="m12 3 9 5-9 5-9-5 9-5Zm-9 9 9 5 9-5M3 16l9 5 9-5" />,
  "LayersIcon",
);
export const UsersIcon = icon(
  <>
    <path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2m20 0v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75" />
    <circle cx="9" cy="7" r="4" />
  </>,
  "UsersIcon",
);
export const FolderOpenIcon = icon(
  <path d="M6 20h14l3-10H9L6 20Zm0 0H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2v1" />,
  "FolderOpenIcon",
);
export const FolderPlusIcon = icon(
  <>
    <path d="M20 20H4a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2Z" />
    <path d="M12 10v7m-3.5-3.5h7" />
  </>,
  "FolderPlusIcon",
);
export const ComposeIcon = icon(
  <>
    <path d="M12 4H5a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2h13a2 2 0 0 0 2-2v-7" />
    <path d="m16 3 5 5M10 14l-1 4 4-1L22 8a2.12 2.12 0 0 0-5-5Z" />
  </>,
  "ComposeIcon",
);
export const HistoryIcon = icon(
  <>
    <path d="M3 11a9 9 0 1 1 2.6 7.4M3 4v7h7" />
    <path d="M12 7v5l3 2" />
  </>,
  "HistoryIcon",
);
export const PanelLeftOpenIcon = icon(
  <>
    <rect x="3" y="3" width="18" height="18" rx="2" />
    <path d="M9 3v18m4-12 3 3-3 3" />
  </>,
  "PanelLeftOpenIcon",
);
export const PanelLeftCloseIcon = icon(
  <>
    <rect x="3" y="3" width="18" height="18" rx="2" />
    <path d="M9 3v18m7-12-3 3 3 3" />
  </>,
  "PanelLeftCloseIcon",
);
export const SettingsIcon = icon(
  <>
    <path d="m9 3-.6 2.4-2.1 1.2L4 6l-2 3 1.8 1.8v2.4L2 15l2 3 2.3-.6 2.1 1.2L9 21h6l.6-2.4 2.1-1.2L20 18l2-3-1.8-1.8v-2.4L22 9l-2-3-2.3.6-2.1-1.2L15 3Z" />
    <circle cx="12" cy="12" r="3" />
  </>,
  "SettingsIcon",
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
