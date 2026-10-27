<script lang="ts">
  import { type Tone, toneVar } from "./statusTone";

  let { name, tone }: { name: string; tone: Tone } = $props();
</script>

<span class="status" style:--tone={toneVar(tone)}>
  <span class="dot" aria-hidden="true"></span>{name}
</span>

<style>
  .status {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    max-width: 100%;
    padding: 2px 10px 2px 8px;
    border: 1px solid color-mix(in srgb, var(--tone) 55%, transparent);
    border-radius: var(--atlas-radius-pill);
    background: color-mix(in srgb, var(--tone) 14%, transparent);
    color: var(--color-text);
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--tone);
  }
</style>
