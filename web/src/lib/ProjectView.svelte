<script lang="ts">
  import {
    getProgress,
    getWorkflow,
    type Progress,
    type Repository,
    type User,
    type Workflow,
  } from "./api";
  import BacklogView from "./BacklogView.svelte";
  import Board from "./Board.svelte";
  import ContextView from "./ContextView.svelte";
  import { onChange } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import IssuesView from "./IssuesView.svelte";
  import ItemPanel from "./ItemPanel.svelte";
  import ListView from "./ListView.svelte";
  import MilestonesView from "./MilestonesView.svelte";
  import OverviewView from "./OverviewView.svelte";
  import PageHero from "./PageHero.svelte";
  import ProgressBar from "./ProgressBar.svelte";
  import { projectColor } from "./projectColor";
  import ProjectSettings from "./ProjectSettings.svelte";
  import ReadmeView from "./ReadmeView.svelte";
  import { navigate } from "./router.svelte";
  import { type ProjectViewName, viewPath } from "./routes";
  import TimelineView from "./TimelineView.svelte";
  import TimeView from "./TimeView.svelte";
  import TreeView from "./TreeView.svelte";

  let {
    project,
    name = "",
    canAdmin = false,
    role = "",
    repository = null,
    user,
    section = "",
    onchange = () => undefined,
    view,
    item,
  }: {
    project: string;
    name?: string;
    canAdmin?: boolean;
    /** The signed-in user's role in this project. */
    role?: string;
    repository?: Repository | null;
    user: User;
    section?: string;
    onchange?: () => void;
    view: ProjectViewName;
    item: string | undefined;
  } = $props();

  let workflow = $state<Workflow | undefined>();
  let error = $state("");
  let progress = $state<Progress | undefined>();

  const tabs: {
    view: ProjectViewName;
    suffix: string;
    label:
      | "project.work"
      | "project.timeline"
      | "project.time"
      | "project.context"
      | "project.milestones"
      | "project.issues"
      | "project.readme"
      | "project.settings";
  }[] = [
    { view: "board", suffix: "", label: "project.work" },
    { view: "context", suffix: "/context", label: "project.context" },
    { view: "milestones", suffix: "/milestones", label: "project.milestones" },
    { view: "timeline", suffix: "/timeline", label: "project.timeline" },
    { view: "time", suffix: "/time", label: "project.time" },
    { view: "issues", suffix: "/issues", label: "project.issues" },
    { view: "readme", suffix: "/readme", label: "project.readme" },
    { view: "settings", suffix: "/settings", label: "project.settings" },
  ];
  // Board, list, backlog and hierarchy are views of the same items: one tab with sub-tabs. The old
  // /overview address still works and opens the board with the overview unfolded.
  const workViews: ProjectViewName[] = [
    "overview",
    "board",
    "list",
    "backlog",
    "tree",
  ];
  const subtabs = [
    { view: "board", suffix: "", label: "project.board", icon: "board" },
    { view: "list", suffix: "/list", label: "project.list", icon: "list" },
    {
      view: "backlog",
      suffix: "/backlog",
      label: "project.backlog",
      icon: "backlog",
    },
    { view: "tree", suffix: "/tree", label: "project.tree", icon: "tree" },
  ] as const;
  const inWork = $derived(workViews.includes(view));
  function tabActive(tabView: ProjectViewName): boolean {
    return tabView === "board" ? inWork : view === tabView;
  }

  const OVERVIEW_KEY = "atlas-overview-open";
  let overviewOpen = $state(readOverviewOpen());
  function readOverviewOpen(): boolean {
    try {
      return localStorage.getItem(OVERVIEW_KEY) === "1";
    } catch {
      return false;
    }
  }
  function toggleOverview(): void {
    overviewOpen = !overviewOpen;
    try {
      localStorage.setItem(OVERVIEW_KEY, overviewOpen ? "1" : "0");
    } catch {
      /* storage unavailable: the choice just won't persist */
    }
  }

  const icons = {
    overview: "board",
    timeline: "calendar",
    time: "clock",
    board: "board",
    list: "list",
    backlog: "backlog",
    tree: "tree",
    context: "context",
    milestones: "milestone",
    issues: "bug",
    readme: "story",
    settings: "settings",
  } as const;
  const visibleTabs = $derived(
    tabs.filter(
      (tab) =>
        (tab.view !== "settings" || canAdmin) &&
        (tab.view !== "time" || workflow?.time_tracking === true),
    ),
  );

  async function loadWorkflow(key: string): Promise<void> {
    try {
      const loaded = await getWorkflow(key);
      if (key === project) workflow = loaded;
    } catch {
      error = t("project.loadFailed");
    }
  }

  $effect(() => {
    workflow = undefined;
    error = "";
    void loadWorkflow(project);
  });

  let progressTimer: ReturnType<typeof setTimeout> | undefined;
  async function loadProgress(key: string): Promise<void> {
    try {
      const loaded = await getProgress(key);
      if (key === project) progress = loaded;
    } catch {
      /* the overview tab reports a failed load */
    }
  }
  $effect(() => {
    progress = undefined;
    void loadProgress(project);
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(progressTimer);
      progressTimer = setTimeout(() => void loadProgress(project), 500);
    }),
  );

  // Statuses and labels change rarely; reload them when the project's settings were touched.
  $effect(() =>
    onChange((event) => {
      if (event.project === project && event.type === "project.updated")
        void loadWorkflow(project);
    }),
  );
</script>

