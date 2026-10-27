<script lang="ts">
  import { initials } from "./people.svelte";
  import { hashString } from "./projectColor";

  // Initials avatar; the hue comes from the name so a person keeps their colour everywhere.
  let { name, size = 24 }: { name: string; size?: number } = $props();

  const hue = $derived(hashString(name) % 360);
</script>

<span
  class="avatar"
  title={name}
  style:width="{size}px"
  style:height="{size}px"
  style:font-size="{Math.max(9, Math.round(size * 0.42))}px"
  style:--hue={hue}
  aria-hidden="true">{initials(name)}</span
>

<style>
  .avatar {
    flex: none;
    display: inline-grid;
    place-items: center;
    border-radius: 50%;
    font-weight: var(--atlas-fw-bold);
    color: var(--color-text);
    border: 1px solid
      color-mix(in srgb, hsl(var(--hue) 75% 60%) 60%, transparent);
    background: color-mix(
      in srgb,
      hsl(var(--hue) 75% 52%) 28%,
      var(--color-surface)
    );
  }
</style>
