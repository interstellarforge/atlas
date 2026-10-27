<script lang="ts">
  import { onMount } from "svelte";

  import {
    ApiError,
    archiveContext,
    CONTEXT_CATEGORIES,
    CONTEXT_SOURCE_TYPES,
    type ContextCategory,
    type ContextDraft,
    type ContextEntry,
    type ContextSource,
    type ContextSourceType,
    createContext,
    getContextEntry,
    supersedeContext,
    updateContext,
    verifyContext,
  } from "./api";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { renderMarkdown } from "./markdown";

  let {
    project,
    entryKey,
    canWrite,
    canVerify,
    entries,
    onsaved,
    onselect,
    onclose,
  }: {
    project: string;
    /** Empty for a new entry. */
    entryKey: string;
    canWrite: boolean;
    canVerify: boolean;
    entries: ContextEntry[];
    onsaved: () => void;
    onselect: (key: string) => void;
    onclose: () => void;
  } = $props();

  // A source as the form edits it: one text field per type, plus what the form does not show but must keep.
  interface FormSource {
    type: ContextSourceType;
    path: string;
    ticket: string;
    url: string;
    note: string;
    keep: Pick<ContextSource, "repository" | "commit_sha" | "content_hash">;
  }
  interface Form {
    category: ContextCategory;
    title: string;
    summary: string;
    body: string;
    pinned: boolean;
    sources: FormSource[];
  }

  const SUMMARY_MAX = 1000;
  const dateTime = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  let dialog: HTMLDialogElement;
  let entry = $state<ContextEntry | undefined>();
  let form = $state<Form>(emptyForm());
  let editing = $state(false);
  let loadError = $state(false);
  let saving = $state(false);
  let error = $state("");
  let notice = $state("");
  let conflict = $state<number | undefined>();
  let superseding = $state(false);
  let replacement = $state("");

  const canEdit = $derived(canWrite && !!entry);
  const replacements = $derived(
    entries.filter((e) => e.status === "active" && e.key !== entryKey),
  );

  function emptyForm(): Form {
    return {
      category: "architecture",
      title: "",
      summary: "",
      body: "",
      pinned: false,
      sources: [],
    };
  }

  function toForm(e: ContextEntry): Form {
    return {
      category: e.category,
      title: e.title,
      summary: e.summary,
      body: e.body ?? "",
      pinned: e.pinned,
      sources: e.sources.map((s) => ({
        type: s.type,
        path: s.path ?? "",
        ticket: s.ticket ?? "",
        url: s.url ?? "",
        note: s.note ?? "",
        keep: {
          ...(s.repository && { repository: s.repository }),
          ...(s.commit_sha && { commit_sha: s.commit_sha }),
          ...(s.content_hash && { content_hash: s.content_hash }),
        },
      })),
    };
  }

  function toDraft(f: Form): ContextDraft {
    return {
      category: f.category,
      title: f.title.trim(),
      summary: f.summary.trim(),
      body: f.body,
      pinned: f.pinned,
      sources: f.sources.map((s) => {
        const locator =
          s.type === "ticket"
            ? { ticket: s.ticket.trim() }
            : s.type === "url"
              ? { url: s.url.trim() }
              : { path: s.path.trim() };
        return {
          type: s.type,
          ...locator,
          ...(s.note.trim() && { note: s.note.trim() }),
          ...s.keep,
        };
      }),
    };
  }

  function apply(e: ContextEntry): void {
    entry = e;
    form = toForm(e);
    conflict = undefined;
  }

  async function load(): Promise<void> {
    try {
      apply(await getContextEntry(project, entryKey));
      loadError = false;
    } catch {
      loadError = true;
    }
  }

  function fail(e: unknown): void {
    if (e instanceof ApiError && e.status === 409) {
      const version = e.current?.["version"];
      conflict = typeof version === "number" ? version : (entry?.version ?? 0);
      error = "";
      return;
    }
    error = t("context.saveFailed", {
      message: e instanceof Error ? e.message : "",
    });
  }

  async function save(event: Event, version = entry?.version): Promise<void> {
    event.preventDefault();
    if (saving) return;
    saving = true;
    error = "";
    notice = "";
    try {
      const draft = toDraft(form);
      if (!entry || version === undefined) {
        const made = await createContext(project, draft);
        onsaved();
        onselect(made.key);
        return;
      }
      const saved = await updateContext(project, entry.key, version, draft);
      apply({ ...saved, body: draft.body });
      editing = false;
      notice = t("context.saved");
      onsaved();
    } catch (e) {
      fail(e);
    } finally {
      saving = false;
    }
  }

  // After a conflict the user either takes what the other person saved or saves their own text over it.
  async function useCurrent(): Promise<void> {
    await load();
    editing = false;
  }

  async function action(run: () => Promise<ContextEntry>): Promise<void> {
    error = "";
    notice = "";
    try {
      await run();
      onsaved();
      await load();
      superseding = false;
    } catch (e) {
      fail(e);
    }
  }

  function addSource(): void {
    if (form.sources.length >= 10) return;
    form.sources.push({
      type: "file",
      path: "",
      ticket: "",
      url: "",
      note: "",
      keep: {},
    });
  }

  onMount(() => {
    dialog.showModal();
    if (entryKey) void load();
  });

  function backdropClick(event: MouseEvent): void {
    if (event.target === dialog) dialog.close();
  }
