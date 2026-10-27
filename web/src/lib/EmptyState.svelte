<script lang="ts">
  import type { Snippet } from "svelte";

  import Icon from "./Icon.svelte";
  import type { IconName } from "./icons";

  let {
    icon,
    title,
    text = "",
    compact = false,
    children,
  }: {
    icon: IconName;
    title: string;
    text?: string;
    compact?: boolean;
    children?: Snippet;
  } = $props();
</script>

<div class="empty" class:compact>
  <span class="art"><Icon name={icon} size={compact ? 20 : 28} /></span>
  <p class="title">{title}</p>
  {#if text}<p class="text">{text}</p>{/if}
  {@render children?.()}
</div>

<style>
  .empty {
    display: grid;
    justify-items: center;
    gap: var(--space-2);
    padding: var(--space-6) var(--space-4);
    text-align: center;
    color: var(--color-muted);
  }
  .empty.compact {
    padding: var(--space-4) var(--space-2);
    gap: 6px;
  }
  .art {
    display: grid;
    place-items: center;
    width: 52px;
    height: 52px;
    border-radius: 50%;
    border: 1px dashed var(--color-border-strong);
    background: var(--color-soft);
    color: var(--color-accent);
  }
  .compact .art {
    width: 38px;
    height: 38px;
  }
  .title {
    margin: 0;
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-text);
  }
  .text {
    margin: 0;
    max-width: 34ch;
    font-size: var(--atlas-fs-sm);
  }
</style>
