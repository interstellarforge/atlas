<script lang="ts">
  import {
    type Card,
    emptyFilters,
    filterQuery,
    type Filters,
    getItems,
    type Workflow,
  } from "./api";
  import Avatar from "./Avatar.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import FiltersBar from "./Filters.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { personName } from "./people.svelte";
  import PriorityIndicator from "./PriorityIndicator.svelte";
  import { itemHref } from "./routes";
  import StatusChip from "./StatusChip.svelte";
  import { statusTone } from "./statusTone";
  import TypeIcon from "./TypeIcon.svelte";
  import VirtualList from "./VirtualList.svelte";

  const ROW = 48; // --atlas-list-row-h
  const FIELDS =
    "key,type,title,status,priority,assignee,due_at,blocked,updated_at,archived_at";
  type SortKey = "key" | "priority" | "due" | "updated";

  // With `fixedType` the list shows only items of that type (the issues page lists bugs).
  let {
    project,
    workflow,
    fixedType = "",
  }: { project: string; workflow?: Workflow; fixedType?: string } = $props();

  let items = $state<Card[]>([]);
  let nextCursor = $state<string | null>(null);
  let filters = $state<Filters>({ ...emptyFilters });
  let sort = $state<SortKey>("key");
  let ready = $state(false);
  let loading = false;
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;

  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });
  const columns: {
    key: SortKey | undefined;
    label:
      | "list.key"
      | "list.title"
      | "list.type"
      | "list.status"
      | "list.priority"
      | "list.assignee"
      | "list.due"
      | "list.updated";
  }[] = [
    { key: "key", label: "list.key" },
    { key: undefined, label: "list.title" },
    { key: undefined, label: "list.type" },
    { key: undefined, label: "list.status" },
    { key: "priority", label: "list.priority" },
    { key: undefined, label: "list.assignee" },
    { key: "due", label: "list.due" },
    { key: "updated", label: "list.updated" },
  ];

  async function load(reset: boolean): Promise<void> {
    if (loading) return;
    loading = true;
    try {
      const page = await getItems({
        ...filterQuery(filters),
        ...(fixedType && { type: fixedType }),
        project,
        sort,
        fields: FIELDS,
        // A reload keeps as many rows as are already on screen.
        limit: reset ? Math.min(200, Math.max(100, items.length)) : 100,
        ...(!reset && nextCursor && { cursor: nextCursor }),
      });
      items = reset ? page.items : [...items, ...page.items];
      nextCursor = page.next_cursor;
      ready = true;
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    const trigger = JSON.stringify([project, sort, filters]);
    const timer = setTimeout(
      () => {
        void load(true);
      },
      ready && trigger ? 250 : 0,
    );
    return () => {
      clearTimeout(timer);
    };
  });

  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => {
        void load(true);
      }, 200);
    }),
  );
  $effect(() =>
    onResync(() => {
      void load(true);
    }),
  );

  function tone(name: string | undefined): ReturnType<typeof statusTone> {
    const all = workflow?.statuses ?? [];
    const st = all.find((x) => x.name === name);
    return st ? statusTone(st, all) : "todo";
  }

  function isDone(name: string | undefined): boolean {
    return (
      workflow?.statuses.find((x) => x.name === name)?.category === "completed"
    );
  }

  function more(): void {
    if (nextCursor) void load(false);
  }
</script>

