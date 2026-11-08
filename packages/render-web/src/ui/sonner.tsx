import { Toaster as Sonner, type ToasterProps, toast } from "sonner";

/**
 * shadcn/ui Toaster on Sonner: short confirmations and failures from an action
 * (connected, disconnected, could not save) show here and go away, instead of
 * as text left on the page.
 */
function Toaster(props: ToasterProps) {
  return (
    <Sonner
      theme="system"
      className="toaster group"
      style={
        {
          "--normal-bg": "var(--popover)",
          "--normal-text": "var(--popover-foreground)",
          "--normal-border": "var(--border)",
        } as React.CSSProperties
      }
      {...props}
    />
  );
}

export { Toaster, toast };
