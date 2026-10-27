<script lang="ts">
  import { ApiError, getReadme, type Readme } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import { renderMarkdown } from "./markdown";

  // The README of the linked repository, fetched by the server (ADR-0013). Relative links and images were
  // already pointed at the repository there; raw HTML stays off in the renderer.
  let { project }: { project: string } = $props();

  let readme = $state<Readme | undefined>();
  let loading = $state(false);
  let error = $state("");

  const time = new Intl.DateTimeFormat(undefined, {
    dateStyle: "medium",
    timeStyle: "short",
  });

  async function load(refresh: boolean): Promise<void> {
    loading = true;
    error = "";
    try {
      readme = await getReadme(project, refresh);
    } catch (e) {
      error = t("readme.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    } finally {
      loading = false;
    }
  }

  $effect(() => {
    readme = undefined;
    void load(false);
  });
</script>

<section class="readme" aria-labelledby="readme-title" aria-busy={loading}>
  <header>
    <h2 id="readme-title">{t("readme.title")}</h2>
    <span class="grow"></span>
    {#if readme?.fetched_at}
      <small class="muted"
        >{t("readme.fetched", { date: time.format(readme.fetched_at) })}</small
      >
    {/if}
    {#if readme?.source_url}
      <a href={readme.source_url} target="_blank" rel="noopener noreferrer"
        ><Icon name="external" size={14} />{t("readme.open")}</a
      >
    {/if}
    <button type="button" disabled={loading} onclick={() => void load(true)}
      ><Icon name="restore" size={14} />{t("readme.refresh")}</button
    >
  </header>

  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if readme?.state === "ok"}
    <!-- Safe: markdown-it runs with raw HTML off (see markdown.ts); images come from the project's own proxy. -->
    <article class="body">
      <!-- eslint-disable-next-line svelte/no-at-html-tags -->
      {@html renderMarkdown(readme.markdown ?? "")}
    </article>
  {:else if readme?.state === "empty"}
    <EmptyState icon="list" title={t("readme.empty")} />
  {:else if readme?.state === "unlinked"}
    <EmptyState icon="link" title={t("readme.unlinked")} />
  {:else if readme?.state === "error"}
    <p class="banner error" role="alert">
      {t("readme.failed", { message: readme.message ?? "" })}
    </p>
  {:else if !error}
    <p class="muted">{t("readme.loading")}</p>
  {/if}
</section>

<style>
  .readme {
    display: grid;
    gap: var(--space-3);
    align-content: start;
    overflow-y: auto;
    max-height: 100%;
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  header {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--space-3);
  }
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  .grow {
    flex: 1;
  }
  header a,
  header button {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .body {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .body :global(img) {
    max-width: 100%;
    height: auto;
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
</style>
