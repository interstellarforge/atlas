<script lang="ts">
  import { ApiError, getMyAccess, type MyAccess } from "./api";
  import EmptyState from "./EmptyState.svelte";
  import { t } from "./i18n";
  import PageHero from "./PageHero.svelte";

  let access = $state<MyAccess | undefined>();
  let error = $state("");

  void getMyAccess().then(
    (a) => (access = a),
    (e: unknown) =>
      (error = t("access.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      })),
  );

  function sourceLabel(source: string): string {
    return t(`access.source.${source}` as "access.source.explicit");
  }
</script>

<PageHero title={t("access.title")} description={t("access.intro")} />

{#if error}
  <p class="banner error" role="alert">{error}</p>
{:else if access}
  {#each access.organizations as o (o.key)}
    <section class="panel" aria-labelledby="org-{o.key}">
      <h2 id="org-{o.key}">
        {o.name}
        <span class="muted"
          >{o.key} · {t(`access.role.${o.role}` as "access.role.user")}</span
        >
      </h2>
      {#if o.projects.length === 0}
        <p class="muted">{t("access.noProjects")}</p>
      {:else}
        <ul class="list">
          {#each o.projects as p (p.key)}
            <li>
              <a href="#/p/{p.key}"><strong>{p.name}</strong></a>
              <span class="muted">{p.key}</span>
              <span class="muted">{sourceLabel(p.source)}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </section>
  {:else}
    <EmptyState icon="projects" title={t("access.empty")} />
  {/each}
{/if}

<style>
  .panel {
    margin-bottom: var(--space-4);
  }
  .list {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .list li {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    padding: 10px 0;
    border-top: 1px solid var(--color-border);
  }
  .list li:first-child {
    border-top: none;
  }
</style>
