<script lang="ts">
  import { getWorkflow, type Workflow } from "./api";
  import BacklogView from "./BacklogView.svelte";
  import Board from "./Board.svelte";
  import { onChange } from "./events.svelte";
  import { t } from "./i18n";
  import ItemPanel from "./ItemPanel.svelte";
  import ListView from "./ListView.svelte";
  import { navigate } from "./router.svelte";
  import { type ProjectViewName, viewPath } from "./routes";
  import TreeView from "./TreeView.svelte";

  let {
    project,
    view,
    item,
  }: { project: string; view: ProjectViewName; item: string | undefined } =
    $props();

  let workflow = $state<Workflow | undefined>();
  let error = $state("");

  const tabs: {
    view: ProjectViewName;
    suffix: string;
    label:
      "project.board" | "project.list" | "project.backlog" | "project.tree";
  }[] = [
    { view: "board", suffix: "", label: "project.board" },
    { view: "list", suffix: "/list", label: "project.list" },
    { view: "backlog", suffix: "/backlog", label: "project.backlog" },
    { view: "tree", suffix: "/tree", label: "project.tree" },
  ];

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
  <div class="head">
    <a href="#/">{t("project.back")}</a>
    <h1>{project}</h1>
    <nav aria-label={t("project.views")}>
      {#each tabs as tab (tab.view)}
        <a
          href="#/p/{project}{tab.suffix}"
          aria-current={view === tab.view ? "page" : undefined}
          >{t(tab.label)}</a
        >
      {/each}
    </nav>
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  {#if workflow}
    <div class="view">
      {#if view === "board"}
        <Board {project} {workflow} />
      {:else if view === "list"}
        <ListView {project} {workflow} />
      {:else if view === "backlog"}
        <BacklogView {project} {workflow} />
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
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-height: 0;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: var(--space-4);
  }
  nav {
    display: flex;
    gap: var(--space-1);
  }
  nav a {
    padding: var(--space-1) var(--space-3);
    text-decoration: none;
    color: var(--color-muted);
    border-radius: var(--radius);
  }
  nav a[aria-current="page"] {
    color: var(--color-text);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
  }
  .view {
    flex: 1;
    min-height: 0;
  }
</style>
