Use this skill when a person wants to turn a way of working into a skill: a named set of instructions an assistant follows when asked, written as `/name` in the chat bar.

Work in this order.

1. Find the task. Ask what the person wants done the same way every time. If they already described it, or the session so far shows it, use that and do not ask again.

2. Ask only for what is missing. You need: what starts the task, what it takes in, what it must produce, and what "done right" looks like. Ask one question at a time. Stop asking once you can write the skill.

3. Write the skill. Give it:
   - A name: lowercase words joined by hyphens, a verb first where it fits (`review-contract`, `weekly-report`). No spaces, no capitals.
   - A description: one or two sentences that say what it does and when to use it. Name the situations that should trigger it, in the words the person would use. This is what decides whether the skill is picked, so be specific.
   - Instructions: numbered steps in the order the work is done. One action per step. Say what to check before finishing. Say what to do when something is missing or unclear. Leave out anything a capable assistant already knows.
   - Inputs and outputs, when the task has them: what the person gives, and the exact shape of what comes back.

4. Keep it small. A skill does one task. If the person describes two, write the one they need first and name the other as a follow-up. Aim for instructions under 400 words.

5. Show the result as one fenced block in this form, and nothing after it but one line saying how to save it:

```
---
name: the-name
description: What it does and when to use it.
---

The instructions, as numbered steps.
```

6. Tell the person to save it with Save as Skill under your answer, or in Agents, then Skills, then New Skill. You cannot save a skill yourself. Say that a saved skill is a draft until it is tested and published.

Do not invent a tool, a file or a system the person did not mention. If the task needs something you cannot see, write the step so it asks for it.
