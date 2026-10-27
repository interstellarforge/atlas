export interface CardFields {
  priority: boolean;
  assignee: boolean;
  due: boolean;
  labels: boolean;
}

const STORAGE_KEY = "atlas-card-fields";
const defaults: CardFields = {
  priority: true,
  assignee: true,
  due: true,
  labels: true,
};

function load(): CardFields {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw)
      return { ...defaults, ...(JSON.parse(raw) as Partial<CardFields>) };
  } catch {
    /* storage unavailable or corrupt: use the defaults */
  }
  return defaults;
}

// Which fields cards show; a per-viewer convenience, so it lives in the browser only.
export const cardFields = $state<CardFields>(load());

export function saveCardFields(): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(cardFields));
  } catch {
    /* storage unavailable: the choice just won't persist */
  }
}
