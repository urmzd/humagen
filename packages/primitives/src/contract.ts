import type {
  ActionId,
  ActionPayload,
  ArtifactKind,
  ArtifactPhase,
  Citation,
  TaskStatus,
  Tone,
} from "@vultus/core";
import type { ComponentType, ReactNode } from "react";

/**
 * The primitive contract: the intersection of what every surface can draw.
 * Shared components are built only from these; each renderer implements all
 * of them. Props are data, not DOM: no className, no style, no event handlers.
 * Interaction goes through `action` and the renderer's ActionHandler.
 */

export interface ActionRef {
  id: ActionId;
  payload: ActionPayload;
}

export interface HeaderProps {
  text: string;
}

/**
 * Short formatted text. Standard markdown subset: bold, italic, strike, code,
 * links, and `{{time:ISO}}` tokens (see core `timeToken`) that each surface localizes.
 */
export interface TextProps {
  text: string;
}

/** Long-form model output in standard markdown. */
export interface MarkdownProps {
  text: string;
  /** Still arriving. The web shows a caret; Slack streams it with chat.appendStream instead. */
  streaming?: boolean;
}

export interface FieldsProps {
  items: { label: string; value: string }[];
}

/** Small, muted secondary line(s). */
export interface ContextProps {
  items: string[];
}

export type DividerProps = Record<string, never>;

export interface CodeProps {
  text: string;
  language?: string;
  /** File path or label shown above the code. */
  title?: string;
}

/** A shell command and its output. Surfaces keep the tail and say how much was cut. */
export interface TerminalProps {
  command: string;
  cwd?: string;
  status: TaskStatus;
  exitCode?: number;
  durationMs?: number;
  output?: string;
}

/** Severity-framed note. Slack messages have no alert block; renderers degrade. */
export interface CalloutProps {
  tone: Tone;
  title?: string;
  text: string;
}

export interface ConfirmSpec {
  title: string;
  text: string;
  confirm: string;
  deny: string;
}

export interface ButtonProps {
  label: string;
  action?: ActionRef;
  url?: string;
  style?: "primary" | "danger";
  confirm?: ConfirmSpec;
  /** Read aloud instead of the label; required when the label is ambiguous out of context. */
  accessibilityLabel?: string;
  /** Name of a Checkboxes group whose selection is sent with this action. */
  collects?: string;
}

/** A group of checkboxes whose selection rides along with a Button that names it in `collects`. */
export interface CheckboxesProps {
  name: string;
  options: { id: string; label: string; description?: string; disabled?: boolean }[];
  selected: string[];
}

/** A file the agent produced, in any phase. Renderers own icons, previews, and download affordances. */
export interface FileProps {
  id: string;
  title: string;
  kind: ArtifactKind;
  phase: ArtifactPhase;
  mimeType?: string;
  sizeBytes?: number;
  version?: number;
  url?: string;
  downloadUrl?: string;
  description?: string;
  previewText?: string;
  thumbnailUrl?: string;
  error?: string;
}

/**
 * Secondary choices behind a chevron, attached to the button before it (a split
 * button on the web, an `overflow` menu on Slack). Keep the visible set to 2-3.
 */
export interface MenuProps {
  label: string;
  items: { label: string; action: ActionRef; description?: string; danger?: boolean }[];
}

export interface ActionsProps {
  children: ReactNode;
  /** Change it when the proposal changes so clicks on an older copy can be told apart. */
  blockId?: string;
}

/**
 * The one-line point of the whole layout. Notifications and screen readers read
 * this instead of the blocks. Renders nothing visible on the web.
 */
export interface SummaryProps {
  text: string;
}

export interface TaskProps {
  id: string;
  title: string;
  status: TaskStatus;
  details?: string;
  output?: string;
  sources?: Citation[];
  /**
   * Decision controls held by the task itself (a gated tool call). Surfaces that
   * can't put controls in a task row (Slack plans) render the approval card instead.
   */
  decision?: {
    approve: {
      label: string;
      action: ActionRef;
      style?: "primary" | "danger";
      confirm?: ConfirmSpec;
    };
    decline: { label: string; action: ActionRef };
    menu?: MenuProps["items"];
  };
}

