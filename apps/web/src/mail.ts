/**
 * The console's inbox transport. The model, the wire mapping and the routes are
 * shared with every other surface in `@humagen/adapter`. What is the
 * console's own is how it calls: same origin, with the session cookie.
 */
import {
  type Http,
  MailError,
  mailTransport,
  projectTransport,
  type Transport,
} from "@humagen/adapter";
import { track } from "./transition.ts";

export * from "@humagen/adapter/mail";

// Reads are tracked, so a change of screen can wait for the next one's (transition.ts).
const http: Http = <T>(method: string, path: string, json?: unknown): Promise<T> =>
  method === "GET" ? track(send<T>(method, path, json)) : send<T>(method, path, json);

const send = async <T>(method: string, path: string, json?: unknown): Promise<T> => {
  const res = await fetch(path, {
    method,
    credentials: "same-origin",
    headers: json === undefined ? undefined : { "Content-Type": "application/json" },
    body: json === undefined ? undefined : JSON.stringify(json),
  });
  if (res.status === 204) return undefined as T;
  const data = (await res.json().catch(() => ({}))) as { error?: string };
  if (!res.ok) throw new MailError(res.status, data.error ?? `Request failed (${res.status})`);
  return data as T;
};

export const transport: Transport = mailTransport(http);

/** A project's inbox, read through the same session; with `children`, the projects below it too. */
export const projectInbox = (project: string, children = false): Transport =>
  projectTransport(http, project, children);
