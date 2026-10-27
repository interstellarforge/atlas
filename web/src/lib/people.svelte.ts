import { listUsers, type User } from "./api";
import { t } from "./i18n";

// version changes when someone's picture does, so browsers refetch it.
export const people = $state<{ users: User[]; version: number }>({
  users: [],
  version: 0,
});

export async function loadPeople(): Promise<void> {
  people.users = await listUsers();
  people.version = Date.now();
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

// human:user-4 shows the person's name; a token is an agent.
export function actorName(actor: string): string {
  const [kind, id] = actor.split(/:\w+-/);
  if (kind === "human") return personName(Number(id));
  return t("item.agentToken", { id: id ?? "" });
}
