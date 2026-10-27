<script lang="ts">
  import logoDark from "../assets/atlas/logo-dark.webp";
  import logoLight from "../assets/atlas/logo-light.webp";
  import markDark from "../assets/atlas/mark-dark.png";
  import markLight from "../assets/atlas/mark-light.png";
  import { t } from "./i18n";
  import { theme } from "./theme.svelte";

  let { compact = false }: { compact?: boolean } = $props();
  const dark = $derived(theme.current === "dark");
</script>

{#if compact}
  <img
    class="mark"
    src={dark ? markDark : markLight}
    width="128"
    height="128"
    alt={t("app.name")}
  />
{:else}
  <img
    class="logo"
    src={dark ? logoDark : logoLight}
    width="1600"
    height="380"
    alt={t("app.name")}
  />
{/if}

<style>
  .mark {
    width: 34px;
    height: 34px;
  }
  /* The logo art has a baked background; fade its edges so it blends into the surface. */
  .logo {
    display: block;
    height: 44px;
    width: auto;
    mask-image:
      linear-gradient(90deg, transparent, #000 10%, #000 90%, transparent),
      linear-gradient(0deg, transparent, #000 12%, #000 88%, transparent);
    mask-composite: intersect;
  }
</style>
