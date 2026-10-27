<script lang="ts">
  import { onMount } from "svelte";

  import {
    deleteTime,
    getItemTime,
    logTime,
    me,
    type TimeEntry,
    updateTime,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import { formatMinutes } from "./time";

  let { itemKey, enabled }: { itemKey: string; enabled: boolean } = $props();

  const project = $derived(itemKey.slice(0, itemKey.lastIndexOf("-")));
  let entries = $state<TimeEntry[]>([]);
  let userId = $state(0);
  let isAdmin = $state(false);
  let error = $state("");
  let duration = $state("");
  let date = $state("");
  let note = $state("");
  let editing = $state<number | undefined>();
  let edit = $state({ duration: "", date: "", note: "" });

  const total = $derived(entries.reduce((n, e) => n + e.minutes, 0));
  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  async function load(): Promise<void> {
    try {
      entries = await getItemTime(itemKey);
    } catch {
      /* the panel reports a failed load; the next event retries */
    }
  }

  async function run(action: () => Promise<unknown>): Promise<boolean> {
    error = "";
    try {
      await action();
      await load();
      return true;
    } catch (e) {
      error = t("time.failed", {
        message: e instanceof Error ? e.message : "",
      });
      return false;
    }
  }

  async function add(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (await run(() => logTime(itemKey, duration.trim(), date, note.trim()))) {
      duration = note = "";
    }
  }

  function startEdit(e: TimeEntry): void {
    editing = e.id;
    edit = { duration: String(e.minutes), date: e.spent_on, note: e.note };
  }

  async function save(event: SubmitEvent, id: number): Promise<void> {
    event.preventDefault();
    if (
      await run(() =>
        updateTime(project, id, {
          duration: edit.duration.trim(),
          date: edit.date,
          note: edit.note.trim(),
        }),
      )
    )
      editing = undefined;
  }

  const mine = (e: TimeEntry): boolean => e.user_id === userId || isAdmin;

  onMount(() => {
    void load();
    void me().then((u) => {
      userId = u?.id ?? 0;
      isAdmin = u?.is_admin ?? false;
    });
  });
  $effect(() =>
    onChange((event) => {
      if (event.key === itemKey) void load();
    }),
  );
  $effect(() => onResync(() => void load()));
</script>

<section>
  <h3>{t("time.itemTitle")}</h3>
  <p class="muted">{t("time.total", { time: formatMinutes(total) })}</p>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if entries.length === 0}
    <p class="muted">{t("time.empty")}</p>
  {:else}
    <ul>
      {#each entries as e (e.id)}
        <li>
          {#if editing === e.id}
            <form class="row" onsubmit={(ev) => void save(ev, e.id)}>
              <label
                >{t("time.duration")}<input
                  bind:value={edit.duration}
                  required
                /></label
              >
              <label
                >{t("time.date")}<input
                  type="date"
                  bind:value={edit.date}
                  required
                /></label
              >
              <label
                >{t("time.note")}<input
                  bind:value={edit.note}
                  maxlength="500"
                /></label
              >
              <button type="submit" class="primary">{t("time.save")}</button>
              <button type="button" onclick={() => (editing = undefined)}
                >{t("time.cancel")}</button
              >
            </form>
          {:else}
            <strong>{formatMinutes(e.minutes)}</strong>
            <span>{e.user}</span>
            <span class="muted"
              >{dateFormat.format(new Date(`${e.spent_on}T00:00`))}</span
            >
            {#if e.status}<span class="chip"
                >{t("time.statusAt", { status: e.status })}</span
              >{/if}
            {#if e.note}<span class="note">{e.note}</span>{/if}
            {#if mine(e)}
              <button
                type="button"
                onclick={() => {
                  startEdit(e);
                }}>{t("time.edit")}</button
              >
              <button
                type="button"
                onclick={() => void run(() => deleteTime(project, e.id))}
                >{t("time.delete")}</button
              >
            {/if}
          {/if}
        </li>
      {/each}
    </ul>
  {/if}
  {#if enabled}
    <form class="row" onsubmit={(ev) => void add(ev)}>
      <label
        >{t("time.duration")}<input
          bind:value={duration}
          placeholder={t("time.durationHint")}
          required
        /></label
      >
      <label>{t("time.date")}<input type="date" bind:value={date} /></label>
      <label>{t("time.note")}<input bind:value={note} maxlength="500" /></label>
      <button type="submit" class="primary">{t("time.log")}</button>
    </form>
  {:else}
    <p class="muted">{t("time.off")}</p>
  {/if}
</section>

<style>
  section {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  h3 {
    margin: 0;
    font-size: var(--atlas-fs-md);
    font-weight: var(--atlas-fw-bold);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  li,
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .row {
    align-items: end;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .note {
    flex-basis: 100%;
  }
</style>
