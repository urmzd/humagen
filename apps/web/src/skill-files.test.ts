import assert from "node:assert/strict";
import { test } from "node:test";
import {
  encodeSkillFile,
  importSkillFolder,
  skillFiles,
  skillFileText,
  validSkillPath,
} from "./skill-files.ts";

test("skill folder imports preserve paths, Unicode and binary bytes", async () => {
  const file = (path: string, bytes: string | Uint8Array) => {
    const value = new File(
      [typeof bytes === "string" ? bytes : new Uint8Array(bytes)],
      path.split("/").at(-1)!,
    );
    Object.defineProperty(value, "webkitRelativePath", { value: `brief/${path}` });
    return value;
  };
  const result = await importSkillFolder([
    file("SKILL.md", "---\nname: brief\ndescription: Résumé\n---\nReview résumé"),
    file("assets/icon.bin", new Uint8Array([255, 0])),
  ]);
  assert.equal(result.name, "brief");
  assert.match(skillFileText(result.files[0]!)!, /Résumé/);
  assert.equal(skillFileText(result.files[1]!), null);
  assert.equal(skillFileText(encodeSkillFile("assets/raw.bin", new Uint8Array([0, 1]))), null);
  await assert.rejects(importSkillFolder([file("references/notes.md", "Missing SKILL.md")]));
  await assert.rejects(importSkillFolder([file("SKILL.md", "ok"), file("../escape", "x")]));
});

test("legacy skill content becomes a valid editable file and paths stay relative", () => {
  const files = skillFiles({
    name: "brief",
    description: 'A "brief"',
    instructions: "Write clearly",
    tools: [],
  });
  assert.equal(files.length, 1);
  assert.match(skillFileText(files[0]!)!, /name: brief/);
  for (const path of ["../x", "/x", "refs/../x", "refs//x", "refs\\x", ""])
    assert.equal(validSkillPath(path), false);
  assert.equal(validSkillPath("references/style.md"), true);
});
