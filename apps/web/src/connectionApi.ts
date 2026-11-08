import type {
  ConnectionAuthorization,
  ConnectionAuthorizationInput,
  ConnectionDetail,
  ConnectionHealth,
  ConnectionPreset,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";
export type { ConnectionPreset, ConnectionHealth, ConnectionDetail };
const path = (provider: string) => `/api/connections/${encodeURIComponent(provider)}`;
export const connectionApi = {
  detail: (provider: string) => call<ConnectionDetail>("GET", path(provider)),
  presets: () => call<ConnectionPreset[]>("GET", "/api/connections/presets"),
  authorize: (provider: string, input?: ConnectionAuthorizationInput) =>
    call<ConnectionAuthorization>("POST", `${path(provider)}/authorize`, input),
  complete: (provider: string, state: string, code: string, installation_id?: string) =>
    call<void>("POST", `${path(provider)}/callback`, {
      state,
      code,
      ...(installation_id ? { installation_id } : {}),
    }),
  disconnect: (provider: string) => call<void>("DELETE", path(provider)),
  check: (provider: string) => call<ConnectionHealth>("POST", `${path(provider)}/check`),
  checkClient: (id: string) =>
    call<ConnectionHealth>("POST", `/api/clients/${encodeURIComponent(id)}/check`),
  useSource: (name: string, oauth: "drive") =>
    call("PUT", `/api/knowledge/${encodeURIComponent(name)}/credential`, { oauth }),
};
