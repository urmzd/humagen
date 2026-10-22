import type { PrimitiveImpls, PrimitiveName, PrimitiveProps } from "@humagen/primitives";
import { createElement } from "react";

/** Host element type for a primitive. The serializer switches on these. */
export const hostType = (name: PrimitiveName) => `aui:${name}`;

function host<K extends PrimitiveName>(name: K) {
  return (props: PrimitiveProps[K]) => createElement(hostType(name), props);
}

export const slackImpls: PrimitiveImpls = {
  Header: host("Header"),
  Text: host("Text"),
  Markdown: host("Markdown"),
  Fields: host("Fields"),
  Context: host("Context"),
  Divider: host("Divider"),
  Code: host("Code"),
  Callout: host("Callout"),
  Button: host("Button"),
  Actions: host("Actions"),
  Task: host("Task"),
  Plan: host("Plan"),
  Sources: host("Sources"),
  Feedback: host("Feedback"),
  Table: host("Table"),
  Summary: host("Summary"),
  Terminal: host("Terminal"),
  Checkboxes: host("Checkboxes"),
  File: host("File"),
  Menu: host("Menu"),
  Select: host("Select"),
  Row: host("Row"),
  Prompt: host("Prompt"),
  Tiles: host("Tiles"),
  Amount: host("Amount"),
  Request: host("Request"),
};
