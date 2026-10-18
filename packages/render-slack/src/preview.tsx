import type { Block } from "./blocks";

/**
 * Approximate, dependency-free DOM preview of Block Kit for Storybook and docs.
 * Not pixel-accurate; use blockKitBuilderUrl for the real thing.
 */

// biome-ignore lint/suspicious/noExplicitAny: Block Kit JSON is untyped by design
type Any = any;

function mrkdwnToHtml(text: string): string {
  return text
    .replace(/<(https?:[^|>]+)\|([^>]+)>/g, '<a href="$1">$2</a>')
    .replace(/```([\s\S]+?)```/g, "<pre>$1</pre>")
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/(^|\W)\*([^*\n]+)\*/g, "$1<b>$2</b>")
    .replace(/(^|\W)_([^_\n]+)_/g, "$1<i>$2</i>")
    .replace(/(^|\W)~([^~\n]+)~/g, "$1<s>$2</s>")
    .replace(/:white_check_mark:/g, "✅")
    .replace(/:warning:/g, "⚠️")
    .replace(/:x:/g, "❌")
    .replace(/:information_source:/g, "ℹ️")
    .replace(/\n/g, "<br/>");
}

function markdownToHtml(text: string): string {
  return text
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/\[([^\]]+)\]\(([^)]+)\)/g, '<a href="$2">$1</a>')
    .replace(/`([^`]+)`/g, "<code>$1</code>")
    .replace(/\*\*(.+?)\*\*/g, "<b>$1</b>")
    .replace(/\n/g, "<br/>");
}

function Mrkdwn({ text, className }: { text: string; className?: string }) {
  // Preview only renders our own serializer output, never untrusted input.
  // biome-ignore lint/security/noDangerouslySetInnerHtml: trusted preview content
  return <span className={className} dangerouslySetInnerHTML={{ __html: mrkdwnToHtml(text) }} />;
}

const STATUS: Record<string, string> = {
  pending: "○",
  in_progress: "◐",
  complete: "✅",
  error: "❌",
};

function Task({ t }: { t: Any }) {
  return (
    <div style={{ display: "flex", gap: 8, padding: "2px 0" }}>
      <span>{STATUS[t.status] ?? "•"}</span>
      <span>
        {t.title}
        {t.sources?.length ? (
          <span style={{ color: "#616061", fontSize: 12 }}>
            {" "}
            · {t.sources.length} source{t.sources.length > 1 ? "s" : ""}
          </span>
        ) : null}
      </span>
    </div>
  );
}

const btn = (style?: string): React.CSSProperties => ({
  border: style ? "none" : "1px solid #ddd",
  background: style === "primary" ? "#007a5a" : style === "danger" ? "#e01e5a" : "#fff",
  color: style ? "#fff" : "#1d1c1d",
  borderRadius: 4,
  padding: "4px 12px",
  fontWeight: 600,
  fontSize: 13,
});

function BlockView({ b }: { b: Any }) {
  switch (b.type) {
    case "header":
      return <div style={{ fontWeight: 900, fontSize: 18 }}>{b.text.text}</div>;
    case "section":
      return b.fields ? (
        <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 8 }}>
          {b.fields.map((f: Any) => (
            <Mrkdwn key={f.text} text={f.text} />
          ))}
        </div>
      ) : (
        <Mrkdwn text={b.text.text} />
      );
    case "markdown":
      return (
        // biome-ignore lint/security/noDangerouslySetInnerHtml: escaped above
        <div dangerouslySetInnerHTML={{ __html: markdownToHtml(b.text) }} />
      );
    case "context":
      return (
        <div style={{ color: "#616061", fontSize: 12, display: "flex", gap: 12, flexWrap: "wrap" }}>
          {b.elements.map((e: Any, i: number) => (
            // biome-ignore lint/suspicious/noArrayIndexKey: static preview
            <Mrkdwn key={i} text={e.text ?? ""} />
          ))}
        </div>
      );
    case "divider":
      return <hr style={{ border: 0, borderTop: "1px solid #ddd" }} />;
    case "rich_text":
      return (
        <pre
          style={{
            background: "#f8f8f8",
            border: "1px solid #ddd",
            padding: 8,
            borderRadius: 4,
            fontSize: 12,
            whiteSpace: "pre-wrap",
          }}
        >
          {b.elements.map((e: Any) => e.elements.map((t: Any) => t.text).join("")).join("\n")}
        </pre>
      );
    case "actions":
      return (
        <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
          {b.elements.map((e: Any) => (
            <button
              key={e.action_id}
              type="button"
              style={btn(e.style)}
              title={e.confirm ? "Asks for confirmation" : undefined}
            >
              {e.text.text}
            </button>
          ))}
        </div>
      );
    case "context_actions":
      return (
        <div style={{ display: "flex", gap: 6 }}>
          <button type="button" style={btn()} aria-label={b.elements[0].positive_button.text.text}>
            👍
          </button>
          <button type="button" style={btn()} aria-label={b.elements[0].negative_button.text.text}>
            👎
          </button>
        </div>
      );
    case "task_card":
      return <Task t={b} />;
    case "plan":
      return (
        <div style={{ border: "1px solid #ddd", borderRadius: 8, padding: 8 }}>
          <div style={{ fontWeight: 700, marginBottom: 4 }}>{b.title}</div>
          {b.tasks.map((t: Any) => (
            <Task key={t.task_id} t={t} />
          ))}
        </div>
      );
    case "alert":
      return (
        <div style={{ borderLeft: "4px solid #e8912d", padding: "4px 8px", background: "#fff8ec" }}>
          {b.text}
        </div>
      );
    case "table":
      return (
        <table style={{ borderCollapse: "collapse", fontSize: 13 }}>
          <tbody>
            {b.rows.map((r: Any[], i: number) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: static preview
              <tr key={i} style={{ fontWeight: i === 0 ? 700 : 400 }}>
                {r.map((c: Any, j: number) => (
                  // biome-ignore lint/suspicious/noArrayIndexKey: static preview
                  <td key={j} style={{ border: "1px solid #ddd", padding: "2px 6px" }}>
                    {c.text}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      );
    default:
      return <code style={{ color: "#e01e5a" }}>unsupported block: {b.type}</code>;
  }
}

export function BlockKitPreview({
  blocks,
  botName = "Assistant",
}: {
  blocks: Block[];
  botName?: string;
}) {
  return (
    <div
      style={{
        fontFamily: "Lato, -apple-system, system-ui, sans-serif",
        fontSize: 15,
        lineHeight: 1.45,
        color: "#1d1c1d",
        background: "#fff",
        display: "flex",
        gap: 8,
        padding: 12,
        borderRadius: 8,
        border: "1px solid #e8e8e8",
      }}
    >
      <div
        style={{ width: 36, height: 36, borderRadius: 6, background: "#4a154b", flexShrink: 0 }}
      />
      <div style={{ display: "flex", flexDirection: "column", gap: 8, minWidth: 0, flex: 1 }}>
        <div>
          <b>{botName}</b>{" "}
          <span style={{ fontSize: 10, background: "#ddd", padding: "0 3px", borderRadius: 2 }}>
            APP
          </span>
        </div>
        {blocks.map((b, i) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: blocks have no stable id
          <BlockView key={i} b={b} />
        ))}
      </div>
    </div>
  );
}
