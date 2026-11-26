/**
 * The Humagen mark, kept as a vector so it follows the current theme: three
 * squares in ink and one in the brand's color, the agent that joins the team.
 */
export function HumagenMark({ size = 24 }: { size?: number }) {
  return (
    <svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true" fill="currentColor">
      <rect x="5" y="5" width="9" height="9" rx="2.5" />
      <rect x="18" y="5" width="9" height="9" rx="2.5" />
      <rect x="5" y="18" width="9" height="9" rx="2.5" />
      <rect x="18" y="18" width="9" height="9" rx="2.5" fill="var(--aui-accent)" />
    </svg>
  );
}
