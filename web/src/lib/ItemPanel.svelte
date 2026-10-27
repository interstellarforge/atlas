<script lang="ts">
  import { onMount } from "svelte";

  import {
    addLink,
    ApiError,
    getFollowing,
    getItem,
    getLinks,
    type ItemDetail,
    type Links,
    removeLink,
    setFollowing,
    updateItem,
    type Workflow,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import ItemAttachments from "./ItemAttachments.svelte";
  import ItemNotes from "./ItemNotes.svelte";
  import { people } from "./people.svelte";

  let {
    itemKey,
    workflow,
    onclose,
  }: { itemKey: string; workflow: Workflow; onclose: () => void } = $props();

  const PRIORITIES = ["low", "normal", "high", "urgent"];
  const LINK_TYPES = ["blocks", "relates", "duplicates"] as const;

  // The editable copy of an item. Everything is a string so it binds to plain inputs.
  interface Form {
    title: string;
    description: string;
    status: string;
    priority: string;
    assignee: string;
    due: string;
    labels: string;
    milestone: string;
    parent: string;
    custom: Record<string, string>;
  }

  let dialog: HTMLDialogElement;
  let item = $state<ItemDetail | undefined>();
  let links = $state<Links | undefined>();
  let form = $state<Form | undefined>();
  let loadError = $state(false);
  let error = $state("");
  let notice = $state("");
  let stale = $state(false);
  let saving = $state(false);
  let linkType = $state<(typeof LINK_TYPES)[number]>("blocks");
  let linkTarget = $state("");
  let linkError = $state("");
  let following = $state(false);

  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  function toDateInput(ms: number | null): string {
    if (ms === null) return "";
    const d = new Date(ms);
    const pad = (n: number): string => String(n).padStart(2, "0");
    return `${String(d.getFullYear())}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
  }

  function fromDateInput(value: string): number | null {
    if (!value) return null;
    const [y, m, d] = value.split("-").map(Number);
    return new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).getTime();
  }

  function customString(value: unknown): string {
    if (typeof value === "string") return value;
    if (typeof value === "number" || typeof value === "boolean")
      return String(value);
    return "";
  }

  function toForm(it: ItemDetail): Form {
    return {
      title: it.title,
      description: it.description,
      status: it.status,
      priority: it.priority,
      assignee: it.assignee === null ? "" : String(it.assignee),
      due: toDateInput(it.due_at),
      labels: it.labels.join(", "),
      milestone: it.milestone ?? "",
      parent: it.parent ?? "",
      custom: Object.fromEntries(
        workflow.custom_fields.map((f) => [
          f.name,
          customString(it.custom_data[f.name]),
        ]),
      ),
    };
  }

  function splitLabels(text: string): string[] {
    return text
      .split(",")
      .map((l) => l.trim())
      .filter(Boolean);
  }

  function customValue(type: string, value: string): unknown {
    if (value === "") return null; // null removes the key
    if (type === "number") return Number(value);
    if (type === "bool") return value === "true";
    return value;
  }

  // Only the fields the user changed, so a save never overwrites someone else's edit of another field.
  type Patch = Partial<{
    title: string;
    description: string;
    status: string;
    priority: string;
    assignee: number | null;
    due_at: number | null;
    labels: string[];
    milestone: string | null;
    parent: string | null;
    custom_data: Record<string, unknown>;
  }>;

  function diff(it: ItemDetail, f: Form): Patch {
    const patch: Patch = {};
    if (f.title !== it.title) patch.title = f.title;
    if (f.description !== it.description) patch.description = f.description;
    if (f.status !== it.status) patch.status = f.status;
    if (f.priority !== it.priority) patch.priority = f.priority;
    if (f.assignee !== (it.assignee === null ? "" : String(it.assignee)))
      patch.assignee = f.assignee === "" ? null : Number(f.assignee);
    if (f.due !== toDateInput(it.due_at)) patch.due_at = fromDateInput(f.due);
    if (splitLabels(f.labels).join("\n") !== it.labels.join("\n"))
      patch.labels = splitLabels(f.labels);
    if (f.milestone !== (it.milestone ?? ""))
      patch.milestone = f.milestone === "" ? null : f.milestone;
    if (f.parent.trim() !== (it.parent ?? ""))
      patch.parent = f.parent.trim() === "" ? null : f.parent.trim();
    const custom: Record<string, unknown> = {};
    for (const def of workflow.custom_fields) {
      const now = f.custom[def.name] ?? "";
      if (now !== customString(it.custom_data[def.name]))
        custom[def.name] = customValue(def.type, now);
    }
    if (Object.keys(custom).length > 0) patch.custom_data = custom;
    return patch;
  }

  const patch = $derived(item && form ? diff(item, form) : {});
  const dirty = $derived(Object.keys(patch).length > 0);

  function apply(it: ItemDetail): void {
    item = it;
    form = toForm(it);
    stale = false;
    error = "";
  }

  async function load(): Promise<void> {
    try {
      const [it, l, f] = await Promise.all([
        getItem(itemKey),
        getLinks(itemKey),
        getFollowing(itemKey),
      ]);
      links = l;
      following = f;
      apply(it);
      loadError = false;
    } catch {
      loadError = true;
    }
  }

  async function toggleFollow(): Promise<void> {
    error = "";
    try {
      await setFollowing(itemKey, !following);
      following = !following;
    } catch (e) {
      error = t("item.saveFailed", {
        message: e instanceof Error ? e.message : "",
      });
    }
  }

  async function reloadLinks(): Promise<void> {
    try {
      links = await getLinks(itemKey);
    } catch {
      /* the next event or reload refreshes it */
    }
  }

  async function save(event: Event): Promise<void> {
    event.preventDefault();
    if (!item || !dirty || saving) return;
    saving = true;
    error = "";
    notice = "";
    try {
      apply(await updateItem(item.key, item.version, patch));
      notice = t("item.saved");
    } catch (e) {
      if (e instanceof ApiError && e.status === 409) {
        stale = true;
        error = t("item.conflict");
      } else {
        error = t("item.saveFailed", {
          message: e instanceof Error ? e.message : "",
        });
      }
    } finally {
      saving = false;
    }
  }

  async function toggleArchive(): Promise<void> {
    if (!item || dirty) return;
    error = "";
    try {
      apply(
        await updateItem(item.key, item.version, {
          archived: item.archived_at === null,
        }),
      );
    } catch (e) {
      stale = e instanceof ApiError && e.status === 409;
      error = t(stale ? "item.conflict" : "item.saveFailed", {
        message: e instanceof Error ? e.message : "",
      });
    }
  }

  async function changeLink(run: () => Promise<unknown>): Promise<void> {
    linkError = "";
    try {
      await run();
      await reloadLinks();
    } catch (e) {
      linkError = t("item.linkFailed", {
        message: e instanceof Error ? e.message : "",
      });
    }
  }

  function submitLink(event: Event): void {
    event.preventDefault();
    const target = linkTarget.trim().toUpperCase();
    if (!target) return;
    void changeLink(async () => {
      await addLink(itemKey, linkType, target);
      linkTarget = "";
    });
  }

  onMount(() => {
    dialog.showModal();
    void load();
    // Edits in progress are never overwritten: when the item changes elsewhere the user decides.
    const offChange = onChange((event) => {
      if (event.key !== itemKey) return;
      // Checklist and comments reload themselves and never conflict with the form.
      if (
        event.type === "checklist.changed" ||
        event.type === "comment.added" ||
        event.type === "attachment.added"
      )
        return;
      if (dirty) {
        stale = true;
        return;
      }
      void load();
    });
    const offResync = onResync(() => {
      if (dirty) stale = true;
      else void load();
    });
    return () => {
      offChange();
      offResync();
    };
  });

  function backdropClick(event: MouseEvent): void {
    if (event.target === dialog) dialog.close();
  }
</script>

<!-- The native dialog gives focus trapping, Esc to close and focus return for free. Clicking the backdrop is a pointer convenience on top of that. -->
<dialog
  bind:this={dialog}
  aria-labelledby="item-panel-title"
  {onclose}
  onclick={backdropClick}
>
  <header>
    <h2 id="item-panel-title">{itemKey}</h2>
    {#if item}
      <span class="chip type">{t(`types.${item.type}` as "types.epic")}</span>
      {#if item.archived_at !== null}<span class="chip"
          >{t("board.archived")}</span
        >{/if}
      {#if item.blocked}<span class="chip warn">{t("board.blocked")}</span>{/if}
    {/if}
    <span class="spacer"></span>
    {#if item}
      <button
        type="button"
        aria-pressed={following}
        onclick={() => void toggleFollow()}
        >{following ? t("item.unfollow") : t("item.follow")}</button
      >
    {/if}
    <button
      type="button"
      onclick={() => {
        dialog.close();
      }}>{t("item.close")}</button
    >
  </header>

  {#if loadError}
    <p class="banner error" role="alert">{t("item.loadFailed")}</p>
  {:else if !item || !form}
    <p>{t("item.loading")}</p>
  {:else}
    <form onsubmit={save}>
      {#if stale}
        <p class="banner conflict" role="alert">
          {error || t("item.changed")}
          <button type="button" onclick={() => void load()}
            >{t("item.reload")}</button
          >
        </p>
      {:else if error}
        <p class="banner error" role="alert">{error}</p>
      {:else if notice && !dirty}
        <p class="banner ok" role="status">{notice}</p>
      {/if}

      <label>
        {t("item.title")}
        <input bind:value={form.title} required maxlength="500" />
      </label>
      <label>
        {t("item.description")}
        <textarea bind:value={form.description} rows="6"></textarea>
      </label>
      <div class="row">
        <label>
          {t("item.status")}
          <select bind:value={form.status}>
            {#each workflow.statuses as st (st.id)}
              <option value={st.name}>{st.name}</option>
            {/each}
          </select>
        </label>
        <label>
          {t("item.priority")}
          <select bind:value={form.priority}>
            {#each PRIORITIES as p (p)}
              <option value={p}>{t(`priority.${p}` as "priority.low")}</option>
            {/each}
          </select>
        </label>
      </div>
      <div class="row">
        <label>
          {t("item.assignee")}
          <select bind:value={form.assignee}>
            <option value="">{t("item.unassigned")}</option>
            {#each people.users as u (u.id)}
              <option value={String(u.id)}>{u.name}</option>
            {/each}
          </select>
        </label>
        <label>
          {t("item.due")}
          <input type="date" bind:value={form.due} />
        </label>
      </div>
      {#if workflow.milestones.length > 0 || form.milestone !== ""}
        <label>
          {t("item.milestone")}
          <select bind:value={form.milestone}>
            <option value="">{t("item.noMilestone")}</option>
            {#each workflow.milestones as m (m.id)}
              <option value={m.name}>{m.name}</option>
            {/each}
          </select>
        </label>
      {/if}
      <label>
        {t("item.labels")}
        <input bind:value={form.labels} placeholder={t("item.labelsHint")} />
      </label>
      <label>
        {t("item.parent")}
        <input bind:value={form.parent} placeholder={t("item.parentHint")} />
      </label>

      {#if workflow.custom_fields.length > 0}
        <fieldset>
          <legend>{t("item.customFields")}</legend>
          {#each workflow.custom_fields as def (def.name)}
            <label>
              {def.name}
              {#if def.type === "select"}
                <select bind:value={form.custom[def.name]}>
                  <option value="">{t("item.unset")}</option>
                  {#each def.options ?? [] as o (o)}<option value={o}
                      >{o}</option
                    >{/each}
                </select>
              {:else if def.type === "bool"}
                <select bind:value={form.custom[def.name]}>
                  <option value="">{t("item.unset")}</option>
                  <option value="true">{t("item.yes")}</option>
                  <option value="false">{t("item.no")}</option>
                </select>
              {:else}
                <input
                  type={def.type === "text" ? "text" : def.type}
                  bind:value={form.custom[def.name]}
                />
              {/if}
            </label>
          {/each}
        </fieldset>
      {/if}

      <div class="actions">
        <button class="primary" type="submit" disabled={!dirty || saving}>
          {saving ? t("item.saving") : t("item.save")}
        </button>
        <button
          type="button"
          disabled={!dirty}
          onclick={() => {
            if (item) apply(item);
          }}>{t("item.discard")}</button
        >
        <span class="spacer"></span>
        <button type="button" disabled={dirty} onclick={toggleArchive}>
          {item.archived_at === null ? t("item.archive") : t("item.restore")}
        </button>
      </div>
      <p class="meta">
        {t("item.updated", { date: dateTime.format(item.updated_at) })}
      </p>
    </form>

    <section aria-labelledby="item-links">
      <h3 id="item-links">{t("item.links")}</h3>
      {#if linkError}<p class="banner error" role="alert">{linkError}</p>{/if}
      {#if links && links.outgoing.length + links.incoming.length > 0}
        <ul>
          {#each links.outgoing as l (`out-${l.type}-${l.key}`)}
            <li>
              {t(`item.out.${l.type}` as "item.out.blocks")}
              <a href="#/p/{l.key.slice(0, l.key.lastIndexOf('-'))}/{l.key}"
                >{l.key}</a
              >
              {l.title} <span class="chip">{l.status}</span>
              <button
                type="button"
                aria-label={t("item.removeLink", { key: l.key })}
                onclick={() =>
                  void changeLink(() => removeLink(itemKey, l.type, l.key))}
                >×</button
              >
            </li>
          {/each}
          {#each links.incoming as l (`in-${l.type}-${l.key}`)}
            <li>
              {t(`item.in.${l.type}` as "item.in.blocks")}
              <a href="#/p/{l.key.slice(0, l.key.lastIndexOf('-'))}/{l.key}"
                >{l.key}</a
              >
              {l.title} <span class="chip">{l.status}</span>
            </li>
          {/each}
        </ul>
      {:else}
        <p class="meta">{t("item.noLinks")}</p>
      {/if}
      <form class="row" onsubmit={submitLink}>
        <label>
          {t("item.linkType")}
          <select bind:value={linkType}>
            {#each LINK_TYPES as lt (lt)}
              <option value={lt}>{t(`item.out.${lt}`)}</option>
            {/each}
          </select>
        </label>
        <label>
          {t("item.linkTarget")}
          <input bind:value={linkTarget} />
        </label>
        <button class="primary" type="submit" disabled={!linkTarget.trim()}
          >{t("item.addLink")}</button
        >
      </form>
    </section>
    <ItemAttachments {itemKey} />
    <ItemNotes {itemKey} />
  {/if}
</dialog>

<style>
  dialog {
    margin: 0 0 0 auto;
    height: 100dvh;
    max-height: 100dvh;
    width: min(var(--atlas-panel-w), 100vw);
    max-width: 100vw;
    padding: 20px 24px;
    background: var(--color-surface);
    color: var(--color-text);
    border: none;
    border-left: 1px solid var(--color-border);
    box-shadow: var(--atlas-shadow-lg);
    overflow-y: auto;
  }
  dialog::backdrop {
    background: var(--atlas-overlay);
  }
  header,
  .actions,
  .row {
    display: flex;
    flex-wrap: wrap;
    align-items: end;
    gap: var(--space-2);
  }
  header {
    align-items: center;
    margin-bottom: var(--space-3);
  }
  h2 {
    margin: 0;
    font: 700 var(--atlas-fs-md) var(--font-mono);
  }
  h3 {
    margin: 0;
    padding-bottom: 7px;
    border-bottom: 1px solid var(--color-border);
    font-size: 13px;
    font-weight: var(--atlas-fw-bold);
  }
  form {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  label {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
    flex: 1;
    min-width: 8rem;
  }
  fieldset {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  textarea {
    resize: vertical;
  }
  section {
    margin-top: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  ul {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .spacer {
    flex: 1;
  }
  .meta {
    margin: 0;
    font-size: 0.75rem;
    color: var(--color-muted);
  }
  .error,
  .ok {
    margin: 0;
  }
</style>
