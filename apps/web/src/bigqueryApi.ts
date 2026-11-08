import type {
  BigQuerySetupInput,
  BigQuerySetupView,
  CatalogConnector,
} from "../../../packages/client/src/generated/types.ts";
import { call } from "./api.ts";

export const bigqueryApi = {
  setup: () => call<BigQuerySetupView>("GET", "/api/catalog/connectors/bigquery/setup"),
  create: (input: BigQuerySetupInput) =>
    call<CatalogConnector>("POST", "/api/catalog/connectors/bigquery", input),
  reconnect: (name: string) =>
    call<CatalogConnector>("POST", `/api/catalog/connectors/${encodeURIComponent(name)}/reconnect`),
};
