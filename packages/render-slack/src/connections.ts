/** Slack's private account controls. Credential values are never placed in a view. */
export const CONNECTIONS_MODAL = "warp.connections";
export interface ConnectionStatus {
  provider: string;
  connected: boolean;
  endpoint?: string;
}
const plain = (text: string) => ({ type: "plain_text", text });
export function connectionsModal(providers?: ConnectionStatus[], channel = "", message?: string) {
  return {
    type: "modal",
    callback_id: CONNECTIONS_MODAL,
    title: plain("Account connections"),
    ...(providers?.length ? { submit: plain("Save") } : {}),
    private_metadata: channel,
    close: plain("Cancel"),
    blocks: [
      {
        type: "section",
        text: {
          type: "plain_text",
          text:
            message ??
            (providers?.length
              ? providers
                  .slice(0, 64)
                  .map(
                    (p) =>
                      `${p.provider}: ${p.connected ? "Connected" : "Disconnected"}${p.endpoint ? ` (${p.endpoint})` : ""}`,
                  )
                  .join("\n")
                  .slice(0, 2900)
              : providers
                ? "No personal tool providers are available."
                : "Loading your connections…"),
        },
      },
      ...(providers?.length
        ? [
            {
              type: "input",
              block_id: "provider",
              label: plain("Provider"),
              element: {
                type: "static_select",
                action_id: "value",
                options: providers
                  .slice(0, 64)
                  .map((p) => ({ text: plain(p.provider.slice(0, 75)), value: p.provider })),
              },
            },
            {
              type: "input",
              block_id: "operation",
              label: plain("Action"),
              element: {
                type: "static_select",
                action_id: "value",
                initial_option: { text: plain("Connect"), value: "connect" },
                options: [
                  { text: plain("Connect"), value: "connect" },
                  { text: plain("Disconnect"), value: "disconnect" },
                ],
              },
            },
            {
              type: "input",
              block_id: "reference",
              optional: true,
              label: plain("Credential reference"),
              element: {
                type: "plain_text_input",
                action_id: "value",
                max_length: 512,
                placeholder: plain("Your allowed secret reference; never paste a token"),
              },
            },
          ]
        : []),
    ],
  };
}