</script>

<!-- The native dialog gives focus trapping, Esc to close and focus return for free. Clicking the backdrop is a pointer convenience on top of that. -->
<dialog
  bind:this={dialog}
  aria-labelledby="context-panel-title"
  {onclose}
  onclick={backdropClick}
>
  <header class="bar">
    <h2 id="context-panel-title">
      {entry ? entry.title : t("context.newTitle")}
    </h2>
    {#if entry}
      <span class="chip state {entry.verification_state}"
        >{t(`context.state.${entry.verification_state}`)}</span
      >
      {#if entry.status !== "active"}<span class="chip"
          >{t(`context.status.${entry.status}`)}</span
        >{/if}
    {/if}
    <span class="spacer"></span>
    <button
      type="button"
      class="iconbtn"
      aria-label={t("context.close")}
      onclick={() => {
        dialog.close();
      }}><Icon name="close" /></button
    >
  </header>

  {#if loadError}
    <p class="banner error pad" role="alert">{t("context.loadFailed")}</p>
  {:else if entryKey && !entry}
    <p class="pad muted">…</p>
  {:else}
    <div class="scroll">
      {#if conflict !== undefined}
        <p class="banner conflict" role="alert">
          <Icon name="alert" size={16} />
          <span>{t("context.conflict", { version: conflict })}</span>
          <button type="button" onclick={() => void useCurrent()}
            >{t("context.useCurrent")}</button
          >
          <button type="button" onclick={(e) => void save(e, conflict)}
            >{t("context.overwrite")}</button
          >
        </p>
      {:else if error}
        <p class="banner error" role="alert">{error}</p>
      {:else if notice}
        <p class="banner ok" role="status">{notice}</p>
      {/if}

      {#if editing || !entry}
        <form id="context-form" class="fields" onsubmit={(e) => void save(e)}>
          <label>
            {t("context.category")}
            <select bind:value={form.category}>
              {#each CONTEXT_CATEGORIES as c (c)}
                <option value={c}>{t(`context.categories.${c}`)}</option>
              {/each}
            </select>
          </label>
          <label>
            {t("context.titleField")}
            <input bind:value={form.title} maxlength="160" required />
          </label>
          <label>
            {t("context.summary")}
            <textarea
              bind:value={form.summary}
              rows="3"
              maxlength={SUMMARY_MAX}
              required></textarea>
            <span class="hint"
              >{t("context.summaryCount", { count: form.summary.length })}</span
            >
          </label>
          <label>
            {t("context.body")}
            <textarea bind:value={form.body} rows="9" maxlength="12288"
            ></textarea>
          </label>
          <label class="check">
            <input type="checkbox" bind:checked={form.pinned} />
            {t("context.pinField")}
          </label>

          <fieldset>
            <legend>{t("context.sources")}</legend>
            {#each form.sources as src, i (i)}
              <div class="source">
                <select
                  aria-label={t("context.sourceType")}
                  bind:value={src.type}
                >
                  {#each CONTEXT_SOURCE_TYPES as s (s)}
                    <option value={s}>{t(`context.sourceTypes.${s}`)}</option>
                  {/each}
                </select>
                {#if src.type === "ticket"}
                  <input
                    aria-label={t("context.sourceTicket")}
                    placeholder={t("context.sourceTicket")}
                    bind:value={src.ticket}
                    required
                  />
                {:else if src.type === "url"}
                  <input
                    type="url"
                    aria-label={t("context.sourceUrl")}
                    placeholder={t("context.sourceUrl")}
                    bind:value={src.url}
                    required
                  />
                {:else}
                  <input
                    aria-label={t("context.sourcePath")}
                    placeholder={t("context.sourcePath")}
                    bind:value={src.path}
                    required
                  />
                {/if}
                <input
                  aria-label={t("context.sourceNote")}
                  placeholder={t("context.sourceNote")}
                  maxlength="200"
                  bind:value={src.note}
                />
                <button
                  type="button"
                  class="iconbtn"
                  aria-label={t("context.removeSource")}
                  onclick={() => {
                    form.sources.splice(i, 1);
                  }}><Icon name="trash" size={16} /></button
                >
              </div>
            {/each}
            {#if form.sources.length < 10}
              <button type="button" class="add" onclick={addSource}
                ><Icon name="plus" size={14} />{t("context.addSource")}</button
              >
            {/if}
          </fieldset>
        </form>
      {:else if entry}
        <p class="summary">{entry.summary}</p>
        {#if entry.body}
          <!-- Safe: markdown-it runs with raw HTML off and refuses javascript: links (see markdown.ts). -->
          <article class="body">
            <!-- eslint-disable-next-line svelte/no-at-html-tags -->
            {@html renderMarkdown(entry.body)}
          </article>
        {/if}

        <section class="panel">
          <h3>{t("context.sources")}</h3>
          {#if entry.sources.length === 0}
            <p class="muted">{t("context.noSources")}</p>
          {:else}
            <ul class="sources">
              {#each entry.sources as s, i (i)}
                <li>
                  <span class="chip">{t(`context.sourceTypes.${s.type}`)}</span>
                  {#if s.type === "url" && s.url}<a
                      href={s.url}
                      target="_blank"
                      rel="noopener noreferrer">{s.url}</a
                    >
                  {:else}<code>{s.path ?? s.ticket}</code>{/if}
                  {#if s.note}<span class="muted">{s.note}</span>{/if}
                </li>
              {/each}
            </ul>
          {/if}
        </section>

        <p class="meta">
          {t("context.version", { version: entry.version })} ·
          {t("context.updated", { date: dateTime.format(entry.updated_at) })}
          {#if entry.verified_at && entry.verification_state === "verified"}
            · {t("context.verifiedBy", {
              date: dateTime.format(entry.verified_at),
            })}{/if}
          {#if entry.superseded_by}
            · {t("context.supersededBy", { key: entry.superseded_by })}{/if}
        </p>

        {#if superseding}
          <div class="panel">
            <label>
              {t("context.supersedeBy")}
              <select bind:value={replacement}>
                <option value="">{t("context.supersedeChoose")}</option>
                {#each replacements as r (r.key)}
                  <option value={r.key}>{r.title}</option>
                {/each}
              </select>
            </label>
            <button
              type="button"
              disabled={!replacement}
              onclick={() =>
                void action(() =>
                  supersedeContext(
                    project,
                    entry?.key ?? "",
                    replacement,
                    entry?.version ?? 0,
                  ),
                )}>{t("context.supersedeConfirm")}</button
            >
          </div>
        {/if}
      {/if}
    </div>

    <footer class="foot">
      {#if editing || !entry}
        <button
          type="submit"
          form="context-form"
          class="primary"
          disabled={saving}
          >{saving ? t("context.saving") : t("context.save")}</button
        >
        <button
          type="button"
          onclick={() => {
            if (entry) {
              form = toForm(entry);
              editing = false;
              error = "";
            } else dialog.close();
          }}>{t("context.cancel")}</button
        >
      {:else if entry}
        {#if canEdit}
          <button
            type="button"
            class="primary"
            onclick={() => {
              editing = true;
              notice = "";
            }}><Icon name="edit" size={16} />{t("context.edit")}</button
          >
        {/if}
        {#if canVerify && entry.status === "active"}
          <button
            type="button"
            title={t("context.verifyHint")}
            disabled={entry.verification_state === "verified"}
            onclick={() =>
              void action(() =>
                verifyContext(project, entry?.key ?? "", entry?.version ?? 0),
              )}><Icon name="check" size={16} />{t("context.verify")}</button
          >
        {/if}
        {#if canEdit && entry.status === "active"}
          <span class="spacer"></span>
          <button
            type="button"
            onclick={() => {
              superseding = !superseding;
            }}>{t("context.supersede")}</button
          >
          <button
            type="button"
            onclick={() =>
              void action(() => archiveContext(project, entry?.key ?? ""))}
            ><Icon name="archive" size={16} />{t("context.archive")}</button
          >
        {/if}
      {/if}
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
  .bar,
  .foot {
    flex: none;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    padding: 12px 20px;
    background: var(--atlas-sidebar);
  }
  .bar {
    border-bottom: 1px solid var(--color-border);
  }
  .foot {
    border-top: 1px solid var(--color-border);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-md);
    overflow-wrap: anywhere;
  }
  h3 {
    margin: 0;
    font-size: var(--atlas-fs-md);
  }
  .spacer {
    flex: 1;
  }
  .iconbtn {
    display: inline-grid;
    place-items: center;
    min-height: 34px;
    padding: 4px 8px;
  }
  .scroll {
    flex: 1;
    display: grid;
    align-content: start;
    gap: var(--space-4);
    padding: 20px;
    overflow-y: auto;
  }
  .pad {
    margin: 20px;
  }
  .fields {
    display: grid;
    gap: var(--space-3);
  }
  label,
  legend {
    display: grid;
    gap: 4px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
  }
  .check {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .hint {
    justify-self: end;
    font-weight: var(--atlas-fw-regular, 400);
    color: var(--color-muted);
  }
  textarea {
    line-height: 1.55;
    background: var(--color-bg);
  }
  fieldset {
    display: grid;
    gap: var(--space-2);
    margin: 0;
    padding: 12px;
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  .source {
    display: grid;
    grid-template-columns: 110px minmax(0, 1.4fr) minmax(0, 1fr) auto;
    gap: var(--space-2);
  }
  .add {
    justify-self: start;
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .summary {
    margin: 0;
    font-size: var(--atlas-fs-md);
    line-height: 1.55;
    overflow-wrap: anywhere;
  }
  .body {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .body :global(pre) {
    overflow-x: auto;
    padding: 12px;
    background: var(--color-soft);
    border-radius: var(--atlas-radius-md);
  }
  .body :global(table) {
    display: block;
    max-width: 100%;
    overflow-x: auto;
    border-collapse: collapse;
  }
  .body :global(th),
  .body :global(td) {
    padding: 4px 10px;
    border: 1px solid var(--color-border);
  }
  .panel {
    display: grid;
    gap: var(--space-3);
    padding: 16px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-md);
  }
  .sources {
    display: grid;
    gap: 6px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .sources li {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-2);
    font-size: var(--atlas-fs-sm);
    overflow-wrap: anywhere;
  }
  .meta {
    margin: 0;
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
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
  .state.verified {
    border-color: transparent;
    background: var(--color-success-soft);
    color: var(--color-success);
  }
  .state.stale {
    border-color: transparent;
    background: var(--color-warning-soft);
    color: var(--color-warning);
  }
  .foot button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  @media (max-width: 720px) {
    dialog {
      width: 100vw;
      border-left: none;
    }
    .source {
      grid-template-columns: minmax(0, 1fr);
    }
    .source .iconbtn {
      justify-self: end;
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
