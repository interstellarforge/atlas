<script lang="ts">
  import Icon from "./Icon.svelte";
  import type { IconName } from "./icons";

  let { type, size = 16 }: { type: string; size?: number } = $props();

  const icons: Record<string, IconName> = {
    epic: "epic",
    story: "story",
    task: "task",
    bug: "bug",
  };
</script>

<span class="type {type}" aria-hidden="true"
  ><Icon name={icons[type] ?? "task"} {size} /></span
>

<style>
  .type {
    display: inline-grid;
    place-items: center;
    flex: none;
    padding: 4px;
    border-radius: var(--atlas-radius-sm);
  }
  .epic {
    color: var(--atlas-purple);
    background: var(--atlas-purple-soft);
  }
  .story {
    color: var(--atlas-info);
    background: var(--atlas-info-soft);
  }
  .task {
    color: var(--color-muted);
    background: var(--color-soft);
  }
  .bug {
    color: var(--color-danger);
    background: var(--color-danger-soft);
  }
</style>
