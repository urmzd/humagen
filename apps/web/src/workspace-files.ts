import type {
  ConversationSandboxes,
  RunSandbox,
  Scratchpad,
  ScratchpadList,
  WorkspaceFileContent,
  WorkspaceFileList,
  WorkspaceFileWrite,
} from "../../../packages/client/src/generated/types.ts";
import { ApiError } from "./api.ts";
import type { Stored } from "./storage.ts";

export type {
  ConversationSandboxes,
  RunSandbox,
  Scratchpad,
  ScratchpadList,
  WorkspaceFileContent,
  WorkspaceFileList,
  WorkspaceFileWrite,
};
export const MAX_WORKSPACE_FILE_BYTES = 2 * 1024 * 1024;

const at = (thread: string) => `/api/threads/${encodeURIComponent(thread)}/files`;
async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method,
    credentials: "same-origin",
    ...(body === undefined
      ? {}
      : { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) }),
  });
  if (response.status === 204) return undefined as T;
  const value = await response.json().catch(() => ({}));
  if (!response.ok)
    throw new ApiError(response.status, value.error || `Request failed (${response.status})`);
  return value as T;
}

const pads = (thread: string) => `/api/threads/${encodeURIComponent(thread)}/scratchpads`;
export const scratchpadApi = {
  list: (thread: string) => request<ScratchpadList>("GET", pads(thread)),
  read: (thread: string, id: string) =>
    request<Scratchpad>("GET", `${pads(thread)}/${encodeURIComponent(id)}`),
  create: (thread: string, name: string, content = "") =>
    request<Scratchpad>("POST", pads(thread), { name, content }),
  update: (thread: string, id: string, pad: { name: string; content: string; version: number }) =>
    request<Scratchpad>("PUT", `${pads(thread)}/${encodeURIComponent(id)}`, pad),
  remove: (thread: string, id: string, version: number) =>
    request<void>(
      "DELETE",
      `${pads(thread)}/${encodeURIComponent(id)}?${new URLSearchParams({ version: String(version) })}`,
    ),
};

export const workspaceFiles = {
  sandboxes: (thread: string) =>
    request<ConversationSandboxes>("GET", `/api/threads/${encodeURIComponent(thread)}/sandboxes`),
  artifacts: (thread: string) =>
    request<{ artifacts: Stored[]; next?: string | null }>(
      "GET",
      `/api/artifacts?${new URLSearchParams({ thread, limit: "100" })}`,
    ),
  list: (thread: string) => request<WorkspaceFileList>("GET", at(thread)),
  read: (thread: string, path: string) =>
    request<WorkspaceFileContent>("GET", `${at(thread)}/content?${new URLSearchParams({ path })}`),
  write: (thread: string, file: WorkspaceFileWrite) =>
    request<WorkspaceFileContent>("PUT", `${at(thread)}/content`, file),
  remove: (thread: string, path: string, revision: string) =>
    request<void>("DELETE", `${at(thread)}/content?${new URLSearchParams({ path, revision })}`),
  publish: (thread: string, path: string, revision: string, title?: string) =>
    request<Stored>("POST", `${at(thread)}/publish`, { path, revision, title }),
};

export function validWorkspacePath(path: string): boolean {
  return (
    new TextEncoder().encode(path).length <= 240 &&
    path.length > 0 &&
    !path.startsWith("/") &&
    !path.includes("\\") &&
    [...path].every((char) => char.charCodeAt(0) >= 32) &&
    path.split("/").every((part) => part !== "" && part !== "." && part !== "..")
  );
}

/** Static HTML only: no scripts, forms, navigation, embedded documents or network resources. */
export function staticWorkspaceHTML(source: string): string {
  // Template contents stay inert while remote URLs and active elements are removed.
  const template = document.createElement("template");
  template.innerHTML = source;
  for (const node of template.content.querySelectorAll(
    "script,meta,base,iframe,frame,frameset,object,embed,link,form,template,animate,set,animateMotion,animateTransform",
  ))
    node.remove();
  for (const node of template.content.querySelectorAll("*")) {
    for (const attribute of [...node.attributes]) {
      const name = attribute.name.toLowerCase();
      if (
        name.startsWith("on") ||
        /^(href|xlink:href|action|formaction|srcdoc|srcset|ping|target|background|poster|data|codebase|manifest)$/.test(
          name,
        ) ||
        (name === "src" &&
          !(
            node.tagName === "IMG" &&
            /^data:image\/(png|gif|jpeg|webp);base64,/i.test(attribute.value)
          ))
      )
        node.removeAttribute(attribute.name);
    }
  }
  return `<!doctype html><html><head><meta http-equiv="Content-Security-Policy" content="default-src 'none'; script-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src 'none'; connect-src 'none'; base-uri 'none'; form-action 'none'"></head><body>${template.innerHTML}</body></html>`;
}
