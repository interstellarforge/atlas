<script lang="ts">
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { cardFields, saveCardFields } from "./prefs.svelte";
</script>

<details class="fields">
  <summary aria-label={t("filters.fields")}
    ><Icon name="eye" size={16} /><span class="lbl">{t("filters.fields")}</span
    ></summary
  >
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

<style>
  .fields {
    position: relative;
  }
  summary {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    min-height: 38px;
    padding: 0 14px;
    cursor: pointer;
    font-weight: var(--atlas-fw-semibold);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    background: var(--color-surface);
    list-style: none;
  }
  @media (max-width: 900px) {
    .lbl {
      display: none;
    }
    summary {
      padding: 0 11px;
    }
  }
  summary::-webkit-details-marker {
    display: none;
  }
  summary:hover {
    background: var(--color-hover);
  }
  .menu {
    position: absolute;
    right: 0;
    z-index: 30;
    margin-top: var(--space-1);
    min-width: 200px;
    padding: var(--space-3);
    display: grid;
    gap: var(--space-2);
    background: var(--atlas-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    box-shadow: var(--atlas-shadow-lg);
  }
  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--atlas-fs-md);
  }
</style>
