<script lang="ts">
  import { type Card } from "./api";
  import Avatar from "./Avatar.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { personName } from "./people.svelte";
  import { cardFields } from "./prefs.svelte";
  import PriorityIndicator from "./PriorityIndicator.svelte";
  import { itemHref } from "./routes";
  import TypeIcon from "./TypeIcon.svelte";

  let {
    card,
    done = false,
    showProject = false,
    readonly = false,
    dragging = false,
    selected = false,
    onhandle,
    onkeydown,
    onarchive,
  }: {
    card: Card;
    done?: boolean;
    showProject?: boolean;
    readonly?: boolean;
    dragging?: boolean;
    selected?: boolean;
    onhandle?: (event: PointerEvent) => void;
    onkeydown?: (event: KeyboardEvent) => void;
    onarchive?: () => void;
  } = $props();

  const dateFormat = new Intl.DateTimeFormat(undefined, {
    month: "short",
    day: "numeric",
  });
  const overdue = $derived(
    card.due_at !== null && !done && card.due_at < Date.now(),
  );
  const archived = $derived(Boolean(card.archived_at));

  // The whole card drags with mouse or pen. Touch keeps the handle so the column can still scroll.
  function startFromCard(event: PointerEvent): void {
    if (readonly || !onhandle || event.pointerType === "touch") return;
    if ((event.target as Element).closest("button, a")) return;
    onhandle(event);
  }
</script>

<!-- A card is focusable on purpose: Alt + arrow keys move it (the keyboard alternative to dragging). It holds buttons, so it cannot itself be role=button. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<article
  class="card"
  class:blocked={card.blocked}
  class:archived
  class:dragging
  class:selected
  data-key={card.key}
  tabindex="0"
  aria-label="{card.key}: {card.title}"
  aria-current={selected ? "true" : undefined}
  {onkeydown}
  onpointerdown={startFromCard}
>
  <header>
    <TypeIcon type={card.type} size={14} />
    <span class="key">{card.key}</span>
    {#if showProject && card.project}<span class="chip">{card.project}</span
      >{/if}
    {#if card.blocked}<span class="chip warn"
        ><Icon name="alert" size={12} />{t("board.blocked")}</span
      >{/if}
    {#if archived}<span class="chip">{t("board.archived")}</span>{/if}
    <span class="spacer"></span>
    {#if !readonly && onhandle}
      <button
        type="button"
        class="handle"
        tabindex="-1"
        aria-label={t("board.moveHandle", { key: card.key })}
        onpointerdown={onhandle}><Icon name="drag" size={16} /></button
      >
    {/if}
  </header>
  <p class="title"><a href={itemHref(card.key)}>{card.title}</a></p>
  <div class="labels">
    {#if cardFields.labels}
      {#each card.labels.slice(0, 2) as label (label)}
        <span class="chip tag">{label}</span>
      {/each}
    {/if}
  </div>
  <footer>
    {#if cardFields.priority}
      <PriorityIndicator priority={card.priority} />
    {/if}
    {#if cardFields.due && card.due_at !== null}
      <span class="due" class:late={overdue}>
        <Icon name="calendar" size={13} />{dateFormat.format(card.due_at)}
      </span>
    {/if}
    <span class="spacer"></span>
    {#if !readonly && onarchive}
      <button
        type="button"
        class="action"
        title={archived ? t("board.restore") : t("board.archive")}
        aria-label={archived ? t("board.restore") : t("board.archive")}
        onclick={onarchive}
        ><Icon name={archived ? "restore" : "archive"} size={15} /></button
      >
    {/if}
    {#if cardFields.assignee && card.assignee !== null}
      <Avatar
        name={personName(card.assignee)}
        size={24}
        userId={card.assignee}
      />
    {/if}
  </footer>
</article>

<style>
  .card {
    position: relative;
    height: var(--atlas-card-h);
    padding: 8px 12px;
    display: grid;
    grid-template-rows: auto auto 1fr auto;
    gap: 3px;
    background: var(--atlas-surface-raised);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
    box-shadow: var(--atlas-shadow-sm);
    overflow: hidden;
    cursor: grab;
    transition:
      border-color var(--atlas-duration) var(--atlas-ease),
      box-shadow var(--atlas-duration) var(--atlas-ease);
  }
  .card:hover {
    border-color: var(--color-border-strong);
    box-shadow: var(--atlas-shadow-md);
  }
  .card.selected {
    border-color: var(--color-accent);
    box-shadow:
      0 0 0 1px var(--color-accent),
      var(--atlas-shadow-md);
  }
  .card.blocked::before {
    content: "";
    position: absolute;
    inset: 0 auto 0 0;
    width: 3px;
    background: var(--color-danger);
  }
  .card.archived {
    opacity: 0.62;
    border-style: dashed;
  }
  .card.dragging {
    opacity: 0.35;
    border-style: dashed;
  }
  header,
  footer {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
  }
  header {
    min-height: 22px;
  }
  footer {
    min-height: 28px;
  }
  .key {
    font: 700 11px var(--font-mono);
    color: var(--color-muted);
    white-space: nowrap;
  }
  .chip.warn {
    gap: 3px;
  }
  .title {
    margin: 0;
    font-size: 13.5px;
    font-weight: var(--atlas-fw-semibold);
    line-height: 1.3;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .title a {
    color: var(--color-text);
    text-decoration: none;
  }
  .title a:hover {
    text-decoration: underline;
  }
  .labels {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
  }
  .labels .chip {
    max-width: 48%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .spacer {
    flex: 1;
    min-width: 0;
  }
  .due {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
    white-space: nowrap;
  }
  .due.late {
    color: var(--color-danger);
    font-weight: var(--atlas-fw-semibold);
  }
  .handle,
  .action {
    display: inline-grid;
    place-items: center;
    width: 28px;
    min-height: 28px;
    height: 28px;
    padding: 0;
    border: none;
    background: none;
    color: var(--color-muted);
  }
  .handle {
    cursor: grab;
    touch-action: none;
  }
  .handle:hover:not(:disabled),
  .action:hover:not(:disabled) {
    color: var(--color-text);
    background: var(--color-soft);
  }
</style>
