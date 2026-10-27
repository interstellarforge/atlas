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
