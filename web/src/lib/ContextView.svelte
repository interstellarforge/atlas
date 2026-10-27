<script lang="ts">
  import {
    CONTEXT_CATEGORIES,
    type ContextEntry,
    type ContextList,
    getContextList,
    getContextPreview,
    listRoles,
    type User,
  } from "./api";
  import ContextPanel from "./ContextPanel.svelte";
  import EmptyState from "./EmptyState.svelte";
  import { onChange, onResync } from "./events.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";

  let { project, role, user }: { project: string; role: string; user: User } =
    $props();

  let query = $state({ q: "", category: "", status: "" });
  let list = $state<ContextList | undefined>();
  let error = $state("");
  let actions = $state<string[]>([]);
  // "" closes the panel, "new" opens an empty draft, anything else is an entry key.
  let selected = $state("");
  let preview = $state<{ json: string; bytes: number } | undefined>();
  let previewOpen = $state(false);
  let previewError = $state("");
  let timer: ReturnType<typeof setTimeout> | undefined;

  const canWrite = $derived(actions.includes("context.write"));
  const canVerify = $derived(actions.includes("context.verify"));
  const dateFormat = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
  });

  const groups = $derived(
    CONTEXT_CATEGORIES.map((category) => ({
      category,
      entries: (list?.items ?? []).filter((e) => e.category === category),
    })).filter((g) => g.entries.length > 0),
  );
  const filtered = $derived(query.q !== "" || query.category !== "");

  async function load(): Promise<void> {
    try {
      list = await getContextList(project, query);
      error = "";
    } catch {
      error = t("context.loadFailed");
    }
  }

  async function loadPreview(): Promise<void> {
    try {
      preview = await getContextPreview(project);
      previewError = "";
    } catch {
      previewError = t("context.previewFailed");
    }
  }

  function reload(): void {
    void load();
    if (previewOpen) void loadPreview();
  }

  $effect(() => {
    void load(); // reads the project and every filter before its first await, so the effect tracks them
  });
  $effect(() => {
    const key = project;
    void listRoles()
      .then((r) => {
        if (key !== project) return;
        actions = user.is_admin
          ? r.actions
          : (r.items.find((x) => x.name === role)?.actions ?? []);
      })
      .catch(() => {
        actions = [];
      });
  });
  $effect(() =>
    onChange((event) => {
      if (event.project !== project || event.type !== "context.updated") return;
      clearTimeout(timer);
      timer = setTimeout(reload, 250);
    }),
  );
  $effect(() => onResync(reload));

  function togglePreview(): void {
    previewOpen = !previewOpen;
    if (previewOpen) void loadPreview();
  }

  const tokens = (bytes: number): number => Math.ceil(bytes / 4);
  const sourceRef = (e: ContextEntry): string => {
    const s = e.sources[0];
    return s ? (s.path ?? s.ticket ?? s.url ?? "") : "";
  };
</script>

