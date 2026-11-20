/** A recorded response stream, one event per line, imported as text. */
declare module "*.jsonl?raw" {
  const text: string;
  export default text;
}
