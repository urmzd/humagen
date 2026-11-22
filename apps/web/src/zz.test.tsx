import { test } from "node:test";
import { renderToStaticMarkup } from "react-dom/server";
import { ProjectsPage } from "./Projects.tsx";
import { tree, workspace } from "./projects.fixtures.ts";
test("x", () => {
  console.log(renderToStaticMarkup(<ProjectsPage projects={tree.slice(0,2)} root={workspace} canCreate={false} canGovern={false} me="ada" onFilter={()=>{}} onOpen={()=>{}} onCreated={()=>{}} pinned={["Globex"]} onPin={async()=>{}} />));
});
