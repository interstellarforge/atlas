export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
  }
}

export interface User {
  id: number;
  email: string;
  name: string;
  is_admin: boolean;
}

interface Session {
  user: User;
  csrf_token: string;
}

let csrfToken = "";

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {};
  if (body !== undefined) headers["Content-Type"] = "application/json";
  if (method !== "GET") headers["X-CSRF-Token"] = csrfToken;

  const res = await fetch(`/api/v1${path}`, {
    method,
    headers,
    ...(body !== undefined && { body: JSON.stringify(body) }),
  });
  if (res.status === 204) return undefined as T;
  const data = (await res.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string };
  };
  if (!res.ok) {
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
    );
  }
  return data as T;
}

export async function me(): Promise<User | undefined> {
  try {
    const s = await request<Session>("GET", "/auth/me");
    csrfToken = s.csrf_token;
    return s.user;
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return undefined;
    throw e;
  }
}

export async function login(email: string, password: string): Promise<User> {
  const s = await request<Session>("POST", "/auth/login", { email, password });
  csrfToken = s.csrf_token;
  return s.user;
}

export async function logout(): Promise<void> {
  await request<undefined>("POST", "/auth/logout");
  csrfToken = "";
}

// ---- projects and workflow ----

export interface Project {
  key: string;
  name: string;
  role?: string;
}

export interface StatusInfo {
  id: number;
  name: string;
  position: number;
  category: string;
  wip_limit: number | null;
}

export interface CustomField {
  name: string;
  type: "text" | "number" | "bool" | "date" | "select";
  required: boolean;
  options?: string[];
}

export interface Milestone {
  id: number;
  name: string;
  due_at: number | null;
  released_at: number | null;
  total: number;
  done: number;
}

export interface Workflow {
  types: string[];
  milestones: Milestone[];
  custom_fields: CustomField[];
  hierarchy: Record<string, string[]>;
  statuses: StatusInfo[];
  labels: string[];
}

export async function listProjects(): Promise<Project[]> {
  return (await request<{ items: Project[] }>("GET", "/projects")).items;
}

export function createProject(key: string, name: string): Promise<Project> {
  return request<Project>("POST", "/projects", { key, name });
}

export function getWorkflow(project: string): Promise<Workflow> {
  return request<Workflow>("GET", `/projects/${project}/workflow`);
}

export async function listUsers(): Promise<User[]> {
  return (await request<{ items: User[] }>("GET", "/users")).items;
}

// ---- items, board ----

export interface Card {
  key: string;
  type: string;
  title: string;
  priority: string;
  assignee: number | null;
  due_at: number | null;
  parent: string | null;
  root: string;
  blocked: boolean;
  version: number;
  labels: string[];
  project?: string;
  status?: string;
  archived_at?: number | null;
  updated_at?: number;
}

export interface Column {
  status: StatusInfo;
  count: number;
  over_wip: boolean;
  items: Card[];
  next_cursor: string | null;
}

export interface Board {
  seq: number;
  columns: Column[];
}

export interface Filters {
  q: string;
  type: string;
  priority: string;
  label: string;
  milestone: string;
  assignee: string;
  archived: boolean;
}

export const emptyFilters: Filters = {
  q: "",
  type: "",
  priority: "",
  label: "",
  milestone: "",
  assignee: "",
  archived: false,
};

function query(
  params: Record<string, string | number | boolean | undefined>,
): string {
  const q = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== "" && v !== false) q.set(k, String(v));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

function filterParams(f: Filters): Record<string, string | boolean> {
  return {
    q: f.q,
    type: f.type,
    priority: f.priority,
    label: f.label,
    milestone: f.milestone,
    assignee: f.assignee,
    archived: f.archived,
  };
}

export function getBoard(
  project: string,
  f: Filters,
  limit: number,
): Promise<Board> {
  return request<Board>(
    "GET",
    `/projects/${project}/board${query({ ...filterParams(f), limit })}`,
  );
}

export function getBoardColumn(
  project: string,
  statusId: number,
  f: Filters,
  cursor: string,
): Promise<Column> {
  return request<Column>(
    "GET",
    `/projects/${project}/board/columns/${String(statusId)}${query({ ...filterParams(f), cursor })}`,
  );
}

export function getCrossBoard(projects: string[], f: Filters): Promise<Board> {
  return request<Board>(
    "GET",
    `/board${query({ ...filterParams(f), projects: projects.join(",") })}`,
  );
}

export interface Page {
  items: Card[];
  next_cursor: string | null;
}

export function getItems(
  params: Record<string, string | number | boolean | undefined>,
): Promise<Page> {
  return request<Page>("GET", `/items${query(params)}`);
}

export function getCard(key: string, fields: string): Promise<Card> {
  return request<Card>("GET", `/items/${key}${query({ fields })}`);
}

export function filterQuery(f: Filters): Record<string, string | boolean> {
  return filterParams(f);
}

export function createItem(body: {
  project: string;
  type: string;
  title: string;
  parent?: string;
  status?: string;
}): Promise<Card> {
  return request<Card>("POST", "/items", body);
}

export function moveItem(
  key: string,
  version: number,
  to: { status?: string; after?: string; before?: string },
): Promise<Card> {
  return request<Card>("POST", `/items/${key}/move`, { version, ...to });
}

export function archiveItem(
  key: string,
  version: number,
  archived: boolean,
): Promise<Card> {
  return request<Card>("PATCH", `/items/${key}`, { version, archived });
}

