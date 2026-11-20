Use this skill when a person wants a new agent: an assistant with its own job, instructions and tools that people can tag with `@name`.

Work in this order.

1. Find the job. Ask what the agent is for, if the request or the session so far does not already say. One job per agent: if the person describes two, build the one they need first and name the other as a follow-up.

2. Ask only for what is missing, one short question at a time, and stop once you can write the agent. You need:
   - Who uses it, and what they ask it.
   - What it must produce, and what a good answer looks like.
   - What it must never do, and what it should hand to a person instead.
   - The services or files it needs, if any.

3. Check what exists. If you have list_agents, look for an agent that already does this job, and read_agent before changing one, so you build on it instead of starting a duplicate.

4. Write the agent:
   - A name: lowercase words joined by hyphens, naming the job (`invoice-checker`, `release-notes`). No spaces, no capitals.
   - A one-sentence description of what it does, in the words the people who use it would say.
   - A system prompt: its job, the steps it follows, how it answers, and what it refuses or hands to a person. Use ask_person where it must check with someone before acting.
   - The fewest tools that do the job. Do not grant a tool "just in case".
   - Two or three test cases: a request and what a right answer must contain.

5. Save it the reviewed way. If you have preview_agent_change, prepare the agent with it and show the change card; apply it with apply_agent_change only after the person approves. If you have write_eval_suite and run_eval_suite, turn the test cases into a suite and run it before applying. If the agent needs a service that is not connected, use list_connections and connect_connection, and say that connecting alone grants nothing until the agent's tools are approved.

6. If you cannot save agents, show the result as one fenced block in this form, and nothing after it but one line telling the person to send it to `@humagen`, or to use Agents, then New Agent:

```
---
name: the-name
description: What it does.
tools: [tool_one, tool_two]
---

The system prompt.

Tests:
- "A request" → must contain …
```

Do not invent a tool, a connection or a system the person did not mention. If the job needs something you cannot see, say what is missing instead of guessing.
