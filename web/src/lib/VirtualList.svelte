<script lang="ts" generics="T">
  import type { Snippet } from "svelte";

  // Renders only the rows in (and just around) the viewport. Rows have a fixed height, which keeps the
  // maths trivial and lets other code (drag and drop) map a pointer position to a row index.
  let {
    items,
    itemHeight,
    itemKey,
    row,
    overscan = 6,
    onNearEnd,
    scroller = $bindable(),
  }: {
    items: T[];
    itemHeight: number;
    itemKey: (item: T) => string;
    row: Snippet<[T, number]>;
    overscan?: number;
    onNearEnd?: () => void;
    scroller?: HTMLElement | undefined;
  } = $props();

  let scrollTop = $state(0);
  let viewport = $state(0);

  const start = $derived(
    Math.max(0, Math.floor(scrollTop / itemHeight) - overscan),
  );
  const end = $derived(
    Math.min(
      items.length,
      Math.ceil((scrollTop + viewport) / itemHeight) + overscan,
    ),
  );
  const visible = $derived(items.slice(start, end));

  $effect(() => {
    if (onNearEnd && viewport > 0 && end >= items.length - 2) onNearEnd();
  });
</script>

<div
  class="scroller"
  bind:this={scroller}
  bind:clientHeight={viewport}
  onscroll={(e) => (scrollTop = e.currentTarget.scrollTop)}
>
  <div class="space" style:height="{items.length * itemHeight}px">
    <div class="window" style:transform="translateY({start * itemHeight}px)">
      {#each visible as item, i (itemKey(item))}
        <div class="row" style:height="{itemHeight}px">
          {@render row(item, start + i)}
        </div>
      {/each}
    </div>
  </div>
</div>

<style>
  .scroller {
    height: 100%;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .space {
    position: relative;
  }
  .window {
    will-change: transform;
  }
  .row {
    box-sizing: border-box;
  }
</style>
