<script lang="ts">
  import { tick } from "svelte";

  import {
    ApiError,
    archiveItem,
    type Card,
    type Column,
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
  import CardFieldsMenu from "./CardFieldsMenu.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import FiltersBar from "./Filters.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import ItemPanel from "./ItemPanel.svelte";
  import { route } from "./router.svelte";
  import { matchProject } from "./routes";
  import { statusTone, type Tone, toneVar } from "./statusTone";
  import VirtualList from "./VirtualList.svelte";

  // One card slot: the 140px card (--atlas-card-h) plus an 8px gap. Fixed heights keep windowing and drag maths simple.
  const SLOT = 148;
  const CARD_FIELDS =
    "key,type,title,priority,assignee,due_at,parent,root,blocked,version,labels,status,archived_at";

  interface Drag {
    card: Card;
    x: number;
    y: number;
    over:
      { status: number; index: number; top: number; sub?: string } | undefined;
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

  // On a phone the filters start collapsed, so the columns get the screen.
  let filtersOpen = $state(!matchMedia("(max-width: 900px)").matches);
  // Status the empty "new item" panel starts in; undefined while it is closed.
  let drafting = $state<string | undefined>();
  const openKey = $derived(matchProject(route.path)?.item);

  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;

  function openNew(status?: string): void {
    drafting =
      status ?? workflow?.statuses.find((st) => st.allow_create)?.name ?? "";
  }

  // Opening the created item replaces the draft panel.
  $effect(() => {
    if (openKey) drafting = undefined;
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
        !(column.statuses ?? [column.status]).some(
          (st) => st.name === fresh.status,
        ) ||
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
    if (column.statuses) return; // WIP is per status there; the refresh after the move corrects the flag
    column.over_wip =
      column.status.wip_limit !== null &&
      column.count > column.status.wip_limit;
  }

  // The status "Add item" creates in: the first one of the column that allows creating.
  function creatable(column: Column): string | undefined {
    return (column.statuses ?? [column.status]).find((st) => st.allow_create)
      ?.name;
  }

  function cardStatus(
    column: Column,
    name: string,
  ): { name: string; tone: Tone } | undefined {
    const st = column.statuses?.find((s) => s.name === name);
    return st
      ? {
          name,
          tone: statusTone(st, workflow?.statuses ?? column.statuses ?? []),
        }
      : undefined;
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
    sub?: string,
  ): Promise<number | undefined> {
    const source = columns.find((c) => c.items.some((i) => i.key === card.key));
    const target = columns.find((c) => c.status.id === toStatus);
    const moving = source?.items.find((i) => i.key === card.key);
    if (!source || !target || !moving) return undefined;

    const others = target.items.filter((i) => i.key !== card.key);
    const position = Math.max(0, Math.min(index, others.length));
    const after = others[position - 1]?.key;
    const before = others[position]?.key;
    // In a merged column `sub` names the member status the card was dropped on.
    const newStatus = sub ?? target.status.name;
    const changedStatus =
      source.status.id !== target.status.id ||
      newStatus !== (moving.status ?? source.status.name);

    source.items = source.items.filter((i) => i.key !== card.key);
    source.count -= 1;
    if (changedStatus) moving.status = newStatus;
    target.items.splice(position, 0, moving);
    target.count += 1;
    recount(source);
    recount(target);
    error = "";

    try {
      const saved = await moveItem(card.key, card.version, {
        ...(changedStatus && { status: newStatus }),
        ...(after && { after }),
        ...(before && { before }),
      });
      moving.version = saved.version;
      announcement = changedStatus
        ? t("board.moved", { key: card.key, status: newStatus })
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

  // Hint only: the server decides from the item's whole history, this looks at the card's current status.
  function blocked(column: Column): boolean {
    if (!drag) return false;
    const from =
      drag.card.status ??
      columns.find((c) => c.items.some((i) => i.key === drag?.card.key))?.status
        .name;
    return (column.statuses ?? [column.status]).every(
      (st) =>
        st.name !== from &&
        st.requires.length > 0 &&
        from !== undefined &&
        !st.requires.includes(from),
    );
  }

  function locate(x: number, y: number): Drag["over"] {
    for (const el of document.elementsFromPoint(x, y)) {
      const columnEl = el.closest<HTMLElement>("[data-col]");
      if (!columnEl) continue;
      const id = Number(columnEl.dataset["col"]);
      const scroller = scrollers[id];
      const column = columns.find((c) => c.status.id === id);
      if (!scroller || !column) return undefined;
      const sub = el
        .closest<HTMLElement>("[data-sub]")
        ?.getAttribute("data-sub");
      if (sub) {
        // A status row of a merged column: drop at the end of the column, in that status.
        return { status: id, index: column.items.length, top: 0, sub };
      }
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
      const sub = finished.over.sub;
      const moved =
        sub !== undefined &&
        sub !== (finished.card.status ?? target?.status.name);
      if (current !== index || moved)
        void commitMove(finished.card, finished.over.status, index, sub);
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
</script>

<section class="board">
  <div class="toolbar">
    <button
      type="button"
      class="filtertoggle"
      aria-expanded={filtersOpen}
      onclick={() => (filtersOpen = !filtersOpen)}
      ><Icon name="search" size={16} />{t("filters.label")}</button
    >
    <div class="filters" class:open={filtersOpen}>
      <FiltersBar
        bind:filters
        labels={workflow?.labels ?? []}
        milestones={workflow?.milestones ?? []}
        types={workflow?.types ?? ["epic", "story", "task", "bug"]}
      />
    </div>
    <div class="actions">
      <CardFieldsMenu />
      {#if !cross && workflow}
        <button
          type="button"
          class="primary new"
          onclick={() => {
            openNew();
          }}><Icon name="plus" size={16} />{t("board.newItem")}</button
        >
      {/if}
    </div>
  </div>

  {#if !cross && workflow && project !== undefined && drafting !== undefined}
    <ItemPanel
      create={{ project, status: drafting }}
      {workflow}
      onclose={() => {
        drafting = undefined;
      }}
    />
  {/if}

  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  <p class="sr-only">{cross ? t("cross.readOnly") : t("board.help")}</p>
  <p class="sr-only" aria-live="polite">{announcement}</p>

  <div class="columns">
    {#each columns as column (`${String(column.status.id)}:${column.status.name}`)}
      <div
        class="column"
        data-col={column.status.id}
        class:over={drag?.over?.status === column.status.id}
        class:blocked={blocked(column)}
        title={blocked(column) ? t("board.dropBlocked") : undefined}
        class:overwip={column.over_wip}
        style:--tone={toneVar(
          statusTone(
            column.status,
            columns.map((c) => c.status),
          ),
        )}
      >
        <header>
          <span class="dot" aria-hidden="true"></span>
          <div class="title">
            <h2>
              {column.statuses
                ? t(
                    `workflow.cat.${column.status.category}` as "workflow.cat.todo",
                  )
                : column.status.name}
            </h2>
            {#if column.statuses && column.statuses.length > 1}
              <p class="members">
                {column.statuses.map((st) => st.name).join(" · ")}
              </p>
            {/if}
          </div>
          {#if !cross && creatable(column) === undefined}
            <span
              class="lock"
              role="img"
              title={t("board.createBlocked")}
              aria-label={t("board.createBlocked")}
              ><Icon name="lock" size={14} /></span
            >
          {/if}
          <span class="count" class:warn={column.over_wip}>
            {column.status.wip_limit && !column.statuses
              ? t("board.wip", {
                  count: column.count,
                  limit: column.status.wip_limit,
                })
              : column.count}
          </span>
          {#if column.over_wip}<span class="warn-text"
              >{t("board.overWip")}</span
            >{/if}
        </header>
        <div class="body">
          {#if ready && column.items.length === 0}
            <EmptyState compact icon="board" title={t("board.empty")}>
              {#if !cross && creatable(column) !== undefined}
                <button
                  type="button"
                  class="addhere"
                  onclick={() => {
                    openNew(creatable(column));
                  }}><Icon name="plus" size={14} />{t("board.addItem")}</button
                >
              {/if}
            </EmptyState>
          {/if}
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
                  selected={openKey === card.key}
                  status={column.statuses && card.status
                    ? cardStatus(column, card.status)
                    : undefined}
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
          {#if drag && column.statuses && column.statuses.length > 1}
            <div class="subdrops">
              {#each column.statuses as st (st.id)}
                <div
                  class="subdrop"
                  class:hot={drag.over?.sub === st.name &&
                    drag.over.status === column.status.id}
                  data-sub={st.name}
                  style:--tone={toneVar(
                    statusTone(st, workflow?.statuses ?? column.statuses),
                  )}
                >
                  <span class="dot" aria-hidden="true"></span>{st.name}
                </div>
              {/each}
            </div>
          {:else if drag?.over?.status === column.status.id}
            <div class="drop" style:top="{drag.over.top}px"></div>
          {/if}
        </div>
        {#if !cross && column.items.length > 0 && creatable(column) !== undefined}
          <button
            type="button"
            class="addbtn"
            onclick={() => {
              openNew(creatable(column));
            }}><Icon name="plus" size={14} />{t("board.addItem")}</button
          >
        {/if}
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
  .toolbar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .filters {
    flex: 1 1 auto;
    min-width: 0;
  }
  .filtertoggle {
    display: none;
    align-items: center;
    gap: 8px;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .new {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 38px;
  }
  .columns {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--space-4);
    overflow-x: auto;
    overflow-y: hidden;
    scroll-snap-type: x proximity;
  }
  .column {
    position: relative;
    flex: 0 0 var(--atlas-column-w);
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 var(--space-2) var(--space-2);
    background: linear-gradient(
      180deg,
      color-mix(in srgb, var(--tone) 7%, var(--color-surface)),
      var(--color-surface) 90px
    );
    border: 1px solid var(--color-border);
    border-top: 3px solid var(--tone);
    border-radius: var(--atlas-radius-md);
    scroll-snap-align: start;
    transition:
      border-color var(--atlas-duration) var(--atlas-ease),
      background var(--atlas-duration) var(--atlas-ease);
  }
  .column.over {
    border-color: var(--color-accent);
    border-top-color: var(--color-accent);
    background: var(--color-accent-soft);
  }
  .column.blocked {
    border: 2px dashed var(--color-danger);
    background: var(--color-danger-soft);
    cursor: not-allowed;
  }
  .column.overwip {
    border-color: var(--color-danger);
  }
  .column header {
    flex: none;
    height: var(--atlas-column-header-h);
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 0 6px;
  }
  .dot {
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: 50%;
    background: var(--tone);
    box-shadow: 0 0 0 3px color-mix(in srgb, var(--tone) 24%, transparent);
  }
  .title {
    min-width: 0;
  }
  .members {
    margin: 0;
    overflow: hidden;
    color: var(--color-muted);
    font-size: 11px;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-md);
    font-weight: var(--atlas-fw-bold);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .count {
    margin-left: auto;
    padding: 2px 9px;
    font: 700 11px var(--font-mono);
    color: var(--color-text);
    background: var(--color-soft);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-pill);
    white-space: nowrap;
  }
  .count.warn {
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .lock {
    display: inline-flex;
    color: var(--color-muted);
  }
  .warn-text {
    color: var(--color-danger);
    font-size: 10px;
    font-weight: 700;
    white-space: nowrap;
  }
  .body {
    position: relative;
    flex: 1;
    min-height: 0;
  }
  .addhere,
  .addbtn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    min-height: 38px;
    color: var(--color-muted);
    font-size: var(--atlas-fs-sm);
    background: transparent;
    border: 1px dashed var(--color-border-strong);
  }
  .addbtn {
    flex: none;
    margin-top: var(--space-2);
    width: 100%;
  }
  .addhere:hover:not(:disabled),
  .addbtn:hover:not(:disabled) {
    color: var(--color-accent);
    border-color: var(--color-accent);
  }
  .slot {
    height: 100%;
    padding-bottom: 8px;
  }
  /* While dragging, a merged column offers one drop row per member status, like Jira's swimlanes. */
  .subdrops {
    position: absolute;
    inset: 0;
    z-index: 2;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding-bottom: var(--space-2);
  }
  .subdrop {
    flex: 1;
    display: flex;
    align-items: flex-start;
    gap: var(--space-2);
    padding: var(--space-3);
    font-weight: var(--atlas-fw-semibold);
    background: color-mix(in srgb, var(--tone) 10%, var(--color-surface));
    border: 1px dashed var(--tone);
    border-radius: var(--atlas-radius-md);
  }
  .subdrop.hot {
    background: color-mix(in srgb, var(--tone) 28%, var(--color-surface));
    border-style: solid;
  }
  .drop {
    position: absolute;
    left: 0;
    right: 0;
    height: 4px;
    margin-top: -2px;
    border-radius: var(--atlas-radius-pill);
    background: var(--color-accent);
    box-shadow: 0 0 8px var(--color-accent);
    pointer-events: none;
  }
  .ghost {
    position: fixed;
    z-index: 60;
    max-width: 18rem;
    padding: var(--space-2) var(--space-3);
    font-size: 0.85rem;
    background: var(--atlas-surface-raised);
    border: 1px solid var(--color-accent);
    border-radius: var(--radius);
    pointer-events: none;
    box-shadow: var(--atlas-shadow-lg);
    transform: rotate(-1.5deg);
  }
  @media (max-width: 900px) {
    .toolbar {
      align-items: center;
      flex-wrap: wrap;
    }
    .filtertoggle {
      display: inline-flex;
    }
    .filters {
      display: none;
      order: 3;
      flex-basis: 100%;
    }
    .filters.open {
      display: block;
    }
    .actions {
      margin-left: auto;
    }
    .column {
      flex-basis: min(86vw, var(--atlas-column-w));
    }
  }
</style>
