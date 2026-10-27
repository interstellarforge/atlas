<script lang="ts">
  import {
    type ActivityEntry,
    getActivity,
    getTimeline,
    type Timeline,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import { actorName } from "./people.svelte";
  import { itemHref } from "./routes";

  let { project }: { project: string } = $props();

  type Zoom = "week" | "month" | "quarter";
  const DAY = 86_400_000;
  // Days shown per zoom level: four weeks, three months, one year.
  const SPAN: Record<Zoom, number> = { week: 28, month: 90, quarter: 365 };

  let tab = $state<"plan" | "activity">("plan");
  let zoom = $state<Zoom>("month");
  let chart = $state(true);
  let start = $state(startOfDay(Date.now()) - 22 * DAY);
  let timeline = $state<Timeline | undefined>();
  let activity = $state<ActivityEntry[]>([]);
  let more = $state(false);
  let error = $state("");
  let timer: ReturnType<typeof setTimeout> | undefined;

  const from = $derived(start);
  const to = $derived(start + SPAN[zoom] * DAY);
  const date = new Intl.DateTimeFormat(undefined, { dateStyle: "medium" });
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
  const tickFormat = $derived(
    new Intl.DateTimeFormat(undefined, {
      month: "short",
      ...(zoom === "week" && { day: "numeric" }),
    }),
  );

  function startOfDay(ms: number): number {
    const d = new Date(ms);
    return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
  }

  function pct(ms: number): number {
    return Math.min(100, Math.max(0, ((ms - from) / (to - from)) * 100));
  }

  // Weekly ticks for the week zoom, otherwise the first of each month, so labels match the dates they sit on.
  const ticks = $derived.by(() => {
    const out: { at: number; label: string }[] = [];
    if (zoom === "week") {
      for (let ms = from; ms < to; ms += 7 * DAY)
        out.push({ at: pct(ms), label: tickFormat.format(ms) });
      return out;
    }
    const d = new Date(from);
    for (
      let ms = new Date(d.getFullYear(), d.getMonth() + 1, 1).getTime();
      ms < to;
      ms = new Date(
        new Date(ms).getFullYear(),
        new Date(ms).getMonth() + 1,
        1,
      ).getTime()
    )
      out.push({ at: pct(ms), label: tickFormat.format(ms) });
    return out;
  });

  async function loadPlan(): Promise<void> {
    try {
      timeline = await getTimeline(project, from, to);
      error = "";
    } catch {
      error = t("timeline.failed");
    }
  }

  async function loadActivity(before = 0): Promise<void> {
    try {
      const page = await getActivity(project, before);
      activity = before ? [...activity, ...page] : page;
      more = page.length >= 50;
      error = "";
    } catch {
      error = t("timeline.failed");
    }
  }

  function shift(direction: number): void {
    start += direction * Math.round((SPAN[zoom] * DAY) / 2);
  }

  function setZoom(next: Zoom): void {
    const middle = start + (SPAN[zoom] * DAY) / 2;
    zoom = next;
    start = startOfDay(middle - (SPAN[next] * DAY) / 2);
  }

  function describe(e: ActivityEntry): string {
    const key = e.operation.replace(".", "_");
    const known = key in OPS;
    return t(`timeline.ops.${known ? key : "other"}` as "timeline.ops.other", {
      actor: actorName(e.actor),
      target: e.target,
      operation: e.operation,
    });
  }
  const OPS: Record<string, true> = {
    item_create: true,
    item_update: true,
    item_move: true,
    comment_add: true,
    attachment_add: true,
    milestone_create: true,
    milestone_update: true,
    milestone_delete: true,
    time_log: true,
    time_update: true,
    time_delete: true,
  };

  $effect(() => {
    if (tab === "plan") void loadPlan(); // loadPlan reads from and to synchronously, so the effect tracks them
  });
  $effect(() => {
    if (tab === "activity") void loadActivity();
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(timer);
      timer = setTimeout(() => {
        if (tab === "plan") void loadPlan();
        else void loadActivity();
      }, 250);
    }),
  );
  $effect(() =>
    onResync(() => (tab === "plan" ? void loadPlan() : void loadActivity())),
  );
</script>