<section class="list">
  <FiltersBar
    bind:filters
    labels={workflow?.labels ?? []}
    milestones={workflow?.milestones ?? []}
    types={fixedType ? [fixedType] : (workflow?.types ?? [])}
    statuses={fixedType ? (workflow?.statuses.map((st) => st.name) ?? []) : []}
  />

  <div class="table" role="table" aria-label={t("project.list")}>
    <div class="head" role="row">
      {#each columns as column (column.label)}
        <div
          role="columnheader"
          aria-sort={column.key && sort === column.key ? "ascending" : "none"}
        >
          {#if column.key}
            {@const sortKey = column.key}
            <button
              type="button"
              class:active={sort === sortKey}
              onclick={() => (sort = sortKey)}
            >
              {t(column.label)}{#if sort === sortKey}<Icon
                  name="arrow-down"
                  size={12}
                />{/if}
            </button>
          {:else}
            {t(column.label)}
          {/if}
        </div>
      {/each}
    </div>
    <div class="rows">
      {#if ready && items.length === 0}
        <EmptyState icon="list" title={t("list.empty")} />
      {/if}
      <VirtualList
        {items}
        itemHeight={ROW}
        itemKey={(i: Card) => i.key}
        onNearEnd={more}
      >
        {#snippet row(item: Card)}
          <div
            class="line"
            class:archived={Boolean(item.archived_at)}
            class:blocked={item.blocked}
            role="row"
          >
            <span role="cell"
              ><a class="mono" href={itemHref(item.key)}>{item.key}</a></span
            >
            <span role="cell" class="title"
              >{item.title}{#if item.blocked}<em class="blocked-label">
                  · {t("board.blocked")}</em
                >{/if}</span
            >
            <span role="cell" class="typ"
              ><TypeIcon type={item.type} size={14} />{t(
                `types.${item.type}` as "types.epic",
              )}</span
            >
            <span role="cell"
              ><StatusChip
                name={item.status ?? ""}
                tone={tone(item.status)}
              /></span
            >
            <span role="cell"
              ><PriorityIndicator priority={item.priority} /></span
            >
            <span role="cell" class="who"
              >{#if item.assignee !== null}<Avatar
                  name={personName(item.assignee)}
                  size={22}
                  userId={item.assignee}
                />{personName(item.assignee)}{/if}</span
            >
            <span
              role="cell"
              class="due"
              class:late={item.due_at !== null &&
                item.due_at < Date.now() &&
                !isDone(item.status)}
              >{item.due_at === null
                ? ""
                : dateFormat.format(item.due_at)}</span
            >
            <span role="cell" class="muted"
              >{item.updated_at ? dateFormat.format(item.updated_at) : ""}</span
            >
          </div>
        {/snippet}
      </VirtualList>
    </div>
  </div>
</section>

<style>
  .list {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    min-height: 0;
  }
  .table {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    background: var(--color-surface);
    overflow-x: auto;
  }
  .head,
  .line {
    display: grid;
    grid-template-columns:
      6rem minmax(14rem, 1fr)
      6.5rem 9rem 6.5rem 9rem 6.5rem 6.5rem;
    gap: var(--space-3);
    align-items: center;
    min-width: 62rem;
    padding: 0 var(--space-4);
    height: 100%;
    font-size: var(--atlas-fs-md);
    border-bottom: 1px solid var(--color-border);
  }
  .head {
    flex: none;
    height: 44px;
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-muted);
    background: var(--color-soft);
  }
  .head button {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-height: 28px;
    padding: 0 2px;
    border: none;
    background: none;
    color: inherit;
    font: inherit;
    letter-spacing: inherit;
    text-transform: inherit;
  }
  .head button:hover:not(:disabled) {
    color: var(--color-text);
    background: none;
  }
  .head button.active {
    color: var(--color-accent);
  }
  .rows {
    flex: 1;
    min-height: 0;
    min-width: 62rem;
  }
  .line {
    position: relative;
    border-bottom-color: color-mix(
      in srgb,
      var(--color-border) 60%,
      transparent
    );
  }
  .line.archived {
    opacity: 0.62;
  }
  .line.blocked::before {
    content: "";
    position: absolute;
    inset: 6px auto 6px 0;
    width: 3px;
    border-radius: 0 3px 3px 0;
    background: var(--color-danger);
  }
  .line:hover {
    background: var(--color-soft);
  }
  .line > span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title {
    font-weight: var(--atlas-fw-semibold);
  }
  .typ,
  .who {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .mono {
    font: 700 11px var(--font-mono);
    color: var(--color-accent);
    text-decoration: none;
  }
  .mono:hover {
    text-decoration: underline;
  }
  .blocked-label {
    color: var(--color-danger);
    font-style: normal;
    font-weight: var(--atlas-fw-regular);
  }
  .due {
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .due.late {
    color: var(--color-danger);
    font-weight: var(--atlas-fw-semibold);
  }
  .muted {
    font-size: var(--atlas-fs-sm);
  }
</style>
