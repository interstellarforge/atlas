import { listUsers, type User } from "./api";

export const people = $state<{ users: User[] }>({ users: [] });

export async function loadPeople(): Promise<void> {
  people.users = await listUsers();
}

export function personName(id: number | null): string {
  if (id === null) return "";
  return people.users.find((u) => u.id === id)?.name ?? `#${String(id)}`;
}

export function initials(name: string): string {
  return name
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map((part) => part.charAt(0).toUpperCase())
    .join("");
}
