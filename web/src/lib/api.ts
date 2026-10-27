export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    /** On a 409: the current state of the thing that was changed elsewhere. */
    readonly current?: Record<string, unknown>,
  ) {
    super(message);
  }
}

export interface User {
  id: number;
  email: string;
  name: string;
  is_admin: boolean;
  avatar?: boolean;
  totp?: boolean;
  must_change_password?: boolean;
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
    error?: {
      code?: string;
      message?: string;
      current?: Record<string, unknown>;
    };
  };
  if (!res.ok) {
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
      data.error?.current,
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

export async function login(
  email: string,
  password: string,
  code = "",
): Promise<User> {
  const s = await request<Session>("POST", "/auth/login", {
    email,
    password,
    ...(code && { code }),
  });
  csrfToken = s.csrf_token;
  return s.user;
}

export async function logout(): Promise<void> {
  await request<undefined>("POST", "/auth/logout");
  csrfToken = "";
}

// ---- projects and workflow ----

export interface Repository {
  url: string;
  host: string;
  path: string;
  provider: "github" | "gitlab" | "gitea" | "bitbucket" | "other";
  branch: string;
}

export interface Project {
  key: string;
  name: string;
  role?: string;
  repository?: Repository | null;
  archived_at?: number | null;
  created_at?: number;
  time_tracking?: boolean;
}

export interface StatusInfo {
  id: number;
  name: string;
  position: number;
  category: string;
  wip_limit: number | null;
  allow_create: boolean;
  /** Names of statuses an item must have been in before it may enter this one. */
  requires: string[];
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
  item_templates: string[];
  /** Statuses of one category share a board column. */
  merge_columns: boolean;
  /** Members may book time on items. */
  time_tracking: boolean;
}

export async function listProjects(): Promise<Project[]> {
  return (await request<{ items: Project[] }>("GET", "/projects")).items;
}

export function createProject(
  key: string,
  name: string,
  template = 0,
): Promise<Project> {
  return request<Project>("POST", "/projects", {
    key,
    name,
    ...(template > 0 && { template }),
  });
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
  /** Every status of a merged column, in order; `status` is the first. */
  statuses?: StatusInfo[];
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
  status: string;
  priority: string;
  label: string;
  milestone: string;
  assignee: string;
  archived: boolean;
}

export const emptyFilters: Filters = {
  q: "",
  type: "",
  status: "",
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
    status: f.status,
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
  template?: string;
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
    error?: {
      code?: string;
      message?: string;
      current?: Record<string, unknown>;
    };
  };
  if (!res.ok) {
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
      data.error?.current,
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

export interface Token {
  id: number;
  name: string;
  prefix: string;
  scopes: string[];
  projects: string[] | null;
  expires_at: number | null;
  revoked_at: number | null;
  created_at: number;
  last_used_at?: number | null;
}

export async function listTokens(): Promise<Token[]> {
  return (await request<{ items: Token[] }>("GET", "/tokens")).items;
}

// The secret is returned once, here, and never again.
export function createToken(body: {
  name: string;
  scopes: string[];
  projects: string[];
}): Promise<{ token: Token; secret: string }> {
  return request("POST", "/tokens", body);
}

export function revokeToken(id: number): Promise<undefined> {
  return request("DELETE", `/tokens/${String(id)}`);
}

export function addStatus(
  project: string,
  body: { name: string; category: string; wip_limit: number | null },
): Promise<StatusInfo> {
  return request("POST", `/projects/${project}/statuses`, body);
}

export function updateStatus(
  project: string,
  id: number,
  patch: Record<string, unknown>,
): Promise<StatusInfo> {
  return request("PATCH", `/projects/${project}/statuses/${String(id)}`, patch);
}

export function setMergeColumns(
  project: string,
  mergeColumns: boolean,
): Promise<Workflow> {
  return request("PATCH", `/projects/${project}/workflow`, {
    merge_columns: mergeColumns,
  });
}

export function deleteStatus(project: string, id: number): Promise<undefined> {
  return request("DELETE", `/projects/${project}/statuses/${String(id)}`);
}

export interface Preferences {
  theme: "system" | "light" | "dark";
  default_view: "board" | "list" | "backlog" | "tree";
  muted_notifications: string[];
  email_notifications: boolean;
}

export interface SessionInfo {
  id: number;
  created_at: number;
  expires_at: number;
  current: boolean;
}

export async function updateProfile(name: string): Promise<User> {
  return (await request<{ user: User }>("PATCH", "/me", { name })).user;
}

export function changePassword(
  current: string,
  next: string,
): Promise<undefined> {
  return request("PUT", "/me/password", { current, new: next });
}

export async function listSessions(): Promise<SessionInfo[]> {
  return (await request<{ items: SessionInfo[] }>("GET", "/me/sessions")).items;
}

export function revokeSession(id: number): Promise<undefined> {
  return request("DELETE", `/me/sessions/${String(id)}`);
}

export function getPreferences(): Promise<Preferences> {
  return request("GET", "/me/preferences");
}

export function setPreferences(p: Preferences): Promise<Preferences> {
  return request("PUT", "/me/preferences", p);
}

export interface Member {
  user_id: number;
  name: string;
  email: string;
  role: string;
}

export async function listMembers(project: string): Promise<Member[]> {
  return (
    await request<{ items: Member[] }>("GET", `/projects/${project}/members`)
  ).items;
}

export function setMember(
  project: string,
  userId: number,
  role: string,
): Promise<undefined> {
  return request("PUT", `/projects/${project}/members/${String(userId)}`, {
    role,
  });
}

export function removeMember(
  project: string,
  userId: number,
): Promise<undefined> {
  return request("DELETE", `/projects/${project}/members/${String(userId)}`);
}

export function updateProject(
  project: string,
  patch: {
    name?: string;
    archived?: boolean;
    repository?: { url: string; branch: string } | null;
  },
): Promise<Project> {
  return request("PATCH", `/projects/${project}`, patch);
}

export interface Readme {
  state: "unlinked" | "ok" | "empty" | "error";
  markdown?: string;
  source_url?: string;
  fetched_at?: number;
  message?: string;
}

export function getReadme(project: string, refresh = false): Promise<Readme> {
  return request(
    "GET",
    `/projects/${project}/readme${refresh ? "?refresh=1" : ""}`,
  );
}

export interface ProjectToken extends Token {
  owner: string;
}

export async function listProjectTokens(
  project: string,
): Promise<ProjectToken[]> {
  return (
    await request<{ items: ProjectToken[] }>(
      "GET",
      `/projects/${project}/tokens`,
    )
  ).items;
}

export function createProjectToken(
  project: string,
  body: { name: string; write: boolean; expires_at: number | null },
): Promise<{ token: ProjectToken; secret: string }> {
  return request("POST", `/projects/${project}/tokens`, body);
}

export function revokeProjectToken(
  project: string,
  id: number,
): Promise<undefined> {
  return request("DELETE", `/projects/${project}/tokens/${String(id)}`);
}

export interface ImportReport {
  dry_run: boolean;
  applied: boolean;
  created: number;
  duplicates: { row: number; key?: string; message: string }[];
  errors: { row: number; key?: string; message: string }[];
  mapping: Record<string, string>;
}

// Import takes the raw file text (an export JSON, or CSV); dryRun validates without writing.
export async function importItems(
  project: string,
  text: string,
  format: "json" | "csv",
  dryRun: boolean,
): Promise<ImportReport> {
  const res = await fetch(
    `/api/v1/projects/${project}/import${query({ format, dry_run: dryRun ? 1 : undefined })}`,
    { method: "POST", headers: { "X-CSRF-Token": csrfToken }, body: text },
  );
  const data = (await res.json().catch(() => ({}))) as {
    error?: { code?: string; message?: string };
  } & ImportReport;
  if (!res.ok)
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
    );
  return data;
}

export interface AdminUser extends User {
  deactivated_at: number | null;
  created_at: number;
  must_change_password: boolean;
}

export interface AuditEntry {
  id: number;
  actor: string;
  operation: string;
  target: string;
  changes: string;
  batch_id?: string;
  ts: number;
}

export interface Role {
  name: string;
  builtin: boolean;
  actions: string[];
}

export function listRoles(): Promise<{
  items: Role[];
  actions: string[];
  default_role: string;
}> {
  return request("GET", "/roles");
}

export function saveRole(name: string, actions: string[]): Promise<Role> {
  return request("PUT", `/admin/roles/${encodeURIComponent(name)}`, {
    actions,
  });
}

export function deleteRole(name: string): Promise<undefined> {
  return request("DELETE", `/admin/roles/${encodeURIComponent(name)}`);
}

export async function listAdminUsers(): Promise<AdminUser[]> {
  return (await request<{ items: AdminUser[] }>("GET", "/admin/users")).items;
}

export function patchAdminUser(
  id: number,
  patch: {
    is_admin?: boolean;
    active?: boolean;
    reset_totp?: boolean;
    force_password_reset?: boolean;
  },
): Promise<undefined> {
  return request("PATCH", `/admin/users/${String(id)}`, patch);
}

export function createUser(body: {
  email: string;
  name: string;
  password: string;
  is_admin: boolean;
}): Promise<User> {
  return request("POST", "/users", body);
}

export async function getAudit(f: {
  actor: string;
  operation: string;
  target: string;
  before?: number;
}): Promise<AuditEntry[]> {
  return (
    await request<{ items: AuditEntry[] }>(
      "GET",
      `/admin/audit${query({ ...f, limit: 50 })}`,
    )
  ).items;
}

export interface McpClient {
  actor: string;
  label?: string;
  name?: string;
  user_agent?: string;
  first_seen: number;
  last_seen: number;
  requests: number;
  calls: number;
  errors: number;
  last_tool?: string;
  connected: boolean;
}

export interface McpCall {
  ts: number;
  actor: string;
  tool: string;
  ms: number;
  bytes: number;
  error?: string;
}

export interface McpStats {
  status: string;
  started_at: number;
  calls: number;
  connected: number;
  clients: McpClient[];
  recent: McpCall[];
  tools: Record<
    string,
    { calls: number; errors: number; p95_ms: number; mean_bytes: number }
  >;
}

export function getMcpStats(): Promise<McpStats> {
  return request("GET", "/mcp/stats");
}

export interface LogEntry {
  ts: number;
  level: string;
  msg: string;
  attrs?: Record<string, string>;
}

export async function getLogs(f: {
  level: string;
  q: string;
}): Promise<LogEntry[]> {
  return (
    await request<{ items: LogEntry[] }>(
      "GET",
      `/admin/logs${query({ ...f, limit: 200 })}`,
    )
  ).items;
}

export function getInstance(): Promise<{
  settings: Record<string, string | number | boolean>;
}> {
  return request("GET", "/admin/instance");
}

export interface ItemTemplate {
  id: number;
  name: string;
  type: string;
  title_prefix: string;
  description: string;
  priority: string;
  labels: string[];
  status: string;
  checklist: string[];
  subitems: SubItem[];
  scope?: string;
}

export interface SubItem {
  type: string;
  title: string;
  children?: SubItem[];
}

export interface ProjectTemplate {
  id: number;
  name: string;
  description: string;
  data: unknown;
}

export async function listItemTemplates(
  project: string,
): Promise<ItemTemplate[]> {
  return (
    await request<{ items: ItemTemplate[] }>(
      "GET",
      `/projects/${project}/item-templates`,
    )
  ).items;
}

export function saveItemTemplate(
  project: string,
  t: Omit<ItemTemplate, "id">,
): Promise<ItemTemplate> {
  return request("POST", `/projects/${project}/item-templates`, t);
}

export function deleteItemTemplate(
  project: string,
  id: number,
): Promise<undefined> {
  return request("DELETE", `/projects/${project}/item-templates/${String(id)}`);
}

export async function listProjectTemplates(): Promise<ProjectTemplate[]> {
  return (
    await request<{ items: ProjectTemplate[] }>("GET", "/project-templates")
  ).items;
}

export function saveProjectTemplate(body: {
  name: string;
  description?: string;
  from_project?: string;
  data?: unknown;
}): Promise<ProjectTemplate> {
  return request("POST", "/project-templates", body);
}

export function deleteProjectTemplate(id: number): Promise<undefined> {
  return request("DELETE", `/project-templates/${String(id)}`);
}

export interface PublicPolicy {
  registration: "closed" | "invite" | "open";
  min_password_length: number;
  allowed_domain: string;
}

export function getPublicPolicy(): Promise<PublicPolicy> {
  return request("GET", "/auth/policy");
}

export function register(body: {
  email: string;
  name: string;
  password: string;
  invite?: string;
}): Promise<{ user: User }> {
  return request("POST", "/auth/register", body);
}

export async function changeEmail(
  email: string,
  password: string,
): Promise<User> {
  return (
    await request<{ user: User }>("PUT", "/me/email", { email, password })
  ).user;
}

export function deleteAccount(password: string): Promise<undefined> {
  return request("DELETE", "/me", { password });
}

async function sendFile(
  method: string,
  path: string,
  file: File,
): Promise<void> {
  const form = new FormData();
  form.append("file", file);
  const res = await fetch(`/api/v1${path}`, {
    method,
    headers: { "X-CSRF-Token": csrfToken },
    body: form,
  });
  if (!res.ok) {
    const data = (await res.json().catch(() => ({}))) as {
      error?: { code?: string; message?: string };
    };
    throw new ApiError(
      res.status,
      data.error?.code ?? "unknown",
      data.error?.message ?? res.statusText,
    );
  }
}

export function uploadAvatar(file: File): Promise<void> {
  return sendFile("PUT", "/me/avatar", file);
}

export function removeAvatar(): Promise<undefined> {
  return request("DELETE", "/me/avatar");
}

export interface SecurityInfo {
  totp: boolean;
  recovery_remaining: number;
}

export function getSecurity(): Promise<SecurityInfo> {
  return request("GET", "/me/security");
}

export function totpSetup(): Promise<{ secret: string; uri: string }> {
  return request("POST", "/me/totp/setup");
}

export async function totpEnable(code: string): Promise<string[]> {
  return (
    await request<{ recovery_codes: string[] }>("POST", "/me/totp/enable", {
      code,
    })
  ).recovery_codes;
}

export function totpDisable(
  password: string,
  code: string,
): Promise<undefined> {
  return request("POST", "/me/totp/disable", { password, code });
}

export interface Policy {
  min_password_length: number;
  registration: "closed" | "invite" | "open";
  allowed_domain: string;
  session_days: number;
  default_role: string;
}

export function getPolicy(): Promise<Policy> {
  return request("GET", "/admin/policy");
}

export function setPolicy(p: Policy): Promise<Policy> {
  return request("PUT", "/admin/policy", p);
}

export interface MailSettings {
  configured: boolean;
  source: "database" | "environment" | "none";
  host: string;
  port: number;
  security: "tls" | "starttls" | "none";
  username: string;
  password_set: boolean;
  from_addr: string;
  from_name: string;
}

/** `password` omitted keeps the stored one; an empty string removes it. */
export type MailInput = Omit<
  MailSettings,
  "configured" | "source" | "password_set"
> & {
  password?: string;
};

export function getMailSettings(): Promise<MailSettings> {
  return request("GET", "/admin/mail");
}

export function saveMailSettings(input: MailInput): Promise<MailSettings> {
  return request("PUT", "/admin/mail", input);
}

export function clearMailSettings(): Promise<MailSettings> {
  return request("DELETE", "/admin/mail");
}

export function sendTestMail(): Promise<undefined> {
  return request("POST", "/admin/mail/test");
}

export interface Invite {
  id: number;
  email: string;
  is_admin: boolean;
  created_at: number;
  expires_at: number;
  used_at: number | null;
}

export async function listInvites(): Promise<Invite[]> {
  return (await request<{ items: Invite[] }>("GET", "/admin/invites")).items;
}

export function createInvite(body: {
  email: string;
  is_admin: boolean;
  days: number;
}): Promise<{ invite: Invite; code: string }> {
  return request("POST", "/admin/invites", body);
}

export function revokeInvite(id: number): Promise<undefined> {
  return request("DELETE", `/admin/invites/${String(id)}`);
}

export async function listInstanceItemTemplates(): Promise<ItemTemplate[]> {
  return (await request<{ items: ItemTemplate[] }>("GET", "/item-templates"))
    .items;
}

export function saveInstanceItemTemplate(
  t: Omit<ItemTemplate, "id" | "scope">,
): Promise<ItemTemplate> {
  return request("POST", "/item-templates", t);
}

export function deleteInstanceItemTemplate(id: number): Promise<undefined> {
  return request("DELETE", `/item-templates/${String(id)}`);
}

// ---- progress, timeline, time tracking (ADR-0014) ----

export interface StatusCount {
  id: number;
  name: string;
  category: string;
  count: number;
}

export interface Progress {
  total: number;
  categories: Record<"todo" | "active" | "review" | "completed", number>;
  statuses: StatusCount[];
  priorities: Record<string, number>;
  types: Record<string, number>;
}

export function getProgress(project: string): Promise<Progress> {
  return request("GET", `/projects/${project}/progress`);
}

export interface TimelineItem {
  key: string;
  type: string;
  title: string;
  status: string;
  category: string;
  start_at: number;
  due_at: number;
}

export interface Timeline {
  milestones: Milestone[];
  items: TimelineItem[];
  truncated: boolean;
}

export function getTimeline(
  project: string,
  from: number,
  to: number,
): Promise<Timeline> {
  return request(
    "GET",
    `/projects/${project}/timeline?from=${String(from)}&to=${String(to)}`,
  );
}

export interface ActivityEntry {
  id: number;
  actor: string;
  operation: string;
  target: string;
  changes: string;
  at: number;
}

export async function getActivity(
  project: string,
  before = 0,
): Promise<ActivityEntry[]> {
  return (
    await request<{ items: ActivityEntry[] }>(
      "GET",
      `/projects/${project}/activity${before ? `?before=${String(before)}` : ""}`,
    )
  ).items;
}

export function setTimeTracking(
  project: string,
  enabled: boolean,
): Promise<undefined> {
  return request("PUT", `/projects/${project}/time-tracking`, { enabled });
}

export interface TimeEntry {
  id: number;
  item: string;
  title?: string;
  user_id: number;
  user: string;
  status: string;
  minutes: number;
  spent_on: string;
  note: string;
}

export async function getItemTime(key: string): Promise<TimeEntry[]> {
  return (await request<{ items: TimeEntry[] }>("GET", `/items/${key}/time`))
    .items;
}

export function logTime(
  key: string,
  duration: string,
  date: string,
  note: string,
): Promise<TimeEntry> {
  return request("POST", `/items/${key}/time`, { duration, date, note });
}

export function updateTime(
  project: string,
  id: number,
  patch: { duration?: string; date?: string; note?: string },
): Promise<TimeEntry> {
  return request("PATCH", `/projects/${project}/time/${String(id)}`, patch);
}

export function deleteTime(project: string, id: number): Promise<undefined> {
  return request("DELETE", `/projects/${project}/time/${String(id)}`);
}

export interface TimeBucket {
  key: string;
  label?: string;
  minutes: number;
}

export interface TimeSummary {
  total_minutes: number;
  entries: number;
  items: TimeBucket[];
  people: TimeBucket[];
  statuses: TimeBucket[];
  periods: TimeBucket[];
}

export interface TimeQuery {
  from: string;
  to: string;
  user: string;
  period: string;
}

function timeParams(q: TimeQuery): string {
  const params = new URLSearchParams();
  for (const k of ["from", "to", "user", "period"] as const)
    if (q[k]) params.set(k, q[k]);
  const text = params.toString();
  return text ? `?${text}` : "";
}

export function getTimeSummary(
  project: string,
  q: TimeQuery,
): Promise<TimeSummary> {
  return request("GET", `/projects/${project}/time${timeParams(q)}`);
}

export function timeExportUrl(project: string, q: TimeQuery): string {
  return `/api/v1/projects/${project}/time/export${timeParams(q)}`;
}

// ---- project context (ADR-0015) ----

export const CONTEXT_CATEGORIES = [
  "architecture",
  "decision",
  "convention",
  "pitfall",
  "dependency",
  "reference",
] as const;
export type ContextCategory = (typeof CONTEXT_CATEGORIES)[number];
export const CONTEXT_SOURCE_TYPES = ["file", "adr", "ticket", "url"] as const;
export type ContextSourceType = (typeof CONTEXT_SOURCE_TYPES)[number];

export interface ContextSource {
  type: ContextSourceType;
  path?: string;
  ticket?: string;
  url?: string;
  repository?: string;
  commit_sha?: string;
  content_hash?: string;
  note?: string;
  verified_at?: number | null;
}

export interface ContextEntry {
  key: string;
  category: ContextCategory;
  title: string;
  summary: string;
  /** Only present when one entry is read. */
  body?: string;
  status: "active" | "superseded" | "archived";
  superseded_by?: string;
  verification_state: "verified" | "unverified" | "stale";
  source_kind: string;
  pinned: boolean;
  version: number;
  updated_at: number;
  verified_at: number | null;
  verified_by?: string;
  sources: ContextSource[];
}

export interface ContextList {
  context_version: number;
  items: ContextEntry[];
  warnings?: string[];
  next_cursor?: string;
}

export interface ContextQuery {
  q: string;
  category: string;
  status: string;
}

export function getContextList(
  project: string,
  q: ContextQuery,
): Promise<ContextList> {
  const params = new URLSearchParams({ limit: "200" });
  if (q.q) params.set("q", q.q);
  if (q.category) params.set("category", q.category);
  if (q.status) params.set("status", q.status);
  return request("GET", `/projects/${project}/context?${params.toString()}`);
}

export function getContextEntry(
  project: string,
  key: string,
): Promise<ContextEntry> {
  return request("GET", `/projects/${project}/context/${key}`);
}

export interface ContextDraft {
  category: ContextCategory;
  title: string;
  summary: string;
  body: string;
  pinned: boolean;
  sources: ContextSource[];
}

export async function createContext(
  project: string,
  draft: ContextDraft,
): Promise<ContextEntry> {
  return (
    await request<{ entry: ContextEntry }>(
      "POST",
      `/projects/${project}/context`,
      draft,
    )
  ).entry;
}

export async function updateContext(
  project: string,
  key: string,
  version: number,
  draft: ContextDraft,
): Promise<ContextEntry> {
  return (
    await request<{ entry: ContextEntry }>(
      "PATCH",
      `/projects/${project}/context/${key}`,
      { ...draft, version },
    )
  ).entry;
}

export async function archiveContext(
  project: string,
  key: string,
): Promise<ContextEntry> {
  return (
    await request<{ entry: ContextEntry }>(
      "DELETE",
      `/projects/${project}/context/${key}`,
    )
  ).entry;
}

export async function verifyContext(
  project: string,
  key: string,
  version: number,
): Promise<ContextEntry> {
  return (
    await request<{ entry: ContextEntry }>(
      "POST",
      `/projects/${project}/context/${key}/verify`,
      { version },
    )
  ).entry;
}

export async function supersedeContext(
  project: string,
  key: string,
  by: string,
  version: number,
): Promise<ContextEntry> {
  return (
    await request<{ entry: ContextEntry }>(
      "POST",
      `/projects/${project}/context/${key}/supersede`,
      { by, version },
    )
  ).entry;
}

/** The answer of GET .../context/bootstrap exactly as an agent receives it, with its size. */
export async function getContextPreview(
  project: string,
): Promise<{ json: string; bytes: number }> {
  const data = await request<unknown>(
    "GET",
    `/projects/${project}/context/bootstrap`,
  );
  const json = JSON.stringify(data, null, 2);
  return { json, bytes: new TextEncoder().encode(JSON.stringify(data)).length };
}
