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
    type ItemTemplate,
    listItemTemplates,
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
  import { route } from "./router.svelte";
  import { matchProject } from "./routes";
  import { statusTone, toneVar } from "./statusTone";
  import VirtualList from "./VirtualList.svelte";

  // One card slot: the 140px card (--atlas-card-h) plus an 8px gap. Fixed heights keep windowing and drag maths simple.
  const SLOT = 148;
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

  // On a phone the filters start collapsed, so the columns get the screen.
  let filtersOpen = $state(!matchMedia("(max-width: 900px)").matches);
  let dialog = $state<HTMLDialogElement | undefined>();
  const openKey = $derived(matchProject(route.path)?.item);

  let newType = $state("task");
  let newTitle = $state("");
  let newStatus = $state("");

  let refreshTimer: ReturnType<typeof setTimeout> | undefined;
  let dirty = false;

  let itemTemplates = $state<ItemTemplate[]>([]);
  let newTemplate = $state("");

  // Choosing a template pre-fills type and status (still editable); the server adds the rest on create.
  function pickTemplate(): void {
    const tpl = itemTemplates.find((x) => x.name === newTemplate);
    if (!tpl) return;
    newType = tpl.type;
    if (
      workflow?.statuses.some((st) => st.name === tpl.status && st.allow_create)
    )
      newStatus = tpl.status;
  }

  function openNew(status?: string): void {
    if (status) newStatus = status;
    newTemplate = "";
    if (project !== undefined && (workflow?.item_templates.length ?? 0) > 0)
      void listItemTemplates(project).then((list) => (itemTemplates = list));
    dialog?.showModal();
  }

  // New cards start in the first status unless another is chosen.
  $effect(() => {
    if (!newStatus && workflow)
      newStatus = workflow.statuses.find((st) => st.allow_create)?.name ?? "";
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
        ...(newTemplate && { template: newTemplate }),
      });
      newTitle = "";
      error = "";
      dialog?.close();
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

  {#if !cross && workflow}
    <dialog class="newdialog" bind:this={dialog} aria-labelledby="new-title">
      <form class="add" onsubmit={add}>
        <h2 id="new-title">{t("board.newItem")}</h2>
        {#if itemTemplates.length > 0}
          <label>
            {t("board.template")}
            <select bind:value={newTemplate} onchange={pickTemplate}>
              <option value="">{t("board.noTemplate")}</option>
              {#each itemTemplates as tpl (tpl.id)}
                <option value={tpl.name}>{tpl.name}</option>
              {/each}
            </select>
          </label>
        {/if}
        <label>
          {t("board.type")}
          <select bind:value={newType}>
            {#each workflow.types as typ (typ)}
              <option value={typ}>{t(`types.${typ}` as "types.epic")}</option>
            {/each}
          </select>
        </label>
        <label>
          {t("board.itemTitle")}
          <input
            bind:value={newTitle}
            required
            maxlength="500"
            autocomplete="off"
          />
        </label>
        <label>
          {t("board.status")}
          <select bind:value={newStatus}>
            {#each workflow.statuses.filter((st) => st.allow_create) as st (st.id)}
              <option value={st.name}>{st.name}</option>
            {/each}
          </select>
        </label>
        <div class="buttons">
          <button type="button" onclick={() => dialog?.close()}
            >{t("board.cancel")}</button
          >
          <button class="primary" type="submit">{t("board.submit")}</button>
        </div>
      </form>
    </dialog>
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
          <h2>{column.status.name}</h2>
          {#if !cross && !column.status.allow_create}
            <span
              class="lock"
              role="img"
              title={t("board.createBlocked")}
              aria-label={t("board.createBlocked")}
              ><Icon name="lock" size={14} /></span
            >
          {/if}
          <span class="count" class:warn={column.over_wip}>
            {column.status.wip_limit
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
              {#if !cross && column.status.allow_create}
                <button
                  type="button"
                  class="addhere"
                  onclick={() => {
                    openNew(column.status.name);
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
        {#if !cross && column.items.length > 0 && column.status.allow_create}
          <button
            type="button"
            class="addbtn"
            onclick={() => {
              openNew(column.status.name);
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
  .newdialog {
    width: min(420px, calc(100vw - 32px));
    padding: 0;
    color: var(--color-text);
    background: var(--atlas-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
    box-shadow: var(--atlas-shadow-lg);
  }
  .newdialog::backdrop {
    background: var(--atlas-overlay);
  }
  .add {
    display: grid;
    gap: var(--space-3);
    padding: 20px;
  }
  .add h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  .add label {
    display: grid;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  .buttons {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
  }
  .columns {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--space-4);
    overflow-x: auto;
    scroll-snap-type: x proximity;
    padding-bottom: var(--space-2);
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
