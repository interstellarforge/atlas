<script lang="ts">
  import { type Filters } from "./api";
  import { t } from "./i18n";
  import { people } from "./people.svelte";
  import { cardFields, saveCardFields } from "./prefs.svelte";

  let {
    // eslint-disable-next-line @typescript-eslint/no-useless-default-assignment -- $bindable() is how Svelte 5 declares a two-way prop
    filters = $bindable(),
    labels,
    types,
    showCardFields = false,
  }: {
    filters: Filters;
    labels: string[];
    types: string[];
    showCardFields?: boolean;
  } = $props();

  const active = $derived(
    filters.q !== "" ||
      filters.type !== "" ||
      filters.priority !== "" ||
      filters.label !== "" ||
      filters.assignee !== "" ||
      filters.archived,
  );

  function clear(): void {
    filters = {
      q: "",
      type: "",
      priority: "",
      label: "",
      assignee: "",
      archived: false,
    };
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
  <input
    type="search"
    placeholder={t("filters.search")}
    aria-label={t("filters.search")}
    bind:value={filters.q}
  />
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
  <label class="check">
    <input type="checkbox" bind:checked={filters.archived} />
    {t("filters.archived")}
  </label>
  {#if active}
    <button type="button" onclick={clear}>{t("filters.clear")}</button>
  {/if}
  {#if showCardFields}
    <details class="fields">
      <summary>{t("filters.fields")}</summary>
      <div class="menu">
        {#each [["priority", "filters.showPriority"], ["assignee", "filters.showAssignee"], ["due", "filters.showDue"], ["labels", "filters.showLabels"]] as const as [field, label] (field)}
          <label class="check">
            <input
              type="checkbox"
              bind:checked={cardFields[field]}
              onchange={saveCardFields}
            />
            {t(label)}
          </label>
        {/each}
      </div>
    </details>
  {/if}
</form>

<style>
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .filters input[type="search"] {
    flex: 1 1 10rem;
    max-width: 16rem;
  }
  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .fields {
    position: relative;
  }
  .fields summary {
    cursor: pointer;
    min-height: 40px;
    display: inline-flex;
    align-items: center;
    padding: var(--space-2) var(--space-3);
    font-weight: var(--atlas-fw-semibold);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface);
    list-style: none;
  }
  .menu {
    position: absolute;
    z-index: 5;
    margin-top: var(--space-1);
    padding: var(--space-3);
    display: grid;
    gap: var(--space-2);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    box-shadow: var(--atlas-shadow-md);
  }
</style>
