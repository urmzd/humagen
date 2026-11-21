/**
 * What each permission lets a person do, in words. Pure: the platform
 * decides, this only phrases it for the Access section.
 */

/** Every permission, in the order the platform lists them. */
export const PERMISSIONS = [
  "agents:read",
  "agents:write",
  "runs:read",
  "runs:write",
  "schedules:read",
  "schedules:write",
  "alerts:read",
  "alerts:write",
  "knowledge:read",
  "knowledge:write",
  "knowledge:import",
  "evals:read",
  "evals:write",
  "billing:read",
  "billing:write",
  "artifacts:read",
  "artifacts:write",
  "clients:read",
  "clients:write",
  "projects:read",
  "projects:write",
  "projects:govern",
  "people:read",
  "speech:write",
] as const;

/** What each permission lets a person do, after "can". */
export const doing: Record<string, string> = {
  "agents:read": "see agents",
  "agents:write": "change an agent",
  "runs:read": "see runs and conversations",
  "runs:write": "run agents",
  "schedules:read": "see schedules",
  "schedules:write": "manage schedules",
  "alerts:read": "see alerts",
  "alerts:write": "acknowledge alerts and set their rules",
  "knowledge:read": "search what agents know",
  "knowledge:write": "change what agents know",
  "knowledge:import": "import conversations from other tools",
  "evals:read": "see evals",
  "evals:write": "run and change evals",
  "billing:read": "see limits and usage",
  "billing:write": "set limits",
  "artifacts:read": "see stored files",
  "artifacts:write": "delete stored files",
  "clients:read": "see connections",
  "clients:write": "manage connections",
  "people:read": "see who is in the workspace",
  "speech:write": "record voice notes",
  "projects:read": "see projects",
  "projects:write": "make and change projects",
  // Break-glass: owner of every project, the workspace and company projects included.
  "projects:govern": "own every project",
};
