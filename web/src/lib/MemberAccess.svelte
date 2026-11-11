<script lang="ts">
  import {
    ApiError,
    getMemberAccess,
    type OrgMember,
    type Project,
    setMemberProjects,
  } from "./api";
  import { t } from "./i18n";
  import ProjectPicker from "./ProjectPicker.svelte";

  // One member's effective access (every project with its source) and, for a plain member, the
  // explicit project assignment as one atomic set.
  let {
    orgKey,
    member,
    projects,
  }: { orgKey: string; member: OrgMember; projects: Project[] } = $props();

  let access = $state<{ key: string; name: string; source: string }[]>([]);
  let selected = $state<string[]>([]);
  let message = $state("");
  let error = $state("");

  async function load(): Promise<void> {
    try {
      const res = await getMemberAccess(orgKey, member.user_id);
      const list = res.organizations.find((o) => o.key === orgKey)?.projects;
      access = list ?? [];
      selected = access
        .filter((p) => p.source === "explicit")
        .map((p) => p.key);
    } catch (e) {
      error = t("company.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }
  void load();

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    message = error = "";
    try {
      await setMemberProjects(orgKey, member.user_id, selected);
      message = t("company.access.saved");
      await load();
    } catch (e) {
      error = t("company.failed", {
        message: e instanceof ApiError ? e.message : String(e),
      });
    }
  }
</script>

<div class="access">
  {#if error}<p class="banner error" role="alert">{error}</p>{/if}
  {#if message}<p class="banner ok" role="status">{message}</p>{/if}
  <h3>{t("company.access.effective")}</h3>
  {#if access.length === 0}
    <p class="muted">{t("company.access.none")}</p>
  {:else}
    <ul>
      {#each access as p (p.key)}
        <li>
          <strong>{p.name}</strong>
          <span class="muted">{p.key}</span>
          <span class="muted"
            >{t(
              `company.access.source.${p.source}` as "company.access.source.explicit",
            )}</span
          >
        </li>
      {/each}
    </ul>
  {/if}
  {#if member.role === "user"}
    <form onsubmit={save}>
      <ProjectPicker
        {projects}
        bind:selected
        legend={t("company.access.assign")}
      />
      <button class="primary" type="submit">{t("company.access.save")}</button>
    </form>
  {/if}
</div>

<style>
  .access {
    flex-basis: 100%;
    padding: var(--space-2) 0;
  }
  ul {
    list-style: none;
    margin: 0 0 var(--space-3);
    padding: 0;
  }
  li {
    display: flex;
    gap: var(--space-2);
    padding: 2px 0;
  }
  h3 {
    margin: 0 0 var(--space-2);
    font-size: 1rem;
  }
</style>
