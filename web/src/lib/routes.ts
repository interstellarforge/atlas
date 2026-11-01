import { route } from "./router.svelte";

export type ProjectViewName =
  | "overview"
  | "timeline"
  | "time"
  | "board"
  | "list"
  | "backlog"
  | "tree"
  | "context"
  | "milestones"
  | "issues"
  | "readme"
  | "settings";

const PROJECT_ROUTE =
  /^\/p\/([A-Za-z][A-Za-z0-9]{1,9})(?:\/(overview|timeline|time|list|backlog|tree|context|milestones|issues|readme|settings))?(?:\/([0-9]+))?(?:\/([A-Za-z][A-Za-z0-9]{1,9}-[0-9]+))?$/;

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
      milestone?: number;
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
  // A number after the view is a milestone id, which only the milestones view knows.
  if (match[3] !== undefined && match[2] !== "milestones") return undefined;
  return {
    key: (match[1] ?? "").toUpperCase(),
    view: (match[2] as ProjectViewName | undefined) ?? "board",
    item: match[4]?.toUpperCase(),
    ...(match[3] !== undefined && { milestone: Number(match[3]) }),
  };
}

export function viewPath(
  project: string,
  view: ProjectViewName,
  milestone?: number,
): string {
  if (view === "board") return `/p/${project}`;
  const base = `/p/${project}/${view}`;
  return milestone === undefined ? base : `${base}/${String(milestone)}`;
}

// Link target that opens an item over the view the user is on (or the board of the item's project).
export function itemHref(key: string): string {
  const project = key.slice(0, key.lastIndexOf("-"));
  const current = matchProject(route.path);
  const view = current?.key === project ? current.view : "board";
  const milestone = current?.key === project ? current.milestone : undefined;
  return `#${viewPath(project, view, milestone)}/${key}`;
}
