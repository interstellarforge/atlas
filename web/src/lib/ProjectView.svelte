<script lang="ts">
  import { getWorkflow, type User, type Workflow } from "./api";
  import BacklogView from "./BacklogView.svelte";
  import Board from "./Board.svelte";
  import { onChange } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import ItemPanel from "./ItemPanel.svelte";
  import ListView from "./ListView.svelte";
  import MilestonesView from "./MilestonesView.svelte";
  import PageHero from "./PageHero.svelte";
  import { projectColor } from "./projectColor";
  import ProjectSettings from "./ProjectSettings.svelte";
  import { navigate } from "./router.svelte";
  import { type ProjectViewName, viewPath } from "./routes";
  import TreeView from "./TreeView.svelte";

  let {
    project,
    name = "",
    canAdmin = false,
    user,
    section = "",
    onchange = () => undefined,
    view,
    item,
  }: {
    project: string;
    name?: string;
    canAdmin?: boolean;
    user: User;
    section?: string;
    onchange?: () => void;
    view: ProjectViewName;
    item: string | undefined;
  } = $props();

  let workflow = $state<Workflow | undefined>();
  let error = $state("");

  const tabs: {
    view: ProjectViewName;
    suffix: string;
    label:
      | "project.board"
      | "project.list"
      | "project.backlog"
      | "project.tree"
      | "project.milestones"
      | "project.settings";
  }[] = [
    { view: "board", suffix: "", label: "project.board" },
    { view: "list", suffix: "/list", label: "project.list" },
    { view: "backlog", suffix: "/backlog", label: "project.backlog" },
    { view: "tree", suffix: "/tree", label: "project.tree" },
    { view: "milestones", suffix: "/milestones", label: "project.milestones" },
    { view: "settings", suffix: "/settings", label: "project.settings" },
  ];
  const icons = {
    board: "board",
    list: "list",
    backlog: "backlog",
    tree: "tree",
    milestones: "milestone",
    settings: "settings",
  } as const;
  const visibleTabs = $derived(
    tabs.filter((tab) => tab.view !== "settings" || canAdmin),
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
    {#snippet tabs()}
      <nav class="tabs" aria-label={t("project.views")}>
        {#each visibleTabs as tab (tab.view)}
          <a
            href="#/p/{project}{tab.suffix}"
            aria-current={view === tab.view ? "page" : undefined}
            ><Icon name={icons[tab.view]} size={16} />{t(tab.label)}</a
          >
        {/each}
      </nav>
    {/snippet}
  </PageHero>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if workflow}
    <div class="view">
      {#if view === "board"}
        <Board {project} {workflow} />
      {:else if view === "list"}
        <ListView {project} {workflow} />
      {:else if view === "backlog"}
        <BacklogView {project} {workflow} />
      {:else if view === "milestones"}
        <MilestonesView {project} />
      {:else if view === "settings"}
        <ProjectSettings
          {project}
          {name}
          {section}
          {workflow}
          {onchange}
          {user}
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
  .tabs a:hover {
    color: #fff;
  }
  .tabs a[aria-current="page"] {
    color: #fff;
    border-bottom-color: #4aa0ff;
    background: linear-gradient(0deg, rgb(74 160 255 / 22%), transparent);
  }
  .view {
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
