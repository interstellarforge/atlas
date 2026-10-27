<script lang="ts">
  import { ApiError, createProject, listProjects, type Project } from "./api";
  import { t } from "./i18n";

  let projects = $state<Project[]>([]);
  let loaded = $state(false);
  let key = $state("");
  let name = $state("");
  let error = $state("");

  async function load(): Promise<void> {
    projects = await listProjects();
    loaded = true;
  }
  void load();

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = "";
    try {
      await createProject(key, name);
      key = "";
      name = "";
      await load();
    } catch (e) {
      error = t("projects.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }
</script>

<h1>{t("home.title")}</h1>

{#if loaded && projects.length === 0}
  <p class="muted">{t("home.empty")}</p>
{/if}

<ul class="projects">
  {#each projects as project (project.key)}
    <li>
      <a href="#/p/{project.key}">
        <strong>{project.key}</strong>
        <span>{project.name}</span>
      </a>
      {#if project.role}<small class="muted"
          >{t("home.role", { role: project.role })}</small
        >{/if}
    </li>
  {/each}
</ul>

<form class="card" onsubmit={submit}>
  <h2>{t("projects.create")}</h2>
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
  {#if error}<p class="error" role="alert">{error}</p>{/if}
  <button class="primary" type="submit">{t("projects.submit")}</button>
</form>

<style>
  h2 {
    margin: 0;
    font-size: 1rem;
  }
  .projects {
    list-style: none;
    margin: 0;
    padding: 0;
    display: grid;
    gap: var(--space-2);
  }
  .projects li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
    padding: var(--space-3) var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  .projects a {
    display: flex;
    gap: var(--space-3);
    color: var(--color-text);
    text-decoration: none;
  }
  .card {
    display: grid;
    gap: var(--space-3);
    max-width: 24rem;
    padding: var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-border);
    border-radius: var(--radius);
  }
  label {
    display: grid;
    gap: var(--space-1);
    font-size: 0.875rem;
    color: var(--color-muted);
  }
</style>
