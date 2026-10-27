<script lang="ts">
  import { onMount, tick } from "svelte";

  import {
    addLink,
    ApiError,
    createItem,
    getFollowing,
    getItem,
    getLinks,
    type ItemDetail,
    type ItemTemplate,
    type Links,
    listItemTemplates,
    removeLink,
    setFollowing,
    updateItem,
    type Workflow,
  } from "./api";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import ItemAttachments from "./ItemAttachments.svelte";
  import ItemNotes from "./ItemNotes.svelte";
  import ItemTime from "./ItemTime.svelte";
  import { people } from "./people.svelte";
  import PriorityIndicator from "./PriorityIndicator.svelte";
  import { navigate } from "./router.svelte";
  import { itemHref } from "./routes";
  import StatusChip from "./StatusChip.svelte";
  import { statusTone } from "./statusTone";
  import TypeIcon from "./TypeIcon.svelte";

  // With `create` the panel is an empty draft: saving creates the item and then opens it.
  let {
    itemKey = "",
    create,
    workflow,
    onclose,
  }: {
    itemKey?: string;
    create?: { project: string; status: string; type?: string };
    workflow: Workflow;
    onclose: () => void;
  } = $props();

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
  let titleInput = $state<HTMLInputElement | undefined>();
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
  let draftType = $state("task");
  let template = $state("");
  let templates = $state<ItemTemplate[]>([]);
  const shownType = $derived(create ? draftType : (item?.type ?? ""));

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
  const currentTone = $derived.by(() => {
    const current = workflow.statuses.find((s) => s.name === form?.status);
    return current ? statusTone(current, workflow.statuses) : "todo";
  });

  function apply(it: ItemDetail): void {
    item = it;
    form = toForm(it);
    stale = false;
    error = "";
  }

  function draft(status: string): ItemDetail {
    return {
      key: "",
      type: "task",
      title: "",
      description: "",
      parent: null,
      status,
      priority: "normal",
      assignee: null,
      due_at: null,
      labels: [],
      milestone: null,
      custom_data: {},
      blocked: false,
      version: 0,
      archived_at: null,
      updated_at: Date.now(),
    };
  }

  // A template pre-fills type and status (still editable); the server adds the rest on create.
  function pickTemplate(): void {
    const tpl = templates.find((x) => x.name === template);
    if (!tpl || !form) return;
    draftType = tpl.type;
    if (
      workflow.statuses.some((st) => st.name === tpl.status && st.allow_create)
    )
      form.status = tpl.status;
  }

  async function load(): Promise<void> {
    if (create) {
      if (create.type) draftType = create.type;
      apply(draft(create.status));
      void tick().then(() => titleInput?.focus());
      if (workflow.item_templates.length > 0)
        templates = await listItemTemplates(create.project).catch(() => []);
      return;
    }
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
      if (create && form) {
        // Title, status and parent go in the create call; the rest follows as an update.
        const rest = { ...patch };
        delete rest.title;
        delete rest.status;
        delete rest.parent;
        const made = await createItem({
          project: create.project,
          type: draftType,
          title: form.title,
          status: form.status,
          ...(form.parent.trim() && { parent: form.parent.trim() }),
          ...(template && { template }),
        });
        if (Object.keys(rest).length > 0)
          await updateItem(made.key, made.version, rest);
        navigate(itemHref(made.key).slice(1));
        return;
      }
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
  <header class="bar">
    {#if item}<TypeIcon type={shownType} />{/if}
    <h2 id="item-panel-title">{create ? t("board.newItem") : itemKey}</h2>
    {#if item}
      <span class="chip type">{t(`types.${shownType}` as "types.epic")}</span>
      {#if item.archived_at !== null}<span class="chip"
          >{t("board.archived")}</span
        >{/if}
      {#if item.blocked}<span class="chip warn"
          ><Icon name="alert" size={12} />{t("board.blocked")}</span
        >{/if}
    {/if}
    <span class="spacer"></span>
    {#if item && !create}
      <button
        type="button"
        class="ghost"
        aria-pressed={following}
        onclick={() => void toggleFollow()}
        ><Icon name="eye" size={16} />{following
          ? t("item.unfollow")
          : t("item.follow")}</button
      >
    {/if}
    <button
      type="button"
      class="iconbtn"
      aria-label={t("item.close")}
      onclick={() => {
        dialog.close();
      }}><Icon name="close" /></button
    >
  </header>

  {#if loadError}
    <p class="banner error pad" role="alert">{t("item.loadFailed")}</p>
  {:else if !item || !form}
    <p class="pad muted">{t("item.loading")}</p>
  {:else}
    <form id="item-form" onsubmit={save}></form>
    <div class="scroll">
      {#if stale}
        <p class="banner conflict" role="alert">
          <Icon name="alert" size={16} />
          <span>{error || t("item.changed")}</span>
          <button type="button" onclick={() => void load()}
            >{t("item.reload")}</button
          >
        </p>
      {:else if error}
        <p class="banner error" role="alert">{error}</p>
      {:else if notice && !dirty}
        <p class="banner ok" role="status">{notice}</p>
      {/if}

      <div class="titlebar">
        <input
          class="title"
          bind:this={titleInput}
          form="item-form"
          aria-label={t("item.title")}
          placeholder={create ? t("board.itemTitle") : undefined}
          bind:value={form.title}
          required
          maxlength="500"
        />
        <div class="pills">
          <StatusChip name={form.status} tone={currentTone} />
          <PriorityIndicator priority={form.priority} />
        </div>
      </div>

      <div class="layout">
        <div class="main">
          <section class="panel" aria-labelledby="item-desc">
            <h3 id="item-desc">{t("item.description")}</h3>
            <textarea
              form="item-form"
              aria-labelledby="item-desc"
              bind:value={form.description}
              rows="7"
              placeholder={t("item.descriptionHint")}></textarea>
          </section>
          {#if !create}
            <div class="panel"><ItemAttachments {itemKey} /></div>
            {#if workflow.time_tracking}
              <div class="panel">
                <ItemTime {itemKey} enabled={workflow.time_tracking} />
              </div>
            {/if}
            <div class="panel"><ItemNotes {itemKey} /></div>
          {/if}
        </div>

        <aside class="side">
          <section class="panel" aria-labelledby="item-props">
            <h3 id="item-props">{t("item.properties")}</h3>
            <div class="props">
              {#if create}
                {#if templates.length > 0}
                  <label class="prop">
                    <span class="plabel">{t("board.template")}</span>
                    <select
                      form="item-form"
                      bind:value={template}
                      onchange={pickTemplate}
                    >
                      <option value="">{t("board.noTemplate")}</option>
                      {#each templates as tpl (tpl.id)}
                        <option value={tpl.name}>{tpl.name}</option>
                      {/each}
                    </select>
                  </label>
                {/if}
                <label class="prop">
                  <span class="plabel">{t("item.type")}</span>
                  <select form="item-form" bind:value={draftType}>
                    {#each workflow.types as typ (typ)}
                      <option value={typ}
                        >{t(`types.${typ}` as "types.epic")}</option
                      >
                    {/each}
                  </select>
                </label>
              {/if}
              <label class="prop">
                <span class="plabel">{t("item.status")}</span>
                <select form="item-form" bind:value={form.status}>
                  {#each create ? workflow.statuses.filter((st) => st.allow_create) : workflow.statuses as st (st.id)}
                    <option value={st.name}>{st.name}</option>
                  {/each}
                </select>
              </label>
              <label class="prop">
                <span class="plabel">{t("item.priority")}</span>
                <select form="item-form" bind:value={form.priority}>
                  {#each PRIORITIES as p (p)}
                    <option value={p}
                      >{t(`priority.${p}` as "priority.low")}</option
                    >
                  {/each}
                </select>
              </label>
              <label class="prop">
                <span class="plabel">{t("item.assignee")}</span>
                <select form="item-form" bind:value={form.assignee}>
                  <option value="">{t("item.unassigned")}</option>
                  {#each people.users as u (u.id)}
                    <option value={String(u.id)}>{u.name}</option>
                  {/each}
                </select>
              </label>
              <label class="prop">
                <span class="plabel">{t("item.due")}</span>
                <input form="item-form" type="date" bind:value={form.due} />
              </label>
              {#if workflow.milestones.length > 0 || form.milestone !== ""}
                <label class="prop">
                  <span class="plabel">{t("item.milestone")}</span>
                  <select form="item-form" bind:value={form.milestone}>
                    <option value="">{t("item.noMilestone")}</option>
                    {#each workflow.milestones as m (m.id)}
                      <option value={m.name}>{m.name}</option>
                    {/each}
                  </select>
                </label>
              {/if}
              <label class="prop">
                <span class="plabel">{t("item.labels")}</span>
                <input
                  form="item-form"
                  bind:value={form.labels}
                  placeholder={t("item.labelsHint")}
                />
              </label>
              <label class="prop">
                <span class="plabel">{t("item.parent")}</span>
                <input
                  form="item-form"
                  bind:value={form.parent}
                  placeholder={t("item.parentHint")}
                />
              </label>
              {#each workflow.custom_fields as def (def.name)}
                <label class="prop">
                  <span class="plabel">{def.name}</span>
                  {#if def.type === "select"}
                    <select form="item-form" bind:value={form.custom[def.name]}>
                      <option value="">{t("item.unset")}</option>
                      {#each def.options ?? [] as o (o)}<option value={o}
                          >{o}</option
                        >{/each}
                    </select>
                  {:else if def.type === "bool"}
                    <select form="item-form" bind:value={form.custom[def.name]}>
                      <option value="">{t("item.unset")}</option>
                      <option value="true">{t("item.yes")}</option>
                      <option value="false">{t("item.no")}</option>
                    </select>
                  {:else}
                    <input
                      form="item-form"
                      type={def.type === "text" ? "text" : def.type}
                      bind:value={form.custom[def.name]}
                    />
                  {/if}
                </label>
              {/each}
            </div>
            {#if !create}<p class="meta">
                {t("item.updated", { date: dateTime.format(item.updated_at) })}
              </p>{/if}
          </section>

          {#if !create}
            <section class="panel" aria-labelledby="item-links">
              <h3 id="item-links">{t("item.links")}</h3>
              {#if linkError}<p class="banner error" role="alert">
                  {linkError}
                </p>{/if}
              {#if links && links.outgoing.length + links.incoming.length > 0}
                <ul class="linklist">
                  {#each links.outgoing as l (`out-${l.type}-${l.key}`)}
                    <li>
                      <span class="dir"
                        >{t(`item.out.${l.type}` as "item.out.blocks")}</span
                      >
                      <a
                        class="lkey"
                        href="#/p/{l.key.slice(
                          0,
                          l.key.lastIndexOf('-'),
                        )}/{l.key}">{l.key}</a
                      >
                      <span class="ltitle">{l.title}</span>
                      <span class="chip">{l.status}</span>
                      <button
                        type="button"
                        class="iconbtn small"
                        aria-label={t("item.removeLink", { key: l.key })}
                        onclick={() =>
                          void changeLink(() =>
                            removeLink(itemKey, l.type, l.key),
                          )}><Icon name="close" size={14} /></button
                      >
                    </li>
                  {/each}
                  {#each links.incoming as l (`in-${l.type}-${l.key}`)}
                    <li>
                      <span class="dir"
                        >{t(`item.in.${l.type}` as "item.in.blocks")}</span
                      >
                      <a
                        class="lkey"
                        href="#/p/{l.key.slice(
                          0,
                          l.key.lastIndexOf('-'),
                        )}/{l.key}">{l.key}</a
                      >
                      <span class="ltitle">{l.title}</span>
                      <span class="chip">{l.status}</span>
                    </li>
                  {/each}
                </ul>
              {:else}
                <p class="meta">{t("item.noLinks")}</p>
              {/if}
              <form class="linkform" onsubmit={submitLink}>
                <select aria-label={t("item.linkType")} bind:value={linkType}>
                  {#each LINK_TYPES as lt (lt)}
                    <option value={lt}>{t(`item.out.${lt}`)}</option>
                  {/each}
                </select>
                <input
                  aria-label={t("item.linkTarget")}
                  placeholder={t("item.linkTarget")}
                  bind:value={linkTarget}
                />
                <button
                  class="primary"
                  type="submit"
                  disabled={!linkTarget.trim()}
                  ><Icon name="link" size={14} />{t("item.addLink")}</button
                >
              </form>
            </section>
          {/if}
        </aside>
      </div>
    </div>

    <footer class="foot">
      {#if !create}
        <button type="button" disabled={dirty} onclick={toggleArchive}>
          <Icon
            name={item.archived_at === null ? "archive" : "restore"}
            size={16}
          />
          {item.archived_at === null ? t("item.archive") : t("item.restore")}
        </button>
      {/if}
      <span class="spacer"></span>
      {#if dirty}<span class="unsaved">{t("item.unsaved")}</span>{/if}
      <button
        type="button"
        disabled={!dirty}
        onclick={() => {
          if (item) apply(item);
        }}>{t("item.discard")}</button
      >
      <button
        class="primary"
        type="submit"
        form="item-form"
        disabled={!dirty || saving}
      >
        {saving
          ? t("item.saving")
          : create
            ? t("board.submit")
            : t("item.save")}
      </button>
    </footer>
  {/if}
</dialog>

<style>
  dialog {
    margin: 0 0 0 auto;
    height: 100dvh;
    max-height: 100dvh;
    width: min(var(--atlas-panel-w), 100vw);
    max-width: 100vw;
    padding: 0;
    background: var(--color-bg);
    color: var(--color-text);
    border: none;
    border-left: 1px solid var(--color-border);
    box-shadow: var(--atlas-shadow-lg);
  }
  dialog[open] {
    display: flex;
    flex-direction: column;
  }
  dialog::backdrop {
    background: var(--atlas-overlay);
  }
  .bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 12px 20px;
    background: var(--atlas-sidebar);
    border-bottom: 1px solid var(--color-border);
  }
  h2 {
    margin: 0;
    font: 700 var(--atlas-fs-md) var(--font-mono);
  }
  .spacer {
    flex: 1;
  }
  .ghost {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-height: 34px;
    background: transparent;
  }
  .iconbtn {
    display: inline-grid;
    place-items: center;
    width: 36px;
    min-height: 36px;
    height: 36px;
    padding: 0;
    background: transparent;
    border-color: transparent;
    color: var(--color-muted);
  }
  .iconbtn.small {
    width: 28px;
    min-height: 28px;
    height: 28px;
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: var(--space-4);
    padding: 20px;
  }
  .pad {
    margin: 20px;
  }
  .titlebar {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .title {
    flex: 1 1 18rem;
    min-width: 0;
    padding: 6px 10px;
    font-size: 24px;
    font-weight: var(--atlas-fw-bold);
    letter-spacing: -0.02em;
    line-height: 1.25;
    background: transparent;
    border: 1px solid transparent;
  }
  .title:hover {
    border-color: var(--color-border);
  }
  .title:focus {
    background: var(--color-surface);
    border-color: var(--color-accent);
  }
  .pills {
    display: flex;
    align-items: center;
    gap: var(--space-3);
  }
  .layout {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 280px;
    gap: var(--space-4);
    align-items: start;
  }
  .main,
  .side {
    display: grid;
    gap: var(--space-4);
    min-width: 0;
  }
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 16px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  h3 {
    margin: 0;
    font-size: var(--atlas-fs-md);
    font-weight: var(--atlas-fw-bold);
  }
  textarea {
    resize: vertical;
    line-height: 1.55;
    background: var(--color-bg);
  }
  .props {
    display: grid;
    gap: 6px;
  }
  .prop {
    display: grid;
    grid-template-columns: 84px minmax(0, 1fr);
    align-items: center;
    gap: var(--space-2);
  }
  .plabel {
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  .prop select,
  .prop input {
    min-height: 34px;
    padding-top: 4px;
    padding-bottom: 4px;
  }
  .meta {
    margin: 0;
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .linklist {
    margin: 0;
    padding: 0;
    list-style: none;
    display: grid;
    gap: 6px;
  }
  .linklist li {
    display: grid;
    grid-template-columns: auto auto minmax(0, 1fr) auto auto;
    align-items: center;
    gap: 6px;
    padding: 6px 8px;
    font-size: var(--atlas-fs-sm);
    background: var(--color-soft);
    border-radius: var(--atlas-radius-sm);
  }
  .dir {
    color: var(--color-muted);
    font-size: var(--atlas-fs-xs);
    text-transform: uppercase;
    letter-spacing: 0.04em;
  }
  .lkey {
    font: 700 11px var(--font-mono);
  }
  .ltitle {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .linkform {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: var(--space-2);
  }
  .linkform button {
    grid-column: 1 / -1;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
  }
  .banner {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .banner.conflict {
    border-left: 4px solid currentcolor;
  }
  .banner.conflict span {
    flex: 1;
  }
  .foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: 12px 20px;
    background: var(--atlas-sidebar);
    border-top: 1px solid var(--color-border);
  }
  .foot button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .unsaved {
    font-size: var(--atlas-fs-sm);
    color: var(--color-warning);
    font-weight: var(--atlas-fw-semibold);
  }
  @media (max-width: 720px) {
    .title {
      font-size: 20px;
    }
    dialog {
      width: 100vw;
      border-left: none;
    }
    .layout {
      grid-template-columns: minmax(0, 1fr);
    }
    .side {
      order: -1;
    }
    .scroll {
      padding: 14px 12px;
    }
    .bar,
    .foot {
      padding: 10px 12px;
    }
  }
</style>
