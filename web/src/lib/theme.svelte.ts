type Theme = "light" | "dark";

function initial(): Theme {
  const set = document.documentElement.dataset["theme"];
  if (set === "light" || set === "dark") return set;
  return matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

export const theme = $state<{ current: Theme }>({ current: initial() });

export function toggleTheme(): void {
  theme.current = theme.current === "dark" ? "light" : "dark";
  document.documentElement.dataset["theme"] = theme.current;
  try {
    localStorage.setItem("atlas-theme", theme.current);
  } catch {
    /* storage unavailable, theme just won't persist */
  }
}
