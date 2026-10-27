<script lang="ts">
  import {
    addStatus,
    ApiError,
    deleteStatus,
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
  const categories = ["todo", "active", "completed"] as const;

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

  // Positions are plain numbers, so moving a status swaps its position with its neighbour's.
  function move(index: number, by: -1 | 1): Promise<void> {
    const a = statuses[index];
    const b = statuses[index + by];
    if (!a || !b) return Promise.resolve();
    return run(async () => {
      await updateStatus(project, a.id, { position: b.position });
      await updateStatus(project, b.id, { position: a.position });
    });
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

    <div class="table" role="table" aria-label={t("workflow.title")}>
      <div class="row headrow" role="row">
        <span role="columnheader">#</span>
        <span role="columnheader">{t("workflow.status")}</span>
        <span role="columnheader">{t("workflow.category")}</span>
        <span role="columnheader">{t("workflow.wip")}</span>
        <span role="columnheader">{t("workflow.allowCreate")}</span>
        <span role="columnheader" class="sr-only">{t("workflow.actions")}</span>
      </div>
      {#each statuses as status, i (status.id)}
        <div class="row" role="row">
          <span class="order">
            <button
              type="button"
              class="mini"
              aria-label={t("workflow.moveUp", { name: status.name })}
              disabled={i === 0}
              onclick={() => void move(i, -1)}
              ><Icon name="arrow-up" size={14} /></button
            >
            <button
              type="button"
              class="mini"
              aria-label={t("workflow.moveDown", { name: status.name })}
              disabled={i === statuses.length - 1}
              onclick={() => void move(i, 1)}
              ><Icon name="arrow-down" size={14} /></button
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
    grid-template-columns: 76px minmax(0, 1.6fr) 150px 130px 70px 44px;
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
      grid-template-columns: minmax(0, 1fr) 44px;
    }
    .headrow {
      display: none;
    }
    .order,
    .row select,
    .row .wip {
      grid-column: 1;
    }
    .add {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
