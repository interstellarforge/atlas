<script lang="ts">
  import {
    getTimeSummary,
    type TimeBucket,
    timeExportUrl,
    type TimeQuery,
    type TimeSummary,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import { people } from "./people.svelte";
  import { itemHref } from "./routes";
  import { formatMinutes } from "./time";

  let { project, enabled }: { project: string; enabled: boolean } = $props();

  let query = $state<TimeQuery>({ from: "", to: "", user: "", period: "week" });
  let summary = $state<TimeSummary | undefined>();
  let error = $state("");
  let timer: ReturnType<typeof setTimeout> | undefined;

  async function load(): Promise<void> {
    try {
      summary = await getTimeSummary(project, query);
      error = "";
    } catch {
      error = t("progress.failed");
    }
  }

  $effect(() => {
    void load(); // reads the project and every filter before its first await, so the effect tracks them
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(timer);
      timer = setTimeout(() => void load(), 250);
    }),
  );
  $effect(() => onResync(() => void load()));

  const max = (list: TimeBucket[]): number =>
    Math.max(1, ...list.map((b) => b.minutes));
</script>

<section class="time">
  {#if !enabled}<p class="banner" role="status">
      {t("time.off")}
      {t("time.turnOn")}
    </p>{/if}
  <form
    class="filters"
    onsubmit={(e) => {
      e.preventDefault();
    }}
  >
    <label>{t("time.from")}<input type="date" bind:value={query.from} /></label>
    <label>{t("time.to")}<input type="date" bind:value={query.to} /></label>
    <label
      >{t("time.person")}
      <select bind:value={query.user}>
        <option value="">{t("time.anyone")}</option>
        {#each people.users as u (u.id)}<option value={String(u.id)}
            >{u.name}</option
          >{/each}
      </select>
    </label>
    <label
      >{t("time.period")}
      <select bind:value={query.period}>
        <option value="day">{t("time.day")}</option>
        <option value="week">{t("time.week")}</option>
        <option value="month">{t("time.month")}</option>
      </select>
    </label>
    <button
      type="button"
      onclick={() => {
        location.href = timeExportUrl(project, query);
      }}>{t("time.export")}</button
    >
  </form>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if summary}
    {#if summary.entries === 0}
      <EmptyState icon="clock" title={t("time.empty")} />
    {:else}
      <p class="total">
        <strong
          >{t("time.total", {
            time: formatMinutes(summary.total_minutes),
          })}</strong
        >
        <span class="muted"
          >{t("time.entries", { count: summary.entries })}</span
        >
      </p>
      <div class="grid">
        {#each [{ title: t("time.perItem"), list: summary.items, link: true }, { title: t("time.perPerson"), list: summary.people, link: false }, { title: t("time.perStatus"), list: summary.statuses, link: false }, { title: t("time.perPeriod"), list: summary.periods, link: false }] as block (block.title)}
          <div class="card">
            <h2>{block.title}</h2>
            <ul>
              {#each block.list as b (b.key)}
                <li>
                  <span class="name">
                    {#if block.link}<a href={itemHref(b.key)}>{b.key}</a>
                      {b.label}
                    {:else}{[b.label, b.key].find(Boolean) ??
                        t("time.noStatus")}{/if}
                  </span>
                  <strong>{formatMinutes(b.minutes)}</strong>
                  <progress value={b.minutes} max={max(block.list)}></progress>
                </li>
              {/each}
            </ul>
          </div>
        {/each}
      </div>
    {/if}
  {/if}
</section>

<style>
  .time {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    overflow-y: auto;
    align-content: start;
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-3);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .total {
    display: flex;
    gap: var(--space-3);
    margin: 0;
  }
  .grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(320px, 1fr));
    gap: 14px;
  }
  .card {
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2 {
    margin: 0 0 var(--space-2);
    font-size: var(--atlas-fs-md);
  }
  ul {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  li {
    display: grid;
    grid-template-columns: 1fr auto;
    gap: 2px var(--space-2);
  }
  li progress {
    grid-column: 1 / -1;
    width: 100%;
    accent-color: var(--color-accent);
  }
  .name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
</style>
