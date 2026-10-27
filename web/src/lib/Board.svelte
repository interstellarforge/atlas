<script lang="ts">
  import { tick } from "svelte";

  import {
    ApiError,
    archiveItem,
    type Card,
    type Column,
    createItem,
    emptyFilters,
    filterQuery,
    type Filters,
    getBoard,
    getBoardColumn,
    getCard,
    getCrossBoard,
    getItems,
    moveItem,
    type Workflow,
  } from "./api";
  import CardView from "./Card.svelte";
  import { onChange, onResync } from "./events.svelte";
  import FiltersBar from "./Filters.svelte";
  import { t } from "./i18n";
  import VirtualList from "./VirtualList.svelte";

  // One card slot: the 88px card plus an 8px gap. Fixed heights keep windowing and drag maths simple.
  const SLOT = 96;
  const CARD_FIELDS =
    "key,type,title,priority,assignee,due_at,parent,root,blocked,version,labels,status,archived_at";

  interface Drag {
    card: Card;
    x: number;
    y: number;
    over: { status: number; index: number; top: number } | undefined;
  }

  // With a project this is its board; without one it is the read-only cross-project overview.
  let {
    project,
    workflow,
    projects = [],
  }: { project?: string; workflow?: Workflow; projects?: string[] } = $props();

  const cross = $derived(project === undefined);

  let columns = $state<Column[]>([]);
  let ready = $state(false);
  let filters = $state<Filters>({ ...emptyFilters });
  let error = $state("");
  let announcement = $state("");
  let drag = $state<Drag | undefined>();
  let loadingMore = $state<Record<number, boolean>>({});
  const scrollers = $state<Record<number, HTMLElement | undefined>>({});

  // On a phone the filters and the add form start collapsed, so the columns get the screen.
  let toolsOpen = $state(!matchMedia("(max-width: 640px)").matches);

  let newType = $state("task");
  let newTitle = $state("");
  let newStatus = $state("");

  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;

  // New cards start in the first status unless another is chosen.
  $effect(() => {
    if (!newStatus && workflow) newStatus = workflow.statuses[0]?.name ?? "";
  });

  async function load(): Promise<void> {
    try {
      // Keep as many cards per column as the user has already scrolled through.
      const limit = Math.min(
        200,
        Math.max(50, ...columns.map((c) => c.items.length)),
      );
      const board =
        project === undefined
          ? await getCrossBoard(projects, filters)
          : await getBoard(project, filters, limit);
      columns = board.columns;
      ready = true;
      error = "";
    } catch {
      error = t("board.loadFailed");
    }
  }

  // Reload when the project, the project selection or a filter changes; typing in search is debounced.
  $effect(() => {
    const trigger = JSON.stringify([project, projects, filters]);
    const timer = setTimeout(
      () => {
        void load();
      },
      ready && trigger ? 250 : 0,
    );
    return () => {
      clearTimeout(timer);
    };
  });

  function refresh(): void {
    clearTimeout(refreshTimer);
    refreshTimer = setTimeout(() => {
      if (drag) dirty = true;
      else void load();
    }, 150);
  }

  // Live updates: a plain field change refetches just that card; anything that can change
  // positions (create, move, archive, rebalance) refetches the board.
  $effect(() =>
    onChange((event) => {
      if (project !== undefined && event.project !== project) return;
      if (event.type === "item.updated" && event.key && project !== undefined) {
        void patchCard(event.key);
        return;
      }
      refresh();
    }),
  );
  $effect(() =>
    onResync(() => {
      void load();
    }),
  );

  async function patchCard(key: string): Promise<void> {
    const column = columns.find((c) => c.items.some((i) => i.key === key));
    if (!column) return;
    try {
      const fresh = await getCard(key, CARD_FIELDS);
      if (
        fresh.status !== column.status.name ||
        (fresh.archived_at && !filters.archived)
      ) {
        refresh();
        return;
      }
      const index = column.items.findIndex((i) => i.key === key);
      if (index >= 0) column.items[index] = fresh;
    } catch {
      refresh();
    }
  }

  async function loadMore(column: Column): Promise<void> {
    const id = column.status.id;
    const cursor = column.next_cursor;
    if (!cursor || loadingMore[id]) return;
    loadingMore[id] = true;
    try {
      const page =
        project !== undefined
          ? await getBoardColumn(project, id, filters, cursor)
          : await getItems({
              ...filterQuery(filters),
              projects: projects.join(","),
              category: column.status.category,
              fields: `${CARD_FIELDS},project`,
              limit: 50,
              cursor,
            });
      const have = new Set(column.items.map((i) => i.key));
      column.items.push(...page.items.filter((i) => !have.has(i.key)));
      column.next_cursor = page.next_cursor;
    } catch {
      error = t("board.loadFailed");
    } finally {
      loadingMore[id] = false;
    }
  }

  // ---- moving cards ----

  function recount(column: Column): void {
    column.over_wip =
      column.status.wip_limit !== null &&
      column.count > column.status.wip_limit;
  }

  function fail(e: unknown): void {
    error =
      e instanceof ApiError && e.status === 409
        ? t("board.conflict")
        : t("board.saveFailed", {
            message: e instanceof ApiError ? e.message : String(e),
          });
    void load();
  }

  // Moves a card to `index` among the target column's other cards. The board updates at once;
  // the server confirms or the board is reloaded to show what really happened.
  async function commitMove(
    card: Card,
    toStatus: number,
    index: number,
  ): Promise<number | undefined> {
    const source = columns.find((c) => c.items.some((i) => i.key === card.key));
    const target = columns.find((c) => c.status.id === toStatus);
    const moving = source?.items.find((i) => i.key === card.key);
    if (!source || !target || !moving) return undefined;

    const others = target.items.filter((i) => i.key !== card.key);
    const position = Math.max(0, Math.min(index, others.length));
    const after = others[position - 1]?.key;
    const before = others[position]?.key;
    const changedStatus = source.status.id !== target.status.id;

    source.items = source.items.filter((i) => i.key !== card.key);
    source.count -= 1;
    target.items.splice(position, 0, moving);
    target.count += 1;
    recount(source);
    recount(target);
    error = "";

    try {
      const saved = await moveItem(card.key, card.version, {
        ...(changedStatus && { status: target.status.name }),
        ...(after && { after }),
        ...(before && { before }),
      });
      moving.version = saved.version;
      announcement = changedStatus
        ? t("board.moved", { key: card.key, status: target.status.name })
        : t("board.reordered", {
            key: card.key,
            position: position + 1,
            status: target.status.name,
          });
      return position;
    } catch (e) {
      fail(e);
      return undefined;
    }
  }

  function locate(x: number, y: number): Drag["over"] {
    for (const el of document.elementsFromPoint(x, y)) {
      const columnEl = el.closest<HTMLElement>("[data-col]");
      if (!columnEl) continue;
      const id = Number(columnEl.dataset["col"]);
      const scroller = scrollers[id];
      const column = columns.find((c) => c.status.id === id);
      if (!scroller || !column) return undefined;
      const rect = scroller.getBoundingClientRect();
      const index = Math.min(
        column.items.length,
        Math.max(0, Math.round((y - rect.top + scroller.scrollTop) / SLOT)),
      );
      return { status: id, index, top: index * SLOT - scroller.scrollTop };
    }
    return undefined;
  }

  function startDrag(event: PointerEvent, card: Card): void {
    if (event.pointerType === "mouse" && event.button !== 0) return;
    event.preventDefault();
    drag = {
      card,
      x: event.clientX,
      y: event.clientY,
      over: locate(event.clientX, event.clientY),
    };
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", stopDrag);
  }

  function onMove(event: PointerEvent): void {
    if (!drag) return;
    drag.x = event.clientX;
    drag.y = event.clientY;
    drag.over = locate(event.clientX, event.clientY);
  }

  function stopDrag(): void {
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
    window.removeEventListener("pointercancel", stopDrag);
    drag = undefined;
  }

  function onUp(): void {
    const finished = drag;
    stopDrag();
    if (finished?.over) {
      const target = columns.find((c) => c.status.id === finished.over?.status);
      const current =
        target?.items.findIndex((i) => i.key === finished.card.key) ?? -1;
      // The pointer index counts the dragged card itself when it is in the same column.
      const index =
        current >= 0 && current < finished.over.index
          ? finished.over.index - 1
          : finished.over.index;
      if (!(current === index))
        void commitMove(finished.card, finished.over.status, index);
    }
    if (dirty) {
      dirty = false;
      refresh();
    }
  }

  // After a keyboard move the card may sit far down a windowed column: scroll to it, wait until
  // it is rendered, then give it focus again.
  async function refocus(
    key: string,
    status: number,
    position: number,
  ): Promise<void> {
    const scroller = scrollers[status];
    if (scroller)
      scroller.scrollTop = Math.max(
        0,
        position * SLOT - scroller.clientHeight / 2,
      );
    for (let attempt = 0; attempt < 6; attempt++) {
      await tick();
      const el = document.querySelector<HTMLElement>(
        `[data-key="${CSS.escape(key)}"]`,
      );
      if (el) {
        el.focus();
        return;
      }
      await new Promise((resolve) => requestAnimationFrame(resolve));
    }
  }

  // Alt + arrows reorder or change column; plain up/down moves focus between cards.
  function onCardKey(event: KeyboardEvent, card: Card): void {
    const columnIndex = columns.findIndex((c) =>
      c.items.some((i) => i.key === card.key),
    );
    const column = columns[columnIndex];
    if (!column) return;
    const position = column.items.findIndex((i) => i.key === card.key);

    if (!event.altKey) {
      if (event.key !== "ArrowDown" && event.key !== "ArrowUp") return;
      const cards = [
        ...((event.currentTarget as HTMLElement)
          .closest("[data-col]")
          ?.querySelectorAll<HTMLElement>("[data-key]") ?? []),
      ];
      const next =
        cards[
          cards.indexOf(event.currentTarget as HTMLElement) +
            (event.key === "ArrowDown" ? 1 : -1)
        ];
      if (next) {
        event.preventDefault();
        next.focus();
      }
      return;
    }

    let status = column.status.id;
    let index = position;
    switch (event.key) {
      case "ArrowUp":
        index = position - 1;
        break;
      case "ArrowDown":
        index = position + 1;
        if (index >= column.items.length) return;
        break;
      case "ArrowLeft":
      case "ArrowRight": {
        const neighbour =
          columns[columnIndex + (event.key === "ArrowLeft" ? -1 : 1)];
        if (!neighbour) return;
        status = neighbour.status.id;
        index = neighbour.items.length;
        break;
      }
      default:
        return;
    }
    if (index < 0) return;
    event.preventDefault();
    void commitMove(card, status, index).then((position) => {
      if (position !== undefined) void refocus(card.key, status, position);
    });
  }

  async function toggleArchive(card: Card): Promise<void> {
    try {
      await archiveItem(card.key, card.version, !card.archived_at);
      await load();
    } catch (e) {
      fail(e);
    }
  }

  async function add(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (project === undefined) return;
    try {
      await createItem({
        project,
        type: newType,
        title: newTitle,
        ...(newStatus && { status: newStatus }),
      });
      newTitle = "";
      error = "";
      await load();
    } catch (e) {
      fail(e);
    }
  }
