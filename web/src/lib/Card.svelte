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
    <span class="chip type">{t(`types.${card.type}` as "types.epic")}</span>
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
    height: var(--atlas-card-h);
    padding: 8px 9px;
    display: grid;
    grid-template-rows: auto 1fr auto;
    gap: 2px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    box-shadow: var(--atlas-shadow-sm);
    overflow: hidden;
  }
  .card:hover {
    border-color: var(--color-border-strong);
  }
  .card.blocked {
    border-left: 4px solid var(--color-danger);
    padding-left: 6px;
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
    gap: 5px;
    min-width: 0;
  }
  header {
    min-height: 17px;
  }
  footer {
    min-height: 21px;
  }
  .key {
    font: 700 11px var(--font-mono);
    color: var(--color-muted);
    white-space: nowrap;
  }
  .title {
    margin: 0;
    font-size: 13px;
    font-weight: var(--atlas-fw-semibold);
    line-height: 1.23;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .title a {
    color: inherit;
    text-decoration: none;
  }
  .title a:hover {
    text-decoration: underline;
  }
  footer .chip {
    max-width: 73px;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .spacer {
    flex: 1;
    min-width: 0;
  }
  .handle {
    min-height: 0;
    padding: 2px 6px;
    border: none;
    background: none;
    color: var(--color-muted);
    cursor: grab;
    touch-action: none;
    line-height: 1.2;
  }
  .link {
    min-height: 0;
    padding: 2px 0;
    border: none;
    background: none;
    color: var(--color-muted);
    font-size: 0.7rem;
    font-weight: var(--atlas-fw-regular);
    text-decoration: underline;
  }
  .avatar {
    flex: none;
    display: grid;
    place-items: center;
    width: 24px;
    height: 24px;
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-bold);
    border: 1px solid var(--color-border);
    border-radius: 50%;
    background: var(--color-accent-soft);
    color: var(--color-accent-soft-text);
  }
</style>
