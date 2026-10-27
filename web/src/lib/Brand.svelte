<script lang="ts">
  import markDark from "../assets/atlas/mark-dark.png";
  import markLight from "../assets/atlas/mark-light.png";
  import { t } from "./i18n";
  import { theme } from "./theme.svelte";

  let { compact = false, by = true }: { compact?: boolean; by?: boolean } =
    $props();
</script>

<span class="brand">
  <img
    class="mark"
    src={theme.current === "dark" ? markDark : markLight}
    width="128"
    height="128"
    alt=""
  />
  {#if !compact}
    <span class="words">
      <span class="name">{t("app.name")}</span>
      {#if by}<span class="by">{t("app.by")}</span>{/if}
    </span>
  {/if}
</span>

<style>
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-weight: 800;
    letter-spacing: 0.055em;
    font-size: 20px;
    text-transform: uppercase;
    color: var(--color-text);
  }
  .words {
    display: grid;
    gap: 2px;
    line-height: 1;
  }
  .by {
    font-size: 8.5px;
    font-weight: var(--atlas-fw-semibold);
    letter-spacing: 0.12em;
    white-space: nowrap;
    color: var(--atlas-text-subtle);
  }
  .mark {
    width: 34px;
    height: 34px;
  }
  @media (max-width: 600px) {
    .brand {
      font-size: 16px;
      gap: 7px;
    }
    .mark {
      width: 27px;
      height: 27px;
    }
  }
</style>