</script>

<section class="board">
  <details class="tools" bind:open={toolsOpen}>
    <summary>{t("board.tools")}</summary>
    <div class="toolbar">
      <FiltersBar
        bind:filters
        labels={workflow?.labels ?? []}
        milestones={workflow?.milestones ?? []}
        types={workflow?.types ?? ["epic", "story", "task", "bug"]}
        showCardFields
      />
      {#if !cross && workflow}
        <form class="add" onsubmit={add}>
          <select aria-label={t("board.type")} bind:value={newType}>
            {#each workflow.types as typ (typ)}
              <option value={typ}>{t(`types.${typ}` as "types.epic")}</option>
            {/each}
          </select>
          <input
            class="title-input"
            placeholder={t("board.itemTitle")}
            aria-label={t("board.itemTitle")}
            bind:value={newTitle}
            required
            maxlength="500"
          />
          <select aria-label={t("board.status")} bind:value={newStatus}>
            {#each workflow.statuses as st (st.id)}
              <option value={st.name}>{st.name}</option>
            {/each}
          </select>
          <button class="primary" type="submit">{t("board.submit")}</button>
        </form>
      {/if}
    </div>
  </details>

  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <p class="muted note">{cross ? t("cross.readOnly") : t("board.help")}</p>
  <p class="sr-only" aria-live="polite">{announcement}</p>

  <div class="columns">
    {#each columns as column (`${String(column.status.id)}:${column.status.name}`)}
      <div
        class="column"
        data-col={column.status.id}
        class:over={drag?.over?.status === column.status.id}
      >
        <header>
          <h2>{column.status.name}</h2>
          <span class="count" class:warn={column.over_wip}>
            {column.status.wip_limit
              ? t("board.wip", {
                  count: column.count,
                  limit: column.status.wip_limit,
                })
              : t("board.count", { count: column.count })}
          </span>
          {#if column.over_wip}<span class="warn-text"
              >{t("board.overWip")}</span
            >{/if}
        </header>
        <div class="body">
          {#if ready && column.items.length === 0}<p class="muted empty">
              {t("board.empty")}
            </p>{/if}
          <VirtualList
            items={column.items}
            itemHeight={SLOT}
            itemKey={(c: Card) => c.key}
            bind:scroller={scrollers[column.status.id]}
            onNearEnd={() => void loadMore(column)}
          >
            {#snippet row(card: Card)}
              <div class="slot">
                <CardView
                  {card}
                  done={column.status.category === "completed"}
                  showProject={cross}
                  readonly={cross}
                  dragging={drag?.card.key === card.key}
                  onhandle={(e: PointerEvent) => {
                    startDrag(e, card);
                  }}
                  onkeydown={(e: KeyboardEvent) => {
                    onCardKey(e, card);
                  }}
                  onarchive={() => void toggleArchive(card)}
                />
              </div>
            {/snippet}
          </VirtualList>
          {#if drag?.over?.status === column.status.id}
            <div class="drop" style:top="{drag.over.top}px"></div>
          {/if}
        </div>
      </div>
    {/each}
  </div>

  {#if drag}
    <div class="ghost" style:left="{drag.x + 12}px" style:top="{drag.y + 8}px">
      <strong>{drag.card.key}</strong>
      {drag.card.title}
    </div>
  {/if}
</section>

<style>
  .board {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    min-height: 0;
  }
  .tools summary {
    cursor: pointer;
    margin-bottom: var(--space-2);
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-bold);
    color: var(--color-muted);
  }
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    justify-content: space-between;
  }
  .add {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .title-input {
    min-width: 12rem;
  }
  .note {
    margin: 0;
    font-size: 0.8rem;
  }
  .columns {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--space-3);
    overflow-x: auto;
    scroll-snap-type: x proximity;
    padding-bottom: var(--space-2);
  }
  .column {
    flex: 0 0 var(--atlas-column-w);
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: var(--space-2);
    background: var(--color-soft);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
    scroll-snap-align: start;
  }
  .column.over {
    border-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .column header {
    flex: none;
    height: var(--atlas-column-header-h);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 6px;
  }
  h2 {
    margin: 0;
    font-size: 13px;
    font-weight: var(--atlas-fw-bold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    margin-left: auto;
    padding: 3px 7px;
    font: 600 11px var(--font-mono);
    color: var(--color-muted);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-pill);
    white-space: nowrap;
  }
  .count.warn {
    color: var(--color-danger);
    border-color: var(--color-danger);
    font-weight: 700;
  }
  .warn-text {
    color: var(--color-danger);
    font-size: 10px;
    font-weight: 700;
    white-space: nowrap;
  }
  .column:has(.count.warn) {
    border-color: var(--color-danger);
  }
  .body {
    position: relative;
    flex: 1;
    min-height: 0;
    padding: 0 var(--space-2);
  }
  .empty {
    margin: 0;
    padding: var(--space-3);
    font-size: 0.85rem;
  }
  .slot {
    height: 100%;
    padding-bottom: 8px;
  }
  .drop {
    position: absolute;
    left: var(--space-2);
    right: var(--space-2);
    height: 4px;
    margin-top: -2px;
    border-radius: var(--atlas-radius-pill);
    background: var(--color-accent);
    pointer-events: none;
  }
  .ghost {
    position: fixed;
    z-index: 20;
    max-width: 18rem;
    padding: var(--space-2) var(--space-3);
    font-size: 0.85rem;
    background: var(--color-surface);
    border: 1px solid var(--color-accent);
    border-radius: var(--radius);
    pointer-events: none;
    box-shadow: var(--atlas-shadow-lg);
    transform: rotate(-1.5deg);
  }
  @media (max-width: 640px) {
    .column {
      flex-basis: var(--atlas-column-w);
    }
  }
</style>
