<script lang="ts">
  import { type Workflow } from "./api";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import ItemPanel from "./ItemPanel.svelte";
  import ListView from "./ListView.svelte";
  import { route } from "./router.svelte";
  import { matchProject } from "./routes";

  // Issues are work items of type bug (ADR-0013): the list view with the type fixed, and a button that
  // starts a new bug. Opening one uses the normal item panel.
  let { project, workflow }: { project: string; workflow: Workflow } = $props();

  let drafting = $state(false);
  const openKey = $derived(matchProject(route.path)?.item);
  const status = $derived(
    workflow.statuses.find((st) => st.allow_create)?.name,
  );

  $effect(() => {
    if (openKey) drafting = false;
  });
</script>

<section class="issues">
  <div class="bar">
    <p class="muted">{t("issues.intro")}</p>
    {#if status !== undefined}
      <button
        type="button"
        class="primary new"
        onclick={() => {
          drafting = true;
        }}><Icon name="plus" size={16} />{t("issues.new")}</button
      >
    {/if}
  </div>
  <ListView {project} {workflow} fixedType="bug" />
</section>

{#if drafting && status !== undefined}
  <ItemPanel
    create={{ project, status, type: "bug" }}
    {workflow}
    onclose={() => {
      drafting = false;
    }}
  />
{/if}

<style>
  .issues {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    min-height: 0;
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .bar p {
    margin: 0;
  }
  .new {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 38px;
  }
</style>
