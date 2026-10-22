import type { ReactNode } from "react";
import Reconciler from "react-reconciler";
import { ConcurrentRoot, DefaultEventPriority } from "react-reconciler/constants";

/**
 * A minimal mutation-mode host that builds a plain tree of host nodes.
 * Slack output is static, so we render synchronously once and read the tree.
 */

export interface HostNode {
  type: string;
  props: Record<string, unknown>;
  children: HostChild[];
}

export interface HostText {
  type: "#text";
  text: string;
}

export type HostChild = HostNode | HostText;

interface Container {
  children: HostChild[];
}

function propsWithoutChildren(props: Record<string, unknown>): Record<string, unknown> {
  const { children: _children, ...rest } = props;
  return rest;
}

function remove(list: HostChild[], child: HostChild) {
  const i = list.indexOf(child);
  if (i !== -1) list.splice(i, 1);
}

function insertBefore(list: HostChild[], child: HostChild, before: HostChild) {
  remove(list, child);
  const i = list.indexOf(before);
  list.splice(i === -1 ? list.length : i, 0, child);
}

let updatePriority = DefaultEventPriority;

// The host config is typed loosely on purpose: @types/react-reconciler lags the
// runtime, and this host only implements the mutation subset React calls.
const hostConfig = {
  rendererPackageName: "@humagen/render-slack",
  rendererVersion: "0.0.0",
  supportsMutation: true,
  supportsPersistence: false,
  supportsHydration: false,
  supportsMicrotasks: true,
  isPrimaryRenderer: false,
  warnsIfNotActing: false,
  noTimeout: -1,
  NotPendingTransition: null,
  HostTransitionContext: { $$typeof: Symbol.for("react.context"), _currentValue: null },

  createInstance: (type: string, props: Record<string, unknown>): HostNode => ({
    type,
    props: propsWithoutChildren(props),
    children: [],
  }),
  createTextInstance: (text: string): HostText => ({ type: "#text", text }),
  appendInitialChild: (parent: HostNode, child: HostChild) => parent.children.push(child),
  appendChild: (parent: HostNode, child: HostChild) => parent.children.push(child),
  appendChildToContainer: (c: Container, child: HostChild) => c.children.push(child),
  insertBefore: (parent: HostNode, child: HostChild, before: HostChild) =>
    insertBefore(parent.children, child, before),
  insertInContainerBefore: (c: Container, child: HostChild, before: HostChild) =>
    insertBefore(c.children, child, before),
  removeChild: (parent: HostNode, child: HostChild) => remove(parent.children, child),
  removeChildFromContainer: (c: Container, child: HostChild) => remove(c.children, child),
  clearContainer: (c: Container) => {
    c.children = [];
  },
  commitUpdate: (node: HostNode, _type: string, _old: unknown, next: Record<string, unknown>) => {
    node.props = propsWithoutChildren(next);
  },
  commitTextUpdate: (node: HostText, _old: string, next: string) => {
    node.text = next;
  },
  finalizeInitialChildren: () => false,
  shouldSetTextContent: () => false,
  getRootHostContext: () => ({}),
  getChildHostContext: (ctx: object) => ctx,
  getPublicInstance: (node: HostChild) => node,
  prepareForCommit: () => null,
  resetAfterCommit: () => {},
  preparePortalMount: () => {},
  detachDeletedInstance: () => {},
  hideInstance: () => {},
  unhideInstance: () => {},
  hideTextInstance: () => {},
  unhideTextInstance: () => {},
  resetTextContent: () => {},
  commitMount: () => {},
  getInstanceFromNode: () => null,
  beforeActiveInstanceBlur: () => {},
  afterActiveInstanceBlur: () => {},
  prepareScopeUpdate: () => {},
  getInstanceFromScope: () => null,
  scheduleTimeout: setTimeout,
  cancelTimeout: clearTimeout,
  scheduleMicrotask: queueMicrotask,
  setCurrentUpdatePriority: (p: number) => {
    updatePriority = p;
  },
  getCurrentUpdatePriority: () => updatePriority,
  resolveUpdatePriority: () => updatePriority || DefaultEventPriority,
  resolveEventType: () => null,
  resolveEventTimeStamp: () => -1.1,
  shouldAttemptEagerTransition: () => false,
  trackSchedulerEvent: () => {},
  requestPostPaintCallback: () => {},
  maySuspendCommit: () => false,
  maySuspendCommitOnUpdate: () => false,
  maySuspendCommitInSyncRender: () => false,
  preloadInstance: () => true,
  startSuspendingCommit: () => {},
  suspendInstance: () => {},
  waitForCommitToBeReady: () => null,
  getSuspendedCommitReason: () => null,
  resetFormInstance: () => {},
  bindToConsole: (methodName: keyof Console, args: unknown[]) =>
    // biome-ignore lint/suspicious/noExplicitAny: console method binding mirrors react-dom
    (console[methodName] as any).bind(console, ...args),
};

const reconciler = Reconciler(hostConfig as never);

/** Render synchronously and return the committed host tree. Throws render errors. */
export function renderToHostTree(element: ReactNode): HostChild[] {
  const container: Container = { children: [] };
  let error: unknown = null;
  const capture = (e: unknown) => {
    error ??= e;
  };
  // biome-ignore lint/suspicious/noExplicitAny: runtime 0.34 signature differs from @types 0.33
  const r = reconciler as any;
  const root = r.createContainer(
    container,
    ConcurrentRoot,
    null,
    false,
    null,
    "aui",
    capture,
    capture,
    capture,
    () => {},
  );
  r.updateContainerSync(element, root, null, null);
  r.flushSyncWork();
  if (error) throw error;
  // Unmounting mutates the container, so snapshot the committed tree first.
  const children = structuredClone(container.children);
  r.updateContainerSync(null, root, null, null);
  r.flushSyncWork();
  return children;
}
