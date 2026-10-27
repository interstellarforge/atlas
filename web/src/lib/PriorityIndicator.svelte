<script lang="ts">
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import type { IconName } from "./icons";

  let { priority, label = true }: { priority: string; label?: boolean } =
    $props();

  const icons: Record<string, IconName> = {
    urgent: "chevrons-up",
    high: "arrow-up",
    normal: "minus",
    low: "arrow-down",
  };
</script>

<span class="prio {priority}">
  <Icon name={icons[priority] ?? "minus"} size={14} />
  {#if label}{t(`priority.${priority}` as "priority.low")}{/if}
</span>

<style>
  .prio {
    display: inline-flex;
    align-items: center;
    gap: 3px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    white-space: nowrap;
  }
  .urgent,
  .high {
    color: var(--color-danger);
  }
  .normal {
    color: var(--atlas-warning);
  }
  .low {
    color: var(--atlas-info);
  }
</style>
