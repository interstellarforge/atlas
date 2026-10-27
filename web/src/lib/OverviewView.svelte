<script lang="ts">
  import { getProgress, type Progress } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import ProgressBar from "./ProgressBar.svelte";

  let { project }: { project: string } = $props();

  let progress = $state<Progress | undefined>();
  let error = $state("");
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function load(): Promise<void> {
    try {
      progress = await getProgress(project);
      error = "";
    } catch {
      error = t("progress.failed");
    }
  }

  $effect(() => {
    if (project) void load();
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 250);
    }),
  );
  $effect(() => onResync(() => void load()));
</script>

<section class="overview">
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if progress}
    {#if progress.total === 0}
      <EmptyState icon="overview" title={t("progress.empty")} />
    {:else}
      <div class="card">
        <h2>{t("progress.title")}</h2>
        <ProgressBar {progress} />
      </div>
    {/if}
  {/if}
</section>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-width: 900px;
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
</style>
