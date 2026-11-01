<script lang="ts">
  import {
    type Card,
    createMilestone,
    deleteMilestone,
    getItems,
    listMilestones,
    type Milestone,
    updateMilestone,
    type Workflow,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { viewPath } from "./routes";
  import TicketRows from "./TicketRows.svelte";

  const TICKETS = "key,type,title,status,due_at";
  const TICKET_LIMIT = 100;

  let {
    project,
    workflow,
    selected,
  }: {
    project: string;
    workflow?: Workflow | undefined;
    selected?: number | undefined;
  } = $props();

  let items = $state<Milestone[]>([]);
  let ready = $state(false);
  let error = $state("");
  let name = $state("");
  let due = $state("");
  let onlyUnscheduled = $state(false);
  let tickets = $state<Card[]>([]);
  let ticketsMore = $state(false);
  let ticketsReady = $state(false);
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;

  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  function fromDateInput(value: string): number | null {
    if (!value) return null;
    const [y, m, d] = value.split("-").map(Number);
    return new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).getTime();
  }

  function toDateInput(ms: number): string {
    const d = new Date(ms);
    const pad = (n: number): string => String(n).padStart(2, "0");
    return `${String(d.getFullYear())}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  }

  const current = $derived(items.find((m) => m.id === selected));
  const currentName = $derived(current?.name);
  const unscheduledCount = $derived(
    items.filter((m) => m.due_at === null && m.released_at === null).length,
  );
  const shown = $derived(
    onlyUnscheduled
      ? items.filter((m) => m.due_at === null && m.released_at === null)
      : items,
  );

  async function load(): Promise<void> {
    try {
      items = await listMilestones(project);
      ready = true;
    } catch {
      error = t("project.loadFailed");
    }
  }

  // Tickets of the opened milestone; a reload keeps the old rows on screen until the new ones arrive.
  async function loadTickets(name: string): Promise<void> {
    try {
      const page = await getItems({
        project,
        milestone: name,
        fields: TICKETS,
        limit: TICKET_LIMIT,
      });
      tickets = page.items;
      ticketsMore = page.next_cursor !== null;
      ticketsReady = true;
    } catch {
      error = t("milestones.loadFailed");
    }
  }

  function saveDue(m: Milestone, value: string): void {
    void run(() =>
      updateMilestone(project, m.id, { due_at: fromDateInput(value) }),
    );
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
  $effect(() => {
    // Depends on the milestone's name only, so progress refreshes do not refetch the tickets twice.
    const name = currentName;
    ticketsReady = false;
    tickets = [];
    if (name) void loadTickets(name);
  });
  // Counts change whenever items move or are assigned, so any change in the project refreshes them.
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => {
        void load();
        if (current) void loadTickets(current.name);
      }, 250);
    }),
  );
  $effect(() =>
    onResync(() => {
      void load();
      if (current) void loadTickets(current.name);
    }),
  );
</script>

<section class="milestones">
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if selected !== undefined}
    <a class="back" href={`#${viewPath(project, "milestones")}`}
      ><Icon name="chevrons-left" size={16} />{t("milestones.back")}</a
    >
    {#if ready && !current}
      <EmptyState icon="milestone" title={t("milestones.notFound")} />
    {:else if current}
      <div class="detail">
        <div class="head">
          <Icon name="milestone" size={18} />
          <h2>{current.name}</h2>
          {#if current.released_at !== null}<span class="chip type"
              >{t("milestones.released")}</span
            >{/if}
          {#if current.due_at !== null}<span class="chip"
              >{dateFormat.format(current.due_at)}</span
            >{:else}<span class="chip">{t("milestones.unscheduled")}</span>{/if}
        </div>
        <label class="progress">
          <span class="muted"
            >{t("milestones.progress", {
              done: current.done,
              total: current.total,
            })}</span
          >
          <progress value={current.done} max={Math.max(current.total, 1)}
          ></progress>
        </label>
        <h3>{t("milestones.items")}</h3>
        {#if ticketsReady && tickets.length === 0}
          <EmptyState icon="milestone" title={t("milestones.noItems")} />
        {:else}
          <TicketRows items={tickets} {workflow} />
          {#if ticketsMore}<p class="muted" role="status">
              {t("tickets.more")}
            </p>{/if}
        {/if}
      </div>
    {/if}
  {:else}
    {#if ready && items.length === 0}
      <EmptyState icon="milestone" title={t("milestones.empty")} />
    {/if}
    {#if unscheduledCount > 0}
      <label class="check">
        <input type="checkbox" bind:checked={onlyUnscheduled} />
        {t("milestones.onlyUnscheduled")}
      </label>
    {/if}
    <ul>
      {#each shown as m (m.id)}
        <li>
          <div class="head">
            <Icon name="milestone" size={18} />
            <h2>
              <a
                href={`#${viewPath(project, "milestones", m.id)}`}
                aria-label={t("milestones.open", { name: m.name })}>{m.name}</a
              >
            </h2>
            {#if m.released_at !== null}<span class="chip type"
                >{t("milestones.released")}</span
              >{/if}
            {#if m.due_at !== null}<span class="chip"
                >{dateFormat.format(m.due_at)}</span
              >{:else if m.released_at === null}<span class="chip"
                >{t("milestones.unscheduled")}</span
              >{/if}
          </div>
          <label class="progress">
            <span class="muted"
              >{t("milestones.progress", {
                done: m.done,
                total: m.total,
              })}</span
            >
            <progress value={m.done} max={Math.max(m.total, 1)}></progress>
          </label>
          <div class="due">
            <label>
              {t("milestones.dueEdit", { name: m.name })}
              <input
                type="date"
                value={m.due_at === null ? "" : toDateInput(m.due_at)}
                onchange={(e) => {
                  saveDue(m, e.currentTarget.value);
                }}
              />
            </label>
          </div>
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
  {/if}
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
    border-radius: var(--atlas-radius-lg);
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
  h2 a {
    color: inherit;
    text-decoration: none;
  }
  h2 a:hover {
    text-decoration: underline;
  }
  h2 a:focus-visible,
  .back:focus-visible {
    outline: 2px solid var(--atlas-primary);
    outline-offset: 2px;
  }
  .back {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    width: fit-content;
    color: var(--color-muted);
    text-decoration: none;
  }
  .detail {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-width: 900px;
  }
  h3 {
    margin: var(--space-2) 0 0;
    font-size: var(--atlas-fs-md);
  }
  .check {
    flex-direction: row;
    align-items: center;
    gap: var(--space-2);
  }
</style>
