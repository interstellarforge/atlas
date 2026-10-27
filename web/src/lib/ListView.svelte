<script lang="ts">
  import {
    type Card,
    emptyFilters,
    filterQuery,
    type Filters,
    getItems,
    type Workflow,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import FiltersBar from "./Filters.svelte";
  import { t } from "./i18n";
  import { personName } from "./people.svelte";
  import { itemHref } from "./routes";
  import VirtualList from "./VirtualList.svelte";

  const ROW = 48; // --atlas-list-row-h
  const FIELDS =
    "key,type,title,status,priority,assignee,due_at,blocked,updated_at,archived_at";
  type SortKey = "key" | "priority" | "due" | "updated";

  let { project, workflow }: { project: string; workflow?: Workflow } =
    $props();

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

  function more(): void {
    if (nextCursor) void load(false);
  }
</script>

<section class="list">
  <FiltersBar
    bind:filters
    labels={workflow?.labels ?? []}
    milestones={workflow?.milestones ?? []}
    types={workflow?.types ?? []}
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
              {t(column.label)}
            </button>
          {:else}
            {t(column.label)}
          {/if}
        </div>
      {/each}
    </div>
    <div class="rows">
      {#if ready && items.length === 0}<p class="muted pad">
          {t("list.empty")}
        </p>{/if}
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
            role="row"
          >
            <a class="mono" href={itemHref(item.key)}>{item.key}</a>
            <span class="title"
              >{item.title}{#if item.blocked}<em class="blocked">
                  · {t("board.blocked")}</em
                >{/if}</span
            >
            <span>{t(`types.${item.type}` as "types.epic")}</span>
            <span>{item.status ?? ""}</span>
            <span>{t(`priority.${item.priority}` as "priority.low")}</span>
            <span
              >{item.assignee === null ? "" : personName(item.assignee)}</span
            >
            <span
              >{item.due_at === null
                ? ""
                : dateFormat.format(item.due_at)}</span
            >
            <span
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
    border-radius: var(--radius-md);
    background: var(--color-surface);
    overflow-x: auto;
  }
  .head,
  .line {
    height: 100%;
    font-size: 12px;
    border-bottom: 1px solid var(--color-border);
  }
  .head {
    height: 44px;
    font-size: 11px;
    font-weight: var(--atlas-fw-bold);
    color: var(--color-muted);
    background: var(--color-soft);
    border-bottom: 1px solid var(--color-border);
  }
  .head button {
    min-height: 0;
    padding: 0;
    border: none;
    background: none;
    color: inherit;
    font-weight: inherit;
    text-decoration: underline;
  }
  .head button.active {
    color: var(--color-accent);
    font-weight: 700;
  }
  .rows {
    flex: 1;
    min-height: 0;
    min-width: 60rem;
  }
  .line {
    height: 100%;
    font-size: 0.875rem;
    border-bottom: 1px solid var(--color-border);
  }
  .line.archived {
    opacity: 0.62;
  }
  .line:hover {
    background: var(--color-soft);
  }
  .line > span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .mono {
    font: 700 11px var(--font-mono);
    color: var(--color-muted);
  }
  .blocked {
    color: var(--color-danger);
    font-style: normal;
  }
  .pad {
    padding: var(--space-3);
  }
</style>
