import { route } from "./router.svelte";

export type ProjectViewName =
  | "board"
  | "list"
  | "backlog"
  | "tree"
  | "milestones"
  | "issues"
  | "readme"
  | "settings";

const PROJECT_ROUTE =
  /^\/p\/([A-Za-z][A-Za-z0-9]{1,9})(?:\/(list|backlog|tree|milestones|issues|readme|settings))?(?:\/([A-Za-z][A-Za-z0-9]{1,9}-[0-9]+))?$/;

// /p/EXO is the board; /p/EXO/list, /backlog and /tree are the other views of the same data.
// A trailing item key (/p/EXO/list/EXO-12) opens that item's panel over the view.
const SETTINGS_ROUTE =
  /^\/p\/([A-Za-z][A-Za-z0-9]{1,9})\/settings(?:\/([a-z]+))?$/;

export function matchProject(path: string):
  | {
      key: string;
      view: ProjectViewName;
      item: string | undefined;
      section?: string;
    }
  | undefined {
  const settings = SETTINGS_ROUTE.exec(path);
  if (settings)
    return {
      key: (settings[1] ?? "").toUpperCase(),
      view: "settings",
      item: undefined,
      ...(settings[2] && { section: settings[2] }),
    };
  const match = PROJECT_ROUTE.exec(path);
  if (!match) return undefined;
  return {
    key: (match[1] ?? "").toUpperCase(),
    view: (match[2] as ProjectViewName | undefined) ?? "board",
    item: match[3]?.toUpperCase(),
  };
}

export function viewPath(project: string, view: ProjectViewName): string {
  return view === "board" ? `/p/${project}` : `/p/${project}/${view}`;
}

// Link target that opens an item over the view the user is on (or the board of the item's project).
export function itemHref(key: string): string {
  const project = key.slice(0, key.lastIndexOf("-"));
  const current = matchProject(route.path);
  const view = current?.key === project ? current.view : "board";
  return `#${viewPath(project, view)}/${key}`;
}
