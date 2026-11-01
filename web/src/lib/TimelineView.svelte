<script lang="ts">
  import { SvelteSet } from "svelte/reactivity";

  import {
    type ActivityEntry,
    type Card,
    getActivity,
    getItems,
    getTimeline,
    type Timeline,
    type Workflow,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { actorName } from "./people.svelte";
  import { itemHref, viewPath } from "./routes";
  import TicketRows from "./TicketRows.svelte";

  let {
    project,
    workflow,
  }: { project: string; workflow?: Workflow | undefined } = $props();

  type Zoom = "week" | "month" | "quarter";
  const DAY = 86_400_000;
  // Days shown per zoom level: four weeks, three months, one year.
  const SPAN: Record<Zoom, number> = { week: 28, month: 90, quarter: 365 };
  // Pixels per day: the chart is as wide as the period needs and scrolls sideways when the screen is narrower.
  const PPD: Record<Zoom, number> = { week: 40, month: 14, quarter: 6 };
  const CHILD_FIELDS = "key,type,title,status,due_at";
  const CHILD_LIMIT = 100;

  interface Children {
    items: Card[];
    more: boolean;
    failed: boolean;
    loaded: boolean;
  }

  let tab = $state<"plan" | "activity">("plan");
  let zoom = $state<Zoom>("month");
  let chart = $state(true);
  let start = $state(startOfDay(Date.now()) - 22 * DAY);
  let timeline = $state<Timeline | undefined>();
  let activity = $state<ActivityEntry[]>([]);
  let more = $state(false);
  let error = $state("");
  let timer: ReturnType<typeof setTimeout> | undefined;
  let planRequest = 0;
  // Rows whose tickets are unfolded, keyed "i:<item key>" or "m:<milestone id>"; the tickets load on demand.
  const open = new SvelteSet<string>();
  let children = $state<Record<string, Children | undefined>>({});

  const from = $derived(start);
  const to = $derived(start + SPAN[zoom] * DAY);
  const trackWidth = $derived(SPAN[zoom] * PPD[zoom]);
  // The server already returns only what is due in the range; filtering again here keeps a slow answer for an
  // earlier range from drawing rows that are outside the one on screen.
  const items = $derived(
    (timeline?.items ?? []).filter(
      (it) => it.due_at >= from && it.due_at <= to,
    ),
  );
  const milestones = $derived(
    (timeline?.milestones ?? []).filter(
      (m) => m.due_at !== null && m.due_at >= from && m.due_at <= to,
    ),
  );
  const unscheduled = $derived(timeline?.unscheduled_milestones ?? []);
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
    const request = ++planRequest;
    try {
      const result = await getTimeline(project, from, to);
      if (request !== planRequest) return; // a newer range was requested meanwhile
      timeline = result;
      error = "";
      void loadOpenChildren();
    } catch {
      if (request === planRequest) error = t("timeline.failed");
    }
  }

  function expandable(type: string): boolean {
    return type === "epic" || type === "story";
  }

  async function loadChildren(id: string): Promise<void> {
    const [kind, ref] = [id.slice(0, 1), id.slice(2)];
    const milestone =
      kind === "m"
        ? timeline?.milestones.find((m) => String(m.id) === ref)
        : undefined;
    if (kind === "m" && !milestone) return;
    try {
      const page = await getItems({
        project,
        fields: CHILD_FIELDS,
        limit: CHILD_LIMIT + 1,
        ...(milestone ? { milestone: milestone.name } : { under: ref }),
      });
      // `under` returns the item itself too.
      const rows = page.items.filter((c) => c.key !== ref);
      children[id] = {
        items: rows.slice(0, CHILD_LIMIT),
        more: rows.length > CHILD_LIMIT || page.next_cursor !== null,
        failed: false,
        loaded: true,
      };
    } catch {
      children[id] = {
        items: children[id]?.items ?? [],
        more: false,
        failed: true,
        loaded: true,
      };
    }
  }

  // A refresh reloads the unfolded rows but keeps their old tickets on screen until the new ones arrive.
  async function loadOpenChildren(): Promise<void> {
    await Promise.all([...open].map((id) => loadChildren(id)));
  }

  function toggle(id: string): void {
    if (open.has(id)) {
      open.delete(id);
      return;
    }
    open.add(id);
    void loadChildren(id);
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
    {#if items.length + milestones.length === 0}
      <EmptyState icon="calendar" title={t("timeline.empty")} />
    {:else if chart}
      <p class="muted">{t("timeline.chartHint")}</p>
      <!-- svelte-ignore a11y_no_noninteractive_tabindex -->
      <div
        class="scroller"
        role="region"
        aria-label={t("timeline.scrollRegion")}
        tabindex="0"
      >
        <div class="chart" style:--track-w="{trackWidth}px">
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
          {#each milestones as m (m.id)}
            {@const id = `m:${String(m.id)}`}
            <div class="row">
              <span class="label milestone">
                <button
                  type="button"
                  class="fold"
                  aria-expanded={open.has(id)}
                  aria-controls={`tl-${String(m.id)}-m`}
                  aria-label={t(
                    open.has(id) ? "timeline.collapse" : "timeline.expand",
                    { name: m.name },
                  )}
                  onclick={() => {
                    toggle(id);
                  }}
                  ><Icon
                    name={open.has(id) ? "chevron-down" : "chevron-right"}
                    size={14}
                  /></button
                >
                <b>◆</b>
                <a href={`#${viewPath(project, "milestones", m.id)}`}
                  >{m.name}</a
                >
              </span>
              <div class="track">
                <i
                  class="diamond"
                  style:left="{pct(m.due_at ?? from)}%"
                  title={t("timeline.milestone", { name: m.name })}
                ></i>
              </div>
            </div>
            {#if open.has(id)}
              {@render tickets(id, `tl-${String(m.id)}-m`)}
            {/if}
          {/each}
          {#each items as it (it.key)}
            {@const id = `i:${it.key}`}
            <div class="row">
              <span class="label">
                {#if expandable(it.type)}
                  <button
                    type="button"
                    class="fold"
                    aria-expanded={open.has(id)}
                    aria-controls={`tl-${it.key}-i`}
                    aria-label={t(
                      open.has(id) ? "timeline.collapse" : "timeline.expand",
                      { name: it.key },
                    )}
                    onclick={() => {
                      toggle(id);
                    }}
                    ><Icon
                      name={open.has(id) ? "chevron-down" : "chevron-right"}
                      size={14}
                    /></button
                  >
                {/if}
                <a href={itemHref(it.key)}>
                  <span class="key">{it.key}</span>
                  {it.title}
                </a>
              </span>
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
            {#if open.has(id)}
              {@render tickets(id, `tl-${it.key}-i`)}
            {/if}
          {/each}
        </div>
      </div>
    {:else}
      <ul class="list">
        {#each milestones as m (m.id)}
          <li>
            <b>◆</b>
            <a href={`#${viewPath(project, "milestones", m.id)}`}
              >{t("timeline.milestone", { name: m.name })}</a
            >
            <span class="chip">{date.format(m.due_at ?? from)}</span>
          </li>
        {/each}
        {#each items as it (it.key)}
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
    {#if unscheduled.length > 0}
      <h2 class="sub">{t("timeline.unscheduledMilestones")}</h2>
      <ul class="list">
        {#each unscheduled as m (m.id)}
          <li>
            <b>◆</b>
            <a href={`#${viewPath(project, "milestones", m.id)}`}>{m.name}</a>
            <span class="chip">{t("milestones.unscheduled")}</span>
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

{#snippet tickets(id: string, domId: string)}
  {@const c = children[id]}
  <div class="children" id={domId}>
    {#if !c?.loaded}
      <p class="muted" role="status">…</p>
    {:else if c.failed}
      <p class="banner error" role="alert">{t("timeline.ticketsFailed")}</p>
    {:else if c.items.length === 0}
      <p class="muted">{t("timeline.noTickets")}</p>
    {:else}
      <TicketRows items={c.items} {workflow} indent />
      {#if c.more}<p class="muted" role="status">{t("tickets.more")}</p>{/if}
    {/if}
  </div>
{/snippet}

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
  /* The scroller is the only horizontal scroll area; the label column sticks while the period scrolls. */
  .scroller {
    flex: none;
    overflow-x: auto;
    container-type: inline-size;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  .scroller:focus-visible {
    outline: 2px solid var(--atlas-primary);
    outline-offset: 2px;
  }
  .chart {
    --label-w: 220px;
    position: relative;
    display: flex;
    flex-direction: column;
    width: calc(var(--label-w) + var(--track-w));
    min-width: 100%;
  }
  .axis,
  .row {
    display: grid;
    grid-template-columns: var(--label-w) 1fr;
  }
  .axis {
    position: relative;
    display: block;
    height: 32px;
    margin-left: var(--label-w);
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
    inset: 0 0 0 var(--label-w);
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
    position: sticky;
    left: 0;
    z-index: 2;
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    height: 100%;
    padding: 0 var(--space-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    background: var(--color-surface);
    color: inherit;
  }
  .label a {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    color: inherit;
    text-decoration: none;
  }
  .label a:hover {
    text-decoration: underline;
  }
  .label a:focus-visible,
  .fold:focus-visible {
    outline: 2px solid var(--atlas-primary);
    outline-offset: 1px;
  }
  .fold {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: 24px;
    height: 24px;
    padding: 0;
    border: 0;
    border-radius: var(--atlas-radius-sm);
    background: transparent;
    color: var(--color-muted);
  }
  .children {
    position: sticky;
    left: 0;
    width: 100cqw;
    box-sizing: border-box;
    padding: var(--space-2) var(--space-3);
    border-bottom: 1px solid var(--color-border);
    background: var(--color-soft);
  }
  .sub {
    margin: var(--space-2) 0 0;
    font-size: var(--atlas-fs-md);
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
