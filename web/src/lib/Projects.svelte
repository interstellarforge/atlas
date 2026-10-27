<script lang="ts">
  import {
    ApiError,
    createProject,
    getProgress,
    listProjectTemplates,
    type Progress,
    type Project,
    type ProjectTemplate,
  } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { t } from "./i18n";
  import Icon from "./Icon.svelte";
  import PageHero from "./PageHero.svelte";
  import ProgressBar from "./ProgressBar.svelte";
  import { projectColor, projectCoverName } from "./projectColor";

  let { projects, onchange }: { projects: Project[]; onchange: () => void } =
    $props();

  let creating = $state(false);
  let key = $state("");
  let name = $state("");
  let error = $state("");
  let query = $state("");
  let template = $state("0");
  let projectTemplates = $state<ProjectTemplate[]>([]);
  void listProjectTemplates().then((list) => (projectTemplates = list));
  let counts = $state<Record<string, Progress>>({});

  const shown = $derived(
    projects.filter((p) =>
      `${p.key} ${p.name}`.toLowerCase().includes(query.trim().toLowerCase()),
    ),
  );

  // One grouped query per project (ADR-0014); the compact bar shows the split by status.
  $effect(() => {
    for (const project of projects) {
      if (counts[project.key]) continue;
      void getProgress(project.key)
        .then((p) => (counts[project.key] = p))
        .catch(() => undefined);
    }
  });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      await createProject(key, name, Number(template));
      key = "";
      name = "";
      creating = false;
      onchange();
    } catch (e) {
      error = t("projects.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }

  const covers = Object.fromEntries(
    Object.entries(
      import.meta.glob<string>("../assets/atlas/projects/*.webp", {
        eager: true,
        import: "default",
      }),
    ).map(([path, url]) => [path.split("/").pop()?.replace(".webp", ""), url]),
  ) as Record<string, string>;
</script>

<PageHero title={t("home.title")} description={t("home.intro")}>
  {#snippet actions()}
    <button
      type="button"
      class="primary withicon"
      aria-expanded={creating}
      onclick={() => (creating = !creating)}
      ><Icon name="plus" size={16} />{t("projects.create")}</button
    >
  {/snippet}
</PageHero>

{#if creating || projects.length === 0}
  <form class="newform" onsubmit={submit}>
    <h2>{t("projects.create")}</h2>
    <div class="fields">
      <label>
        {t("projects.key")}
        <input
          bind:value={key}
          required
          maxlength="10"
          pattern={"[A-Za-z][A-Za-z0-9]{1,9}"}
        />
      </label>
      <label>
        {t("projects.name")}
        <input bind:value={name} required />
      </label>
      {#if projectTemplates.length > 0}
        <label>
          {t("projects.template")}
          <select bind:value={template}>
            <option value="0">{t("projects.defaultTemplate")}</option>
            {#each projectTemplates as tpl (tpl.id)}
              <option value={String(tpl.id)}>{tpl.name}</option>
            {/each}
          </select>
        </label>
      {/if}
      <button class="primary" type="submit">{t("projects.submit")}</button>
    </div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
{/if}

{#if projects.length === 0}
  <div class="emptycard">
    <EmptyState
      icon="projects"
      title={t("home.emptyTitle")}
      text={t("home.empty")}
    />
  </div>
{:else}
  <div class="bar">
    <div class="search">
      <Icon name="search" size={16} />
      <input
        type="search"
        placeholder={t("home.filter")}
        aria-label={t("home.filter")}
        bind:value={query}
      />
    </div>
    <span class="muted">{t("home.count", { count: shown.length })}</span>
  </div>
  <ul class="grid">
    {#each shown as project (project.key)}
      {@const c = counts[project.key]}
      <li style:--accent={projectColor(project.key)}>
        <a class="cardlink" href="#/p/{project.key}">
          <img
            class="cover"
            src={covers[projectCoverName(project.key)]}
            width="1280"
            height="720"
            alt=""
            loading="lazy"
            decoding="async"
          />
          <div class="body">
            <div class="head">
              <span class="tile">{project.key.slice(0, 1)}</span>
              <div class="titles">
                <strong>{project.name}</strong>
                <span class="key">{project.key}</span>
              </div>
              {#if project.role}<span class="chip">{project.role}</span>{/if}
            </div>
            {#if c}
              <ProgressBar progress={c} compact />
              <p class="stats">
                <span><b>{c.total}</b> {t("home.items")}</span>
                <span><b>{c.categories.active}</b> {t("home.inProgress")}</span>
                <span><b>{c.categories.completed}</b> {t("home.done")}</span>
              </p>
            {:else}
              <p class="stats muted">…</p>
            {/if}
          </div>
        </a>
      </li>
    {/each}
  </ul>
{/if}

<style>
  h2 {
    margin: 0;
    font-size: var(--atlas-fs-lg);
  }
  .withicon {
    display: inline-flex;
    align-items: center;
    gap: 6px;
  }
  .newform,
  .emptycard {
    padding: 20px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
  }
  .newform {
    display: grid;
    gap: var(--space-3);
  }
  .fields {
    display: grid;
    grid-template-columns: 160px minmax(0, 1fr) auto;
    gap: var(--space-3);
    align-items: end;
  }
  label {
    display: grid;
    gap: 6px;
    font-size: var(--atlas-fs-sm);
    font-weight: var(--atlas-fw-semibold);
    color: var(--color-muted);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .search {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex: 1;
    max-width: 360px;
    padding-left: 12px;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
    color: var(--color-muted);
  }
  .search input {
    flex: 1;
    border: none;
    background: transparent;
    padding-left: 0;
  }
  .grid {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: var(--space-4);
  }
  .cardlink {
    display: block;
    height: 100%;
    overflow: hidden;
    color: var(--color-text);
    text-decoration: none;
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--atlas-radius-lg);
    transition:
      border-color var(--atlas-duration) var(--atlas-ease),
      transform var(--atlas-duration) var(--atlas-ease);
  }
  .cardlink:hover {
    border-color: var(--accent);
    transform: translateY(-2px);
  }
  .cover {
    display: block;
    width: 100%;
    height: 84px;
    object-fit: cover;
    object-position: center 75%;
  }
  .body {
    display: grid;
    gap: var(--space-3);
    padding: 16px 18px 18px;
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    margin-top: -34px;
  }
  .tile {
    flex: none;
    display: grid;
    place-items: center;
    width: 44px;
    height: 44px;
    border-radius: var(--atlas-radius-md);
    border: 2px solid var(--color-surface);
    background: var(--accent);
    color: #06101f;
    font-size: 20px;
    font-weight: var(--atlas-fw-bold);
  }
  .titles {
    flex: 1;
    min-width: 0;
    display: grid;
    margin-top: 30px;
  }
  .titles strong {
    font-size: var(--atlas-fs-lg);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .key {
    font: var(--atlas-fw-semibold) var(--atlas-fs-xs) var(--font-mono);
    color: var(--color-muted);
  }
  .head .chip {
    margin-top: 30px;
  }
  .stats {
    margin: 0;
    display: flex;
    flex-wrap: wrap;
    gap: 4px var(--space-4);
    font-size: var(--atlas-fs-sm);
    color: var(--color-muted);
  }
  .stats b {
    color: var(--color-text);
  }
  @media (max-width: 1100px) {
    .grid {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  @media (max-width: 640px) {
    .grid {
      grid-template-columns: minmax(0, 1fr);
    }
    .fields {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
