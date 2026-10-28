import { defaultCopy } from "@humagen/components";
import {
  ChevronRightIcon,
  DatabaseIcon,
  FileIcon,
  GoogleDriveIcon,
  PlugIcon,
  SlackIcon,
} from "@humagen/render-web";
import { Badge } from "@humagen/render-web/ui/badge";

export type SourceChoice = "manual" | "drive" | "notion";

/** Display only native creation paths as available; tool access is a separate capability. */
export function SourceCatalog({ onSelect }: { onSelect: (provider: SourceChoice) => void }) {
  const copy = defaultCopy.sourceCatalog;
  return (
    <div className="space-y-6">
      <p className="text-sm leading-6 text-aui-muted">{copy.description}</p>
      <section className="space-y-2" aria-label={copy.available}>
        {(
          [
            {
              id: "manual",
              title: copy.manual,
              description: copy.manualHint,
              icon: <FileIcon size={20} />,
            },
            {
              id: "drive",
              title: copy.drive,
              description: copy.driveHint,
              icon: <GoogleDriveIcon size={20} className="text-[#4285F4]" />,
            },
            {
              id: "notion",
              title: copy.notion,
              description: copy.notionHint,
              icon: <FileIcon size={20} />,
            },
          ] as const
        ).map((provider) => (
          <button
            key={provider.id}
            type="button"
            onClick={() => onSelect(provider.id)}
            className="flex min-h-20 w-full items-start gap-3 rounded-aui-lg border border-aui-border p-4 text-left transition-colors hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent motion-reduce:transition-none"
          >
            <span className="mt-1 shrink-0 text-aui-muted">{provider.icon}</span>
            <span className="min-w-0 flex-1">
              <span className="block text-sm font-medium">{provider.title}</span>
              <span className="mt-1 block text-sm leading-6 text-aui-muted">
                {provider.description}
              </span>
            </span>
            <ChevronRightIcon size={16} className="mt-1 shrink-0 text-aui-muted" />
          </button>
        ))}
      </section>
      <section
        className="space-y-3 border-t border-aui-border pt-4"
        aria-label={copy.moreProviders}
      >
        {["Snowflake", "BigQuery"].map((provider) => (
          <div key={provider} className="flex items-start gap-3">
            <DatabaseIcon size={18} className="mt-1 shrink-0 text-aui-muted" />
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2">
                <h3 className="text-sm font-medium">{provider}</h3>
                <Badge variant="outline">{copy.connectorRequired}</Badge>
              </div>
              <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.warehouseHint}</p>
            </div>
          </div>
        ))}
        <div className="flex items-start gap-3">
          <SlackIcon size={18} className="mt-1 shrink-0" />
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <h3 className="text-sm font-medium">Slack</h3>
              <Badge variant="outline">{copy.connectorRequired}</Badge>
            </div>
            <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.slackHint}</p>
          </div>
        </div>
        <div className="flex items-start gap-3">
          <PlugIcon size={18} className="mt-1 shrink-0 text-aui-muted" />
          <div>
            <h3 className="text-sm font-medium">{copy.mcp}</h3>
            <p className="mt-1 text-sm leading-6 text-aui-muted">{copy.mcpHint}</p>
          </div>
        </div>
      </section>
      <section className="space-y-2 border-t border-aui-border pt-4">
        <h3 className="text-sm font-medium">{copy.refreshTitle}</h3>
        <p className="text-sm leading-6 text-aui-muted">{copy.refreshHint}</p>
        <p className="text-sm leading-6 text-aui-muted">{copy.formatsHint}</p>
      </section>
    </div>
  );
}