<section class="context">
  <header class="head">
    <p class="muted intro">{t("context.intro")}</p>
    <div class="actions">
      <button type="button" aria-pressed={previewOpen} onclick={togglePreview}
        ><Icon name="eye" size={16} />{previewOpen
          ? t("context.previewHide")
          : t("context.preview")}</button
      >
      {#if canWrite}
        <button
          type="button"
          class="primary"
          onclick={() => {
            selected = "new";
          }}><Icon name="plus" size={16} />{t("context.add")}</button
        >
      {/if}
    </div>
  </header>

  {#if previewOpen}
    <div class="preview" aria-live="polite">
      <p class="muted">{t("context.previewIntro")}</p>
      {#if previewError}
        <p class="banner error" role="alert">{previewError}</p>
      {:else if preview}
        <p class="size">
          <strong
            >{t("context.previewSize", {
              bytes: preview.bytes,
              tokens: tokens(preview.bytes),
            })}</strong
          >
        </p>
        {#if list && !list.items.some((e) => e.pinned)}
          <p class="muted">{t("context.previewEmpty")}</p>
        {/if}
        <pre><code>{preview.json}</code></pre>
      {/if}
    </div>
  {/if}

  <form
    class="filters"
    onsubmit={(e) => {
      e.preventDefault();
    }}
  >
    <input
      type="search"
      aria-label={t("context.search")}
      placeholder={t("context.search")}
      bind:value={query.q}
    />
    <select aria-label={t("context.category")} bind:value={query.category}>
      <option value="">{t("context.allCategories")}</option>
      {#each CONTEXT_CATEGORIES as c (c)}
        <option value={c}>{t(`context.categories.${c}`)}</option>
      {/each}
    </select>
    <label class="check">
      <input
        type="checkbox"
        checked={query.status === "all"}
        onchange={(e) => {
          query.status = e.currentTarget.checked ? "all" : "";
        }}
      />
      {t("context.showInactive")}
    </label>
  </form>

  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if list?.warnings}
    {#each list.warnings as w (w)}<p class="banner conflict">{w}</p>{/each}
  {/if}

  {#if list}
    {#if groups.length === 0}
      <EmptyState
        icon="context"
        title={filtered ? t("context.noMatch") : t("context.empty")}
        text={filtered ? "" : t("context.emptyText")}
      />
    {:else}
      <div class="groups">
        {#each groups as g (g.category)}
          <section aria-labelledby="ctx-{g.category}">
            <h2 id="ctx-{g.category}">
              {t(`context.categories.${g.category}`)}
              <span class="count">{g.entries.length}</span>
            </h2>
            <ul>
              {#each g.entries as e (e.key)}
                <li>
                  <button
                    type="button"
                    class="entry"
                    class:inactive={e.status !== "active"}
                    onclick={() => {
                      selected = e.key;
                    }}
                  >
                    <span class="top">
                      <span class="title">{e.title}</span>
                      <span class="chip state {e.verification_state}"
                        >{t(`context.state.${e.verification_state}`)}</span
                      >
                      {#if e.status !== "active"}<span class="chip"
                          >{t(`context.status.${e.status}`)}</span
                        >{/if}
                      {#if e.pinned}<span
                          class="pin"
                          title={t("context.pinned")}
                          role="img"
                          aria-label={t("context.pinned")}
                          ><Icon name="flag" size={14} /></span
                        >{/if}
                    </span>
                    <span class="summary">{e.summary}</span>
                    <span class="meta">
                      {#if sourceRef(e)}<span class="src"
                          ><Icon name="link" size={12} />{sourceRef(e)}{e
                            .sources.length > 1
                            ? ` +${String(e.sources.length - 1)}`
                            : ""}</span
                        >{/if}
                      <span
                        >{t("context.updated", {
                          date: dateFormat.format(e.updated_at),
                        })}</span
                      >
                    </span>
                  </button>
                </li>
              {/each}
            </ul>
          </section>
        {/each}
      </div>
    {/if}
  {:else if !error}
    <p class="muted">…</p>
  {/if}

  {#if selected}
    {#key selected}
      <ContextPanel
        {project}
        entryKey={selected === "new" ? "" : selected}
        {canWrite}
        {canVerify}
        entries={list?.items ?? []}
        onsaved={reload}
        onselect={(key: string) => {
          selected = key;
        }}
        onclose={() => {
          selected = "";
        }}
      />
    {/key}
  {/if}
</section>

<style>
  .context {
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    height: 100%;
    overflow-y: auto;
    align-content: start;
  }
  .head {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .intro {
    margin: 0;
    max-width: 70ch;
  }
  .actions {
    display: flex;
    gap: var(--space-2);
  }
  .actions button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .filters {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
  }
  .filters input[type="search"] {
    flex: 1 1 220px;
  }
  .check {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-size: var(--atlas-fs-sm);
  }
  .preview {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: var(--space-2);
    padding: 16px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  .preview p {
    margin: 0;
  }
  pre {
    max-height: 320px;
    margin: 0;
    padding: 12px;
    overflow: auto;
    background: var(--color-soft);
    border-radius: var(--atlas-radius-md);
    font: 12px/1.5 var(--font-mono);
  }
  .groups {
    display: grid;
    gap: var(--space-4);
  }
  h2 {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    margin: 0 0 var(--space-2);
    font-size: var(--atlas-fs-md);
  }
  .count {
    font-size: var(--atlas-fs-xs);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  ul {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(min(100%, 340px), 1fr));
    gap: var(--space-2);
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .entry {
    display: grid;
    gap: 6px;
    width: 100%;
    height: 100%;
    padding: 14px;
    text-align: left;
    font-weight: var(--atlas-fw-regular, 400);
    background: var(--color-surface);
    border-radius: var(--atlas-radius-md);
  }
  .entry.inactive {
    opacity: 0.7;
  }
  .top {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
  }
  .title {
    flex: 1 1 auto;
    min-width: 0;
    font-weight: var(--atlas-fw-bold);
    overflow-wrap: anywhere;
  }
  .summary {
    display: -webkit-box;
    -webkit-line-clamp: 3;
    line-clamp: 3;
    -webkit-box-orient: vertical;
    overflow: hidden;
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2) var(--space-3);
    font-size: var(--atlas-fs-xs);
    color: var(--color-muted);
  }
  .src {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-family: var(--font-mono);
  }
  .pin {
    display: inline-flex;
    color: var(--color-accent);
  }
  .state.verified {
    border-color: transparent;
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .state.unverified {
    background: var(--color-soft);
  }
  .state.stale {
    border-color: transparent;
    background: var(--color-warning-soft);
    color: var(--color-warning);
  }
  @media (max-width: 720px) {
    .actions {
      width: 100%;
    }
    .actions button {
      flex: 1;
      justify-content: center;
    }
  }
</style>
