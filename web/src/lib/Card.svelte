<script lang="ts">
  import { type Card } from "./api";
  import { t } from "./i18n";
  import { initials, personName } from "./people.svelte";
  import { cardFields } from "./prefs.svelte";
  import { itemHref } from "./routes";

  let {
    card,
    done = false,
    showProject = false,
    readonly = false,
    dragging = false,
    onhandle,
    onkeydown,
    onarchive,
  }: {
    card: Card;
    done?: boolean;
    showProject?: boolean;
    readonly?: boolean;
    dragging?: boolean;
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
</script>

<!-- A card is focusable on purpose: Alt + arrow keys move it (the keyboard alternative to dragging). It holds buttons, so it cannot itself be role=button. -->
<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
<article
  class="card"
  class:blocked={card.blocked}
  class:archived
  class:dragging
  data-key={card.key}
  tabindex="0"
  aria-label="{card.key}: {card.title}"
  {onkeydown}
>
  <header>
    <span class="key">{card.key}</span>
    <span class="chip">{t(`types.${card.type}` as "types.epic")}</span>
    {#if showProject && card.project}<span class="chip">{card.project}</span
      >{/if}
    {#if card.blocked}<span class="chip warn">{t("board.blocked")}</span>{/if}
    {#if archived}<span class="chip">{t("board.archived")}</span>{/if}
    <span class="spacer"></span>
    {#if !readonly && onhandle}
      <button
        type="button"
        class="handle"
        tabindex="-1"
        aria-label={t("board.moveHandle", { key: card.key })}
        onpointerdown={onhandle}>⠿</button
      >
    {/if}
  </header>
  <p class="title"><a href={itemHref(card.key)}>{card.title}</a></p>
  <footer>
    {#if cardFields.priority && card.priority !== "normal"}
      <span class="chip" class:warn={card.priority === "urgent"}
        >{t(`priority.${card.priority}` as "priority.low")}</span
      >
    {/if}
    {#if cardFields.due && card.due_at !== null}
      <span class="chip" class:warn={overdue}
        >{dateFormat.format(card.due_at)}</span
      >
    {/if}
    {#if cardFields.labels}
      {#each card.labels.slice(0, 2) as label (label)}
        <span class="chip tag">{label}</span>
      {/each}
    {/if}
    <span class="spacer"></span>
    {#if !readonly && onarchive}
      <button type="button" class="link" onclick={onarchive}>
        {archived ? t("board.restore") : t("board.archive")}
      </button>
    {/if}
    {#if cardFields.assignee && card.assignee !== null}
      <span class="avatar" title={personName(card.assignee)}
        >{initials(personName(card.assignee))}</span
      >
    {/if}
  </footer>
</article>

<style>
  .card {
    height: 88px;
    padding: var(--space-2) var(--space-3);
    display: grid;
    grid-template-rows: auto 1fr auto;
    gap: var(--space-1);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    overflow: hidden;
  }
  .card.blocked {
    border-left: 3px solid var(--color-danger);
  }
  .card.archived {
    opacity: 0.6;
  }
  .card.dragging {
    opacity: 0.35;
  }
  header,
  footer {
    display: flex;
    align-items: center;
    gap: var(--space-1);
    min-width: 0;
  }
  .key {
    font-size: 0.75rem;
    color: var(--color-muted);
    font-variant-numeric: tabular-nums;
  }
  .title {
    margin: 0;
    font-size: 0.875rem;
    line-height: 1.3;
    display: -webkit-box;
    -webkit-line-clamp: 2;
    line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
  }
  .title a {
    color: inherit;
    text-decoration: none;
  }
  .title a:hover {
    text-decoration: underline;
  }
  .chip {
    padding: 0 var(--space-2);
    font-size: 0.7rem;
    line-height: 1.5;
    border: 1px solid var(--color-border);
    border-radius: 999px;
    color: var(--color-muted);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 7rem;
  }
  .chip.warn {
    color: var(--color-danger);
    border-color: var(--color-danger);
  }
  .spacer {
    flex: 1;
  }
  .handle {
    padding: 0 var(--space-2);
    border: none;
    background: none;
    color: var(--color-muted);
    cursor: grab;
    touch-action: none;
    line-height: 1.4;
  }
  .link {
    padding: 0;
    border: none;
    background: none;
    color: var(--color-muted);
    font-size: 0.7rem;
    text-decoration: underline;
  }
  .avatar {
    display: grid;
    place-items: center;
    width: 1.4rem;
    height: 1.4rem;
    font-size: 0.6rem;
    border-radius: 50%;
    background: var(--color-accent);
    color: var(--color-accent-text);
  }
</style>
