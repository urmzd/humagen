import { defaultCopy } from "@humagen/components";
import { SlidersIcon } from "@humagen/render-web";
import { useEffect, useRef, useState } from "react";
import type { ProjectView } from "./api.ts";
import { AUTO } from "./availability.ts";
import type { HelpSuggestion } from "./FindHelp.tsx";
import { SettingsMenu } from "./pages.ts";
import type { SessionImage, SessionOptions, SessionSetup } from "./sessionOptions.ts";
import { composerIconControl } from "./ui.tsx";

/*
 * A new session's settings, behind one control beside the composer's +. The
 * control is drawn with Home; its menu (SettingsMenu.tsx) is read as soon as
 * the control is, so it opens with nothing to wait for.
 */

const setup = defaultCopy.chat.setup;

export type { SessionImage, SessionOptions, SessionSetup };

/** What the settings menu sets up for a new session. */
export interface SessionSettingsProps {
  project: {
    projects: ProjectView[] | null;
    value: string | null;
    personalProject?: string;
    channel?: string;
    onChange: (name: string) => void;
    onCreated: (project: ProjectView) => void;
  };
  agents: {
    /** The agents that may answer where the session starts. Null while they load. */
    available: { name: string; description?: string }[] | null;
    mentioned: readonly string[];
    /** Who answers a message that mentions nobody, when the project names one. */
    defaultAgent?: string;
    /** Mentions an agent in the draft, or takes its mention out. */
    onMention: (name: string) => void;
    /** Describe the work to find an agent: another way to choose. */
    finder?: {
      onSearch: (description: string) => Promise<HelpSuggestion[]>;
      onChoose: (suggestion: HelpSuggestion, description: string) => void;
      draft: () => string;
    };
  };
  /** What the deployment offers a new session: undefined while it is read, null when it cannot be. */
  options: SessionOptions | null | undefined;
  setup: SessionSetup;
  onSetup: (next: SessionSetup) => void;
  /** Why the image and repositories cannot be chosen here, as for a thread in a channel. */
  fixed?: string;
  /** Where to connect GitHub when no repository is connected. */
  connectHref?: string;
  maxRepos?: number;
}

/** The words a row says for the agents: those mentioned, else who answers. */
export function agentsValue(mentioned: readonly string[], defaultAgent?: string): string {
  // Auto is no agent: each available one decides whether it answers.
  const named = defaultAgent && defaultAgent !== AUTO ? defaultAgent : undefined;
  return mentioned.length === 0 ? (named ?? setup.anyAgent) : mentioned.join(", ");
}

/**
 * The session settings control beside +: a small sliders icon that opens a
 * menu of the new session's settings. Up and Down move through the rows,
 * Right or Enter opens one, Escape steps back and then closes.
 */
export function SessionSettings(props: SessionSettingsProps) {
  const [open, setOpen] = useState(false);
  // The menu's code is read now, not on the first press.
  useEffect(() => void SettingsMenu.preload(), []);
  const button = useRef<HTMLButtonElement>(null);
  const panel = useRef<HTMLDivElement>(null);
  const close = () => {
    setOpen(false);
    button.current?.focus();
  };
  // A press outside the menu closes it, as Escape does.
  useEffect(() => {
    if (!open) return;
    const away = (e: PointerEvent) => {
      const at = e.target as Node;
      if (!panel.current?.contains(at) && !button.current?.contains(at)) setOpen(false);
    };
    document.addEventListener("pointerdown", away);
    return () => document.removeEventListener("pointerdown", away);
  }, [open]);
  return (
    <>
      <button
        ref={button}
        type="button"
        aria-label={setup.label}
        title={setup.label}
        aria-haspopup="dialog"
        aria-expanded={open}
        data-session-settings
        className={`${composerIconControl} mb-0.5 pointer-coarse:mb-0 text-aui-muted hover:text-aui-fg aria-expanded:bg-aui-hover aria-expanded:text-aui-fg`}
        onClick={() => (open ? close() : setOpen(true))}
      >
        <SlidersIcon size={18} />
      </button>
      {open && (
        <SettingsMenu
          {...props}
          panel={panel}
          onClose={close}
          onUnavailable={() => setOpen(false)}
        />
      )}
    </>
  );
}
