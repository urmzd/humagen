import { defaultCopy } from "@humagen/components";
import { CheckIcon } from "@humagen/render-web";
import type { MessagePick } from "./api.ts";
import { modelName, type Offers } from "./modelPick.ts";
import { Popover } from "./ui.tsx";

/**
 * The model and reasoning effort for the next message alone, as a text
 * control under the composer (DES-067). It offers what the agents the
 * message may start offer, and renders nothing when they offer nothing.
 * Each agent applies only the picks it offers.
 */
export function ModelPicker({
  offers,
  pick,
  onPick,
}: {
  offers: Offers;
  pick: MessagePick;
  onPick: (pick: MessagePick) => void;
}) {
  const words = defaultCopy.pick;
  if (offers.models.length === 0 && offers.efforts.length === 0) return null;
  const parts = [
    pick.model ? modelName(pick.model) : offers.models.length > 0 ? words.agentModel : "",
    pick.effort ? words.effort(pick.effort) : offers.efforts.length > 0 ? words.agentEffort : "",
  ].filter(Boolean);
  const label = parts.join(" | ");
  const item = (
    key: string,
    name: string,
    checked: boolean,
    choose: () => void,
    close: () => void,
  ) => (
    <button
      key={key}
      type="button"
      role="menuitemradio"
      aria-checked={checked}
      onClick={() => {
        close();
        choose();
      }}
      className="flex items-center gap-2 rounded-aui px-2.5 py-1.5 text-left text-aui-sm text-aui-fg hover:bg-aui-hover focus-visible:outline-2 focus-visible:outline-aui-accent"
    >
      <span className="size-3.5 shrink-0">{checked && <CheckIcon size={14} />}</span>
      <span className="truncate">{name}</span>
    </button>
  );
  return (
    <Popover
      narrow
      up
      align="right"
      label={`${words.label}: ${label}`}
      className="inline-flex min-h-8 pointer-coarse:min-h-11 max-w-[12rem] items-center gap-1 rounded-aui px-1.5 text-aui-xs text-aui-muted hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:text-aui-fg"
      button={
        <>
          <span
            className="truncate"
            data-model-pick={pick.model ?? ""}
            data-effort-pick={pick.effort ?? ""}
          >
            {label}
          </span>
          <span aria-hidden>▾</span>
        </>
      }
    >
      {(close) => (
        <div role="menu" aria-label={words.label} className="flex w-64 max-w-[80vw] flex-col gap-1">
          {offers.models.length > 0 && (
            <div role="group" aria-label={words.models} className="flex flex-col">
              <span className="px-2.5 pt-1 text-aui-2xs text-aui-muted">{words.models}</span>
              {item(
                "",
                words.agentModel,
                !pick.model,
                () => onPick({ ...pick, model: undefined }),
                close,
              )}
              {offers.models.map((m) =>
                item(m, m, pick.model === m, () => onPick({ ...pick, model: m }), close),
              )}
            </div>
          )}
          {offers.efforts.length > 0 && (
            <div role="group" aria-label={words.efforts} className="flex flex-col">
              <span className="px-2.5 pt-1 text-aui-2xs text-aui-muted">{words.efforts}</span>
              {item(
                "",
                words.agentEffort,
                !pick.effort,
                () => onPick({ ...pick, effort: undefined }),
                close,
              )}
              {offers.efforts.map((e) =>
                item(
                  e,
                  words.effort(e),
                  pick.effort === e,
                  () => onPick({ ...pick, effort: e }),
                  close,
                ),
              )}
            </div>
          )}
          <p className="px-2.5 pb-1 text-aui-2xs text-aui-muted">{words.hint}</p>
        </div>
      )}
    </Popover>
  );
}
