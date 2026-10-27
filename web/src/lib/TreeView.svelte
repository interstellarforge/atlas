<script lang="ts">
  import { type Card, getItems } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import PriorityIndicator from "./PriorityIndicator.svelte";
  import { itemHref } from "./routes";
  import TypeIcon from "./TypeIcon.svelte";
  import VirtualList from "./VirtualList.svelte";

  const ROW = 44; // --atlas-hierarchy-row-h
  const FIELDS = "key,type,title,status,priority,parent,blocked,version";

  interface Row {
    item: Card;
    depth: number;
    children: number;
  }

  let { project }: { project: string } = $props();

  let items = $state<Card[]>([]);
  let collapsed = $state<Record<string, boolean>>({});
  let ready = $state(false);
  let refreshTimer: ReturnType<typeof setTimeout> | undefined;

  const rows = $derived(flatten(items, collapsed));

  // Epics, stories and tasks as one indented list; collapsed branches drop out of the rows.
  function flatten(all: Card[], closed: Record<string, boolean>): Row[] {
    const keys = new Set(all.map((i) => i.key));
    const children: Record<string, Card[]> = {};
    const roots: Card[] = [];
    for (const i of all) {
      if (i.parent !== null && keys.has(i.parent))
        children[i.parent] = [...(children[i.parent] ?? []), i];
      else roots.push(i);
    }
    const out: Row[] = [];
    const walk = (list: Card[], depth: number): void => {
      for (const i of list) {
        const kids = children[i.key] ?? [];
        out.push({ item: i, depth, children: kids.length });
        if (!closed[i.key]) walk(kids, depth + 1);
      }
    };
    walk(roots, 0);
    return out;
  }

  // The whole project is loaded (200 per request) so branches can open instantly; rows are windowed.
  async function load(): Promise<void> {
    const all: Card[] = [];
    let cursor = "";
    do {
      const page = await getItems({
        project,
        fields: FIELDS,
        limit: 200,
        ...(cursor && { cursor }),
      });
      all.push(...page.items);
      cursor = page.next_cursor ?? "";
    } while (cursor);
    items = all;
    ready = true;
  }

  $effect(() => {
    if (project) void load();
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project) return;
      clearTimeout(refreshTimer);
      refreshTimer = setTimeout(() => void load(), 250);
    }),
  );
  $effect(() =>
    onResync(() => {
      void load();
    }),
  );
</script>

<section class="tree">
  {#if ready && rows.length === 0}<EmptyState
      icon="tree"
      title={t("tree.empty")}
    />{/if}
  <div class="rows">
    <VirtualList items={rows} itemHeight={ROW} itemKey={(r: Row) => r.item.key}>
      {#snippet row(r: Row)}
        <div class="line" role="row" aria-level={r.depth + 1}>
          <span class="guides" style:width="calc({r.depth} * 1.5rem)"></span>
          {#if r.children > 0}
            <button
              type="button"
              class="toggle"
              aria-expanded={!collapsed[r.item.key]}
              aria-label={t(
                collapsed[r.item.key] ? "tree.expand" : "tree.collapse",
                { key: r.item.key },
              )}
              onclick={() => (collapsed[r.item.key] = !collapsed[r.item.key])}
              ><Icon
                name={collapsed[r.item.key] ? "chevron-right" : "chevron-down"}
                size={16}
              /></button
            >
          {:else}
            <span class="toggle"></span>
          {/if}
          <TypeIcon type={r.item.type} />
          <a class="mono" href={itemHref(r.item.key)}>{r.item.key}</a>
          <span class="title">{r.item.title}</span>
          {#if r.children > 0}<span class="chip">{r.children}</span>{/if}
          {#if r.item.blocked}<span class="chip warn"
              ><Icon name="alert" size={12} />{t("board.blocked")}</span
            >{/if}
          <span class="chip">{r.item.status ?? ""}</span>
          <PriorityIndicator priority={r.item.priority} label={false} />
        </div>
      {/snippet}
    </VirtualList>
  </div>
</section>

<style>
  .tree {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
  }
  .rows {
    flex: 1;
    min-height: 0;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    background: var(--color-surface);
    overflow: hidden;
  }
  .line {
    height: 100%;
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 0 14px 0 var(--space-3);
    border-bottom: 1px solid
      color-mix(in srgb, var(--color-border) 60%, transparent);
    font-size: var(--atlas-fs-md);
    min-width: 0;
  }
  .line:hover {
    background: var(--color-soft);
  }
  .guides {
    flex: none;
    align-self: stretch;
    background: repeating-linear-gradient(
      90deg,
      transparent 0 11px,
      var(--color-border) 11px 12px,
      transparent 12px 24px
    );
  }
  .toggle {
    flex: none;
    display: inline-grid;
    place-items: center;
    width: 26px;
    min-height: 26px;
    height: 26px;
    padding: 0;
    border: none;
    background: none;
    color: var(--color-muted);
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
</style>
