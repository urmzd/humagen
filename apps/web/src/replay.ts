/** Keep the prompt's words, replacing its leading agent addresses. */
export function replayText(agent: string, prompt: string): string {
  const words = prompt.replace(/^(?:\s*@[\w.-]+\s*)+/, "").trim();
  return `@${agent} ${words}`.trim();
}

/** Save passive history through the finished turn, then address its prompt anew. */
export async function replayWith(r: {
  thread: string;
  point: string;
  text: string;
}): Promise<string> {
  const { coworking } = await import("./api.ts");
  const fork = await coworking.fork(r.thread, { through: r.point });
  await coworking.post(fork.id, { text: r.text });
  return fork.id;
}
