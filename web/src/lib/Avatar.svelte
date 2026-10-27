<script lang="ts">
  import { initials, people } from "./people.svelte";
  import { hashString } from "./projectColor";

  // Initials avatar; the hue comes from the name so a person keeps their colour everywhere.
  let {
    name,
    size = 24,
    userId,
  }: { name: string; size?: number; userId?: number | null } = $props();

  // A person with a profile picture shows it; everyone else gets initials.
  const hasPicture = $derived(
    userId !== undefined &&
      userId !== null &&
      people.users.some((u) => u.id === userId && u.avatar),
  );
  let broken = $state(false);

  const hue = $derived(hashString(name) % 360);
</script>

<span
  class="avatar"
  title={name}
  style:width="{size}px"
  style:height="{size}px"
  style:font-size="{Math.max(9, Math.round(size * 0.42))}px"
  style:--hue={hue}
  aria-hidden="true"
  >{#if hasPicture && !broken}<img
      src="/api/v1/users/{userId}/avatar?v={people.version}"
      alt=""
      onerror={() => (broken = true)}
    />{:else}{initials(name)}{/if}</span
>

<style>
  img {
    width: 100%;
    height: 100%;
    border-radius: 50%;
    object-fit: cover;
  }
  .avatar {
    flex: none;
    overflow: hidden;
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
