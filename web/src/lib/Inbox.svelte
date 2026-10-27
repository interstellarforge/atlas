<script lang="ts">
  import {
    deleteNotification,
    getInbox,
    markAllNotificationsRead,
    markNotificationRead,
    type Notification,
  } from "./api";
  import Avatar from "./Avatar.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import PageHero from "./PageHero.svelte";
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

  // Notifications are listed under Today, Yesterday and Earlier; the list arrives newest first.
  const groups = $derived.by(() => {
    // eslint-disable-next-line svelte/prefer-svelte-reactivity -- a local value inside the derivation, never state
    const start = new Date();
    start.setHours(0, 0, 0, 0);
    const today = start.getTime();
    const yesterday = today - 86_400_000;
    const buckets = [
      { label: t("inbox.today"), items: [] as Notification[] },
      { label: t("inbox.yesterday"), items: [] as Notification[] },
      { label: t("inbox.earlier"), items: [] as Notification[] },
    ];
    for (const n of items)
      buckets[
        n.created_at >= today ? 0 : n.created_at >= yesterday ? 1 : 2
      ]?.items.push(n);
    return buckets.filter((b) => b.items.length > 0);
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

<PageHero title={t("inbox.title")} description={t("inbox.intro")}>
  {#snippet actions()}
    <button
      type="button"
      class="markall"
      disabled={inbox.unread === 0}
      onclick={() => void run(markAllNotificationsRead)}
      ><Icon name="check" size={16} />{t("inbox.markAll")}</button
    >
  {/snippet}
</PageHero>

<section class="inbox">
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if ready && items.length === 0}
    <div class="panel">
      <EmptyState
        icon="inbox"
        title={t("inbox.empty")}
        text={t("inbox.emptyText")}
      />
    </div>
  {/if}
  {#each groups as group (group.label)}
    <div class="panel">
      <h2>
        {group.label}
        <span class="muted"
          >{t("inbox.count", { count: group.items.length })}</span
        >
      </h2>
      <ul>
        {#each group.items as n (n.id)}
          <li class:unread={!n.read}>
            <span class="dot" aria-hidden="true"></span>
            <Avatar name={actorName(n.actor)} size={36} />
            <div class="text">
              <a
                href={itemHref(n.item)}
                onclick={() => {
                  if (!n.read)
                    void markNotificationRead(n.id).then(refreshInbox);
                }}>{message(n)}</a
              >
              <span class="meta"
                ><span class="mono">{n.item}</span> · {dateTime.format(
                  n.created_at,
                )}</span
              >
            </div>
            <div class="actions">
              {#if !n.read}
                <button
                  type="button"
                  class="iconbtn"
                  title={t("inbox.markRead")}
                  aria-label={t("inbox.markRead")}
                  onclick={() => void run(() => markNotificationRead(n.id))}
                  ><Icon name="check" size={16} /></button
                >
              {/if}
              <button
                type="button"
                class="iconbtn"
                title={t("inbox.delete")}
                aria-label={t("inbox.delete")}
                onclick={() => void run(() => deleteNotification(n.id))}
                ><Icon name="trash" size={16} /></button
              >
            </div>
          </li>
        {/each}
      </ul>
    </div>
  {/each}
</section>

<style>
  .inbox {
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
  }
  .markall {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .panel {
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
    overflow: hidden;
  }
  h2 {
    display: flex;
    justify-content: space-between;
    margin: 0;
    padding: 14px 18px;
    font-size: var(--atlas-fs-lg);
    border-bottom: 1px solid var(--color-border);
  }
  h2 .muted {
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-regular);
  }
  ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  li {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: 12px 18px;
    border-top: 1px solid
      color-mix(in srgb, var(--color-border) 60%, transparent);
  }
  li:first-child {
    border-top: none;
  }
  li:hover {
    background: var(--color-soft);
  }
  .dot {
    flex: none;
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: transparent;
  }
  li.unread .dot {
    background: var(--atlas-primary);
    box-shadow: 0 0 0 3px
      color-mix(in srgb, var(--atlas-primary) 28%, transparent);
  }
  .text {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
  }
  .text a {
    color: var(--color-text);
    text-decoration: none;
    overflow-wrap: anywhere;
  }
  .text a:hover {
    text-decoration: underline;
  }
  li.unread .text a {
    font-weight: var(--atlas-fw-bold);
  }
  .meta {
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .mono {
    font: 700 11px var(--font-mono);
  }
  .actions {
    display: flex;
    gap: 2px;
  }
  .iconbtn {
    display: inline-grid;
    place-items: center;
    width: 36px;
    min-height: 36px;
    height: 36px;
    padding: 0;
    color: var(--color-muted);
    background: transparent;
    border-color: transparent;
  }
</style>
