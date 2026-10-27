<script lang="ts">
  import { getWorkflow, type Workflow } from "./api";
  import BacklogView from "./BacklogView.svelte";
  import Board from "./Board.svelte";
  import { onChange } from "./events.svelte";
  import { t } from "./i18n";
  import ItemPanel from "./ItemPanel.svelte";
  import ListView from "./ListView.svelte";
  import MilestonesView from "./MilestonesView.svelte";
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
      | "project.board"
      | "project.list"
      | "project.backlog"
      | "project.tree"
      | "project.milestones";
  }[] = [
    { view: "board", suffix: "", label: "project.board" },
    { view: "list", suffix: "/list", label: "project.list" },
    { view: "backlog", suffix: "/backlog", label: "project.backlog" },
    { view: "tree", suffix: "/tree", label: "project.tree" },
    { view: "milestones", suffix: "/milestones", label: "project.milestones" },
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
  <div class="hero">
    <a class="back" href="#/">{t("project.back")}</a>
    <h1>{project}</h1>
  </div>
  <nav class="tabs" aria-label={t("project.views")}>
    {#each tabs as tab (tab.view)}
      <a
        href="#/p/{project}{tab.suffix}"
        aria-current={view === tab.view ? "page" : undefined}>{t(tab.label)}</a
      >
    {/each}
  </nav>
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
  .hero {
    position: relative;
    overflow: hidden;
    isolation: isolate;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: var(--space-2);
    min-height: 120px;
    padding: 20px 28px;
    border-radius: var(--radius-lg);
    background: linear-gradient(
      115deg,
      var(--atlas-hero-1),
      var(--atlas-hero-2)
    );
    color: var(--atlas-hero-text);
  }
  .hero::before {
    content: "";
    position: absolute;
    inset: 0;
    z-index: -1;
    opacity: 0.75;
    pointer-events: none;
    background:
      radial-gradient(circle at 71% 29%, #fff 0 1px, transparent 2px),
      radial-gradient(circle at 89% 15%, #fff 0 1px, transparent 2px),
      radial-gradient(circle at 49% 62%, #8bc7ff 0 1px, transparent 2px),
      radial-gradient(
        ellipse 31% 90% at 104% 115%,
        rgb(79 165 255 / 65%) 1%,
        transparent 70%
      );
  }
  .hero h1 {
    color: #fff;
    font-size: var(--atlas-fs-2xl);
    letter-spacing: -0.035em;
  }
  .back {
    display: inline-block;
    padding: 4px 0; /* WCAG 2.2 target size minimum */
    color: #d7e8ff;
    font-size: var(--atlas-fs-sm);
  }
  .tabs {
    display: flex;
    gap: var(--space-1);
    width: max-content;
    max-width: 100%;
    padding: var(--space-1);
    overflow: auto;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
  }
  .tabs a {
    display: inline-flex;
    align-items: center;
    min-height: 36px;
    padding: 7px 15px;
    border-radius: var(--radius);
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
    text-decoration: none;
    white-space: nowrap;
  }
  .tabs a:hover {
    background: var(--color-soft);
  }
  .tabs a[aria-current="page"] {
    color: var(--color-accent-soft-text);
    background: var(--color-accent-soft);
  }
  .view {
    flex: 1;
    min-height: 0;
  }
  @media (max-width: 600px) {
    .hero {
      min-height: 100px;
      padding: 16px;
      border-radius: var(--radius-md);
    }
    .tabs {
      width: 100%;
    }
    .tabs a {
      padding: 7px 10px;
      font-size: var(--atlas-fs-sm);
    }
  }
</style>