export interface ItemDetail {
  key: string;
  type: string;
  title: string;
  description: string;
  parent: string | null;
  status: string;
  priority: string;
  assignee: number | null;
  due_at: number | null;
  labels: string[];
  milestone: string | null;
  custom_data: Record<string, unknown>;
  blocked: boolean;
  version: number;
  archived_at: number | null;
  updated_at: number;
}

export interface Link {
  type: string;
  key: string;
  title: string;
  status: string;
}

export interface Links {
  outgoing: Link[];
  incoming: Link[];
}

export function getItem(key: string): Promise<ItemDetail> {
  return request<ItemDetail>("GET", `/items/${key}`);
}

export function updateItem(
  key: string,
  version: number,
  patch: Record<string, unknown>,
): Promise<ItemDetail> {
  return request<ItemDetail>("PATCH", `/items/${key}`, { ...patch, version });
}

export function getLinks(key: string): Promise<Links> {
  return request<Links>("GET", `/items/${key}/links`);
}

export function addLink(
  key: string,
  type: string,
  target: string,
): Promise<undefined> {
  return request<undefined>("POST", `/items/${key}/links`, { type, target });
}

export function removeLink(
  key: string,
  type: string,
  target: string,
): Promise<undefined> {
  return request<undefined>("DELETE", `/items/${key}/links/${type}/${target}`);
}

export interface Comment {
  id: number;
  actor: string;
  body: string;
  created_at: number;
}

export interface ChecklistItem {
  id: number;
  text: string;
  done: boolean;
}

export async function getComments(key: string): Promise<Comment[]> {
  return (await request<{ items: Comment[] }>("GET", `/items/${key}/comments`))
    .items;
}

export function addComment(key: string, body: string): Promise<Comment> {
  return request<Comment>("POST", `/items/${key}/comments`, { body });
}

export async function getChecklist(key: string): Promise<ChecklistItem[]> {
  return (
    await request<{ items: ChecklistItem[] }>("GET", `/items/${key}/checklist`)
  ).items;
}

export function addChecklistItem(
  key: string,
  text: string,
): Promise<ChecklistItem> {
  return request<ChecklistItem>("POST", `/items/${key}/checklist`, { text });
}

export function updateChecklistItem(
  key: string,
  id: number,
  patch: { text?: string; done?: boolean },
): Promise<ChecklistItem> {
  return request<ChecklistItem>(
    "PATCH",
    `/items/${key}/checklist/${String(id)}`,
    patch,
  );
}

export function removeChecklistItem(
  key: string,
  id: number,
): Promise<undefined> {
  return request<undefined>("DELETE", `/items/${key}/checklist/${String(id)}`);
}

export interface Attachment {
  id: number;
  filename: string;
  mime: string;
  size: number;
  width: number;
  height: number;
  url: string;
  thumb_url: string;
}

export async function getAttachments(key: string): Promise<Attachment[]> {
  return (
    await request<{ items: Attachment[] }>("GET", `/items/${key}/attachments`)
  ).items;
}

// Multipart upload; the browser sets the content type and boundary itself.
export async function uploadAttachment(
  key: string,
  file: File,
): Promise<Attachment> {
  const body = new FormData();
  body.append("file", file, file.name);
  const res = await fetch(`/api/v1/items/${key}/attachments`, {
    method: "POST",
    headers: { "X-CSRF-Token": csrfToken },
    body,
  });
  const data = (await res.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string };
  };
  if (!res.ok) {
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
    );
  }
  return data as unknown as Attachment;
}

export function createMilestone(
  project: string,
  name: string,
  dueAt: number | null,
): Promise<Milestone> {
  return request<Milestone>("POST", `/projects/${project}/milestones`, {
    name,
    due_at: dueAt,
  });
}

export function updateMilestone(
  project: string,
  id: number,
  patch: { name?: string; due_at?: number | null; released?: boolean },
): Promise<Milestone> {
  return request<Milestone>(
    "PATCH",
    `/projects/${project}/milestones/${String(id)}`,
    patch,
  );
}

export function deleteMilestone(
  project: string,
  id: number,
): Promise<undefined> {
  return request<undefined>(
    "DELETE",
    `/projects/${project}/milestones/${String(id)}`,
  );
}

export async function listMilestones(project: string): Promise<Milestone[]> {
  return (
    await request<{ items: Milestone[] }>(
      "GET",
      `/projects/${project}/milestones`,
    )
  ).items;
}

export interface Notification {
  id: number;
  type: "assigned" | "mention" | "blocker_done" | "followed";
  project: string;
  item: string;
  actor: string;
  detail: string;
  created_at: number;
  read: boolean;
}

export interface Inbox {
  items: Notification[];
  unread: number;
}

export function getInbox(unreadOnly = false): Promise<Inbox> {
  return request<Inbox>(
    "GET",
    `/notifications${query({ unread: unreadOnly ? 1 : undefined })}`,
  );
}

export function markNotificationRead(id: number): Promise<undefined> {
  return request<undefined>("POST", `/notifications/${String(id)}/read`);
}

export function markAllNotificationsRead(): Promise<undefined> {
  return request<undefined>("POST", "/notifications/read");
}

export function deleteNotification(id: number): Promise<undefined> {
  return request<undefined>("DELETE", `/notifications/${String(id)}`);
}

export async function getFollowing(key: string): Promise<boolean> {
  return (await request<{ following: boolean }>("GET", `/items/${key}/follow`))
    .following;
}

export function setFollowing(key: string, on: boolean): Promise<undefined> {
  return request<undefined>(on ? "PUT" : "DELETE", `/items/${key}/follow`);
}
