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

const LAYOUT_KEY = "atlas-sidebar-collapsed";

function loadCollapsed(): boolean {
  try {
    return localStorage.getItem(LAYOUT_KEY) === "1";
  } catch {
    return false;
  }
}

// Sidebar width choice; a per-viewer convenience like the card fields.
export const layout = $state({ collapsed: loadCollapsed() });

export function toggleSidebar(): void {
  layout.collapsed = !layout.collapsed;
  try {
    localStorage.setItem(LAYOUT_KEY, layout.collapsed ? "1" : "0");
  } catch {
    /* storage unavailable: the choice just won't persist */
  }
}

// Server-side preferences of the signed-in user (theme, default view, muted notifications).
export const account = $state<{ defaultView: string }>({
  defaultView: "board",
});
