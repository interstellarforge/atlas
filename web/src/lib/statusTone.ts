import type { StatusInfo } from "./api";

type ToneStatus = Pick<StatusInfo, "id" | "category">;

export type Tone = "backlog" | "todo" | "active" | "review" | "done";

// Statuses have a category (todo, active, review, completed), not a colour. The first todo status reads as
// the backlog and the other ones as todo; the other categories map to their own tone.
export function statusTone(status: ToneStatus, all: ToneStatus[]): Tone {
  if (status.category === "completed") return "done";
  if (status.category === "review") return "review";
  if (status.category === "active") return "active";
  const first = all.find((s) => s.category === "todo");
  return first?.id === status.id ? "backlog" : "todo";
}

export function toneVar(tone: Tone): string {
  return `var(--atlas-st-${tone === "active" ? "active" : tone})`;
}
