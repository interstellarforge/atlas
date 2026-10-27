<script lang="ts">
  import { type IconName, paths } from "./icons";

  let { name, size = 18 }: { name: IconName; size?: number } = $props();
</script>

<svg
  width={size}
  height={size}
  viewBox="0 0 24 24"
  fill="none"
  stroke="currentColor"
  stroke-width="1.8"
  stroke-linecap="round"
  stroke-linejoin="round"
  aria-hidden="true"
  focusable="false"><path d={paths[name]} /></svg
>

<style>
  svg {
    flex: none;
    display: block;
  }
</style>
