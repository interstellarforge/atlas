<script lang="ts">
  import {
    deleteNotification,
    getInbox,
    markAllNotificationsRead,
    markNotificationRead,
    type Notification,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import { actorName } from "./people.svelte";
  import { itemHref } from "./routes";
  import { inbox, refreshInbox } from "./unread.svelte";

  let items = $state<Notification[]>([]);
  let ready = $state(false);
  let error = $state("");

  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  function message(n: Notification): string {
    const params = { actor: actorName(n.actor), key: n.item, other: n.detail };
    if (n.type === "followed")
      return t(
        `inbox.followed.${n.detail}` as "inbox.followed.item.updated",
        params,
      );
    return t(`inbox.${n.type}` as "inbox.assigned", params);
  }

  async function load(): Promise<void> {
    try {
      const data = await getInbox();
      items = data.items;
      inbox.unread = data.unread;
      ready = true;
    } catch {
      error = t("inbox.loadFailed");
    }
  }

  async function run(action: () => Promise<unknown>): Promise<void> {
    error = "";
    try {
      await action();
      await load();
    } catch {
      error = t("inbox.failed");
    }
  }

  $effect(() => {
    void load();
    const offChange = onChange((event) => {
      if (event.type === "notification.created") void load();
    });
    const offResync = onResync(() => void load());
    return () => {
      offChange();
      offResync();
    };
  });
</script>

<section class="inbox">
  <div class="bar">
    <h1>{t("inbox.title")}</h1>
    <button
      type="button"
      disabled={inbox.unread === 0}
      onclick={() => void run(markAllNotificationsRead)}
      >{t("inbox.markAll")}</button
    >
  </div>
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if ready && items.length === 0}<p class="muted">{t("inbox.empty")}</p>{/if}
  <ul>
    {#each items as n (n.id)}
      <li class:unread={!n.read}>
        <div class="text">
          <a
            href={itemHref(n.item)}
            onclick={() => {
              if (!n.read) void markNotificationRead(n.id).then(refreshInbox);
            }}>{message(n)}</a
          >
          <span class="meta">{dateTime.format(n.created_at)}</span>
        </div>
        <div class="actions">
          {#if !n.read}
            <button
              type="button"
              onclick={() => void run(() => markNotificationRead(n.id))}
              >{t("inbox.markRead")}</button
            >
          {/if}
          <button
            type="button"
            onclick={() => void run(() => deleteNotification(n.id))}
            >{t("inbox.delete")}</button
          >
        </div>
      </li>
    {/each}
  </ul>
</section>

<style>
  .inbox {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    max-width: 800px;
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius-md);
  }
  li.unread {
    border-left: 4px solid var(--color-accent);
    padding-left: 12px;
  }
  .text {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  li.unread a {
    font-weight: var(--atlas-fw-bold);
  }
  .meta {
    font-size: var(--atlas-fs-xs);
    color: var(--color-muted);
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
</style>
