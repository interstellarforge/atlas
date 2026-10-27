<script lang="ts" module>
  let counter = 0;
</script>

<script lang="ts">
  import { t } from "./i18n";

  // Gradient ids must be unique per instance: a hidden copy (the sidebar on a phone) would otherwise break the others.
  const uid = `atlas-brand-${String(++counter)}`;
  let { compact = false, by = true }: { compact?: boolean; by?: boolean } =
    $props();
</script>

<span class="brand">
  <svg viewBox="0 0 80 80" fill="none" aria-hidden="true" class="mark">
    <defs>
      <linearGradient id="{uid}-blue" x1="0" y1="0" x2="1" y2="1">
        <stop style="stop-color: var(--atlas-brand-1)" />
        <stop offset="1" style="stop-color: var(--atlas-brand-2)" />
      </linearGradient>
      <linearGradient id="{uid}-orbit" x1="0" y1="0" x2="1" y2="1">
        <stop stop-color="#3D85DE" />
        <stop offset="1" stop-color="#FFAD66" />
      </linearGradient>
    </defs>
    <path
      d="M14 61L38 14a3 3 0 0 1 5 0l24 47h-12L40.5 31 26 61H14Z"
      fill="url(#{uid}-blue)"
    />
    <path
      d="M18 25a32 32 0 0 1 40-7M67 31a31 31 0 0 1-5 33"
      stroke="url(#{uid}-orbit)"
      stroke-width="4"
      stroke-linecap="round"
    />
    <circle cx="61" cy="20" r="6" fill="#FFB26B" />
  </svg>
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