/** Groups Tasks under a title. Children must be Task primitives. */
export interface PlanProps {
  title: string;
  children: ReactNode;
}

export interface SourcesProps {
  items: Citation[];
}

/** Words for the optional comment prompt that follows a rating. */
export interface FeedbackPromptLabels {
  askUp: string;
  askDown: string;
  placeholder: string;
  send: string;
  skip: string;
  thanks: string;
  /** Quick reasons offered with a thumbs-down, in order. */
  reasons: { id: string; label: string }[];
}

export interface FeedbackProps {
  up: ActionRef;
  down: ActionRef;
  selected?: "up" | "down";
  labels?: { up: string; down: string };
  /**
   * The comment prompt shown after a rating, or `false` for none. The rating is
   * sent on the click; the prompt only adds a comment (`ActionId.feedbackComment`).
   * The web asks in place. Slack buttons can't, so the host opens `feedbackModal`.
   */
  prompt?: FeedbackPromptLabels | false;
  blockId?: string;
}

/**
 * One choice from a short list, sent the moment it changes. The renderer puts
 * the chosen option's id in the action's `option`. A Slack `static_select`.
 */
export interface SelectProps {
  label: string;
  options: { id: string; label: string }[];
  /** The option shown as chosen. Defaults to the first. */
  selected?: string;
  action: ActionRef;
}

/**
 * One line item: what it is, an optional muted line under it, and at most one
 * control at its end (a Button or a Select child). A Slack section with an accessory.
 */
export interface RowProps {
  /** Short formatted text, like Text. */
  text: string;
  detail?: string;
  /** Shown at the end when there is no control: "Connected to acme-corp". */
  status?: { tone: Tone; text: string };
  children?: ReactNode;
  /** Unique within the layout, for hosts that update one row. */
  blockId?: string;
}

/**
 * A free-text question with its send button. The web sends the typed words as
 * the action's `text`. Slack has nowhere to type outside a modal, so it shows
 * the button and the host collects the words in a form. Children (a Select, a
 * Button) sit under the field.
 */
export interface PromptProps {
  label: string;
  placeholder?: string;
  submit: { label: string; action: ActionRef };
  children?: ReactNode;
}

/** A small grid of places to go, each with a line saying what it is for. */
export interface TilesProps {
  label?: string;
  items: {
    id: string;
    label: string;
    description?: string;
    /** A count beside the label. */
    badge?: number;
    action?: ActionRef;
    url?: string;
  }[];
}

export interface TableProps {
  columns: { key: string; label: string; align?: "left" | "center" | "right" }[];
  rows: Record<string, string | number>[];
}

export interface PrimitiveProps {
  Header: HeaderProps;
  Text: TextProps;
  Markdown: MarkdownProps;
  Fields: FieldsProps;
  Context: ContextProps;
  Divider: DividerProps;
  Code: CodeProps;
  Callout: CalloutProps;
  Button: ButtonProps;
  Actions: ActionsProps;
  Task: TaskProps;
  Plan: PlanProps;
  Sources: SourcesProps;
  Feedback: FeedbackProps;
  Table: TableProps;
  Summary: SummaryProps;
  Terminal: TerminalProps;
  Checkboxes: CheckboxesProps;
  File: FileProps;
  Menu: MenuProps;
  Select: SelectProps;
  Row: RowProps;
  Prompt: PromptProps;
  Tiles: TilesProps;
}

export type PrimitiveName = keyof PrimitiveProps;

/** What a renderer supplies: one component per primitive. */
export type PrimitiveImpls = { [K in PrimitiveName]: ComponentType<PrimitiveProps[K]> };

export type SurfaceKind = "web" | "slack";

export interface Renderer {
  surface: SurfaceKind;
  /** Narrower placement within a surface, e.g. Slack "message" vs "modal". */
  placement?: string;
  impls: PrimitiveImpls;
}