<section class="project">
  <PageHero
    title={name || project}
    description={name ? "" : t("project.heroText")}
  >
    {#snippet icon()}
      <span class="tile" style:--accent={projectColor(project)}
        >{project.slice(0, 1)}</span
      >
    {/snippet}
    {#snippet actions()}
      {#if progress && progress.total > 0}
        <div class="heroprogress"><ProgressBar {progress} compact /></div>
      {/if}
    {/snippet}
    {#snippet tabs()}
      <nav class="tabs" aria-label={t("project.views")}>
        {#each visibleTabs as tab (tab.view)}
          <a
            href="#/p/{project}{tab.suffix}"
            aria-current={tabActive(tab.view) ? "page" : undefined}
            ><Icon name={icons[tab.view]} size={16} />{t(tab.label)}</a
          >
        {/each}
        {#if repository}
          <a
            class="repo"
            href={repository.url}
            target="_blank"
            rel="noopener noreferrer"
            title={t("project.repository", { branch: repository.branch })}
            ><Icon name="external" size={16} />{repository.path}</a
          >
        {/if}
      </nav>
    {/snippet}
  </PageHero>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if workflow}
    <div class="view">
      {#if inWork}
        <div class="subtabs" role="tablist" aria-label={t("project.work")}>
          {#each subtabs as sub (sub.view)}
            <a
              href="#/p/{project}{sub.suffix}"
              aria-current={view === sub.view ||
              (view === "overview" && sub.view === "board")
                ? "page"
                : undefined}><Icon name={sub.icon} size={16} />{t(sub.label)}</a
            >
          {/each}
          {#if view === "board" || view === "overview"}
            <button
              type="button"
              class="foldbtn"
              aria-expanded={overviewOpen || view === "overview"}
              aria-controls="project-overview"
              onclick={toggleOverview}
              ><Icon name="overview" size={16} />{t("project.overview")}<Icon
                name={overviewOpen || view === "overview"
                  ? "chevron-up"
                  : "chevron-down"}
                size={16}
              /></button
            >
          {/if}
        </div>
        {#if (view === "board" || view === "overview") && (overviewOpen || view === "overview")}
          <div id="project-overview" class="overviewfold">
            <OverviewView {project} />
          </div>
        {/if}
      {/if}
      {#if view === "timeline"}
        <TimelineView {project} />
      {:else if view === "time"}
        <TimeView {project} enabled={workflow.time_tracking} />
      {:else if view === "board" || view === "overview"}
        <Board {project} {workflow} />
      {:else if view === "list"}
        <ListView {project} {workflow} />
      {:else if view === "backlog"}
        <BacklogView {project} {workflow} />
      {:else if view === "context"}
        <ContextView {project} {role} {user} />
      {:else if view === "milestones"}
        <MilestonesView {project} />
      {:else if view === "issues"}
        <IssuesView {project} {workflow} />
      {:else if view === "readme"}
        <ReadmeView {project} />
      {:else if view === "settings"}
        <ProjectSettings
          {project}
          {name}
          {section}
          {workflow}
          {onchange}
          {user}
          {repository}
        />
      {:else}
        <TreeView {project} />
      {/if}
    </div>
    {#if item}
      {#key item}
        <ItemPanel
          itemKey={item}
          {workflow}
          onclose={() => {
            navigate(viewPath(project, view));
          }}
        />
      {/key}
    {/if}
  {/if}
</section>

<style>
  .project {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    min-height: 0;
  }
  .tile {
    display: grid;
    place-items: center;
    width: 56px;
    height: 56px;
    border-radius: var(--atlas-radius-md);
    background: var(--accent);
    color: #06101f;
    font-size: 26px;
    font-weight: var(--atlas-fw-bold);
    box-shadow: 0 0 0 1px rgb(255 255 255 / 25%);
  }
  .tabs {
    display: flex;
    gap: var(--space-1);
    max-width: 100%;
    overflow-x: auto;
    scrollbar-width: none;
  }
  .tabs a {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 40px;
    padding: 8px 14px;
    border-bottom: 2px solid transparent;
    color: #c5d6ea;
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
    white-space: nowrap;
  }
  .tabs a.repo {
    margin-left: auto;
    font-weight: var(--atlas-fw-regular, 400);
  }
  .tabs a:hover {
    color: #fff;
  }
  .tabs a[aria-current="page"] {
    color: #fff;
    border-bottom-color: #4aa0ff;
    background: linear-gradient(0deg, rgb(74 160 255 / 22%), transparent);
  }
  .heroprogress {
    width: 220px;
    color: #fff;
  }
  .view {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-height: 0;
  }
  .subtabs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-1);
  }
  .subtabs a,
  .foldbtn {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    padding: 6px 12px;
    border: 1px solid transparent;
    border-radius: var(--atlas-radius-sm);
    background: transparent;
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
  }
  .subtabs a:hover,
  .foldbtn:hover {
    color: var(--color-text);
    background: var(--color-soft);
  }
  .subtabs a[aria-current="page"] {
    color: var(--color-accent-soft-text);
    background: var(--color-accent-soft);
  }
  .foldbtn {
    margin-left: auto;
    border-color: var(--color-border);
  }
  /* The views fill what the sub-tab row and the overview leave over. */
  .view > :global(:not(.subtabs, .overviewfold)) {
    flex: 1;
    min-height: 0;
  }
  @media (max-width: 720px) {
    .tile {
      width: 44px;
      height: 44px;
      font-size: 20px;
    }
    .tabs a {
      padding: 8px 10px;
      font-size: var(--atlas-fs-sm);
    }
  }
</style>
