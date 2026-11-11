<script lang="ts">
  import type { Project } from "./api";
  import { t } from "./i18n";

  // Multi-select of an organization's projects (assignment and invitation dialogs).
  let {
    projects,
    selected = $bindable([]),
    legend,
  }: { projects: Project[]; selected?: string[]; legend: string } = $props();
</script>

<fieldset class="picker">
  <legend>{legend}</legend>
  {#each projects as p (p.key)}
    <label class="opt">
      <input type="checkbox" value={p.key} bind:group={selected} />
      {p.name} <span class="muted">{p.key}</span>
    </label>
  {:else}
    <p class="muted">{t("company.picker.none")}</p>
  {/each}
</fieldset>

<style>
  .picker {
    border: 1px solid var(--color-border);
    border-radius: var(--radius-2, 8px);
    padding: var(--space-2) var(--space-3);
    margin: 0;
  }
  .opt {
    display: flex;
    gap: var(--space-2);
    align-items: center;
    padding: 4px 0;
  }
</style>
