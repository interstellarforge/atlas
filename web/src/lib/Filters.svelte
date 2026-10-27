<script lang="ts">
  import { emptyFilters, type Filters, type Milestone } from "./api";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { people } from "./people.svelte";

  let {
    // eslint-disable-next-line @typescript-eslint/no-useless-default-assignment -- $bindable() is how Svelte 5 declares a two-way prop
    filters = $bindable(),
    labels,
    milestones,
    types,
  }: {
    filters: Filters;
    labels: string[];
    milestones: Milestone[];
    types: string[];
  } = $props();

  const active = $derived(
    filters.q !== "" ||
      filters.type !== "" ||
      filters.priority !== "" ||
      filters.label !== "" ||
      filters.milestone !== "" ||
      filters.assignee !== "" ||
      filters.archived,
  );

  function clear(): void {
    filters = { ...emptyFilters };
  }
</script>

<form
  class="filters"
  role="search"
  aria-label={t("filters.label")}
  onsubmit={(e) => {
    e.preventDefault();
  }}
>
  <div class="search">
    <Icon name="search" size={16} />
    <input
      type="search"
      placeholder={t("filters.search")}
      aria-label={t("filters.search")}
      bind:value={filters.q}
    />
  </div>
  <select aria-label={t("filters.type")} bind:value={filters.type}>
    <option value="">{t("filters.anyType")}</option>
    {#each types as typ (typ)}
      <option value={typ}>{t(`types.${typ}` as "types.epic")}</option>
    {/each}
  </select>
  <select aria-label={t("filters.priority")} bind:value={filters.priority}>
    <option value="">{t("filters.anyPriority")}</option>
    {#each ["urgent", "high", "normal", "low"] as p (p)}
      <option value={p}>{t(`priority.${p}` as "priority.low")}</option>
    {/each}
  </select>
  <select aria-label={t("filters.assignee")} bind:value={filters.assignee}>
    <option value="">{t("filters.anyone")}</option>
    {#each people.users as user (user.id)}
      <option value={String(user.id)}>{user.name}</option>
    {/each}
  </select>
  {#if labels.length > 0}
    <select aria-label={t("filters.tag")} bind:value={filters.label}>
      <option value="">{t("filters.anyTag")}</option>
      {#each labels as label (label)}
        <option value={label}>{label}</option>
      {/each}
    </select>
  {/if}
  {#if milestones.length > 0}
    <select aria-label={t("filters.milestone")} bind:value={filters.milestone}>
      <option value="">{t("filters.anyMilestone")}</option>
      {#each milestones as m (m.id)}
        <option value={m.name}>{m.name}</option>
      {/each}
    </select>
  {/if}
  <label class="check">
    <input type="checkbox" bind:checked={filters.archived} />
    {t("filters.archived")}
  </label>
  {#if active}
    <button type="button" class="clear" onclick={clear}
      >{t("filters.clear")}</button
    >
  {/if}
</form>

<style>
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1 1 12rem;
    max-width: 18rem;
    padding-left: 11px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-muted);
  }
  .search:focus-within {
    border-color: var(--color-accent);
  }
  .search input {
    flex: 1;
    min-height: 38px;
    padding-left: 0;
    border: none;
    background: transparent;
  }
  .search input:focus-visible {
    outline: none;
    box-shadow: none;
  }
  .filters select {
    min-height: 38px;
    padding-top: 6px;
    padding-bottom: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .clear {
    min-height: 38px;
    border-color: transparent;
    background: transparent;
    color: var(--color-accent);
  }
</style>
