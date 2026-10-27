import { FolderIcon, PlusIcon } from "@humagen/render-web";
import { Badge } from "@humagen/render-web/ui/badge";
import { Button } from "@humagen/render-web/ui/button";
import { Input } from "@humagen/render-web/ui/input";
import { Label } from "@humagen/render-web/ui/label";
import { NativeSelect } from "@humagen/render-web/ui/native-select";
import { Textarea } from "@humagen/render-web/ui/textarea";
import { FileBrowser, ResourceBrowser } from "@humagen/web";
import type { Meta, StoryObj } from "@storybook/react-vite";
import { useId, useState } from "react";

const meta = { title: "Foundations/Controls", tags: ["foundations"] } satisfies Meta;
export default meta;
type Story = StoryObj<typeof meta>;

function ControlsExample() {
  const id = useId();
  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <div className="flex flex-wrap items-center gap-3">
        <Button>
          <PlusIcon />
          Import Folder
        </Button>
        <Button variant="outline">New Skill</Button>
        <Button variant="secondary">Selected</Button>
        <Button variant="ghost">Cancel</Button>
        <Button disabled>Save Draft</Button>
        <Badge variant="secondary">Draft</Badge>
      </div>
      <div className="grid gap-5 sm:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor={`${id}-search`}>Find a file</Label>
          <Input id={`${id}-search`} placeholder="Search skills" />
        </div>
        <div className="space-y-2">
          <Label htmlFor={`${id}-scope`}>Scope</Label>
          <NativeSelect id={`${id}-scope`}>
            <option>Personal</option>
            <option>Company</option>
          </NativeSelect>
        </div>
      </div>
      <div className="space-y-2">
        <Label htmlFor={`${id}-source`}>File contents</Label>
        <Textarea
          id={`${id}-source`}
          defaultValue={"# Release brief\n\nName the decision, owner and review date."}
          className="min-h-40 font-mono"
        />
      </div>
    </div>
  );
}
export const Variants: Story = { render: () => <ControlsExample /> };

function BrowserExample() {
  const [scope, setScope] = useState("personal");
  const [file, setFile] = useState("SKILL.md");
  return (
    <div className="mx-auto max-w-6xl">
      <ResourceBrowser
        scopeLabel="Skill Scopes"
        selectedScope={scope}
        onScopeChange={setScope}
        scopes={[
          { id: "personal", label: "Personal", icon: <FolderIcon />, count: 1 },
          { id: "workspace", label: "Company", icon: <FolderIcon />, count: 3 },
          { id: "project:release", label: "Release", group: "Projects", icon: <FolderIcon /> },
        ]}
      >
        <div className="flex flex-col gap-5">
          <header className="flex flex-wrap items-center justify-between gap-3">
            <h2 className="text-xl font-semibold">release-brief</h2>
            <Button>Save Draft</Button>
          </header>
          <FileBrowser
            label="Skill Files"
            paths={["SKILL.md", "references/style.md", "scripts/check.py"]}
            selected={file}
            onSelect={setFile}
          >
            <div className="border-b border-border px-5 py-3 text-sm text-muted-foreground">
              {file}
            </div>
            <div className="min-h-72 p-5">
              <h3 className="text-lg font-semibold">Release brief</h3>
              <p className="mt-3 max-w-[65ch] text-sm leading-6 text-muted-foreground">
                Summarize the release decision, identify its owner and record the review date.
                Supporting files remain in the same skill folder.
              </p>
            </div>
          </FileBrowser>
        </div>
      </ResourceBrowser>
    </div>
  );
}
export const ResourceWorkspace: Story = { render: () => <BrowserExample /> };
