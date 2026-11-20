import { FileIcon } from "@humagen/render-web";
import { useState } from "react";
import { previewText } from "./artifactThumb.ts";
import { contentUrl, kindWord, type Stored, showsInline } from "./storage.ts";

const frame = "relative h-36 overflow-hidden bg-aui-hover";

/**
 * What an artifact looks like before it is opened, at the top of its card:
 * the picture itself, the first lines of a text file on a small page, or
 * else its kind. It repeats what the card says in words, so
 * it is hidden from assistive technology.
 */
export function ArtifactThumb({ file }: { file: Stored }) {
  const [broken, setBroken] = useState(false);
  if (file.kind === "image" && showsInline(file) && !broken)
    return (
      <div className={frame} aria-hidden="true" data-thumb="image">
        {/* biome-ignore lint/a11y/noNoninteractiveElementInteractions: onError is a load failure, not an interaction */}
        <img
          src={contentUrl(file.id, true)}
          alt=""
          loading="lazy"
          decoding="async"
          onError={() => setBroken(true)}
          className="size-full object-cover"
        />
      </div>
    );
  const text = previewText(file);
  if (text)
    return (
      <div className={frame} aria-hidden="true" data-thumb="text">
        <div
          className={`${file.kind === "code" || file.kind === "data" ? "font-aui-mono " : ""}absolute inset-x-5 top-4 bottom-0 overflow-hidden whitespace-pre-wrap break-words rounded-t-aui border border-[var(--humagen-rule)] border-b-0 bg-aui-bg px-3 pt-2.5 text-aui-muted text-aui-xs [mask-image:linear-gradient(to_bottom,black_65%,transparent)]`}
        >
          {text}
        </div>
      </div>
    );
  return (
    <div
      className={`${frame} flex flex-col items-center justify-center gap-2 px-6 text-center text-aui-muted`}
      aria-hidden="true"
      data-thumb="kind"
    >
      <FileIcon size={28} />
      <span className="line-clamp-2 text-aui-xs">{kindWord[file.kind]}</span>
    </div>
  );
}
