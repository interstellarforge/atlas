<script lang="ts">
  import type { Snippet } from "svelte";

  import HeroArt from "./HeroArt.svelte";
  import Icon from "./Icon.svelte";

  // The cinematic page header: breadcrumb, title and description on the left, actions on the right,
  // optional tab row at the bottom. The artwork is decorative and sits behind a readability overlay.
  let {
    title,
    description = "",
    icon,
    crumbs,
    actions,
    tabs,
    collapsed = false,
    toggleLabel = "",
    ontoggle,
  }: {
    title: string;
    description?: string;
    icon?: Snippet;
    crumbs?: Snippet;
    actions?: Snippet;
    tabs?: Snippet;
    collapsed?: boolean;
    toggleLabel?: string;
    ontoggle?: () => void;
  } = $props();
</script>

<header class="hero" class:with-tabs={Boolean(tabs)} class:collapsed>
  <HeroArt />
  <div class="shade"></div>
  {#if ontoggle}
    <button
      type="button"
      class="toggle"
      aria-label={toggleLabel}
      title={toggleLabel}
      aria-expanded={!collapsed}
      onclick={ontoggle}
      ><Icon
        name={collapsed ? "chevron-down" : "chevron-up"}
        size={18}
      /></button
    >
  {/if}
  <div class="inner">
    {#if crumbs}<div class="crumbs" class:sr-only={collapsed}>
        {@render crumbs()}
      </div>{/if}
    <div class="row" class:sr-only={collapsed}>
      {#if icon}<div class="icon">{@render icon()}</div>{/if}
      <div class="text">
        <h1>{title}</h1>
        {#if description}<p>{description}</p>{/if}
      </div>
      {#if actions}<div class="actions">{@render actions()}</div>{/if}
    </div>
    {#if tabs}<div class="tabs">{@render tabs()}</div>{/if}
  </div>
</header>

<style>
  .hero {
    position: relative;
    isolation: isolate;
    overflow: hidden;
    flex: none;
    min-height: 156px;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
    background: linear-gradient(
      115deg,
      var(--atlas-hero-1),
      var(--atlas-hero-2)
    );
    color: var(--atlas-hero-text);
  }
  .hero.with-tabs {
    min-height: 164px;
  }
  .hero.collapsed {
    min-height: 52px;
  }
  .shade {
    position: absolute;
    inset: 0;
    background: linear-gradient(
      90deg,
      rgb(4 10 22 / 78%) 0%,
      rgb(4 10 22 / 35%) 55%,
      transparent 100%
    );
  }
  .inner {
    position: relative;
    display: flex;
    flex-direction: column;
    justify-content: flex-end;
    gap: var(--space-2);
    height: 100%;
    min-height: inherit;
    padding: 18px 28px 0;
  }
  .hero:not(.with-tabs) .inner {
    padding-bottom: 20px;
  }
  .collapsed .inner {
    padding: 0 56px 0 16px;
  }
  .crumbs {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    color: #c5d6ea;
  }
  .crumbs :global(a) {
    color: #d7e8ff;
  }
  .row {
    display: flex;
    align-items: center;
    gap: var(--space-4);
  }
  .icon {
    flex: none;
  }
  .text {
    flex: 1;
    min-width: 0;
  }
  h1 {
    color: #fff;
    font-size: var(--atlas-fs-2xl);
    letter-spacing: -0.03em;
    overflow-wrap: anywhere;
  }
  p {
    margin: 4px 0 0;
    max-width: 62ch;
    color: #c5d6ea;
    font-size: var(--atlas-fs-md);
  }
  .actions {
    flex: none;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .tabs {
    padding-top: var(--space-2);
  }
  .collapsed .tabs {
    padding-top: 0;
  }
  .toggle {
    position: absolute;
    top: 8px;
    right: 8px;
    z-index: 1;
    display: inline-grid;
    place-items: center;
    width: 36px;
    min-height: 36px;
    height: 36px;
    padding: 0;
    color: #fff;
    background: rgb(4 10 22 / 58%);
    border-color: rgb(255 255 255 / 28%);
  }
  .toggle:hover:not(:disabled) {
    background: rgb(4 10 22 / 78%);
    border-color: rgb(255 255 255 / 45%);
  }
  @media (max-width: 720px) {
    .hero,
    .hero.with-tabs {
      min-height: 132px;
    }
    .inner {
      padding: 14px 16px 0;
    }
    .hero:not(.with-tabs) .inner {
      padding-bottom: 14px;
    }
    .hero.collapsed {
      min-height: 52px;
    }
    .collapsed .inner {
      padding: 0 56px 0 8px;
    }
    .row {
      flex-wrap: wrap;
      gap: var(--space-3);
    }
    h1 {
      font-size: 22px;
    }
    p {
      display: -webkit-box;
      -webkit-line-clamp: 2;
      line-clamp: 2;
      -webkit-box-orient: vertical;
      overflow: hidden;
    }
  }
</style>
