// Minimal hash router; enough until real routes exist.
export const route = $state<{ path: string }>({ path: current() });

function current(): string {
  return location.hash.replace(/^#/, "") || "/";
}

addEventListener("hashchange", () => {
  route.path = current();
});

export function navigate(path: string): void {
  location.hash = path;
}
