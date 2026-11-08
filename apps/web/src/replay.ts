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
  const { coworking, call } = await import("./api.ts");
  if (r.thread.startsWith("s_")) {
    const session = await call<{id:string}>("POST", "/api/sessions", {parent:r.thread,text:r.text,agent:/^@([\w.-]+)/.exec(r.text)?.[1],fresh:true});
    return session.id;
  }
  const fork = await coworking.fork(r.thread, { through: r.point });
  await coworking.post(fork.id, { text: r.text });
  return fork.id;
}
