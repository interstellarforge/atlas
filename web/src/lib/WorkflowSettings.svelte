<script lang="ts">
  import {
    addStatus,
    ApiError,
    deleteStatus,
    setMergeColumns,
    setTimeTracking,
    type StatusInfo,
    updateStatus,
    type Workflow,
  } from "./api";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import StatusChip from "./StatusChip.svelte";
  import { statusTone } from "./statusTone";

  let { project, workflow }: { project: string; workflow: Workflow } = $props();

  let error = $state("");
  let newName = $state("");
  let newCategory = $state("todo");
  let newWip = $state("");

  const statuses = $derived(
    [...workflow.statuses].sort((a, b) => a.position - b.position),
  );
  const categories = ["todo", "active", "review", "completed"] as const;

  // The order shown while dragging or right after a drop; cleared when the server's workflow arrives (or a save fails).
  let local = $state<StatusInfo[] | undefined>();
  let dragging = $state<number | undefined>();
  let announcement = $state("");
  let table = $state<HTMLElement | undefined>();
  let localFor: Workflow | undefined;
  const list = $derived(local && localFor === workflow ? local : statuses);

  async function run(action: () => Promise<unknown>): Promise<void> {
    error = "";
    try {
      await action();
    } catch (e) {
      error = t("workflow.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  function rename(status: StatusInfo, event: Event): Promise<void> {
    const value = (event.currentTarget as HTMLInputElement).value.trim();
    if (!value || value === status.name) return Promise.resolve();
    return run(() => updateStatus(project, status.id, { name: value }));
  }

  function setWip(status: StatusInfo, event: Event): Promise<void> {
    const raw = (event.currentTarget as HTMLInputElement).value.trim();
    const limit = raw === "" ? null : Number(raw);
    if (limit === status.wip_limit) return Promise.resolve();
    return run(() => updateStatus(project, status.id, { wip_limit: limit }));
  }

  function toggleRequirement(
    status: StatusInfo,
    name: string,
    on: boolean,
  ): Promise<void> {
    const requires = on
      ? [...status.requires, name]
      : status.requires.filter((n) => n !== name);
    return run(() => updateStatus(project, status.id, { requires }));
  }

  // Positions are plain numbers: the new order takes over the existing set of positions, and only the rows that end up elsewhere are written.
  async function commit(order: StatusInfo[], moved: StatusInfo): Promise<void> {
    const changed = order.flatMap((st, i) => {
      const position = statuses[i]?.position;
      return position === undefined || st.position === position
        ? []
        : [{ id: st.id, position }];
    });
    if (changed.length === 0) {
      local = undefined;
      return;
    }
    local = order;
    localFor = workflow;
    await run(async () => {
      try {
        for (const c of changed)
          await updateStatus(project, c.id, { position: c.position });
      } catch (e) {
        local = undefined;
        throw e;
      }
    });
    announcement = t("workflow.moved", {
      name: moved.name,
      n: order.findIndex((st) => st.id === moved.id) + 1,
    });
  }

  function reorder(from: number, to: number): StatusInfo[] {
    const next = [...list];
    const [item] = next.splice(from, 1);
    if (item) next.splice(to, 0, item);
    return next;
  }

  function gripKey(event: KeyboardEvent, index: number): void {
    const to = index + (event.key === "ArrowUp" ? -1 : 1);
    if (
      (event.key !== "ArrowUp" && event.key !== "ArrowDown") ||
      to < 0 ||
      to >= list.length
    )
      return;
    event.preventDefault();
    const moved = list[index];
    if (moved) void commit(reorder(index, to), moved);
  }

  function gripDown(event: PointerEvent, index: number): void {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    event.preventDefault();
    (event.currentTarget as HTMLElement).setPointerCapture(event.pointerId);
    local = [...list];
    localFor = workflow;
    dragging = index;
  }

  function gripMove(event: PointerEvent): void {
    if (dragging === undefined || !table) return;
    const rows = [...table.querySelectorAll<HTMLElement>("[data-status]")];
    const over = rows.findIndex((row) => {
      const r = row.getBoundingClientRect();
      return event.clientY >= r.top && event.clientY < r.bottom;
    });
    if (over < 0 || over === dragging) return;
    local = reorder(dragging, over);
    dragging = over;
  }

  function gripUp(): void {
    if (dragging === undefined || !local) return;
    const moved = local[dragging];
    const order = local;
    dragging = undefined;
    if (moved) void commit(order, moved);
  }

  function add(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    return run(async () => {
      await addStatus(project, {
        name: newName,
        category: newCategory,
        wip_limit: newWip === "" ? null : Number(newWip),
      });
      newName = "";
      newWip = "";
    });
  }
</script>

<div class="settings">
  <section class="panel" aria-labelledby="wf-title">
    <div class="head">
      <div>
        <h2 id="wf-title">{t("workflow.title")}</h2>
        <p class="muted">{t("workflow.intro")}</p>
      </div>
    </div>
    {#if error}<p class="banner error" role="alert">{error}</p>{/if}
    <p class="sr-only" aria-live="polite">{announcement}</p>

    <label class="merge">
      <input
        type="checkbox"
        checked={workflow.merge_columns}
        onchange={(e) =>
          void run(() => setMergeColumns(project, e.currentTarget.checked))}
      />
      <span
        ><strong>{t("workflow.merge")}</strong>
        <small class="muted">{t("workflow.mergeHint")}</small></span
      >
    </label>

    <label class="merge">
      <input
        type="checkbox"
        checked={workflow.time_tracking}
        onchange={(e) =>
          void run(() => setTimeTracking(project, e.currentTarget.checked))}
      />
      <span
        ><strong>{t("workflow.timeTracking")}</strong>
        <small class="muted">{t("workflow.timeTrackingHint")}</small></span
      >
    </label>

    <div
      class="table"
      class:sorting={dragging !== undefined}
      role="table"
      aria-label={t("workflow.title")}
      bind:this={table}
    >
      <div class="row headrow" role="row">
        <span role="columnheader">#</span>
        <span role="columnheader">{t("workflow.status")}</span>
        <span role="columnheader">{t("workflow.category")}</span>
        <span role="columnheader">{t("workflow.wip")}</span>
        <span role="columnheader">{t("workflow.requires")}</span>
        <span role="columnheader">{t("workflow.allowCreate")}</span>
        <span role="columnheader" class="sr-only">{t("workflow.actions")}</span>
      </div>
      {#each list as status, i (status.id)}
        <div
          class="row"
          class:lifted={dragging === i}
          role="row"
          data-status={status.id}
        >
          <span class="order">
            <button
              type="button"
              class="mini grip"
              aria-label={t("workflow.grip", { name: status.name })}
              onpointerdown={(e) => {
                gripDown(e, i);
              }}
              onpointermove={gripMove}
              onpointerup={gripUp}
              onpointercancel={() => {
                dragging = undefined;
                local = undefined;
              }}
              onkeydown={(e) => {
                gripKey(e, i);
              }}><Icon name="drag" size={16} /></button
            >
          </span>
          <span class="name">
            <StatusChip
              name={t("workflow.position", { n: i + 1 })}
              tone={statusTone(status, statuses)}
            />
            <input
              aria-label={t("workflow.nameOf", { name: status.name })}
              value={status.name}
              maxlength="60"
              onchange={(e) => void rename(status, e)}
            />
          </span>
          <select
            aria-label={t("workflow.categoryOf", { name: status.name })}
            value={status.category}
            onchange={(e) =>
              void run(() =>
                updateStatus(project, status.id, {
                  category: e.currentTarget.value,
                }),
              )}
          >
            {#each categories as c (c)}
              <option value={c}
                >{t(`workflow.cat.${c}` as "workflow.cat.todo")}</option
              >
            {/each}
          </select>
          <input
            type="number"
            min="1"
            class="wip"
            placeholder={t("workflow.noLimit")}
            aria-label={t("workflow.wipOf", { name: status.name })}
            value={status.wip_limit ?? ""}
            onchange={(e) => void setWip(status, e)}
          />
          <details class="requires">
            <summary
              aria-label={t("workflow.requiresOf", { name: status.name })}
              >{status.requires.length
                ? status.requires.join(", ")
                : t("workflow.requiresNone")}</summary
            >
            <p class="muted">{t("workflow.requiresHint")}</p>
            {#each list.filter((o) => o.id !== status.id) as other (other.id)}
              <label class="check">
                <input
                  type="checkbox"
                  checked={status.requires.includes(other.name)}
                  onchange={(e) =>
                    void toggleRequirement(
                      status,
                      other.name,
                      e.currentTarget.checked,
                    )}
                />{other.name}
              </label>
            {/each}
          </details>
          <label class="toggle">
            <input
              type="checkbox"
              aria-label={t("workflow.allowCreateOf", { name: status.name })}
              checked={status.allow_create}
              onchange={(e) =>
                void run(() =>
                  updateStatus(project, status.id, {
                    allow_create: e.currentTarget.checked,
                  }),
                )}
            />
          </label>
          <button
            type="button"
            class="mini danger"
            aria-label={t("workflow.delete", { name: status.name })}
            onclick={() => void run(() => deleteStatus(project, status.id))}
            ><Icon name="trash" size={16} /></button
          >
        </div>
      {/each}
    </div>

    <form class="add" onsubmit={(e) => void add(e)}>
      <input
        placeholder={t("workflow.newName")}
        aria-label={t("workflow.newName")}
        bind:value={newName}
        required
        maxlength="60"
      />
      <select aria-label={t("workflow.category")} bind:value={newCategory}>
        {#each categories as c (c)}
          <option value={c}
            >{t(`workflow.cat.${c}` as "workflow.cat.todo")}</option
          >
        {/each}
      </select>
      <input
        type="number"
        min="1"
        class="wip"
        placeholder={t("workflow.noLimit")}
        aria-label={t("workflow.wip")}
        bind:value={newWip}
      />
      <button class="primary" type="submit"
        ><Icon name="plus" size={16} /> {t("workflow.add")}</button
      >
    </form>
  </section>

  <section class="panel" aria-labelledby="wf-preview">
    <h2 id="wf-preview">{t("workflow.preview")}</h2>
    <p class="muted">{t("workflow.previewText")}</p>
    <ol class="flow">
      {#each statuses as status (status.id)}
        <li>
          <StatusChip name={status.name} tone={statusTone(status, statuses)} />
          {#if status.wip_limit}<small class="muted"
              >{t("workflow.limit", { count: status.wip_limit })}</small
            >{/if}
        </li>
      {/each}
    </ol>
  </section>
</div>

<style>
  .settings {
    display: grid;
    gap: var(--space-4);
    overflow-y: auto;
    max-height: 100%;
  }
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  .panel p {
    margin: 4px 0 0;
  }
  .table {
    display: grid;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    overflow: hidden;
  }
  .row {
    display: grid;
    grid-template-columns: 44px minmax(0, 1.6fr) 150px 130px 170px 70px 44px;
    align-items: center;
    gap: var(--space-3);
    min-height: 56px;
    padding: 6px 12px;
    border-top: 1px solid var(--color-border);
  }
  .headrow {
    min-height: 40px;
    border-top: none;
    background: var(--color-soft);
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-muted);
  }
  .order {
    display: flex;
    gap: 2px;
  }
  .grip {
    cursor: grab;
    touch-action: none;
  }
  .row {
    transition:
      background var(--atlas-duration) var(--atlas-ease),
      opacity var(--atlas-duration) var(--atlas-ease);
  }
  .table.sorting,
  .table.sorting * {
    cursor: grabbing;
    user-select: none;
  }
  .table.sorting .row:not(.lifted, .headrow) {
    opacity: 0.55;
  }
  /* Inset ring: the table clips overflow, so an outer shadow would not show. */
  .row.lifted {
    background: color-mix(
      in srgb,
      var(--color-accent) 16%,
      var(--color-surface)
    );
    box-shadow: inset 0 0 0 2px var(--color-accent);
  }
  .row.lifted .grip {
    color: var(--color-accent);
    border-color: var(--color-accent);
  }
  .name {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    min-width: 0;
  }
  .name input {
    flex: 1;
  }
  .mini {
    display: inline-grid;
    place-items: center;
    width: 32px;
    min-height: 32px;
    height: 32px;
    padding: 0;
  }
  .mini.danger {
    color: var(--color-danger);
  }
  .wip {
    width: 100%;
  }
  .toggle {
    display: flex;
    justify-content: center;
  }
  .merge {
    display: flex;
    align-items: flex-start;
    gap: var(--space-3);
  }
  .merge span {
    display: grid;
    gap: 2px;
  }
  .requires summary {
    cursor: pointer;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .requires p {
    margin: 4px 0;
    font-size: var(--atlas-fs-xs);
  }
  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .add {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 150px 130px auto;
    gap: var(--space-3);
  }
  .add button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .flow {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
  }
  .flow li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .flow li:not(:last-child)::after {
    content: "→";
    color: var(--color-muted);
  }
  @media (max-width: 800px) {
    .row {
      grid-template-columns: 44px minmax(0, 1fr);
    }
    .headrow {
      display: none;
    }
    .row > * {
      grid-column: 2;
    }
    .row > .order {
      grid-column: 1;
      grid-row: 1;
    }
    .add {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
