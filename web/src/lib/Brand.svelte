<script lang="ts">
  import { t } from "./i18n";
</script>

<span class="brand">
  <svg viewBox="0 0 80 80" fill="none" aria-hidden="true" class="mark">
    <defs>
      <linearGradient id="atlas-blue" x1="0" y1="0" x2="1" y2="1">
        <stop stop-color="#BCE2FF" />
        <stop offset="1" stop-color="#3C86EF" />
      </linearGradient>
      <linearGradient id="atlas-orbit" x1="0" y1="0" x2="1" y2="1">
        <stop stop-color="#3D85DE" />
        <stop offset="1" stop-color="#FFAD66" />
      </linearGradient>
    </defs>
    <path
      d="M14 61L38 14a3 3 0 0 1 5 0l24 47h-12L40.5 31 26 61H14Z"
      fill="url(#atlas-blue)"
    />
    <path
      d="M18 25a32 32 0 0 1 40-7M67 31a31 31 0 0 1-5 33"
      stroke="url(#atlas-orbit)"
      stroke-width="4"
      stroke-linecap="round"
    />
    <circle cx="61" cy="20" r="6" fill="#FFB26B" />
  </svg>
  <span class="name">{t("app.name")}</span>
</span>

<style>
  .brand {
    display: inline-flex;
    align-items: center;
    gap: 10px;
    font-weight: 800;
    letter-spacing: 0.055em;
    font-size: 18px;
    text-transform: uppercase;
    color: var(--color-text);
  }
  .mark {
    width: 32px;
    height: 32px;
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
