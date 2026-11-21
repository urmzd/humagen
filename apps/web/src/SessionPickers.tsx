import { defaultCopy } from "@humagen/components";
import { CheckIcon, ChevronDownIcon, LayersIcon, SettingsIcon } from "@humagen/render-web";
import type { KeyboardEvent } from "react";
import { href } from "./route.ts";
import type { SessionImage, SessionOptions, SessionSetup } from "./sessionOptions.ts";
import { useSessionOptions } from "./sessionOptions.ts";
import { Popover } from "./ui.tsx";

const words = defaultCopy.sessionSetup;

export type { SessionImage, SessionOptions, SessionSetup };

/** Text controls under the composer, as the approval mode is: no boxes. */
const control =
  "inline-flex h-8 min-w-8 pointer-coarse:h-11 pointer-coarse:min-w-11 shrink-0 items-center justify-center gap-1.5 rounded-aui px-2 text-aui-muted hover:bg-aui-hover hover:text-aui-fg focus-visible:outline-2 focus-visible:outline-aui-accent aria-expanded:bg-aui-hover aria-expanded:text-aui-fg";
const row =
  "flex min-h-11 w-full items-start gap-2.5 rounded-aui px-2.5 py-2 text-left outline-none hover:bg-aui-hover focus-visible:bg-aui-hover aria-disabled:cursor-not-allowed aria-disabled:opacity-60 aria-disabled:hover:bg-transparent";

/** Arrow keys, Home and End move through a menu's rows; Escape or Tab closes it. */
const keys =
  (close: () => void) =>
  (e: KeyboardEvent<HTMLDivElement>): void => {
    if (e.key === "Escape" || e.key === "Tab") {
      e.preventDefault();
      e.stopPropagation();
      close();
      return;
    }
    const rows = [
      ...e.currentTarget.querySelectorAll<HTMLElement>('[role="menuitemradio"],[role="menuitem"]'),
    ];
    const at = rows.indexOf(document.activeElement as HTMLElement);
    const to =
      e.key === "ArrowDown"
        ? (at + 1) % rows.length
        : e.key === "ArrowUp"
          ? (at - 1 + rows.length) % rows.length
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? rows.length - 1
              : -1;
    if (to < 0) return;
    e.preventDefault();
    rows[to]?.focus();
  };

/** An image's name as a person reads it. */
export function imageName(img: SessionImage): string {
  return img.default ? words.defaultImage : img.name;
}

/** What an image offers now, in a few words. */
export function imageHint(img: SessionImage): string {
  if (img.default) return words.defaultHint(img.ref);
  if (img.status === "checking") return words.checking;
  if (img.status === "failed") return img.problem ? `${words.failed}: ${img.problem}` : words.failed;
  return `${img.ref} · ${img.warm ? words.warm : words.cold}`;
}

/**
 * The image a new session's tools run in: the deployment's default, or one
 * of the workspace's own. Only a ready image may be chosen; the others show
 * why not. Chosen before the first message.
 */
export function ImagePicker({
  images,
  value,
  onChange,
  canAdd,
}: {
  images: readonly SessionImage[];
  value: string;
  onChange: (id: string) => void;
  /** The workspace may add images: the menu ends with a way to. */
  canAdd: boolean;
}) {
  const current = images.find((i) => i.id === value) ?? images[0];
  const name = current ? imageName(current) : words.defaultImage;
  return (
    <Popover
      up
      popup="menu"
      label={`${words.image}: ${name}`}
      tooltip={`${words.image}: ${name}`}
      className={control}
      button={
        <span aria-hidden className="inline-flex items-center gap-1 text-aui-xs" data-label>
          <span className="max-w-36 truncate">{name}</span>
          <ChevronDownIcon size={12} />
        </span>
      }
    >
      {(close) => (
        <div
          role="menu"
          aria-label={words.image}
          onKeyDown={keys(close)}
          className="flex w-72 max-w-[calc(100vw-2rem)] flex-col"
          data-testid="image-menu"
        >
          {images.map((img) => {
            const on = img.id === current?.id;
            const ready = img.status === "ready";
            return (
              <button
                key={img.id}
                type="button"
                role="menuitemradio"
                aria-checked={on}
                aria-disabled={!ready || undefined}
                // biome-ignore lint/a11y/noAutofocus: an opened menu takes focus on its current row.
                autoFocus={on}
                data-image={img.id}
                data-status={img.status}
                onClick={() => {
                  if (!ready) return;
                  close();
                  onChange(img.id);
                }}
                className={row}
              >
                <LayersIcon size={16} className="mt-0.5 shrink-0 text-aui-muted" />
                <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                  <span className="truncate text-aui-sm text-aui-fg">{imageName(img)}</span>
                  <span className="line-clamp-2 break-all text-aui-xs text-aui-muted">
                    {imageHint(img)}
                  </span>
                </span>
                <CheckIcon
                  size={16}
                  aria-hidden
                  className={`mt-0.5 shrink-0 text-aui-fg ${on ? "" : "invisible"}`}
                />
              </button>
            );
          })}
          {canAdd && (
            <a
              role="menuitem"
              href={href({ page: "settings", section: "images" })}
              className={`${row} mt-1 items-center border-aui-border border-t text-aui-sm text-aui-muted`}
            >
              <SettingsIcon size={16} className="shrink-0" />
              {words.manage}
            </a>
          )}
        </div>
      )}
    </Popover>
  );
}

/**
 * A new session's setup beside the composer's plus: the image it runs in.
 * Nothing shows until the options are read, when they cannot be, or when the
 * default is the only image and none may be added. Repositories are context
 * from the plus menu, not a control here.
 */
export function SessionSetupPickers({
  value,
  onChange,
  options: given,
}: {
  value: SessionSetup;
  onChange: (next: SessionSetup) => void;
  /** For a test to stand in for the server. */
  options?: SessionOptions | null;
}) {
  const read = useSessionOptions(given === undefined);
  const options = given !== undefined ? given : read;
  if (!options) return null;
  const canAdd = options.imageRegistries.length > 0;
  if (options.images.length < 2 && !canAdd) return null;
  return (
    <span className="inline-flex min-w-0 items-center gap-0.5" data-testid="session-setup">
      <ImagePicker
        images={options.images}
        value={value.image}
        onChange={(image) => onChange({ ...value, image })}
        canAdd={canAdd}
      />
    </span>
  );
}
