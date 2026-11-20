import { defaultCopy } from "@humagen/components";
import { MaximizeIcon, MinimizeIcon } from "@humagen/render-web";
import { Inspector } from "@humagen/web";
import { useId } from "react";
import type { Run } from "./adapter.ts";
import { useHumanActivity } from "./humanActivity.tsx";
import { LazyFrame } from "./lazy.tsx";
import type { TraceLoader } from "./responseSource.ts";
import type { ConversationView } from "./route.ts";
import { useSessionSummary } from "./SessionPanel.tsx";
import type { SavedFile } from "./sessionSummary.ts";
import type { Reference as Ref } from "./trace.ts";
import { SkeletonRows, tabItem, tabRule, tabStrip, useStrip } from "./ui.tsx";
import {
  activeBench,
  openBench,
  WORKBENCH_TABS,
  type WorkbenchSession,
  type WorkbenchTab,
  workbenchTabs,
} from "./workbench.ts";

const words = defaultCopy.workbench;

// The one tab look (ui.tsx), a step larger: a tab's name here is read at a
// glance beside a terminal or a browser, and nothing in the workbench is set
// under 14px.
const benchTab = tabItem.replace("text-aui-sm", "text-aui-base");

/**
 * The session's workbench: the pane Details opens, as tabs. Details is the
 * first tab and the pane's own address; each other tab is `?bench=`, with
 * what it has selected in `?benchRef=`. With only Details to show there is no
 * strip, and the pane is the Details it always was.
 *
 * Details keeps the pane's narrow width. Any other tab is wide, since a
 * terminal or a browser needs the room, and the pane can fill the page
 * (`?full=1`). On a phone it is the Inspector's sheet with the same tabs.
 */
export function SessionWorkbench({
  runs,
  thread,
  uploads,
  origin,
  canPost = false,
  view,
  onView,
  leaving,
  onClose,
  onOpenFile,
  onAsk,
  loader,
  files,
  tabs = WORKBENCH_TABS,
}: {
  runs: Run[];
  thread: string;
  uploads?: string[];
  origin?: Ref;
  /** Whether the reader may post here. */
  canPost?: boolean;
  /** The conversation's view: the open tab and its selection are read from it. */
  view: ConversationView;
  /** Shows a view of this pane: another tab, a selection, the pane filling the page. */
  onView: (view: ConversationView) => void;
  leaving?: boolean;
  onClose: () => void;
  onOpenFile?: (artifactId: string) => void;
  /** Puts words in the composer for the reader to send. */
  onAsk?: (text: string) => void;
  loader?: TraceLoader;
  files?: (thread: string) => Promise<{ files: SavedFile[]; sandbox?: string }>;
  /** The registry, for a test to stand in for. */
  tabs?: readonly WorkbenchTab[];
}) {
  const { responses, summary } = useSessionSummary({ runs, thread, uploads, loader, files });
  const { record } = useHumanActivity(thread);
  const panel = useId();
  const session: WorkbenchSession = { thread, runs, responses, summary, canPost };
  const shown = workbenchTabs(session, view.bench, tabs);
  const active = activeBench(view, shown);
  const strip = useStrip<HTMLDivElement>(active?.id);
  if (!active) return null;
  const expanded = !!view.expanded;
  const View = active.view;
  const body = active.fill ? "flex min-h-0 min-w-0 flex-1 flex-col" : "min-w-0";
  const content = (
    <LazyFrame bare resetKey={active.id} fallback={<SkeletonRows count={4} />}>
      <View
        {...session}
        origin={origin}
        selection={view.bench === active.id ? view.benchRef : undefined}
        onSelection={(ref) => onView(openBench(view, active.id, ref))}
        openTab={(id, ref) => onView(openBench(view, id, ref))}
        openFile={onOpenFile}
        record={record}
        ask={canPost ? onAsk : undefined}
        expanded={expanded}
      />
    </LazyFrame>
  );
  return (
    <Inspector
      open={!leaving}
      title={active.label}
      onClose={onClose}
      wide={active.id !== "details"}
      expanded={expanded}
      fill={!!active.fill}
      actions={
        // Below the dock's breakpoint the pane is a sheet that already has the screen.
        <button
          type="button"
          aria-label={expanded ? words.collapse : words.expand}
          title={expanded ? words.collapse : words.expand}
          aria-pressed={expanded}
          onClick={() =>
            onView({
              ...openBench(view, active.id, view.benchRef),
              expanded: !expanded || undefined,
            })
          }
          className="inline-flex size-9 items-center justify-center rounded-aui text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-pressed:text-aui-fg max-lg:hidden"
        >
          {expanded ? <MinimizeIcon size={16} /> : <MaximizeIcon size={16} />}
        </button>
      }
      tabs={
        shown.length > 1 ? (
          <div className="relative" data-testid="workbench-tabs">
            <span aria-hidden className={tabRule} />
            <div ref={strip} role="tablist" aria-label={words.tabs} className={tabStrip}>
              {shown.map((tab) => (
                <button
                  key={tab.id}
                  type="button"
                  role="tab"
                  id={`${panel}-${tab.id}`}
                  aria-selected={tab.id === active.id}
                  aria-controls={panel}
                  data-bench={tab.id}
                  onClick={() => onView(openBench(view, tab.id))}
                  className={benchTab}
                >
                  <tab.icon size={16} />
                  {tab.label}
                </button>
              ))}
            </div>
          </div>
        ) : undefined
      }
    >
      {shown.length > 1 ? (
        <div
          id={panel}
          role="tabpanel"
          aria-labelledby={`${panel}-${active.id}`}
          data-testid="workbench-tab"
          data-bench={active.id}
          className={body}
        >
          {content}
        </div>
      ) : (
        <div data-testid="workbench-tab" data-bench={active.id} className={body}>
          {content}
        </div>
      )}
    </Inspector>
  );
}
