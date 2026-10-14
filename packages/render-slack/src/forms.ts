import { defaultCopy } from "@vultus/components";
import { decodeAction, encodeAction, formatAmount, parseAmount, type UiAction } from "@vultus/core";

/**
 * The ways into an agent besides a direct message or a mention, and the forms
 * a host opens with `views.open` to collect what a button can't carry. The ids
 * are shared, so a host's manifest and the handlers that answer it can't drift.
 */
export const ENTRY = {
  /** Global shortcut: ask from anywhere, answered in the app's messages with that person. */
  ask: "aui_ask",
  /** Message shortcut: ask about one message, answered in its thread. */
  askAbout: "aui_ask_about",
  command: "/agent",
  /** Workflow Builder step: a workflow asks, and waits for the turn (and any approval) to end. */
  step: "aui_ask_step",
  /** The one text form: `askForm`, and any other words a host collects (Edit, Ask, Something Else). */
  form: "aui_form",
} as const;

/** `callback_id` of `amountModal`; match it on view_submission. */
export const AMOUNT_MODAL = "aui.amount.modal";

const plain = (text: string, max: number) => ({
  type: "plain_text",
  text: text.length <= max ? text : `${text.slice(0, max - 1)}…`,
});

/** The form's words. Defaults are `copy.askForm` from @vultus/components. */
export interface AskFormLabels {
  title: string;
  label: string;
  send: string;
  cancel: string;
}

/** private_metadata as JSON, or null when it isn't. A form never throws on what Slack sends back. */
function metadata<T>(text: string | undefined): T | null {
  if (!text) return null;
  try {
    const parsed: unknown = JSON.parse(text);
    return typeof parsed === "object" && parsed !== null ? (parsed as T) : null;
  } catch {
    return null;
  }
}

/**
 * The one-field form a shortcut opens (`ENTRY.form`). `meta` comes back on
 * submit, so the handler knows what the words are for; it defaults to
 * `{ kind: "ask" }`. Read the submission with `decodeAskForm`.
 */
export function askForm(
  options: Partial<AskFormLabels> & { initial?: string; meta?: Record<string, unknown> } = {},
) {
  const { initial, meta = { kind: "ask" }, ...labels } = options;
  const l = { ...defaultCopy.askForm, ...labels };
  const metadata = JSON.stringify(meta);
  if (metadata.length > 3000)
    throw new RangeError("private_metadata is limited to 3000 characters");
  return {
    type: "modal",
    callback_id: ENTRY.form,
    private_metadata: metadata,
    title: plain(l.title, 24),
    submit: plain(l.send, 24),
    close: plain(l.cancel, 24),
    blocks: [
      {
        type: "input",
        block_id: "text",
        label: plain(l.label, 2000),
        element: {
          type: "plain_text_input",
          action_id: "value",
          multiline: true,
          ...(initial ? { initial_value: initial.slice(0, 3000) } : {}),
        },
      },
    ],
  };
}

/** The words and `meta` of a submitted `askForm`, or null when it isn't one or was sent empty. */
export function decodeAskForm<M = { kind: string }>(view: {
  callback_id?: string;
  private_metadata?: string;
  state?: { values?: Record<string, Record<string, { value?: string | null }>> };
}): { text: string; meta: M } | null {
  if (view.callback_id !== ENTRY.form) return null;
  const text = view.state?.values?.text?.value?.value?.trim() ?? "";
  if (!text) return null;
  const meta = view.private_metadata ? metadata<M>(view.private_metadata) : ({} as M);
  return meta ? { text, meta } : null;
}

/** The modal's words. Defaults come from `defaultCopy` in @vultus/components. */
export interface AmountModalLabels {
  title: string;
  label: string;
  submit: string;
  cancel: string;
  /** Said under the field when there is a lower bound: "More than $50.00". */
  moreThan: (amount: string) => string;
  /** The label with its unit: "New limit ($)". */
  withUnit: (label: string, unit: string) => string;
}

const amountLabels = (): AmountModalLabels => ({
  title: defaultCopy.raiseLimitTitle,
  label: defaultCopy.newLimit,
  submit: defaultCopy.raiseLimit,
  cancel: defaultCopy.cancel,
  moreThan: defaultCopy.moreThan,
  withUnit: defaultCopy.amountLabel,
});

/** The smallest step a unit takes: a cent for money, one for counts. */
function step(unit: string) {
  return unit === "usd" ? 0.01 : 1;
}

/**
 * The form an `Amount` button opens on Slack, where a message has no number
 * field: one `number_input`, bounded like the web's field. `action` is the
 * clicked action as `decodeSlackAction` returned it; `decodeAmountModal`
 * gives it back with the number in `option`. Open it straight away: the
 * trigger_id lasts about three seconds.
 */
export function amountModal(
  {
    action,
    unit,
    above,
    max,
    initial,
  }: { action: UiAction; unit: string; above?: number; max?: number; initial?: number },
  labels: Partial<AmountModalLabels> = {},
) {
  const l = { ...amountLabels(), ...labels };
  const decimal = unit === "usd";
  const min = above === undefined ? undefined : above + step(unit);
  const fixed = (n: number) => (decimal ? n.toFixed(2) : String(Math.round(n)));
  return {
    type: "modal",
    callback_id: AMOUNT_MODAL,
    // The action to send, ids only, never content.
    private_metadata: JSON.stringify({
      id: action.id,
      p: encodeAction(action.payload),
      unit,
      above,
      max,
    }),
    title: plain(l.title, 24),
    submit: plain(l.submit, 24),
    close: plain(l.cancel, 24),
    blocks: [
      {
        type: "input",
        block_id: "amount",
        label: plain(l.withUnit(l.label, unit), 2000),
        ...(above !== undefined
          ? { hint: plain(l.moreThan(formatAmount(above, unit)), 2000) }
          : {}),
        element: {
          type: "number_input",
          action_id: "value",
          is_decimal_allowed: decimal,
          ...(min !== undefined ? { min_value: fixed(min) } : {}),
          ...(max !== undefined ? { max_value: fixed(max) } : {}),
          ...(initial !== undefined ? { initial_value: fixed(initial) } : {}),
        },
      },
    ],
  };
}

/**
 * The action a submitted `amountModal` means, with the number in `option`, or
 * null when it isn't one, or the number isn't a plain amount in the unit
 * (cents at most for money, whole for counts) above the lower bound and
 * within the upper one. Never throws on what Slack sends back.
 */
export function decodeAmountModal(view: {
  callback_id?: string;
  private_metadata?: string;
  state?: { values?: Record<string, Record<string, { value?: string | null }>> };
}): UiAction | null {
  if (view.callback_id !== AMOUNT_MODAL) return null;
  const meta = metadata<{
    id?: unknown;
    p?: unknown;
    unit?: unknown;
    above?: unknown;
    max?: unknown;
  }>(view.private_metadata);
  if (!meta || typeof meta.id !== "string" || typeof meta.p !== "string") return null;
  if (typeof meta.unit !== "string") return null;
  const n = parseAmount(view.state?.values?.amount?.value?.value ?? "", meta.unit);
  if (n === undefined) return null;
  if (typeof meta.above === "number" && n <= meta.above) return null;
  if (typeof meta.max === "number" && n > meta.max) return null;
  let payload: UiAction["payload"];
  try {
    payload = decodeAction(meta.p);
  } catch {
    return null;
  }
  return { id: meta.id as UiAction["id"], payload: { ...payload, option: String(n) } };
}
