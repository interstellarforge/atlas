<script lang="ts">
  import type { Progress } from "./api";
  import { t } from "./i18n";
  import { statusTone, toneVar } from "./statusTone";

  type Mode = "status" | "category" | "priority" | "type";

  let { progress, compact = false }: { progress: Progress; compact?: boolean } =
    $props();

  let mode = $state<Mode>("status");

  const MODES: {
    mode: Mode;
    label:
      | "progress.byStatus"
      | "progress.byCategory"
      | "progress.byPriority"
      | "progress.byType";
  }[] = [
    { mode: "status", label: "progress.byStatus" },
    { mode: "category", label: "progress.byCategory" },
    { mode: "priority", label: "progress.byPriority" },
    { mode: "type", label: "progress.byType" },
  ];
  const PRIORITY_COLOR: Record<string, string> = {
    low: "var(--atlas-st-backlog)",
    normal: "var(--atlas-st-todo)",
    high: "var(--atlas-st-active)",
    urgent: "var(--atlas-danger)",
  };
  const TYPE_COLOR: Record<string, string> = {
    epic: "var(--atlas-st-review)",
    story: "var(--atlas-st-todo)",
    task: "var(--atlas-st-done)",
    bug: "var(--atlas-danger)",
  };

  interface Segment {
    id: string;
    label: string;
    count: number;
    color: string;
  }

  const segments = $derived.by((): Segment[] => {
    const current = compact ? "status" : mode;
    let list: Segment[];
    if (current === "category") {
      list = (["todo", "active", "review", "completed"] as const).map((c) => ({
        id: c,
        label: t(`workflow.cat.${c}`),
        count: progress.categories[c],
        color: toneVar(c === "completed" ? "done" : c),
      }));
    } else if (current === "priority") {
      list = ["urgent", "high", "normal", "low"].map((p) => ({
        id: p,
        label: t(`priority.${p}` as "priority.low"),
        count: progress.priorities[p] ?? 0,
        color: PRIORITY_COLOR[p] ?? "",
      }));
    } else if (current === "type") {
      list = ["epic", "story", "task", "bug"].map((k) => ({
        id: k,
        label: t(`types.${k}` as "types.bug"),
        count: progress.types[k] ?? 0,
        color: TYPE_COLOR[k] ?? "",
      }));
    } else {
      list = progress.statuses.map((s) => ({
        id: String(s.id),
        label: s.name,
        count: s.count,
        color: toneVar(statusTone(s, progress.statuses)),
      }));
    }
    return list;
  });

  const done = $derived(progress.categories.completed);
  const percent = $derived(
    progress.total ? Math.round((done / progress.total) * 100) : 0,
  );
  const summary = $derived(
    t("progress.summary", { done, total: progress.total, percent }),
  );
  const alt = $derived(
    `${summary}. ${segments
      .filter((s) => s.count > 0)
      .map((s) => `${s.label}: ${String(s.count)}`)
      .join(", ")}`,
  );
</script>

<div class="progress" class:compact>
  <div class="bar" role="img" aria-label={alt}>
    {#each segments.filter((s) => s.count > 0) as s (s.id)}
      <span style:flex-grow={s.count} style:background={s.color}></span>
    {/each}
  </div>
  {#if compact}
    <span class="meta">{done}/{progress.total}</span>
  {:else}
    <p class="summary">{summary}</p>
    <div class="modes" role="group" aria-label={t("progress.by")}>
      {#each MODES as m (m.mode)}
        <button
          type="button"
          class="mode"
          aria-pressed={mode === m.mode}
          onclick={() => (mode = m.mode)}>{t(m.label)}</button
        >
      {/each}
    </div>
    <ul class="legend">
      {#each segments as s (s.id)}
        <li class:zero={s.count === 0}>
          <i style:background={s.color}></i>
          <span class="name">{s.label}</span>
          <strong>{s.count}</strong>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .progress {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .progress.compact {
    flex-direction: row;
    align-items: center;
  }
  .bar {
    display: flex;
    gap: 2px;
    height: 14px;
    flex: 1;
    min-width: 80px;
    border-radius: 7px;
    overflow: hidden;
    background: var(--atlas-surface-soft);
  }
  .compact .bar {
    height: 8px;
  }
  .bar span {
    flex-basis: 0;
    min-width: 4px;
  }
  .meta {
    font-size: var(--atlas-fs-sm);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .summary {
    margin: 0;
    font-weight: var(--atlas-fw-semibold);
  }
  .modes {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-1);
  }
  .mode[aria-pressed="true"] {
    font-weight: var(--atlas-fw-bold);
    outline: 2px solid currentcolor;
  }
  .legend {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
    gap: var(--space-1) var(--space-3);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .legend li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .legend li.zero {
    opacity: 0.6;
  }
  .legend i {
    width: 12px;
    height: 12px;
    border-radius: 3px;
    flex: none;
  }
  .name {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
