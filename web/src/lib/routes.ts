export type ProjectViewName = "board" | "list" | "backlog" | "tree";

const PROJECT_ROUTE =
  /^\/p\/([A-Za-z][A-Za-z0-9]{1,9})(?:\/(list|backlog|tree))?$/;

// /p/EXO is the board; /p/EXO/list, /backlog and /tree are the other views of the same data.
export function matchProject(
  path: string,
): { key: string; view: ProjectViewName } | undefined {
  const match = PROJECT_ROUTE.exec(path);
  if (!match) return undefined;
  return {
    key: (match[1] ?? "").toUpperCase(),
    view: (match[2] as ProjectViewName | undefined) ?? "board",
  };
}
