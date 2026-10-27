<script lang="ts">
  import type { Snippet } from "svelte";

  import HeroArt from "./HeroArt.svelte";

  // The cinematic page header: breadcrumb, title and description on the left, actions on the right,
  // optional tab row at the bottom. The artwork is decorative and sits behind a readability overlay.
  let {
    title,
    description = "",
    icon,
    crumbs,
    actions,
    tabs,
  }: {
    title: string;
    description?: string;
    icon?: Snippet;
    crumbs?: Snippet;
    actions?: Snippet;
    tabs?: Snippet;
  } = $props();
</script>

<header class="hero" class:with-tabs={Boolean(tabs)}>
  <HeroArt />
  <div class="shade"></div>
  <div class="inner">
    {#if crumbs}<div class="crumbs">{@render crumbs()}</div>{/if}
    <div class="row">
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
