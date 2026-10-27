<script lang="ts">
  import {
    createMilestone,
    deleteMilestone,
    listMilestones,
    type Milestone,
    updateMilestone,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";

  let { project }: { project: string } = $props();

  let items = $state<Milestone[]>([]);
  let ready = $state(false);
  let error = $state("");
  let name = $state("");
  let due = $state("");
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;

  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  function fromDateInput(value: string): number | null {
    if (!value) return null;
    const [y, m, d] = value.split("-").map(Number);
    return new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).getTime();
  }

  async function load(): Promise<void> {
    try {
      items = await listMilestones(project);
      ready = true;
    } catch {
      error = t("project.loadFailed");
    }
  }

  async function run(action: () => Promise<unknown>): Promise<void> {
    error = "";
    try {
      await action();
      await load();
    } catch (e) {
      error = t("milestones.failed", {
        message: e instanceof Error ? e.message : "",
      });
    }
  }

  function add(event: Event): void {
    event.preventDefault();
    const trimmed = name.trim();
    if (!trimmed) return;
    void run(async () => {
      await createMilestone(project, trimmed, fromDateInput(due));
      name = "";
      due = "";
    });
  }

  $effect(() => {
    if (project) void load();
  });
  // Counts change whenever items move or are assigned, so any change in the project refreshes them.
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => void load(), 250);
    }),
  );
  $effect(() => onResync(() => void load()));
</script>

<section class="milestones">
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if ready && items.length === 0}<p class="muted">
      {t("milestones.empty")}
    </p>{/if}
  <ul>
    {#each items as m (m.id)}
      <li>
        <div class="head">
          <h2>{m.name}</h2>
          {#if m.released_at !== null}<span class="chip type"
              >{t("milestones.released")}</span
            >{/if}
          {#if m.due_at !== null}<span class="chip"
              >{dateFormat.format(m.due_at)}</span
            >{/if}
        </div>
        <label class="progress">
          <span class="muted"
            >{t("milestones.progress", { done: m.done, total: m.total })}</span
          >
          <progress value={m.done} max={Math.max(m.total, 1)}></progress>
        </label>
        <div class="actions">
          <button
            type="button"
            onclick={() =>
              void run(() =>
                updateMilestone(project, m.id, {
                  released: m.released_at === null,
                }),
              )}
            >{m.released_at === null
              ? t("milestones.release")
              : t("milestones.reopen")}</button
          >
          <button
            type="button"
            title={m.total > 0 ? t("milestones.deleteBlocked") : undefined}
            onclick={() => void run(() => deleteMilestone(project, m.id))}
            >{t("milestones.delete")}</button
          >
        </div>
      </li>
    {/each}
  </ul>
  <form class="add" onsubmit={add}>
    <label>
      {t("milestones.name")}
      <input bind:value={name} maxlength="100" required />
    </label>
    <label>
      {t("milestones.due")}
      <input type="date" bind:value={due} />
    </label>
    <button class="primary" type="submit">{t("milestones.add")}</button>
  </form>
</section>

<style>
  .milestones {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    height: 100%;
    overflow-y: auto;
    align-content: start;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(260px, 1fr));
    gap: 14px;
  }
  li {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
  }
  .head,
  .actions,
  .add {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .add {
    align-items: end;
    max-width: 700px;
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-lg);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  progress {
    width: 100%;
    accent-color: var(--color-accent);
  }
  .banner {
    flex: none;
  }
</style>
