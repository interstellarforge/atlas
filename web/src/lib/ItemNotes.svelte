<script lang="ts">
  import { onMount } from "svelte";

  import {
    addChecklistItem,
    addComment,
    type ChecklistItem,
    type Comment,
    getChecklist,
    getComments,
    removeChecklistItem,
    updateChecklistItem,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import { renderMarkdown } from "./markdown";
  import { personName } from "./people.svelte";

  let { itemKey }: { itemKey: string } = $props();

  let checklist = $state<ChecklistItem[]>([]);
  let comments = $state<Comment[]>([]);
  let newItem = $state("");
  let newComment = $state("");
  let checklistError = $state("");
  let commentError = $state("");

  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });
  const done = $derived(checklist.filter((c) => c.done).length);

  // human:user-4 shows the person's name; a token is an agent.
  function actorName(actor: string): string {
    const [kind, id] = actor.split(/:\w+-/);
    if (kind === "human") return personName(Number(id));
    return t("item.agentToken", { id: id ?? "" });
  }

  async function load(): Promise<void> {
    try {
      [checklist, comments] = await Promise.all([
        getChecklist(itemKey),
        getComments(itemKey),
      ]);
    } catch {
      /* the panel itself reports a failed load; the next event retries */
    }
  }

  function message(e: unknown): string {
    return e instanceof Error ? e.message : "";
  }

  async function checklistChange(run: () => Promise<unknown>): Promise<void> {
    checklistError = "";
    try {
      await run();
      checklist = await getChecklist(itemKey);
    } catch (e) {
      checklistError = t("item.checklistFailed", { message: message(e) });
    }
  }

  function submitItem(event: Event): void {
    event.preventDefault();
    const text = newItem.trim();
    if (!text) return;
    void checklistChange(async () => {
      await addChecklistItem(itemKey, text);
      newItem = "";
    });
  }

  async function submitComment(event: Event): Promise<void> {
    event.preventDefault();
    const body = newComment.trim();
    if (!body) return;
    commentError = "";
    try {
      await addComment(itemKey, body);
      newComment = "";
      comments = await getComments(itemKey);
    } catch (e) {
      commentError = t("item.commentFailed", { message: message(e) });
    }
  }

  onMount(() => {
    void load();
    const offChange = onChange((event) => {
      if (event.key !== itemKey) return;
      if (event.type === "checklist.changed" || event.type === "comment.added")
        void load();
    });
    const offResync = onResync(() => void load());
    return () => {
      offChange();
      offResync();
    };
  });
</script>

<section aria-labelledby="item-checklist">
  <h3 id="item-checklist">
    {t("item.checklist")}{#if checklist.length > 0}
      <span class="count">{done}/{checklist.length}</span>{/if}
  </h3>
  {#if checklistError}<p class="banner error" role="alert">{checklistError}</p>{/if}
  {#if checklist.length === 0}
    <p class="meta">{t("item.noChecklist")}</p>
  {:else}
    <ul>
      {#each checklist as c (c.id)}
        <li>
          <label class="check">
            <input
              type="checkbox"
              checked={c.done}
              onchange={(e) =>
                void checklistChange(() =>
                  updateChecklistItem(itemKey, c.id, {
                    done: e.currentTarget.checked,
                  }),
                )}
            />
            <span class:done={c.done}>{c.text}</span>
          </label>
          <button
            type="button"
            aria-label={t("item.checklistRemove", { text: c.text })}
            onclick={() =>
              void checklistChange(() => removeChecklistItem(itemKey, c.id))}
            >×</button
          >
        </li>
      {/each}
    </ul>
  {/if}
  <form class="row" onsubmit={submitItem}>
    <label>
      {t("item.checklistAdd")}
      <input bind:value={newItem} maxlength="500" />
    </label>
    <button class="primary" type="submit" disabled={!newItem.trim()}
      >{t("item.checklistAddButton")}</button
    >
  </form>
</section>

<section aria-labelledby="item-comments">
  <h3 id="item-comments">{t("item.comments")}</h3>
  {#if comments.length === 0}
    <p class="meta">{t("item.noComments")}</p>
  {:else}
    <ul class="comments">
      {#each comments as c (c.id)}
        <li>
          <p class="meta">
            <strong>{actorName(c.actor)}</strong> · {dateTime.format(
              c.created_at,
            )}
          </p>
          <!-- Safe: markdown-it runs with raw HTML off (see markdown.ts). -->
          <!-- eslint-disable-next-line svelte/no-at-html-tags -->
          <div class="body">{@html renderMarkdown(c.body)}</div>
        </li>
      {/each}
    </ul>
  {/if}
  {#if commentError}<p class="banner error" role="alert">{commentError}</p>{/if}
  <form onsubmit={submitComment}>
    <label>
      {t("item.commentLabel")}
      <textarea bind:value={newComment} rows="3" maxlength="10000"></textarea>
    </label>
    <button class="primary" type="submit" disabled={!newComment.trim()}
      >{t("item.commentSubmit")}</button
    >
  </form>
</section>

<style>
  section {
    margin-top: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  h3 {
    margin: 0;
    padding-bottom: 7px;
    border-bottom: 1px solid var(--color-border);
    font-size: 13px;
    font-weight: var(--atlas-fw-bold);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .comments li {
    flex-direction: column;
    align-items: stretch;
    gap: var(--space-1);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    align-items: flex-start;
  }
  form.row {
    flex-direction: row;
    align-items: end;
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: 0.8rem;
    color: var(--color-muted);
    flex: 1;
    width: 100%;
  }
  .check {
    flex-direction: row;
    align-items: center;
    font-size: 0.9rem;
    color: var(--color-text);
  }
  .check input {
    width: auto;
  }
  .done {
    text-decoration: line-through;
    color: var(--color-muted);
  }
  .count,
  .meta {
    font-size: 0.75rem;
    color: var(--color-muted);
    margin: 0;
  }
  .body :global(p) {
    margin: 0 0 var(--space-1);
  }
  .body :global(pre) {
    overflow-x: auto;
  }
</style>