<section class="timeline">
  <div class="bar">
    <div class="seg" role="group" aria-label={t("timeline.title")}>
      <button
        type="button"
        aria-pressed={tab === "plan"}
        onclick={() => (tab = "plan")}>{t("timeline.gantt")}</button
      >
      <button
        type="button"
        aria-pressed={tab === "activity"}
        onclick={() => (tab = "activity")}>{t("timeline.activity")}</button
      >
    </div>
    {#if tab === "plan"}
      <div class="seg">
        {#each ["week", "month", "quarter"] as const as z (z)}
          <button
            type="button"
            aria-pressed={zoom === z}
            onclick={() => {
              setZoom(z);
            }}>{t(`timeline.${z}`)}</button
          >
        {/each}
      </div>
      <div class="seg">
        <button
          type="button"
          onclick={() => {
            shift(-1);
          }}>{t("timeline.previous")}</button
        >
        <button
          type="button"
          onclick={() =>
            (start =
              startOfDay(Date.now()) - Math.round((SPAN[zoom] * DAY) / 4))}
          >{t("timeline.today")}</button
        >
        <button
          type="button"
          onclick={() => {
            shift(1);
          }}>{t("timeline.next")}</button
        >
      </div>
      <div class="seg">
        <button
          type="button"
          aria-pressed={chart}
          onclick={() => (chart = true)}>{t("timeline.chartView")}</button
        >
        <button
          type="button"
          aria-pressed={!chart}
          onclick={() => (chart = false)}>{t("timeline.listView")}</button
        >
      </div>
      <span class="range">{date.format(from)} – {date.format(to)}</span>
    {/if}
  </div>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}

  {#if tab === "plan" && timeline}
    {#if timeline.items.length + timeline.milestones.length === 0}
      <EmptyState icon="calendar" title={t("timeline.empty")} />
    {:else if chart}
      <p class="muted">{t("timeline.chartHint")}</p>
      <div class="chart">
        <div class="plot" aria-hidden="true">
          {#each ticks as tk (tk.at)}<i class="grid" style:left="{tk.at}%"
            ></i>{/each}
          {#if Date.now() >= from && Date.now() <= to}
            <i class="today" style:left="{pct(Date.now())}%"
              ><em>{t("timeline.today")}</em></i
            >
          {/if}
        </div>
        <div class="axis" aria-hidden="true">
          {#each ticks as tk (tk.at)}<span style:left="{tk.at}%"
              >{tk.label}</span
            >{/each}
        </div>
        {#each timeline.milestones as m (m.id)}
          <div class="row">
            <span class="label milestone"><b>◆</b> {m.name}</span>
            <div class="track">
              <i
                class="diamond"
                style:left="{pct(m.due_at ?? from)}%"
                title={t("timeline.milestone", { name: m.name })}
              ></i>
            </div>
          </div>
        {/each}
        {#each timeline.items as it (it.key)}
          <div class="row">
            <a class="label" href={itemHref(it.key)}>
              <span class="key">{it.key}</span>
              {it.title}
            </a>
            <div class="track">
              <span
                class="item {it.category}"
                style:left="{pct(it.start_at)}%"
                style:width="max(6px, {pct(it.due_at) - pct(it.start_at)}%)"
                role="img"
                aria-label={t("timeline.bar", {
                  key: it.key,
                  title: it.title,
                  start: date.format(it.start_at),
                  due: date.format(it.due_at),
                  status: it.status,
                })}
              ></span>
            </div>
          </div>
        {/each}
      </div>
    {:else}
      <ul class="list">
        {#each timeline.milestones as m (m.id)}
          <li>
            <b>◆</b>
            {t("timeline.milestone", { name: m.name })}
            <span class="chip">{date.format(m.due_at ?? from)}</span>
          </li>
        {/each}
        {#each timeline.items as it (it.key)}
          <li>
            <a href={itemHref(it.key)}
              ><span class="key">{it.key}</span> {it.title}</a
            >
            <span class="chip">{it.status}</span>
            <span class="muted"
              >{t("timeline.due", { date: date.format(it.due_at) })}</span
            >
          </li>
        {/each}
      </ul>
    {/if}
    {#if timeline.truncated}<p class="banner" role="status">
        {t("timeline.truncated")}
      </p>{/if}
  {:else if tab === "activity"}
    {#if activity.length === 0}
      <EmptyState icon="overview" title={t("timeline.activityEmpty")} />
    {:else}
      <ul class="list">
        {#each activity as e (e.id)}
          <li>
            <span>{describe(e)}</span>
            <span class="muted">{dateTime.format(e.at)}</span>
          </li>
        {/each}
      </ul>
      {#if more}<button
          type="button"
          onclick={() =>
            void loadActivity(activity[activity.length - 1]?.id ?? 0)}
          >{t("timeline.more")}</button
        >{/if}
    {/if}
  {/if}
</section>

<style>
  .timeline {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    overflow-y: auto;
    align-content: start;
  }
  .bar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .seg {
    display: inline-flex;
    padding: 3px;
    gap: 2px;
    background: var(--color-soft);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  .seg button {
    min-height: 32px;
    padding: 0 14px;
    border: 0;
    border-radius: calc(var(--atlas-radius-md) - 3px);
    background: transparent;
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
  }
  .seg button:hover {
    color: var(--color-text);
  }
  .seg button[aria-pressed="true"] {
    background: var(--color-surface);
    color: var(--color-text);
    box-shadow: 0 0 0 1px var(--color-border);
    outline: 0;
  }
  .seg button:focus-visible {
    outline: 2px solid var(--atlas-primary);
    outline-offset: 1px;
  }
  .range {
    margin-left: auto;
    color: var(--color-muted);
    font-size: var(--atlas-fs-sm);
  }
  .chart {
    position: relative;
    display: flex;
    flex-direction: column;
    flex: none;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
    overflow: hidden;
  }
  .axis,
  .row {
    display: grid;
    grid-template-columns: 28% 1fr;
  }
  .axis {
    position: relative;
    display: block;
    height: 32px;
    margin-left: 28%;
    border-bottom: 1px solid var(--color-border);
  }
  .axis span {
    position: absolute;
    top: 8px;
    padding-left: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  /* Grid lines and the today marker live in one overlay aligned with the track column. */
  .plot {
    position: absolute;
    inset: 0 0 0 28%;
    pointer-events: none;
  }
  .grid {
    position: absolute;
    top: 0;
    bottom: 0;
    border-left: 1px dashed var(--color-border);
  }
  .today {
    position: absolute;
    top: 0;
    bottom: 0;
    z-index: 1;
    border-left: 2px solid var(--atlas-danger);
  }
  .today em {
    position: absolute;
    bottom: 0;
    left: 0;
    padding: 1px 8px;
    transform: translateX(-50%);
    border-radius: 6px 6px 0 0;
    background: var(--atlas-danger);
    color: #fff;
    font-size: 11px;
    font-style: normal;
    font-weight: var(--atlas-fw-semibold);
  }
  .row {
    align-items: center;
    min-height: 40px;
    border-bottom: 1px solid var(--color-border);
  }
  .row:last-child {
    border-bottom: 0;
  }
  .row:hover {
    background: var(--color-soft);
  }
  .label {
    padding: 0 var(--space-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: inherit;
    text-decoration: none;
  }
  a.label:hover {
    text-decoration: underline;
  }
  .label.milestone {
    font-weight: var(--atlas-fw-semibold);
  }
  .key {
    margin-right: 4px;
    color: var(--color-muted);
    font-weight: var(--atlas-fw-semibold);
  }
  .track {
    position: relative;
    height: 40px;
  }
  .item {
    position: absolute;
    top: 12px;
    height: 16px;
    border-radius: 8px;
    background: var(--atlas-st-todo);
    box-shadow: inset 0 0 0 1px rgb(0 0 0 / 15%);
  }
  .item.active {
    background: var(--atlas-st-active);
  }
  .item.completed {
    background: var(--atlas-st-done);
  }
  .diamond {
    position: absolute;
    top: 13px;
    width: 14px;
    height: 14px;
    margin-left: -7px;
    transform: rotate(45deg);
    border-radius: 2px;
    background: var(--atlas-st-review);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .list li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: 10px var(--space-3);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
</style>
