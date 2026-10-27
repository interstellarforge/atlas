<script lang="ts">
  import {
    type Card,
    emptyFilters,
    filterQuery,
    type Filters,
    getItems,
    type Workflow,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import FiltersBar from "./Filters.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import PriorityIndicator from "./PriorityIndicator.svelte";
  import { itemHref } from "./routes";
  import StatusChip from "./StatusChip.svelte";
  import { statusTone } from "./statusTone";
  import TypeIcon from "./TypeIcon.svelte";

  const FIELDS = "key,type,title,priority,parent,root,blocked,version,status";

  interface Group {
    epic: string;
    title: string;
    items: Card[];
  }

  let { project, workflow }: { project: string; workflow?: Workflow } =
    $props();

  let items = $state<Card[]>([]);
  let epics = $state<Card[]>([]);
  let nextCursor = $state<string | null>(null);
  let filters = $state<Filters>({ ...emptyFilters });
  let ready = $state(false);
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let closed = $state<Record<string, boolean>>({});

  function tone(name: string | undefined): ReturnType<typeof statusTone> {
    const all = workflow?.statuses ?? [];
    const st = all.find((x) => x.name === name);
    return st ? statusTone(st, all) : "todo";
  }

  // Work that has not started yet (status category "todo"), in board order, grouped under its epic.
  const groups = $derived(group(items, epics));

  function group(all: Card[], epicList: Card[]): Group[] {
    const known: Record<string, Group> = {};
    const out: Group[] = epicList.map(
      (e) => (known[e.key] = { epic: e.key, title: e.title, items: [] }),
    );
    const none: Group = { epic: "", title: t("backlog.noEpic"), items: [] };
    for (const item of all) {
      if (item.type === "epic") continue;
      (known[item.root] ?? none).items.push(item);
    }
    return [...out, none].filter((g) => g.items.length > 0);
  }

  async function load(more: boolean): Promise<void> {
    const base = {
      ...filterQuery(filters),
      project,
      category: "todo",
      sort: "rank",
      fields: FIELDS,
    };
    const [page, epicPage] = await Promise.all([
      getItems({
        ...base,
        limit: more ? 100 : Math.min(200, Math.max(100, items.length)),
        ...(more && nextCursor && { cursor: nextCursor }),
      }),
      more
        ? Promise.resolve({ items: epics, next_cursor: null })
        : getItems({
            project,
            type: "epic",
            fields: "key,title,type,root,priority,version",
            limit: 200,
          }),
    ]);
    items = more ? [...items, ...page.items] : page.items;
    epics = epicPage.items;
    nextCursor = page.next_cursor;
    ready = true;
  }

  $effect(() => {
    const trigger = JSON.stringify([project, filters]);
    const timer = setTimeout(
      () => {
        void load(false);
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
        void load(false);
      }, 200);
    }),
  );
  $effect(() =>
    onResync(() => {
      void load(false);
    }),
  );
</script>

<section class="backlog">
  <FiltersBar
    bind:filters
    labels={workflow?.labels ?? []}
    milestones={workflow?.milestones ?? []}
    types={workflow?.types ?? []}
  />
  {#if ready && groups.length === 0}
    <EmptyState icon="backlog" title={t("backlog.empty")} />
  {/if}
  {#each groups as g (g.epic)}
    <section class="group">
      <h2>
        <button
          type="button"
          class="gtoggle"
          aria-expanded={!closed[g.epic]}
          onclick={() => (closed[g.epic] = !closed[g.epic])}
        >
          <Icon
            name={closed[g.epic] ? "chevron-right" : "chevron-down"}
            size={16}
          />
          {#if g.epic}<TypeIcon type="epic" />
            <span class="mono">{g.epic}</span>{/if}
          <span class="gtitle">{g.title}</span>
          <span class="chip">{g.items.length}</span>
        </button>
      </h2>
      {#if !closed[g.epic]}
        <ul>
          {#each g.items as item (item.key)}
            <li>
              <TypeIcon type={item.type} size={14} />
              <a class="mono" href={itemHref(item.key)}>{item.key}</a>
              <span class="title">{item.title}</span>
              {#if item.blocked}<span class="chip warn"
                  ><Icon name="alert" size={12} />{t("board.blocked")}</span
                >{/if}
              <StatusChip name={item.status ?? ""} tone={tone(item.status)} />
              <PriorityIndicator priority={item.priority} />
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {/each}
  {#if nextCursor}
    <button type="button" onclick={() => void load(true)}
      >{t("backlog.loadMore")}</button
    >
  {/if}
</section>

<style>
  .backlog {
    display: grid;
    gap: var(--space-4);
    align-content: start;
    height: 100%;
    overflow-y: auto;
  }
  .group {
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    background: var(--color-surface);
    overflow: hidden;
  }
  .group h2 {
    margin: 0;
    font-size: var(--atlas-fs-md);
    background: var(--color-soft);
    border-bottom: 1px solid var(--color-border);
  }
  .gtoggle {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
    height: 48px;
    padding: 0 14px;
    border: none;
    border-radius: 0;
    background: none;
    text-align: left;
    font: inherit;
    font-weight: var(--atlas-fw-bold);
  }
  .gtitle {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    height: 44px;
    display: flex;
    align-items: center;
    gap: 10px;
    min-width: 0;
    padding: 0 14px 0 32px;
    font-size: var(--atlas-fs-md);
    border-bottom: 1px solid
      color-mix(in srgb, var(--color-border) 60%, transparent);
  }
  li:hover {
    background: var(--color-soft);
  }
  li:last-child {
    border-bottom: 0;
  }
  .mono {
    font: 700 11px var(--font-mono);
    color: var(--color-accent);
    text-decoration: none;
  }
  .title {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: var(--atlas-fw-semibold);
  }
  @media (max-width: 720px) {
    li {
      padding-left: 14px;
      gap: 8px;
    }
  }
</style>
