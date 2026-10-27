import type { StatusInfo } from "./api";

type ToneStatus = Pick<StatusInfo, "id" | "category">;

export type Tone = "backlog" | "todo" | "active" | "review" | "done";

// Statuses have a category (todo, active, completed), not a colour. The first todo status reads as the
// backlog, the first active one as work in progress and any further active one as review.
export function statusTone(status: ToneStatus, all: ToneStatus[]): Tone {
  if (status.category === "completed") return "done";
  const same = all.filter((s) => s.category === status.category);
  const index = same.findIndex((s) => s.id === status.id);
  if (status.category === "todo") return index === 0 ? "backlog" : "todo";
  return index === 0 ? "active" : "review";
}

export function toneVar(tone: Tone): string {
  return `var(--atlas-st-${tone === "active" ? "active" : tone})`;
}
